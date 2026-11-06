package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"licentivo-launch/internal/mailer"
	"licentivo-launch/internal/storage"
)

type MailMessage = mailer.Message

// Credential metadata stays inside the encrypted queue, never in SMTP headers.
// Embedding Message preserves the JSON shape of already queued messages.
type queuedMail struct {
	mailer.Message
	CredentialID string `json:"credential_id,omitempty"`
}

// Transactional messages never contain passwords, API keys, or license keys.
func (s *Service) queueCustomerNotification(tx *gorm.DB, userID, subject, body string) error {
	var count int64
	if err := tx.Model(&storage.MailSettings{}).Where("id = ? AND enabled = true AND disable_order_notifications = false", mailSettingsID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	var user storage.User
	if err := tx.First(&user, "id = ?", userID).Error; err != nil {
		return err
	}
	return s.queueMailWithPriority(tx, MailMessage{To: user.Email, Subject: subject, Body: body + "\r\n\r\n" + s.PublicURL() + "/#/account"}, 10)
}

func (s *Service) queueMail(tx *gorm.DB, message MailMessage) error {
	return s.queueMailWithPriority(tx, message, 0)
}

func (s *Service) queueMailWithPriority(tx *gorm.DB, message MailMessage, priority int) error {
	return s.queueMailRecord(tx, queuedMail{Message: message}, priority)
}

func (s *Service) queueCredentialMail(tx *gorm.DB, credentialID string, message MailMessage) error {
	return s.queueMailRecord(tx, queuedMail{Message: message, CredentialID: credentialID}, 0)
}

func (s *Service) queueMailRecord(tx *gorm.DB, message queuedMail, priority int) error {
	enabled, err := mailEnabled(tx)
	if err != nil || !enabled {
		return err
	}
	id, err := newID()
	if err != nil {
		return err
	}
	sealed, err := s.vault.Seal(marshal(message), "email-outbox:"+id)
	if err != nil {
		return err
	}
	at := now()
	return tx.Create(&storage.EmailOutbox{ID: id, Ciphertext: sealed, Status: "pending", Priority: priority, AvailableAt: at, CreatedAt: at}).Error
}

func (s *Service) RunMail(ctx context.Context) {
	// Scan independently: a large, low-priority scan must not hold up the
	// existing authentication mail sender.
	go s.RunExpiryReminderWorker(ctx)
	timer := time.NewTicker(time.Second)
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
			_ = s.db.WithContext(ctx).Model(&storage.RuntimeStatus{}).Where("id = ?", 1).Updates(map[string]any{"mail_heartbeat": at, "updated_at": at}).Error
			lastHeartbeat = at
		}
		activeSender, err := s.configuredMailSender(ctx)
		if err != nil || activeSender == nil {
			continue
		}
		for i := 0; i < 20; i++ {
			handled, err := s.ProcessMail(ctx, activeSender)
			if err != nil || !handled {
				break
			}
		}
	}
}

func (s *Service) ProcessMail(ctx context.Context, sender mailer.Sender) (bool, error) {
	if sender == nil {
		return false, nil
	}
	var row storage.EmailOutbox
	lease, err := newID()
	if err != nil {
		return false, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		at := now()
		result := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("(status IN ('pending','retrying') AND available_at <= ?) OR (status = 'running' AND lease_until < ?)", at, at).Order("priority,available_at,id").First(&row)
		if result.Error != nil {
			return result.Error
		}
		until := at.Add(time.Minute)
		row.Attempts++
		return tx.Model(&row).Updates(map[string]any{"status": "running", "lease_token": lease, "lease_until": until, "attempts": row.Attempts}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, ErrStorage
	}
	row.LeaseToken = &lease
	plain, deliveryErr := s.vault.Open(row.Ciphertext, "email-outbox:"+row.ID)
	var queued queuedMail
	if deliveryErr == nil {
		deliveryErr = json.Unmarshal(plain, &queued)
	}
	message := queued.Message
	var cancellation string
	if deliveryErr == nil {
		cancellation, deliveryErr = s.credentialMailCancellation(ctx, message, queued.CredentialID)
	}
	if deliveryErr == nil && cancellation == "" && row.Kind == "expiry_reminder" {
		message, cancellation, deliveryErr = s.prepareExpiryReminder(ctx, row)
	}
	if deliveryErr == nil && cancellation != "" {
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		result := s.db.WithContext(finishCtx).Model(&storage.EmailOutbox{}).Where("id=? AND lease_token=?", row.ID, lease).Updates(map[string]any{"status": "cancelled", "last_error_code": cancellation, "ciphertext": []byte{}, "notification_key": nil, "lease_token": nil, "lease_until": nil})
		return true, databaseError(result.Error)
	}
	if deliveryErr == nil {
		var wait time.Time
		wait, deliveryErr = s.reserveMailQuota(ctx, row.Priority >= expiryMailPriority)
		if deliveryErr == nil && !wait.IsZero() {
			result := s.db.WithContext(ctx).Model(&storage.EmailOutbox{}).Where("id=? AND lease_token=?", row.ID, lease).Updates(map[string]any{"status": "pending", "available_at": wait, "attempts": row.Attempts - 1, "last_error_code": "MAIL_QUOTA_WAIT", "lease_token": nil, "lease_until": nil})
			return true, databaseError(result.Error)
		}
	}
	if deliveryErr == nil {
		message.IdempotencyKey = "launch-mail/" + row.ID
		deliveryCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		deliveryErr = sender.Send(deliveryCtx, message)
		cancel()
	}
	changes := map[string]any{"lease_token": nil, "lease_until": nil}
	if deliveryErr == nil {
		changes["status"], changes["sent_at"], changes["last_error_code"] = "sent", now(), nil
		if row.Kind == "expiry_reminder" || row.Kind == "expiry_test" {
			changes["ciphertext"] = []byte{}
		}
	} else {
		changes["status"] = "retrying"
		if row.Attempts >= 10 {
			changes["status"] = "manual_review"
		}
		changes["last_error_code"] = "MAIL_DELIVERY_FAILED"
		changes["available_at"] = now().Add(time.Duration(1<<min(row.Attempts, 10)) * time.Second)
	}
	// Cancellation may interrupt sending; persist recoverable state independently.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	result := s.db.WithContext(finishCtx).Model(&storage.EmailOutbox{}).Where("id = ? AND lease_token = ?", row.ID, lease).Updates(changes)
	return true, databaseError(result.Error)
}
