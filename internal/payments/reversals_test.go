package payments

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"licentivo-launch/internal/domain"
	"licentivo-launch/internal/ports"
)

func TestStripeReversalRequiresLostDisputeAndMatchingCapture(t *testing.T) {
	p, err := NewStripe(StripeConfig{APIKey: "sk_test_fixture", WebhookSecret: "whsec_fixture", MerchantRef: "acct_fixture", APIVersion: "2025-02-24.acacia", Environment: ports.Sandbox})
	if err != nil {
		t.Fatal(err)
	}
	status, live, currency := "lost", false, "usd"
	p.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer sk_test_fixture" {
			t.Fatal("unexpected financial write or missing authentication")
		}
		switch r.URL.Path {
		case "/v1/disputes/dp_fixture":
			raw, _ := json.Marshal(map[string]any{"id": "dp_fixture", "status": status, "livemode": live, "currency": currency, "amount": 1000, "payment_intent": "pi_fixture"})
			return response(200, string(raw)), nil
		case "/v1/payment_intents/pi_fixture":
			if r.URL.Query().Get("expand[]") != "latest_charge" {
				t.Fatal("original capture not expanded")
			}
			return response(200, `{"id":"pi_fixture","currency":"usd","amount_received":1000,"metadata":{"launch_order_id":"local_order"},"latest_charge":{"captured":true,"created":1800000000}}`), nil
		default:
			t.Fatal("unexpected query", r.URL.Path)
			return nil, ErrVerification
		}
	})
	event := ports.VerifiedEvent{Kind: "charge.dispute.closed", ResourceID: "dp_fixture"}
	for _, value := range []string{"needs_response", "under_review", "won", "lost"} {
		status = value
		got, err := p.QueryPaymentReversal(context.Background(), event)
		if err != nil || got.Confirmed != (value == "lost") || got.PaymentID != "pi_fixture" || got.OriginalAmount.Minor != 1000 || got.PaidAt == nil {
			t.Fatalf("dispute %s: %+v %v", value, got, err)
		}
	}
	live = true
	if _, err := p.QueryPaymentReversal(context.Background(), event); !errors.Is(err, ErrVerification) {
		t.Fatal("cross-environment dispute accepted")
	}
	live = false
	currency = "eur"
	if _, err := p.QueryPaymentReversal(context.Background(), event); !errors.Is(err, ErrVerification) {
		t.Fatal("currency mismatch accepted")
	}
}

func TestPayPalReversalKeepsCaptureIDAndBindsOriginalMerchant(t *testing.T) {
	p, err := NewPayPal(PayPalConfig{ClientID: "client", ClientSecret: "secret", MerchantRef: "merchant", WebhookID: "webhook", Environment: ports.Sandbox})
	if err != nil {
		t.Fatal(err)
	}
	merchant := "merchant"
	p.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/oauth2/token":
			return response(200, `{"access_token":"token","expires_in":300}`), nil
		case "POST /v1/notifications/verify-webhook-signature":
			return response(200, `{"verification_status":"SUCCESS"}`), nil
		case "GET /v2/payments/captures/capture_fixture":
			raw, _ := json.Marshal(map[string]any{"id": "capture_fixture", "status": "COMPLETED", "payee": map[string]string{"merchant_id": merchant}, "amount": map[string]string{"currency_code": "USD", "value": "10.00"}, "create_time": "2026-10-15T01:00:00Z", "supplementary_data": map[string]any{"related_ids": map[string]string{"order_id": "order_fixture"}}})
			return response(200, string(raw)), nil
		case "GET /v2/checkout/orders/order_fixture":
			return response(200, `{"id":"order_fixture","purchase_units":[{"custom_id":"local_order","payee":{"merchant_id":"merchant"}}]}`), nil
		default:
			t.Fatal("unexpected query", r.Method, r.URL.Path)
			return nil, ErrVerification
		}
	})
	headers := http.Header{}
	for _, key := range []string{"Paypal-Auth-Algo", "Paypal-Cert-Url", "Paypal-Transmission-Id", "Paypal-Transmission-Sig", "Paypal-Transmission-Time"} {
		headers.Set(key, "fixture")
	}
	body := []byte(`{"id":"evt_fixture","event_type":"PAYMENT.CAPTURE.REVERSED","create_time":"2026-10-16T01:00:00Z","resource":{"id":"capture_fixture","amount":{"currency_code":"USD","value":"10.00"},"supplementary_data":{"related_ids":{"order_id":"order_fixture"}}}}`)
	event, err := p.VerifyWebhook(context.Background(), headers, body)
	if err != nil || event.ResourceID != "capture_fixture" || event.ReversalAmount == nil {
		t.Fatal("reversal normalized to checkout instead of capture", event, err)
	}
	got, err := p.QueryPaymentReversal(context.Background(), event)
	if err != nil || !got.Confirmed || got.CheckoutID != "order_fixture" || got.OrderID != "local_order" {
		t.Fatal("original capture not bound", got, err)
	}
	merchant = "foreign"
	if _, err := p.QueryPaymentReversal(context.Background(), event); !errors.Is(err, ErrVerification) {
		t.Fatal("foreign capture accepted")
	}
	merchant = "merchant"
	event.ReversalAmount = &domain.Money{Currency: "EUR", Minor: 1000}
	if _, err := p.QueryPaymentReversal(context.Background(), event); !errors.Is(err, ErrVerification) {
		t.Fatal("foreign currency accepted")
	}
}

func TestPaddleChargebackIsSeparateFromRefundAndWarning(t *testing.T) {
	p := newPaddleFixture(t)
	action, status := "chargeback", "approved"
	p.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			t.Fatal("reversal attempted financial write")
		}
		switch r.URL.Path {
		case "/adjustments/" + paddleAdj:
			raw, _ := json.Marshal(map[string]any{"data": map[string]any{"id": paddleAdj, "transaction_id": paddleTxn, "action": action, "status": status, "currency_code": "USD", "totals": map[string]string{"total": "1000"}}})
			return response(200, string(raw)), nil
		case "/transactions/" + paddleTxn:
			raw, _ := json.Marshal(map[string]any{"data": map[string]any{"id": paddleTxn, "status": "completed", "currency_code": "USD", "collection_mode": "automatic", "custom_data": paddleMetadata{"local_order", "stable_operation", paddleSeller, "sandbox"}, "details": map[string]any{"totals": map[string]string{"total": "1000", "grand_total": "1000", "credit": "0"}}, "payments": []any{map[string]string{"status": "captured", "amount": "1000", "captured_at": "2026-10-15T01:00:00Z"}}}})
			return response(200, string(raw)), nil
		default:
			t.Fatal("unexpected query", r.URL.Path)
			return nil, ErrVerification
		}
	})
	event := ports.VerifiedEvent{Kind: "adjustment.created", ResourceID: paddleAdj}
	got, err := p.QueryPaymentReversal(context.Background(), event)
	if err != nil || !got.Confirmed || got.PaymentID != paddleTxn || got.Amount.Minor != 1000 {
		t.Fatal("approved chargeback rejected", got, err)
	}
	status = "pending_approval"
	if got, err := p.QueryPaymentReversal(context.Background(), event); err != nil || got.Confirmed {
		t.Fatal("pending chargeback confirmed", err)
	}
	action = "chargeback_warning"
	if got, err := p.QueryPaymentReversal(context.Background(), event); err != nil || got.Confirmed {
		t.Fatal("warning removed purchase", err)
	}
	action = "refund"
	if _, err := p.QueryPaymentReversal(context.Background(), event); !errors.Is(err, ErrNotReversal) {
		t.Fatal("normal refund routed as reversal", err)
	}
}
