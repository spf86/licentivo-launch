package httpapi_test

import (
	"context"
	"encoding/json"
	"time"

	"licentivo-launch/internal/app"
	"licentivo-launch/internal/domain"
	"licentivo-launch/internal/storage"
)

func testSalesLifecycle(a *testApp, admin, customer *browser, base app.PlanView, service *app.Service, gateway *paymentDouble, licensing *licenseDouble) {
	t := a.t
	ctx := context.Background()
	var original storage.EntitlementCoverage
	if err := a.db.First(&original).Error; err != nil {
		t.Fatal(err)
	}
	var product storage.Product
	if a.db.First(&product).Error != nil {
		t.Fatal("product fixture missing")
	}
	quote := func(action string, version app.PlanView) app.QuoteView {
		t.Helper()
		w := a.request("POST", "/api/v1/quotes", app.QuoteInput{ProductID: product.ID, PlanVersionID: version.VersionID, Environment: "sandbox", Action: action}, customer)
		a.expect(w, 200)
		return data[app.QuoteView](t, w)
	}
	quotePeriods := func(action string, version app.PlanView, periods uint64) app.QuoteView {
		t.Helper()
		w := a.request("POST", "/api/v1/quotes", app.QuoteInput{ProductID: product.ID, PlanVersionID: version.VersionID, Environment: "sandbox", Action: action, PurchasePeriods: periods}, customer)
		a.expect(w, 200)
		return data[app.QuoteView](t, w)
	}
	checkout := func(q app.QuoteView, key string) app.CheckoutInput {
		return app.CheckoutInput{Action: q.Action, QuoteToken: q.Token, ProductID: product.ID, PlanVersionID: q.Plan.VersionID, PurchasePeriods: q.PurchasePeriods, Provider: "stripe", Environment: "sandbox", IdempotencyKey: key}
	}
	settle := func(in app.CheckoutInput) app.CheckoutView {
		t.Helper()
		w := a.request("POST", "/api/v1/orders", in, customer)
		a.expect(w, 200)
		o := data[app.CheckoutView](t, w)
		gateway.markPaid("cs_test_" + o.ID)
		w = a.request("POST", "/api/v1/orders/"+o.ID+"/reconcile", map[string]any{}, customer)
		a.expect(w, 200)
		if handled, err := service.ProcessAuthorization(ctx); err != nil || !handled {
			t.Fatalf("authorization: %v", err)
		}
		return o
	}
	higher := base
	higher.ID = ""
	higher.Version = 0
	higher.VersionID = ""
	higher.Name = "Studio"
	higher.AmountMinor = 3999
	higher.PurchaseOptions = append([]app.PurchaseOption(nil), base.PurchaseOptions...)
	for i := range higher.PurchaseOptions {
		higher.PurchaseOptions[i].AmountMinor = higher.AmountMinor * int64(higher.PurchaseOptions[i].Periods)
	}
	w := a.request("POST", "/api/v1/admin/plans", higher, admin)
	a.expect(w, 200)
	higher = data[app.PlanView](t, w)
	a.expect(a.request("POST", "/api/v1/admin/connections/mappings", app.MappingInput{VersionID: higher.VersionID, PolicyID: "44444444-4444-4444-8444-444444444444"}, admin), 200)
	a.expect(a.request("POST", "/api/v1/admin/plans/"+higher.ID+"/publish", map[string]string{"version_id": higher.VersionID, "environment": "sandbox"}, admin), 200)
	stale := quote("upgrade", higher)
	renewQuote := quotePeriods("renew", base, 3)
	if !renewQuote.ValidFrom.Equal(*original.ValidUntil) {
		t.Fatal("active renewal did not join original end")
	}
	if original.PaidPeriods == nil || *original.PaidPeriods != 3 {
		t.Fatalf("selected three-month purchase was not retained: %+v", original.PaidPeriods)
	}
	expected, err := (domain.Schedule{Anchor: original.ContractAnchor, Unit: domain.TermUnit(*original.TermUnit), Interval: int(*original.TermCount), Periods: int(*original.PaidPeriods + 3)}).End()
	if err != nil || !renewQuote.ValidUntil.Equal(expected) {
		t.Fatal("renewal drifted from original calendar anchor")
	}
	if renewQuote.PurchasePeriods != 3 || renewQuote.AmountMinor != base.PurchaseOptions[1].AmountMinor {
		t.Fatalf("three-month renewal quote used the wrong price or duration: %+v", renewQuote)
	}
	wrongDuration := checkout(renewQuote, "test-renew-periods-01")
	wrongDuration.PurchasePeriods = 1
	a.expect(a.request("POST", "/api/v1/orders", wrongDuration, customer), 400)
	renewal := settle(checkout(renewQuote, "test-renew-key-00001"))
	if renewal.PurchasePeriods != 3 || renewal.AmountMinor != base.PurchaseOptions[1].AmountMinor {
		t.Fatalf("three-month renewal order lost the selected price or duration: %+v", renewal)
	}
	a.expect(a.request("POST", "/api/v1/orders", checkout(stale, "test-stale-upgrade-01"), customer), 409)
	tampered := checkout(quote("upgrade", higher), "test-tampered-quote-01")
	tampered.QuoteToken = "x" + tampered.QuoteToken[1:]
	a.expect(a.request("POST", "/api/v1/orders", tampered, customer), 400)
	upgradeQuote := quote("upgrade", higher)
	if upgradeQuote.AmountMinor <= 0 || upgradeQuote.AmountMinor > higher.AmountMinor-base.AmountMinor || !upgradeQuote.ValidUntil.Equal(*original.ValidUntil) {
		t.Fatal("upgrade quote changed paid cycle or charged wrong difference")
	}
	upgrade := settle(checkout(upgradeQuote, "test-upgrade-key-0001"))
	var e storage.Entitlement
	if a.db.First(&e).Error != nil {
		t.Fatal("entitlement missing")
	}
	var rights app.PlanSnapshot
	_ = json.Unmarshal(e.Snapshot, &rights)
	if rights.MaxDevices != 4 || !e.ValidUntil.Equal(*original.ValidUntil) {
		t.Fatal("upgrade not applied within original cycle")
	}
	firstRefund := true
	refund := func(id string) app.RefundView {
		t.Helper()
		w := a.request("POST", "/api/v1/orders/"+id+"/refund", map[string]string{"reason": "本地验收退款"}, customer)
		a.expect(w, 200)
		r := data[app.RefundView](t, w)
		if firstRefund {
			again := data[app.RefundView](t, a.request("POST", "/api/v1/orders/"+id+"/refund", map[string]string{"reason": "重试相同申请"}, customer))
			if again.ID != r.ID {
				t.Fatal("refund request duplicated")
			}
			firstRefund = false
		}
		a.expect(a.request("POST", "/api/v1/admin/refunds/"+r.ID+"/approve", map[string]string{"reason": "批准本地测试"}, admin), 200)
		return r
	}
	a.expect(a.request("POST", "/api/v1/orders/"+upgrade.ID+"/refund", map[string]string{"reason": "越权退款"}, admin), 404)
	a.expect(a.request("GET", "/api/v1/admin/refunds", nil, customer), 403)
	r := refund(upgrade.ID)
	if err := service.ProcessRefunds(ctx); err != nil {
		t.Fatal(err)
	}
	var stored storage.Refund
	if a.db.First(&stored, "id = ?", r.ID).Error != nil || stored.Status != "unknown" {
		t.Fatal("uncertain refund lost durable state")
	}
	if err := a.db.Model(&stored).Update("next_check_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.ProcessRefunds(ctx); err != nil {
		t.Fatal(err)
	}
	if err := service.ProcessRefunds(ctx); err != nil {
		t.Fatal(err)
	}
	if gateway.refundCalls != 1 {
		t.Fatal("lost refund response caused another money operation")
	}
	if handled, err := service.ProcessAuthorization(ctx); err != nil || !handled {
		t.Fatalf("upgrade refund authorization: %v", err)
	}
	if a.db.First(&e, "id = ?", e.ID).Error != nil {
		t.Fatal("entitlement missing")
	}
	_ = json.Unmarshal(e.Snapshot, &rights)
	if rights.MaxDevices != 2 || !e.ValidUntil.Equal(*renewQuote.ValidUntil) {
		t.Fatal("upgrade refund destroyed base or renewal rights")
	}
	refund(original.SourceOrderID)
	if err := service.ProcessRefunds(ctx); err != nil {
		t.Fatal(err)
	}
	if handled, err := service.ProcessAuthorization(ctx); err != nil || !handled {
		t.Fatalf("base refund: %v", err)
	}
	if a.db.First(&e, "id = ?", e.ID).Error != nil || e.ValidFrom == nil || !e.ValidFrom.Equal(*original.ValidUntil) || !e.ValidUntil.Equal(*renewQuote.ValidUntil) {
		t.Fatal("base refund removed independently paid renewal")
	}
	refund(renewal.ID)
	if err := service.ProcessRefunds(ctx); err != nil {
		t.Fatal(err)
	}
	if handled, err := service.ProcessAuthorization(ctx); err != nil || !handled {
		t.Fatalf("renewal refund: %v", err)
	}
	if a.db.First(&e, "id = ?", e.ID).Error != nil || e.Status != "suspended" || e.AppliedVersion != e.DesiredVersion {
		t.Fatal("fully refunded entitlement remained usable")
	}
	var bindings int64
	a.db.Model(&storage.LicenseBinding{}).Count(&bindings)
	if bindings != 1 {
		t.Fatal("lifecycle replaced original license")
	}
	for _, raw := range licensing.requests {
		var command map[string]any
		if json.Unmarshal([]byte(raw), &command) != nil {
			t.Fatal("invalid command")
		}
		if command["license_id"] != nil && command["license_id"] != "33333333-3333-4333-8333-333333333333" {
			t.Fatal("lifecycle changed license identity")
		}
	}
	t.Log("renewal, stale/tampered quote, upgrade, uncertain refund recovery and independent coverage retained")
}
