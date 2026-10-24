package httpapi_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"licentivo-launch/internal/app"
	"licentivo-launch/internal/httpapi"
	"licentivo-launch/internal/payments"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/security"
	"licentivo-launch/internal/storage"
)

func testCommerce(a *testApp, admin, customer *browser, credentials map[string]string, plan app.PlanView) {
	t := a.t
	ctx := context.Background()
	id := func() string {
		value, err := security.ID()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	secretID, accountID := id(), id()
	if err := a.db.Create(&storage.SecretRecord{ID: secretID, Purpose: "test-only", KeyID: "test", Ciphertext: []byte{1}}).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.db.Create(&storage.PaymentAccount{ID: accountID, Provider: "stripe", Environment: "sandbox", MerchantRef: "acct_test", CredentialSecretID: secretID, Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	gateway := newPaymentDouble(t)
	licensing := &licenseDouble{}
	bridge := &accountBridgeDouble{}
	config := app.Config{PublicURL: "https://merchant.example", MasterKey: a.key, PaymentFactory: func(context.Context, storage.PaymentAccount) (ports.PaymentProvider, error) { return gateway, nil }, BridgeFactory: func(string) (ports.LaunchBridge, error) { return bridge, nil }, LicensingFactory: func(context.Context, storage.LicentivoConnection) (ports.LicensingProvider, error) {
		return licensing, nil
	}, LicensingSetupFactory: func(string) (app.LicentivoSetupProvider, error) { return licensing, nil }}
	service, err := app.New(ctx, a.db, config)
	if err != nil {
		t.Fatal(err)
	}
	a.handler = httpapi.Handler(httpapi.Options{App: service, PublicURL: config.PublicURL, SecureCookies: true})
	a.expect(a.request("POST", "/api/v1/admin/connections/licentivo", app.LicenseSettings{APIKey: "lv_api_test_launch_connection"}, admin), 200)
	a.expect(a.request("POST", "/api/v1/admin/connections/mappings", app.MappingInput{VersionID: plan.VersionID, PolicyID: "22222222-2222-4222-8222-222222222222"}, admin), 200)
	var mapped storage.PlanVersion
	var display app.PlanSnapshot
	if a.db.First(&mapped, "id = ?", plan.VersionID).Error != nil || json.Unmarshal(mapped.EntitlementSnapshot, &display) != nil || display.MaxDevices != 2 || len(display.Features) != 1 {
		t.Fatal("read-only policy display was not copied from Licentivo")
	}
	a.expect(a.request("POST", "/api/v1/admin/plans/"+plan.ID+"/publish", map[string]string{"version_id": plan.VersionID, "environment": "sandbox"}, admin), 200)
	a.expect(a.request("POST", "/api/v1/admin/connections/mappings", app.MappingInput{VersionID: plan.VersionID, PolicyID: "44444444-4444-4444-8444-444444444444"}, admin), 409)
	input := app.CheckoutInput{ProductID: plan.ProductID, PlanVersionID: plan.VersionID, PurchasePeriods: 3, Provider: "stripe", Environment: "sandbox", IdempotencyKey: "test-account-purchase-key"}
	order := data[app.CheckoutView](t, a.request("POST", "/api/v1/orders", input, customer))
	replay := data[app.CheckoutView](t, a.request("POST", "/api/v1/orders", input, customer))
	if order.ID != replay.ID || gateway.creates != 1 {
		t.Fatal("purchase replay duplicated checkout")
	}
	a.expect(a.request("GET", "/api/v1/orders/"+order.ID, nil, admin), 404)
	gateway.markPaid("cs_test_" + order.ID)
	body := map[string]any{"id": "evt_account_repeat_test", "type": "checkout.session.completed", "livemode": false, "created": time.Now().Unix(), "data": map[string]any{"object": map[string]string{"id": "cs_test_" + order.ID}}}
	raw, _ := json.Marshal(body)
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(gateway.secret))
	mac.Write([]byte(stamp + "."))
	mac.Write(raw)
	signature := "t=" + stamp + ",v1=" + hex.EncodeToString(mac.Sum(nil))
	for range 3 {
		a.expect(a.request("POST", "/api/v1/webhooks/payments/"+accountID, body, nil, func(r *http.Request) { r.Header.Set("Stripe-Signature", signature) }), 200)
	}
	if handled, err := service.ProcessPaymentEvent(ctx); err != nil || !handled {
		t.Fatalf("payment event: %v", err)
	}
	paid := data[app.CheckoutView](t, a.request("GET", "/api/v1/orders/"+order.ID, nil, customer))
	if paid.Status != "paid" || paid.Fulfillment != "pending_authorization" {
		t.Fatal("payment was mistaken for applied purchase")
	}
	if handled, err := service.ProcessAuthorization(ctx); err != nil || !handled {
		t.Fatal("purchase snapshot was not sent", err)
	}
	var settled storage.Order
	if err := a.db.First(&settled, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	var purchase storage.Entitlement
	if err := a.db.First(&purchase, "user_id = ? AND product_id = ? AND environment = 'sandbox'", settled.UserID, plan.ProductID).Error; err != nil {
		t.Fatal(err)
	}
	if purchase.LaunchPurchaseRef == nil || purchase.AppliedVersion != purchase.DesiredVersion || len(bridge.events) != 1 {
		t.Fatal("purchase was not confirmed by its event")
	}
	for _, event := range bridge.events {
		if event.PlanRef != plan.VersionID || event.ValidUntil == nil || event.Type != "entitlement.created" {
			t.Fatal("purchase snapshot did not contain absolute dates")
		}
		encoded, _ := json.Marshal(event)
		for _, secret := range []string{"@", "password", "amount_minor", "license_key"} {
			if strings.Contains(string(encoded), secret) {
				t.Fatal("commercial snapshot leaked private data")
			}
		}
	}
	var bindings int64
	a.db.Model(&storage.LicenseBinding{}).Where("entitlement_id = ?", purchase.ID).Count(&bindings)
	if bindings != 0 {
		t.Fatal("account issuance stored a long-term license binding")
	}
	a.expect(a.request("POST", "/api/v1/software/products/"+plan.ProductID+"/identity", map[string]string{"environment": "sandbox"}, customer), 403)
	login := map[string]string{"email": credentials["email"], "password": credentials["password"], "kind": "desktop"}
	grant := data[app.SessionGrant](t, a.request("POST", "/api/v1/auth/login", login, nil, func(r *http.Request) { r.Header.Del("Origin") }))
	desktop := func(r *http.Request) {
		r.Header.Del("Origin")
		r.Header.Set("Authorization", "Bearer "+grant.AccessToken)
	}
	a.expect(a.request("POST", "/api/v1/software/products/"+plan.ProductID+"/credential", map[string]string{"environment": "sandbox"}, nil, desktop), 410)
	identity := data[app.IdentityView](t, a.request("POST", "/api/v1/software/products/"+plan.ProductID+"/identity", map[string]string{"environment": "sandbox"}, nil, desktop))
	if identity.IdentityToken == "" || identity.LaunchConnectionID == "" || identity.LicentivoProductID != "11111111-1111-4111-8111-111111111111" {
		t.Fatal("identity context missing")
	}
	device := id()
	bridge.view = ports.LaunchDevices{Items: []ports.Device{{ID: device, Generation: 7, Status: "active"}}, Remaining: 7, CanRelease: true}
	view := data[app.DevicesView](t, a.request("GET", "/api/v1/me/products/"+plan.ProductID+"/devices?environment=sandbox", nil, customer))
	if view.Remaining != 7 || bridge.lastPurchase != *purchase.LaunchPurchaseRef || bridge.lastAccount == "" {
		t.Fatal("remote device scope or quota changed")
	}
	operation := id()
	a.expect(a.request("POST", "/api/v1/me/products/"+plan.ProductID+"/devices/release", map[string]any{"device_id": device, "environment": "sandbox", "idempotency_key": operation, "expected_generation": 7}, customer), 200)
	if bridge.lastRelease.OperationID != operation || bridge.lastRelease.ExpectedGeneration != 7 {
		t.Fatal("release changed operation or generation")
	}
	var jobs int64
	a.db.Model(&storage.DeviceOperation{}).Count(&jobs)
	if jobs != 0 {
		t.Fatal("Launch created a competing release job")
	}
	a.expect(a.request("POST", "/api/v1/auth/logout", map[string]any{}, nil, desktop), 200)
	a.expect(a.request("POST", "/api/v1/software/products/"+plan.ProductID+"/identity", map[string]string{"environment": "sandbox"}, nil, desktop), 401)
	var notifications int64
	a.db.Model(&storage.LaunchOutbox{}).Where("kind = 'identity.session.revoked'").Count(&notifications)
	if notifications == 0 {
		t.Fatal("identity revocation was not durably retained")
	}
	testStaffAndExports(a, admin, customer, credentials)
	// Account security is delivered through the identity contract, never by
	// changing purchase dates. Website recovery stays available during retries.
	for range 10 {
		handled, err := service.ProcessIdentityRevocation(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !handled {
			break
		}
	}
	a.expect(a.request("POST", "/api/v1/admin/users/"+settled.UserID+"/status", map[string]string{"status": "disabled", "reason": "verify remote account revocation"}, admin), 200)
	a.expect(a.request("POST", "/api/v1/admin/users/"+settled.UserID+"/status", map[string]string{"status": "active", "reason": "verify fresh login after revocation"}, admin), 200)
	a.expect(a.request("POST", "/api/v1/auth/login", login, nil, func(r *http.Request) { r.Header.Del("Origin") }), 503)
	recovered := &browser{}
	a.expect(a.request("POST", "/api/v1/auth/login", map[string]string{"email": credentials["email"], "password": credentials["password"], "kind": "website"}, recovered), 200)
	var notification storage.LaunchOutbox
	if a.db.Where("kind = 'identity.account.revoked' AND status = 'pending'").Order("created_at DESC").First(&notification).Error != nil {
		t.Fatal("account revocation not queued")
	}
	if err := a.db.Model(&notification).Update("status", "manual_review").Error; err != nil {
		t.Fatal(err)
	}
	retryURL := "/api/v1/admin/identity-notifications/" + notification.ID + "/retry"
	a.expect(a.request("POST", retryURL, map[string]string{"reason": "customer cannot retry"}, recovered), 403)
	a.expect(a.request("POST", retryURL, map[string]string{"reason": "verified original identity notification"}, admin), 200)
	for range 10 {
		handled, err := service.ProcessIdentityRevocation(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !handled {
			break
		}
	}
	if a.db.First(&notification, "id = ?", notification.ID).Error != nil || notification.Status != "succeeded" || notification.RevokedBefore == nil {
		t.Fatal("account notification was not confirmed")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Unix() <= notification.RevokedBefore.Unix() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	fresh := data[app.SessionGrant](t, a.request("POST", "/api/v1/auth/login", login, nil, func(r *http.Request) { r.Header.Del("Origin") }))
	a.expect(a.request("POST", "/api/v1/software/products/"+plan.ProductID+"/identity", map[string]string{"environment": "sandbox"}, nil, func(r *http.Request) {
		r.Header.Del("Origin")
		r.Header.Set("Authorization", "Bearer "+fresh.AccessToken)
	}), 200)
	a.expect(a.request("POST", retryURL, map[string]string{"reason": "do not repeat a committed cutoff"}, admin), 409)

	// A lost refund response is recoverable from the original provider receipt.
	// The admin route enforces both the account role and website CSRF protection.
	refund := data[app.RefundView](t, a.request("POST", "/api/v1/orders/"+order.ID+"/refund", map[string]string{"reason": "test receipt recovery"}, recovered))
	a.expect(a.request("POST", "/api/v1/admin/refunds/"+refund.ID+"/approve", map[string]string{"reason": "approved test refund"}, admin), 200)
	if err := service.ProcessRefunds(ctx); err != nil {
		t.Fatal(err)
	}
	var original storage.Refund
	if err := a.db.First(&original, "id = ?", refund.ID).Error; err != nil {
		t.Fatal(err)
	}
	if original.Status != "unknown" {
		t.Fatal("lost response did not retain unknown refund")
	}
	if err := a.db.Model(&original).Update("review_required", true).Error; err != nil {
		t.Fatal(err)
	}
	recoverURL := "/api/v1/admin/refunds/" + refund.ID + "/reconcile"
	recovery := app.RefundRecoveryInput{Reason: "verified original provider receipt", ExternalRefundID: "re_" + original.OperationID}
	a.expect(a.request("POST", recoverURL, recovery, recovered), 403)
	a.expect(a.request("POST", recoverURL, recovery, admin, func(r *http.Request) { r.Header.Del("X-CSRF-Token") }), 403)
	a.expect(a.request("POST", recoverURL, app.RefundRecoveryInput{Reason: "x"}, admin), 400)
	a.expect(a.request("POST", recoverURL, recovery, admin), 200)
	if err := service.ProcessRefunds(ctx); err != nil {
		t.Fatal(err)
	}
	var completed storage.Refund
	if err := a.db.First(&completed, "id = ?", refund.ID).Error; err != nil {
		t.Fatal(err)
	}
	if completed.Status != "succeeded" || completed.OperationID != original.OperationID || gateway.refundCalls != 1 {
		t.Fatal("recovery repeated financial write or changed operation")
	}
	// Previously unsupported, authenticated reversals can be resumed by a
	// channel resend. Reusing an event ID with changed bytes remains forbidden.
	legacyBody := map[string]any{"id": "evt_legacy_reversal", "type": "charge.dispute.closed", "livemode": false, "created": time.Now().Unix(), "data": map[string]any{"object": map[string]string{"id": "dp_legacy_reversal"}}}
	legacyRaw, _ := json.Marshal(legacyBody)
	legacyStamp := strconv.FormatInt(time.Now().Unix(), 10)
	legacyMAC := hmac.New(sha256.New, []byte(gateway.secret))
	legacyMAC.Write([]byte(legacyStamp + "."))
	legacyMAC.Write(legacyRaw)
	legacySignature := "t=" + legacyStamp + ",v1=" + hex.EncodeToString(legacyMAC.Sum(nil))
	deliverLegacy := func() {
		a.expect(a.request("POST", "/api/v1/webhooks/payments/"+accountID, legacyBody, nil, func(r *http.Request) { r.Header.Set("Stripe-Signature", legacySignature) }), 200)
	}
	deliverLegacy()
	var legacy storage.PaymentEvent
	if err := a.db.First(&legacy, "account_id = ? AND external_event_id = ?", accountID, "evt_legacy_reversal").Error; err != nil {
		t.Fatal(err)
	}
	if err := a.db.Model(&legacy).Updates(map[string]any{"status": "review", "attempts": 12}).Error; err != nil {
		t.Fatal(err)
	}
	deliverLegacy()
	if err := a.db.First(&legacy, "id = ?", legacy.ID).Error; err != nil || legacy.Status != "pending" || legacy.Attempts != 0 {
		t.Fatal("channel resend did not recover original reviewed event", err)
	}
}

type paymentDouble struct {
	mu             sync.Mutex
	payments       map[string]ports.Payment
	creates        int
	secret         string
	verifier       *payments.Stripe
	refunds        map[string]ports.Refund
	refundRequests map[string]ports.RefundRequest
	refundCalls    int
	refundTimeout  bool
}

func newPaymentDouble(t *testing.T) *paymentDouble {
	t.Helper()
	secret := "whsec_" + strings.Repeat("t", 32)
	v, err := payments.NewStripe(payments.StripeConfig{APIKey: "sk_test_fixture", WebhookSecret: secret, MerchantRef: "acct_test", APIVersion: "2025-02-24.acacia", Environment: ports.Sandbox})
	if err != nil {
		t.Fatal(err)
	}
	return &paymentDouble{payments: map[string]ports.Payment{}, secret: secret, verifier: v, refunds: map[string]ports.Refund{}, refundRequests: map[string]ports.RefundRequest{}, refundTimeout: true}
}
func (p *paymentDouble) CreateCheckout(ctx context.Context, in ports.CheckoutRequest) (ports.Checkout, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	id := "cs_test_" + in.OrderID
	if _, exists := p.payments[id]; !exists {
		p.creates++
		p.payments[id] = ports.Payment{OrderID: in.OrderID, MerchantRef: "acct_test", Environment: ports.Sandbox, Amount: in.Amount, Status: ports.PaymentPending}
	}
	return ports.Checkout{ExternalID: id, RedirectURL: "https://checkout.stripe.com/test", ExpiresAt: time.Now().Add(time.Hour)}, nil
}
func (p *paymentDouble) markPaid(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v := p.payments[id]
	at := time.Now().UTC()
	v.PaidAt = &at
	v.Status = ports.PaymentSucceeded
	v.ExternalID = "pi_test_" + v.OrderID
	p.payments[id] = v
}
func (p *paymentDouble) QueryPayment(ctx context.Context, id string) (ports.Payment, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, exists := p.payments[id]
	if !exists {
		return v, errors.New("unknown checkout")
	}
	return v, nil
}
func (p *paymentDouble) CapturePayment(ctx context.Context, id, operation string) (ports.Payment, error) {
	return p.QueryPayment(ctx, id)
}
func (p *paymentDouble) Refund(_ context.Context, in ports.RefundRequest) (ports.Refund, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if r, ok := p.refunds[in.OperationID]; ok {
		if p.refundRequests[in.OperationID] != in {
			return ports.Refund{}, errors.New("refund idempotency conflict")
		}
		return r, nil
	}
	p.refundCalls++
	r := ports.Refund{ExternalID: "re_" + in.OperationID, PaymentID: in.PaymentID, Amount: in.Amount, Status: ports.PaymentSucceeded}
	p.refunds[in.OperationID] = r
	p.refundRequests[in.OperationID] = in
	if p.refundTimeout {
		p.refundTimeout = false
		return ports.Refund{}, errors.New("refund committed before timeout")
	}
	return r, nil
}
func (p *paymentDouble) QueryRefund(_ context.Context, id string) (ports.Refund, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, r := range p.refunds {
		if r.ExternalID == id {
			return r, nil
		}
	}
	return ports.Refund{}, errors.New("refund not found")
}
func (p *paymentDouble) VerifyWebhook(ctx context.Context, h http.Header, body []byte) (ports.VerifiedEvent, error) {
	return p.verifier.VerifyWebhook(ctx, h, body)
}

type licenseDouble struct {
	results        map[string]ports.EntitlementResult
	requests       map[string]string
	creates        int
	timeoutFirst   bool
	devices        []ports.Device
	releases       map[string]ports.DeviceRelease
	releaseTimeout bool
	externalStatus string
	policyName     string
}

func (l *licenseDouble) Capabilities(context.Context) (ports.LicensingCapabilities, error) {
	return ports.LicensingCapabilities{AbsoluteValidity: true, PreserveUsage: true, IdempotentRecovery: true, CompareAndSwap: true, DeviceOperations: true}, nil
}
func (l *licenseDouble) Products(context.Context) (json.RawMessage, error) {
	return json.RawMessage(`{"items":[{"id":"11111111-1111-4111-8111-111111111111","name":"测试产品"}],"total":1}`), nil
}
func (l *licenseDouble) ProductPublicKeys(_ context.Context, product string) (map[string]string, error) {
	if product != "11111111-1111-4111-8111-111111111111" {
		return nil, errors.New("unexpected product")
	}
	return map[string]string{"test-key": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}, nil
}
func (l *licenseDouble) Policies(context.Context) (json.RawMessage, error) {
	name := l.policyName
	if name == "" {
		name = "30 天专业版"
	}
	raw, _ := json.Marshal(map[string]any{"items": []map[string]any{
		{"id": "22222222-2222-4222-8222-222222222222", "product_id": "11111111-1111-4111-8111-111111111111", "name": name, "mode": "timed", "duration_days": 30, "max_devices": 2, "version": 1},
		{"id": "44444444-4444-4444-8444-444444444444", "product_id": "11111111-1111-4111-8111-111111111111", "name": "30 天工作室版", "mode": "timed", "duration_days": 30, "max_devices": 4, "version": 1},
	}, "total": 2})
	return raw, nil
}
func (l *licenseDouble) PolicyDefinition(_ context.Context, id string) (ports.PolicyDefinition, error) {
	duration := 30
	definition := ports.PolicyDefinition{
		ID: id, ProductID: "11111111-1111-4111-8111-111111111111", Mode: "timed", DurationDays: &duration,
		Version: 1, LicenseModel: "node_locked", HeartbeatSeconds: 600, OfflineMode: "limited",
		OfflineSeconds: 86400, SessionSeconds: 1200, SelfReleaseLimit: 2,
	}
	switch id {
	case "22222222-2222-4222-8222-222222222222":
		definition.Name, definition.MaxDevices, definition.Features = "30 天专业版", 2, []string{"export"}
	case "44444444-4444-4444-8444-444444444444":
		definition.Name, definition.MaxDevices, definition.Features = "30 天工作室版", 4, []string{"export", "batch"}
	default:
		return ports.PolicyDefinition{}, errors.New("policy not found")
	}
	return definition, nil
}
func (l *licenseDouble) CheckPolicy(_ context.Context, in ports.PolicyCheck) error {
	if in.PolicyVersion == 0 || len(in.SnapshotHash) != 64 || len(in.Snapshot) == 0 {
		return errors.New("policy mismatch")
	}
	return nil
}
func (l *licenseDouble) Apply(ctx context.Context, c ports.EntitlementCommand) (ports.EntitlementResult, error) {
	raw, _ := json.Marshal(c)
	if previous, exists := l.results[c.OperationID]; exists {
		if l.requests[c.OperationID] != string(raw) {
			return ports.EntitlementResult{}, errors.New("idempotency conflict")
		}
		return previous, nil
	}
	l.creates++
	result := ports.EntitlementResult{OperationID: c.OperationID, LicenseID: "33333333-3333-4333-8333-333333333333", Version: c.TargetVersion, ValidFrom: c.ValidFrom, ValidUntil: c.ValidUntil, Status: c.DesiredStatus, Secret: []byte("lv_lic_test_customer_only")}
	l.results[c.OperationID] = result
	l.requests[c.OperationID] = string(raw)
	if l.timeoutFirst {
		l.timeoutFirst = false
		return ports.EntitlementResult{}, errors.New("remote committed before transport timeout")
	}
	return result, nil
}
func (l *licenseDouble) LookupOperation(ctx context.Context, id string) (ports.EntitlementResult, error) {
	v, ok := l.results[id]
	if !ok {
		return v, errors.New("not found")
	}
	return v, nil
}
func (l *licenseDouble) Devices(_ context.Context, id string) ([]ports.Device, error) {
	out := []ports.Device{}
	for _, d := range l.devices {
		if d.LicenseID == id {
			out = append(out, d)
		}
	}
	return out, nil
}
func (l *licenseDouble) ReleaseDevice(_ context.Context, in ports.DeviceRelease) error {
	if previous, ok := l.releases[in.OperationID]; ok {
		if previous != in {
			return errors.New("operation changed")
		}
		return nil
	}
	for i := range l.devices {
		d := &l.devices[i]
		if d.ID == in.DeviceID && d.LicenseID == in.LicenseID {
			if d.Generation != in.ExpectedGeneration {
				return errors.New("generation changed")
			}
			d.Status = "released"
			l.releases[in.OperationID] = in
			if l.releaseTimeout {
				l.releaseTimeout = false
				return errors.New("release committed before timeout")
			}
			return nil
		}
	}
	return errors.New("foreign activation")
}
func (l *licenseDouble) LicenseState(_ context.Context, id string) (ports.LicenseState, error) {
	var latest ports.EntitlementResult
	for _, r := range l.results {
		if r.Version > latest.Version {
			latest = r
		}
	}
	var c ports.EntitlementCommand
	if json.Unmarshal([]byte(l.requests[latest.OperationID]), &c) != nil {
		return ports.LicenseState{}, errors.New("unknown license")
	}
	status := latest.Status
	if l.externalStatus != "" {
		status = l.externalStatus
	}
	return ports.LicenseState{LicenseID: id, ProductID: c.ProductID, Status: status, Version: latest.Version, PolicyID: c.PolicyID, PolicyVersion: c.PolicyVersion, SnapshotHash: c.SnapshotHash, ValidFrom: latest.ValidFrom, ValidUntil: latest.ValidUntil}, nil
}
