package app

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"licentivo-launch/internal/origin"
	"licentivo-launch/internal/storage"
)

type TransportSettingsInput struct {
	Mode         string `json:"mode"`
	PublicURL    string `json:"public_url"`
	AllowSDKHTTP *bool  `json:"allow_sdk_http"`
}

type TransportSettingsView struct {
	Mode                  string `json:"mode"`
	PublicURL             string `json:"public_url"`
	RedirectURL           string `json:"redirect_url,omitempty"`
	HTTPSListenerEnabled  bool   `json:"https_listener_enabled"`
	TLSCertificatePresent bool   `json:"tls_certificate_present"`
	AllowSDKHTTP          bool   `json:"allow_sdk_http"`
}

func normalizePublicOrigin(raw, mode string) (string, error) {
	if mode != "http" && mode != "https" {
		return "", ErrInvalid
	}
	publicURL, err := origin.Normalize(raw)
	if err != nil || !strings.HasPrefix(publicURL, mode+"://") {
		return "", problem(400, "PUBLIC_URL_INVALID", "访问地址必须是与所选协议一致且不带路径的完整网址")
	}
	return publicURL, nil
}

func (s *Service) TransportSettings(ctx context.Context, p Principal) (TransportSettingsView, error) {
	if !p.IsAdmin() {
		return TransportSettingsView{}, ErrForbidden
	}
	var settings storage.InstanceSettings
	if err := s.db.WithContext(ctx).First(&settings, "id = ?", 1).Error; err != nil {
		return TransportSettingsView{}, ErrStorage
	}
	publicURL := s.PublicURL()
	mode := "http"
	if parsed, err := url.Parse(publicURL); err == nil && parsed.Scheme == "https" {
		mode = "https"
	}
	return TransportSettingsView{
		Mode:                  mode,
		PublicURL:             publicURL,
		HTTPSListenerEnabled:  s.config.TLSListenerEnabled,
		TLSCertificatePresent: settings.TLSSecretID != nil && *settings.TLSSecretID != "",
		AllowSDKHTTP:          settings.AllowSDKHTTP,
	}, nil
}

func (s *Service) validateStoredTLSForHostname(tx *gorm.DB, settings storage.InstanceSettings, hostname string) error {
	if settings.TLSSecretID == nil || *settings.TLSSecretID == "" {
		return problem(409, "TLS_CERTIFICATE_REQUIRED", "请先上传有效的 SSL/TLS 证书和私钥")
	}
	var record storage.SecretRecord
	if err := tx.First(&record, "id = ?", *settings.TLSSecretID).Error; err != nil {
		return ErrStorage
	}
	plain, err := s.vault.Open(record.Ciphertext, "secret:"+record.ID)
	if err != nil {
		return ErrStorage
	}
	var bundle managedTLSBundle
	if json.Unmarshal(plain, &bundle) != nil {
		return ErrStorage
	}
	_, _, err = validateManagedTLSBundle(bundle.CertificatePEM, bundle.PrivateKeyPEM, hostname, now())
	return err
}

func (s *Service) SaveTransportSettings(ctx context.Context, p Principal, in TransportSettingsInput) (TransportSettingsView, error) {
	if !p.IsAdmin() {
		return TransportSettingsView{}, ErrForbidden
	}
	publicURL, err := normalizePublicOrigin(in.PublicURL, in.Mode)
	if err != nil {
		return TransportSettingsView{}, err
	}
	parsed, _ := url.Parse(publicURL)
	// A proxy may already serve this HTTPS origin without a Launch listener.
	// Keep it usable when saving other transport settings; enabling a new
	// HTTPS origin through this endpoint still requires the managed listener.
	keepProxyHTTPS := in.Mode == "https" && !s.config.TLSListenerEnabled && publicURL == s.PublicURL()
	if in.Mode == "https" && !s.config.TLSListenerEnabled && !keepProxyHTTPS {
		return TransportSettingsView{}, problem(409, "HTTPS_LISTENER_DISABLED", "服务器尚未启用内置 HTTPS 监听，请先配置 LAUNCH_HTTPS_ADDR 并重启")
	}
	certificatePresent := false
	allowSDKHTTP := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var settings storage.InstanceSettings
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&settings, "id = ?", 1).Error; err != nil {
			return err
		}
		certificatePresent = settings.TLSSecretID != nil && *settings.TLSSecretID != ""
		allowSDKHTTP = settings.AllowSDKHTTP
		if in.AllowSDKHTTP != nil {
			allowSDKHTTP = *in.AllowSDKHTTP
		}
		if in.Mode == "https" && !keepProxyHTTPS {
			if err := s.validateStoredTLSForHostname(tx, settings, parsed.Hostname()); err != nil {
				return err
			}
		}
		if err := tx.Model(&settings).Updates(map[string]any{"public_origin": publicURL, "allow_sdk_http": allowSDKHTTP}).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "transport.switch", "instance", settings.InstanceRef, "switch to "+in.Mode)
	})
	if err != nil {
		return TransportSettingsView{}, databaseError(err)
	}
	s.publicURL.Store(publicURL)
	return TransportSettingsView{
		Mode:                  in.Mode,
		PublicURL:             publicURL,
		RedirectURL:           publicURL + "/#/admin/settings",
		HTTPSListenerEnabled:  s.config.TLSListenerEnabled,
		TLSCertificatePresent: certificatePresent,
		AllowSDKHTTP:          allowSDKHTTP,
	}, nil
}

// SDKHTTPEnabled is a merchant-controlled opt-in, independent of the Licentivo API key.
func (s *Service) SDKHTTPEnabled(ctx context.Context) (bool, error) {
	var settings storage.InstanceSettings
	if err := s.db.WithContext(ctx).Select("allow_sdk_http").First(&settings, "id = ?", 1).Error; err != nil {
		return false, ErrStorage
	}
	return settings.AllowSDKHTTP, nil
}

func (s *Service) RequireSDKTransport(ctx context.Context, environment string) error {
	if strings.HasPrefix(s.PublicURL(), "https://") {
		return nil
	}
	if environment != "sandbox" {
		return problem(403, "SDK_HTTPS_REQUIRED", "正式环境的 SDK 接入必须使用 HTTPS，请更新 Launch 地址和客户端接入配置")
	}
	allowed, err := s.SDKHTTPEnabled(ctx)
	if err != nil {
		return err
	}
	if !allowed {
		return problem(403, "SDK_HTTP_DISABLED", "Launch 尚未允许 SDK 使用 HTTP，请在“系统设置 → 网站与安全”开启 HTTP 测试接入，或为 Launch 配置 HTTPS")
	}
	return nil
}
