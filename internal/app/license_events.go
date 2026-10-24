package app

import (
	"bytes"
	"context"
	"encoding/json"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/adapters/licentivo"
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
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&storage.LicentivoEvent{ID: id, ConnectionID: connectionID, ExternalEventID: eventID, LicenseID: event.ResourceID, BodyHash: digest, BodyCiphertext: sealed, Status: "processed", VerifiedAt: now()}).Error; err != nil {
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

// Legacy license reconciliation cannot override Launch purchase dates or make
// local runtime decisions. Current account changes are confirmed by event IDs.
func (s *Service) ReconcileEntitlement(context.Context, string) error {
	return problem(410, "LEGACY_LICENSE_RECONCILIATION_REMOVED", "请使用账号购买快照同步状态")
}
func (s *Service) ProcessLicenseEvent(context.Context) (bool, error) { return false, nil }
func (s *Service) ReconcileLicenses(context.Context) error           { return nil }
