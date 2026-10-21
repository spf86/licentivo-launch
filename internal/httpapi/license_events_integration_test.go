package httpapi_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"licentivo-launch/internal/app"
	"licentivo-launch/internal/security"
	"licentivo-launch/internal/storage"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func testLicenseEventLifecycle(a *testApp, service *app.Service, gateway *paymentDouble, l *licenseDouble, connectionID string, plan app.PlanView) {
	t := a.t
	ctx := context.Background()
	var e storage.Entitlement
	if a.db.First(&e).Error != nil {
		t.Fatal("missing entitlement")
	}
	if err := service.ReconcileEntitlement(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	if a.db.First(&e, "id = ?", e.ID).Error != nil || e.ManualHold {
		t.Fatal("expected sales suspension became an unexplained manual hold")
	}
	var user storage.User
	if a.db.First(&user, "id = ?", e.UserID).Error != nil {
		t.Fatal("customer missing")
	}
	principal := app.Principal{User: user, Role: "customer"}
	pending, err := service.CreateOrder(ctx, principal, app.CheckoutInput{ProductID: e.ProductID, PlanVersionID: plan.VersionID, Provider: "stripe", Environment: "sandbox", IdempotencyKey: "late-after-revocation-01"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := security.ID()
	if err != nil {
		t.Fatal(err)
	}
	secret := strings.Repeat("license-event-test-secret-", 2)
	vault, _ := security.NewVault(a.key)
	sealed, _ := vault.Seal([]byte(secret), "secret:"+id)
	if a.db.Create(&storage.SecretRecord{ID: id, Purpose: "licentivo_webhook", KeyID: "master-v1", Ciphertext: sealed}).Error != nil || a.db.Model(&storage.LicentivoConnection{}).Where("id = ?", connectionID).Update("webhook_secret_id", id).Error != nil {
		t.Fatal("could not configure isolated webhook fixture")
	}
	eventID, _ := security.ID()
	body := map[string]any{"id": eventID, "type": "license.updated", "resource_id": "33333333-3333-4333-8333-333333333333", "data": map[string]string{"status": "active"}}
	raw, _ := json.Marshal(body)
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stamp + "." + eventID + "."))
	mac.Write(raw)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	sign := func(r *http.Request) {
		r.Header.Set("X-Licentivo-Event", eventID)
		r.Header.Set("X-Licentivo-Timestamp", stamp)
		r.Header.Set("X-Licentivo-Signature", signature)
	}
	a.expect(a.request("POST", "/api/v1/webhooks/licentivo/"+connectionID, body, nil), 400)
	l.externalStatus = "revoked"
	for i := 0; i < 10; i++ {
		a.expect(a.request("POST", "/api/v1/webhooks/licentivo/"+connectionID, body, nil, sign), 200)
	}
	if handled, err := service.ProcessLicenseEvent(ctx); err != nil || !handled {
		t.Fatalf("license event processing: %v", err)
	}
	var events int64
	a.db.Model(&storage.LicentivoEvent{}).Count(&events)
	if events != 1 {
		t.Fatal("license event deduplication failed")
	}
	if a.db.First(&e, "id = ?", e.ID).Error != nil || e.Status != "revoked" || !e.ManualHold {
		t.Fatal("stale event body overrode fresh revocation")
	}
	before := len(l.requests)
	gateway.markPaid("cs_test_" + pending.ID)
	if _, err := service.ReconcileOrder(ctx, principal, pending.ID); err != nil {
		t.Fatal(err)
	}
	paid, err := service.Order(ctx, principal, pending.ID)
	if err != nil || paid.Status != "paid" || paid.Fulfillment != "manual_review" {
		t.Fatal("late payment fact was lost or revoked authorization revived")
	}
	if len(l.requests) != before {
		t.Fatal("late payment issued a new remote grant")
	}
	l.externalStatus = "active"
	if err := service.ReconcileEntitlement(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	if a.db.First(&e, "id = ?", e.ID).Error != nil || e.Status != "revoked" {
		t.Fatal("permanent revocation was cleared by later active observation")
	}
	t.Log("signed licensing events deduplicated, fresh state overrides old payload, terminal revocation survives late payment")
}
