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
		v, err := security.ID()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	secretID, accountID := id(), id()
	if err := a.db.Create(&storage.SecretRecord{ID: secretID, Purpose: "test-only", KeyID: "test", Ciphertext: []byte{1}}).Error; err != nil {
		t.Fatal(err)
	}
	account := storage.PaymentAccount{ID: accountID, Provider: "stripe", Environment: "sandbox", MerchantRef: "acct_test", CredentialSecretID: secretID, Enabled: true}
	if a.db.Create(&account).Error != nil {
		t.Fatal("commerce fixtures failed")
	}
	gateway := newPaymentDouble(t)
	licensing := &licenseDouble{results: map[string]ports.EntitlementResult{}, requests: map[string]string{}, timeoutFirst: true}
	config := app.Config{PublicURL: "https://merchant.example", MasterKey: a.key, PaymentFactory: func(context.Context, storage.PaymentAccount) (ports.PaymentProvider, error) { return gateway, nil }, LicensingFactory: func(context.Context, storage.LicentivoConnection) (ports.LicensingProvider, error) {
		return licensing, nil
	}, LicensingSetupFactory: func(string) (app.LicentivoSetupProvider, error) {
		return licensing, nil
	}}
	service, err := app.New(ctx, a.db, config)
	if err != nil {
		t.Fatal(err)
	}
	a.handler = httpapi.Handler(httpapi.Options{App: service, PublicURL: config.PublicURL, SecureCookies: true})
	a.expect(a.request("POST", "/api/v1/admin/connections/licentivo", app.LicenseSettings{APIKey: "lv_api_test_launch_connection"}, admin), 200)
	var connection storage.LicentivoConnection
	if a.db.First(&connection).Error != nil {
		t.Fatal("Licentivo connection was not saved")
	}
	connectionID := connection.ID
	connections := data[map[string]json.RawMessage](t, a.request("GET", "/api/v1/admin/connections", nil, admin))
	var serviceURL string
	_ = json.Unmarshal(connections["licentivo_url"], &serviceURL)
	if serviceURL != app.LicentivoServiceURL {
		t.Fatalf("Licentivo did not use the official service URL: %q", serviceURL)
	}
	licensing.policyName = "专业版-测试"
	a.expect(a.request("POST", "/api/v1/admin/connections/licentivo", app.LicenseSettings{}, admin), 200)
	connections = data[map[string]json.RawMessage](t, a.request("GET", "/api/v1/admin/connections", nil, admin))
	var refreshed struct {
		Licensing []struct {
			Catalog struct {
				Policies struct {
					Items []struct {
						Name string `json:"name"`
					} `json:"items"`
				} `json:"policies"`
			} `json:"catalog"`
		} `json:"licensing"`
	}
	refreshedRaw, _ := json.Marshal(connections)
	if json.Unmarshal(refreshedRaw, &refreshed) != nil || len(refreshed.Licensing) != 1 || len(refreshed.Licensing[0].Catalog.Policies.Items) == 0 || refreshed.Licensing[0].Catalog.Policies.Items[0].Name != "专业版-测试" {
		t.Fatalf("Licentivo policy catalog was not refreshed with the saved API key: %s", refreshedRaw)
	}
	a.expect(a.request("POST", "/api/v1/admin/connections/mappings", app.MappingInput{VersionID: plan.VersionID, PolicyID: "22222222-2222-4222-8222-222222222222"}, admin), 200)
	versions := data[[]app.PlanVersionHistory](t, a.request("GET", "/api/v1/admin/plans/"+plan.ID+"/versions", nil, admin))
	if len(versions) == 0 || versions[0].RemoteProductName != "测试产品" || versions[0].RemotePolicyName != "专业版-测试" {
		t.Fatalf("plan history did not expose readable Licentivo names: %+v", versions)
	}
	if err := a.db.Model(&storage.PlanLicenseMapping{}).Where("plan_version_id = ?", plan.VersionID).Updates(map[string]any{"remote_product_name": "", "remote_policy_name": ""}).Error; err != nil {
		t.Fatal(err)
	}
	versions = data[[]app.PlanVersionHistory](t, a.request("GET", "/api/v1/admin/plans/"+plan.ID+"/versions", nil, admin))
	if len(versions) == 0 || versions[0].RemoteProductName != "测试产品" || versions[0].RemotePolicyName != "专业版-测试" {
		t.Fatalf("legacy mapping names were not resolved from the verified catalog: %+v", versions)
	}
	var mappedVersion storage.PlanVersion
	var mappedRights app.PlanSnapshot
	if a.db.First(&mappedVersion, "id = ?", plan.VersionID).Error != nil || json.Unmarshal(mappedVersion.EntitlementSnapshot, &mappedRights) != nil {
		t.Fatal("mapped plan snapshot was not saved")
	}
	if mappedRights.MaxDevices != 2 || mappedRights.LicenseModel != "node_locked" || mappedRights.HeartbeatSeconds != 600 || mappedRights.OfflineMode != "limited" || mappedRights.OfflineSeconds != 86400 || mappedRights.SessionSeconds != 1200 || mappedRights.SelfReleaseLimit != 2 || len(mappedRights.Features) != 1 || mappedRights.Features[0] != "export" {
		t.Fatalf("Licentivo policy was not copied into the plan snapshot: %+v", mappedRights)
	}
	a.expect(a.request("POST", "/api/v1/admin/plans/"+plan.ID+"/publish", map[string]string{"version_id": plan.VersionID, "environment": "sandbox"}, admin), 200)
	site := data[app.SiteView](t, a.request("GET", "/api/v1/site?environment=sandbox", nil, nil))
	if len(site.Plans) != 1 || site.Plans[0].PreviewOnly {
		t.Fatal("configured sandbox did not become purchasable")
	}
	a.expect(a.request("POST", "/api/v1/orders", app.CheckoutInput{ProductID: "00000000-0000-4000-8000-000000000099", PlanVersionID: plan.VersionID, Environment: "sandbox", Provider: "stripe", IdempotencyKey: "wrong-product-key-0001"}, customer), 409)
	input := app.CheckoutInput{ProductID: plan.ProductID, PlanVersionID: plan.VersionID, PurchasePeriods: 3, Environment: "sandbox", Provider: "stripe", IdempotencyKey: "test-purchase-key-0001"}
	w := a.request("POST", "/api/v1/orders", input, customer)
	a.expect(w, 200)
	order := data[app.CheckoutView](t, w)
	if order.CheckoutURL == nil {
		t.Fatal("checkout missing")
	}
	if order.PurchasePeriods != 3 || order.AmountMinor != plan.PurchaseOptions[1].AmountMinor {
		t.Fatalf("three-month checkout used the wrong server price: %+v", order)
	}
	w = a.request("POST", "/api/v1/orders", input, customer)
	a.expect(w, 200)
	replay := data[app.CheckoutView](t, w)
	if replay.ID != order.ID || gateway.creates != 1 {
		t.Fatal("purchase replay created a new checkout")
	}
	a.expect(a.request("POST", "/api/v1/orders", map[string]any{"product_id": plan.ProductID, "plan_version_id": plan.VersionID, "provider": "stripe", "environment": "sandbox", "idempotency_key": "invalid-price-key", "amount_minor": 1}, customer), 400)
	a.expect(a.request("GET", "/api/v1/orders/"+order.ID, nil, admin), 404)
	checkout := "cs_test_" + order.ID
	gateway.markPaid(checkout)
	body := map[string]any{"id": "evt_repeat_test", "type": "checkout.session.completed", "livemode": false, "created": time.Now().Unix(), "data": map[string]any{"object": map[string]string{"id": checkout}}}
	raw, _ := json.Marshal(body)
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(gateway.secret))
	mac.Write([]byte(stamp + "."))
	mac.Write(raw)
	signature := "t=" + stamp + ",v1=" + hex.EncodeToString(mac.Sum(nil))
	for i := 0; i < 10; i++ {
		a.expect(a.request("POST", "/api/v1/webhooks/payments/"+accountID, body, nil, func(r *http.Request) { r.Header.Set("Stripe-Signature", signature) }), 200)
	}
	if handled, err := service.ProcessPaymentEvent(ctx); err != nil || !handled {
		t.Fatalf("payment event: %v", err)
	}
	w = a.request("GET", "/api/v1/orders/"+order.ID, nil, customer)
	a.expect(w, 200)
	paid := data[app.CheckoutView](t, w)
	if paid.Status != "paid" || paid.Fulfillment != "pending_authorization" {
		t.Fatal("payment and fulfillment were not separated")
	}
	if handled, err := service.ProcessAuthorization(ctx); err != nil || !handled {
		t.Fatal("authorization task not attempted")
	}
	var task storage.SyncTask
	if a.db.First(&task, "source_order_id = ?", order.ID).Error != nil || task.Status != "retrying" {
		t.Fatal("ambiguous remote success was not retained")
	}
	if err := a.db.Model(&task).Update("available_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	service, err = app.New(ctx, a.db, config)
	if err != nil {
		t.Fatal(err)
	}
	if handled, err := service.ProcessAuthorization(ctx); err != nil || !handled {
		t.Fatalf("recovery failed: %v", err)
	}
	if licensing.creates != 1 || len(licensing.results) != 1 {
		t.Fatal("remote replay issued another license")
	}
	for model, want := range map[any]int64{&storage.PaymentEvent{}: 1, &storage.Payment{}: 1, &storage.SyncTask{}: 1, &storage.EntitlementCoverage{}: 1, &storage.LicenseBinding{}: 1} {
		var count int64
		if a.db.Model(model).Count(&count).Error != nil || count != want {
			t.Fatalf("duplicate ledger data %T: %d", model, count)
		}
	}
	for _, request := range licensing.requests {
		if strings.Contains(request, "@") || strings.Contains(request, "password") || strings.Contains(request, "amount") || strings.Contains(request, `"environment"`) {
			t.Fatal("customer or payment data sent to licensing")
		}
	}
	var product storage.Product
	if a.db.First(&product).Error != nil {
		t.Fatal("product missing")
	}
	a.expect(a.request("POST", "/api/v1/software/products/"+product.ID+"/credential", map[string]string{"environment": "sandbox"}, customer), 403)
	login := map[string]string{"email": credentials["email"], "password": credentials["password"], "kind": "desktop"}
	w = a.request("POST", "/api/v1/auth/login", login, nil, func(r *http.Request) { r.Header.Del("Origin") })
	a.expect(w, 200)
	grant := data[app.SessionGrant](t, w)
	w = a.request("POST", "/api/v1/software/products/"+product.ID+"/credential", map[string]string{"environment": "sandbox"}, nil, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+grant.AccessToken)
		r.Header.Del("Origin")
	})
	a.expect(w, 200)
	credential := data[map[string]any](t, w)
	if credential["license_key"] != "lv_lic_test_customer_only" {
		t.Fatal("own credential was not restored")
	}
	paid = data[app.CheckoutView](t, a.request("GET", "/api/v1/orders/"+order.ID, nil, customer))
	if paid.Fulfillment != "active" {
		t.Fatal("completed task not reflected in order")
	}
	englishHeader := func(r *http.Request) { r.Header.Set("Accept-Language", "en") }
	owned := data[[]app.ProductEntitlement](t, a.request("GET", "/api/v1/me/products", nil, customer, englishHeader))
	if len(owned) != 1 || owned[0].ProductID != product.ID || !strings.HasPrefix(owned[0].ProductName, "EN-US · ") {
		t.Fatalf("localized owned product was not served: %+v", owned)
	}
	ownedPlans := data[[]app.PlanView](t, a.request("GET", "/api/v1/me/products/"+product.ID+"/plans?environment=sandbox", nil, customer, englishHeader))
	if len(ownedPlans) != 1 || !strings.HasPrefix(ownedPlans[0].Name, "EN-US · ") {
		t.Fatalf("localized continuation plan was not served: %+v", ownedPlans)
	}
	testDeviceLifecycle(a, admin, customer, service, licensing, product.ID)
	testSalesLifecycle(a, admin, customer, plan, service, gateway, licensing)
	testStaffAndExports(a, admin, customer, credentials)
	testBackupRecovery(a, service, config)
	testLicenseEventLifecycle(a, service, gateway, licensing, connectionID, plan)
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
