package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"licentivo-launch/internal/mailer"
	"licentivo-launch/internal/storage"
)

func credentialMailRows(t *testing.T, f *expiryFixture, legacy bool) []storage.EmailOutbox {
	t.Helper()
	var rows []storage.EmailOutbox
	f.must(f.db.Order("created_at,id").Find(&rows).Error)
	if legacy {
		for i := range rows {
			plain, err := f.s.vault.Open(rows[i].Ciphertext, "email-outbox:"+rows[i].ID)
			f.must(err)
			var message mailer.Message
			f.must(json.Unmarshal(plain, &message))
			clear(plain)
			// Older releases encrypted Message alone, without credential metadata.
			sealed, err := f.s.vault.Seal(marshal(message), "email-outbox:"+rows[i].ID)
			f.must(err)
			f.must(f.db.Model(&rows[i]).Update("ciphertext", sealed).Error)
		}
	}
	return rows
}

func credentialMailToken(t *testing.T, body string) string {
	t.Helper()
	_, remaining, ok := strings.Cut(body, "token=")
	if !ok || len(remaining) < 43 {
		t.Fatal("credential email has no complete link")
	}
	return remaining[:43]
}

func TestQueuedCredentialMail(t *testing.T) {
	ctx := context.Background()
	for _, legacy := range []bool{false, true} {
		format := "current"
		if legacy {
			format = "legacy"
		}
		t.Run(format+"_expired_after_quota_wait", func(t *testing.T) {
			f := newExpiryFixture(t)
			f.quotas(MailPeriodQuota{Total: 2, Reserve: 1, Limit: 1})
			for i := 0; i < 2; i++ {
				_, err := f.s.CheckMail(ctx, f.admin, MailCheckInput{To: "quota-check@example.test"})
				f.must(err)
			}
			f.sender.mu.Lock()
			f.sender.messages = nil
			f.sender.mu.Unlock()
			f.must(f.s.RequestEmail(ctx, f.admin.User.Email, "reset"))
			rows := credentialMailRows(t, f, legacy)
			if len(rows) != 1 {
				t.Fatal("expected one queued reset email")
			}
			handled, err := f.s.ProcessMail(ctx, f.sender)
			f.must(err)
			if !handled || len(f.sender.snapshot()) != 0 {
				t.Fatal("exhausted quota did not defer the reset email")
			}
			var job storage.EmailOutbox
			f.must(f.db.First(&job, "id=?", rows[0].ID).Error)
			if job.LastErrorCode == nil || *job.LastErrorCode != "MAIL_QUOTA_WAIT" {
				t.Fatal("fixture did not enter a quota wait")
			}
			f.must(f.db.Model(&storage.EmailCredential{}).Where("kind='reset'").Updates(map[string]any{"created_at": now().Add(-time.Hour), "expires_at": now().Add(-time.Second)}).Error)
			f.quotas(MailPeriodQuota{Total: 3, Reserve: 1, Limit: 1})
			handled, err = f.s.ProcessMail(ctx, f.sender)
			f.must(err)
			f.must(f.db.First(&job, "id=?", job.ID).Error)
			if !handled || len(f.sender.snapshot()) != 0 || job.Status != "cancelled" || job.SentAt != nil || len(job.Ciphertext) != 0 || job.LastErrorCode == nil || *job.LastErrorCode != "MAIL_CREDENTIAL_EXPIRED" {
				t.Fatal("expired reset email was not cancelled before SMTP")
			}
			quota, err := f.s.MailQuotas(ctx, f.admin)
			f.must(err)
			if quota.Usage["hour"].TotalUsed != 2 {
				t.Fatal("cancelling an expired link consumed SMTP quota")
			}
		})
		t.Run(format+"_superseded_reset", func(t *testing.T) {
			f := newExpiryFixture(t)
			f.must(f.s.RequestEmail(ctx, f.admin.User.Email, "reset"))
			f.must(f.s.RequestEmail(ctx, f.admin.User.Email, "reset"))
			rows := credentialMailRows(t, f, legacy)
			if len(rows) != 2 {
				t.Fatal("expected both reset requests to be queued")
			}
			f.process(2)
			messages := f.sender.snapshot()
			if len(messages) != 1 {
				t.Fatal("a superseded reset link was delivered")
			}
			f.must(f.s.CompleteEmail(ctx, credentialMailToken(t, messages[0].Body), "New Credential Test Password 123!"))
			var obsolete storage.EmailOutbox
			f.must(f.db.First(&obsolete, "id=?", rows[0].ID).Error)
			if obsolete.Status != "cancelled" || obsolete.LastErrorCode == nil || *obsolete.LastErrorCode != "MAIL_CREDENTIAL_USED" {
				t.Fatal("superseded credential was not cancelled")
			}
		})
		t.Run(format+"_used_link_is_not_retried", func(t *testing.T) {
			f := newExpiryFixture(t)
			f.must(f.s.RequestEmail(ctx, f.admin.User.Email, "reset"))
			rows := credentialMailRows(t, f, legacy)
			f.process(1)
			messages := f.sender.snapshot()
			if len(messages) != 1 {
				t.Fatal("valid reset email was not delivered")
			}
			token := credentialMailToken(t, messages[0].Body)
			f.must(f.s.CompleteEmail(ctx, token, "New Credential Test Password 123!"))
			// Model recovery after an uncertain delivery without creating a new job.
			f.must(f.db.Model(&rows[0]).Updates(map[string]any{"status": "retrying", "available_at": now().Add(-time.Second), "sent_at": nil}).Error)
			f.process(1)
			if len(f.sender.snapshot()) != 1 {
				t.Fatal("retry delivered an already consumed reset link")
			}
			var job storage.EmailOutbox
			f.must(f.db.First(&job, "id=?", rows[0].ID).Error)
			if job.Status != "cancelled" {
				t.Fatal("used credential retry was not cancelled")
			}
		})
	}
	for _, kind := range []string{"verify", "change_email"} {
		t.Run("valid_"+kind, func(t *testing.T) {
			f := newExpiryFixture(t)
			address := "new-customer@example.test"
			user := storage.User{ID: f.id(), Email: address, EmailKey: address, PasswordHash: f.admin.User.PasswordHash, Status: "active"}
			f.must(f.db.Create(&user).Error)
			if kind == "change_email" {
				f.must(f.s.ChangeEmail(ctx, Principal{User: user, Role: "customer"}, "changed@example.test", "Expiry Test Password 123!"))
			} else {
				f.must(f.s.RequestEmail(ctx, address, kind))
			}
			f.process(1)
			messages := f.sender.snapshot()
			if len(messages) != 1 {
				t.Fatal("a valid account confirmation was cancelled")
			}
			f.must(f.s.CompleteEmail(ctx, credentialMailToken(t, messages[0].Body), ""))
			f.must(f.db.First(&user, "id=?", user.ID).Error)
			if user.EmailVerifiedAt == nil || kind == "change_email" && user.Email != "changed@example.test" {
				t.Fatal("confirmation did not update the account")
			}
		})
	}
}

// Preserve the one-time nature of a cancelled expired credential as well.
func TestExpiredCredentialStillRejectsCompletion(t *testing.T) {
	f := newExpiryFixture(t)
	ctx := context.Background()
	f.must(f.s.RequestEmail(ctx, f.admin.User.Email, "reset"))
	rows := credentialMailRows(t, f, false)
	plain, err := f.s.vault.Open(rows[0].Ciphertext, "email-outbox:"+rows[0].ID)
	f.must(err)
	var message mailer.Message
	f.must(json.Unmarshal(plain, &message))
	clear(plain)
	f.must(f.db.Model(&storage.EmailCredential{}).Where("kind='reset'").Update("expires_at", now().Add(-time.Second)).Error)
	f.process(1)
	err = f.s.CompleteEmail(ctx, credentialMailToken(t, message.Body), "New Credential Test Password 123!")
	var business *Error
	if !errors.As(err, &business) || business.Code != "LINK_INVALID" || len(f.sender.snapshot()) != 0 {
		t.Fatal("cancellation extended or bypassed credential validity")
	}
}
