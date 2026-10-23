package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"licentivo-launch/internal/app"
	"licentivo-launch/internal/storage"
)

func testIntegrationWizard(a *testApp, admin, customer *browser, plan app.PlanView) {
	t := a.t
	in := app.IntegrationInput{ProductID: plan.ProductID, Environment: "sandbox", Language: "go"}
	a.expect(a.request("GET", "/api/v1/admin/integration/context", nil, nil), 401)
	a.expect(a.request("GET", "/api/v1/admin/integration/context", nil, customer), 403)
	context := data[map[string]string](t, a.request("GET", "/api/v1/admin/integration/context", nil, admin, func(r *http.Request) { r.Host = "attacker.invalid" }))
	if context["merchant_origin"] != "https://merchant.example" || context["licensing_origin"] != app.LicentivoServiceURL {
		t.Fatal("deployment context used an untrusted request host")
	}
	a.expect(a.request("POST", "/api/v1/admin/integration/check", in, nil), 401)
	a.expect(a.request("POST", "/api/v1/admin/integration/check", in, customer), 403)
	for index, language := range []string{"go", "python", "csharp", "java", "javascript", "rust", "ruby", "c", "cpp", "web"} {
		in.Language = language
		w := a.request("POST", "/api/v1/admin/integration/check", in, admin, func(r *http.Request) {
			r.RemoteAddr = fmt.Sprintf("198.51.100.%d:4000", index+1)
		})
		report := data[app.IntegrationReport](t, w)
		if !report.Ready || report.ProductConfig == nil || report.Config.LicentivoProductID != "11111111-1111-4111-8111-111111111111" || report.Config.LaunchProductID != plan.ProductID || report.Config.MerchantOrigin != "https://merchant.example" || report.ExamplePath == "" || len(report.ProductConfig.TrustedKeys) != 1 {
			t.Fatalf("automatic integration not ready for %s: %+v", language, report)
		}
		for _, secret := range []string{"lv_api_test_launch_connection", "license_key", "cache_path", "credential_secret_id", "private_key"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatalf("private field in initialization response: %s", secret)
			}
		}
	}
	// The input accepts only the selected software, language and environment.
	a.expect(a.request("POST", "/api/v1/admin/integration/check", map[string]any{"product_id": plan.ProductID, "environment": "sandbox", "language": "go", "product_config": map[string]string{"license_key": "never-export"}}, admin), 400)
	in.Environment = "live"
	report := data[app.IntegrationReport](t, a.request("POST", "/api/v1/admin/integration/check", in, admin))
	if report.Ready || report.Config.LicentivoProductID != "" || report.ProductConfig != nil {
		t.Fatal("sandbox mapping crossed into live")
	}
}

func testSoftwareStatus(a *testApp, customer *browser, grant app.SessionGrant, productID string) {
	t := a.t
	auth := func(r *http.Request) {
		r.Header.Del("Origin")
		r.Header.Set("Authorization", "Bearer "+grant.AccessToken)
	}
	path := "/api/v1/software/products/" + productID
	a.expect(a.request("GET", "/api/v1/software/products/not-a-product-id/status?environment=sandbox", nil, nil, auth), 400)
	a.expect(a.request("POST", "/api/v1/software/products/not-a-product-id/credential", map[string]string{"environment": "sandbox"}, nil, auth), 400)
	a.expect(a.request("GET", path+"/status?environment=sandbox", nil, customer), 403)
	a.expect(a.request("GET", path+"/status?environment=bogus", nil, nil, auth), 400)
	status := data[app.SoftwareStatus](t, a.request("GET", path+"/status?environment=sandbox", nil, nil, auth))
	if status.Code != "ENTITLEMENT_READY" || status.NextAction != "activate" {
		t.Fatalf("unexpected ready status: %+v", status)
	}
	status = data[app.SoftwareStatus](t, a.request("GET", path+"/status?environment=live", nil, nil, auth))
	// The earlier invoice fixture has a paid live order but no license. It
	// must wait for issuance, and must never borrow the sandbox license.
	if status.Code != "ENTITLEMENT_PENDING" || status.NextAction != "retry" || status.Synchronized {
		t.Fatalf("paid live invoice borrowed sandbox state or lost issuance guidance: %+v", status)
	}
	w := a.request("POST", path+"/credential", map[string]string{"environment": "live"}, nil, auth)
	a.expect(w, 403)
	if !strings.Contains(w.Body.String(), "ENTITLEMENT_PENDING") || w.Header().Get("Retry-After") != "3" {
		t.Fatal("paid live invoice should wait for its own license")
	}
	var entitlement storage.Entitlement
	if err := a.db.First(&entitlement, "product_id = ? AND environment = 'sandbox'", productID).Error; err != nil {
		t.Fatal(err)
	}
	restore := func() {
		if err := a.db.Model(&entitlement).Updates(map[string]any{"manual_hold": entitlement.ManualHold, "status": entitlement.Status, "applied_version": entitlement.AppliedVersion, "valid_until": entitlement.ValidUntil}).Error; err != nil {
			t.Fatal(err)
		}
	}
	defer restore()
	for _, test := range []struct {
		changes map[string]any
		code    string
		retry   bool
	}{
		{map[string]any{"applied_version": 0}, "ENTITLEMENT_PENDING", true},
		{map[string]any{"manual_hold": true}, "ENTITLEMENT_SUSPENDED", false},
		{map[string]any{"status": "revoked"}, "ENTITLEMENT_SUSPENDED", false},
		{map[string]any{"status": "expired", "valid_until": entitlement.ValidFrom.Add(time.Microsecond)}, "ENTITLEMENT_EXPIRED", false},
	} {
		restore()
		if err := a.db.Model(&storage.Entitlement{}).Where("id = ?", entitlement.ID).Updates(test.changes).Error; err != nil {
			t.Fatal(err)
		}
		w := a.request("POST", path+"/credential", map[string]string{"environment": "sandbox"}, nil, auth)
		a.expect(w, 403)
		var envelope struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if json.Unmarshal(w.Body.Bytes(), &envelope) != nil || envelope.Error.Code != test.code {
			t.Fatalf("wrong actionable error: %s", w.Body.String())
		}
		if (w.Header().Get("Retry-After") == "3") != test.retry {
			t.Fatal("incorrect credential retry hint")
		}
		status = data[app.SoftwareStatus](t, a.request("GET", path+"/status?environment=sandbox", nil, nil, auth))
		if status.Code != test.code {
			t.Fatal("status and credential disagree")
		}
	}
}

func testUnpaidSoftwareStatus(a *testApp, credentials map[string]string, productID string) {
	login := map[string]string{"email": credentials["email"], "password": credentials["password"], "kind": "desktop"}
	grant := data[app.SessionGrant](a.t, a.request("POST", "/api/v1/auth/login", login, nil, func(r *http.Request) { r.Header.Del("Origin") }))
	auth := func(r *http.Request) {
		r.Header.Del("Origin")
		r.Header.Set("Authorization", "Bearer "+grant.AccessToken)
	}
	defer func() { a.expect(a.request("POST", "/api/v1/auth/logout", nil, nil, auth), 200) }()
	path := "/api/v1/software/products/" + productID
	status := data[app.SoftwareStatus](a.t, a.request("GET", path+"/status?environment=sandbox", nil, nil, auth))
	if status.Code != "ENTITLEMENT_NOT_PURCHASED" || status.MaxRetryAttempts != 0 {
		a.t.Fatalf("unpaid empty entitlement was treated as paid issuance: %+v", status)
	}
	w := a.request("POST", path+"/credential", map[string]string{"environment": "sandbox"}, nil, auth)
	a.expect(w, 404)
	if !strings.Contains(w.Body.String(), "ENTITLEMENT_NOT_PURCHASED") || w.Header().Get("Retry-After") != "" {
		a.t.Fatal("unpaid checkout received issuance retry")
	}
}
