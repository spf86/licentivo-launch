package httpapi_test

import (
	"fmt"
	"sort"
	"time"

	"licentivo-launch/internal/app"
	"licentivo-launch/internal/security"
	"licentivo-launch/internal/storage"
)

func testOrderWorkspace(a *testApp, admin, customer *browser, paid storage.Order) {
	t := a.t
	before := data[app.AdminOrderPage](t, a.request("GET", "/api/v1/admin/orders/search", nil, admin))
	var ids []string
	fixtures := make(map[string]bool)
	defer func() {
		if err := a.db.Where("id IN ?", ids).Delete(&storage.Order{}).Error; err != nil {
			t.Fatal(err)
		}
	}()
	stamp := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	for i := 0; i < 106; i++ {
		row := paid
		row.ID, _ = security.ID()
		row.IdempotencyKey = fmt.Sprintf("order-search-fixture-%03d", i)
		row.CreatedAt = stamp
		row.FulfillmentReview = i%9 == 0
		if i%2 == 0 {
			row.Status, row.PaidAt = "pending", nil
		}
		if err := a.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		ids = append(ids, row.ID)
		fixtures[row.ID] = true
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	page := data[app.AdminOrderPage](t, a.request("GET", "/api/v1/admin/orders/search?limit=25&offset=100", nil, admin))
	if page.Total != before.Total+106 || page.Offset != 100 || page.Items[0].ID != ids[100] || page.Items[0].CustomerEmail == "" || page.Items[0].ProductName == "" {
		t.Fatalf("complete history or stable tied-time pagination failed: %+v", page)
	}
	found := data[app.AdminOrderPage](t, a.request("GET", "/api/v1/admin/orders/search?q="+ids[105], nil, admin))
	if found.Total != 1 || found.Items[0].ID != ids[105] {
		t.Fatal("search did not find an order outside the first 100")
	}
	for _, query := range []string{"%25", "_", "%27%20OR%201%3D1--"} {
		result := data[app.AdminOrderPage](t, a.request("GET", "/api/v1/admin/orders/search?q="+query, nil, admin))
		if result.Total != 0 {
			t.Fatal("wildcards or SQL input broadened literal search")
		}
	}
	filtered := data[app.AdminOrderPage](t, a.request("GET", "/api/v1/admin/orders/search?status=pending&environment=sandbox&needs_review=true&limit=100", nil, admin))
	for _, row := range filtered.Items {
		if row.Status != "pending" || row.Environment != "sandbox" || (fixtures[row.ID] && !row.FulfillmentReview) {
			t.Fatal("filters were not applied")
		}
	}
	for _, query := range []string{"limit=0", "limit=101", "offset=-1", "offset=abc", "status=unknown", "status=pending%7Cclosed", "environment=prod", "needs_review=maybe"} {
		a.expect(a.request("GET", "/api/v1/admin/orders/search?"+query, nil, admin), 400)
	}
	a.expect(a.request("GET", "/api/v1/admin/orders/search", nil, customer), 403)
	input := app.ReconcileOrdersInput{OrderIDs: []string{paid.ID, paid.ID}, Reason: "验证重复选择"}
	a.expect(a.request("POST", "/api/v1/admin/orders/reconcile", input, admin), 400)
	input.OrderIDs = ids[:6]
	a.expect(a.request("POST", "/api/v1/admin/orders/reconcile", input, admin), 400)
	input.OrderIDs = nil
	a.expect(a.request("POST", "/api/v1/admin/orders/reconcile", input, admin), 400)
	input.OrderIDs = []string{paid.ID}
	a.expect(a.request("POST", "/api/v1/admin/orders/reconcile", input, customer), 403)
	missing, _ := security.ID()
	input.OrderIDs = []string{missing, paid.ID}
	results := data[[]app.ReconcileOrderResult](t, a.request("POST", "/api/v1/admin/orders/reconcile", input, admin))
	if len(results) != 2 || results[0].Checked || results[0].Code != "NOT_FOUND" || !results[1].Checked || results[1].OrderID != paid.ID {
		t.Fatalf("partial batch failure prevented subsequent reconciliation: %+v", results)
	}
	var audits int64
	a.db.Model(&storage.AuditEntry{}).Where("object_id = ? AND action = 'order.reconciled' AND reason = ?", paid.ID, input.Reason).Count(&audits)
	if audits != 1 {
		t.Fatal("successful batch reconciliation did not retain its audit reason")
	}
}
