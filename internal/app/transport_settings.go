package app

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"licentivo-launch/internal/storage"
)

type TransportSettingsInput struct {
	Mode      string `json:"mode"`
	PublicURL string `json:"public_url"`
}

type TransportSettingsView struct {
	Mode                  string `json:"mode"`
	PublicURL             string `json:"public_url"`
	RedirectURL           string `json:"redirect_url,omitempty"`
	HTTPSListenerEnabled  bool   `json:"https_listener_enabled"`
	TLSCertificatePresent bool   `json:"tls_certificate_present"`
}

func normalizePublicOrigin(raw, mode string) (string, error) {
	raw = strings.TrimSpace(raw)
	if mode != "http" && mode != "https" {
		return "", ErrInvalid
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != mode || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", problem(400, "PUBLIC_URL_INVALID", "访问地址必须是与所选协议一致且不带路径的完整网址")
	}
	if strings.ContainsAny(parsed.Host, "\r\n\t") {
		return "", ErrInvalid
	}
	for _, character := range parsed.Host {
		if character > 127 {
			return "", problem(400, "PUBLIC_URL_INVALID", "访问地址域名必须使用 ASCII 或 Punycode 格式")
		}
	}
	parsed.Path = ""
	return strings.TrimRight(parsed.String(), "/"), nil
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
	if in.Mode == "https" && !s.config.TLSListenerEnabled {
		return TransportSettingsView{}, problem(409, "HTTPS_LISTENER_DISABLED", "服务器尚未启用内置 HTTPS 监听，请先配置 LAUNCH_HTTPS_ADDR 并重启")
	}
	certificatePresent := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var settings storage.InstanceSettings
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&settings, "id = ?", 1).Error; err != nil {
			return err
		}
		certificatePresent = settings.TLSSecretID != nil && *settings.TLSSecretID != ""
		if in.Mode == "https" {
			if err := s.validateStoredTLSForHostname(tx, settings, parsed.Hostname()); err != nil {
				return err
			}
		}
		if err := tx.Model(&settings).Update("public_origin", publicURL).Error; err != nil {
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
	}, nil
}
