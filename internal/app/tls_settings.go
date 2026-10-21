package app

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"licentivo-launch/internal/storage"
)

const maxTLSBundleSize = 256 << 10

type TLSSettingsInput struct {
	CertificatePEM string `json:"certificate_pem"`
	PrivateKeyPEM  string `json:"private_key_pem"`
}

type TLSSettingsView struct {
	Configured          bool       `json:"configured"`
	ListenerEnabled     bool       `json:"listener_enabled"`
	Active              bool       `json:"active"`
	RestartRequired     bool       `json:"restart_required"`
	ExpectedHostname    string     `json:"expected_hostname,omitempty"`
	CommonName          string     `json:"common_name,omitempty"`
	Names               []string   `json:"names"`
	Issuer              string     `json:"issuer,omitempty"`
	SerialNumber        string     `json:"serial_number,omitempty"`
	FingerprintSHA256   string     `json:"fingerprint_sha256,omitempty"`
	NotBefore           *time.Time `json:"not_before,omitempty"`
	NotAfter            *time.Time `json:"not_after,omitempty"`
	UpdatedAt           *time.Time `json:"updated_at,omitempty"`
	Expired             bool       `json:"expired"`
	ExpiresWithin30Days bool       `json:"expires_within_30_days"`
}

type managedTLSBundle struct {
	CertificatePEM string `json:"certificate_pem"`
	PrivateKeyPEM  string `json:"private_key_pem"`
}

type managedTLSMetadata struct {
	CommonName        string    `json:"common_name,omitempty"`
	Names             []string  `json:"names"`
	Issuer            string    `json:"issuer"`
	SerialNumber      string    `json:"serial_number"`
	FingerprintSHA256 string    `json:"fingerprint_sha256"`
	NotBefore         time.Time `json:"not_before"`
	NotAfter          time.Time `json:"not_after"`
}

func expectedTLSHostname(publicURL string) string {
	parsed, err := url.Parse(publicURL)
	if err != nil || parsed.Scheme != "https" {
		return ""
	}
	return parsed.Hostname()
}

func certificateFingerprint(raw []byte) string {
	sum := sha256.Sum256(raw)
	plain := strings.ToUpper(hex.EncodeToString(sum[:]))
	parts := make([]string, 0, len(plain)/2)
	for index := 0; index < len(plain); index += 2 {
		parts = append(parts, plain[index:index+2])
	}
	return strings.Join(parts, ":")
}

func parseManagedTLSBundle(certificatePEM, privateKeyPEM string) (*tls.Certificate, *x509.Certificate, error) {
	certificatePEM = strings.TrimSpace(certificatePEM)
	privateKeyPEM = strings.TrimSpace(privateKeyPEM)
	if certificatePEM == "" || privateKeyPEM == "" {
		return nil, nil, problem(400, "TLS_CERTIFICATE_REQUIRED", "请同时提供 PEM 格式的证书链和私钥")
	}
	if len(certificatePEM)+len(privateKeyPEM) > maxTLSBundleSize || strings.ContainsRune(certificatePEM, 0) || strings.ContainsRune(privateKeyPEM, 0) {
		return nil, nil, problem(400, "TLS_CERTIFICATE_TOO_LARGE", "证书或私钥文件无效或过大")
	}
	pair, err := tls.X509KeyPair([]byte(certificatePEM), []byte(privateKeyPEM))
	if err != nil || len(pair.Certificate) == 0 {
		return nil, nil, problem(400, "TLS_KEY_PAIR_INVALID", "证书链与私钥无法解析或不匹配")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, nil, problem(400, "TLS_CERTIFICATE_INVALID", "无法解析服务器证书")
	}
	for _, raw := range pair.Certificate[1:] {
		if _, err := x509.ParseCertificate(raw); err != nil {
			return nil, nil, problem(400, "TLS_CERTIFICATE_CHAIN_INVALID", "证书链中包含无法解析的证书")
		}
	}
	switch key := pair.PrivateKey.(type) {
	case *rsa.PrivateKey:
		if key.N.BitLen() < 2048 {
			return nil, nil, problem(400, "TLS_PRIVATE_KEY_WEAK", "RSA 私钥至少需要 2048 位")
		}
	case *ecdsa.PrivateKey:
		if key.Curve.Params().BitSize < 256 {
			return nil, nil, problem(400, "TLS_PRIVATE_KEY_WEAK", "ECDSA 私钥强度不足")
		}
	case ed25519.PrivateKey:
	default:
		return nil, nil, problem(400, "TLS_PRIVATE_KEY_UNSUPPORTED", "不支持该私钥类型")
	}
	pair.Leaf = leaf
	return &pair, leaf, nil
}

func validateManagedTLSBundle(certificatePEM, privateKeyPEM, expectedHostname string, at time.Time) (*tls.Certificate, managedTLSMetadata, error) {
	pair, leaf, err := parseManagedTLSBundle(certificatePEM, privateKeyPEM)
	if err != nil {
		return nil, managedTLSMetadata{}, err
	}
	if at.Before(leaf.NotBefore) {
		return nil, managedTLSMetadata{}, problem(400, "TLS_CERTIFICATE_NOT_ACTIVE", "证书尚未到生效时间")
	}
	if !at.Before(leaf.NotAfter) {
		return nil, managedTLSMetadata{}, problem(400, "TLS_CERTIFICATE_EXPIRED", "证书已经过期")
	}
	if expectedHostname != "" {
		if err := leaf.VerifyHostname(expectedHostname); err != nil {
			return nil, managedTLSMetadata{}, problem(400, "TLS_HOSTNAME_MISMATCH", "证书不包含当前正式网址的域名 "+expectedHostname)
		}
	}
	if len(leaf.ExtKeyUsage) > 0 {
		serverUsage := false
		for _, usage := range leaf.ExtKeyUsage {
			if usage == x509.ExtKeyUsageServerAuth || usage == x509.ExtKeyUsageAny {
				serverUsage = true
				break
			}
		}
		if !serverUsage {
			return nil, managedTLSMetadata{}, problem(400, "TLS_CERTIFICATE_USAGE_INVALID", "证书不能用于 HTTPS 服务器身份验证")
		}
	}
	names := append([]string{}, leaf.DNSNames...)
	for _, address := range leaf.IPAddresses {
		names = append(names, address.String())
	}
	metadata := managedTLSMetadata{
		CommonName:        leaf.Subject.CommonName,
		Names:             names,
		Issuer:            leaf.Issuer.String(),
		SerialNumber:      strings.ToUpper(leaf.SerialNumber.Text(16)),
		FingerprintSHA256: certificateFingerprint(leaf.Raw),
		NotBefore:         leaf.NotBefore.UTC(),
		NotAfter:          leaf.NotAfter.UTC(),
	}
	return pair, metadata, nil
}

func (s *Service) managedTLSCertificate(ctx context.Context) (*tls.Certificate, error) {
	var settings storage.InstanceSettings
	if err := s.db.WithContext(ctx).First(&settings, "id = ?", 1).Error; err != nil {
		return nil, ErrStorage
	}
	if settings.TLSSecretID == nil || *settings.TLSSecretID == "" {
		return nil, nil
	}
	plain, err := s.readSecret(ctx, *settings.TLSSecretID)
	if err != nil {
		return nil, err
	}
	var bundle managedTLSBundle
	if json.Unmarshal(plain, &bundle) != nil {
		return nil, ErrStorage
	}
	pair, _, err := parseManagedTLSBundle(bundle.CertificatePEM, bundle.PrivateKeyPEM)
	if err != nil {
		return nil, ErrStorage
	}
	return pair, nil
}

func (s *Service) tlsSettingsView(settings storage.InstanceSettings) (TLSSettingsView, error) {
	view := TLSSettingsView{
		ListenerEnabled:  s.config.TLSListenerEnabled,
		ExpectedHostname: expectedTLSHostname(s.PublicURL()),
		Names:            []string{},
		UpdatedAt:        settings.TLSUpdatedAt,
	}
	if settings.TLSSecretID == nil || *settings.TLSSecretID == "" {
		return view, nil
	}
	var metadata managedTLSMetadata
	if len(settings.TLSMetadata) == 0 || json.Unmarshal(settings.TLSMetadata, &metadata) != nil {
		return TLSSettingsView{}, ErrStorage
	}
	view.Configured = true
	view.Active = s.config.TLSListenerEnabled && s.config.ApplyTLSCertificate != nil
	view.RestartRequired = !view.Active
	view.CommonName = metadata.CommonName
	view.Names = metadata.Names
	view.Issuer = metadata.Issuer
	view.SerialNumber = metadata.SerialNumber
	view.FingerprintSHA256 = metadata.FingerprintSHA256
	view.NotBefore = &metadata.NotBefore
	view.NotAfter = &metadata.NotAfter
	view.Expired = !now().Before(metadata.NotAfter)
	view.ExpiresWithin30Days = !view.Expired && metadata.NotAfter.Before(now().Add(30*24*time.Hour))
	return view, nil
}

func (s *Service) TLSSettings(ctx context.Context, p Principal) (TLSSettingsView, error) {
	if !p.IsAdmin() {
		return TLSSettingsView{}, ErrForbidden
	}
	var settings storage.InstanceSettings
	if err := s.db.WithContext(ctx).First(&settings, "id = ?", 1).Error; err != nil {
		return TLSSettingsView{}, ErrStorage
	}
	return s.tlsSettingsView(settings)
}

func (s *Service) SaveTLSSettings(ctx context.Context, p Principal, in TLSSettingsInput) (TLSSettingsView, error) {
	if !p.IsAdmin() {
		return TLSSettingsView{}, ErrForbidden
	}
	expectedHostname := expectedTLSHostname(s.PublicURL())
	pair, metadata, err := validateManagedTLSBundle(in.CertificatePEM, in.PrivateKeyPEM, expectedHostname, now())
	if err != nil {
		return TLSSettingsView{}, err
	}
	bundleJSON, err := json.Marshal(managedTLSBundle{CertificatePEM: strings.TrimSpace(in.CertificatePEM), PrivateKeyPEM: strings.TrimSpace(in.PrivateKeyPEM)})
	if err != nil {
		return TLSSettingsView{}, ErrStorage
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return TLSSettingsView{}, ErrStorage
	}
	updated := now()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var settings storage.InstanceSettings
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&settings, "id = ?", 1).Error; err != nil {
			return err
		}
		oldSecretID := settings.TLSSecretID
		secretID, err := s.createSecret(tx, "tls_certificate_bundle", bundleJSON)
		if err != nil {
			return err
		}
		if err := tx.Model(&settings).Updates(map[string]any{"tls_secret_id": secretID, "tls_metadata": metadataJSON, "tls_updated_at": updated}).Error; err != nil {
			return err
		}
		action := "tls.configure"
		if oldSecretID != nil && *oldSecretID != "" {
			action = "tls.replace"
			if err := tx.Delete(&storage.SecretRecord{}, "id = ? AND purpose = ?", *oldSecretID, "tls_certificate_bundle").Error; err != nil {
				return err
			}
		}
		return audit(tx, p.User.ID, action, "tls_settings", "1", "")
	})
	if err != nil {
		return TLSSettingsView{}, databaseError(err)
	}
	if s.config.ApplyTLSCertificate != nil {
		s.config.ApplyTLSCertificate(pair)
	}
	configured := "configured"
	settings := storage.InstanceSettings{TLSSecretID: &configured, TLSMetadata: metadataJSON, TLSUpdatedAt: &updated}
	return s.tlsSettingsView(settings)
}
