package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"

	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"licentivo-launch/internal/adapters/externallicensing"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/security"
	"licentivo-launch/internal/storage"
)

type externalPeer struct {
	mu       sync.Mutex
	t        *testing.T
	key      []byte
	events   map[string]ports.ExternalSnapshot
	hashes   map[string]string
	latest   map[string]ports.ExternalSnapshot
	nonces   map[string]bool
	created  int
	dropNext bool
	badHash  bool
}

func (p *externalPeer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	raw, _ := io.ReadAll(r.Body)
	if !externallicensing.VerifyRequest(r, raw, p.key, "key-1", time.Now()) || p.nonces[r.Header.Get("X-Launch-Nonce")] {
		p.t.Error("invalid or replayed signed request")
		w.WriteHeader(401)
		return
	}
	p.nonces[r.Header.Get("X-Launch-Nonce")] = true
	var result any
	switch {
	case r.Method == "GET" && r.URL.Path == "/launch/v1/capabilities":
		result = ports.ExternalCapabilities{Protocol: ports.ExternalLicensingVersion, ConnectionRef: "merchant-sandbox", Environment: "sandbox", PurchaseTypes: []string{"perpetual", "fixed_term"}, DeliveryTypes: []string{"license_key", "instructions"}}
	case r.Method == "POST" && r.URL.Path == "/launch/v1/events":
		var snapshot ports.ExternalSnapshot
		if json.Unmarshal(raw, &snapshot) != nil || snapshot.Validate() != nil {
			p.t.Error("invalid snapshot")
			w.WriteHeader(422)
			return
		}
		sum := sha256.Sum256(raw)
		hash := hex.EncodeToString(sum[:])
		if saved, ok := p.hashes[snapshot.EventID]; ok && saved != hash {
			w.WriteHeader(409)
			return
		}
		if _, ok := p.events[snapshot.EventID]; !ok {
			p.created++
		}
		p.events[snapshot.EventID] = snapshot
		p.hashes[snapshot.EventID] = hash
		p.latest[snapshot.EntitlementRef] = snapshot
		if p.dropNext {
			p.dropNext = false
			connection, _, e := w.(http.Hijacker).Hijack()
			if e == nil {
				_ = connection.Close()
			}
			return
		}
		result = p.receipt(snapshot)
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/launch/v1/events/"):
		id := strings.TrimPrefix(r.URL.Path, "/launch/v1/events/")
		snapshot, ok := p.events[id]
		if !ok {
			w.WriteHeader(404)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "EVENT_NOT_FOUND", "message": "not found"}})
			return
		}
		result = p.receipt(snapshot)
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/launch/v1/entitlements/"):
		ref := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/launch/v1/entitlements/"), "/delivery")
		snapshot := p.latest[ref]
		result = ports.ExternalDelivery{ConnectionRef: snapshot.ConnectionRef, Environment: snapshot.Environment, EntitlementRef: ref, Version: snapshot.Version, Status: "ready", Delivery: &ports.ExternalDeliveryValue{Type: "license_key", Value: "MERCHANT-TEST-LICENSE"}}
	default:
		p.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(404)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": result, "request_id": "fixture"})
}
func (p *externalPeer) receipt(s ports.ExternalSnapshot) ports.ExternalReceipt {
	hash := p.hashes[s.EventID]
	if p.badHash {
		hash = strings.Repeat("0", 64)
	}
	return ports.ExternalReceipt{EventID: s.EventID, ConnectionRef: s.ConnectionRef, Environment: s.Environment, EntitlementRef: s.EntitlementRef, Version: s.Version, PayloadSHA256: hash, Status: "applied"}
}

func TestCustomLicensingLifecycle(t *testing.T) {
	dsn := os.Getenv("LAUNCH_TEST_EXTERNAL_DATABASE_DSN")
	if dsn == "" {
		t.Skip("set LAUNCH_TEST_EXTERNAL_DATABASE_DSN to an isolated empty launch_test_* database")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(cfg.DBName, "launch_test_") {
		t.Fatal("isolated launch_test_* database required")
	}
	db, err := storage.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close(db)
	var tables int64
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE()").Scan(&tables).Error; err != nil || tables != 0 {
		t.Fatal("empty external licensing test database required")
	}
	ctx := context.Background()
	if err := storage.Initialize(ctx, db); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{51}, 32)
	peer := &externalPeer{t: t, key: key, events: map[string]ports.ExternalSnapshot{}, hashes: map[string]string{}, latest: map[string]ports.ExternalSnapshot{}, nonces: map[string]bool{}}
	server := httptest.NewServer(peer)
	defer server.Close()
	payments := &purchasePayments{requests: map[string]ports.CheckoutRequest{}}
	config := Config{PublicURL: "https://merchant.example", MasterKey: bytes.Repeat([]byte{52}, 32), UploadDir: t.TempDir(), PaymentFactory: func(context.Context, storage.PaymentAccount) (ports.PaymentProvider, error) { return payments, nil }, BridgeFactory: func(string) (ports.LaunchBridge, error) {
		t.Fatal("custom flow attempted Licentivo bridge")
		return nil, nil
	}, LicensingFactory: func(context.Context, storage.LicentivoConnection) (ports.LicensingProvider, error) {
		t.Fatal("custom flow attempted Licentivo management")
		return nil, nil
	}}
	service, err := New(ctx, db, config)
	if err != nil {
		t.Fatal(err)
	}
	id := func() string {
		value, e := security.ID()
		if e != nil {
			t.Fatal(e)
		}
		return value
	}
	create := func(value any) {
		t.Helper()
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	checked := now()
	user := storage.User{ID: id(), Email: "custom@example.invalid", EmailKey: "custom@example.invalid", PasswordHash: "test-only", Status: "active", EmailVerifiedAt: &checked}
	create(&user)
	owner := storage.User{ID: id(), Email: "custom-owner@example.invalid", EmailKey: "custom-owner@example.invalid", PasswordHash: "test-only", Status: "active", EmailVerifiedAt: &checked}
	create(&owner)
	customer := Principal{User: user, Role: "customer"}
	admin := Principal{User: owner, Role: "admin"}
	if err := service.SaveLicensingProvider(ctx, customer, LicensingProviderInput{Provider: CustomLicensing}); err != ErrForbidden {
		t.Fatal("customer changed the global licensing provider", err)
	}
	if err := service.SaveLicensingProvider(ctx, admin, LicensingProviderInput{Provider: "unknown"}); err != ErrInvalid {
		t.Fatal("invalid global licensing provider accepted", err)
	}
	connection, err := service.SaveExternalConnection(ctx, admin, "", ExternalConnectionInput{Provider: CustomLicensing, Name: "Merchant licensing", BaseURL: server.URL, Environment: "sandbox", ConnectionRef: "merchant-sandbox", KeyID: "key-1", SigningKey: base64.StdEncoding.EncodeToString(key), AllowLocalHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	create(&storage.PaymentAccount{ID: id(), Provider: "stripe", Environment: "sandbox", MerchantRef: "test-merchant", CredentialSecretID: func() string {
		var c storage.ExternalLicensingConnection
		db.First(&c, "id = ?", connection.ID)
		return c.CredentialSecretID
	}(), Enabled: true})
	product, err := service.SaveProduct(ctx, admin, ProductView{Name: "External editor", Slug: "external-editor", Status: "published", Content: ProductContent{Features: []string{}}})
	if err != nil {
		t.Fatal(err)
	}
	unit := "month"
	count := uint64(1)
	makePlan := func(name string, amount int64) PlanView {
		t.Helper()
		plan, e := service.SavePlan(ctx, admin, PlanView{ProductID: product.ID, Name: name, Currency: "USD", AmountMinor: amount, PurchaseType: "fixed_term", TermUnit: &unit, TermCount: &count, PurchaseOptions: defaultPurchaseOptions(amount), AllowPurchase: true, AllowRenewal: true})
		if e != nil {
			t.Fatal(e)
		}
		return plan
	}
	basic, pro := makePlan("Basic", 1000), makePlan("Professional", 2000)
	binding := ProductLicensingInput{Provider: CustomLicensing, Environment: "sandbox", ConnectionID: connection.ID, Mappings: []ExternalMappingInput{{basic.VersionID, "editor", "basic-v1"}, {pro.VersionID, "editor", "pro-v1"}}}
	if err := service.SaveProductLicensing(ctx, admin, product.ID, binding); err == nil {
		t.Fatal("inactive custom licensing was bound to a product")
	}
	if err := service.SaveLicensingProvider(ctx, admin, LicensingProviderInput{Provider: CustomLicensing}); err != nil {
		t.Fatal(err)
	}
	for _, environment := range []string{"sandbox", "live"} {
		selected, err := service.productLicensing(db, product.ID, environment)
		if err != nil || selected.Provider != CustomLicensing {
			t.Fatal("global selection did not apply to both environments", selected, err)
		}
	}
	if err := service.SaveProductLicensing(ctx, admin, product.ID, binding); err != nil {
		t.Fatal(err)
	}
	for _, plan := range []PlanView{basic, pro} {
		if err := service.PublishPlan(ctx, admin, plan.ID, plan.VersionID, "sandbox"); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.SaveLicensingProvider(ctx, admin, LicensingProviderInput{Provider: "licentivo"}); err != nil {
		t.Fatal("switching without orders failed", err)
	}
	var activePublications int64
	if err := db.Model(&storage.PlanPublication{}).Where("preview_only = false").Count(&activePublications).Error; err != nil || activePublications != 0 {
		t.Fatal("switching providers left old plans for sale", err)
	}
	if err := service.SaveLicensingProvider(ctx, admin, LicensingProviderInput{Provider: CustomLicensing}); err != nil {
		t.Fatal(err)
	}
	for _, plan := range []PlanView{basic, pro} {
		if err := service.PublishPlan(ctx, admin, plan.ID, plan.VersionID, "sandbox"); err != nil {
			t.Fatal("saved custom mappings were lost when switching back", err)
		}
	}
	var licenses int64
	db.Model(&storage.LicentivoConnection{}).Count(&licenses)
	if licenses != 0 {
		t.Fatal("test accidentally configured Licentivo")
	}
	purchase := func() storage.Entitlement {
		t.Helper()
		var value storage.Entitlement
		if err := db.First(&value, "user_id = ? AND product_id = ? AND environment = 'sandbox'", user.ID, product.ID).Error; err != nil {
			t.Fatal(err)
		}
		return value
	}
	pay := func(plan PlanView, periods uint64, action, quote string) storage.Order {
		t.Helper()
		view, e := service.CreateOrder(ctx, customer, CheckoutInput{ProductID: product.ID, PlanVersionID: plan.VersionID, Action: action, QuoteToken: quote, PurchasePeriods: periods, Provider: "stripe", Environment: "sandbox", IdempotencyKey: id()})
		if e != nil {
			t.Fatal(e)
		}
		if e := service.reconcilePayment(ctx, view.ID); e != nil {
			t.Fatal(e)
		}
		var order storage.Order
		db.First(&order, "id = ?", view.ID)
		return order
	}
	finish := func() {
		t.Helper()
		if err := db.Model(&storage.SyncTask{}).Where("provider = ? AND status = 'retrying'", CustomLicensing).Update("available_at", now().Add(-time.Second)).Error; err != nil {
			t.Fatal(err)
		}
		handled, e := service.ProcessAuthorization(ctx)
		if e != nil || !handled {
			t.Fatalf("sync handled=%v error=%v", handled, e)
		}
		value := purchase()
		if value.AppliedVersion != value.DesiredVersion {
			t.Fatalf("not synchronized: %d/%d", value.AppliedVersion, value.DesiredVersion)
		}
	}
	baseOrder := pay(basic, 1, "purchase", "")
	if err := service.SaveLicensingProvider(ctx, admin, LicensingProviderInput{Provider: "licentivo"}); err == nil {
		t.Fatal("global provider switched despite existing custom orders")
	}
	if selected, err := service.activeLicensingProvider(db); err != nil || selected != CustomLicensing {
		t.Fatal("rejected switch changed the active provider", selected, err)
	}
	if peer.created != 0 {
		t.Fatal("authorization issued before queue processing")
	}
	peer.dropNext = true
	if handled, e := service.ProcessAuthorization(ctx); e != nil || !handled {
		t.Fatal(handled, e)
	}
	service, err = New(ctx, db, config)
	if err != nil {
		t.Fatal(err)
	}
	finish()
	if peer.created != 1 {
		t.Fatal("lost response created duplicate authorization")
	}
	if result, e := service.CustomerDelivery(ctx, customer, product.ID, "sandbox"); e != nil || result.Delivery == nil || result.Delivery.Value != "MERCHANT-TEST-LICENSE" {
		t.Fatal(result, e)
	}
	stranger := customer
	stranger.User.ID = id()
	if _, e := service.CustomerDelivery(ctx, stranger, product.ID, "sandbox"); e == nil {
		t.Fatal("cross-account delivery")
	}
	if _, e := service.CustomerDelivery(ctx, customer, product.ID, "live"); e == nil {
		t.Fatal("cross-environment delivery")
	}
	if _, e := service.Identity(ctx, customer, product.ID, "sandbox"); e == nil {
		t.Fatal("custom product entered Licentivo identity flow")
	}
	if _, e := service.Devices(ctx, customer, product.ID, "sandbox"); e == nil {
		t.Fatal("custom product entered Licentivo device flow")
	}
	if _, e := service.CheckIntegration(ctx, admin, IntegrationInput{ProductID: product.ID, Environment: "sandbox", Language: "go"}); e == nil {
		t.Fatal("custom product generated Licentivo SDK configuration")
	}
	if e := service.SaveProductLicensing(ctx, admin, product.ID, ProductLicensingInput{Provider: "licentivo", Environment: "sandbox"}); e == nil {
		t.Fatal("existing orders silently migrated")
	}
	quote, err := service.Quote(ctx, customer, QuoteInput{ProductID: product.ID, PlanVersionID: basic.VersionID, Environment: "sandbox", Action: "renew", PurchasePeriods: 3}, "en")
	if err != nil {
		t.Fatal(err)
	}
	renewal := pay(basic, 3, "renew", quote.Token)
	finish()
	afterRenewal := purchase()
	refund := func(order storage.Order) {
		t.Helper()
		request, e := service.RequestRefund(ctx, customer, order.ID, "merchant integration test")
		if e != nil {
			t.Fatal(e)
		}
		if e := service.ApproveRefund(ctx, admin, request.ID, "confirm merchant refund"); e != nil {
			t.Fatal(e)
		}
		if e := service.ProcessRefunds(ctx); e != nil {
			t.Fatal(e)
		}
		finish()
	}
	refund(renewal)
	if value := purchase(); value.ValidUntil == nil || afterRenewal.ValidUntil == nil || !value.ValidUntil.Before(*afterRenewal.ValidUntil) || value.Status != "active" {
		t.Fatal("renewal refund did not retain base coverage")
	}
	quote, err = service.Quote(ctx, customer, QuoteInput{ProductID: product.ID, PlanVersionID: pro.VersionID, Environment: "sandbox", Action: "upgrade"}, "en")
	if err != nil {
		t.Fatal(err)
	}
	upgrade := pay(pro, 1, "upgrade", quote.Token)
	finish()
	refund(upgrade)
	if e := service.SetUserStatus(ctx, admin, user.ID, "disabled", "test account disabled"); e != nil {
		t.Fatal(e)
	}
	finish()
	if purchase().Status != "suspended" {
		t.Fatal("disabled customer remained active")
	}
	if e := service.SetUserStatus(ctx, admin, user.ID, "active", "test account enabled"); e != nil {
		t.Fatal(e)
	}
	finish()
	if e := service.SetEntitlementHold(ctx, admin, purchase().ID, "merchant pause", true); e != nil {
		t.Fatal(e)
	}
	finish()
	if _, e := service.CustomerDelivery(ctx, customer, product.ID, "sandbox"); e == nil {
		t.Fatal("suspended delivery was exposed")
	}
	if e := service.SetEntitlementHold(ctx, admin, purchase().ID, "merchant resume", false); e != nil {
		t.Fatal(e)
	}
	finish()
	peer.badHash = true
	if e := service.SetEntitlementHold(ctx, admin, purchase().ID, "receipt mismatch", true); e != nil {
		t.Fatal(e)
	}
	if _, e := service.ProcessAuthorization(ctx); e != nil {
		t.Fatal(e)
	}
	var task storage.SyncTask
	db.Where("entitlement_id = ?", purchase().ID).Order("target_version DESC").First(&task)
	if task.Status != "manual_review" {
		t.Fatal("mismatched receipt accepted")
	}
	peer.badHash = false
	if e := service.RetryAuthorization(ctx, admin, task.OperationID, "receipt fixed at original service"); e != nil {
		t.Fatal(e)
	}
	finish()
	if e := service.SetEntitlementHold(ctx, admin, purchase().ID, "merchant resume", false); e != nil {
		t.Fatal(e)
	}
	finish()
	refund(baseOrder)
	if purchase().Status != "revoked" {
		t.Fatal("full refund failed to revoke purchase")
	}
	if _, e := service.CustomerDelivery(ctx, customer, product.ID, "sandbox"); e == nil {
		t.Fatal("refunded delivery exposed")
	}
	views, err := service.LicensingConnections(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(marshal(views)), base64.StdEncoding.EncodeToString(key)) {
		t.Fatal("signing key leaked")
	}
	var externalConnection storage.ExternalLicensingConnection
	if err := db.First(&externalConnection, "id = ?", connection.ID).Error; err != nil {
		t.Fatal(err)
	}
	create(&storage.LicentivoConnection{ID: id(), BaseURL: LicentivoServiceURL, CredentialSecretID: externalConnection.CredentialSecretID, Capabilities: json.RawMessage(`{}`), Catalog: json.RawMessage(`{}`)})
	if _, err := service.Connections(ctx, admin); err != nil {
		t.Fatal("inactive saved Licentivo connection affected custom licensing", err)
	}
	if _, err := service.OperationalReadiness(ctx, admin, "licentivo"); err != nil {
		t.Fatal("readiness did not check only the active licensing provider", err)
	}
	// A pre-selection backup should retain its custom provider on upgrade.
	if err := db.Model(&storage.InstanceSettings{}).Where("id = 1").Update("licensing_provider", "").Error; err != nil {
		t.Fatal(err)
	}
	if err := service.normalizeProviderSettings(ctx); err != nil {
		t.Fatal(err)
	}
	if selected, err := service.activeLicensingProvider(db); err != nil || selected != CustomLicensing {
		t.Fatal("legacy custom licensing selection was not preserved", selected, err)
	}
	if _, err := storage.MakeSnapshot(ctx, db, config.MasterKey, map[string]string{}); err != nil {
		t.Fatal("backup with external connections:", err)
	}
	t.Logf("custom licensing complete: %d applied events; Licentivo connections=%d; payment/refund HTTP calls use fixtures", peer.created, licenses)

}
