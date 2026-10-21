package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"licentivo-launch/internal/security"
	"licentivo-launch/internal/storage"
)

type StaffView struct {
	UserView
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateStaffInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

func validStaffRole(role string) bool {
	return role == "admin" || role == "operations" || role == "support"
}

func (s *Service) Staff(ctx context.Context, p Principal) ([]StaffView, error) {
	if !p.IsAdmin() {
		return nil, ErrForbidden
	}
	var accounts []storage.StaffAccount
	if err := s.db.WithContext(ctx).Preload("FkStaffUser").Order("created_at ASC").Find(&accounts).Error; err != nil {
		return nil, ErrStorage
	}
	views := make([]StaffView, 0, len(accounts))
	for _, account := range accounts {
		if account.FkStaffUser == nil {
			return nil, ErrStorage
		}
		user := *account.FkStaffUser
		views = append(views, StaffView{
			UserView:  viewUser(user, account.Role),
			Status:    user.Status,
			CreatedAt: user.CreatedAt,
		})
	}
	return views, nil
}

func (s *Service) CreateStaff(ctx context.Context, p Principal, in CreateStaffInput) error {
	if !p.IsAdmin() {
		return ErrForbidden
	}
	if !validStaffRole(in.Role) {
		return problem(400, "STAFF_ROLE_INVALID", "请选择管理员、运营或客服身份")
	}
	address, err := email(in.Email)
	if err != nil {
		return err
	}
	hash, err := security.HashPassword(in.Password)
	if err != nil {
		return problem(400, "PASSWORD_INVALID", "初始密码长度应为 12–128 字节")
	}
	id, err := newID()
	if err != nil {
		return err
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var settings storage.InstanceSettings
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&settings, "id = 1").Error; err != nil {
			return err
		}
		var existing storage.User
		err := tx.First(&existing, "email_key = ?", address).Error
		if err == nil {
			return problem(409, "EMAIL_IN_USE", "这个邮箱已用于客户或网站人员账号，请更换邮箱")
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		at := now()
		user := storage.User{ID: id, Email: address, EmailKey: address, PasswordHash: hash, EmailVerifiedAt: &at, Status: "active"}
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		if err := tx.Create(&storage.StaffAccount{UserID: id, Role: in.Role, Permissions: permissionsForRole(in.Role)}).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "staff.created", "staff", id, "创建网站人员："+in.Role)
	}))
}

func activeAdminCount(tx *gorm.DB) (int64, error) {
	var count int64
	err := tx.Model(&storage.StaffAccount{}).
		Joins("JOIN users ON users.id = staff_accounts.user_id").
		Where("staff_accounts.role = 'admin' AND users.status = 'active'").
		Count(&count).Error
	return count, err
}

func (s *Service) SetStaffRole(ctx context.Context, p Principal, userID, role, reason string) error {
	if !p.IsAdmin() {
		return ErrForbidden
	}
	if !validStaffRole(role) || len(strings.TrimSpace(reason)) < 2 || len(reason) > 500 {
		return ErrInvalid
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var settings storage.InstanceSettings
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&settings, "id = 1").Error; err != nil {
			return err
		}
		var user storage.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ? AND status = 'active'", userID).Error; err != nil {
			return ErrNotFound
		}
		var staff storage.StaffAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&staff, "user_id = ?", user.ID).Error; err != nil {
			return ErrNotFound
		}
		if staff.Role == role {
			return tx.Model(&staff).Update("permissions", permissionsForRole(role)).Error
		}
		if staff.Role == "admin" && role != "admin" {
			count, err := activeAdminCount(tx)
			if err != nil {
				return err
			}
			if count <= 1 {
				return problem(409, "LAST_ADMIN_PROTECTED", "至少保留一名可登录的管理员")
			}
		}
		if err := tx.Model(&staff).Updates(map[string]any{"role": role, "permissions": permissionsForRole(role)}).Error; err != nil {
			return err
		}
		if err := tx.Model(&storage.Session{}).Where("user_id = ? AND revoked_at IS NULL", user.ID).Update("revoked_at", now()).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "staff.role_changed", "staff", user.ID, reason)
	}))
}

func (s *Service) SetStaffStatus(ctx context.Context, p Principal, userID, status, reason string) error {
	if !p.IsAdmin() {
		return ErrForbidden
	}
	if status != "active" && status != "disabled" {
		return ErrInvalid
	}
	if len(strings.TrimSpace(reason)) < 2 || len(reason) > 500 {
		return problem(400, "REASON_REQUIRED", "请填写操作原因")
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var settings storage.InstanceSettings
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&settings, "id = 1").Error; err != nil {
			return err
		}
		var user storage.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", userID).Error; err != nil {
			return err
		}
		var staff storage.StaffAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&staff, "user_id = ?", user.ID).Error; err != nil {
			return ErrNotFound
		}
		if user.Status == "closed" {
			return problem(409, "ACCOUNT_CLOSED", "已关闭的网站人员账号不能重新启用")
		}
		if user.Status == status {
			return nil
		}
		if status == "disabled" && staff.Role == "admin" {
			count, err := activeAdminCount(tx)
			if err != nil {
				return err
			}
			if count <= 1 {
				return problem(409, "LAST_ADMIN_PROTECTED", "至少保留一名可登录的管理员")
			}
		}
		changes := map[string]any{"status": status, "disabled_reason": nil}
		if status == "disabled" {
			changes["disabled_reason"] = strings.TrimSpace(reason)
		}
		if err := tx.Model(&user).Updates(changes).Error; err != nil {
			return err
		}
		if err := tx.Model(&storage.Session{}).Where("user_id = ? AND revoked_at IS NULL", user.ID).Update("revoked_at", now()).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "staff."+status, "staff", user.ID, reason)
	}))
}
