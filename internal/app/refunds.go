package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/domain"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/storage"
)

type RefundView struct {
	ID             string     `json:"id"`
	OrderID        string     `json:"order_id"`
	UserID         string     `json:"user_id"`
	CustomerEmail  string     `json:"customer_email,omitempty"`
	ProductName    string     `json:"product_name,omitempty"`
	Status         string     `json:"status"`
	Currency       string     `json:"currency"`
	AmountMinor    int64      `json:"amount_minor"`
	Reason         string     `json:"reason"`
	Environment    string     `json:"environment"`
	ReviewRequired bool       `json:"review_required"`
	CreatedAt      time.Time  `json:"created_at"`
	CompletedAt    *time.Time `json:"completed_at"`
}

func refundView(r storage.Refund, p storage.Payment, o storage.Order) RefundView {
	return RefundView{ID: r.ID, OrderID: o.ID, UserID: o.UserID, Status: r.Status, Currency: p.Currency, AmountMinor: r.AmountMinor, Reason: r.Reason, Environment: o.Environment, ReviewRequired: r.ReviewRequired, CreatedAt: r.CreatedAt, CompletedAt: r.CompletedAt}
}

func (s *Service) RequestRefund(ctx context.Context, p Principal, orderID, reason string) (RefundView, error) {
	reason = strings.TrimSpace(reason)
	if len(reason) < 2 || len(reason) > 500 {
		return RefundView{}, problem(400, "REASON_REQUIRED", "请填写退款原因")
	}
	var refund storage.Refund
	var payment storage.Payment
	var order storage.Order
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user storage.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", p.User.ID).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ? AND user_id = ?", orderID, p.User.ID).Error; err != nil {
			return ErrNotFound
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&payment, "order_id = ? AND status = 'succeeded'", order.ID).Error; err != nil {
			return problem(409, "PAYMENT_NOT_REFUNDABLE", "该订单尚无可退款的成功付款")
		}
		if err := tx.First(&refund, "payment_id = ?", payment.ID).Error; err == nil {
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if order.Status != "paid" {
			return problem(409, "ORDER_NOT_REFUNDABLE", "订单当前不可申请退款")
		}
		id, err := newID()
		if err != nil {
			return err
		}
		operation, err := newID()
		if err != nil {
			return err
		}
		refund = storage.Refund{ID: id, PaymentID: payment.ID, AccountID: payment.AccountID, OperationID: operation, AmountMinor: payment.AmountMinor, Status: "requested", Reason: reason, CreatedAt: now()}
		if err := tx.Create(&refund).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "refund.requested", "refund", refund.ID, reason)
	})
	if err != nil {
		return RefundView{}, databaseError(err)
	}
	return refundView(refund, payment, order), nil
}
func (s *Service) Refunds(ctx context.Context, p Principal, all bool) ([]RefundView, error) {
	if all && !p.IsAdmin() {
		return nil, ErrForbidden
	}
	var rows []storage.Refund
	q := s.db.WithContext(ctx).Model(&storage.Refund{}).Select("refunds.*").Joins("JOIN payments ON payments.id = refunds.payment_id JOIN orders ON orders.id = payments.order_id")
	if !all {
		q = q.Where("orders.user_id = ?", p.User.ID)
	}
	if err := q.Order("refunds.created_at DESC").Limit(100).Find(&rows).Error; err != nil {
		return nil, ErrStorage
	}
	out := make([]RefundView, 0, len(rows))
	customerEmails := map[string]string{}
	productNames := map[string]string{}
	for _, r := range rows {
		var payment storage.Payment
		var order storage.Order
		if s.db.WithContext(ctx).First(&payment, "id = ?", r.PaymentID).Error != nil || s.db.WithContext(ctx).First(&order, "id = ?", payment.OrderID).Error != nil {
			return nil, ErrStorage
		}
		view := refundView(r, payment, order)
		if all {
			if email, ok := customerEmails[order.UserID]; ok {
				view.CustomerEmail = email
			} else {
				var user storage.User
				if s.db.WithContext(ctx).Select("email").First(&user, "id = ?", order.UserID).Error != nil {
					return nil, ErrStorage
				}
				customerEmails[order.UserID] = user.Email
				view.CustomerEmail = user.Email
			}
			if name, ok := productNames[order.ProductID]; ok {
				view.ProductName = name
			} else {
				var product storage.Product
				if s.db.WithContext(ctx).Select("name").First(&product, "id = ?", order.ProductID).Error != nil {
					return nil, ErrStorage
				}
				productNames[order.ProductID] = product.Name
				view.ProductName = product.Name
			}
		}
		out = append(out, view)
	}
	return out, nil
}
func (s *Service) ApproveRefund(ctx context.Context, p Principal, id, reason string) error {
	if !p.IsAdmin() {
		return ErrForbidden
	}
	if len(strings.TrimSpace(reason)) < 2 || len(reason) > 500 {
		return ErrInvalid
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var r storage.Refund
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&r, "id = ?", id).Error; err != nil {
			return ErrNotFound
		}
		if r.Status != "requested" {
			return nil
		}
		var payment storage.Payment
		var order storage.Order
		if tx.First(&payment, "id = ? AND status = 'succeeded'", r.PaymentID).Error != nil || tx.First(&order, "id = ? AND status = 'paid'", payment.OrderID).Error != nil || payment.ExternalTransactionID == nil || r.AmountMinor != payment.AmountMinor {
			return problem(409, "REFUND_REVIEW_REQUIRED", "退款与原付款状态不一致，请先对账")
		}
		if err := tx.Model(&r).Updates(map[string]any{"status": "pending", "approved_at": now(), "approved_by": p.User.ID, "next_check_at": now()}).Error; err != nil {
			return err
		}
		if err := tx.Model(&order).Update("status", "refunding").Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "refund.approved", "refund", r.ID, reason)
	}))
}

func (s *Service) ProcessRefunds(ctx context.Context) error {
	for i := 0; i < 10; i++ {
		handled, err := s.processRefund(ctx)
		if err != nil {
			return err
		}
		if !handled {
			return nil
		}
	}
	return nil
}
func (s *Service) processRefund(ctx context.Context) (bool, error) {
	var r storage.Refund
	lease, err := newID()
	if err != nil {
		return false, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		at := now()
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("status IN ('pending','unknown') AND approved_at IS NOT NULL AND review_required = false AND (next_check_at IS NULL OR next_check_at <= ?) AND (lease_until IS NULL OR lease_until < ?)", at, at).Order("created_at").First(&r).Error; err != nil {
			return err
		}
		r.Attempts++
		return tx.Model(&r).Updates(map[string]any{"lease_token": lease, "lease_until": at.Add(time.Minute), "attempts": r.Attempts}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, ErrStorage
	}
	finish := func(status string, review bool, external *string) error {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		changes := map[string]any{"status": status, "review_required": review, "next_check_at": now().Add(time.Duration(1<<min(r.Attempts, 10)) * time.Second), "lease_token": nil, "lease_until": nil}
		if external != nil {
			changes["external_refund_id"] = *external
		}
		if status == "pending" {
			// A confirmed approval queue is healthy, even when it lasts days.
			// Count only consecutive unknown outcomes toward manual review and
			// keep ordinary polling paced independently of the error backoff.
			changes["attempts"] = 0
			changes["next_check_at"] = now().Add(2 * time.Minute)
		}
		return databaseError(s.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
			if status == "failed" {
				var payment storage.Payment
				if err := tx.Select("order_id").First(&payment, "id = ?", r.PaymentID).Error; err != nil {
					return err
				}
				// Release the order from refunding when the provider rejects the
				// refund. Paid coverage remains valid until a confirmed refund.
				if err := tx.Model(&storage.Order{}).Where("id = ? AND status = 'refunding'", payment.OrderID).Update("status", "paid").Error; err != nil {
					return err
				}
			}
			result := tx.Model(&r).Where("lease_token = ?", lease).Updates(changes)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrStorage
			}
			return nil
		}))
	}
	var payment storage.Payment
	var account storage.PaymentAccount
	if s.db.WithContext(ctx).First(&payment, "id = ?", r.PaymentID).Error != nil || s.db.WithContext(ctx).First(&account, "id = ?", r.AccountID).Error != nil || payment.ExternalTransactionID == nil {
		return true, finish("unknown", true, nil)
	}
	provider, err := s.paymentProvider(ctx, account)
	if err != nil {
		return true, finish("unknown", r.Attempts >= 12, nil)
	}
	var observed ports.Refund
	request := ports.RefundRequest{OperationID: r.OperationID, PaymentID: *payment.ExternalTransactionID, Amount: domain.Money{Currency: payment.Currency, Minor: r.AmountMinor}}
	if r.ApprovedAt != nil {
		request.RequestedAt = *r.ApprovedAt
	}
	var orderState storage.Order
	if err := s.db.WithContext(ctx).Select("status").First(&orderState, "id = ?", payment.OrderID).Error; err != nil {
		return true, finish("unknown", true, nil)
	}
	if orderState.Status == "reversed" {
		return true, finish("unknown", true, nil)
	}
	if r.ExternalRefundID != nil {
		observed, err = provider.QueryRefund(ctx, *r.ExternalRefundID)
	} else if recoverable, ok := provider.(ports.RefundRecoveryProvider); ok && (r.SubmittedAt != nil || r.Status == "unknown") {
		// Read-only recovery remains safe beyond a provider's POST replay window.
		observed, err = recoverable.RecoverRefund(ctx, request)
	} else if r.ApprovedAt != nil && now().Before(r.ApprovedAt.Add(23*time.Hour)) {
		if r.SubmittedAt == nil {
			reserved := s.db.WithContext(ctx).Model(&storage.Refund{}).Where("id = ? AND lease_token = ? AND submitted_at IS NULL", r.ID, lease).Update("submitted_at", now())
			if reserved.Error != nil || reserved.RowsAffected != 1 {
				return true, finish("unknown", false, nil)
			}
		}
		observed, err = provider.Refund(ctx, request)
	} else {
		return true, finish("unknown", true, nil)
	}
	if err != nil {
		return true, finish("unknown", r.Attempts >= 12, nil)
	}
	if observed.ExternalID == "" || observed.PaymentID != *payment.ExternalTransactionID || observed.Amount.Currency != payment.Currency || observed.Amount.Minor != r.AmountMinor || r.ExternalRefundID != nil && *r.ExternalRefundID != observed.ExternalID {
		return true, finish("unknown", true, nil)
	}
	if observed.Status != ports.PaymentSucceeded {
		switch observed.Status {
		case ports.PaymentPending:
			return true, finish("pending", false, &observed.ExternalID)
		case ports.PaymentFailed:
			return true, finish("failed", false, &observed.ExternalID)
		case ports.PaymentUnknown:
			return true, finish("unknown", r.Attempts >= 12, &observed.ExternalID)
		default:
			return true, finish("unknown", true, &observed.ExternalID)
		}
	}
	// Lock in the same user/order/payment order as payment settlement.
	var order storage.Order
	if s.db.WithContext(ctx).First(&order, "id = ?", payment.OrderID).Error != nil {
		return true, finish("unknown", true, &observed.ExternalID)
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
		var current storage.Refund
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ? AND lease_token = ?", r.ID, lease).Error; err != nil {
			return err
		}
		if current.Status == "succeeded" {
			return nil
		}
		at := now()
		if err := tx.Model(&current).Updates(map[string]any{"status": "succeeded", "external_refund_id": observed.ExternalID, "completed_at": at, "lease_token": nil, "lease_until": nil, "next_check_at": nil}).Error; err != nil {
			return err
		}
		if err := tx.Model(&order).Where("status <> 'reversed'").Updates(map[string]any{"status": "refunded"}).Error; err != nil {
			return err
		}
		var purchase storage.Entitlement
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&purchase, "user_id = ? AND product_id = ? AND environment = ?", user.ID, order.ProductID, order.Environment).Error; err != nil {
			return err
		}
		if err := tx.Model(&storage.EntitlementCoverage{}).Where("source_order_id = ? AND entitlement_id = ? AND voided_at IS NULL", order.ID, purchase.ID).Update("voided_at", at).Error; err != nil {
			return err
		}
		if err := s.recomputeEntitlement(tx, &purchase, &order.ID, true); err != nil {
			return err
		}
		if err := s.queueCustomerNotification(tx, user.ID, "Refund confirmed", "The payment provider confirmed the full refund for order "+order.ID+". The refunded software coverage has been removed. Bank or payment method processing may take additional time."); err != nil {
			return err
		}
		return audit(tx, "", "refund.confirmed", "refund", r.ID, "")
	})
	if err != nil {
		_ = finish("unknown", false, &observed.ExternalID)
	}
	return true, databaseError(err)
}
