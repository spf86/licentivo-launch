package httpapi_test

import (
	"licentivo-launch/internal/app"
	"strings"
)

func testStaffAndExports(a *testApp, admin, customer *browser, credentials map[string]string) {
	t := a.t
	customers := data[[]app.ManagedUser](t, a.request("GET", "/api/v1/admin/users", nil, admin))
	var customerID string
	for _, user := range customers {
		if user.Email == "owner@example.invalid" {
			t.Fatal("administrator appeared in the customer list")
		}
		if user.Email == credentials["email"] {
			customerID = user.ID
		}
	}
	if customerID == "" {
		t.Fatal("customer missing from customer list")
	}

	staff := data[[]app.StaffView](t, a.request("GET", "/api/v1/admin/staff", nil, admin))
	var adminID string
	for _, person := range staff {
		if person.Email == "owner@example.invalid" {
			adminID = person.ID
		}
		if person.Role == "customer" {
			t.Fatal("customer role appeared in staff list")
		}
	}
	if adminID == "" {
		t.Fatal("administrator missing from staff list")
	}
	a.expect(a.request("POST", "/api/v1/admin/staff", map[string]string{"user_id": adminID, "role": "support", "reason": "测试最后管理员保护"}, admin), 409)
	a.expect(a.request("POST", "/api/v1/admin/staff", map[string]string{"user_id": customerID, "role": "support", "reason": "客户不能转换为网站人员"}, admin), 404)
	a.expect(a.request("POST", "/api/v1/admin/staff/create", map[string]string{"email": credentials["email"], "password": "support-password-for-tests", "role": "support"}, admin), 409)

	staffEmail := "support@example.invalid"
	staffPassword := "support-password-for-tests"
	a.expect(a.request("POST", "/api/v1/admin/staff/create", map[string]string{"email": staffEmail, "password": staffPassword, "role": "support"}, admin), 200)
	staff = data[[]app.StaffView](t, a.request("GET", "/api/v1/admin/staff", nil, admin))
	var supportID string
	for _, person := range staff {
		if person.Email == staffEmail {
			supportID = person.ID
			if person.Role != "support" || person.Status != "active" || !person.Verified {
				t.Fatal("created staff account has unexpected state")
			}
		}
	}
	if supportID == "" {
		t.Fatal("created staff account missing")
	}
	customers = data[[]app.ManagedUser](t, a.request("GET", "/api/v1/admin/users", nil, admin))
	for _, user := range customers {
		if user.Email == staffEmail {
			t.Fatal("staff account appeared in customer list")
		}
	}

	a.expect(a.request("POST", "/api/v1/admin/exports", map[string]string{"kind": "users", "reason": "无权导出"}, customer), 403)
	for _, kind := range []string{"users", "orders", "bindings"} {
		w := a.request("POST", "/api/v1/admin/exports", map[string]string{"kind": kind, "reason": "本地数据最小化验收"}, admin)
		a.expect(w, 200)
		export := data[app.ExportView](t, w)
		for _, secret := range []string{"$argon2", "lv_lic_test_customer_only", "credential_secret_id", "password_hash", "TokenHash"} {
			if strings.Contains(export.Content, secret) {
				t.Fatalf("secret appeared in %s export", kind)
			}
		}
		if export.Rows == 0 {
			t.Fatal("export missing data")
		}
	}

	support := &browser{}
	a.expect(a.request("POST", "/api/v1/auth/login", map[string]string{"email": staffEmail, "password": staffPassword, "kind": "website"}, support), 200)
	a.expect(a.request("GET", "/api/v1/admin/dashboard", nil, support), 200)
	a.expect(a.request("GET", "/api/v1/admin/orders", nil, support), 200)
	a.expect(a.request("GET", "/api/v1/admin/staff", nil, support), 403)
	a.expect(a.request("POST", "/api/v1/admin/exports", map[string]string{"kind": "users", "reason": "客服越权导出"}, support), 403)

	a.expect(a.request("POST", "/api/v1/admin/staff", map[string]string{"user_id": supportID, "role": "operations", "reason": "测试运营权限"}, admin), 200)
	a.expect(a.request("GET", "/api/v1/me", nil, support), 401)
	operations := &browser{}
	a.expect(a.request("POST", "/api/v1/auth/login", map[string]string{"email": staffEmail, "password": staffPassword, "kind": "website"}, operations), 200)

	a.expect(a.request("POST", "/api/v1/admin/staff/"+supportID+"/status", map[string]string{"status": "disabled", "reason": "测试停用网站人员"}, admin), 200)
	a.expect(a.request("GET", "/api/v1/me", nil, operations), 401)
	a.expect(a.request("POST", "/api/v1/auth/login", map[string]string{"email": staffEmail, "password": staffPassword, "kind": "website"}, &browser{}), 401)
	a.expect(a.request("POST", "/api/v1/admin/staff/"+supportID+"/status", map[string]string{"status": "active", "reason": "测试恢复网站人员"}, admin), 200)
	a.expect(a.request("POST", "/api/v1/auth/login", map[string]string{"email": staffEmail, "password": staffPassword, "kind": "website"}, &browser{}), 200)
	a.expect(a.request("POST", "/api/v1/admin/staff/"+adminID+"/status", map[string]string{"status": "disabled", "reason": "测试最后管理员保护"}, admin), 409)
}
