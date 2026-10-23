package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
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
	next := now().Add(2 * time.Minute)
	_ = s.db.WithContext(ctx).Model(&payment).Update("next_check_at", next).Error
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
		if err := s.applyPaidCoverage(tx, user, order, paidAt); err != nil {
			return err
		}
		if err := s.queueCustomerNotification(tx, user.ID, "Payment confirmed", "Payment for order "+order.ID+" has been confirmed. Software access is being prepared. View the order in your account."); err != nil {
			return err
		}
		return audit(tx, "", "payment.confirmed", "order", order.ID, "")
	}))
}

func securityDigest(raw []byte) []byte { sum := sha256.Sum256(raw); return sum[:] }

func (s *Service) ProcessAuthorization(ctx context.Context) (bool, error) {
	var task storage.SyncTask
	lease, err := newID()
	if err != nil {
		return false, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		at := now()
		err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("((status IN ('pending','retrying') AND available_at <= ?) OR (status = 'running' AND lease_until < ?)) AND NOT EXISTS (SELECT 1 FROM sync_tasks prior WHERE prior.entitlement_id = sync_tasks.entitlement_id AND prior.target_version < sync_tasks.target_version AND prior.status <> 'succeeded')", at, at).Order("available_at,operation_id").First(&task).Error
		if err != nil {
			return err
		}
		task.Attempts++
		return tx.Model(&task).Updates(map[string]any{"status": "running", "lease_token": lease, "lease_until": at.Add(time.Minute), "attempts": task.Attempts}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, ErrStorage
	}
	var command ports.EntitlementCommand
	var connection storage.LicentivoConnection
	var currentEntitlement storage.Entitlement
	if s.db.WithContext(ctx).First(&currentEntitlement, "id = ?", task.EntitlementID).Error != nil || currentEntitlement.Status == "revoked" {
		return true, s.failAuthorization(ctx, task, lease, "TERMINAL_AUTHORIZATION_HOLD", true)
	}
	if json.Unmarshal(task.RequestJSON, &command) != nil || task.ConnectionID == nil || !bytes.Equal(securityDigest(marshal(command)), task.RequestHash) {
		return true, s.failAuthorization(ctx, task, lease, "COMMAND_INVALID", true)
	}
	if command.LicenseID == "" && command.ExpectedVersion > 0 {
		var binding storage.LicenseBinding
		if s.db.WithContext(ctx).First(&binding, "entitlement_id = ? AND replaced_at IS NULL", task.EntitlementID).Error != nil {
			return true, s.failAuthorization(ctx, task, lease, "BINDING_PENDING", false)
		}
		command.LicenseID = binding.RemoteLicenseID
		// A preceding create may have still been pending when this task was queued.
		// Persist its recovered identity before the first outbound call.
		if err := s.db.WithContext(ctx).Model(&task).Where("lease_token = ?", lease).Updates(map[string]any{"request_json": marshal(command), "request_hash": securityDigest(marshal(command))}).Error; err != nil {
			return true, ErrStorage
		}
	}
	if command.Validate() != nil {
		return true, s.failAuthorization(ctx, task, lease, "COMMAND_INVALID", true)
	}
	if s.db.WithContext(ctx).First(&connection, "id = ?", *task.ConnectionID).Error != nil {
		return true, s.failAuthorization(ctx, task, lease, "CONNECTION_MISSING", true)
	}
	client, err := s.licensingProvider(ctx, connection)
	if err != nil {
		return true, s.failAuthorization(ctx, task, lease, "CONNECTION_UNAVAILABLE", false)
	}
	result, err := client.Apply(ctx, command)
	if err != nil {
		return true, s.failAuthorization(ctx, task, lease, "REMOTE_OPERATION_PENDING", errors.Is(err, ports.ErrContractUnsupported))
	}
	if result.OperationID != command.OperationID || result.Version != command.TargetVersion || result.Status != command.DesiredStatus || result.LicenseID == "" || !result.ValidFrom.Equal(command.ValidFrom) || !sameTime(result.ValidUntil, command.ValidUntil) || (command.LicenseID != "" && result.LicenseID != command.LicenseID) {
		return true, s.failAuthorization(ctx, task, lease, "REMOTE_RESULT_MISMATCH", true)
	}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	err = s.db.WithContext(finishCtx).Transaction(func(tx *gorm.DB) error {
		var entitlement storage.Entitlement
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&entitlement, "id = ?", task.EntitlementID).Error; err != nil {
			return err
		}
		var current storage.SyncTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "operation_id = ? AND lease_token = ?", task.OperationID, lease).Error; err != nil {
			return err
		}
		if entitlement.AppliedVersion < result.Version {
			if entitlement.AppliedVersion != command.ExpectedVersion {
				return problem(409, "LOCAL_VERSION_CONFLICT", "授权状态需要对账")
			}
			var binding storage.LicenseBinding
			err := tx.First(&binding, "entitlement_id = ? AND replaced_at IS NULL", entitlement.ID).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				if len(result.Secret) == 0 {
					return problem(409, "REMOTE_KEY_MISSING", "授权结果缺少可恢复凭证")
				}
				secretID, err := s.createSecret(tx, "license_key", result.Secret)
				if err != nil {
					return err
				}
				id, err := newID()
				if err != nil {
					return err
				}
				if err := tx.Create(&storage.LicenseBinding{ID: id, EntitlementID: entitlement.ID, ConnectionID: connection.ID, CustomerRef: command.CustomerRef, RemoteLicenseID: result.LicenseID, CredentialSecretID: secretID}).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else if binding.RemoteLicenseID != result.LicenseID {
				return problem(409, "LICENSE_BINDING_CONFLICT", "授权关联需要核实")
			}
			state := result.Status
			if entitlement.Status == "revoked" {
				state = "revoked"
			}
			if entitlement.DesiredVersion > result.Version && state != "revoked" {
				state = "pending"
			}
			if err := tx.Model(&entitlement).Updates(map[string]any{"applied_version": result.Version, "status": state}).Error; err != nil {
				return err
			}
			if state == "active" {
				if err := s.queueCustomerNotification(tx, entitlement.UserID, "Your software access is ready", "Your software access has been activated or updated. Sign in to your account to view your products, downloads, and access period."); err != nil {
					return err
				}
			}
		}
		sealed, err := s.vault.Seal(marshal(map[string]any{"operation_id": result.OperationID, "license_id": result.LicenseID, "version": result.Version}), "sync-result:"+task.OperationID)
		if err != nil {
			return err
		}
		return tx.Model(&current).Updates(map[string]any{"status": "succeeded", "result_ciphertext": sealed, "lease_token": nil, "lease_until": nil, "last_error_code": nil}).Error
	})
	if err != nil {
		_ = s.failAuthorization(finishCtx, task, lease, "LOCAL_RESULT_PENDING", false)
	}
	return true, databaseError(err)
}
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
		if s.RecoveryRequired(ctx) {
			continue
		}
		_ = s.ProcessCoverageTransitions(ctx)
		_ = s.ReconcileLicenses(ctx)
		for i := 0; i < 3; i++ {
			handled, err := s.ProcessTranslation(ctx)
			if err != nil || !handled {
				break
			}
		}
		for i := 0; i < 10; i++ {
			handled, err := s.ProcessLicenseEvent(ctx)
			if err != nil || !handled {
				break
			}
		}
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
		var outstanding []storage.Payment
		if s.db.WithContext(ctx).Where("status IN ('creating','pending','unknown') AND (next_check_at IS NULL OR next_check_at <= ?)", now()).Order("created_at").Limit(10).Find(&outstanding).Error == nil {
			for _, p := range outstanding {
				if ctx.Err() != nil {
					return
				}
				_ = s.reconcilePayment(ctx, p.OrderID)
			}
		}
		for i := 0; i < 20; i++ {
			handled, err := s.ProcessAuthorization(ctx)
			if err != nil || !handled {
				break
			}
		}
	}
}
