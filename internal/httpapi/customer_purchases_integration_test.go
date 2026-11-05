package httpapi_test

import (
	"licentivo-launch/internal/app"
	"licentivo-launch/internal/security"
	"licentivo-launch/internal/storage"
	"strings"
	"time"
)

func testCustomerPurchaseWorkspace(a *testApp, admin, customer *browser, template storage.Order) {
	t := a.t
	at := time.Now().UTC().Truncate(time.Microsecond)
	id := func() string {
		v, e := security.ID()
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	var original storage.User
	must(a.db.First(&original, "id=?", template.UserID).Error)
	owner := original
	owner.ID = id()
	owner.Email = "purchases@customer-view.example"
	owner.EmailKey = owner.Email
	owner.Status = "active"
	must(a.db.Create(&owner).Error)
	var sourceProduct storage.Product
	var sourcePlan storage.Plan
	var sourceVersion storage.PlanVersion
	must(a.db.First(&sourceProduct, "id=?", template.ProductID).Error)
	must(a.db.First(&sourceVersion, "id=?", template.PlanVersionID).Error)
	must(a.db.First(&sourcePlan, "id=?", sourceVersion.PlanID).Error)
	createProduct := func(name string, permanent bool) (storage.Product, storage.PlanVersion) {
		p := sourceProduct
		p.ID = id()
		p.Name = name
		p.Slug = "customer-" + p.ID
		must(a.db.Omit("FkProductBrand").Create(&p).Error)
		pl := sourcePlan
		pl.ID = id()
		pl.ProductID = p.ID
		pl.Name = name + "现名"
		must(a.db.Create(&pl).Error)
		v := sourceVersion
		v.ID = id()
		v.PlanID = pl.ID
		v.ProductID = p.ID
		v.Version = 1
		if permanent {
			v.PurchaseType = "perpetual"
			v.TermUnit = nil
			v.TermCount = nil
			v.PermanentUpgradeMinor = nil
		}
		must(a.db.Create(&v).Error)
		return p, v
	}
	createEnt := func(p storage.Product, env string) storage.Entitlement {
		ref := id()
		e := storage.Entitlement{ID: id(), UserID: owner.ID, ProductID: p.ID, Environment: env, Status: "active", DesiredVersion: 1, AppliedVersion: 1, LaunchPurchaseRef: &ref, Snapshot: []byte(`{}`), CreatedAt: at, UpdatedAt: at}
		must(a.db.Create(&e).Error)
		return e
	}
	createOrder := func(p storage.Product, v storage.PlanVersion, env, action, status string, paid *time.Time) storage.Order {
		o := template
		o.ID = id()
		o.UserID = owner.ID
		o.ProductID = p.ID
		o.PlanVersionID = v.ID
		o.Environment = env
		o.Action = action
		o.Status = status
		o.PaidAt = paid
		o.IdempotencyKey = o.ID
		o.CreatedAt = at
		o.ExpiresAt = at.Add(time.Hour)
		o.EntitlementSnapshot = []byte(`{}`)
		o.PriceSnapshot = []byte(`{"plan_name":"购买时套餐名称"}`)
		must(a.db.Create(&o).Error)
		return o
	}
	createCover := func(e storage.Entitlement, o storage.Order, from time.Time, until *time.Time, base *string, voided bool) storage.EntitlementCoverage {
		c := storage.EntitlementCoverage{ID: id(), EntitlementID: e.ID, SourceOrderID: o.ID, ValidFrom: from, ValidUntil: until, BaseCoverageID: base, ContractAnchor: from, Snapshot: []byte(`{}`), CreatedAt: at}
		if voided {
			c.VoidedAt = &at
		}
		must(a.db.Create(&c).Error)
		return c
	}
	paid := at.Add(-time.Hour)
	start := at.Add(-24 * time.Hour)
	until := at.Add(29 * 24 * time.Hour)
	renewEnd := until.Add(30 * 24 * time.Hour)
	p, v := createProduct("A多期软件", false)
	e := createEnt(p, "sandbox")
	o := createOrder(p, v, "sandbox", "purchase", "paid", &paid)
	base := createCover(e, o, start, &until, nil, false)
	renewal := createOrder(p, v, "sandbox", "renew", "paid", &at)
	createCover(e, renewal, until, &renewEnd, nil, false)
	voidEnd := renewEnd.Add(30 * 24 * time.Hour)
	refunded := createOrder(p, v, "sandbox", "renew", "refunded", &at)
	createCover(e, refunded, renewEnd, &voidEnd, nil, true)
	upgradeVersion := v
	upgradeVersion.ID = id()
	upgradeVersion.Version = 2
	must(a.db.Create(&upgradeVersion).Error)
	upgrade := createOrder(p, upgradeVersion, "sandbox", "upgrade", "paid", &at)
	createCover(e, upgrade, start.Add(time.Hour), &until, &base.ID, false)
	nearProduct, nearVersion := createProduct("B临期软件", false)
	near := createEnt(nearProduct, "sandbox")
	nearEnd := at.Add(5 * 24 * time.Hour)
	nearOrder := createOrder(nearProduct, nearVersion, "sandbox", "purchase", "paid", &paid)
	createCover(near, nearOrder, start, &nearEnd, nil, false)
	permanentProduct, permanentVersion := createProduct("C永久软件", true)
	permanent := createEnt(permanentProduct, "sandbox")
	permanentOrder := createOrder(permanentProduct, permanentVersion, "sandbox", "purchase", "paid", &paid)
	createCover(permanent, permanentOrder, start, nil, nil, false)
	expiredProduct, expiredVersion := createProduct("D过期软件", false)
	expired := createEnt(expiredProduct, "sandbox")
	past := at.Add(-time.Hour)
	expiredOrder := createOrder(expiredProduct, expiredVersion, "sandbox", "purchase", "paid", &paid)
	createCover(expired, expiredOrder, start, &past, nil, false)
	manualProduct, _ := createProduct("E手动授予", false)
	createEnt(manualProduct, "sandbox")
	live := createEnt(p, "live")
	liveOrder := createOrder(p, v, "live", "purchase", "paid", &paid)
	liveEnd := at.Add(90 * 24 * time.Hour)
	createCover(live, liveOrder, start, &liveEnd, nil, false)
	oldStart := start.Add(-60 * 24 * time.Hour)
	oldEnd := start.Add(-30 * 24 * time.Hour)
	oldBaseOrder := createOrder(p, v, "sandbox", "purchase", "refunded", &paid)
	oldBase := createCover(e, oldBaseOrder, oldStart, &oldEnd, nil, true)
	oldOverlayOrder := createOrder(p, upgradeVersion, "sandbox", "upgrade", "paid", &paid)
	createCover(e, oldOverlayOrder, oldStart.Add(time.Hour), &oldEnd, &oldBase.ID, false)
	for i := 0; i < 108; i++ {
		row := createOrder(p, v, "sandbox", "purchase", "pending", nil)
		must(a.db.Model(&row).Update("created_at", at.Add(-time.Duration(i+1)*time.Minute)).Error)
	}
	endpoint := "/api/v1/admin/users/" + owner.ID
	page := data[app.CustomerProductPage](t, a.request("GET", endpoint+"/products?environment=sandbox", nil, admin))
	if page.Total != 5 || len(page.Items) != 5 {
		t.Fatalf("unexpected product page: %+v", page)
	}
	first := page.Items[0]
	if first.PlanName != "购买时套餐名称" || first.PlanVersion != 2 || first.SourceOrderID != upgrade.ID || first.ValidUntil == nil || !first.ValidUntil.Equal(until) || first.PurchasedUntil == nil || !first.PurchasedUntil.Equal(renewEnd) {
		t.Fatalf("upgrade, renewal or voided coverage calculation failed: %+v", first)
	}
	if !page.Items[2].Permanent || !page.Items[2].PurchasedPermanent || page.Items[3].Status != "expired" || page.Items[4].DatesKnown || page.Items[4].Permanent {
		t.Fatal("permanent, stale expiry or missing dates misrepresented", page.Items)
	}
	search := data[app.UserPage](t, a.request("GET", "/api/v1/admin/users/search?q=purchases%40customer-view.example&environment=sandbox", nil, admin))
	if search.Total != 1 || search.Items[0].Purchases.ProductCount != 5 || !search.Items[0].Purchases.NextExpiry.Equal(nearEnd) {
		t.Fatalf("summary must show earliest product expiry: %+v", search)
	}
	formal := data[app.CustomerProductPage](t, a.request("GET", endpoint+"/products", nil, admin))
	if formal.Total != 1 || !formal.Items[0].ValidUntil.Equal(liveEnd) {
		t.Fatal("live default or environment isolation failed", formal)
	}
	orders := data[app.CustomerPurchaseOrderPage](t, a.request("GET", endpoint+"/orders?environment=sandbox&limit=25&offset=100", nil, admin))
	if orders.Total != 117 || len(orders.Items) != 17 {
		t.Fatalf("history truncated: total=%d len=%d", orders.Total, len(orders.Items))
	}
	found := data[app.CustomerPurchaseOrderPage](t, a.request("GET", endpoint+"/orders?environment=sandbox&q="+refunded.ID, nil, admin))
	if found.Total != 1 || !found.Items[0].Voided || !found.Items[0].CoverageRecorded || found.Items[0].PlanName != "购买时套餐名称" {
		t.Fatal("refunded history lost its original term or name", found)
	}
	oldName := data[app.CustomerPurchaseOrderPage](t, a.request("GET", endpoint+"/orders?environment=sandbox&q=购买时套餐名称", nil, admin))
	if oldName.Total != 117 {
		t.Fatal("history must be searchable by its displayed paid-snapshot plan name", oldName.Total)
	}
	catalogName := data[app.AdminOrderPage](t, a.request("GET", "/api/v1/admin/orders/search?environment=sandbox&q="+p.Name, nil, admin))
	if catalogName.Total != 114 {
		t.Fatal("Chinese product search must work in the order workspace", catalogName.Total)
	}
	dependent := data[app.CustomerPurchaseOrderPage](t, a.request("GET", endpoint+"/orders?environment=sandbox&q="+oldOverlayOrder.ID, nil, admin))
	if dependent.Total != 1 || !dependent.Items[0].Voided {
		t.Fatal("refunded base left an apparently valid upgrade term", dependent)
	}
	other := data[app.CustomerPurchaseOrderPage](t, a.request("GET", "/api/v1/admin/users/"+template.UserID+"/orders?environment=sandbox&q="+refunded.ID, nil, admin))
	if other.Total != 0 {
		t.Fatal("customer history crossed owner scope")
	}
	for _, suffix := range []string{"products", "orders"} {
		a.expect(a.request("GET", endpoint+"/"+suffix, nil, customer), 403)
		a.expect(a.request("GET", endpoint+"/"+suffix, nil, nil), 401)
		a.expect(a.request("GET", "/api/v1/admin/users/"+id()+"/"+suffix, nil, admin), 404)
		for _, q := range []string{"limit=0", "limit=101", "offset=-1", "environment=unknown"} {
			a.expect(a.request("GET", endpoint+"/"+suffix+"?"+q, nil, admin), 400)
		}
	}
	raw := a.request("GET", endpoint+"/products?environment=sandbox", nil, admin).Body.String()
	for _, secret := range []string{"password_hash", "identity_token", "license_key", "snapshot", "request_hash"} {
		if strings.Contains(raw, secret) {
			t.Fatal("private customer data leaked", secret)
		}
	}
}
