package app

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"licentivo-launch/internal/storage"
)

type ManagedUser struct {
	UserView
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Service) Users(ctx context.Context, p Principal, query string) ([]ManagedUser, error) {
	if p.Role == "customer" {
		return nil, ErrForbidden
	}
	if len(query) > 254 {
		return nil, ErrInvalid
	}
	var users []storage.User
	q := s.db.WithContext(ctx).
		Where("NOT EXISTS (SELECT 1 FROM staff_accounts WHERE staff_accounts.user_id = users.id)").
		Order("created_at DESC").Limit(100)
	if query != "" {
		q = q.Where("email_key LIKE ?", strings.ToLower(query)+"%")
	}
	if err := q.Find(&users).Error; err != nil {
		return nil, ErrStorage
	}
	views := make([]ManagedUser, 0, len(users))
	for _, user := range users {
		views = append(views, ManagedUser{UserView: viewUser(user, "customer"), Status: user.Status, CreatedAt: user.CreatedAt})
	}
	return views, nil
}

func (s *Service) SetUserStatus(ctx context.Context, p Principal, id, status, reason string) error {
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
		var user storage.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", id).Error; err != nil {
			return err
		}
		var staffCount int64
		if err := tx.Model(&storage.StaffAccount{}).Where("user_id = ?", user.ID).Count(&staffCount).Error; err != nil {
			return err
		}
		if staffCount != 0 {
			return problem(409, "STAFF_ACCOUNT", "该账号属于网站人员，请在系统设置中管理")
		}
		if err := tx.Model(&user).Updates(map[string]any{"status": status, "disabled_reason": reason}).Error; err != nil {
			return err
		}
		if status == "disabled" {
			if err := tx.Model(&storage.Session{}).Where("user_id = ? AND revoked_at IS NULL", id).Update("revoked_at", now()).Error; err != nil {
				return err
			}
			var entitlements []storage.Entitlement
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", id).Find(&entitlements).Error; err != nil {
				return err
			}
			for _, e := range entitlements {
				if err := tx.Model(&e).Updates(map[string]any{"manual_hold": true, "hold_reason": reason}).Error; err != nil {
					return err
				}
				e.ManualHold = true
				if err := s.recomputeEntitlement(tx, &e, nil, true); err != nil {
					return err
				}
			}
		}
		// Restoring a login never silently clears a manual authorization hold.
		return audit(tx, p.User.ID, "user."+status, "user", id, reason)
	}))
}

type OrderView struct {
	ID              string     `json:"id"`
	ProductID       string     `json:"product_id"`
	ProductName     string     `json:"product_name"`
	ProductSlug     string     `json:"product_slug"`
	PlanVersionID   string     `json:"plan_version_id"`
	PlanName        string     `json:"plan_name"`
	PlanVersion     uint64     `json:"plan_version"`
	Action          string     `json:"action"`
	Status          string     `json:"status"`
	Currency        string     `json:"currency"`
	AmountMinor     int64      `json:"amount_minor"`
	PurchasePeriods uint64     `json:"purchase_periods"`
	CreatedAt       time.Time  `json:"created_at"`
	PaidAt          *time.Time `json:"paid_at"`
	Environment     string     `json:"environment"`
}

func (s *Service) orderView(ctx context.Context, o storage.Order) (OrderView, error) {
	v := OrderView{ID: o.ID, ProductID: o.ProductID, PlanVersionID: o.PlanVersionID, Action: o.Action, Status: o.Status, Currency: o.Currency, AmountMinor: o.AmountMinor, PurchasePeriods: o.PurchasedPeriods, CreatedAt: o.CreatedAt, PaidAt: o.PaidAt, Environment: o.Environment}
	var product storage.Product
	var version storage.PlanVersion
	var plan storage.Plan
	if s.db.WithContext(ctx).Select("name", "slug").First(&product, "id = ?", o.ProductID).Error != nil ||
		s.db.WithContext(ctx).Select("plan_id", "version").First(&version, "id = ?", o.PlanVersionID).Error != nil ||
		s.db.WithContext(ctx).Select("name").First(&plan, "id = ?", version.PlanID).Error != nil {
		return OrderView{}, ErrStorage
	}
	v.ProductName, v.ProductSlug, v.PlanName, v.PlanVersion = product.Name, product.Slug, plan.Name, version.Version
	return v, nil
}

func (s *Service) Orders(ctx context.Context, p Principal) ([]OrderView, error) {
	var orders []storage.Order
	if err := s.db.WithContext(ctx).Where("user_id = ?", p.User.ID).Order("created_at DESC").Limit(100).Find(&orders).Error; err != nil {
		return nil, ErrStorage
	}
	views := make([]OrderView, 0, len(orders))
	for _, o := range orders {
		view, err := s.orderView(ctx, o)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *Service) Audit(ctx context.Context, p Principal) ([]map[string]any, error) {
	if !p.IsAdmin() {
		return nil, ErrForbidden
	}
	var rows []storage.AuditEntry
	if s.db.WithContext(ctx).Order("created_at DESC").Limit(100).Find(&rows).Error != nil {
		return nil, ErrStorage
	}
	views := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		views = append(views, map[string]any{"id": row.ID, "action": row.Action, "object_type": row.ObjectType, "object_id": row.ObjectID, "reason": row.Reason, "created_at": row.CreatedAt})
	}
	return views, nil
}

func (s *Service) RetryMail(ctx context.Context, p Principal) error {
	if !p.IsAdmin() {
		return ErrForbidden
	}
	return databaseError(s.db.WithContext(ctx).Model(&storage.EmailOutbox{}).Where("status = 'manual_review'").Updates(map[string]any{"status": "pending", "attempts": 0, "available_at": now(), "last_error_code": nil}).Error)
}
