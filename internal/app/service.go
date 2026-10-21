package app

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"licentivo-launch/internal/mailer"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/security"
	"licentivo-launch/internal/storage"
)

type Error struct {
	Status        int
	Code, Message string
}

func (e *Error) Error() string                       { return e.Code }
func problem(status int, code, message string) error { return &Error{status, code, message} }

var (
	ErrInvalid      = problem(400, "INVALID_INPUT", "请检查填写的信息")
	ErrUnauthorized = problem(401, "AUTH_REQUIRED", "请重新登录")
	ErrForbidden    = problem(403, "FORBIDDEN", "你没有执行此操作的权限")
	ErrNotFound     = problem(404, "NOT_FOUND", "未找到该记录")
	ErrStorage      = problem(503, "STORAGE_UNAVAILABLE", "服务暂不可用，请稍后重试")
)

type Config struct {
	PublicURL, SetupToken string
	MasterKey             []byte
	UploadDir             string
	MailFactory           func(mailer.Config) (mailer.Sender, error)
	PaymentFactory        func(context.Context, storage.PaymentAccount) (ports.PaymentProvider, error)
	LicensingFactory      func(context.Context, storage.LicentivoConnection) (ports.LicensingProvider, error)
	LicensingSetupFactory func(apiKey string) (LicentivoSetupProvider, error)
	TranslationFactory    func(edition, apiKey string) (ports.Translator, error)
	TLSListenerEnabled    bool
	ApplyTLSCertificate   func(*tls.Certificate)
}
type Service struct {
	db        *gorm.DB
	vault     *security.Vault
	config    Config
	dummyHash string
	uploadDir string
	publicURL atomic.Value
}

func New(ctx context.Context, db *gorm.DB, config Config) (*Service, error) {
	vault, err := security.NewVault(config.MasterKey)
	if err != nil {
		return nil, err
	}
	dummy, err := security.HashPassword("constant-work-for-unknown-users")
	if err != nil {
		return nil, err
	}
	uploadDir := strings.TrimSpace(config.UploadDir)
	if uploadDir == "" {
		uploadDir, err = filepath.Abs(filepath.Join(".local", "uploads"))
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(uploadDir, 0o700); err != nil {
		return nil, err
	}
	s := &Service{db: db, vault: vault, config: config, dummyHash: dummy, uploadDir: uploadDir}
	ref, err := security.RandomReference()
	if err != nil {
		return nil, err
	}
	settings := storage.InstanceSettings{ID: 1, InstanceRef: ref, Brand: json.RawMessage(`{"name":"Licentivo Launch","tagline":"从产品到第一次使用","description":"商家独立部署的软件服务","theme":"forest"}`)}
	if err := db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&settings).Error; err != nil {
		return nil, ErrStorage
	}
	if err := db.WithContext(ctx).First(&settings, "id = ?", 1).Error; err != nil {
		return nil, ErrStorage
	}
	publicURL := strings.TrimRight(config.PublicURL, "/")
	if settings.PublicOrigin != nil && strings.TrimSpace(*settings.PublicOrigin) != "" {
		publicURL = strings.TrimRight(strings.TrimSpace(*settings.PublicOrigin), "/")
	}
	s.publicURL.Store(publicURL)
	if err := db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&storage.RuntimeStatus{ID: 1, UpdatedAt: now()}).Error; err != nil {
		return nil, ErrStorage
	}
	// Administrators always have the complete backend scope. Keep the stored
	// marker normalized as well, so existing installations and future
	// permission-aware handlers cannot interpret an old empty array as a
	// restricted administrator.
	if err := db.WithContext(ctx).Model(&storage.StaffAccount{}).Where("role = ?", "admin").Update("permissions", permissionsForRole("admin")).Error; err != nil {
		return nil, ErrStorage
	}
	// Fixed customer-facing interface text is bundled with the application.
	// Remove the legacy DeepL work item so only administrator-authored content
	// consumes translation quota.
	if err := db.WithContext(ctx).Where("resource_type = ?", "site_ui").Delete(&storage.ContentTranslation{}).Error; err != nil {
		return nil, ErrStorage
	}
	if err := db.WithContext(ctx).Where("resource_type = ?", "site_ui").Delete(&storage.TranslationTask{}).Error; err != nil {
		return nil, ErrStorage
	}
	if config.ApplyTLSCertificate != nil {
		certificate, err := s.managedTLSCertificate(ctx)
		if err != nil {
			return nil, err
		}
		if certificate != nil {
			config.ApplyTLSCertificate(certificate)
		}
	}
	return s, nil
}

func (s *Service) PublicURL() string {
	if value := s.publicURL.Load(); value != nil {
		return value.(string)
	}
	return strings.TrimRight(s.config.PublicURL, "/")
}

func marshal(value any) json.RawMessage { b, _ := json.Marshal(value); return b }
func now() time.Time                    { return time.Now().UTC().Truncate(time.Microsecond) }
func newID() (string, error) {
	id, err := security.ID()
	if err != nil {
		return "", ErrStorage
	}
	return id, nil
}
func email(value string) (string, error) {
	value = strings.TrimSpace(value)
	addr, err := mail.ParseAddress(value)
	if err != nil || addr.Address != value || len(value) > 254 || !strings.Contains(value, "@") || strings.ContainsAny(value, "\r\n") {
		return "", ErrInvalid
	}
	return strings.ToLower(value), nil
}
func installed(tx *gorm.DB) (storage.InstanceSettings, error) {
	var v storage.InstanceSettings
	if tx.First(&v, "id = ?", 1).Error != nil {
		return v, ErrStorage
	}
	if v.InstallationCompletedAt == nil {
		return v, problem(409, "SETUP_REQUIRED", "请先完成商家安装初始化")
	}
	return v, nil
}
func databaseError(err error) error {
	if err == nil {
		return nil
	}
	var business *Error
	if errors.As(err, &business) {
		return err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	var sqlError *mysql.MySQLError
	if errors.As(err, &sqlError) && sqlError.Number == 1062 {
		return problem(409, "CONFLICT", "记录已存在或状态已改变，请刷新后重试")
	}
	return ErrStorage
}

func audit(tx *gorm.DB, actor, action, objectType, objectID, reason string) error {
	id, err := newID()
	if err != nil {
		return err
	}
	request, err := newID()
	if err != nil {
		return err
	}
	var who *string
	if actor != "" {
		who = &actor
	}
	return tx.Create(&storage.AuditEntry{ID: id, ActorID: who, Action: action, ObjectType: objectType, ObjectID: objectID, RequestID: request, Reason: &reason, SafeMetadata: json.RawMessage(`{}`), CreatedAt: now()}).Error
}

type UserView struct {
	ID                     string `json:"id"`
	Email                  string `json:"email"`
	Verified               bool   `json:"verified"`
	Role                   string `json:"role"`
	FullAccess             bool   `json:"full_access"`
	PasswordChangeRequired bool   `json:"password_change_required"`
}
type Principal struct {
	User    storage.User
	Session storage.Session
	Role    string
}

func (p Principal) View() UserView {
	return viewUser(p.User, p.Role)
}
func (p Principal) CanManage() bool {
	return !p.User.PasswordChangeRequired && (p.Role == "admin" || p.Role == "operations")
}
func (p Principal) IsAdmin() bool { return !p.User.PasswordChangeRequired && p.Role == "admin" }

func viewUser(user storage.User, role string) UserView {
	return UserView{ID: user.ID, Email: user.Email, Verified: user.EmailVerifiedAt != nil, Role: role, FullAccess: role == "admin" && !user.PasswordChangeRequired, PasswordChangeRequired: user.PasswordChangeRequired}
}

func permissionsForRole(role string) json.RawMessage {
	if role == "admin" {
		return json.RawMessage(`["*"]`)
	}
	return json.RawMessage(`[]`)
}

func (s *Service) role(tx *gorm.DB, user storage.User) (string, error) {
	var staff storage.StaffAccount
	err := tx.First(&staff, "user_id = ?", user.ID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "customer", nil
	}
	if err != nil {
		return "", ErrStorage
	}
	return staff.Role, nil
}
