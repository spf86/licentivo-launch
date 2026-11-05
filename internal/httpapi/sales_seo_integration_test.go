package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"licentivo-launch/internal/app"
	"licentivo-launch/internal/security"
	"licentivo-launch/internal/storage"
)

func testSalesSEO(a *testApp, admin, customer *browser) {
	t := a.t
	id := func() string { return newFixtureID(t) }
	var payment storage.Payment
	if err := a.db.Where("status='succeeded' AND paid_at IS NOT NULL").First(&payment).Error; err != nil {
		t.Fatal(err)
	}
	var templateOrder storage.Order
	if err := a.db.First(&templateOrder, "id=?", payment.OrderID).Error; err != nil {
		t.Fatal(err)
	}
	var sourceProduct storage.Product
	var sourcePlan storage.Plan
	var sourceVersion storage.PlanVersion
	if err := a.db.First(&sourceProduct, "id=?", templateOrder.ProductID).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.db.First(&sourceVersion, "id=?", templateOrder.PlanVersionID).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.db.First(&sourcePlan, "id=?", sourceVersion.PlanID).Error; err != nil {
		t.Fatal(err)
	}
	product := sourceProduct
	product.ID = id()
	product.Slug = "sales-fixture-" + product.ID[:8]
	product.Name = "Analytics fixture"
	product.Status = "published"
	product.SEO = nil
	product.LicentivoProductID = nil
	if err := a.db.Create(&product).Error; err != nil {
		t.Fatal(err)
	}
	plan := sourcePlan
	plan.ID = id()
	plan.ProductID = product.ID
	plan.Name = "Analytics plan"
	if err := a.db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	version := sourceVersion
	version.ID = id()
	version.PlanID = plan.ID
	version.ProductID = product.ID
	if err := a.db.Create(&version).Error; err != nil {
		t.Fatal(err)
	}
	makePayment := func(orderID string, at time.Time, amount int64) storage.Payment {
		row := payment
		row.ID = id()
		row.OrderID = orderID
		row.OperationID = id()
		row.AmountMinor = amount
		row.Currency = "USD"
		row.PaidAt = &at
		row.CreatedAt = at.Add(-time.Minute)
		row.ExternalCheckoutID = nil
		transaction := id()
		row.ExternalTransactionID = &transaction
		row.CaptureOperationID = nil
		if err := a.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	makeOrder := func(day int, amount int64, status string) (storage.Order, storage.Payment) {
		at := time.Date(2026, time.January, day, 10, 0, 0, 0, time.UTC)
		row := templateOrder
		row.ID = id()
		row.ProductID = product.ID
		row.PlanVersionID = version.ID
		row.IdempotencyKey = id()
		row.AmountMinor = amount
		row.Currency = "USD"
		row.Status = status
		row.PaidAt = &at
		row.CreatedAt = at.Add(-time.Hour)
		row.ExpiresAt = at.Add(time.Hour)
		row.Action = "purchase"
		if err := a.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		return row, makePayment(row.ID, at, amount)
	}
	o1, p1 := makeOrder(2, 10000, "refunded")
	_, p2 := makeOrder(3, 5000, "reversed")
	makePayment(o1.ID, *p1.PaidAt, 10000)
	completed := time.Date(2026, 1, 4, 10, 0, 0, 0, time.UTC)
	refund := storage.Refund{ID: id(), PaymentID: p1.ID, AccountID: p1.AccountID, OperationID: id(), AmountMinor: 10000, Status: "succeeded", Reason: "fixture", CompletedAt: &completed, CreatedAt: completed}
	if err := a.db.Create(&refund).Error; err != nil {
		t.Fatal(err)
	}
	reversal := storage.PaymentReversal{ID: id(), AccountID: p2.AccountID, ExternalID: id(), PaymentID: p2.ID, SourceEventID: id(), AmountMinor: 5000, Currency: "USD", CreatedAt: completed.AddDate(0, 0, 1)}
	if err := a.db.Create(&reversal).Error; err != nil {
		t.Fatal(err)
	}
	query := "?from=2026-01-01&to=2026-01-05&time_zone=UTC&environment=" + payment.Environment + "&currency=USD&product_id=" + product.ID
	report := data[app.SalesReport](t, a.request("GET", "/api/v1/admin/sales"+query, nil, admin))
	if report.Metrics.Gross != 25000 || report.Metrics.Orders != 2 || report.Metrics.Customers != 1 || report.Metrics.Refunds != 10000 || report.Metrics.Reversals != 5000 || report.Metrics.Net != 10000 || len(report.Days) != 5 {
		t.Fatalf("sales report does not reconcile: %+v", report)
	}
	if report.TrafficCollected {
		t.Fatal("uncollected traffic was claimed")
	}
	transactions := data[app.SalesTransactionPage](t, a.request("GET", "/api/v1/admin/sales/transactions"+query+"&kind=payment&limit=25", nil, admin))
	var receiptTotal int64
	for _, row := range transactions.Items {
		receiptTotal += row.AmountMinor
		if row.ID == "" || row.OrderID == "" {
			t.Fatal("transaction references missing")
		}
	}
	if transactions.Total != 3 || receiptTotal != report.Metrics.Gross {
		t.Fatalf("drill-down does not reconcile: %+v", transactions)
	}
	customer = &browser{}
	pwd, err := security.HashPassword("sales-permissions-password")
	if err != nil {
		t.Fatal(err)
	}
	verified := time.Now().UTC()
	address := "sales-" + id() + "@example.invalid"
	mustCustomer := storage.User{ID: id(), Email: address, EmailKey: address, PasswordHash: pwd, Status: "active", EmailVerifiedAt: &verified}
	if err := a.db.Create(&mustCustomer).Error; err != nil {
		t.Fatal(err)
	}
	a.expect(a.request("POST", "/api/v1/auth/login", map[string]string{"email": address, "password": "sales-permissions-password"}, customer, func(r *http.Request) { r.RemoteAddr = "192.0.2.199:4000" }), 200)
	a.expect(a.request("GET", "/api/v1/admin/sales"+query, nil, customer), 403)
	a.expect(a.request("GET", "/api/v1/admin/seo", nil, customer), 403)
	a.expect(a.request("PUT", "/api/v1/admin/seo", app.SEOInput{ProductID: product.ID, Locale: "zh-CN"}, nil), 401)
	site := data[app.SiteView](t, a.request("GET", "/api/v1/site", nil, nil))
	locale := site.SourceLocale
	config := app.SEOInput{ProductID: product.ID, Locale: locale, Page: app.PageSEO{Title: "Export <script>alert(1)</script>", Description: "Independent product description", Keywords: []string{"export", "reports"}}}
	a.expect(a.request("PUT", "/api/v1/admin/seo", config, admin), 200)
	for _, entry := range []struct{ locale, title, description, keyword string }{{"ja", "日本向けソフトウェア", "日本のお客様向けのソフトウェアライセンス", "正規ライセンス"}, {"en", "Desktop software", "Software licensing for desktop customers", "desktop software"}} {
		input := app.SEOInput{ProductID: product.ID, Locale: entry.locale, Page: app.PageSEO{Title: entry.title, Description: entry.description, Keywords: []string{entry.keyword}}}
		a.expect(a.request("PUT", "/api/v1/admin/seo", input, admin), 200)
		defaults := app.SEODefaults{ProductTitleTemplate: "{product} | {brand}", Description: entry.description, Keywords: []string{entry.keyword + " default"}}
		a.expect(a.request("PUT", "/api/v1/admin/seo", app.SEOInput{Locale: entry.locale, Page: app.PageSEO{Title: entry.title, Description: entry.description, Keywords: []string{entry.keyword + " home"}}, Defaults: &defaults}, admin), 200)
		localized := a.request("GET", "/"+app.PublicLocalePath(entry.locale)+"/products/"+product.Slug, nil, nil)
		a.expect(localized, 200)
		if !strings.Contains(localized.Body.String(), `<meta name="keywords" content="`+entry.keyword+`">`) || !strings.Contains(localized.Body.String(), entry.description) || strings.Contains(localized.Body.String(), entry.keyword+" default") {
			t.Fatal("server HTML ignored the product's market-specific keywords or description")
		}
	}
	jaSettings := data[app.SEOEditorView](t, a.request("GET", "/api/v1/admin/seo?locale=ja", nil, admin))
	if len(jaSettings.Defaults.Keywords) != 1 || jaSettings.Defaults.Keywords[0] != "正規ライセンス default" {
		t.Fatal("saving another language overwrote Japanese defaults")
	}
	path := "/" + app.PublicLocalePath(locale) + "/products/" + product.Slug
	page := a.request("GET", path, nil, nil)
	a.expect(page, 200)
	if !strings.Contains(page.Body.String(), "Independent product description") || !strings.Contains(page.Body.String(), "Analytics fixture") || strings.Contains(page.Body.String(), "<script>alert(1)</script>") {
		t.Fatal("server-rendered public content or escaping is incorrect")
	}
	if !strings.Contains(page.Body.String(), `<meta name="keywords" content="export, reports">`) {
		t.Fatal("source-language product keywords were overwritten by another locale")
	}
	if !strings.Contains(page.Header().Get("Content-Type"), "text/html") || page.Header().Get("X-Robots-Tag") == "noindex" {
		t.Fatal("indexable page was excluded")
	}
	preview := a.request("GET", path+"?preview=sandbox", nil, nil)
	if preview.Header().Get("X-Robots-Tag") != "noindex" {
		t.Fatal("sandbox preview was indexable")
	}
	config.Page.NoIndex = true
	a.expect(a.request("PUT", "/api/v1/admin/seo", config, admin), 200)
	if a.request("GET", path, nil, nil).Header().Get("X-Robots-Tag") != "noindex" {
		t.Fatal("product noindex ignored")
	}
	sitemap := a.request("GET", "/sitemap.xml", nil, nil)
	a.expect(sitemap, 200)
	if strings.Contains(sitemap.Body.String(), product.Slug) {
		t.Fatal("noindex product leaked into sitemap")
	}
	config.Page.NoIndex = false
	a.expect(a.request("PUT", "/api/v1/admin/seo", config, admin), 200)
	var content app.ProductContent
	_ = json.Unmarshal(product.Content, &content)
	newSlug := product.Slug + "-renamed"
	a.expect(a.request("PUT", "/api/v1/admin/product", app.ProductView{ID: product.ID, Name: product.Name, Slug: newSlug, Status: "published", Content: content}, admin), 200)
	redirect := a.request("GET", path, nil, nil)
	if redirect.Code != http.StatusMovedPermanently || !strings.HasSuffix(redirect.Header().Get("Location"), newSlug) {
		t.Fatal("old product URL was not redirected")
	}
	settings := data[app.SEOEditorView](t, a.request("GET", "/api/v1/admin/seo?product_id="+product.ID+"&locale="+locale, nil, admin))
	if settings.Page.Description != config.Page.Description {
		t.Fatal("saving product content erased its SEO settings")
	}
	a.expect(a.request("GET", "/"+app.PublicLocalePath(locale)+"/products/"+newSlug+"/docs/missing", nil, nil), 404)
}

func newFixtureID(t *testing.T) string {
	t.Helper()
	// Kept local so fixtures use the same UUID contract as normal records.
	id, err := security.ID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
