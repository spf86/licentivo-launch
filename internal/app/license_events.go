package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/adapters/licentivo"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/storage"
	"net/http"
	"time"
)

func (s *Service) ReceiveLicenseEvent(ctx context.Context, connectionID string, headers http.Header, body []byte) error {
	var connection storage.LicentivoConnection
	if s.db.WithContext(ctx).First(&connection, "id = ?", connectionID).Error != nil || connection.WebhookSecretID == nil {
		return ErrNotFound
	}
	secret, err := s.readSecret(ctx, *connection.WebhookSecretID)
	if err != nil {
		return err
	}
	eventID, err := licentivo.VerifyWebhook(headers, body, secret, now(), 5*time.Minute, true)
	if err != nil {
		return problem(400, "WEBHOOK_INVALID", "通知验证未通过")
	}
	var event struct {
		ID         string `json:"id"`
		ResourceID string `json:"resource_id"`
	}
	if json.Unmarshal(body, &event) != nil || event.ID != eventID || len(event.ResourceID) != 36 {
		return ErrInvalid
	}
	id, err := newID()
	if err != nil {
		return err
	}
	sealed, err := s.vault.Seal(body, "license-event:"+id)
	if err != nil {
		return ErrStorage
	}
	digest := securityDigest(body)
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&storage.LicentivoEvent{ID: id, ConnectionID: connectionID, ExternalEventID: eventID, LicenseID: event.ResourceID, BodyHash: digest, BodyCiphertext: sealed, Status: "pending", VerifiedAt: now()}).Error; err != nil {
			return err
		}
		var stored storage.LicentivoEvent
		if err := tx.First(&stored, "connection_id = ? AND external_event_id = ?", connectionID, eventID).Error; err != nil {
			return err
		}
		if !bytes.Equal(stored.BodyHash, digest) {
			return problem(409, "EVENT_CONFLICT", "通知内容与原事件不一致")
		}
		return nil
	}))
}
func (s *Service) ReconcileEntitlement(ctx context.Context, id string) error {
	var e storage.Entitlement
	var binding storage.LicenseBinding
	var connection storage.LicentivoConnection
	if s.db.WithContext(ctx).First(&e, "id = ?", id).Error != nil || s.db.WithContext(ctx).First(&binding, "entitlement_id = ? AND replaced_at IS NULL", e.ID).Error != nil {
		return ErrNotFound
	}
	if s.db.WithContext(ctx).First(&connection, "id = ?", binding.ConnectionID).Error != nil {
		return ErrStorage
	}
	provider, err := s.licensingProvider(ctx, connection)
	if err != nil {
		return err
	}
	stateProvider, ok := provider.(ports.LicenseStateProvider)
	if !ok {
		return ports.ErrContractUnsupported
	}
	observed, err := stateProvider.LicenseState(ctx, binding.RemoteLicenseID)
	if err != nil {
		return problem(503, "LICENSE_RECONCILIATION_PENDING", "授权状态暂未核实")
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user storage.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", e.UserID).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&e, "id = ?", e.ID).Error; err != nil {
			return err
		}
		changes := map[string]any{"last_reconciled_at": now()}
		var command ports.EntitlementCommand
		var task storage.SyncTask
		if tx.First(&task, "entitlement_id = ? AND target_version = ?", e.ID, e.AppliedVersion).Error != nil || json.Unmarshal(task.RequestJSON, &command) != nil {
			return ErrStorage
		}
		if observed.LicenseID != binding.RemoteLicenseID || observed.ProductID != command.ProductID || observed.Status != "active" && observed.Status != "expired" && observed.Status != "suspended" && observed.Status != "revoked" {
			return problem(409, "LICENSE_STATE_INVALID", "远端授权身份不一致")
		}
		if observed.NativeManagement {
			changes["remote_observed_version"] = e.AppliedVersion
			known := matchesLicenseState(observed, command)
			if observed.Status == "revoked" {
				changes["status"] = "revoked"
				changes["manual_hold"] = true
				changes["hold_reason"] = "远端授权已永久吊销"
			} else if !known {
				changes["manual_hold"] = true
				changes["hold_reason"] = "销售权益与远端授权存在差异，请人工核对"
			}
			if err := tx.Model(&e).Updates(changes).Error; err != nil {
				return err
			}
			if changes["manual_hold"] == true {
				return audit(tx, "", "authorization.remote_hold", "entitlement", e.ID, "")
			}
			return nil
		}
		// Events are hints. Only a fresh read can establish current state, so an old
		// active event never clears a manual hold or revives a permanently revoked key.
		if observed.Version < e.AppliedVersion {
			return problem(409, "LICENSE_STATE_STALE", "远端状态版本落后，需要继续核实")
		}
		changes["remote_observed_version"] = observed.Version
		var expected storage.SyncTask
		var expectedCommand ports.EntitlementCommand
		known := tx.First(&expected, "entitlement_id = ? AND target_version = ?", e.ID, observed.Version).Error == nil && json.Unmarshal(expected.RequestJSON, &expectedCommand) == nil && matchesLicenseState(observed, expectedCommand)
		if observed.Status == "revoked" {
			changes["status"] = "revoked"
			changes["manual_hold"] = true
			changes["hold_reason"] = "远端授权已永久吊销"
		} else if observed.Status == "suspended" && !known {
			changes["manual_hold"] = true
			changes["hold_reason"] = "远端授权已暂停"
			if e.DesiredVersion == e.AppliedVersion {
				changes["status"] = "suspended"
			}
		} else if !known && observed.Version >= e.AppliedVersion {
			changes["manual_hold"] = true
			changes["hold_reason"] = "销售权益与远端授权存在差异，请人工核对"
		}
		if err := tx.Model(&e).Updates(changes).Error; err != nil {
			return err
		}
		if changes["manual_hold"] == true {
			return audit(tx, "", "authorization.remote_hold", "entitlement", e.ID, "")
		}
		return nil
	}))
}
func matchesLicenseState(state ports.LicenseState, c ports.EntitlementCommand) bool {
	if state.NativeManagement {
		statusMatches := state.Status == c.DesiredStatus || c.DesiredStatus == "suspended" && state.Status == "expired"
		return state.ProductID == c.ProductID && state.PolicyID == c.PolicyID && state.PolicyVersion == c.PolicyVersion && statusMatches
	}
	return state.Version == c.TargetVersion && state.ProductID == c.ProductID && state.PolicyID == c.PolicyID && state.PolicyVersion == c.PolicyVersion && state.SnapshotHash == c.SnapshotHash && state.Status == c.DesiredStatus && state.ValidFrom.Equal(c.ValidFrom) && sameTime(state.ValidUntil, c.ValidUntil)
}
func (s *Service) ProcessLicenseEvent(ctx context.Context) (bool, error) {
	var event storage.LicentivoEvent
	if err := s.db.WithContext(ctx).Where("status = 'pending' AND available_at <= ?", now()).Order("received_at,id").First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, ErrStorage
	}
	var binding storage.LicenseBinding
	if s.db.WithContext(ctx).First(&binding, "connection_id = ? AND remote_license_id = ? AND replaced_at IS NULL", event.ConnectionID, event.LicenseID).Error != nil {
		return true, databaseError(s.db.WithContext(ctx).Model(&event).Update("status", "review").Error)
	}
	err := s.ReconcileEntitlement(ctx, binding.EntitlementID)
	state := "processed"
	if err != nil {
		state = "pending"
		if event.Attempts >= 11 || errors.Is(err, ports.ErrContractUnsupported) {
			state = "review"
		}
	}
	updateErr := s.db.WithContext(ctx).Model(&event).Where("status = 'pending'").Updates(map[string]any{"status": state, "attempts": event.Attempts + 1, "available_at": now().Add(time.Duration(1<<min(event.Attempts+1, 10)) * time.Second)}).Error
	return true, databaseError(updateErr)
}
func (s *Service) ReconcileLicenses(ctx context.Context) error {
	var rows []storage.Entitlement
	if s.db.WithContext(ctx).Where("applied_version > 0 AND (last_reconciled_at IS NULL OR last_reconciled_at < ?)", now().Add(-time.Minute)).Order("last_reconciled_at,id").Limit(5).Find(&rows).Error != nil {
		return ErrStorage
	}
	for _, e := range rows {
		if err := s.ReconcileEntitlement(ctx, e.ID); err != nil {
			_ = s.db.WithContext(ctx).Model(&e).Update("last_reconciled_at", now()).Error
		}
	}
	return nil
}
