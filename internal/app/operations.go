package app

import (
	"context"
	"strings"

	"gorm.io/gorm"
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
	var notifications []storage.LaunchOutbox
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
		safeEntitlements = append(safeEntitlements, map[string]any{"id": e.ID, "user_id": e.UserID, "customer_email": customerEmail, "product_id": e.ProductID, "product_name": productName, "environment": e.Environment, "status": e.Status, "manual_hold": e.ManualHold, "hold_reason": e.HoldReason, "valid_until": e.ValidUntil, "synchronized": e.LaunchPurchaseRef != nil && e.AppliedVersion > 0 && e.AppliedVersion == e.DesiredVersion})
	}
	safeNotifications := make([]map[string]any, 0)
	if p.IsAdmin() {
		if s.db.WithContext(ctx).Order("created_at DESC").Limit(100).Find(&notifications).Error != nil {
			return nil, ErrStorage
		}
		for _, n := range notifications {
			safeNotifications = append(safeNotifications, map[string]any{"id": n.ID, "kind": n.Kind, "status": n.Status, "attempts": n.Attempts, "error_code": n.LastErrorCode, "created_at": n.CreatedAt, "delivered_at": n.DeliveredAt})
		}
	}
	return map[string]any{"tasks": safeTasks, "entitlements": safeEntitlements, "identity_notifications": safeNotifications}, nil
}
func (s *Service) SetEntitlementHold(ctx context.Context, p Principal, id, reason string, hold bool) error {
	if !p.IsAdmin() {
		return ErrForbidden
	}
	return problem(410, "AUTHORIZATION_MANAGED_BY_LICENTIVO", "请在 Licentivo 管理授权")
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
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		recovered, err := s.recoverReviewedRenewal(tx, id)
		if err != nil {
			return err
		}
		action := "order.reconciled"
		if recovered {
			action = "order.fulfillment_recovered"
		}
		return audit(tx, p.User.ID, action, "order", id, reason)
	}))
}
