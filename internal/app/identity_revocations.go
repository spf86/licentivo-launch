package app

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/adapters/licentivo"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/storage"
)

func factRevocation(fact identityFact) (ports.SessionRevocation, bool, error) {
	in := ports.SessionRevocation{EventID: fact.EventID, AccountRef: fact.AccountRef}
	var data struct {
		SessionID string `json:"session_id"`
		Kind      string `json:"kind"`
		Reason    string `json:"reason"`
		Status    string `json:"status"`
	}
	if json.Unmarshal(fact.Data, &data) != nil {
		return in, false, ErrInvalid
	}
	in.Reason = data.Reason
	switch fact.Type {
	case "identity.session.revoked":
		if data.Kind != "desktop" {
			return in, false, nil
		}
		in.Scope, in.SessionID = "session", data.SessionID
	case "identity.account.revoked":
		in.Scope = "account"
	case "identity.account.status":
		if data.Status != "disabled" {
			return in, false, nil
		}
		in.Scope, in.Reason = "account", "account_disabled"
	default:
		return in, false, ErrInvalid
	}
	return in, true, nil
}

// Negative identity notifications have a separate transport queue and never
// change purchased dates. A disabled connection still receives revocations.
func (s *Service) ProcessIdentityRevocation(ctx context.Context) (bool, error) {
	var row storage.LaunchOutbox
	lease, err := newID()
	if err != nil {
		return false, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("available_at <= ? AND (status IN ('pending','retrying') OR (status='running' AND lease_until < ?))", now(), now()).Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Order("created_at,id").First(&row).Error; err != nil {
			return err
		}
		row.Attempts++
		return tx.Model(&row).Updates(map[string]any{"status": "running", "attempts": row.Attempts, "lease_token": lease, "lease_until": now().Add(90 * time.Second)}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, databaseError(err)
	}
	fail := func(code string, manual bool) (bool, error) {
		state := "retrying"
		if manual || row.Attempts >= 12 {
			state = "manual_review"
		}
		finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err := s.db.WithContext(finish).Model(&storage.LaunchOutbox{}).Where("id = ? AND lease_token = ?", row.ID, lease).Updates(map[string]any{"status": state, "last_error_code": code, "available_at": now().Add(time.Duration((1<<min(row.Attempts, 10))+uint64(rand.IntN(4))) * time.Second), "lease_token": nil, "lease_until": nil}).Error
		return true, databaseError(err)
	}
	var fact identityFact
	if json.Unmarshal(row.Body, &fact) != nil || fact.EventID != row.ID || fact.AccountRef != row.Subject || fact.Type != row.Kind {
		return fail("IDENTITY_EVENT_INVALID", true)
	}
	request, needed, err := factRevocation(fact)
	if err != nil {
		return fail("IDENTITY_EVENT_INVALID", true)
	}
	if !needed {
		return true, databaseError(s.db.WithContext(ctx).Model(&row).Where("lease_token = ?", lease).Updates(map[string]any{"status": "not_required", "delivered_at": now(), "lease_token": nil, "lease_until": nil}).Error)
	}
	if len(fact.Targets) == 0 {
		return fail("IDENTITY_TARGETS_REQUIRED", true)
	}
	base, err := s.launchBridge(ctx)
	if err != nil {
		return fail("REVOCATION_CONNECTION_UNAVAILABLE", false)
	}
	peer, ok := base.(ports.LaunchRevocationBridge)
	if !ok {
		return fail("REVOCATION_CONTRACT_UNSUPPORTED", true)
	}
	results := map[string]ports.SessionRevocationReceipt{}
	if len(row.Results) > 0 && json.Unmarshal(row.Results, &results) != nil {
		return fail("REVOCATION_RECEIPT_INVALID", true)
	}
	remote, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	for _, target := range fact.Targets {
		request.Environment = target.Environment
		if !integrationUUIDPattern.MatchString(target.ConnectionID) || request.Validate() != nil {
			return fail("IDENTITY_EVENT_INVALID", true)
		}
		if receipt, done := results[target.Environment]; done {
			if receipt.ConnectionID != target.ConnectionID || receipt.SessionRevocation != request || receipt.Status != "succeeded" {
				return fail("REVOCATION_RECEIPT_INVALID", true)
			}
			continue
		}
		// Recover the original committed receipt before retrying an ambiguous
		// POST. The original event ID never advances an account cutoff twice.
		receipt, err := peer.SessionRevocation(remote, target.ConnectionID, row.ID)
		if errors.Is(err, ports.ErrContractUnsupported) {
			receipt, err = peer.RevokeSessions(remote, target.ConnectionID, request)
		}
		if err != nil {
			return fail("REVOCATION_DELIVERY_PENDING", errors.Is(err, licentivo.ErrCredentials) || errors.Is(err, licentivo.ErrConflict) || errors.Is(err, ports.ErrContractUnsupported))
		}
		if receipt.ConnectionID != target.ConnectionID || receipt.SessionRevocation != request || receipt.Status != "succeeded" || request.Scope == "account" && receipt.RevokedBefore == nil {
			return fail("REVOCATION_RESPONSE_MISMATCH", true)
		}
		results[target.Environment] = receipt
		if receipt.RevokedBefore != nil && (row.RevokedBefore == nil || receipt.RevokedBefore.After(*row.RevokedBefore)) {
			cutoff := *receipt.RevokedBefore
			row.RevokedBefore = &cutoff
		}
		checkpoint, checkpointCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		update := s.db.WithContext(checkpoint).Model(&storage.LaunchOutbox{}).Where("id = ? AND lease_token = ?", row.ID, lease).Updates(map[string]any{"results": marshal(results), "revoked_before": row.RevokedBefore})
		checkpointCancel()
		if update.Error != nil {
			return true, ErrStorage
		}
		if update.RowsAffected != 1 {
			return true, nil
		}
	}
	finish, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finishCancel()
	err = s.db.WithContext(finish).Model(&storage.LaunchOutbox{}).Where("id = ? AND lease_token = ?", row.ID, lease).Updates(map[string]any{"status": "succeeded", "attempts": 0, "last_error_code": nil, "delivered_at": now(), "lease_token": nil, "lease_until": nil}).Error
	return true, databaseError(err)
}

func (s *Service) RetryIdentityRevocation(ctx context.Context, p Principal, id, reason string) error {
	if !p.IsAdmin() {
		return ErrForbidden
	}
	if len(reason) < 2 || len(reason) > 500 {
		return ErrInvalid
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row storage.LaunchOutbox
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", id).Error; err != nil {
			return err
		}
		if row.Status == "succeeded" || row.Status == "not_required" || row.Status == "running" {
			return problem(409, "TASK_STATE_CONFLICT", "当前通知无需重试或正在处理")
		}
		if err := tx.Model(&row).Updates(map[string]any{"status": "pending", "attempts": 0, "available_at": now(), "last_error_code": nil}).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "identity.revocation.retry", "identity_event", id, reason)
	}))
}
