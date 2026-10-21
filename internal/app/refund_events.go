package app

import (
	"context"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/storage"
)

func (s *Service) processRefundEvent(ctx context.Context, event storage.PaymentEvent, verified ports.VerifiedEvent) error {
	review := func() error {
		return databaseError(s.db.WithContext(ctx).Model(&event).Update("status", "review").Error)
	}
	var account storage.PaymentAccount
	if s.db.WithContext(ctx).First(&account, "id = ?", event.AccountID).Error != nil {
		return ErrStorage
	}
	provider, err := s.paymentProvider(ctx, account)
	if err != nil {
		return ErrStorage
	}
	observed, err := provider.QueryRefund(ctx, verified.ResourceID)
	if err != nil {
		return problem(503, "REFUND_EVENT_PENDING", "退款通知已保存，正在核实")
	}
	var payment storage.Payment
	var order storage.Order
	if s.db.WithContext(ctx).First(&payment, "account_id = ? AND external_transaction_id = ? AND status = 'succeeded'", account.ID, observed.PaymentID).Error != nil || observed.ExternalID != verified.ResourceID || observed.Amount.Minor != payment.AmountMinor || observed.Amount.Currency != payment.Currency || s.db.WithContext(ctx).First(&order, "id = ?", payment.OrderID).Error != nil {
		return review()
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
		var r storage.Refund
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&r, "payment_id = ?", payment.ID).Error
		if err == gorm.ErrRecordNotFound {
			id, err := newID()
			if err != nil {
				return err
			}
			operation, err := newID()
			if err != nil {
				return err
			}
			at := now()
			r = storage.Refund{ID: id, PaymentID: payment.ID, AccountID: account.ID, OperationID: operation, AmountMinor: payment.AmountMinor, Status: "pending", Reason: "支付渠道退款通知", ApprovedAt: &at, ExternalRefundID: &observed.ExternalID, CreatedAt: at}
			if err := tx.Create(&r).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if r.ExternalRefundID != nil && *r.ExternalRefundID != observed.ExternalID {
			return problem(409, "REFUND_EVENT_CONFLICT", "退款事件与原记录不一致")
		}
		if r.Status == "succeeded" {
			return nil
		}
		if err := tx.Model(&r).Updates(map[string]any{"status": "pending", "external_refund_id": observed.ExternalID, "approved_at": now(), "review_required": false, "next_check_at": now()}).Error; err != nil {
			return err
		}
		if order.Status == "paid" {
			if err := tx.Model(&order).Update("status", "refunding").Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return databaseError(err)
	}
	return databaseError(s.db.WithContext(ctx).Model(&event).Update("status", "processed").Error)
}
