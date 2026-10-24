package app

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/security"
	"licentivo-launch/internal/storage"
)

// Shares the dedicated fresh database and HTTP contract peer of the purchase
// suite, so security notifications are checked against real purchase history.
func testIdentityRevocationDelivery(t *testing.T, db *gorm.DB, service *Service, config Config, user storage.User, admin Principal, product string, peer *purchasePeer) {
	ctx := context.Background()
	id := func() string {
		value, err := security.ID()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	session := func(kind string) storage.Session {
		value := storage.Session{ID: id(), UserID: user.ID, TokenHash: security.Digest(id()), Kind: kind, CreatedAt: now().Add(-time.Minute), ExpiresAt: now().Add(time.Hour)}
		if err := db.Create(&value).Error; err != nil {
			t.Fatal(err)
		}
		return value
	}
	read := func(event string) storage.LaunchOutbox {
		var row storage.LaunchOutbox
		if err := db.First(&row, "id = ?", event).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	latest := func() storage.LaunchOutbox {
		var row storage.LaunchOutbox
		if err := db.Where("subject = ?", service.subject(user.ID)).Order("sequence DESC").First(&row).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	revoke := func(sid, reason string) storage.LaunchOutbox {
		if err := db.Transaction(func(tx *gorm.DB) error {
			var locked storage.User
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, "id = ?", user.ID).Error; err != nil {
				return err
			}
			return service.revokeSessions(tx, user.ID, sid, now(), reason)
		}); err != nil {
			t.Fatal(err)
		}
		return latest()
	}
	deliver := func() {
		t.Helper()
		if handled, err := service.ProcessIdentityRevocation(ctx); err != nil || !handled {
			t.Fatalf("revocation worker: %v %v", handled, err)
		}
	}
	due := func(event string) {
		if err := db.Model(&storage.LaunchOutbox{}).Where("id = ?", event).Update("available_at", now().Add(-time.Second)).Error; err != nil {
			t.Fatal(err)
		}
	}
	var purchases []storage.Entitlement
	if err := db.Order("id").Find(&purchases).Error; err != nil {
		t.Fatal(err)
	}
	before := marshal(purchases)

	t.Run("single logout recovers the original receipt after restart", func(t *testing.T) {
		old, other := session("desktop"), session("desktop")
		row := revoke(old.ID, "forced_logout")
		peer.mu.Lock()
		peer.revocationLoseEnvironment = "live"
		peer.mu.Unlock()
		deliver()
		if got := read(row.ID); got.Status != "retrying" || got.DeliveredAt != nil {
			t.Fatal("unconfirmed notification appeared completed")
		}
		var err error
		service, err = New(ctx, db, config)
		if err != nil {
			t.Fatal(err)
		}
		due(row.ID)
		deliver()
		got := read(row.ID)
		if got.Status != "succeeded" || got.RevokedBefore != nil {
			t.Fatal("single-session receipt not confirmed")
		}
		var receipts map[string]ports.SessionRevocationReceipt
		if json.Unmarshal(got.Results, &receipts) != nil || len(receipts) != 2 {
			t.Fatal("environment receipts missing")
		}
		for env, receipt := range receipts {
			if receipt.Scope != "session" || receipt.SessionID != old.ID || receipt.Environment != env || receipt.AccountRef != service.subject(user.ID) {
				t.Fatal("revocation escaped its account/session scope")
			}
			peer.mu.Lock()
			posts := peer.revocationPosts[receipt.ConnectionID+":"+row.ID]
			peer.mu.Unlock()
			if posts != 1 {
				t.Fatal("lost receipt caused duplicate POST")
			}
		}
		if db.First(&old, "id = ?", old.ID).Error != nil || old.RevokedAt == nil {
			t.Fatal("local logout was not committed")
		}
		if db.First(&other, "id = ?", other.ID).Error != nil || other.RevokedAt != nil {
			t.Fatal("single-session logout revoked a different login")
		}
		if err := service.identityFence(db, user.ID, now()); err != nil {
			t.Fatal("single logout blocked new logins", err)
		}
	})
	t.Run("account cutoff waits for both environments and preserves partial receipt", func(t *testing.T) {
		old := session("desktop")
		row := revoke("", "password_changed")
		if err := service.identityFence(db, user.ID, now().Add(time.Hour)); err == nil {
			t.Fatal("new authentication bypassed pending cutoff")
		}
		peer.mu.Lock()
		peer.revocationFailEnvironment = "sandbox"
		peer.mu.Unlock()
		deliver()
		partial := read(row.ID)
		var receipts map[string]ports.SessionRevocationReceipt
		if json.Unmarshal(partial.Results, &receipts) != nil || len(receipts) != 1 || partial.RevokedBefore == nil {
			t.Fatal("partial receipt was lost")
		}
		first := receipts["live"]
		if err := service.identityFence(db, user.ID, now().Add(time.Hour)); err == nil {
			t.Fatal("one environment unblocked desktop login")
		}
		peer.mu.Lock()
		peer.revocationFailEnvironment = ""
		peer.mu.Unlock()
		due(row.ID)
		deliver()
		got := read(row.ID)
		if got.Status != "succeeded" || got.RevokedBefore == nil {
			t.Fatal("account cutoff was not confirmed")
		}
		if err := service.identityFence(db, user.ID, *got.RevokedBefore); err == nil {
			t.Fatal("same-second authentication bypassed cutoff")
		}
		if err := service.identityFence(db, user.ID, got.RevokedBefore.Add(time.Second)); err != nil {
			t.Fatal("fresh authentication after cutoff rejected", err)
		}
		if _, err := service.Identity(ctx, Principal{User: user, Session: old, Role: "customer"}, product, "sandbox"); err == nil {
			t.Fatal("revoked local session received another assertion")
		}
		peer.mu.Lock()
		live := peer.revocations[first.ConnectionID+":"+row.ID]
		posts := peer.revocationPosts[first.ConnectionID+":"+row.ID]
		peer.mu.Unlock()
		if posts != 1 || live.RevokedBefore == nil || !live.RevokedBefore.Equal(*first.RevokedBefore) {
			t.Fatal("partial retry advanced a confirmed cutoff")
		}
	})
	t.Run("mismatched receipt stays blocked until audited retry", func(t *testing.T) {
		row := revoke("", "account_disabled")
		peer.mu.Lock()
		peer.revocationMismatch = true
		peer.mu.Unlock()
		deliver()
		if read(row.ID).Status != "manual_review" {
			t.Fatal("scope mismatch did not stop delivery")
		}
		if err := service.identityFence(db, user.ID, now().Add(time.Hour)); err == nil {
			t.Fatal("mismatched receipt unblocked authentication")
		}
		if err := service.RetryIdentityRevocation(ctx, Principal{User: user, Role: "customer"}, row.ID, "retry receipt"); err != ErrForbidden {
			t.Fatal("customer retried security notification")
		}
		peer.mu.Lock()
		peer.revocationMismatch = false
		peer.mu.Unlock()
		if err := service.RetryIdentityRevocation(ctx, admin, row.ID, "verified remote receipt"); err != nil {
			t.Fatal(err)
		}
		deliver()
		if read(row.ID).Status != "succeeded" {
			t.Fatal("receipt recovery failed")
		}
	})
	t.Run("preconnection security event is never replayed to a later connection", func(t *testing.T) {
		if err := db.Transaction(func(tx *gorm.DB) error {
			var trusts []storage.LaunchTrustConnection
			if err := tx.Find(&trusts).Error; err != nil {
				return err
			}
			if err := tx.Where("environment IN ?", []string{"sandbox", "live"}).Delete(&storage.LaunchTrustConnection{}).Error; err != nil {
				return err
			}
			if err := service.revokeSessions(tx, user.ID, "", now(), "password_changed"); err != nil {
				return err
			}
			return tx.Create(&trusts).Error
		}); err != nil {
			t.Fatal(err)
		}
		if latest().Status != "not_required" {
			t.Fatal("preconnection event was queued for a future connection")
		}
		if handled, err := service.ProcessIdentityRevocation(ctx); err != nil || handled {
			t.Fatal("historical cutoff was delivered to a new connection", err)
		}
	})
	t.Run("website logout needs no licensing notification", func(t *testing.T) {
		web := session("website")
		previous := latest().ID
		revoke(web.ID, "")
		if latest().ID != previous {
			t.Fatal("website-only logout affected licensing")
		}
	})
	var after []storage.Entitlement
	if err := db.Order("id").Find(&after).Error; err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, marshal(after)) {
		t.Fatal("identity revocation changed purchased dates or versions")
	}
}
