package app

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/payments"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/storage"
)

func paymentReversalKind(kind string) bool {
	switch kind {
	case "charge.dispute.created", "charge.dispute.updated", "charge.dispute.closed", "charge.dispute.funds_withdrawn", "charge.dispute.funds_reinstated", "PAYMENT.CAPTURE.REVERSED", "adjustment.created", "adjustment.updated":
		return true
	}
	return false
}

func (s *Service) processPaymentReversal(ctx context.Context, event storage.PaymentEvent, verified ports.VerifiedEvent) error {
	review := func() error {
		return databaseError(s.db.WithContext(ctx).Model(&event).Update("status", "review").Error)
	}
	var account storage.PaymentAccount
	if err := s.db.WithContext(ctx).First(&account, "id = ?", event.AccountID).Error; err != nil {
		return ErrStorage
	}
	provider, err := s.paymentProvider(ctx, account)
	if err != nil {
		return err
	}
	reader, ok := provider.(ports.PaymentReversalProvider)
	if !ok {
		if verified.Kind == "adjustment.created" || verified.Kind == "adjustment.updated" {
			return s.processRefundEvent(ctx, event, verified)
		}
		return review()
	}
	// Previously stored PayPal events predate the normalized amount field.
	// Their body was authenticated on receipt and encrypted with associated ID.
	if verified.Kind == "PAYMENT.CAPTURE.REVERSED" && verified.ReversalAmount == nil {
		plain, e := s.vault.Open(event.BodyCiphertext, "payment-event:"+event.ID)
		if e != nil {
			return review()
		}
		verified, e = payments.NormalizeVerifiedPayPalReversal(verified, plain)
		if e != nil {
			return review()
		}
	}
	observed, err := reader.QueryPaymentReversal(ctx, verified)
	if errors.Is(err, payments.ErrNotReversal) {
		return s.processRefundEvent(ctx, event, verified)
	}
	if err != nil {
		return problem(503, "PAYMENT_REVERSAL_PENDING", "撤款通知已保存，正在向原渠道核实")
	}
	if !observed.Confirmed {
		return databaseError(s.db.WithContext(ctx).Model(&event).Update("status", "processed").Error)
	}
	if observed.ExternalID != verified.ResourceID || observed.MerchantRef != account.MerchantRef || string(observed.Environment) != account.Environment || observed.Amount.Validate() != nil || observed.Amount.Minor <= 0 || observed.OriginalAmount.Validate() != nil || observed.Amount.Currency != observed.OriginalAmount.Currency || observed.Amount.Minor > observed.OriginalAmount.Minor || observed.PaidAt == nil || observed.PaidAt.IsZero() || observed.PaidAt.After(now().Add(5*time.Minute)) {
		return review()
	}
	var payment storage.Payment
	lookup := s.db.WithContext(ctx).Where("account_id = ? AND external_transaction_id = ?", account.ID, observed.PaymentID).First(&payment).Error
	if errors.Is(lookup, gorm.ErrRecordNotFound) && integrationUUIDPattern.MatchString(observed.OrderID) {
		lookup = s.db.WithContext(ctx).Where("account_id = ? AND order_id = ?", account.ID, observed.OrderID).First(&payment).Error
	}
	if lookup != nil {
		return review()
	}
	var order storage.Order
	if err := s.db.WithContext(ctx).First(&order, "id = ?", payment.OrderID).Error; err != nil {
		return ErrStorage
	}
	if observed.OrderID != order.ID || observed.OriginalAmount.Currency != payment.Currency || observed.OriginalAmount.Minor != payment.AmountMinor || observed.PaidAt.Before(order.CreatedAt.Add(-2*time.Minute)) || (payment.ExternalTransactionID != nil && *payment.ExternalTransactionID != observed.PaymentID) || (observed.CheckoutID != "" && payment.ExternalCheckoutID != nil && *payment.ExternalCheckoutID != observed.CheckoutID) {
		return review()
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
		var existing storage.PaymentReversal
		if err := tx.First(&existing, "account_id = ? AND external_id = ?", account.ID, observed.ExternalID).Error; err == nil {
			if existing.PaymentID != payment.ID || existing.AmountMinor != observed.Amount.Minor || existing.Currency != observed.Amount.Currency {
				return ErrInvalid
			}
			return tx.Model(&event).Update("status", "processed").Error
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if payment.ExternalTransactionID != nil && *payment.ExternalTransactionID != observed.PaymentID {
			return ErrInvalid
		}
		id, err := newID()
		if err != nil {
			return err
		}
		if err := tx.Create(&storage.PaymentReversal{ID: id, AccountID: account.ID, ExternalID: observed.ExternalID, PaymentID: payment.ID, SourceEventID: event.ID, AmountMinor: observed.Amount.Minor, Currency: observed.Amount.Currency, CreatedAt: now()}).Error; err != nil {
			return err
		}
		// A reversal may arrive before the normal payment callback. Record the
		// original capture, but never create a positive coverage for this order.
		if err := tx.Model(&payment).Updates(map[string]any{"status": "succeeded", "external_transaction_id": observed.PaymentID, "paid_at": observed.PaidAt, "next_check_at": nil}).Error; err != nil {
			return err
		}
		if err := tx.Model(&order).Updates(map[string]any{"status": "reversed", "paid_at": observed.PaidAt}).Error; err != nil {
			return err
		}
		if err := tx.Model(&storage.Refund{}).Where("payment_id = ? AND status IN ('requested','pending','unknown')", payment.ID).Update("review_required", true).Error; err != nil {
			return err
		}
		var purchase storage.Entitlement
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&purchase, "user_id = ? AND product_id = ? AND environment = ?", order.UserID, order.ProductID, order.Environment).Error; err != nil {
			return err
		}
		removed := tx.Model(&storage.EntitlementCoverage{}).Where("source_order_id = ? AND entitlement_id = ? AND voided_at IS NULL", order.ID, purchase.ID).Update("voided_at", now())
		if removed.Error != nil {
			return removed.Error
		}
		if removed.RowsAffected > 0 {
			if err := s.recomputeEntitlement(tx, &purchase, &order.ID, true); err != nil {
				return err
			}
		} else {
			var valid int64
			if err := tx.Model(&storage.EntitlementCoverage{}).Where("entitlement_id = ? AND voided_at IS NULL", purchase.ID).Count(&valid).Error; err != nil {
				return err
			}
			if valid == 0 && purchase.DesiredVersion == 0 {
				if err := tx.Model(&purchase).Update("status", "revoked").Error; err != nil {
					return err
				}
			}
		}
		if err := audit(tx, "", "payment.reversed", "order", order.ID, "渠道确认撤款，移除对应购买覆盖"); err != nil {
			return err
		}
		return tx.Model(&event).Update("status", "processed").Error
	}))
}
