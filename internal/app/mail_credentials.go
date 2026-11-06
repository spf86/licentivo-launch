package app

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"gorm.io/gorm"
	"licentivo-launch/internal/mailer"
	"licentivo-launch/internal/security"
	"licentivo-launch/internal/storage"
)

// Older queue payloads have no credential ID. Resolve their existing links
// without extending validity, issuing another credential, or changing the URL.
func legacyMailCredentialToken(message mailer.Message) string {
	switch message.Subject {
	case "验证邮箱", "重置密码", "确认新的邮箱":
	default:
		return ""
	}
	for _, word := range strings.Fields(message.Body) {
		link, err := url.Parse(word)
		if err != nil || (link.Scheme != "http" && link.Scheme != "https") || link.Host == "" || link.Path != "/account" {
			continue
		}
		fragment, err := url.ParseQuery(link.Fragment)
		if err != nil || (fragment.Get("action") != "verify" && fragment.Get("action") != "reset") {
			continue
		}
		if token := fragment.Get("token"); len(token) == 43 {
			return token
		}
	}
	return ""
}

// Run before reserving SMTP quota on every attempt, including retries after a
// quota wait. The same expiry and consumption rules govern CompleteEmail.
func (s *Service) credentialMailCancellation(ctx context.Context, message mailer.Message, credentialID string) (string, error) {
	query := s.db.WithContext(ctx)
	if credentialID != "" {
		query = query.Where("id=?", credentialID)
	} else if token := legacyMailCredentialToken(message); token != "" {
		query = query.Where("token_hash=?", security.Digest(token))
	} else {
		return "", nil
	}
	var credential storage.EmailCredential
	if err := query.First(&credential).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return "MAIL_CREDENTIAL_MISSING", nil
	} else if err != nil {
		return "", ErrStorage
	}
	if credential.UsedAt != nil {
		return "MAIL_CREDENTIAL_USED", nil
	}
	if !credential.ExpiresAt.After(now()) {
		return "MAIL_CREDENTIAL_EXPIRED", nil
	}
	if !strings.EqualFold(credential.TargetEmail, message.To) {
		return "MAIL_CREDENTIAL_RECIPIENT_CHANGED", nil
	}
	var user storage.User
	if err := s.db.WithContext(ctx).Select("id,status,email_key").First(&user, "id=?", credential.UserID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return "MAIL_CREDENTIAL_ACCOUNT_UNAVAILABLE", nil
	} else if err != nil {
		return "", ErrStorage
	}
	if user.Status != "active" || credential.Kind != "change_email" && !strings.EqualFold(user.EmailKey, credential.TargetEmail) {
		return "MAIL_CREDENTIAL_ACCOUNT_UNAVAILABLE", nil
	}
	return "", nil
}
