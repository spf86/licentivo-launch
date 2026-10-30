package app

import (
	"context"
	"crypto/sha256"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/storage"
)

func (s *Service) reconcilePayment(ctx context.Context, orderID string) error {
	var order storage.Order
	var payment storage.Payment
	var account storage.PaymentAccount
	if s.db.WithContext(ctx).First(&order, "id = ?", orderID).Error != nil || s.db.WithContext(ctx).First(&payment, "order_id = ?", orderID).Error != nil || s.db.WithContext(ctx).First(&account, "id = ?", payment.AccountID).Error != nil {
		return ErrNotFound
	}
	if payment.Status == "succeeded" {
		return nil
	}
	// Back off before any early return or remote call. Otherwise old checkout
	// failures can occupy every slot in the worker's bounded payment batch.
	next := now().Add(2 * time.Minute)
	if err := s.db.WithContext(ctx).Model(&storage.Payment{}).Where("id = ? AND status IN ('creating','pending','unknown')", payment.ID).Update("next_check_at", next).Error; err != nil {
		return ErrStorage
	}
	if payment.ExternalCheckoutID == nil {
		if s.RecoveryRequired(ctx) {
			return problem(409, "RECOVERY_REVIEW_REQUIRED", "请先核对恢复后的交易")
		}
		_, err := s.resumeCheckout(ctx, order)
		return err
	}
	client, err := s.paymentProvider(ctx, account)
	if err != nil {
		return ErrStorage
	}
	var observed ports.Payment
	if payment.CaptureOperationID != nil && !s.RecoveryRequired(ctx) && now().Before(payment.CreatedAt.Add(23*time.Hour)) {
		observed, err = client.CapturePayment(ctx, *payment.ExternalCheckoutID, *payment.CaptureOperationID)
	} else {
		observed, err = client.QueryPayment(ctx, *payment.ExternalCheckoutID)
	}
	if err != nil {
		return problem(503, "PAYMENT_QUERY_PENDING", "付款结果暂未明确，系统将继续核实，请勿重复付款")
	}
	if observed.Status != ports.PaymentSucceeded {
		return nil
	}
	if observed.OrderID != order.ID || observed.MerchantRef != account.MerchantRef || string(observed.Environment) != order.Environment || observed.Amount.Currency != order.Currency || observed.Amount.Minor != order.AmountMinor || observed.ExternalID == "" || observed.PaidAt == nil || observed.PaidAt.After(now().Add(5*time.Minute)) || observed.PaidAt.Before(order.CreatedAt.Add(-2*time.Minute)) {
		return problem(409, "PAYMENT_VERIFICATION_FAILED", "付款结果需要商家核实")
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
		if payment.Status == "succeeded" {
			return nil
		}
		paidAt := observed.PaidAt.UTC()
		if err := tx.Model(&payment).Updates(map[string]any{"status": "succeeded", "external_transaction_id": observed.ExternalID, "paid_at": paidAt, "next_check_at": nil}).Error; err != nil {
			return err
		}
		if err := tx.Model(&order).Updates(map[string]any{"status": "paid", "paid_at": paidAt}).Error; err != nil {
			return err
		}
		if err := s.applyPaidCoverage(tx, order, paidAt); err != nil {
			return err
		}
		if err := s.queueCustomerNotification(tx, user.ID, "Payment confirmed", "Payment for order "+order.ID+" has been confirmed. Software access is being prepared. View the order in your account."); err != nil {
			return err
		}
		return audit(tx, "", "payment.confirmed", "order", order.ID, "")
	}))
}

func securityDigest(raw []byte) []byte { sum := sha256.Sum256(raw); return sum[:] }

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}
func (s *Service) failAuthorization(ctx context.Context, task storage.SyncTask, lease, code string, manual bool) error {
	state := "retrying"
	if manual || task.Attempts >= 12 {
		state = "manual_review"
	}
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return databaseError(s.db.WithContext(finish).Model(&storage.SyncTask{}).Where("operation_id = ? AND lease_token = ?", task.OperationID, lease).Updates(map[string]any{"status": state, "available_at": now().Add(time.Duration(1<<min(task.Attempts, 10)) * time.Second), "lease_token": nil, "lease_until": nil, "last_error_code": code}).Error)
}

func (s *Service) processOutstandingPayments(ctx context.Context) error {
	var outstanding []storage.Payment
	if err := s.db.WithContext(ctx).Where("status IN ('creating','pending','unknown') AND (next_check_at IS NULL OR next_check_at <= ?)", now()).Order("next_check_at,created_at,id").Limit(10).Find(&outstanding).Error; err != nil {
		return ErrStorage
	}
	for _, payment := range outstanding {
		if err := ctx.Err(); err != nil {
			return err
		}
		_ = s.reconcilePayment(ctx, payment.OrderID)
	}
	return nil
}

func (s *Service) RunCommerce(ctx context.Context) {
	timer := time.NewTicker(2 * time.Second)
	defer timer.Stop()
	lastHeartbeat := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if lastHeartbeat.IsZero() || now().Sub(lastHeartbeat) >= 15*time.Second {
			at := now()
			_ = s.db.WithContext(ctx).Model(&storage.RuntimeStatus{}).Where("id = ?", 1).Updates(map[string]any{"commerce_heartbeat": at, "updated_at": at}).Error
			lastHeartbeat = at
		}
		// Identity invalidation remains safe while financial recovery is under review.
		for i := 0; i < 10; i++ {
			handled, err := s.ProcessIdentityRevocation(ctx)
			if err != nil || !handled {
				break
			}
		}
		if s.RecoveryRequired(ctx) {
			continue
		}
		for i := 0; i < 3; i++ {
			handled, err := s.ProcessTranslation(ctx)
			if err != nil || !handled {
				break
			}
		}
		_ = s.queueLaunchMigrations(ctx)
		_ = s.ProcessCoverageTransitions(ctx)
		_ = s.ProcessRefunds(ctx)
		for i := 0; i < 10; i++ {
			handled, err := s.ProcessDeviceOperation(ctx)
			if err != nil || !handled {
				break
			}
		}
		for i := 0; i < 10; i++ {
			handled, err := s.ProcessPaymentEvent(ctx)
			if err != nil || !handled {
				break
			}
		}
		_ = s.processOutstandingPayments(ctx)
		for i := 0; i < 20; i++ {
			handled, err := s.ProcessAuthorization(ctx)
			if err != nil || !handled {
				break
			}
		}
	}
}
