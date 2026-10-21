package app

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"licentivo-launch/internal/security"
	"licentivo-launch/internal/storage"
)

type BootstrapInput struct {
	Token     string `json:"setup_token"`
	Email     string `json:"email"`
	Password  string `json:"password"`
	BrandName string `json:"brand_name"`
}

func (s *Service) Bootstrap(ctx context.Context, in BootstrapInput) error {
	if s.config.SetupToken == "" {
		return problem(503, "SETUP_TOKEN_REQUIRED", "请在服务器设置 LAUNCH_SETUP_TOKEN 后重启")
	}
	if subtle.ConstantTimeCompare(security.Digest(in.Token), security.Digest(s.config.SetupToken)) != 1 {
		return ErrForbidden
	}
	address, err := email(in.Email)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(in.BrandName)) < 2 || len(in.BrandName) > 160 {
		return ErrInvalid
	}
	password, err := security.HashPassword(in.Password)
	if err != nil {
		return problem(400, "PASSWORD_INVALID", "密码长度应为 12–128 字节")
	}
	id, err := newID()
	if err != nil {
		return err
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var settings storage.InstanceSettings
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&settings, "id = ?", 1).Error; err != nil {
			return err
		}
		if settings.InstallationCompletedAt != nil {
			return problem(409, "ALREADY_INSTALLED", "安装已完成，初始化入口已关闭")
		}
		at := now()
		user := storage.User{ID: id, Email: address, EmailKey: address, PasswordHash: password, Status: "active", EmailVerifiedAt: &at}
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		if err := tx.Create(&storage.StaffAccount{UserID: id, Role: "admin", Permissions: permissionsForRole("admin")}).Error; err != nil {
			return err
		}
		brand := Brand{Name: strings.TrimSpace(in.BrandName), Tagline: "从产品到第一次使用", SupportEmail: address, Theme: "forest"}
		if err := tx.Model(&settings).Updates(map[string]any{"brand": marshal(brand), "installation_completed_at": at}).Error; err != nil {
			return err
		}
		return audit(tx, id, "setup.complete", "instance", settings.InstanceRef, "")
	}))
}

type SessionGrant struct {
	User        UserView  `json:"user"`
	AccessToken string    `json:"access_token,omitempty"`
	CSRFToken   string    `json:"csrf_token,omitempty"`
	Kind        string    `json:"kind"`
	ExpiresAt   time.Time `json:"expires_at"`
}

func (s *Service) Login(ctx context.Context, address, password, kind string) (SessionGrant, error) {
	if _, err := installed(s.db.WithContext(ctx)); err != nil {
		return SessionGrant{}, err
	}
	if kind != "website" && kind != "desktop" {
		return SessionGrant{}, ErrInvalid
	}
	normalized, parseErr := email(address)
	var user storage.User
	err := s.db.WithContext(ctx).First(&user, "email_key = ?", normalized).Error
	hash := s.dummyHash
	if parseErr == nil && err == nil {
		hash = user.PasswordHash
	}
	valid := security.VerifyPassword(hash, password)
	if !valid || parseErr != nil || err != nil || user.Status != "active" {
		return SessionGrant{}, problem(401, "LOGIN_FAILED", "邮箱或密码不正确，或账号暂不可用")
	}
	token, err := security.Token()
	if err != nil {
		return SessionGrant{}, ErrStorage
	}
	csrf, err := security.Token()
	if err != nil {
		return SessionGrant{}, ErrStorage
	}
	id, err := newID()
	if err != nil {
		return SessionGrant{}, err
	}
	expires := now().Add(14 * 24 * time.Hour)
	if kind == "desktop" {
		expires = now().Add(30 * 24 * time.Hour)
	}
	var role string
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current storage.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", user.ID).Error; err != nil {
			return err
		}
		if current.PasswordHash != user.PasswordHash || current.Status != "active" {
			return ErrUnauthorized
		}
		var err error
		role, err = s.role(tx, current)
		if err != nil {
			return err
		}
		user = current
		var csrfHash []byte
		if kind == "website" {
			csrfHash = security.Digest(csrf)
		}
		return tx.Create(&storage.Session{ID: id, UserID: user.ID, TokenHash: security.Digest(token), CSRFHash: csrfHash, Kind: kind, ExpiresAt: expires}).Error
	})
	if err != nil {
		return SessionGrant{}, databaseError(err)
	}
	grant := SessionGrant{User: viewUser(user, role), AccessToken: token, Kind: kind, ExpiresAt: expires}
	if kind == "website" {
		grant.CSRFToken = csrf
	}
	return grant, nil
}

func (s *Service) Authenticate(ctx context.Context, token, kind string) (Principal, error) {
	if len(token) != 43 {
		return Principal{}, ErrUnauthorized
	}
	var session storage.Session
	if err := s.db.WithContext(ctx).Where("token_hash = ? AND revoked_at IS NULL AND expires_at > ? AND kind = ?", security.Digest(token), now(), kind).First(&session).Error; err != nil {
		return Principal{}, ErrUnauthorized
	}
	var user storage.User
	if err := s.db.WithContext(ctx).First(&user, "id = ? AND status = 'active'", session.UserID).Error; err != nil {
		return Principal{}, ErrUnauthorized
	}
	role, err := s.role(s.db.WithContext(ctx), user)
	if err != nil {
		return Principal{}, err
	}
	return Principal{User: user, Session: session, Role: role}, nil
}

func (s *Service) Register(ctx context.Context, address, password string) error {
	if _, err := installed(s.db.WithContext(ctx)); err != nil {
		return err
	}
	normalized, err := email(address)
	if err != nil {
		return err
	}
	hash, err := security.HashPassword(password)
	if err != nil {
		return problem(400, "PASSWORD_INVALID", "密码长度应为 12–128 字节")
	}
	id, err := newID()
	if err != nil {
		return err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user := storage.User{ID: id, Email: normalized, EmailKey: normalized, PasswordHash: hash, Status: "active"}
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		return s.queueCredential(tx, user, "verify", normalized)
	})
	var mysqlConflict *Error
	converted := databaseError(err)
	if errors.As(converted, &mysqlConflict) && mysqlConflict.Code == "CONFLICT" {
		return nil
	}
	return converted
}

func (s *Service) RequestEmail(ctx context.Context, address, kind string) error {
	if kind != "verify" && kind != "reset" {
		return ErrInvalid
	}
	if _, err := installed(s.db.WithContext(ctx)); err != nil {
		return err
	}
	normalized, err := email(address)
	if err != nil {
		return nil
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user storage.User
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "email_key = ? AND status = 'active'", normalized).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if kind == "verify" && user.EmailVerifiedAt != nil {
			return nil
		}
		return s.queueCredential(tx, user, kind, normalized)
	}))
}

func (s *Service) queueCredential(tx *gorm.DB, user storage.User, kind, target string) error {
	token, err := security.Token()
	if err != nil {
		return err
	}
	id, err := newID()
	if err != nil {
		return err
	}
	at := now()
	duration := 24 * time.Hour
	if kind == "reset" {
		duration = 30 * time.Minute
	}
	// Supersede previous unconsumed links of the same purpose.
	if err := tx.Model(&storage.EmailCredential{}).Where("user_id = ? AND kind = ? AND used_at IS NULL", user.ID, kind).Update("used_at", at).Error; err != nil {
		return err
	}
	credential := storage.EmailCredential{ID: id, UserID: user.ID, Kind: kind, TargetEmail: target, TokenHash: security.Digest(token), ExpiresAt: at.Add(duration), CreatedAt: at}
	if err := tx.Create(&credential).Error; err != nil {
		return err
	}
	subject, action := "验证邮箱", "verify"
	if kind == "reset" {
		subject, action = "重置密码", "reset"
	}
	if kind == "change_email" {
		subject = "确认新的邮箱"
	}
	link := s.PublicURL() + "/account#action=" + action + "&token=" + url.QueryEscape(token)
	return s.queueMail(tx, MailMessage{To: target, Subject: subject, Body: "请打开以下链接完成操作：\r\n" + link + "\r\n\r\n如果不是你发起的操作，请忽略此邮件。"})
}

func (s *Service) CompleteEmail(ctx context.Context, token, newPassword string) error {
	if len(token) != 43 {
		return problem(400, "LINK_INVALID", "链接无效或已过期，请重新申请")
	}
	var credential storage.EmailCredential
	if err := s.db.WithContext(ctx).First(&credential, "token_hash = ?", security.Digest(token)).Error; err != nil {
		return problem(400, "LINK_INVALID", "链接无效或已过期，请重新申请")
	}
	var passwordHash string
	if credential.Kind == "reset" {
		var err error
		passwordHash, err = security.HashPassword(newPassword)
		if err != nil {
			return problem(400, "PASSWORD_INVALID", "密码长度应为 12–128 字节")
		}
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Consistent user-then-credential locking order also covers link requests.
		var user storage.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", credential.UserID).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&credential, "id = ?", credential.ID).Error; err != nil {
			return err
		}
		at := now()
		if credential.UsedAt != nil || !credential.ExpiresAt.After(at) || user.Status != "active" {
			return problem(400, "LINK_INVALID", "链接无效或已过期，请重新申请")
		}
		changes := map[string]any{}
		switch credential.Kind {
		case "verify":
			if user.EmailKey != credential.TargetEmail {
				return ErrInvalid
			}
			changes["email_verified_at"] = at
		case "reset":
			changes["password_hash"] = passwordHash
		case "change_email":
			changes["email"], changes["email_key"], changes["email_verified_at"] = credential.TargetEmail, credential.TargetEmail, at
		default:
			return ErrInvalid
		}
		if err := tx.Model(&user).Updates(changes).Error; err != nil {
			return err
		}
		if credential.Kind != "verify" {
			if err := tx.Model(&storage.Session{}).Where("user_id = ? AND revoked_at IS NULL", user.ID).Update("revoked_at", at).Error; err != nil {
				return err
			}
			if err := tx.Model(&storage.EmailCredential{}).Where("user_id = ? AND used_at IS NULL", user.ID).Update("used_at", at).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&credential).Update("used_at", at).Error; err != nil {
			return err
		}
		return audit(tx, user.ID, "account."+credential.Kind, "user", user.ID, "")
	}))
}

func (s *Service) ChangeEmail(ctx context.Context, p Principal, address, password string) error {
	if !security.VerifyPassword(p.User.PasswordHash, password) {
		return problem(401, "PASSWORD_INCORRECT", "当前密码不正确")
	}
	normalized, err := email(address)
	if err != nil || normalized == p.User.EmailKey {
		return ErrInvalid
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user storage.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", p.User.ID).Error; err != nil {
			return err
		}
		if user.PasswordHash != p.User.PasswordHash || user.Status != "active" {
			return ErrUnauthorized
		}
		return s.queueCredential(tx, user, "change_email", normalized)
	}))
}

func (s *Service) ChangePassword(ctx context.Context, p Principal, current, replacement string) error {
	if !security.VerifyPassword(p.User.PasswordHash, current) {
		return problem(401, "PASSWORD_INCORRECT", "当前密码不正确")
	}
	hash, err := security.HashPassword(replacement)
	if err != nil {
		return problem(400, "PASSWORD_INVALID", "密码长度应为 12–128 字节")
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user storage.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", p.User.ID).Error; err != nil {
			return err
		}
		if user.PasswordHash != p.User.PasswordHash || user.Status != "active" {
			return ErrUnauthorized
		}
		if err := tx.Model(&user).Update("password_hash", hash).Error; err != nil {
			return err
		}
		at := now()
		if err := tx.Model(&storage.Session{}).Where("user_id = ? AND revoked_at IS NULL", user.ID).Update("revoked_at", at).Error; err != nil {
			return err
		}
		if err := tx.Model(&storage.EmailCredential{}).Where("user_id = ? AND used_at IS NULL", user.ID).Update("used_at", at).Error; err != nil {
			return err
		}
		return audit(tx, user.ID, "account.change_password", "user", user.ID, "")
	}))
}

func (s *Service) Logout(ctx context.Context, p Principal) error {
	return databaseError(s.db.WithContext(ctx).Model(&storage.Session{}).Where("id = ? AND user_id = ?", p.Session.ID, p.User.ID).Update("revoked_at", now()).Error)
}

type SessionView struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Current   bool      `json:"current"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *Service) Sessions(ctx context.Context, p Principal) ([]SessionView, error) {
	var sessions []storage.Session
	if err := s.db.WithContext(ctx).Where("user_id = ? AND revoked_at IS NULL AND expires_at > ?", p.User.ID, now()).Order("created_at DESC").Limit(100).Find(&sessions).Error; err != nil {
		return nil, ErrStorage
	}
	views := make([]SessionView, 0, len(sessions))
	for _, v := range sessions {
		views = append(views, SessionView{v.ID, v.Kind, v.ID == p.Session.ID, v.CreatedAt, v.ExpiresAt})
	}
	return views, nil
}
func (s *Service) RevokeSession(ctx context.Context, p Principal, id string) error {
	result := s.db.WithContext(ctx).Model(&storage.Session{}).Where("id = ? AND user_id = ?", id, p.User.ID).Update("revoked_at", now())
	if result.Error != nil {
		return ErrStorage
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
