package httpapi_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"licentivo-launch/internal/app"
	"licentivo-launch/internal/httpapi"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/storage"
)

type providerFixtureTransport func(*http.Request) (*http.Response, error)

func (f providerFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type providerFixturePayment struct {
	provider, id, order, operation, page    string
	minor                                   int64
	paid                                    bool
	creates, refundCreates                  int
	refundID, refundOperation, refundStatus string
}
type providerFixtureAPI struct {
	t        *testing.T
	private  *rsa.PrivateKey
	payments map[string]*providerFixturePayment
	count    int
	calls    []string
}

const fixtureSeller = "slr_01k1e8w9df9pz73s399svdkxtg"
const fixtureProduct = "pro_01gsz4vmqbjk3x4vvtafffd540"

func fixtureSignature(t *testing.T, key *rsa.PrivateKey, raw []byte) string {
	t.Helper()
	sum := sha256.Sum256(raw)
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(sig)
}
func (f *providerFixtureAPI) response(data any, waffo bool) (*http.Response, error) {
	raw, _ := json.Marshal(map[string]any{"data": data, "code": "0"})
	r := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(raw))}
	if waffo {
		r.Header.Set("X-SIGNATURE", fixtureSignature(f.t, f.private, raw))
	}
	return r, nil
}
func (f *providerFixtureAPI) transaction(p *providerFixturePayment) map[string]any {
	status := "draft"
	var payments []any
	if p.paid {
		status = "completed"
		payments = []any{map[string]any{"status": "captured", "amount": strconv.FormatInt(p.minor, 10), "captured_at": time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)}}
	}
	return map[string]any{"id": p.id, "status": status, "currency_code": "USD", "collection_mode": "automatic", "custom_data": map[string]string{"launch_order_id": p.order, "launch_operation_id": p.operation, "launch_merchant_ref": fixtureSeller, "launch_environment": "sandbox"}, "checkout": map[string]string{"url": p.page + "?_ptxn=" + p.id}, "details": map[string]any{"totals": map[string]string{"total": strconv.FormatInt(p.minor, 10), "grand_total": strconv.FormatInt(p.minor, 10), "credit": "0"}}, "payments": payments}
}
func (f *providerFixtureAPI) adjustment(p *providerFixturePayment) map[string]any {
	return map[string]any{"id": p.refundID, "transaction_id": p.id, "currency_code": "USD", "action": "refund", "type": "full", "reason": "Licentivo Launch full refund / " + p.refundOperation, "status": p.refundStatus, "totals": map[string]string{"total": strconv.FormatInt(p.minor, 10)}}
}
func (f *providerFixtureAPI) waffoOrder(p *providerFixturePayment) map[string]any {
	status := "PAY_IN_PROGRESS"
	if p.paid {
		status = "PAY_SUCCESS"
	}
	action, _ := json.Marshal(map[string]string{"actionType": "REDIRECT", "webUrl": "https://cashier-sandbox.waffo.com/pay/" + p.id})
	return map[string]any{"acquiringOrderId": p.id, "merchantOrderId": p.order, "paymentRequestId": strings.ReplaceAll(p.operation, "-", ""), "orderCurrency": "USD", "orderAmount": fmt.Sprintf("%d.%02d", p.minor/100, p.minor%100), "merchantInfo": map[string]string{"merchantId": "fixture_merchant"}, "orderStatus": status, "orderCompletedAt": time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano), "orderAction": string(action)}
}
func (f *providerFixtureAPI) roundTrip(r *http.Request) (*http.Response, error) {
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	var body map[string]json.RawMessage
	if r.Body != nil {
		raw, _ := io.ReadAll(r.Body)
		if len(raw) > 0 && json.Unmarshal(raw, &body) != nil {
			f.t.Fatal("invalid fixture API JSON")
		}
	}
	str := func(key string) string { var v string; _ = json.Unmarshal(body[key], &v); return v }
	waffo := r.URL.Host == "api-sandbox.waffo.com"
	if !waffo && r.URL.Host != "sandbox-api.paddle.com" {
		f.t.Fatalf("unexpected external host: %s", r.URL.Host)
	}
	if waffo {
		switch r.URL.Path {
		case "/api/v1/merchantconfig/inquiry":
			return f.response(map[string]string{"merchantId": "fixture_merchant"}, true)
		case "/api/v1/order/create":
			for _, p := range f.payments {
				if p.operation == str("paymentRequestId") {
					p.creates++
					return nil, errors.New("unexpected duplicate Waffo create")
				}
			}
			f.count++
			minor, _ := strconv.ParseInt(strings.ReplaceAll(str("orderAmount"), ".", ""), 10, 64)
			p := &providerFixturePayment{provider: "waffo", id: fmt.Sprintf("A%d", f.count), order: str("merchantOrderId"), operation: str("paymentRequestId"), minor: minor, creates: 1}
			f.payments[p.id] = p
			return nil, errors.New("checkout committed before transport timeout")
		case "/api/v1/order/inquiry":
			p := f.payments[str("acquiringOrderId")]
			if p == nil {
				for _, candidate := range f.payments {
					if strings.ReplaceAll(candidate.operation, "-", "") == str("paymentRequestId") {
						p = candidate
						break
					}
				}
			}
			if p == nil {
				return nil, errors.New("unknown Waffo payment")
			}
			return f.response(f.waffoOrder(p), true)
		case "/api/v1/order/refund":
			p := f.payments[str("acquiringOrderId")]
			p.refundCreates++
			p.refundID, p.refundOperation, p.refundStatus = str("refundRequestId"), str("refundRequestId"), "REFUND_IN_PROGRESS"
			return f.response(map[string]string{"refundRequestId": p.refundID, "acquiringOrderId": p.id, "refundStatus": p.refundStatus}, true)
		case "/api/v1/refund/inquiry":
			for _, p := range f.payments {
				if p.refundID == str("refundRequestId") {
					return f.response(map[string]string{"refundRequestId": p.refundID, "acquiringOrderId": p.id, "refundAmount": fmt.Sprintf("%d.%02d", p.minor/100, p.minor%100), "refundStatus": p.refundStatus}, true)
				}
			}
		}
	} else {
		switch r.Method + " " + r.URL.Path {
		case "GET /":
			return f.response(map[string]any{"api": map[string]string{"environment": "sandbox"}, "caller": map[string]any{"auth_type": "api_key", "account": map[string]string{"id": fixtureSeller}, "permissions": []string{"transaction.read", "transaction.write", "adjustment.read", "adjustment.write", "product.read"}}}, false)
		case "GET /products/" + fixtureProduct:
			return f.response(map[string]string{"id": fixtureProduct, "status": "active"}, false)
		case "POST /transactions":
			var in struct {
				Custom     struct{ Order, Operation string } `json:"unused"`
				CustomData map[string]string                 `json:"custom_data"`
				Checkout   struct {
					URL string `json:"url"`
				} `json:"checkout"`
				Items []struct {
					Price struct {
						Unit struct {
							Amount string `json:"amount"`
						} `json:"unit_price"`
					} `json:"price"`
				} `json:"items"`
			}
			raw, _ := json.Marshal(body)
			_ = json.Unmarshal(raw, &in)
			f.count++
			minor, _ := strconv.ParseInt(in.Items[0].Price.Unit.Amount, 10, 64)
			p := &providerFixturePayment{provider: "paddle", id: fmt.Sprintf("txn_%026d", f.count), order: in.CustomData["launch_order_id"], operation: in.CustomData["launch_operation_id"], page: in.Checkout.URL, minor: minor, creates: 1}
			f.payments[p.id] = p
			return nil, errors.New("transaction committed before response timeout")
		case "GET /transactions":
			var rows []any
			for _, p := range f.payments {
				if p.provider == "paddle" {
					rows = append(rows, f.transaction(p))
				}
			}
			return f.response(rows, false)
		case "POST /adjustments":
			p := f.payments[str("transaction_id")]
			p.refundCreates++
			p.refundID = fmt.Sprintf("adj_%026d", f.count)
			p.refundOperation = strings.TrimPrefix(str("reason"), "Licentivo Launch full refund / ")
			p.refundStatus = "pending_approval"
			return nil, errors.New("refund committed before response timeout")
		case "GET /adjustments":
			var rows []any
			if p := f.payments[r.URL.Query().Get("transaction_id")]; p != nil && p.refundID != "" {
				rows = append(rows, f.adjustment(p))
			}
			return f.response(rows, false)
		default:
			if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/transactions/") {
				if p := f.payments[strings.TrimPrefix(r.URL.Path, "/transactions/")]; p != nil {
					return f.response(f.transaction(p), false)
				}
			}
			if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/adjustments/") {
				for _, p := range f.payments {
					if p.refundID == strings.TrimPrefix(r.URL.Path, "/adjustments/") {
						return f.response(f.adjustment(p), false)
					}
				}
			}
		}
	}
	f.t.Fatalf("unexpected payment fixture request %s %s", r.Method, r.URL.Path)
	return nil, errors.New("unexpected request")
}

type uniqueProviderLicenses struct{ *licenseDouble }

func (l *uniqueProviderLicenses) Apply(ctx context.Context, c ports.EntitlementCommand) (ports.EntitlementResult, error) {
	result, err := l.licenseDouble.Apply(ctx, c)
	if err != nil {
		return result, err
	}
	sum := sha256.Sum256([]byte(c.CustomerRef))
	s := hex.EncodeToString(sum[:])
	result.LicenseID = s[:8] + "-" + s[8:12] + "-4" + s[13:16] + "-8" + s[17:20] + "-" + s[20:32]
	l.results[c.OperationID] = result
	return result, nil
}

func TestNewPaymentProvidersAndRegistrationWithoutMail(t *testing.T) {
	dsn := os.Getenv("LAUNCH_TEST_PROVIDER_DATABASE_DSN")
	if dsn == "" {
		t.Skip("set LAUNCH_TEST_PROVIDER_DATABASE_DSN to a fresh launch_test_* database")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(cfg.DBName, "launch_test_") {
		t.Fatal("provider tests require an isolated database")
	}
	db, err := storage.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close(db)
	ctx := context.Background()
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE()").Scan(&count).Error; err != nil || count != 0 {
		t.Fatal("provider tests require a fresh database")
	}
	if err := storage.Initialize(ctx, db); err != nil {
		t.Fatal(err)
	}
	merchant, _ := rsa.GenerateKey(rand.Reader, 2048)
	server, _ := rsa.GenerateKey(rand.Reader, 2048)
	private, _ := x509.MarshalPKCS8PrivateKey(merchant)
	public, _ := x509.MarshalPKIXPublicKey(&server.PublicKey)
	api := &providerFixtureAPI{t: t, private: server, payments: map[string]*providerFixturePayment{}}
	previous := http.DefaultTransport
	http.DefaultTransport = providerFixtureTransport(api.roundTrip)
	defer func() { http.DefaultTransport = previous }()
	licensing := &uniqueProviderLicenses{&licenseDouble{results: map[string]ports.EntitlementResult{}, requests: map[string]string{}}}
	mailRecorder := &recordingMailSender{}
	config := app.Config{PublicURL: "https://merchant.example", MasterKey: bytes.Repeat([]byte{39}, 32), UploadDir: t.TempDir(), MailFactory: mailRecorder.factory, LicensingFactory: func(context.Context, storage.LicentivoConnection) (ports.LicensingProvider, error) {
		return licensing, nil
	}, LicensingSetupFactory: func(string) (app.LicentivoSetupProvider, error) { return licensing, nil }}
	var service *app.Service
	a := &testApp{t: t, db: db, key: config.MasterKey}
	restart := func() {
		var err error
		service, err = app.New(ctx, db, config)
		if err != nil {
			t.Fatal(err)
		}
		a.handler = httpapi.Handler(httpapi.Options{App: service, PublicURL: config.PublicURL, SecureCookies: true})
	}
	restart()
	if _, err := service.EnsureInitialAdministrator(ctx, "owner@example.invalid", "owner-provider-test-password"); err != nil {
		t.Fatal(err)
	}
	admin := &browser{}
	login := map[string]string{"email": "owner@example.invalid", "password": "owner-provider-test-password"}
	a.expect(a.request("POST", "/api/v1/auth/login", login, admin), 200)
	a.expect(a.request("POST", "/api/v1/me/password", map[string]string{"current_password": login["password"], "new_password": "owner-provider-password-changed"}, admin), 200)
	login["password"] = "owner-provider-password-changed"
	a.expect(a.request("POST", "/api/v1/auth/login", login, admin), 200)
	product := data[app.ProductView](t, a.request("PUT", "/api/v1/admin/product", app.ProductView{Name: "Provider integration product", Slug: "provider-integration", Status: "published", Content: app.ProductContent{Summary: "Test checkout and refunds"}}, admin))
	unit, period := "month", uint64(1)
	plan := data[app.PlanView](t, a.request("POST", "/api/v1/admin/plans", app.PlanView{ProductID: product.ID, Name: "Pro", Currency: "USD", AmountMinor: 1299, PurchaseType: "fixed_term", TermUnit: &unit, TermCount: &period, AllowPurchase: true, AllowRenewal: true}, admin))
	a.expect(a.request("POST", "/api/v1/admin/connections/licentivo", app.LicenseSettings{APIKey: "lv_api_test"}, admin), 200)
	a.expect(a.request("POST", "/api/v1/admin/connections/mappings", app.MappingInput{VersionID: plan.VersionID, PolicyID: "22222222-2222-4222-8222-222222222222"}, admin), 200)
	settings := []app.PaymentSettings{{Provider: "waffo", Environment: "sandbox", MerchantRef: "fixture_merchant", APIKey: "waffo_fixture_key", PrivateKey: base64.StdEncoding.EncodeToString(private), PublicKey: base64.StdEncoding.EncodeToString(public)}, {Provider: "paddle", Environment: "sandbox", MerchantRef: fixtureSeller, APIKey: "pdl_sdbx_apikey_fixture", WebhookSecret: "pdl_ntfset_fixture", ClientToken: "test_fixture", ProductID: fixtureProduct}}
	for _, setting := range settings {
		a.expect(a.request("POST", "/api/v1/admin/connections/payments", setting, admin), 200)
	}
	a.expect(a.request("POST", "/api/v1/admin/plans/"+plan.ID+"/publish", map[string]string{"version_id": plan.VersionID, "environment": "sandbox"}, admin), 200)
	site := data[app.SiteView](t, a.request("GET", "/api/v1/site?environment=sandbox", nil, nil))
	if site.EmailVerificationRequired || len(site.PaymentProviders) != 2 {
		t.Fatal("optional registration or new provider catalog was not available")
	}
	a.expect(a.request("POST", "/api/v1/auth/email", map[string]string{"email": "buyer@example.invalid", "kind": "reset"}, nil), 409)
	for caseIndex, provider := range []string{"waffo", "paddle", "paddle"} {
		t.Run(fmt.Sprintf("%s-%d", provider, caseIndex), func(t *testing.T) {
			parent := a.t
			a.t = t
			defer func() { a.t = parent }()
			drainAuthorization := func() {
				for range 12 {
					handled, err := service.ProcessAuthorization(ctx)
					if err != nil {
						t.Fatal(err)
					}
					if !handled {
						return
					}
				}
				t.Fatal("authorization queue did not drain")
			}
			customer := &browser{}
			credentials := map[string]string{"email": fmt.Sprintf("buyer%d@example.invalid", caseIndex), "password": "buyer-provider-password"}
			a.expect(a.request("POST", "/api/v1/auth/register", credentials, nil), 200)
			a.expect(a.request("POST", "/api/v1/auth/login", credentials, customer), 200)
			me := data[struct {
				User app.UserView `json:"user"`
			}](t, a.request("GET", "/api/v1/me", nil, customer))
			if me.User.Verified {
				t.Fatal("unverified email was falsely marked verified")
			}
			input := app.CheckoutInput{ProductID: product.ID, PlanVersionID: plan.VersionID, Provider: provider, Environment: "sandbox", IdempotencyKey: fmt.Sprintf("provider-checkout-key-%d", caseIndex)}
			order := data[app.CheckoutView](t, a.request("POST", "/api/v1/orders", input, customer))
			restart()
			order = data[app.CheckoutView](t, a.request("POST", "/api/v1/orders", input, customer))
			if order.CheckoutURL == nil {
				var row storage.Payment
				db.First(&row, "order_id = ?", order.ID)
				t.Fatalf("checkout did not recover after restart: state=%s attempted=%v calls=%v", row.Status, row.CheckoutAttemptedAt != nil, api.calls)
			}
			var external *providerFixturePayment
			for _, payment := range api.payments {
				if payment.order == order.ID {
					external = payment
				}
			}
			if external == nil || external.creates != 1 {
				t.Fatal("ambiguous checkout was duplicated")
			}
			var payment storage.Payment
			if db.First(&payment, "order_id = ?", order.ID).Error != nil {
				t.Fatal("payment missing")
			}
			if provider == "paddle" {
				page := a.request("GET", "/pay/paddle/"+payment.AccountID+"?_ptxn="+external.id, nil, nil)
				a.expect(page, 200)
				if !strings.Contains(page.Body.String(), "test_fixture") || strings.Contains(page.Body.String(), "pdl_sdbx_apikey_fixture") || !strings.Contains(page.Header().Get("Content-Security-Policy"), "https://cdn.paddle.com") {
					t.Fatal("Paddle page exposed secrets or could not load the official checkout")
				}
			}
			external.paid = true
			webhook := func(refund bool) {
				var body any
				headers := http.Header{}
				if provider == "waffo" {
					kind := "PAYMENT_NOTIFICATION"
					result := map[string]string{"acquiringOrderId": external.id, "orderUpdatedAt": time.Now().UTC().Format(time.RFC3339Nano)}
					if refund {
						kind = "REFUND_NOTIFICATION"
						result["refundRequestId"] = external.refundID
					}
					body = map[string]any{"eventType": kind, "result": result}
					raw, _ := json.Marshal(body)
					headers.Set("X-SIGNATURE", fixtureSignature(t, server, raw))
				} else {
					kind, id, number := "transaction.completed", external.id, caseIndex+1
					if refund {
						kind, id, number = "adjustment.updated", external.refundID, caseIndex+100
					}
					body = map[string]any{"event_id": fmt.Sprintf("evt_%026d", number), "event_type": kind, "occurred_at": time.Now().UTC().Format(time.RFC3339Nano), "data": map[string]string{"id": id}}
					raw, _ := json.Marshal(body)
					stamp := strconv.FormatInt(time.Now().Unix(), 10)
					mac := hmac.New(sha256.New, []byte("pdl_ntfset_fixture"))
					mac.Write([]byte(stamp + ":"))
					mac.Write(raw)
					headers.Set("Paddle-Signature", "ts="+stamp+";h1="+hex.EncodeToString(mac.Sum(nil)))
				}
				for range 3 {
					reply := a.request("POST", "/api/v1/webhooks/payments/"+payment.AccountID, body, nil, func(r *http.Request) {
						for name, values := range headers {
							r.Header[name] = values
						}
					})
					a.expect(reply, 200)
					if provider == "waffo" && !strings.Contains(reply.Body.String(), `"message":"success"`) {
						t.Fatal("Waffo acknowledgement used the application JSON envelope")
					}
				}
				if handled, err := service.ProcessPaymentEvent(ctx); err != nil || !handled {
					t.Fatalf("callback was not reconciled: %v", err)
				}
			}
			webhook(false)
			paid := data[app.CheckoutView](t, a.request("GET", "/api/v1/orders/"+order.ID, nil, customer))
			if paid.Status != "paid" {
				t.Fatal("verified payment did not settle")
			}
			drainAuthorization()
			desktop := map[string]string{"email": credentials["email"], "password": credentials["password"], "kind": "desktop"}
			grant := data[app.SessionGrant](t, a.request("POST", "/api/v1/auth/login", desktop, nil, func(r *http.Request) { r.Header.Del("Origin") }))
			a.expect(a.request("POST", "/api/v1/software/products/"+product.ID+"/credential", map[string]string{"environment": "sandbox"}, nil, func(r *http.Request) {
				r.Header.Del("Origin")
				r.Header.Set("Authorization", "Bearer "+grant.AccessToken)
			}), 200)
			refund := data[app.RefundView](t, a.request("POST", "/api/v1/orders/"+order.ID+"/refund", map[string]string{"reason": "Test full refund"}, customer))
			a.expect(a.request("POST", "/api/v1/admin/refunds/"+refund.ID+"/approve", map[string]string{"reason": "Approved integration test"}, admin), 200)
			if err := service.ProcessRefunds(ctx); err != nil {
				t.Fatal(err)
			}
			restart()
			db.Model(&storage.Refund{}).Where("id = ?", refund.ID).Update("next_check_at", time.Now().Add(-time.Minute))
			if err := service.ProcessRefunds(ctx); err != nil {
				t.Fatal(err)
			}
			var pending storage.Refund
			if db.First(&pending, "id = ?", refund.ID).Error != nil || pending.ExternalRefundID == nil || pending.Status != "pending" || external.refundCreates != 1 {
				t.Fatalf("refund recovery failed: %+v", pending)
			}
			db.Model(&pending).Updates(map[string]any{"attempts": 20, "next_check_at": time.Now().Add(-time.Minute)})
			if err := service.ProcessRefunds(ctx); err != nil {
				t.Fatal(err)
			}
			db.First(&pending, "id = ?", refund.ID)
			if pending.ReviewRequired || pending.Status != "pending" {
				t.Fatal("known approval queue was incorrectly exhausted")
			}
			if caseIndex == 2 {
				external.refundStatus = "rejected"
				db.Model(&pending).Update("next_check_at", time.Now().Add(-time.Minute))
				if err := service.ProcessRefunds(ctx); err != nil {
					t.Fatal(err)
				}
				db.First(&pending, "id = ?", refund.ID)
				var unchanged storage.Order
				db.First(&unchanged, "id = ?", order.ID)
				if pending.Status != "failed" || unchanged.Status != "paid" {
					t.Fatal("rejected refund did not preserve the paid order")
				}
				return
			}
			if provider == "waffo" {
				external.refundStatus = "ORDER_FULLY_REFUNDED"
			} else {
				external.refundStatus = "approved"
			}
			webhook(true)
			if err := service.ProcessRefunds(ctx); err != nil {
				t.Fatal(err)
			}
			final := data[app.CheckoutView](t, a.request("GET", "/api/v1/orders/"+order.ID, nil, customer))
			if final.Status != "refunded" {
				t.Fatal("confirmed full refund did not update the order")
			}
			var coverage int64
			db.Model(&storage.EntitlementCoverage{}).Where("source_order_id = ? AND voided_at IS NULL", order.ID).Count(&coverage)
			if coverage != 0 {
				t.Fatal("refunded coverage remained active")
			}
			drainAuthorization()
		})
	}
	db.Model(&storage.EmailOutbox{}).Count(&count)
	if count != 0 {
		t.Fatal("disabled email accumulated undeliverable messages")
	}
	smtp := app.MailSettingsInput{Provider: "smtp", Host: "smtp.example.invalid", Port: "2525", Username: "merchant", APIKey: "ordinary-smtp-password", From: "mail@example.invalid", Encryption: "starttls"}
	a.expect(a.request("POST", "/api/v1/admin/mail/settings", smtp, admin), 200)
	a.expect(a.request("POST", "/api/v1/admin/mail/check", app.MailCheckInput{To: "owner@example.invalid"}, admin), 200)
	view := data[app.MailSettingsView](t, a.request("GET", "/api/v1/admin/mail/settings", nil, admin))
	if !view.Configured || view.Provider != "smtp" || view.Port != "2525" {
		t.Fatal("SMTP still required Resend settings")
	}
	queued := map[string]string{"email": "mail-enabled@example.invalid", "password": "mail-enabled-password"}
	a.expect(a.request("POST", "/api/v1/auth/register", queued, nil), 200)
	var credentials int64
	db.Model(&storage.EmailCredential{}).Count(&credentials)
	if credentials != 1 {
		t.Fatal("enabled mail did not queue verification")
	}
	mailCustomer := &browser{}
	a.expect(a.request("POST", "/api/v1/auth/login", queued, mailCustomer), 200)
	mailCheckout := app.CheckoutInput{ProductID: product.ID, PlanVersionID: plan.VersionID, Provider: "paddle", Environment: "sandbox", IdempotencyKey: "mail-provider-checkout-key"}
	a.expect(a.request("POST", "/api/v1/orders", mailCheckout, mailCustomer), 403)
	a.expect(a.request("POST", "/api/v1/auth/complete-email", map[string]string{"token": a.mailToken("verify", queued["email"])}, nil), 200)
	me := data[struct {
		User app.UserView `json:"user"`
	}](t, a.request("GET", "/api/v1/me", nil, mailCustomer))
	if !me.User.Verified {
		t.Fatal("verification link did not verify the account")
	}
	no := false
	smtp.RequireVerification, smtp.OrderNotifications, smtp.APIKey = &no, &no, ""
	a.expect(a.request("POST", "/api/v1/admin/mail/settings", smtp, admin), 200)
	restart()
	site = data[app.SiteView](t, a.request("GET", "/api/v1/site", nil, nil))
	view = data[app.MailSettingsView](t, a.request("GET", "/api/v1/admin/mail/settings", nil, admin))
	if !site.MailEnabled || site.EmailVerificationRequired || view.RequireVerification || view.OrderNotifications {
		t.Fatal("email preferences did not survive restart")
	}
	direct := map[string]string{"email": "direct-with-mail@example.invalid", "password": "direct-with-mail-password"}
	a.expect(a.request("POST", "/api/v1/auth/register", direct, nil), 200)
	a.expect(a.request("POST", "/api/v1/auth/email", map[string]string{"email": direct["email"], "kind": "verify"}, nil), 200)
	db.Model(&storage.EmailCredential{}).Where("kind = 'verify'").Count(&credentials)
	if credentials != 1 {
		t.Fatal("disabled verification still queued a signup or resend link")
	}
	a.expect(a.request("POST", "/api/v1/auth/email", map[string]string{"email": direct["email"], "kind": "reset"}, nil), 200)
	db.Model(&storage.EmailCredential{}).Where("kind = 'reset'").Count(&credentials)
	if credentials != 1 {
		t.Fatal("turning off verification disabled password recovery")
	}
	// Payment and authorization notifications can be enabled independently.
	yes := true
	smtp.OrderNotifications = &yes
	a.expect(a.request("POST", "/api/v1/admin/mail/settings", smtp, admin), 200)
	directCustomer := &browser{}
	a.expect(a.request("POST", "/api/v1/auth/login", direct, directCustomer), 200)
	mailCheckout.IdempotencyKey = "direct-mail-checkout-key"
	mailOrder := data[app.CheckoutView](t, a.request("POST", "/api/v1/orders", mailCheckout, directCustomer))
	mailOrder = data[app.CheckoutView](t, a.request("POST", "/api/v1/orders", mailCheckout, directCustomer))
	for _, payment := range api.payments {
		if payment.order == mailOrder.ID {
			payment.paid = true
		}
	}
	mailOrder = data[app.CheckoutView](t, a.request("POST", "/api/v1/orders/"+mailOrder.ID+"/reconcile", map[string]string{}, directCustomer))
	if mailOrder.Status != "paid" {
		t.Fatal("optional email verification still blocked payment")
	}
	if handled, err := service.ProcessAuthorization(ctx); err != nil || !handled {
		t.Fatalf("notification authorization: %v", err)
	}
	for range 8 {
		handled, err := service.ProcessMail(ctx, mailRecorder)
		if err != nil {
			t.Fatal(err)
		}
		if !handled {
			break
		}
	}
	var transactionMessages int
	for _, message := range mailRecorder.messages {
		if message.To == direct["email"] && (message.Subject == "Payment confirmed" || message.Subject == "Your software access is ready") {
			transactionMessages++
		}
	}
	if transactionMessages != 2 {
		t.Fatalf("expected payment and access messages, got %d", transactionMessages)
	}
	a.expect(a.request("POST", "/api/v1/admin/mail/settings", app.MailSettingsInput{Provider: "disabled"}, admin), 200)
	site = data[app.SiteView](t, a.request("GET", "/api/v1/site", nil, nil))
	if site.EmailVerificationRequired {
		t.Fatal("mail disabling did not restore direct registration")
	}
}
