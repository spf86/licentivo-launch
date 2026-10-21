package app

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"licentivo-launch/internal/mailer"
	"licentivo-launch/internal/storage"
)

const mailSettingsID uint8 = 1

type MailSettingsInput struct {
	Provider   string `json:"provider"`
	From       string `json:"from"`
	APIKey     string `json:"api_key,omitempty"`
	Host       string `json:"host,omitempty"`
	Port       string `json:"port,omitempty"`
	Username   string `json:"username,omitempty"`
	Encryption string `json:"encryption,omitempty"`
}

type MailSettingsView struct {
	Configured       bool   `json:"configured"`
	Provider         string `json:"provider"`
	From             string `json:"from"`
	CredentialStored bool   `json:"credential_stored"`
	Host             string `json:"host"`
	Port             string `json:"port"`
	Username         string `json:"username"`
	Encryption       string `json:"encryption"`
}

func mailSettingsView(configured bool, from string, credentialStored bool, host, port, username, encryption string) MailSettingsView {
	if strings.TrimSpace(host) == "" {
		host = "smtp.resend.com"
	}
	if strings.TrimSpace(username) == "" {
		username = "resend"
	}
	port, encryption, _ = normalizeResendTransport(port, encryption)
	return MailSettingsView{
		Configured: configured, Provider: "resend", From: from, CredentialStored: credentialStored,
		Host: host, Port: port, Username: username, Encryption: encryption,
	}
}

func resendSMTPConfig(from, apiKey, host, port, username, encryption string) mailer.Config {
	return mailer.Config{
		Host:        host,
		Port:        port,
		Username:    username,
		Password:    apiKey,
		From:        from,
		ImplicitTLS: encryption == "implicit_tls",
	}
}

func normalizeSMTPHost(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		value = "smtp.resend.com"
	}
	if len(value) > 253 || strings.ContainsAny(value, "\r\n\t /:@") {
		return "", problem(400, "SMTP_HOST_INVALID", "请填写有效的 SMTP 服务器主机名")
	}
	if net.ParseIP(value) != nil {
		return value, nil
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", problem(400, "SMTP_HOST_INVALID", "请填写有效的 SMTP 服务器主机名")
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return "", problem(400, "SMTP_HOST_INVALID", "请填写有效的 SMTP 服务器主机名")
			}
		}
	}
	return value, nil
}

func normalizeSMTPUsername(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "resend"
	}
	if len(value) > 254 || strings.ContainsAny(value, "\r\n") {
		return "", problem(400, "SMTP_USERNAME_INVALID", "请填写有效的 SMTP 账号")
	}
	return value, nil
}

func normalizeResendTransport(port, encryption string) (string, string, error) {
	port = strings.TrimSpace(port)
	encryption = strings.TrimSpace(encryption)
	if port == "" {
		if encryption == "starttls" {
			port = "587"
		} else {
			port = "465"
		}
	}
	if encryption == "" {
		if port == "465" || port == "2465" {
			encryption = "implicit_tls"
		} else {
			encryption = "starttls"
		}
	}
	valid := (encryption == "implicit_tls" && (port == "465" || port == "2465")) ||
		(encryption == "starttls" && (port == "25" || port == "587" || port == "2587"))
	if !valid {
		return "", "", problem(400, "RESEND_TRANSPORT_INVALID", "Resend 隐式 TLS 可使用端口 465 或 2465；STARTTLS 可使用端口 25、587 或 2587")
	}
	return port, encryption, nil
}

func validResendKey(value string) bool {
	if len(value) < 8 || len(value) > 256 || !strings.HasPrefix(value, "re_") {
		return false
	}
	for _, b := range []byte(value) {
		if b < 0x21 || b > 0x7e {
			return false
		}
	}
	return true
}

func (s *Service) MailSettings(ctx context.Context, p Principal) (MailSettingsView, error) {
	if !p.IsAdmin() {
		return MailSettingsView{}, ErrForbidden
	}
	var saved storage.MailSettings
	err := s.db.WithContext(ctx).First(&saved, "id = ?", mailSettingsID).Error
	if err == nil {
		return mailSettingsView(saved.Enabled, saved.From, saved.CredentialSecretID != "", saved.Host, saved.Port, saved.Username, saved.Encryption), nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return MailSettingsView{}, ErrStorage
	}
	return mailSettingsView(false, "", false, "smtp.resend.com", "465", "resend", "implicit_tls"), nil
}

func (s *Service) SaveMailSettings(ctx context.Context, p Principal, in MailSettingsInput) (MailSettingsView, error) {
	if !p.IsAdmin() {
		return MailSettingsView{}, ErrForbidden
	}
	if in.Provider == "" {
		in.Provider = "resend"
	}
	if in.Provider != "resend" {
		return MailSettingsView{}, ErrInvalid
	}
	host, err := normalizeSMTPHost(in.Host)
	if err != nil {
		return MailSettingsView{}, err
	}
	username, err := normalizeSMTPUsername(in.Username)
	if err != nil {
		return MailSettingsView{}, err
	}
	port, encryption, err := normalizeResendTransport(in.Port, in.Encryption)
	if err != nil {
		return MailSettingsView{}, err
	}
	from, err := email(in.From)
	if err != nil {
		return MailSettingsView{}, problem(400, "MAIL_FROM_INVALID", "请填写 Resend 已验证域名下的完整发件邮箱")
	}
	key := strings.TrimSpace(in.APIKey)
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current storage.MailSettings
		findErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", mailSettingsID).Error
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		secretID := current.CredentialSecretID
		if key != "" {
			if !validResendKey(key) {
				return problem(400, "RESEND_KEY_INVALID", "请粘贴以 re_ 开头的 Resend API Key")
			}
			if _, configErr := mailer.NewSMTP(resendSMTPConfig(from, key, host, port, username, encryption)); configErr != nil {
				return problem(400, "MAIL_CONFIG_INVALID", "邮件配置格式无效")
			}
			secretID, err = s.createSecret(tx, "resend_api_key", []byte(key))
			if err != nil {
				return err
			}
		}
		if secretID == "" {
			return problem(400, "RESEND_KEY_REQUIRED", "首次配置时请填写 Resend API Key")
		}
		settings := storage.MailSettings{ID: mailSettingsID, Provider: "resend", From: from, Host: host, Port: port, Username: username, Encryption: encryption, CredentialSecretID: secretID, Enabled: true, UpdatedAt: now()}
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			if err := tx.Create(&settings).Error; err != nil {
				return err
			}
		} else if err := tx.Model(&current).Updates(map[string]any{"provider": settings.Provider, "from_address": settings.From, "host": settings.Host, "port": settings.Port, "username": settings.Username, "encryption": settings.Encryption, "credential_secret_id": settings.CredentialSecretID, "enabled": true, "updated_at": settings.UpdatedAt}).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "mail.configure", "mail_settings", "resend", "")
	})
	if err != nil {
		return MailSettingsView{}, databaseError(err)
	}
	return mailSettingsView(true, from, true, host, port, username, encryption), nil
}

func (s *Service) newMailSender(config mailer.Config) (mailer.Sender, error) {
	if s.config.MailFactory != nil {
		return s.config.MailFactory(config)
	}
	return mailer.NewSMTP(config)
}

func (s *Service) configuredMailSender(ctx context.Context) (mailer.Sender, error) {
	var saved storage.MailSettings
	err := s.db.WithContext(ctx).First(&saved, "id = ? AND enabled = true", mailSettingsID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil || saved.Provider != "resend" {
		return nil, ErrStorage
	}
	key, err := s.readSecret(ctx, saved.CredentialSecretID)
	if err != nil {
		return nil, err
	}
	port, encryption, transportErr := normalizeResendTransport(saved.Port, saved.Encryption)
	if transportErr != nil {
		return nil, ErrStorage
	}
	host, hostErr := normalizeSMTPHost(saved.Host)
	username, usernameErr := normalizeSMTPUsername(saved.Username)
	if hostErr != nil || usernameErr != nil {
		return nil, ErrStorage
	}
	sender, err := s.newMailSender(resendSMTPConfig(saved.From, string(key), host, port, username, encryption))
	if err != nil {
		return nil, ErrStorage
	}
	return sender, nil
}

func (s *Service) mailConfigured(ctx context.Context) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&storage.MailSettings{}).Where("id = ? AND enabled = true", mailSettingsID).Count(&count).Error
	if err != nil {
		return false, ErrStorage
	}
	return count > 0, nil
}

type MailCheckResult struct {
	SentTo string `json:"sent_to"`
}

type MailCheckInput struct {
	To string `json:"to,omitempty"`
}

func (s *Service) CheckMail(ctx context.Context, p Principal, in MailCheckInput) (MailCheckResult, error) {
	if !p.IsAdmin() {
		return MailCheckResult{}, ErrForbidden
	}
	sender, err := s.configuredMailSender(ctx)
	if err != nil {
		return MailCheckResult{}, ErrStorage
	}
	if sender == nil {
		return MailCheckResult{}, problem(409, "RESEND_NOT_CONFIGURED", "请先保存 Resend API Key 和发件邮箱")
	}
	to := strings.TrimSpace(in.To)
	if to == "" {
		to = p.User.Email
	}
	to, err = email(to)
	if err != nil {
		return MailCheckResult{}, problem(400, "MAIL_TEST_RECIPIENT_INVALID", "请填写有效的测试收件邮箱")
	}
	id, err := newID()
	if err != nil {
		return MailCheckResult{}, err
	}
	deliveryCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	err = sender.Send(deliveryCtx, mailer.Message{
		To: to, Subject: "Licentivo Launch 邮件发送测试",
		Body:           "这是一封 Resend 配置测试邮件。\n\n收到本邮件表示 Launch 已成功连接 Resend，并已把邮件交给 Resend 处理。",
		IdempotencyKey: "launch-mail-check/" + id,
	})
	cancel()
	if err != nil {
		message := "Resend rejected the test delivery"
		_ = s.db.WithContext(context.WithoutCancel(ctx)).Model(&storage.MailSettings{}).Where("id = ?", mailSettingsID).Updates(map[string]any{"last_check_error": message, "last_checked_at": now()}).Error
		return MailCheckResult{}, problem(502, "RESEND_DELIVERY_FAILED", "Resend 连接、认证或发件地址检查失败，请核对 API Key、已验证域名和发件邮箱")
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&storage.MailSettings{}).Where("id = ?", mailSettingsID).Updates(map[string]any{"last_checked_at": now(), "last_check_error": nil}).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "mail.check", "mail_settings", "resend", "test delivery accepted")
	}); err != nil {
		return MailCheckResult{}, ErrStorage
	}
	return MailCheckResult{SentTo: to}, nil
}
