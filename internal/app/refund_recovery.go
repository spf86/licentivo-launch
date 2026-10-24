package app

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/domain"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/storage"
)

type RefundRecoveryInput struct {
	Reason           string `json:"reason"`
	ExternalRefundID string `json:"external_refund_id,omitempty"`
}

// ReconcileRefund resumes the original approved operation. Providing a receipt
// only schedules read-only verification; it never creates a replacement refund.
func (s *Service) ReconcileRefund(ctx context.Context, p Principal, id string, in RefundRecoveryInput) error {
	if !p.IsAdmin() {
		return ErrForbidden
	}
	in.Reason = strings.TrimSpace(in.Reason)
	in.ExternalRefundID = strings.TrimSpace(in.ExternalRefundID)
	if !integrationUUIDPattern.MatchString(id) || len(in.Reason) < 2 || len(in.Reason) > 500 || len(in.ExternalRefundID) > 128 || strings.Trim(in.ExternalRefundID, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-.") != "" {
		return ErrInvalid
	}
	var row storage.Refund
	var payment storage.Payment
	var order storage.Order
	var account storage.PaymentAccount
	if s.db.WithContext(ctx).First(&row, "id = ?", id).Error != nil {
		return ErrNotFound
	}
	if s.db.WithContext(ctx).First(&payment, "id = ?", row.PaymentID).Error != nil || s.db.WithContext(ctx).First(&order, "id = ?", payment.OrderID).Error != nil || s.db.WithContext(ctx).First(&account, "id = ?", row.AccountID).Error != nil {
		return ErrStorage
	}
	lease, err := newID()
	if err != nil {
		return err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user storage.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", order.UserID).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", order.ID).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&payment, "id = ?", payment.ID).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", id).Error; err != nil {
			return err
		}
		if row.ApprovedAt == nil || row.Status == "requested" || row.Status == "succeeded" || row.Status == "failed" || order.Status == "reversed" || payment.ExternalTransactionID == nil || row.AmountMinor != payment.AmountMinor {
			return problem(409, "REFUND_STATE_CONFLICT", "当前退款无需恢复，或需要先核对订单撤款状态")
		}
		if row.LeaseUntil != nil && row.LeaseUntil.After(now()) {
			return problem(409, "REFUND_BUSY", "退款正在处理，请稍后核对")
		}
		if in.ExternalRefundID != "" && row.ExternalRefundID != nil && *row.ExternalRefundID != in.ExternalRefundID {
			return problem(409, "REFUND_REFERENCE_CONFLICT", "退款编号与原记录不一致")
		}
		return tx.Model(&row).Updates(map[string]any{"review_required": true, "lease_token": lease, "lease_until": now().Add(time.Minute)}).Error
	})
	if err != nil {
		return databaseError(err)
	}
	defer func() {
		finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = s.db.WithContext(finish).Model(&storage.Refund{}).Where("id = ? AND lease_token = ?", id, lease).Updates(map[string]any{"lease_token": nil, "lease_until": nil}).Error
	}()
	provider, err := s.paymentProvider(ctx, account)
	if err != nil {
		return err
	}
	reference := in.ExternalRefundID
	if reference == "" && row.ExternalRefundID != nil {
		reference = *row.ExternalRefundID
	}
	var observed ports.Refund
	hasReceipt := false
	if reference != "" {
		observed, err = provider.QueryRefund(ctx, reference)
		if err != nil {
			return problem(503, "REFUND_QUERY_PENDING", "原渠道退款暂时无法查询，请稍后核对")
		}
		hasReceipt = true
	} else if reader, ok := provider.(ports.RefundRecoveryProvider); ok {
		observed, err = reader.RecoverRefund(ctx, ports.RefundRequest{OperationID: row.OperationID, PaymentID: *payment.ExternalTransactionID, Amount: domain.Money{Currency: payment.Currency, Minor: row.AmountMinor}, RequestedAt: *row.ApprovedAt})
		hasReceipt = err == nil
	}
	if hasReceipt {
		if observed.Status != ports.PaymentSucceeded && observed.Status != ports.PaymentPending && observed.Status != ports.PaymentFailed && observed.Status != ports.PaymentUnknown {
			return problem(409, "REFUND_RECEIPT_MISMATCH", "原渠道退款状态无法识别，请核对原渠道记录")
		}
		if observed.ExternalID == "" || len(observed.ExternalID) > 128 || observed.PaymentID != *payment.ExternalTransactionID || observed.Amount.Currency != payment.Currency || observed.Amount.Minor != row.AmountMinor || reference != "" && observed.ExternalID != reference {
			return problem(409, "REFUND_RECEIPT_MISMATCH", "原渠道退款的交易、金额、币种或编号不匹配")
		}
		reference = observed.ExternalID
	} else if (account.Provider != "stripe" && account.Provider != "paypal") || !now().Before(row.ApprovedAt.Add(23*time.Hour)) {
		return problem(409, "REFUND_REFERENCE_REQUIRED", "原退款结果仍未确认，请填写原渠道退款编号后核对；系统没有重新提交退款")
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user storage.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", order.UserID).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", order.ID).Error; err != nil {
			return err
		}
		if order.Status == "reversed" {
			return problem(409, "REFUND_STATE_CONFLICT", "订单已撤款，请先核对资金状态")
		}
		updates := map[string]any{"status": "pending", "attempts": 0, "review_required": false, "next_check_at": now(), "lease_token": nil, "lease_until": nil}
		if reference != "" {
			updates["external_refund_id"] = reference
		}
		result := tx.Model(&storage.Refund{}).Where("id = ? AND lease_token = ?", id, lease).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return problem(409, "REFUND_STATE_CONFLICT", "退款处理状态已变化，请刷新后核对")
		}
		if err := tx.Model(&order).Where("status = 'paid'").Update("status", "refunding").Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "refund.reconcile", "refund", id, in.Reason)
	}))
}
