package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/adapters/externallicensing"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/storage"
	"time"
)

func (s *Service) queueExternalSnapshot(tx *gorm.DB, e *storage.Entitlement, chosen *coverageRow, source *string, force bool, desired string, from time.Time, until, next *time.Time, currentID *string) error {
	route := externalRoute(chosen.coverage.Snapshot)
	if route == nil || route.Environment != e.Environment {
		return ErrStorage
	}
	var user storage.User
	if err := tx.First(&user, "id = ?", e.UserID).Error; err != nil {
		return err
	}
	if user.Status != "active" && desired != "revoked" {
		desired = "suspended"
	}
	var previous ports.ExternalSnapshot
	var prior storage.SyncTask
	err := tx.Where("entitlement_id = ? AND provider = ?", e.ID, CustomLicensing).Order("target_version DESC").First(&prior).Error
	if err == nil {
		if json.Unmarshal(prior.RequestJSON, &previous) != nil {
			return ErrStorage
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if !force && previous.EventID != "" && previous.DesiredState == desired && previous.ValidFrom.Equal(from) && sameTime(previous.ValidUntil, until) && previous.LaunchPlanVersionID == chosen.version.ID && previous.EntitlementRef == purchaseReference(*e) {
		return tx.Model(e).Updates(map[string]any{"next_transition_at": next, "current_coverage_id": currentID}).Error
	}
	id, err := newID()
	if err != nil {
		return err
	}
	snapshot := ports.ExternalSnapshot{Schema: ports.ExternalLicensingVersion, Type: "entitlement.snapshot", EventID: id, ConnectionRef: route.ConnectionRef, Environment: e.Environment, AccountRef: s.subject("external-account\n" + route.ConnectionID + "\n" + e.UserID), EntitlementRef: purchaseReference(*e), LaunchProductID: e.ProductID, LaunchPlanVersionID: chosen.version.ID, ProductRef: route.ProductRef, PlanRef: route.PlanRef, Version: e.DesiredVersion + 1, DesiredState: desired, ValidFrom: from, ValidUntil: until, ChangeReason: "reconciliation"}
	switch desired {
	case "suspended":
		snapshot.ChangeReason = "suspension"
	case "expired":
		snapshot.ChangeReason = "expiry"
	case "revoked":
		snapshot.ChangeReason = "refund"
	default:
		if e.DesiredVersion == 0 {
			snapshot.ChangeReason = "purchase"
		} else {
			snapshot.ChangeReason = "resumption"
		}
	}
	if source != nil {
		var order storage.Order
		if err := tx.First(&order, "id = ?", *source).Error; err != nil {
			return err
		}
		switch order.Status {
		case "refunded":
			snapshot.ChangeReason = "refund"
		case "reversed":
			snapshot.ChangeReason = "reversal"
		default:
			switch order.Action {
			case "purchase":
				snapshot.ChangeReason = "purchase"
			case "renew":
				snapshot.ChangeReason = "renewal"
			case "upgrade":
				snapshot.ChangeReason = "upgrade"
			}
		}
	}
	if snapshot.Validate() != nil {
		return ErrStorage
	}
	raw := marshal(snapshot)
	if err := tx.Create(&storage.SyncTask{Provider: CustomLicensing, OperationID: id, EntitlementID: e.ID, SourceOrderID: source, ConnectionID: &route.ConnectionID, ExpectedVersion: e.DesiredVersion, TargetVersion: snapshot.Version, RequestJSON: raw, RequestHash: securityDigest(raw), Status: "pending"}).Error; err != nil {
		return err
	}
	e.LaunchPurchaseRef = &snapshot.EntitlementRef
	return tx.Model(e).Updates(map[string]any{"launch_purchase_ref": snapshot.EntitlementRef, "desired_version": snapshot.Version, "status": "pending", "valid_from": from, "valid_until": until, "snapshot": chosen.coverage.Snapshot, "current_coverage_id": currentID, "next_transition_at": next}).Error
}

func (s *Service) processExternalAuthorization(ctx context.Context) (bool, error) {
	var task storage.SyncTask
	lease, err := newID()
	if err != nil {
		return false, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Where("provider = ? AND available_at <= ? AND (status IN ('pending','retrying') OR (status = 'running' AND lease_until < ?))", CustomLicensing, now(), now()).Where("NOT EXISTS (SELECT 1 FROM sync_tasks prior WHERE prior.entitlement_id = sync_tasks.entitlement_id AND prior.target_version < sync_tasks.target_version AND prior.status <> 'succeeded')")
		if err := q.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Order("created_at,operation_id").First(&task).Error; err != nil {
			return err
		}
		task.Attempts++
		return tx.Model(&task).Updates(map[string]any{"status": "running", "attempts": task.Attempts, "lease_token": lease, "lease_until": now().Add(90 * time.Second)}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, databaseError(err)
	}
	var snapshot ports.ExternalSnapshot
	if json.Unmarshal(task.RequestJSON, &snapshot) != nil || snapshot.Validate() != nil || !bytes.Equal(securityDigest(marshal(snapshot)), task.RequestHash) || snapshot.EventID != task.OperationID || snapshot.Version != task.TargetVersion || task.ConnectionID == nil {
		return true, s.failAuthorization(ctx, task, lease, "EXTERNAL_SNAPSHOT_INVALID", true)
	}
	var connection storage.ExternalLicensingConnection
	if s.db.WithContext(ctx).First(&connection, "id = ? AND environment = ? AND connection_ref = ?", *task.ConnectionID, snapshot.Environment, snapshot.ConnectionRef).Error != nil {
		return true, s.failAuthorization(ctx, task, lease, "EXTERNAL_CONNECTION_INVALID", true)
	}
	client, err := s.externalClient(ctx, connection)
	if err != nil {
		return true, s.failAuthorization(ctx, task, lease, "EXTERNAL_CONNECTION_UNAVAILABLE", false)
	}
	// Query before submission, including the very first attempt. A lost POST
	// response must never create another remote license under another event ID.
	receipt, err := client.Event(ctx, task.OperationID)
	var apiError *externallicensing.APIError
	if errors.As(err, &apiError) && apiError.Status == 404 && apiError.Code == "EVENT_NOT_FOUND" {
		receipt, err = client.Submit(ctx, snapshot)
	}
	if err != nil {
		manual := errors.Is(err, externallicensing.ErrResponse)
		if errors.As(err, &apiError) && apiError.Status >= 400 && apiError.Status < 500 && apiError.Status != 429 && apiError.Code != "NONCE_REPLAYED" {
			manual = true
		}
		failure := s.failAuthorization(ctx, task, lease, "EXTERNAL_SYNC_PENDING", manual)
		if failure == nil && apiError != nil && apiError.RetryAfter > 0 {
			failure = databaseError(s.db.WithContext(ctx).Model(&storage.SyncTask{}).Where("operation_id = ? AND status = 'retrying'", task.OperationID).Update("available_at", now().Add(apiError.RetryAfter)).Error)
		}
		return true, failure
	}
	if externallicensing.ValidateReceipt(receipt, snapshot) != nil {
		return true, s.failAuthorization(ctx, task, lease, "EXTERNAL_RESPONSE_MISMATCH", true)
	}
	state := "retrying"
	var code any
	switch receipt.Status {
	case "applied":
		state = "succeeded"
	case "rejected":
		state = "manual_review"
		code = "EXTERNAL_REJECTED"
	case "manual_review":
		state = "manual_review"
		code = "EXTERNAL_REVIEW_REQUIRED"
	}
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err = s.db.WithContext(finish).Transaction(func(tx *gorm.DB) error {
		var user storage.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = (SELECT user_id FROM entitlements WHERE id = ?)", task.EntitlementID).Error; err != nil {
			return err
		}
		var purchase storage.Entitlement
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&purchase, "id = ?", task.EntitlementID).Error; err != nil {
			return err
		}
		var current storage.SyncTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "operation_id = ? AND lease_token = ? AND status = 'running'", task.OperationID, lease).Error; err != nil {
			return err
		}
		at := now()
		if err := tx.Model(&current).Updates(map[string]any{"status": state, "attempts": 0, "remote_accepted_at": at, "retry_requested": false, "available_at": at.Add(5 * time.Second), "last_error_code": code, "lease_token": nil, "lease_until": nil}).Error; err != nil {
			return err
		}
		if state != "succeeded" || purchaseReference(purchase) != snapshot.EntitlementRef || snapshot.Version <= purchase.AppliedVersion {
			return nil
		}
		changes := map[string]any{"applied_version": snapshot.Version, "remote_observed_version": snapshot.Version, "last_reconciled_at": at}
		if purchase.DesiredVersion == snapshot.Version {
			changes["status"] = snapshot.DesiredState
		}
		if err := tx.Model(&purchase).Updates(changes).Error; err != nil {
			return err
		}
		if snapshot.DesiredState == "active" && purchase.DesiredVersion == snapshot.Version && task.SourceOrderID != nil {
			return s.queueCustomerNotification(tx, user.ID, "Your software delivery is ready", "Your purchase has been processed. View its delivery information in your merchant account.")
		}
		return nil
	})
	return true, databaseError(err)
}
