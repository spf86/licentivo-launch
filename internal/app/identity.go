package app

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/storage"
)

const IdentityAudience = "https://www.licentivo.com/integrations/launch"

type IdentityView struct {
	IdentityToken      string    `json:"identity_token"`
	TokenType          string    `json:"token_type"`
	ProductID          string    `json:"product_id"`
	LicentivoProductID string    `json:"licentivo_product_id"`
	LaunchConnectionID string    `json:"launch_connection_id"`
	Environment        string    `json:"environment"`
	Audience           string    `json:"audience"`
	ExpiresAt          time.Time `json:"expires_at"`
}

func (s *Service) identityPrivateKey() ed25519.PrivateKey {
	mac := hmac.New(sha256.New, s.config.MasterKey)
	_, _ = mac.Write([]byte("launch-identity-signing-v1"))
	return ed25519.NewKeyFromSeed(mac.Sum(nil))
}
func (s *Service) identityKeyID() string {
	pub := s.identityPrivateKey().Public().(ed25519.PublicKey)
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:16])
}
func (s *Service) IdentityKeys() map[string]any {
	pub := s.identityPrivateKey().Public().(ed25519.PublicKey)
	return map[string]any{"keys": []any{map[string]string{"kty": "OKP", "crv": "Ed25519", "use": "sig", "alg": "Ed25519", "kid": s.identityKeyID(), "x": base64.RawURLEncoding.EncodeToString(pub)}}}
}
func (s *Service) subject(user string) string {
	mac := hmac.New(sha256.New, s.config.MasterKey)
	_, _ = mac.Write([]byte("launch-customer-subject-v1\n" + user))
	return hex.EncodeToString(mac.Sum(nil))
}
func (s *Service) Identity(ctx context.Context, p Principal, product, environment string) (IdentityView, error) {
	if err := s.rejectExternalRuntime(ctx, product, environment); err != nil {
		return IdentityView{}, err
	}
	if p.Session.Kind != "desktop" {
		return IdentityView{}, ErrForbidden
	}
	if !integrationUUIDPattern.MatchString(product) || (environment != "live" && environment != "sandbox") {
		return IdentityView{}, ErrInvalid
	}
	// Recheck stored session state; a caller cannot reuse a stale Principal.
	var session storage.Session
	if err := s.db.WithContext(ctx).Where("id = ? AND user_id = ? AND revoked_at IS NULL AND expires_at > ?", p.Session.ID, p.User.ID, now()).First(&session).Error; err != nil {
		return IdentityView{}, ErrUnauthorized
	}
	var user storage.User
	if err := s.db.WithContext(ctx).First(&user, "id = ? AND status = 'active'", p.User.ID).Error; err != nil {
		return IdentityView{}, ErrUnauthorized
	}
	if user.PasswordChangeRequired {
		return IdentityView{}, ErrForbidden
	}
	if err := s.identityFence(s.db.WithContext(ctx), user.ID, session.CreatedAt); err != nil {
		return IdentityView{}, err
	}
	var trust storage.LaunchTrustConnection
	if s.db.WithContext(ctx).First(&trust, "environment = ?", environment).Error != nil || trust.Issuer != s.PublicURL() {
		return IdentityView{}, bridgePending()
	}
	var configured storage.Product
	if s.db.WithContext(ctx).First(&configured, "id = ?", product).Error != nil {
		return IdentityView{}, bridgePending()
	}
	remoteProduct, err := productRemoteID(s.db.WithContext(ctx), configured, "")
	if err != nil {
		return IdentityView{}, databaseError(err)
	}
	if remoteProduct == "" {
		return IdentityView{}, bridgePending()
	}
	ref := s.subject("purchase-context\n" + user.ID + "\n" + product + "\n" + environment)
	var historical storage.Entitlement
	if err := s.db.WithContext(ctx).First(&historical, "user_id = ? AND product_id = ? AND environment = ?", user.ID, product, environment).Error; err == nil {
		ref = purchaseReference(historical)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return IdentityView{}, ErrStorage
	}
	return s.signIdentity(user, session, product, remoteProduct, environment, trust, ref)
}
func (s *Service) signIdentity(user storage.User, session storage.Session, product, remoteProduct, environment string, trust storage.LaunchTrustConnection, ref string) (IdentityView, error) {
	at := now()
	if session.CreatedAt.IsZero() || session.CreatedAt.After(at) || session.ExpiresAt.Sub(session.CreatedAt) > 30*24*time.Hour {
		return IdentityView{}, ErrUnauthorized
	}
	until := at.Add(2 * time.Minute)
	if session.ExpiresAt.Before(until) {
		until = session.ExpiresAt
	}
	until = time.Unix(until.Unix(), 0).UTC()
	if !until.After(at) {
		return IdentityView{}, ErrUnauthorized
	}
	jti, err := newID()
	if err != nil {
		return IdentityView{}, err
	}
	header := marshal(map[string]string{"alg": "Ed25519", "typ": "licentivo-launch-identity+jwt", "kid": s.identityKeyID()})
	claims := marshal(map[string]any{"iss": s.PublicURL(), "sub": s.subject(user.ID), "aud": trust.Audience, "sid": session.ID, "jti": jti, "iat": at.Unix(), "nbf": at.Unix(), "exp": until.Unix(), "auth_time": session.CreatedAt.Unix(), "session_exp": session.ExpiresAt.Unix(), "environment": environment, "launch_product_id": product, "product_id": remoteProduct, "entitlement_ref": ref})
	enc := base64.RawURLEncoding.EncodeToString
	message := enc(header) + "." + enc(claims)
	signature := ed25519.Sign(s.identityPrivateKey(), []byte(message))
	return IdentityView{IdentityToken: message + "." + enc(signature), TokenType: "urn:licentivo:launch:identity", Audience: trust.Audience, ExpiresAt: until, ProductID: product, LicentivoProductID: remoteProduct, LaunchConnectionID: trust.RemoteID, Environment: environment}, nil
}

// This only changes account identity. Licentivo owns authorization revocation.
func (s *Service) revokeSessions(tx *gorm.DB, userID, sessionID string, at time.Time, reasons ...string) error {
	reason := ""
	if len(reasons) > 0 {
		reason = reasons[0]
	}
	var rows []storage.Session
	q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND revoked_at IS NULL", userID)
	if sessionID != "" {
		q = q.Where("id = ?", sessionID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if err := tx.Model(&row).Update("revoked_at", at).Error; err != nil {
			return err
		}
		if sessionID != "" && row.Kind == "desktop" {
			if err := s.queueBusinessFact(tx, userID, "identity.session.revoked", "session.revoked:"+row.ID, at, map[string]any{"session_id": row.ID, "kind": row.Kind, "reason": reason}); err != nil {
				return err
			}
		}
	}
	if sessionID == "" {
		id, err := newID()
		if err != nil {
			return err
		}
		return s.queueBusinessFact(tx, userID, "identity.account.revoked", "account.revoked:"+id, at, map[string]string{"reason": reason})
	}
	return nil
}

// Account revocations use Licentivo's receipt-time authentication cutoff.
// Do not issue a new desktop login while a cutoff is outstanding, or fabricate
// a later auth_time for an existing login merely by refreshing its assertion.
func (s *Service) identityFence(tx *gorm.DB, userID string, authenticatedAt time.Time) error {
	q := func() *gorm.DB {
		return tx.Model(&storage.LaunchOutbox{}).Where("subject = ? AND (kind = 'identity.account.revoked' OR (kind = 'identity.account.status' AND JSON_UNQUOTE(JSON_EXTRACT(body,'$.data.status')) = 'disabled'))", s.subject(userID))
	}
	var count int64
	if err := q().Where("status NOT IN ('succeeded','not_required','skipped')").Count(&count).Error; err != nil {
		return ErrStorage
	}
	if count > 0 {
		return problem(503, "ACCOUNT_SECURITY_SYNC_PENDING", "账号安全变更正在同步，请稍后重新登录")
	}
	var latest storage.LaunchOutbox
	err := q().Where("revoked_before IS NOT NULL").Order("revoked_before DESC").First(&latest).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return ErrStorage
	}
	if authenticatedAt.Unix() <= latest.RevokedBefore.Unix() {
		return problem(401, "LAUNCH_LOGIN_REQUIRED", "旧登录已失效，请在安全变更同步后重新登录")
	}
	return nil
}
