package httpapi_test

import (
	"fmt"
	"sort"
	"time"

	"licentivo-launch/internal/app"
	"licentivo-launch/internal/security"
	"licentivo-launch/internal/storage"
)

func testCustomerLists(a *testApp, admin, customer *browser, paid storage.Order) {
	t := a.t
	before := data[app.CustomerOrderPage](t, a.request("GET", "/api/v1/me/orders/search", nil, customer))
	usersBefore := data[app.UserPage](t, a.request("GET", "/api/v1/admin/users/search", nil, admin))
	var orderIDs, userIDs []string
	defer func() {
		if err := a.db.Where("id IN ?", orderIDs).Delete(&storage.Order{}).Error; err != nil {
			t.Fatal(err)
		}
		if err := a.db.Where("user_id IN ?", userIDs).Delete(&storage.StaffAccount{}).Error; err != nil {
			t.Fatal(err)
		}
		if err := a.db.Where("id IN ?", userIDs).Delete(&storage.User{}).Error; err != nil {
			t.Fatal(err)
		}
	}()
	stamp := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Microsecond)
	var template storage.User
	if err := a.db.First(&template, "id = ?", paid.UserID).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 127; i++ {
		id, _ := security.ID()
		userIDs = append(userIDs, id)
		u := storage.User{ID: id, Email: fmt.Sprintf("customer-%03d@n-list-test.example", i), EmailKey: fmt.Sprintf("customer-%03d@n-list-test.example", i), PasswordHash: template.PasswordHash, Status: "disabled", CreatedAt: stamp}
		if i == 0 {
			u.EmailVerifiedAt = &stamp
		}
		if err := a.db.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := a.db.Create(&storage.StaffAccount{UserID: userIDs[126], Role: "support", Permissions: []byte("[]")}).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 108; i++ {
		row := paid
		row.ID, _ = security.ID()
		row.IdempotencyKey = fmt.Sprintf("customer-list-%03d", i)
		row.CreatedAt = stamp
		row.Status = "pending"
		row.PaidAt = nil
		if i == 107 {
			row.UserID = userIDs[0]
		}
		if err := a.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		orderIDs = append(orderIDs, row.ID)
	}
	own := append([]string{}, orderIDs[:107]...)
	sort.Sort(sort.Reverse(sort.StringSlice(own)))
	page := data[app.CustomerOrderPage](t, a.request("GET", "/api/v1/me/orders/search?limit=25&offset=100", nil, customer))
	if page.Total != before.Total+107 || len(page.Items) < 7 || page.Items[0].ID != own[100] || page.Items[6].ID != own[106] || page.Items[0].ProductName == "" || page.Items[0].PurchasePeriods != paid.PurchasedPeriods {
		t.Fatalf("customer history was truncated or changed: %+v", page)
	}
	for _, query := range []string{own[106], orderIDs[107]} {
		found := data[app.CustomerOrderPage](t, a.request("GET", "/api/v1/me/orders/search?q="+query, nil, customer))
		expected := int64(1)
		if query == orderIDs[107] {
			expected = 0
		}
		if found.Total != expected {
			t.Fatal("order search widened the customer's scope or missed old history")
		}
	}
	filtered := data[app.CustomerOrderPage](t, a.request("GET", "/api/v1/me/orders/search?status=pending&environment=sandbox&limit=100", nil, customer))
	for _, o := range filtered.Items {
		if o.Status != "pending" || o.Environment != "sandbox" {
			t.Fatal("customer order filters not applied")
		}
	}
	userPage := data[app.UserPage](t, a.request("GET", "/api/v1/admin/users/search?q=n-list-test.example&limit=25&offset=100", nil, admin))
	if userPage.Total != 126 || len(userPage.Items) != 25 || userPage.Limit != 25 || userPage.Offset != 100 {
		t.Fatalf("customer pagination or staff exclusion failed: %+v", userPage)
	}
	complete := data[app.UserPage](t, a.request("GET", "/api/v1/admin/users/search", nil, admin))
	if complete.Total != usersBefore.Total+126 {
		t.Fatal("customer count truncated or includes staff")
	}
	found := data[app.UserPage](t, a.request("GET", "/api/v1/admin/users/search?q=customer-000%40n-list-test.example&status=disabled", nil, admin))
	if found.Total != 1 || !found.Items[0].Verified || found.Items[0].Role != "customer" || found.Items[0].Status != "disabled" {
		t.Fatalf("public customer fields lost: %+v", found)
	}
	for _, base := range []string{"/api/v1/admin/users/search", "/api/v1/me/orders/search"} {
		for _, query := range []string{"limit=0", "limit=101", "limit=-1", "offset=-1", "offset=abc", "status=unknown"} {
			a.expect(a.request("GET", base+"?"+query, nil, adminOrCustomer(base, admin, customer)), 400)
		}
		for _, query := range []string{"%25", "_", "%27%20OR%201%3D1--"} {
			r := a.request("GET", base+"?q="+query, nil, adminOrCustomer(base, admin, customer))
			if base == "/api/v1/admin/users/search" {
				if data[app.UserPage](t, r).Total != 0 {
					t.Fatal("customer search treats user text as SQL wildcards")
				}
			} else {
				if data[app.CustomerOrderPage](t, r).Total != 0 {
					t.Fatal("order search treats user text as SQL wildcards")
				}
			}
		}
	}
	a.expect(a.request("GET", "/api/v1/admin/users/search", nil, customer), 403)
	a.expect(a.request("GET", "/api/v1/me/orders/search", nil, nil), 401)
}

func adminOrCustomer(path string, admin, customer *browser) *browser {
	if path == "/api/v1/admin/users/search" {
		return admin
	}
	return customer
}
