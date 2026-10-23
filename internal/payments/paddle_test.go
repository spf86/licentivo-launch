package payments

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"licentivo-launch/internal/domain"
	"licentivo-launch/internal/ports"
)

const paddleTxn = "txn_01h0j589qt1nee24210teqtz57"
const paddleSeller = "slr_01k1e8w9df9pz73s399svdkxtg"
const paddleProduct = "pro_01gsz4vmqbjk3x4vvtafffd540"
const paddleAdj = "adj_01hvcc94b7qgz60qmrqmbm19zw"

func newPaddleFixture(t *testing.T) *Paddle {
	t.Helper()
	p, err := NewPaddle(PaddleConfig{APIKey: "pdl_sdbx_apikey_fixture", ClientToken: "test_fixture", WebhookSecret: "pdl_ntfset_fixture", MerchantRef: paddleSeller, ProductID: paddleProduct, Environment: ports.Sandbox})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestPaddleCheckoutCapturedPaymentRefundApprovalAndRecovery(t *testing.T) {
	p := newPaddleFixture(t)
	ctx := context.Background()
	status, refundStatus := "completed", "pending_approval"
	merchant := paddleSeller
	total := "1699"
	creates, refundCreates := 0, 0
	in := ports.CheckoutRequest{OperationID: "stable_operation", OrderID: "local_order", Environment: ports.Sandbox, Amount: domain.Money{Currency: "USD", Minor: 1699}, Description: "Pro", HostedCheckoutURL: "https://merchant.invalid/pay/paddle/account", RequestedAt: time.Now().UTC()}
	transaction := func() map[string]any {
		return map[string]any{"id": paddleTxn, "status": status, "currency_code": "USD", "collection_mode": "automatic", "custom_data": paddleMetadata{in.OrderID, in.OperationID, merchant, "sandbox"}, "checkout": map[string]string{"url": in.HostedCheckoutURL + "?_ptxn=" + paddleTxn}, "details": map[string]any{"totals": map[string]string{"total": total, "grand_total": total, "credit": "0"}}, "payments": []any{map[string]any{"status": "captured", "amount": "1699", "captured_at": "2026-10-22T10:00:00Z"}}}
	}
	adjustment := func() map[string]any {
		return map[string]any{"id": paddleAdj, "transaction_id": paddleTxn, "action": "refund", "type": "full", "reason": paddleRefundReason("stable_refund"), "currency_code": "USD", "status": refundStatus, "totals": map[string]string{"total": "1699"}}
	}
	p.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "sandbox-api.paddle.com" || r.Header.Get("Authorization") != "Bearer pdl_sdbx_apikey_fixture" || r.Header.Get("Paddle-Version") != "1" {
			t.Fatal("wrong Paddle authentication or environment")
		}
		if r.Header.Get("Idempotency-Key") != "" {
			t.Fatal("invented Paddle idempotency guarantee")
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /":
			return response(200, string(encoded(map[string]any{"data": map[string]any{"api": map[string]string{"environment": "sandbox"}, "caller": map[string]any{"auth_type": "api_key", "account": map[string]string{"id": paddleSeller}, "permissions": []string{"transaction.read", "transaction.write", "adjustment.read", "adjustment.write", "product.read"}}}}))), nil
		case "GET /products/" + paddleProduct:
			return response(200, `{"data":{"id":"`+paddleProduct+`","status":"active"}}`), nil
		case "POST /transactions":
			creates++
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			price := body["items"].([]any)[0].(map[string]any)["price"].(map[string]any)
			if price["tax_mode"] != "internal" || price["billing_cycle"] != nil || price["product_id"] != paddleProduct || price["unit_price"].(map[string]any)["amount"] != "1699" {
				t.Fatal("Paddle price was not fixed, tax-inclusive and one-time")
			}
			return response(201, string(encoded(map[string]any{"data": transaction()}))), nil
		case "GET /transactions/" + paddleTxn:
			return response(200, string(encoded(map[string]any{"data": transaction()}))), nil
		case "GET /transactions":
			if !strings.Contains(r.URL.Query().Get("created_at[GTE]"), "T") {
				t.Fatal("recovery did not bound its search")
			}
			return response(200, string(encoded(map[string]any{"data": []any{transaction()}, "meta": map[string]any{"pagination": map[string]any{"has_more": false}}}))), nil
		case "POST /adjustments":
			refundCreates++
			raw, _ := io.ReadAll(r.Body)
			var body map[string]string
			_ = json.Unmarshal(raw, &body)
			if body["type"] != "full" || body["transaction_id"] != paddleTxn || body["action"] != "refund" || body["reason"] != paddleRefundReason("stable_refund") {
				t.Fatal("refund request not linked to payment and stable operation")
			}
			return response(201, string(encoded(map[string]any{"data": adjustment()}))), nil
		case "GET /adjustments/" + paddleAdj:
			return response(200, string(encoded(map[string]any{"data": adjustment()}))), nil
		case "GET /adjustments":
			if r.URL.Query().Get("transaction_id") != paddleTxn {
				t.Fatal("refund search was not scoped")
			}
			return response(200, string(encoded(map[string]any{"data": []any{adjustment()}}))), nil
		default:
			t.Fatalf("unexpected Paddle request %s %s", r.Method, r.URL.Path)
			return nil, ErrRejected
		}
	})
	if err := p.VerifyAccount(ctx); err != nil {
		t.Fatal(err)
	}
	checkout, err := p.CreateCheckout(ctx, in)
	if err != nil || checkout.ExternalID != paddleTxn {
		t.Fatalf("checkout: %+v %v", checkout, err)
	}
	for range 2 {
		recovered, err := p.RecoverCheckout(ctx, in)
		if err != nil || recovered.ExternalID != checkout.ExternalID {
			t.Fatal("checkout recovery failed")
		}
	}
	if creates != 1 {
		t.Fatal("recovery created a duplicate transaction")
	}
	paid, err := p.QueryPayment(ctx, paddleTxn)
	if err != nil || paid.Status != ports.PaymentSucceeded || paid.Amount.Minor != 1699 || paid.PaidAt == nil {
		t.Fatalf("settlement: %+v %v", paid, err)
	}
	status = "paid"
	paid, err = p.QueryPayment(ctx, paddleTxn)
	if err != nil || paid.Status == ports.PaymentSucceeded {
		t.Fatal("paid-but-unprocessed transaction fulfilled")
	}
	status = "completed"
	request := ports.RefundRequest{OperationID: "stable_refund", PaymentID: paddleTxn, Amount: in.Amount}
	refund, err := p.Refund(ctx, request)
	if err != nil || refund.Status != ports.PaymentPending {
		t.Fatalf("approval pending: %+v %v", refund, err)
	}
	for range 2 {
		if _, err := p.RecoverRefund(ctx, request); err != nil {
			t.Fatal(err)
		}
	}
	if refundCreates != 1 {
		t.Fatal("refund recovery submitted a duplicate")
	}
	refundStatus = "approved"
	refund, err = p.QueryRefund(ctx, paddleAdj)
	if err != nil || refund.Status != ports.PaymentSucceeded || refund.Amount != in.Amount {
		t.Fatal("approved refund not reconciled")
	}
	refundStatus = "rejected"
	refund, err = p.QueryRefund(ctx, paddleAdj)
	if err != nil || refund.Status != ports.PaymentFailed {
		t.Fatal("rejected refund was not a failure")
	}
	merchant = "slr_01k1e8w9df9pz73s399svdkxtx"
	if _, err := p.QueryPayment(ctx, paddleTxn); err != ErrVerification {
		t.Fatal("another merchant was accepted")
	}
	merchant = paddleSeller
	total = "1700"
	if _, err := p.QueryPayment(ctx, paddleTxn); err != ErrVerification {
		t.Fatal("captured amount mismatch was accepted")
	}
}
func TestPaddleWebhookSignatureAndEnvironmentValidation(t *testing.T) {
	p := newPaddleFixture(t)
	body := []byte(`{"event_id":"evt_01h0j589qt1nee24210teqtz57","event_type":"adjustment.updated","occurred_at":"2026-10-22T10:00:00Z","data":{"id":"` + paddleAdj + `"}}`)
	sign := func(at time.Time) http.Header {
		stamp := strconv.FormatInt(at.Unix(), 10)
		mac := hmac.New(sha256.New, []byte(p.config.WebhookSecret))
		mac.Write([]byte(stamp + ":"))
		mac.Write(body)
		h := http.Header{}
		h.Set("Paddle-Signature", "ts="+stamp+";h1="+hex.EncodeToString(mac.Sum(nil)))
		return h
	}
	event, err := p.VerifyWebhook(context.Background(), sign(time.Now()), body)
	if err != nil || event.ResourceID != paddleAdj || event.Kind != "adjustment.updated" {
		t.Fatal("valid refund event rejected")
	}
	for _, offset := range []time.Duration{-time.Minute, time.Minute} {
		if _, err := p.VerifyWebhook(context.Background(), sign(time.Now().Add(offset)), body); err != ErrVerification {
			t.Fatal("stale/future callback accepted")
		}
	}
	if _, err := p.VerifyWebhook(context.Background(), sign(time.Now()), append(body, ' ')); err != ErrVerification {
		t.Fatal("tampered raw body accepted")
	}
	if _, err := p.VerifyWebhook(context.Background(), http.Header{}, body); err != ErrVerification {
		t.Fatal("unsigned callback accepted")
	}
	c := p.config
	c.Environment = ports.Live
	if _, err := NewPaddle(c); err != ErrCredentials {
		t.Fatal("sandbox credentials accepted for live")
	}
	if _, err := p.nextPage(paddlePagination{Pagination: struct {
		Next    string `json:"next"`
		HasMore bool   `json:"has_more"`
	}{Next: "https://evil.invalid/transactions", HasMore: true}}, "/transactions"); err != ErrVerification {
		t.Fatal("pagination could leak the bearer credential")
	}
}
