package app

import (
	"context"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/storage"
)

func (s *Service) AdminOrders(ctx context.Context, p Principal) ([]OrderView, error) {
	if p.Role == "customer" {
		return nil, ErrForbidden
	}
	var rows []storage.Order
	if s.db.WithContext(ctx).Order("created_at DESC").Limit(100).Find(&rows).Error != nil {
		return nil, ErrStorage
	}
	out := make([]OrderView, 0, len(rows))
	for _, o := range rows {
		view, err := s.orderView(ctx, o)
		if err != nil {
			return nil, err
		}
		out = append(out, view)
	}
	return out, nil
}
func (s *Service) Operations(ctx context.Context, p Principal) (map[string]any, error) {
	if p.Role == "customer" {
		return nil, ErrForbidden
	}
	var tasks []storage.SyncTask
	var entitlements []storage.Entitlement
	if s.db.WithContext(ctx).Order("created_at DESC").Limit(100).Find(&tasks).Error != nil || s.db.WithContext(ctx).Preload("FkEntitlementUser").Preload("FkEntitlementProduct").Order("updated_at DESC").Limit(100).Find(&entitlements).Error != nil {
		return nil, ErrStorage
	}
	safeTasks := make([]map[string]any, 0, len(tasks))
	for _, t := range tasks {
		safeTasks = append(safeTasks, map[string]any{"id": t.OperationID, "entitlement_id": t.EntitlementID, "status": t.Status, "attempts": t.Attempts, "error_code": t.LastErrorCode, "created_at": t.CreatedAt, "source_order_id": t.SourceOrderID})
	}
	safeEntitlements := make([]map[string]any, 0, len(entitlements))
	for _, e := range entitlements {
		customerEmail, productName := e.UserID, e.ProductID
		if e.FkEntitlementUser != nil {
			customerEmail = e.FkEntitlementUser.Email
		}
		if e.FkEntitlementProduct != nil {
			productName = e.FkEntitlementProduct.Name
		}
		safeEntitlements = append(safeEntitlements, map[string]any{"id": e.ID, "user_id": e.UserID, "customer_email": customerEmail, "product_id": e.ProductID, "product_name": productName, "environment": e.Environment, "status": e.Status, "manual_hold": e.ManualHold, "hold_reason": e.HoldReason, "valid_until": e.ValidUntil, "synchronized": e.AppliedVersion == e.DesiredVersion})
	}
	return map[string]any{"tasks": safeTasks, "entitlements": safeEntitlements}, nil
}
func (s *Service) SetEntitlementHold(ctx context.Context, p Principal, id, reason string, hold bool) error {
	if !p.IsAdmin() {
		return ErrForbidden
	}
	if len(strings.TrimSpace(reason)) < 2 || len(reason) > 500 {
		return ErrInvalid
	}
	var before storage.Entitlement
	if s.db.WithContext(ctx).First(&before, "id = ?", id).Error != nil {
		return ErrNotFound
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user storage.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", before.UserID).Error; err != nil {
			return err
		}
		var e storage.Entitlement
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&e, "id = ?", id).Error; err != nil {
			return err
		}
		if e.Status == "revoked" || !hold && user.Status != "active" {
			return problem(409, "ENTITLEMENT_HELD", "先恢复账号；永久吊销的授权不能由销售流程恢复")
		}
		if e.ManualHold == hold {
			return nil
		}
		if err := tx.Model(&e).Updates(map[string]any{"manual_hold": hold, "hold_reason": reason}).Error; err != nil {
			return err
		}
		e.ManualHold = hold
		if err := s.recomputeEntitlement(tx, &e, nil, true); err != nil {
			return err
		}
		return audit(tx, p.User.ID, "authorization.hold_changed", "entitlement", id, reason)
	}))
}
func (s *Service) AdminReconcile(ctx context.Context, p Principal, id, reason string) error {
	if !p.CanManage() {
		return ErrForbidden
	}
	if len(strings.TrimSpace(reason)) < 2 || len(reason) > 500 {
		return ErrInvalid
	}
	if err := s.reconcilePayment(ctx, id); err != nil {
		return err
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return audit(tx, p.User.ID, "order.reconciled", "order", id, reason) }))
}
