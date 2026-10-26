package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	bridge "licentivo-launch/internal/adapters/licentivo"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/security"
	"licentivo-launch/internal/storage"
	launchsdk "licentivo-launch/pkg/launchclient"
)

func TestProductMappingIntegrity(t *testing.T) {
	dsn := os.Getenv("LAUNCH_TEST_MAPPING_DATABASE_DSN")
	if dsn == "" {
		t.Skip("set LAUNCH_TEST_MAPPING_DATABASE_DSN to a fresh launch_test_* database")
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(parsed.DBName, "launch_test_") {
		t.Fatal("a dedicated launch_test_* database is required")
	}
	db, err := storage.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close(db)
	var tables int64
	db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE()").Scan(&tables)
	if tables != 0 {
		t.Fatal("fresh mapping test database required")
	}
	ctx := context.Background()
	if err := storage.Initialize(ctx, db); err != nil {
		t.Fatal(err)
	}
	peer := &purchasePeer{t: t, events: map[string]ports.CommerceSnapshot{}, bodies: map[string][]byte{}, states: map[string]string{}, deliveries: map[string]int{}}
	server := httptest.NewServer(peer)
	defer server.Close()
	policies := &purchasePolicies{definitions: map[string]ports.PolicyDefinition{}}
	payments := &purchasePayments{requests: map[string]ports.CheckoutRequest{}}
	service, err := New(ctx, db, Config{PublicURL: "https://merchant.example", MasterKey: bytes.Repeat([]byte{29}, 32), UploadDir: t.TempDir(), PaymentFactory: func(context.Context, storage.PaymentAccount) (ports.PaymentProvider, error) { return payments, nil }, BridgeFactory: func(k string) (ports.LaunchBridge, error) { return bridge.NewBridgeClient(server.URL, k, true) }, LicensingFactory: func(context.Context, storage.LicentivoConnection) (ports.LicensingProvider, error) {
		return policies, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	id := func() string {
		v, e := security.ID()
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	create := func(v any) {
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	checked := now()
	user := storage.User{ID: id(), Email: "mapping-audit@example.invalid", EmailKey: "mapping-audit@example.invalid", PasswordHash: "test-only", Status: "active", EmailVerifiedAt: &checked}
	create(&user)
	owner := Principal{User: user, Role: "admin"}
	var secret string
	if err := db.Transaction(func(tx *gorm.DB) error {
		var e error
		secret, e = service.createSecret(tx, "licentivo", []byte("lv_api_"+strings.Repeat("a", 43)))
		return e
	}); err != nil {
		t.Fatal(err)
	}
	product := storage.Product{ID: id(), Name: "Concurrent mappings", Slug: "concurrent-mappings", Status: "published", Content: marshal(map[string]any{})}
	create(&product)
	catalog := licentivoCatalogSnapshot{}
	remoteProducts := []string{id(), id()}
	versions := make([]storage.PlanVersion, 2)
	inputs := make([]MappingInput, 2)
	for i := 0; i < 2; i++ {
		plan := storage.Plan{ID: id(), ProductID: product.ID, Name: "Plan", Status: "draft", AllowPurchase: true, AllowRenewal: true}
		create(&plan)
		versions[i] = storage.PlanVersion{ID: id(), PlanID: plan.ID, ProductID: product.ID, Version: uint64(i + 1), Currency: "USD", AmountMinor: 1000, PurchaseType: "perpetual", EntitlementSnapshot: marshal(map[string]any{}), SnapshotHash: bytes.Repeat([]byte{1}, 32)}
		create(&versions[i])
		policy := id()
		inputs[i] = MappingInput{VersionID: versions[i].ID, PolicyID: policy}
		catalog.Products.Items = append(catalog.Products.Items, struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}{remoteProducts[i], "Remote product"})
		catalog.Policies.Items = append(catalog.Policies.Items, licentivoCatalogPolicy{ID: policy, ProductID: remoteProducts[i], Name: "Policy", Mode: "perpetual", Version: 1, MaxDevices: 2})
		policies.definitions[policy] = ports.PolicyDefinition{ID: policy, ProductID: remoteProducts[i], Mode: "perpetual", Version: 1}
	}
	contract := ports.LaunchBridgeVersion
	create(&storage.LicentivoConnection{ID: id(), BaseURL: LicentivoServiceURL, CredentialSecretID: secret, ContractVersion: &contract, Capabilities: marshal(map[string]any{}), Catalog: marshal(catalog)})
	if err := service.syncLaunchTrust(ctx, "lv_api_"+strings.Repeat("a", 43)); err != nil {
		t.Fatal(err)
	}
	var arrivals atomic.Int32
	ready := make(chan struct{})
	entered := make(chan struct{})
	callback := "test:concurrent_product_mapping"
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		sql := strings.ToLower(tx.Statement.SQL.String())
		if (strings.Contains(sql, "count(") || strings.Contains(sql, "select distinct")) && strings.Contains(sql, "remote_product_id") && strings.Contains(sql, "join plan_versions") {
			n := arrivals.Add(1)
			if n == 1 {
				close(entered)
			}
			if n == 2 {
				close(ready)
			}
			select {
			case <-ready:
			case <-time.After(time.Second):
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Query().Remove(callback) })
	results := make(chan error, 2)
	go func() { results <- service.SaveMapping(ctx, owner, inputs[0]) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first consistency check did not start")
	}
	go func() { results <- service.SaveMapping(ctx, owner, inputs[1]) }()
	successes, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			successes++
			continue
		}
		failure, ok := err.(*Error)
		if !ok || failure.Status != 409 || failure.Code != "PRODUCT_MAPPING_CONFLICT" {
			t.Fatal("unexpected concurrent save result", err)
		}
		conflicts++
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal("concurrent mappings did not preserve a single product", successes, conflicts)
	}
	db.Callback().Query().Remove(callback)
	var count int64
	if err := db.Model(&storage.PlanLicenseMapping{}).Select("COUNT(DISTINCT remote_product_id)").Scan(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("conflicting remote products committed", count)
	}
	var configured storage.Product
	db.First(&configured, "id = ?", product.ID)
	if configured.LicentivoProductID == nil || *configured.LicentivoProductID != remoteProducts[0] {
		t.Fatal("stable product route missing")
	}

	create(&storage.PaymentAccount{ID: id(), Provider: "stripe", Environment: "sandbox", MerchantRef: "test-merchant", CredentialSecretID: secret, Enabled: true})
	if err := service.PublishPlan(ctx, owner, versions[0].PlanID, versions[0].ID, "sandbox"); err != nil {
		t.Fatal(err)
	}
	customer := Principal{User: user, Role: "customer"}
	checkout, err := service.CreateOrder(ctx, customer, CheckoutInput{ProductID: product.ID, PlanVersionID: versions[0].ID, Provider: "stripe", Environment: "sandbox", IdempotencyKey: id()})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.reconcilePayment(ctx, checkout.ID); err != nil {
		t.Fatal(err)
	}
	if handled, err := service.ProcessAuthorization(ctx); !handled || err != nil {
		t.Fatal("paid snapshot not delivered", err)
	}
	var task storage.SyncTask
	if err := db.First(&task, "source_order_id = ?", checkout.ID).Error; err != nil {
		t.Fatal(err)
	}
	peer.mu.Lock()
	peer.states[task.OperationID] = "applied"
	peer.mu.Unlock()
	if err := db.Model(&task).Update("available_at", now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if handled, err := service.ProcessAuthorization(ctx); !handled || err != nil {
		t.Fatal("paid snapshot not confirmed", err)
	}
	var purchase storage.Entitlement
	if err := db.First(&purchase, "user_id = ? AND product_id = ?", user.ID, product.ID).Error; err != nil {
		t.Fatal(err)
	}
	if purchase.AppliedVersion != purchase.DesiredVersion || purchase.AppliedVersion == 0 {
		t.Fatal("paid purchase not synchronized")
	}
	session := storage.Session{ID: id(), UserID: user.ID, TokenHash: bytes.Repeat([]byte{9}, 32), Kind: "desktop", CreatedAt: now(), ExpiresAt: now().Add(time.Hour)}
	create(&session)
	// Represent the old invalid state without changing the new save path.
	create(&storage.PlanLicenseMapping{PlanVersionID: versions[1].ID, ConnectionID: func() string { var c storage.LicentivoConnection; db.First(&c); return c.ID }(), RemoteProductID: remoteProducts[1], RemotePolicyID: inputs[1].PolicyID, RemotePolicyVersion: 1})
	identity, err := service.Identity(ctx, Principal{User: user, Session: session, Role: "customer"}, product.ID, "sandbox")
	if err != nil {
		t.Fatal(err)
	}
	if identity.LicentivoProductID != remoteProducts[0] {
		t.Fatal("draft redirected customer identity")
	}
	// Purchased routing must remain stable even if an old draft is inconsistent.
	var bought storage.PlanLicenseMapping
	if err := db.First(&bought, "plan_version_id = ?", versions[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if bought.RemoteProductID != identity.LicentivoProductID {
		t.Fatal("identity differs from purchased product")
	}
	if _, err := base64.RawURLEncoding.DecodeString(strings.Split(identity.IdentityToken, ".")[1]); err != nil {
		t.Fatal(err)
	}

	runtime, runtimeErr := launchsdk.NewRuntime(launchsdk.ProductConfig{ProductID: remoteProducts[0], BaseURL: server.URL, AllowHTTP: true, TrustedKeys: map[string]string{"test": base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))}}, launchsdk.RuntimeConfig{CachePath: t.TempDir() + "/lease.json", Environment: "sandbox", Identity: launchsdk.Identity{IdentityToken: identity.IdentityToken, ProductID: product.ID, LicentivoProductID: identity.LicentivoProductID, LaunchConnectionID: identity.LaunchConnectionID, Environment: "sandbox"}})
	if runtimeErr != nil {
		t.Fatal("SDK rejected corrected identity routing", runtimeErr)
	}
	runtime.Close()
	if err := service.PublishPlan(ctx, owner, versions[1].PlanID, versions[1].ID, "sandbox"); err == nil {
		t.Fatal("conflicting historical draft was published")
	}
	// An old archive has no stable field. Paid history must still win over draft B.
	if err := db.Model(&storage.Product{}).Where("id = ?", product.ID).Update("licentivo_product_id", nil).Error; err != nil {
		t.Fatal(err)
	}
	legacy, err := service.Identity(ctx, Principal{User: user, Session: session, Role: "customer"}, product.ID, "sandbox")
	if err != nil || legacy.LicentivoProductID != remoteProducts[0] {
		t.Fatal("legacy purchase route not preserved", err)
	}
	if err := service.SaveMapping(ctx, owner, inputs[0]); err != nil {
		t.Fatal("cannot establish stable route from paid history", err)
	}
	if _, err := service.SaveProduct(ctx, owner, ProductView{ID: product.ID, Name: product.Name, Slug: product.Slug, Status: "published", Content: ProductContent{Summary: "Updated content"}}); err != nil {
		t.Fatal(err)
	}
	var edited storage.Product
	db.First(&edited, "id = ?", product.ID)
	if edited.LicentivoProductID == nil || *edited.LicentivoProductID != remoteProducts[0] {
		t.Fatal("product edit discarded stable routing")
	}
	// With no established purchase/publication, mixed legacy drafts are ambiguous.
	other := storage.Product{ID: id(), Name: "Ambiguous legacy", Slug: "ambiguous-legacy", Status: "published", Content: marshal(map[string]any{})}
	create(&other)
	for i := range versions {
		plan := storage.Plan{ID: id(), ProductID: other.ID, Name: "Draft", Status: "draft", AllowPurchase: true}
		create(&plan)
		v := versions[i]
		v.ID = id()
		v.PlanID = plan.ID
		v.ProductID = other.ID
		create(&v)
		var c storage.LicentivoConnection
		db.First(&c)
		create(&storage.PlanLicenseMapping{PlanVersionID: v.ID, ConnectionID: c.ID, RemoteProductID: remoteProducts[i], RemotePolicyID: inputs[i].PolicyID, RemotePolicyVersion: 1})
	}
	_, err = service.Identity(ctx, Principal{User: user, Session: session, Role: "customer"}, other.ID, "sandbox")
	failure, ok := err.(*Error)
	if !ok || failure.Code != "PRODUCT_MAPPING_CONFLICT" {
		t.Fatal("ambiguous legacy route was guessed", err)
	}
	// Different plans of the same product may still map concurrently when
	// they agree on their licensing product.
	same := storage.Product{ID: id(), Name: "Compatible mappings", Slug: "compatible-mappings", Status: "published", Content: marshal(map[string]any{})}
	create(&same)
	compatible := make([]MappingInput, 2)
	for i := range compatible {
		plan := storage.Plan{ID: id(), ProductID: same.ID, Name: "Compatible", Status: "draft", AllowPurchase: true}
		create(&plan)
		v := versions[0]
		v.ID, v.PlanID, v.ProductID = id(), plan.ID, same.ID
		create(&v)
		compatible[i] = MappingInput{VersionID: v.ID, PolicyID: inputs[0].PolicyID}
	}
	for _, in := range compatible {
		go func(in MappingInput) { results <- service.SaveMapping(ctx, owner, in) }(in)
	}
	for range compatible {
		if err := <-results; err != nil {
			t.Fatal("compatible mappings were rejected", err)
		}
	}
	// A historical invalid publication must not produce SDK configuration for B
	// while customer identity and the paid purchase still point to A.
	if err := db.Model(&storage.Plan{}).Where("id = ?", versions[0].PlanID).Update("status", "archived").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&storage.Plan{}).Where("id = ?", versions[1].PlanID).Update("status", "published").Error; err != nil {
		t.Fatal(err)
	}
	create(&storage.PlanPublication{PlanID: versions[1].PlanID, Environment: "sandbox", VersionID: versions[1].ID, PublishedAt: now()})
	checkIntegration := func() {
		t.Helper()
		report, err := service.CheckIntegration(ctx, owner, IntegrationInput{ProductID: product.ID, Environment: "sandbox", Language: "go"})
		if err != nil {
			t.Fatal(err)
		}
		conflict := false
		for _, check := range report.Checks {
			conflict = conflict || check.Code == "PRODUCT_MAPPING_CONFLICT" && check.Status == "failed"
		}
		if report.Ready || !conflict || report.Config.LicentivoProductID != remoteProducts[0] || report.ProductConfig != nil {
			t.Fatal("inconsistent publication produced wrong SDK configuration", report)
		}
	}
	checkIntegration()
	if err := db.Model(&storage.Product{}).Where("id = ?", product.ID).Update("licentivo_product_id", nil).Error; err != nil {
		t.Fatal(err)
	}
	checkIntegration()
}
