package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/storage"
)

// ProcessAuthorization only transports an immutable purchase snapshot. Licentivo
// owns license creation, runtime authorization, devices and the remote job.
func (s *Service) ProcessAuthorization(ctx context.Context) (bool, error) {
	var task storage.SyncTask
	lease, err := newID()
	if err != nil {
		return false, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Where("launch_connection_id IS NOT NULL AND available_at <= ? AND (status IN ('pending','retrying') OR (status = 'running' AND lease_until < ?))", now(), now()).
			Where(`NOT EXISTS (
				SELECT 1 FROM sync_tasks prior
				WHERE prior.entitlement_id = sync_tasks.entitlement_id
				AND prior.launch_connection_id IS NOT NULL
				AND prior.target_version < sync_tasks.target_version
				AND prior.status <> 'succeeded'
				AND (JSON_UNQUOTE(JSON_EXTRACT(prior.request_json, '$.type')) = 'entitlement.migrated'
					OR (JSON_UNQUOTE(JSON_EXTRACT(sync_tasks.request_json, '$.type')) NOT IN ('entitlement.refunded','entitlement.expired','entitlement.suspended')
						AND NOT EXISTS (SELECT 1 FROM orders reversal
							JOIN entitlement_coverages removed ON removed.source_order_id = reversal.id
							WHERE reversal.id = sync_tasks.source_order_id AND reversal.status IN ('refunded','reversed')
							AND removed.entitlement_id = sync_tasks.entitlement_id
							AND removed.voided_at <= sync_tasks.created_at)))
			)`)
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
	var snapshot ports.CommerceSnapshot
	// MySQL normalizes JSON whitespace and object key order on storage. Hash the
	// typed canonical representation, which is also the exact retry payload.
	if json.Unmarshal(task.RequestJSON, &snapshot) != nil || !bytes.Equal(securityDigest(marshal(snapshot)), task.RequestHash) || snapshot.Validate() != nil || snapshot.EventID != task.OperationID || snapshot.Version != task.TargetVersion || task.ConnectionID == nil {
		return true, s.failAuthorization(ctx, task, lease, "PURCHASE_SNAPSHOT_INVALID", true)
	}
	peer, err := s.bridgeForConnection(ctx, *task.ConnectionID)
	if err != nil {
		return true, s.failAuthorization(ctx, task, lease, "LICENTIVO_CONNECTION_UNAVAILABLE", false)
	}
	request, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	if task.RetryRequested {
		if err := peer.RetryEvent(request, *task.LaunchConnectionID, task.OperationID); err != nil {
			return true, s.failAuthorization(ctx, task, lease, "REMOTE_RETRY_PENDING", false)
		}
	}
	var remote ports.LaunchEventStatus
	if task.RemoteAcceptedAt == nil {
		remote, err = peer.Deliver(request, *task.LaunchConnectionID, snapshot)
	} else {
		remote, err = peer.Event(request, *task.LaunchConnectionID, task.OperationID)
	}
	if err != nil {
		return true, s.failAuthorization(ctx, task, lease, "PURCHASE_SYNC_PENDING", false)
	}
	if remote.ConnectionID != *task.LaunchConnectionID || remote.EventID != task.OperationID {
		return true, s.failAuthorization(ctx, task, lease, "PURCHASE_RESPONSE_MISMATCH", true)
	}
	state, code := "retrying", any(nil)
	switch remote.Status {
	case "pending":
	case "applied":
		state = "succeeded"
	case "manual_review":
		state, code = "manual_review", "LICENTIVO_PURCHASE_REVIEW"
	case "stale":
		state, code = "manual_review", "LICENTIVO_PURCHASE_VERSION_CONFLICT"
	default:
		return true, s.failAuthorization(ctx, task, lease, "PURCHASE_RESPONSE_INVALID", true)
	}
	finish, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finishCancel()
	err = s.db.WithContext(finish).Transaction(func(tx *gorm.DB) error {
		var purchase storage.Entitlement
		if err := tx.First(&purchase, "id = ?", task.EntitlementID).Error; err != nil {
			return err
		}
		var user storage.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", purchase.UserID).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&purchase, "id = ?", task.EntitlementID).Error; err != nil {
			return err
		}
		var current storage.SyncTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "operation_id = ? AND lease_token = ?", task.OperationID, lease).Error; err != nil {
			return err
		}
		accepted := task.RemoteAcceptedAt
		if accepted == nil {
			at := now()
			accepted = &at
		}
		if err := tx.Model(&current).Updates(map[string]any{"status": state, "attempts": 0, "remote_accepted_at": accepted, "retry_requested": false, "available_at": now().Add(3 * time.Second), "last_error_code": code, "lease_token": nil, "lease_until": nil}).Error; err != nil {
			return err
		}
		if state != "succeeded" || purchaseReference(purchase) != snapshot.EntitlementRef || snapshot.Version <= purchase.AppliedVersion {
			return nil
		}
		changes := map[string]any{"applied_version": snapshot.Version, "remote_observed_version": snapshot.Version, "last_reconciled_at": now()}
		if purchase.DesiredVersion == snapshot.Version {
			changes["status"] = snapshot.Status()
		}
		if err := tx.Model(&purchase).Updates(changes).Error; err != nil {
			return err
		}
		// A complete snapshot removing coverage may bypass older delayed work,
		// including refunds that leave an active lower plan or shorter term. Once the
		// newer version is confirmed, old jobs are obsolete, even if their original
		// acceptance reply was lost. Their immutable payloads remain for auditing.
		if err := tx.Model(&storage.SyncTask{}).Where("entitlement_id = ? AND launch_connection_id = ? AND target_version < ? AND status <> 'succeeded' AND JSON_UNQUOTE(JSON_EXTRACT(request_json, '$.entitlement_ref')) = ? AND JSON_UNQUOTE(JSON_EXTRACT(request_json, '$.type')) <> 'entitlement.migrated'", purchase.ID, task.LaunchConnectionID, snapshot.Version, snapshot.EntitlementRef).Updates(map[string]any{"status": "succeeded", "last_error_code": "PURCHASE_SNAPSHOT_SUPERSEDED", "lease_token": nil, "lease_until": nil}).Error; err != nil {
			return err
		}
		if purchase.DesiredVersion == snapshot.Version && snapshot.Status() == "active" && task.SourceOrderID != nil {
			var order storage.Order
			if err := tx.First(&order, "id = ? AND user_id = ?", *task.SourceOrderID, purchase.UserID).Error; err != nil {
				return err
			}
			if order.Status == "paid" {
				if err := s.queueCustomerNotification(tx, purchase.UserID, "Your software access is ready", "Your purchase for order "+order.ID+" has been synchronized with Licentivo. Sign in to the application to verify access."); err != nil {
					return err
				}
			}
		}
		if snapshot.Type == "entitlement.migrated" {
			// Migration preserves the old license verbatim. A separate, newer
			// snapshot applies the current purchased dates/status after it succeeds.
			purchase.AppliedVersion = snapshot.Version
			return s.recomputeEntitlement(tx, &purchase, nil, true)
		}
		return nil
	})
	return true, databaseError(err)
}

// Existing long-key bindings are converted by Licentivo's explicit migration
// event. Historical commands and keys stay stored; this worker never replays
// the old management API or returns a long-term key to an account client.
func (s *Service) queueLaunchMigrations(ctx context.Context) error {
	var rows []storage.Entitlement
	if err := s.db.WithContext(ctx).Where("launch_purchase_ref IS NULL AND applied_version > 0 AND status <> 'revoked'").Order("updated_at,id").Limit(10).Find(&rows).Error; err != nil {
		return ErrStorage
	}
	for _, row := range rows {
		coverages, err := s.coverageRows(s.db.WithContext(ctx), row.ID)
		if err != nil {
			return databaseError(err)
		}
		for _, coverage := range coverages {
			if coverage.coverage.VoidedAt == nil {
				if err := s.registerLaunchPlan(ctx, row.Environment, coverage.version, coverage.mapping, false); err != nil {
					return err
				}
			}
		}
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var user storage.User
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", row.UserID).Error; err != nil {
				return err
			}
			var current storage.Entitlement
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", row.ID).Error; err != nil {
				return err
			}
			if current.LaunchPurchaseRef != nil {
				return nil
			}
			return s.recomputeEntitlement(tx, &current, nil, true)
		}); err != nil {
			return databaseError(err)
		}
	}
	return nil
}
