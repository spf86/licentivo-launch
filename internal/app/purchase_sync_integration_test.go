package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"licentivo-launch/internal/adapters/licentivo"
	"licentivo-launch/internal/domain"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/security"
	"licentivo-launch/internal/storage"
)

// Contract peer, not a licensing implementation: it persists the exact event
// bytes, reports asynchronous results and can lose an acceptance response.
type purchasePeer struct {
	t                         *testing.T
	mu                        sync.Mutex
	connections               []ports.LaunchConnection
	events                    map[string]ports.CommerceSnapshot
	bodies                    map[string][]byte
	states                    map[string]string
	deliveries                map[string]int
	loseResponse              bool
	revocations               map[string]ports.SessionRevocationReceipt
	revocationBodies          map[string][]byte
	revocationPosts           map[string]int
	revocationLoseEnvironment string
	revocationFailEnvironment string
	revocationMismatch        bool
}

func (p *purchasePeer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer lv_api_") {
		p.t.Error("management key missing")
	}
	var out any
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case len(parts) >= 6 && parts[5] == "session-revocations":
		if p.revocations == nil {
			p.revocations = map[string]ports.SessionRevocationReceipt{}
			p.revocationBodies = map[string][]byte{}
			p.revocationPosts = map[string]int{}
		}
		if r.Method == "GET" && len(parts) == 7 {
			value, ok := p.revocations[parts[4]+":"+parts[6]]
			if !ok {
				w.WriteHeader(404)
				return
			}
			out = value
		} else if r.Method == "POST" && len(parts) == 6 {
			var in ports.SessionRevocation
			decoder := json.NewDecoder(r.Body)
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&in); err != nil || in.Validate() != nil {
				p.t.Error("invalid revocation body", err)
				w.WriteHeader(400)
				return
			}
			if in.Environment == p.revocationFailEnvironment {
				w.WriteHeader(503)
				return
			}
			key := parts[4] + ":" + in.EventID
			body := marshal(in)
			if previous, ok := p.revocationBodies[key]; ok && !bytes.Equal(previous, body) {
				p.t.Error("revocation retry changed its body")
				w.WriteHeader(409)
				return
			}
			p.revocationPosts[key]++
			value, exists := p.revocations[key]
			if !exists {
				at := now()
				value = ports.SessionRevocationReceipt{ConnectionID: parts[4], SessionRevocation: in, Status: "succeeded", RevokedSessions: 1, RevokedCredentials: 2, ReleasedDevices: 1, CreatedAt: at, CompletedAt: at}
				if in.Scope == "account" {
					value.RevokedBefore = &at
				}
				p.revocations[key], p.revocationBodies[key] = value, body
			}
			if in.Environment == p.revocationLoseEnvironment {
				p.revocationLoseEnvironment = ""
				w.WriteHeader(503)
				return
			}
			if p.revocationMismatch {
				value.AccountRef = "wrong_account"
			}
			out = value
		} else {
			w.WriteHeader(405)
			return
		}
	case r.URL.Path == "/api/v1/launch/connections" && r.Method == "GET":
		out = map[string]any{"items": p.connections}
	case r.URL.Path == "/api/v1/launch/connections" && r.Method == "POST":
		var in ports.LaunchConnectionInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			p.t.Error(err)
		}
		id, _ := security.ID()
		remote := ports.LaunchConnection{ID: id, Issuer: in.Issuer, Audience: in.Audience, Environment: in.Environment, Enabled: in.Enabled, HeartbeatSeconds: 600, SessionSeconds: 86400}
		p.connections = append(p.connections, remote)
		out = remote
	case len(parts) == 7 && parts[5] == "plans" && r.Method == "PUT":
		var in ports.LaunchPlanMapping
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || !integrationUUIDPattern.MatchString(in.LaunchProductID) || in.PolicyVersion == 0 {
			p.t.Error("invalid plan mapping")
		}
		out = map[string]bool{"saved": true}
	case len(parts) == 6 && parts[5] == "events" && r.Method == "POST":
		var in ports.CommerceSnapshot
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&in); err != nil || in.Validate() != nil {
			p.t.Errorf("invalid or unexpected event fields: %v", err)
		}
		body := marshal(in)
		if old, exists := p.bodies[in.EventID]; exists && !bytes.Equal(old, body) {
			p.t.Error("retry changed the event body")
		}
		p.events[in.EventID] = in
		p.bodies[in.EventID] = body
		p.deliveries[in.EventID]++
		if p.states[in.EventID] == "" {
			p.states[in.EventID] = "pending"
		}
		if p.loseResponse {
			p.loseResponse = false
			w.WriteHeader(503)
			return
		}
		out = ports.LaunchEventStatus{ConnectionID: parts[4], EventID: in.EventID, Status: p.states[in.EventID]}
		w.WriteHeader(202)
	case len(parts) == 7 && parts[5] == "events" && r.Method == "GET":
		out = ports.LaunchEventStatus{ConnectionID: parts[4], EventID: parts[6], Status: p.states[parts[6]]}
	case len(parts) == 8 && parts[7] == "retry" && r.Method == "POST":
		p.states[parts[6]] = "pending"
		out = map[string]bool{"queued": true}
	default:
		p.t.Errorf("unexpected management request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected", 500)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": out})
}

type purchasePayments struct {
	ports.PaymentProvider
	requests       map[string]ports.CheckoutRequest
	reversals      map[string]ports.PaymentReversal
	refundRequests []ports.RefundRequest
	refundFailure  bool
	receipts       map[string]ports.Refund
	paidAt         *time.Time
}

func (p *purchasePayments) CreateCheckout(_ context.Context, in ports.CheckoutRequest) (ports.Checkout, error) {
	p.requests[in.OperationID] = in
	return ports.Checkout{ExternalID: in.OperationID, RedirectURL: "https://checkout.example/" + in.OperationID}, nil
}
func (p *purchasePayments) QueryPayment(_ context.Context, id string) (ports.Payment, error) {
	in := p.requests[id]
	at := now()
	if p.paidAt != nil {
		at = *p.paidAt
	}
	return ports.Payment{ExternalID: "paid-" + id, OrderID: in.OrderID, MerchantRef: "test-merchant", Environment: in.Environment, Amount: in.Amount, Status: ports.PaymentSucceeded, PaidAt: &at}, nil
}
func (p *purchasePayments) CapturePayment(ctx context.Context, id, _ string) (ports.Payment, error) {
	return p.QueryPayment(ctx, id)
}
func (p *purchasePayments) Refund(_ context.Context, in ports.RefundRequest) (ports.Refund, error) {
	p.refundRequests = append(p.refundRequests, in)
	if p.refundFailure {
		return ports.Refund{}, errors.New("temporary refund transport failure")
	}
	value := ports.Refund{ExternalID: "refund-" + in.OperationID, PaymentID: in.PaymentID, Amount: in.Amount, Status: ports.PaymentSucceeded}
	if p.receipts == nil {
		p.receipts = map[string]ports.Refund{}
	}
	p.receipts[value.ExternalID] = value
	return value, nil
}
func (p *purchasePayments) QueryRefund(_ context.Context, id string) (ports.Refund, error) {
	value, ok := p.receipts[id]
	if !ok {
		return ports.Refund{}, errors.New("refund not found")
	}
	return value, nil
}
func (p *purchasePayments) QueryPaymentReversal(_ context.Context, in ports.VerifiedEvent) (ports.PaymentReversal, error) {
	value, ok := p.reversals[in.ResourceID]
	if !ok {
		return ports.PaymentReversal{}, errors.New("dispute query unavailable")
	}
	return value, nil
}

type purchasePolicies struct {
	ports.LicensingProvider
	definitions map[string]ports.PolicyDefinition
}

func (p *purchasePolicies) PolicyDefinition(_ context.Context, id string) (ports.PolicyDefinition, error) {
	return p.definitions[id], nil
}

func TestPurchaseSnapshotBridge(t *testing.T) {
	dsn := os.Getenv("LAUNCH_TEST_BRIDGE_DATABASE_DSN")
	if dsn == "" {
		t.Skip("set LAUNCH_TEST_BRIDGE_DATABASE_DSN to a fresh launch_test_* database")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(cfg.DBName, "launch_test_") {
		t.Fatal("a dedicated launch_test_* database is required")
	}
	db, err := storage.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close(db)
	var tables int64
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE()").Scan(&tables).Error; err != nil || tables != 0 {
		t.Fatalf("fresh test database required: tables=%d, error=%v", tables, err)
	}
	ctx := context.Background()
	if err := storage.Initialize(ctx, db); err != nil {
		t.Fatal(err)
	}
	peer := &purchasePeer{t: t, events: map[string]ports.CommerceSnapshot{}, bodies: map[string][]byte{}, states: map[string]string{}, deliveries: map[string]int{}}
	server := httptest.NewServer(peer)
	defer server.Close()
	payments := &purchasePayments{requests: map[string]ports.CheckoutRequest{}}
	policies := &purchasePolicies{definitions: map[string]ports.PolicyDefinition{}}
	config := Config{PublicURL: "https://merchant.example", MasterKey: bytes.Repeat([]byte{17}, 32), UploadDir: t.TempDir(), BridgeFactory: func(key string) (ports.LaunchBridge, error) { return licentivo.NewBridgeClient(server.URL, key, true) }, PaymentFactory: func(context.Context, storage.PaymentAccount) (ports.PaymentProvider, error) { return payments, nil }}
	config.LicensingFactory = func(context.Context, storage.LicentivoConnection) (ports.LicensingProvider, error) {
		return policies, nil
	}
	service, err := New(ctx, db, config)
	if err != nil {
		t.Fatal(err)
	}
	id := func() string {
		v, err := security.ID()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	create := func(value any) {
		t.Helper()
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	checked := now()
	user := storage.User{ID: id(), Email: "customer@example.invalid", EmailKey: "customer@example.invalid", PasswordHash: "test-only", Status: "active", EmailVerifiedAt: &checked}
	create(&user)
	adminUser := storage.User{ID: id(), Email: "owner@example.invalid", EmailKey: "owner@example.invalid", PasswordHash: "test-only", Status: "active", EmailVerifiedAt: &checked}
	create(&adminUser)
	customer := Principal{User: user, Role: "customer"}
	admin := Principal{User: adminUser, Role: "admin"}
	var keyID string
	if err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		keyID, err = service.createSecret(tx, "licentivo", []byte("lv_api_"+strings.Repeat("a", 43)))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	contract := ports.LaunchBridgeVersion
	connection := storage.LicentivoConnection{ID: id(), BaseURL: LicentivoServiceURL, CredentialSecretID: keyID, ContractVersion: &contract, Capabilities: marshal(map[string]any{}), Catalog: marshal(map[string]any{})}
	create(&connection)
	if err := service.syncLaunchTrust(ctx, "lv_api_"+strings.Repeat("a", 43)); err != nil {
		t.Fatal(err)
	}
	account := storage.PaymentAccount{ID: id(), Provider: "stripe", Environment: "sandbox", MerchantRef: "test-merchant", CredentialSecretID: keyID, Enabled: true}
	create(&account)
	product := storage.Product{ID: id(), Name: "Purchase test", Slug: "purchase-test", Status: "published", Content: marshal(map[string]any{})}
	create(&product)
	unit := "month"
	count := uint64(1)
	remoteProduct := id()
	plan := func(name string, amount int64) storage.PlanVersion {
		p := storage.Plan{ID: id(), ProductID: product.ID, Name: name, Status: "published", AllowPurchase: true, AllowRenewal: true}
		create(&p)
		v := storage.PlanVersion{ID: id(), PlanID: p.ID, ProductID: product.ID, Version: 1, Currency: "USD", AmountMinor: amount, PurchaseType: "fixed_term", TermUnit: &unit, TermCount: &count, EntitlementSnapshot: marshal(map[string]any{}), SnapshotHash: securityDigest([]byte("display-only"))}
		if name == "Lifetime" {
			v.PurchaseType = "perpetual"
			v.TermUnit = nil
			v.TermCount = nil
		}
		create(&v)
		policyID := id()
		mode := "timed"
		if name == "Lifetime" {
			mode = "perpetual"
		}
		policies.definitions[policyID] = ports.PolicyDefinition{ID: policyID, ProductID: remoteProduct, Version: 1, Mode: mode}
		create(&storage.PlanLicenseMapping{PlanVersionID: v.ID, ConnectionID: connection.ID, RemoteProductID: remoteProduct, RemotePolicyID: policyID, RemotePolicyVersion: 1})
		create(&storage.PlanPublication{PlanID: p.ID, Environment: "sandbox", VersionID: v.ID, PublishedAt: now()})
		return v
	}
	basic, pro := plan("Basic", 1000), plan("Pro", 2000)
	purchase := func() storage.Entitlement {
		t.Helper()
		var value storage.Entitlement
		if err := db.First(&value, "user_id = ? AND product_id = ? AND environment = 'sandbox'", user.ID, product.ID).Error; err != nil {
			t.Fatal(err)
		}
		return value
	}
	pay := func(version storage.PlanVersion, periods uint64, action, quote string) storage.Order {
		t.Helper()
		view, err := service.CreateOrder(ctx, customer, CheckoutInput{Action: action, QuoteToken: quote, ProductID: product.ID, PlanVersionID: version.ID, PurchasePeriods: periods, Provider: "stripe", Environment: "sandbox", IdempotencyKey: id()})
		if err != nil {
			t.Fatal(err)
		}
		if err := service.reconcilePayment(ctx, view.ID); err != nil {
			t.Fatal(err)
		}
		var order storage.Order
		if err := db.First(&order, "id = ?", view.ID).Error; err != nil {
			t.Fatal(err)
		}
		return order
	}
	latest := func() storage.SyncTask {
		t.Helper()
		e := purchase()
		var task storage.SyncTask
		if err := db.First(&task, "entitlement_id = ? AND target_version = ?", e.ID, e.DesiredVersion).Error; err != nil {
			t.Fatal(err)
		}
		return task
	}
	due := func(ids ...string) {
		t.Helper()
		q := db.Model(&storage.SyncTask{}).Where("status = 'retrying'")
		if len(ids) > 0 {
			q = q.Where("operation_id IN ?", ids)
		}
		if err := q.Update("available_at", now().Add(-time.Second)).Error; err != nil {
			t.Fatal(err)
		}
	}
	finish := func() ports.CommerceSnapshot {
		t.Helper()
		task := latest()
		handled, err := service.ProcessAuthorization(ctx)
		if err != nil || !handled {
			t.Fatalf("delivery: %v %v", handled, err)
		}
		var stored storage.SyncTask
		db.First(&stored, "operation_id = ?", task.OperationID)
		if stored.Status == "succeeded" {
			t.Fatal("202 was mistaken for applied")
		}
		peer.mu.Lock()
		peer.states[task.OperationID] = "applied"
		snapshot := peer.events[task.OperationID]
		peer.mu.Unlock()
		due(task.OperationID)
		if handled, err := service.ProcessAuthorization(ctx); err != nil || !handled {
			t.Fatalf("confirmation: %v %v", handled, err)
		}
		if e := purchase(); e.AppliedVersion != e.DesiredVersion {
			t.Fatal("applied event did not confirm purchase version")
		}
		return snapshot
	}
	refund := func(order storage.Order) {
		t.Helper()
		r, err := service.RequestRefund(ctx, customer, order.ID, "test full refund")
		if err != nil {
			t.Fatal(err)
		}
		if err := service.ApproveRefund(ctx, admin, r.ID, "test approved"); err != nil {
			t.Fatal(err)
		}
		if handled, err := service.processRefund(ctx); err != nil || !handled {
			t.Fatalf("refund: %v %v", handled, err)
		}
	}

	run := func(name string, action func(*testing.T)) {
		parent := t
		if !parent.Run(name, func(current *testing.T) { t = current; defer func() { t = parent }(); action(current) }) {
			parent.FailNow()
		}
	}
	run("purchase loses response and recovers after restart", func(t *testing.T) {
		order := pay(basic, 3, "purchase", "")
		task := latest()
		peer.loseResponse = true
		if handled, err := service.ProcessAuthorization(ctx); err != nil || !handled {
			t.Fatalf("ambiguous delivery: %v %v", handled, err)
		}
		if purchase().AppliedVersion != 0 {
			t.Fatal("unconfirmed response allowed fulfillment")
		}
		service, err = New(ctx, db, config)
		if err != nil {
			t.Fatal(err)
		}
		due()
		snapshot := finish()
		want, err := (domain.Schedule{Anchor: *order.PaidAt, Unit: domain.Month, Interval: 1, Periods: 3}).End()
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Type != "entitlement.created" || !snapshot.ValidFrom.Equal(*order.PaidAt) || snapshot.ValidUntil == nil || !snapshot.ValidUntil.Equal(want) || snapshot.AccountRef != service.subject(user.ID) {
			t.Fatalf("incorrect absolute purchase: %+v", snapshot)
		}
		if peer.deliveries[task.OperationID] != 2 {
			t.Fatal("lost response was not retried with the original event")
		}
		if err := service.reconcilePayment(ctx, order.ID); err != nil {
			t.Fatal(err)
		}
		var coverages int64
		db.Model(&storage.EntitlementCoverage{}).Where("source_order_id = ?", order.ID).Count(&coverages)
		if coverages != 1 {
			t.Fatal("replayed payment duplicated coverage")
		}
	})
	run("upgrade and refund restore the paid base", func(t *testing.T) {
		quote, err := service.Quote(ctx, customer, QuoteInput{Action: "upgrade", ProductID: product.ID, PlanVersionID: pro.ID, Environment: "sandbox"}, "")
		if err != nil {
			t.Fatal(err)
		}
		if quote.AmountMinor != 3000 {
			t.Fatalf("three-period upgrade: got %d, want 3000", quote.AmountMinor)
		}
		order := pay(pro, 1, "upgrade", quote.Token)
		snapshot := finish()
		if snapshot.Type != "entitlement.upgraded" || snapshot.PlanRef != pro.ID {
			t.Fatal("upgrade did not select the purchased plan")
		}
		refund(order)
		snapshot = finish()
		if snapshot.Type == "entitlement.refunded" || snapshot.PlanRef != basic.ID || snapshot.Status() != "active" {
			t.Fatal("upgrade refund revoked the unrefunded base")
		}
	})
	run("renewal extends absolute end and refund removes only the extension", func(t *testing.T) {
		before := purchase()
		quote, err := service.Quote(ctx, customer, QuoteInput{Action: "renew", ProductID: product.ID, PlanVersionID: basic.ID, Environment: "sandbox", PurchasePeriods: 3}, "")
		if err != nil {
			t.Fatal(err)
		}
		if quote.AmountMinor != 3000 {
			t.Fatal("renewal quote was not calculated by Launch")
		}
		order := pay(basic, 3, "renew", quote.Token)
		snapshot := finish()
		if snapshot.Type != "entitlement.renewed" || !snapshot.ValidUntil.After(*before.ValidUntil) {
			t.Fatal("renewal did not extend the absolute end")
		}
		refund(order)
		snapshot = finish()
		if snapshot.Type == "entitlement.refunded" || !snapshot.ValidUntil.Equal(*before.ValidUntil) {
			t.Fatal("renewal refund removed the base purchase")
		}
	})
	run("full refund and repurchase use different references", func(t *testing.T) {
		old := purchase()
		var base storage.Order
		if err := db.First(&base, "user_id = ? AND action = 'purchase' AND status = 'paid'", user.ID).Error; err != nil {
			t.Fatal(err)
		}
		refund(base)
		snapshot := finish()
		if snapshot.Type != "entitlement.refunded" || snapshot.EntitlementRef != purchaseReference(old) {
			t.Fatal("full refund did not revoke the old purchase")
		}
		pay(basic, 1, "purchase", "")
		snapshot = finish()
		if snapshot.Type != "entitlement.created" || snapshot.EntitlementRef == purchaseReference(old) {
			t.Fatal("repurchase reused a revoked purchase reference")
		}
	})
	run("manual review is not reported as applied", func(t *testing.T) {
		quote, err := service.Quote(ctx, customer, QuoteInput{Action: "renew", ProductID: product.ID, PlanVersionID: basic.ID, Environment: "sandbox", PurchasePeriods: 1}, "")
		if err != nil {
			t.Fatal(err)
		}
		pay(basic, 1, "renew", quote.Token)
		task := latest()
		peer.mu.Lock()
		peer.states[task.OperationID] = "manual_review"
		peer.mu.Unlock()
		if _, err := service.ProcessAuthorization(ctx); err != nil {
			t.Fatal(err)
		}
		if purchase().AppliedVersion == purchase().DesiredVersion {
			t.Fatal("reviewed purchase was reported as synchronized")
		}
		if err := service.RetryAuthorization(ctx, admin, task.OperationID, "corrected remote mapping"); err != nil {
			t.Fatal(err)
		}
		finish()
	})
	run("outbox preserves one version per purchase under concurrency", func(t *testing.T) {
		// Both workers may observe pending work, but only one can own a lease.
		quote, err := service.Quote(ctx, customer, QuoteInput{Action: "renew", ProductID: product.ID, PlanVersionID: basic.ID, Environment: "sandbox", PurchasePeriods: 1}, "")
		if err != nil {
			t.Fatal(err)
		}
		pay(basic, 1, "renew", quote.Token)
		var wg sync.WaitGroup
		wg.Add(2)
		errs := make(chan error, 2)
		for i := 0; i < 2; i++ {
			go func() { defer wg.Done(); _, err := service.ProcessAuthorization(ctx); errs <- err }()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		task := latest()
		if peer.deliveries[task.OperationID] != 1 {
			t.Fatal("concurrent workers delivered the same lease twice")
		}
		peer.mu.Lock()
		peer.states[task.OperationID] = "applied"
		peer.mu.Unlock()
		due()
		if _, err := service.ProcessAuthorization(ctx); err != nil {
			t.Fatal(err)
		}
	})
	run("expiry sends an explicit absolute state", func(t *testing.T) {
		e := purchase()
		at := e.ValidUntil.Add(time.Second)
		if err := db.Transaction(func(tx *gorm.DB) error { return service.recomputePurchaseAt(tx, &e, nil, false, at) }); err != nil {
			t.Fatal(err)
		}
		snapshot := finish()
		if snapshot.Type != "entitlement.expired" || !snapshot.ValidUntil.Equal(*e.ValidUntil) {
			t.Fatal("expiry lost the original purchased end")
		}
	})
	run("perpetual purchases retain a null end", func(t *testing.T) {
		product = storage.Product{ID: id(), Name: "Lifetime test", Slug: "lifetime-test", Status: "published", Content: marshal(map[string]any{})}
		create(&product)
		lifetime := plan("Lifetime", 5000)
		var mapping storage.PlanLicenseMapping
		if err := db.First(&mapping, "plan_version_id = ?", lifetime.ID).Error; err != nil {
			t.Fatal(err)
		}
		savedPolicy := policies.definitions[mapping.RemotePolicyID]
		wrong := savedPolicy
		wrong.Mode = "timed"
		policies.definitions[mapping.RemotePolicyID] = wrong
		before := len(payments.requests)
		_, err := service.CreateOrder(ctx, customer, CheckoutInput{ProductID: product.ID, PlanVersionID: lifetime.ID, Provider: "stripe", Environment: "sandbox", IdempotencyKey: id()})
		failure, ok := err.(*Error)
		if !ok || failure.Code != "PURCHASE_POLICY_MODE_MISMATCH" || len(payments.requests) != before {
			t.Fatal("incompatible expiry accepted payment", err)
		}
		policies.definitions[mapping.RemotePolicyID] = savedPolicy
		pay(lifetime, 1, "purchase", "")
		snapshot := finish()
		if snapshot.ValidUntil != nil || snapshot.Type != "entitlement.created" {
			t.Fatal("perpetual purchase acquired an invented expiry")
		}
	})
	run("legacy migration never reads or returns the old license key", func(t *testing.T) {
		product = storage.Product{ID: id(), Name: "Legacy test", Slug: "legacy-test", Status: "published", Content: marshal(map[string]any{})}
		create(&product)
		legacy := plan("Legacy", 1000)
		pay(legacy, 1, "purchase", "")
		e := purchase()
		oldTask := latest()
		// Represent an already issued pre-account purchase. The old secret is
		// deliberately unreadable: migration must only send its public license ID.
		badSecret := storage.SecretRecord{ID: id(), Purpose: "legacy-license", KeyID: "old", Ciphertext: []byte{1}}
		create(&badSecret)
		oldLicense := id()
		create(&storage.LicenseBinding{ID: id(), EntitlementID: e.ID, ConnectionID: connection.ID, CustomerRef: *e.CustomerRef, RemoteLicenseID: oldLicense, CredentialSecretID: badSecret.ID})
		if err := db.Model(&storage.SyncTask{}).Where("operation_id = ?", oldTask.OperationID).Updates(map[string]any{"launch_connection_id": nil, "status": "succeeded"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&e).Updates(map[string]any{"launch_purchase_ref": nil, "applied_version": e.DesiredVersion, "status": "active"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := service.queueLaunchMigrations(ctx); err != nil {
			t.Fatal(err)
		}
		task := latest()
		if _, err := service.ProcessAuthorization(ctx); err != nil {
			t.Fatal(err)
		}
		peer.mu.Lock()
		snapshot := peer.events[task.OperationID]
		peer.states[task.OperationID] = "applied"
		peer.mu.Unlock()
		if snapshot.Type != "entitlement.migrated" || snapshot.LegacyLicenseID != oldLicense {
			t.Fatal("legacy binding was not explicitly migrated")
		}
		due()
		if _, err := service.ProcessAuthorization(ctx); err != nil {
			t.Fatal(err)
		}
		if purchase().AppliedVersion == purchase().DesiredVersion {
			t.Fatal("migration skipped the separate current-purchase snapshot")
		}
		snapshot = finish()
		if snapshot.LegacyLicenseID != "" || snapshot.Type == "entitlement.migrated" {
			t.Fatal("current state attempted to remigrate the license")
		}
	})
	run("refund supersedes an unresolved older delivery", func(t *testing.T) {
		product = storage.Product{ID: id(), Name: "Refund priority", Slug: "refund-priority", Status: "published", Content: marshal(map[string]any{})}
		create(&product)
		version := plan("Refund base", 1000)
		order := pay(version, 1, "purchase", "")
		older := latest()
		peer.mu.Lock()
		peer.loseResponse = true
		peer.mu.Unlock()
		if handled, err := service.ProcessAuthorization(ctx); err != nil || !handled {
			t.Fatal("older delivery was not attempted", err)
		}
		if err := db.Model(&storage.SyncTask{}).Where("operation_id = ?", older.OperationID).Update("available_at", now().Add(time.Hour)).Error; err != nil {
			t.Fatal(err)
		}
		refund(order)
		snapshot := finish()
		if snapshot.Type != "entitlement.refunded" {
			t.Fatal("refund was blocked by earlier delivery")
		}
		var superseded storage.SyncTask
		if err := db.First(&superseded, "operation_id = ?", older.OperationID).Error; err != nil {
			t.Fatal(err)
		}
		if superseded.Status != "succeeded" || superseded.LastErrorCode == nil || *superseded.LastErrorCode != "PURCHASE_SNAPSHOT_SUPERSEDED" {
			t.Fatal("obsolete task still blocks future purchases")
		}
	})
	// Financial reversals operate on the commercial coverage. The bridge peer
	// observes the resulting complete snapshot, without making permission decisions.
	newProduct := func(name string) storage.PlanVersion {
		product = storage.Product{ID: id(), Name: name, Slug: strings.ToLower(strings.ReplaceAll(name, " ", "-")), Status: "published", Content: marshal(map[string]any{})}
		create(&product)
		return plan(name, 1000)
	}
	refundEvent := func(receipt string) storage.PaymentEvent {
		external := id()
		event := storage.PaymentEvent{ID: id(), AccountID: account.ID, ExternalEventID: external, Kind: "refund.updated", BodyHash: bytes.Repeat([]byte{1}, 32), BodyCiphertext: []byte{1}, VerifiedPayload: marshal(ports.VerifiedEvent{ID: external, Kind: "refund.updated", ResourceID: receipt, Environment: ports.Sandbox}), Status: "pending", VerifiedAt: now(), AvailableAt: now().Add(-time.Second)}
		create(&event)
		return event
	}
	run("refund notification arriving before payment settlement is retried", func(t *testing.T) {
		version := newProduct("Early refund notification")
		view, err := service.CreateOrder(ctx, customer, CheckoutInput{ProductID: product.ID, PlanVersionID: version.ID, Provider: "stripe", Environment: "sandbox", IdempotencyKey: id()})
		if err != nil {
			t.Fatal(err)
		}
		var payment storage.Payment
		if err := db.First(&payment, "order_id = ?", view.ID).Error; err != nil {
			t.Fatal(err)
		}
		if payments.receipts == nil {
			payments.receipts = map[string]ports.Refund{}
		}
		receipt := "refund-early-" + id()
		payments.receipts[receipt] = ports.Refund{ExternalID: receipt, PaymentID: "paid-" + payment.OperationID, Amount: domain.Money{Currency: payment.Currency, Minor: payment.AmountMinor}, Status: ports.PaymentSucceeded}
		event := refundEvent(receipt)
		handled, err := service.ProcessPaymentEvent(ctx)
		var pending *Error
		if !handled || !errors.As(err, &pending) || pending.Code != "REFUND_EVENT_PENDING" {
			t.Fatal("early refund notification was not retained for retry", handled, err)
		}
		if err := db.First(&event, "id = ?", event.ID).Error; err != nil || event.Status != "pending" {
			t.Fatal("early refund notification left the automatic queue", event.Status, err)
		}
		if err := service.reconcilePayment(ctx, view.ID); err != nil {
			t.Fatal(err)
		}
		finish()
		if err := db.Model(&event).Update("available_at", now().Add(-time.Second)).Error; err != nil {
			t.Fatal(err)
		}
		if handled, err := service.ProcessPaymentEvent(ctx); !handled || err != nil {
			t.Fatal("refund notification did not recover after settlement", handled, err)
		}
		posts := len(payments.refundRequests)
		if handled, err := service.processRefund(ctx); !handled || err != nil {
			t.Fatal("verified refund did not settle", handled, err)
		}
		var order storage.Order
		if err := db.First(&order, "id = ?", view.ID).Error; err != nil || order.Status != "refunded" {
			t.Fatal("early refund left the settled order paid", order.Status, err)
		}
		if len(payments.refundRequests) != posts || finish().Type != "entitlement.refunded" {
			t.Fatal("refund recovery moved money again or retained the refunded coverage")
		}
	})
	run("healthy pending refunds do not exhaust failure retries or rewrite approval time", func(t *testing.T) {
		order := pay(newProduct("Pending refund recovery"), 1, "purchase", "")
		finish()
		requested, err := service.RequestRefund(ctx, customer, order.ID, "pending refund regression")
		if err != nil {
			t.Fatal(err)
		}
		if err := service.ApproveRefund(ctx, admin, requested.ID, "test approval"); err != nil {
			t.Fatal(err)
		}
		var row storage.Refund
		var payment storage.Payment
		if err := db.First(&row, "id = ?", requested.ID).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.First(&payment, "order_id = ?", order.ID).Error; err != nil {
			t.Fatal(err)
		}
		receipt := "refund-pending-" + id()
		observed := ports.Refund{ExternalID: receipt, PaymentID: *payment.ExternalTransactionID, Amount: domain.Money{Currency: payment.Currency, Minor: row.AmountMinor}, Status: ports.PaymentPending}
		payments.receipts[receipt] = observed
		approved := now().Add(-25 * time.Hour)
		if err := db.Model(&row).Updates(map[string]any{"approved_at": approved, "external_refund_id": receipt, "attempts": 20, "next_check_at": now().Add(-time.Second)}).Error; err != nil {
			t.Fatal(err)
		}
		if handled, err := service.processRefund(ctx); !handled || err != nil {
			t.Fatal("pending refund polling failed", handled, err)
		}
		delete(payments.receipts, receipt)
		if err := db.Model(&row).Update("next_check_at", now().Add(-time.Second)).Error; err != nil {
			t.Fatal(err)
		}
		if handled, err := service.processRefund(ctx); !handled || err != nil {
			t.Fatal("temporary refund query outage", handled, err)
		}
		if err := db.First(&row, "id = ?", row.ID).Error; err != nil || row.ReviewRequired || row.Attempts != 1 {
			t.Fatal("healthy polling consumed the failure retry budget", row.ReviewRequired, row.Attempts, err)
		}
		payments.receipts[receipt] = observed
		refundEvent(receipt)
		if handled, err := service.ProcessPaymentEvent(ctx); !handled || err != nil {
			t.Fatal("verified pending refund notification", handled, err)
		}
		if err := db.First(&row, "id = ?", row.ID).Error; err != nil || row.ApprovedAt == nil || !row.ApprovedAt.Equal(approved) {
			t.Fatal("refund notification rewrote the original approval time", row.ApprovedAt, err)
		}
		observed.Status = ports.PaymentSucceeded
		payments.receipts[receipt] = observed
		posts := len(payments.refundRequests)
		if handled, err := service.processRefund(ctx); !handled || err != nil {
			t.Fatal("pending refund did not recover automatically", handled, err)
		}
		if len(payments.refundRequests) != posts || finish().Type != "entitlement.refunded" {
			t.Fatal("refund polling moved money again or failed to remove coverage")
		}
	})
	reverse := func(order storage.Order, confirmed bool, adjust func(*ports.PaymentReversal)) storage.PaymentEvent {
		t.Helper()
		var payment storage.Payment
		if err := db.First(&payment, "order_id = ?", order.ID).Error; err != nil {
			t.Fatal(err)
		}
		external := "dp-" + order.ID
		at := now()
		if payment.PaidAt != nil {
			at = *payment.PaidAt
		}
		observed := ports.PaymentReversal{ExternalID: external, PaymentID: "paid-" + payment.OperationID, OrderID: order.ID, MerchantRef: account.MerchantRef, Environment: ports.Sandbox, Amount: domain.Money{Currency: payment.Currency, Minor: payment.AmountMinor}, OriginalAmount: domain.Money{Currency: payment.Currency, Minor: payment.AmountMinor}, PaidAt: &at, Confirmed: confirmed}
		if adjust != nil {
			adjust(&observed)
		}
		if payments.reversals == nil {
			payments.reversals = map[string]ports.PaymentReversal{}
		}
		payments.reversals[external] = observed
		event := storage.PaymentEvent{ID: id(), AccountID: account.ID, ExternalEventID: id(), Kind: "charge.dispute.closed", BodyHash: bytes.Repeat([]byte{1}, 32), BodyCiphertext: []byte{1}, VerifiedPayload: marshal(ports.VerifiedEvent{ID: id(), Kind: "charge.dispute.closed", ResourceID: external, Environment: ports.Sandbox}), Status: "pending", VerifiedAt: now(), AvailableAt: now().Add(-time.Second)}
		create(&event)
		if handled, err := service.ProcessPaymentEvent(ctx); !handled || err != nil {
			t.Fatal("reversal worker", handled, err)
		}
		if err := db.First(&event, "id = ?", event.ID).Error; err != nil {
			t.Fatal(err)
		}
		return event
	}
	run("lost renewal dispute preserves base and canonical reversal is idempotent", func(t *testing.T) {
		version := newProduct("Dispute renewal")
		pay(version, 1, "purchase", "")
		finish()
		base := purchase()
		quote, err := service.Quote(ctx, customer, QuoteInput{Action: "renew", ProductID: product.ID, PlanVersionID: version.ID, Environment: "sandbox", PurchasePeriods: 1}, "")
		if err != nil {
			t.Fatal(err)
		}
		order := pay(version, 1, "renew", quote.Token)
		finish()
		if event := reverse(order, true, nil); event.Status != "processed" {
			t.Fatal("lost dispute not processed")
		}
		snapshot := finish()
		if snapshot.Type == "entitlement.refunded" || snapshot.ValidUntil == nil || !snapshot.ValidUntil.Equal(*base.ValidUntil) {
			t.Fatal("dispute removed the unchallenged base purchase")
		}
		versionBefore := purchase().DesiredVersion
		reverse(order, true, nil)
		if purchase().DesiredVersion != versionBefore {
			t.Fatal("distinct webhook redelivery duplicated canonical reversal")
		}
		view, err := service.Order(ctx, customer, order.ID)
		if err != nil || view.Status != "reversed" || view.Fulfillment != "reversed" {
			t.Fatal("reversed order shown as active", view, err)
		}
	})
	run("pending and mismatched disputes cannot remove coverage", func(t *testing.T) {
		version := newProduct("Dispute checks")
		order := pay(version, 1, "purchase", "")
		finish()
		before := purchase()
		if reverse(order, false, nil).Status != "processed" {
			t.Fatal("pending dispute not recorded")
		}
		for _, change := range []func(*ports.PaymentReversal){func(v *ports.PaymentReversal) { v.MerchantRef = "foreign" }, func(v *ports.PaymentReversal) { v.Environment = ports.Live }, func(v *ports.PaymentReversal) { v.Amount.Currency = "EUR" }, func(v *ports.PaymentReversal) { v.OriginalAmount.Minor++ }, func(v *ports.PaymentReversal) { v.OrderID = id() }} {
			if reverse(order, true, change).Status != "review" {
				t.Fatal("invalid dispute bypassed review")
			}
			if purchase().DesiredVersion != before.DesiredVersion {
				t.Fatal("unverified reversal changed coverage")
			}
		}
		if reverse(order, true, nil).Status != "processed" {
			t.Fatal("confirmed dispute did not settle")
		}
		if finish().Type != "entitlement.refunded" {
			t.Fatal("full reversal did not send purchase termination")
		}
	})
	run("reversal before capture callback never issues positive coverage", func(t *testing.T) {
		version := newProduct("Dispute before callback")
		view, err := service.CreateOrder(ctx, customer, CheckoutInput{ProductID: product.ID, PlanVersionID: version.ID, Provider: "stripe", Environment: "sandbox", IdempotencyKey: id()})
		if err != nil {
			t.Fatal(err)
		}
		var order storage.Order
		if err := db.First(&order, "id = ?", view.ID).Error; err != nil {
			t.Fatal(err)
		}
		if reverse(order, true, nil).Status != "processed" {
			t.Fatal("out of order reversal failed")
		}
		if err := service.reconcilePayment(ctx, order.ID); err != nil {
			t.Fatal(err)
		}
		var coverage, tasks int64
		db.Model(&storage.EntitlementCoverage{}).Where("source_order_id = ?", order.ID).Count(&coverage)
		db.Model(&storage.SyncTask{}).Where("entitlement_id = ?", purchase().ID).Count(&tasks)
		if coverage != 0 || tasks != 0 || purchase().Status != "revoked" {
			t.Fatal("late payment revived the reversed purchase", coverage, tasks)
		}
	})
	requestUnknown := func(version storage.PlanVersion) storage.Refund {
		t.Helper()
		order := pay(version, 1, "purchase", "")
		finish()
		requested, err := service.RequestRefund(ctx, customer, order.ID, "test refund recovery")
		if err != nil {
			t.Fatal(err)
		}
		var row storage.Refund
		if err := db.First(&row, "id = ?", requested.ID).Error; err != nil {
			t.Fatal(err)
		}
		if err := service.ApproveRefund(ctx, admin, row.ID, "test approval"); err != nil {
			t.Fatal(err)
		}
		payments.refundFailure = true
		for i := 0; i < 12; i++ {
			if err := db.Model(&storage.Refund{}).Where("id = ?", row.ID).Update("next_check_at", now().Add(-time.Second)).Error; err != nil {
				t.Fatal(err)
			}
			if handled, err := service.processRefund(ctx); err != nil || !handled {
				t.Fatal("unknown refund", handled, err)
			}
		}
		payments.refundFailure = false
		if err := db.First(&row, "id = ?", row.ID).Error; err != nil {
			t.Fatal(err)
		}
		if row.Status != "unknown" || !row.ReviewRequired || row.Attempts != 12 {
			t.Fatal("unknown refund did not reach review")
		}
		return row
	}
	run("admin can resume exhausted refund with original idempotent request", func(t *testing.T) {
		row := requestUnknown(newProduct("Refund recovery"))
		if err := service.ReconcileRefund(ctx, customer, row.ID, RefundRecoveryInput{Reason: "not admin"}); err != ErrForbidden {
			t.Fatal("customer recovered refund", err)
		}
		if err := service.ReconcileRefund(ctx, admin, row.ID, RefundRecoveryInput{Reason: "provider recovered"}); err != nil {
			t.Fatal(err)
		}
		if handled, err := service.processRefund(ctx); !handled || err != nil {
			t.Fatal("refund did not recover", err)
		}
		var result storage.Refund
		db.First(&result, "id = ?", row.ID)
		if result.Status != "succeeded" || result.OperationID != row.OperationID || !result.ApprovedAt.Equal(*row.ApprovedAt) {
			t.Fatal("recovery created a replacement refund")
		}
		for _, in := range payments.refundRequests {
			if in.PaymentID == payments.refundRequests[len(payments.refundRequests)-1].PaymentID && (in.OperationID != row.OperationID || !in.RequestedAt.Equal(*row.ApprovedAt)) {
				t.Fatal("replay changed original payload")
			}
		}
		if finish().Type != "entitlement.refunded" {
			t.Fatal("recovered refund not synchronized")
		}
	})
	run("expired refund recovery requires verified original receipt and performs no POST", func(t *testing.T) {
		row := requestUnknown(newProduct("Refund expired recovery"))
		old := now().Add(-25 * time.Hour)
		if err := db.Model(&storage.Refund{}).Where("id = ?", row.ID).Update("approved_at", old).Error; err != nil {
			t.Fatal(err)
		}
		calls := len(payments.refundRequests)
		if err := service.ReconcileRefund(ctx, admin, row.ID, RefundRecoveryInput{Reason: "provider recovered"}); err == nil {
			t.Fatal("expired operation blindly queued")
		}
		var payment storage.Payment
		db.First(&payment, "id = ?", row.PaymentID)
		payments.receipts["original-receipt"] = ports.Refund{ExternalID: "original-receipt", PaymentID: "foreign", Amount: domain.Money{Currency: payment.Currency, Minor: row.AmountMinor}, Status: ports.PaymentSucceeded}
		if err := service.ReconcileRefund(ctx, admin, row.ID, RefundRecoveryInput{Reason: "manual receipt", ExternalRefundID: "original-receipt"}); err == nil {
			t.Fatal("foreign receipt accepted")
		}
		receipt := payments.receipts["original-receipt"]
		receipt.PaymentID = *payment.ExternalTransactionID
		payments.receipts["original-receipt"] = receipt
		if err := service.ReconcileRefund(ctx, admin, row.ID, RefundRecoveryInput{Reason: "manual receipt verified", ExternalRefundID: "original-receipt"}); err != nil {
			t.Fatal(err)
		}
		if handled, err := service.processRefund(ctx); !handled || err != nil {
			t.Fatal("receipt recovery failed", err)
		}
		if len(payments.refundRequests) != calls {
			t.Fatal("receipt recovery created new outbound refund")
		}
		var result storage.Refund
		db.First(&result, "id = ?", row.ID)
		if result.Status != "succeeded" || result.OperationID != row.OperationID {
			t.Fatal("original operation not settled")
		}
		finish()
	})
	for _, action := range []string{"upgrade", "renew"} {
		run("reviewed "+action+" cannot block the refund snapshot", func(t *testing.T) {
			version := newProduct("Review refund " + action)
			pay(version, 1, "purchase", "")
			finish()
			before := purchase()
			target, periods := version, uint64(3)
			if action == "upgrade" {
				target, periods = plan("Higher", 2000), 1
			}
			quote, err := service.Quote(ctx, customer, QuoteInput{Action: action, ProductID: product.ID, PlanVersionID: target.ID, Environment: "sandbox", PurchasePeriods: periods}, "")
			if err != nil {
				t.Fatal(err)
			}
			order := pay(target, periods, action, quote.Token)
			older := latest()
			// The peer applied the positive change, but all twelve responses were lost.
			for i := 0; i < 12; i++ {
				peer.mu.Lock()
				peer.states[older.OperationID], peer.loseResponse = "applied", true
				peer.mu.Unlock()
				due(older.OperationID)
				if handled, err := service.ProcessAuthorization(ctx); err != nil || !handled {
					t.Fatal("lost response retry", handled, err)
				}
			}
			refund(order)
			newer := latest()
			if handled, err := service.ProcessAuthorization(ctx); err != nil || !handled {
				t.Fatal("refund was blocked by the reviewed positive task", handled, err)
			}
			var pending storage.SyncTask
			if err := db.First(&pending, "operation_id = ?", older.OperationID).Error; err != nil || pending.Status != "manual_review" {
				t.Fatal("older task was superseded before the new snapshot was applied", pending.Status, err)
			}
			peer.mu.Lock()
			snapshot := peer.events[newer.OperationID]
			peer.states[newer.OperationID] = "applied"
			peer.mu.Unlock()
			if snapshot.Status() != "active" || snapshot.PlanRef != version.ID || !sameTime(snapshot.ValidUntil, before.ValidUntil) {
				t.Fatal("refund did not restore the exact remaining base", snapshot)
			}
			due(newer.OperationID)
			if handled, err := service.ProcessAuthorization(ctx); err != nil || !handled {
				t.Fatal("refund confirmation", handled, err)
			}
			if err := db.First(&pending, "operation_id = ?", older.OperationID).Error; err != nil || pending.Status != "succeeded" || pending.LastErrorCode == nil || *pending.LastErrorCode != "PURCHASE_SNAPSHOT_SUPERSEDED" {
				t.Fatal("confirmed refund did not supersede the older task", pending, err)
			}
		})
	}
	settleAll := func() {
		t.Helper()
		for range 20 {
			var tasks []storage.SyncTask
			if err := db.Where("status <> 'succeeded' AND launch_connection_id IS NOT NULL").Find(&tasks).Error; err != nil {
				t.Fatal(err)
			}
			peer.mu.Lock()
			for _, task := range tasks {
				peer.states[task.OperationID] = "applied"
			}
			peer.mu.Unlock()
			due()
			if handled, err := service.ProcessAuthorization(ctx); err != nil {
				t.Fatal(err)
			} else if !handled {
				break
			}
		}
		if e := purchase(); e.AppliedVersion != e.DesiredVersion {
			t.Fatal("purchase did not settle", e.AppliedVersion, e.DesiredVersion)
		}
	}
	run("refund still waits for an unresolved legacy migration", func(t *testing.T) {
		version := newProduct("Refund migration gate")
		order := pay(version, 1, "purchase", "")
		finish()
		e := purchase()
		secret := storage.SecretRecord{ID: id(), Purpose: "legacy-license", KeyID: "old", Ciphertext: []byte{1}}
		create(&secret)
		create(&storage.LicenseBinding{ID: id(), EntitlementID: e.ID, ConnectionID: connection.ID, CustomerRef: *e.CustomerRef, RemoteLicenseID: id(), CredentialSecretID: secret.ID})
		if err := db.Model(&e).Update("launch_purchase_ref", nil).Error; err != nil {
			t.Fatal(err)
		}
		if err := service.queueLaunchMigrations(ctx); err != nil {
			t.Fatal(err)
		}
		migration := latest()
		if err := db.Model(&migration).Update("status", "manual_review").Error; err != nil {
			t.Fatal(err)
		}
		refund(order)
		if handled, err := service.ProcessAuthorization(ctx); err != nil || handled {
			t.Fatal("refund bypassed unresolved migration", handled, err)
		}
		if err := db.Model(&migration).Update("status", "pending").Error; err != nil {
			t.Fatal(err)
		}
		settleAll()
		if err := db.First(&migration, "operation_id = ?", migration.OperationID).Error; err != nil || migration.Status != "succeeded" || migration.LastErrorCode != nil {
			t.Fatal("migration was superseded instead of applied", migration, err)
		}
		settleAll() // Migration queues a separate current-state snapshot.
	})
	run("discounted consecutive upgrades use full three-period price snapshots", func(t *testing.T) {
		base := newProduct("Discounted upgrades")
		pro, highest := plan("Discounted pro", 2000), plan("Discounted highest", 3000)
		for _, price := range []struct {
			version storage.PlanVersion
			amount  int64
		}{{base, 2700}, {pro, 5100}, {highest, 7200}} {
			if err := db.Model(&price.version).Update("purchase_options", marshal([]PurchaseOption{{Periods: 3, AmountMinor: price.amount, Enabled: true}})).Error; err != nil {
				t.Fatal(err)
			}
		}
		pay(base, 3, "purchase", "")
		finish()
		for _, upgrade := range []struct {
			version storage.PlanVersion
			amount  int64
		}{{pro, 2400}, {highest, 2100}} {
			quote, err := service.Quote(ctx, customer, QuoteInput{Action: "upgrade", ProductID: product.ID, PlanVersionID: upgrade.version.ID, Environment: "sandbox"}, "")
			if err != nil || quote.AmountMinor != upgrade.amount {
				t.Fatal("incorrect discounted upgrade price", quote.AmountMinor, upgrade.amount, err)
			}
			pay(upgrade.version, 1, "upgrade", quote.Token)
			finish()
		}
	})
	// Create a genuine one-day coverage whose end is near enough to cross
	// during the same quote. Only the fixture's historical payment time is set.
	prepareRenewal := func(name string) (storage.Order, storage.Entitlement) {
		t.Helper()
		version := newProduct(name)
		if err := db.Model(&version).Update("term_unit", "day").Error; err != nil {
			t.Fatal(err)
		}
		view, err := service.CreateOrder(ctx, customer, CheckoutInput{ProductID: product.ID, PlanVersionID: version.ID, Environment: "sandbox", Provider: "stripe", IdempotencyKey: id()})
		if err != nil {
			t.Fatal(err)
		}
		boundary := now().Add(time.Second)
		paidAt := boundary.Add(-24 * time.Hour)
		if err := db.Model(&storage.Order{}).Where("id = ?", view.ID).Update("created_at", paidAt.Add(-time.Second)).Error; err != nil {
			t.Fatal(err)
		}
		payments.paidAt = &paidAt
		err = service.reconcilePayment(ctx, view.ID)
		payments.paidAt = nil
		if err != nil {
			t.Fatal(err)
		}
		finish()
		e := purchase()
		quote, err := service.Quote(ctx, customer, QuoteInput{Action: "renew", ProductID: product.ID, PlanVersionID: version.ID, Environment: "sandbox", PurchasePeriods: 3}, "")
		if err != nil {
			t.Fatal(err)
		}
		renewal, err := service.CreateOrder(ctx, customer, CheckoutInput{Action: "renew", QuoteToken: quote.Token, ProductID: product.ID, PlanVersionID: version.ID, PurchasePeriods: 3, Environment: "sandbox", Provider: "stripe", IdempotencyKey: id()})
		if err != nil {
			t.Fatal(err)
		}
		var order storage.Order
		if err := db.First(&order, "id = ?", renewal.ID).Error; err != nil {
			t.Fatal(err)
		}
		return order, e
	}
	run("refund does not unlock the original positive renewal snapshot", func(t *testing.T) {
		order, before := prepareRenewal("Refund snapshot identity")
		time.Sleep(time.Until(before.ValidUntil.Add(10 * time.Millisecond)))
		if err := service.ProcessCoverageTransitions(ctx); err != nil {
			t.Fatal(err)
		}
		expiry := latest()
		if err := db.Model(&expiry).Update("status", "manual_review").Error; err != nil {
			t.Fatal(err)
		}
		if err := service.reconcilePayment(ctx, order.ID); err != nil {
			t.Fatal(err)
		}
		positive := latest()
		refund(order)
		negative := latest()
		if handled, err := service.ProcessAuthorization(ctx); err != nil || !handled {
			t.Fatal("refund snapshot not delivered", handled, err)
		}
		peer.mu.Lock()
		_, positiveDelivered := peer.events[positive.OperationID]
		snapshot, negativeDelivered := peer.events[negative.OperationID]
		peer.mu.Unlock()
		if positiveDelivered || !negativeDelivered || snapshot.Type != "entitlement.expired" {
			t.Fatal("refund status unlocked its original positive task", positiveDelivered, negativeDelivered, snapshot.Type)
		}
		settleAll()
	})
	for _, paymentAfterExpiry := range []bool{false, true} {
		run(fmt.Sprintf("renewal survives natural expiry paid after expiry %v", paymentAfterExpiry), func(t *testing.T) {
			order, before := prepareRenewal(fmt.Sprintf("Expiry race %v", paymentAfterExpiry))
			paidAt := before.ValidUntil.Add(-time.Microsecond)
			if paymentAfterExpiry {
				paidAt = before.ValidUntil.Add(time.Microsecond)
			}
			time.Sleep(time.Until(before.ValidUntil.Add(10 * time.Millisecond)))
			if err := service.ProcessCoverageTransitions(ctx); err != nil {
				t.Fatal(err)
			}
			payments.paidAt = &paidAt
			err := service.reconcilePayment(ctx, order.ID)
			payments.paidAt = nil
			if err != nil {
				t.Fatal(err)
			}
			var coverage storage.EntitlementCoverage
			if err := db.First(&coverage, "source_order_id = ?", order.ID).Error; err != nil {
				t.Fatal("successful renewal did not add coverage", err)
			}
			anchor, periods := *before.ValidFrom, 4
			if paymentAfterExpiry {
				anchor, periods = paidAt, 3
			}
			want, err := (domain.Schedule{Anchor: anchor, Unit: domain.Day, Interval: 1, Periods: periods}).End()
			if err != nil || !coverage.ValidUntil.Equal(want) {
				t.Fatal("renewal used the callback time or stale quote dates", coverage, want, err)
			}
			settleAll()
		})
	}
	run("admin recovers paid expiry review exactly once with audit", func(t *testing.T) {
		order, before := prepareRenewal("Expiry recovery")
		time.Sleep(time.Until(before.ValidUntil.Add(10 * time.Millisecond)))
		if err := service.ProcessCoverageTransitions(ctx); err != nil {
			t.Fatal(err)
		}
		paidAt, external := now(), "paid-recovery"
		if err := db.Model(&storage.Payment{}).Where("order_id = ?", order.ID).Updates(map[string]any{"status": "succeeded", "paid_at": paidAt, "external_transaction_id": external}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&order).Updates(map[string]any{"status": "paid", "paid_at": paidAt, "fulfillment_review": true}).Error; err != nil {
			t.Fatal(err)
		}
		if err := service.AdminReconcile(ctx, customer, order.ID, "recover settled renewal"); err != ErrForbidden {
			t.Fatal("customer recovered reviewed fulfillment", err)
		}
		for i := 0; i < 2; i++ {
			if err := service.AdminReconcile(ctx, admin, order.ID, "verify original paid time and recover renewal"); err != nil {
				t.Fatal(err)
			}
		}
		var count int64
		if err := db.Model(&storage.EntitlementCoverage{}).Where("source_order_id = ?", order.ID).Count(&count).Error; err != nil || count != 1 {
			t.Fatal("review recovery did not add exactly one coverage", count, err)
		}
		if err := db.First(&order, "id = ?", order.ID).Error; err != nil || order.FulfillmentReview {
			t.Fatal("successful recovery retained review", err)
		}
		var coverage storage.EntitlementCoverage
		want, err := (domain.Schedule{Anchor: paidAt, Unit: domain.Day, Interval: 1, Periods: 3}).End()
		if err != nil {
			t.Fatal(err)
		}
		if err := db.First(&coverage, "source_order_id = ?", order.ID).Error; err != nil || !coverage.ValidFrom.Equal(paidAt) || !coverage.ValidUntil.Equal(want) {
			t.Fatal("recovery did not use the original verified payment time", coverage, err)
		}
		if err := db.Model(&storage.AuditEntry{}).Where("action = ? AND object_id = ? AND actor_id = ?", "order.fulfillment_recovered", order.ID, admin.User.ID).Count(&count).Error; err != nil || count != 1 {
			t.Fatal("recovery audit missing or duplicated", count, err)
		}
		settleAll()
	})
	for _, conflict := range []string{"refund", "plan change", "hold"} {
		run("review recovery rejects a real "+conflict, func(t *testing.T) {
			order, before := prepareRenewal("Expiry conflict " + conflict)
			switch conflict {
			case "refund":
				var base storage.EntitlementCoverage
				var original storage.Order
				if err := db.First(&base, "id = ?", *before.CurrentCoverageID).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.First(&original, "id = ?", base.SourceOrderID).Error; err != nil {
					t.Fatal(err)
				}
				refund(original)
			case "plan change":
				// The original checkout can still settle after being closed. A
				// later independently paid plan must retain precedence.
				if err := db.Model(&order).Update("status", "closed").Error; err != nil {
					t.Fatal(err)
				}
				target := plan("Conflict higher", 2000)
				if err := db.Model(&target).Update("term_unit", "day").Error; err != nil {
					t.Fatal(err)
				}
				quote, err := service.Quote(ctx, customer, QuoteInput{Action: "upgrade", ProductID: product.ID, PlanVersionID: target.ID, Environment: "sandbox"}, "")
				if err != nil {
					t.Fatal(err)
				}
				pay(target, 1, "upgrade", quote.Token)
			case "hold":
				if err := db.Model(&storage.Entitlement{}).Where("id = ?", before.ID).Update("manual_hold", true).Error; err != nil {
					t.Fatal(err)
				}
			}
			time.Sleep(time.Until(before.ValidUntil.Add(10 * time.Millisecond)))
			if err := service.ProcessCoverageTransitions(ctx); err != nil {
				t.Fatal(err)
			}
			if err := service.reconcilePayment(ctx, order.ID); err != nil {
				t.Fatal(err)
			}
			if err := service.AdminReconcile(ctx, admin, order.ID, "recheck real conflict"); err == nil {
				t.Fatal("real commercial conflict was recovered automatically")
			}
			var count int64
			if err := db.Model(&storage.EntitlementCoverage{}).Where("source_order_id = ?", order.ID).Count(&count).Error; err != nil || count != 0 {
				t.Fatal("conflicted renewal added coverage", count, err)
			}
			if err := db.First(&order, "id = ?", order.ID).Error; err != nil || !order.FulfillmentReview {
				t.Fatal("real conflict left review", order, err)
			}
			settleAll()
		})
	}
	testIdentityRevocationDelivery(t, db, service, config, user, admin, product.ID, peer)
}
