package payments

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"licentivo-launch/internal/domain"
	"licentivo-launch/internal/ports"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
}

func TestStripeCheckoutVerificationAndRefund(t *testing.T) {
	stripe, err := NewStripe(StripeConfig{APIKey: "sk_test_fixture", WebhookSecret: "whsec_fixture", MerchantRef: "acct_fixture", APIVersion: "2025-02-24.acacia", Environment: ports.Sandbox})
	if err != nil {
		t.Fatal(err)
	}
	stripe.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Stripe-Version") != "2025-02-24.acacia" || r.Header.Get("Authorization") != "Bearer sk_test_fixture" {
			t.Fatal("missing pinned version or authentication")
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/checkout/sessions":
			raw, _ := io.ReadAll(r.Body)
			form, _ := url.ParseQuery(string(raw))
			if form.Get("line_items[0][price_data][unit_amount]") != "1699" || form.Get("payment_method_types[0]") != "card" || form.Get("client_reference_id") != "local_order" || r.Header.Get("Idempotency-Key") != "stable_create" {
				t.Fatal("incorrect checkout contract")
			}
			return response(200, `{"id":"cs_fixture","url":"https://checkout.stripe.com/test","expires_at":2000000000,"livemode":false}`), nil
		case "GET /v1/checkout/sessions/cs_fixture":
			return response(200, `{"id":"cs_fixture","currency":"usd","amount_total":1699,"client_reference_id":"local_order","livemode":false,"payment_status":"paid","payment_intent":{"id":"pi_fixture","status":"succeeded","amount_received":1699,"currency":"usd","latest_charge":{"id":"ch_fixture","captured":true,"created":1800000000}}}`), nil
		case "POST /v1/refunds":
			if r.Header.Get("Idempotency-Key") != "stable_refund" {
				t.Fatal("refund idempotency missing")
			}
			return response(200, `{"id":"re_fixture","payment_intent":"pi_fixture","amount":1699,"currency":"usd","status":"succeeded"}`), nil
		default:
			t.Fatalf("unexpected Stripe request %s %s", r.Method, r.URL.Path)
			return nil, errors.New("unexpected request")
		}
	})
	checkout, err := stripe.CreateCheckout(context.Background(), ports.CheckoutRequest{OperationID: "stable_create", OrderID: "local_order", Environment: ports.Sandbox, Amount: domain.Money{Currency: "USD", Minor: 1699}, ReturnURL: "https://merchant.invalid/return", CancelURL: "https://merchant.invalid/cancel", Description: "Pro"})
	if err != nil || checkout.ExternalID != "cs_fixture" {
		t.Fatalf("checkout: %+v %v", checkout, err)
	}
	payment, err := stripe.QueryPayment(context.Background(), checkout.ExternalID)
	if err != nil || payment.Status != ports.PaymentSucceeded || payment.ExternalID != "pi_fixture" || payment.Amount.Minor != 1699 {
		t.Fatalf("payment: %+v %v", payment, err)
	}
	refund, err := stripe.Refund(context.Background(), ports.RefundRequest{OperationID: "stable_refund", PaymentID: payment.ExternalID, Amount: payment.Amount})
	if err != nil || refund.Status != ports.PaymentSucceeded || refund.PaymentID != payment.ExternalID {
		t.Fatal("refund contract failed")
	}
	stripe.http.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		return response(200, `{"id":"cs_fixture","livemode":true,"client_reference_id":"local_order"}`), nil
	})
	if _, err := stripe.QueryPayment(context.Background(), "cs_fixture"); !errors.Is(err, ErrVerification) {
		t.Fatal("cross-environment response accepted")
	}
}

func TestPayPalApprovalIsNotPaymentAndCaptureIsStable(t *testing.T) {
	paypal, err := NewPayPal(PayPalConfig{ClientID: "client", ClientSecret: "secret", MerchantRef: "merchant", WebhookID: "webhook", Environment: ports.Sandbox})
	if err != nil {
		t.Fatal(err)
	}
	captured := false
	authCalls, captures := 0, 0
	orderBody := func() string {
		status, payments := "APPROVED", ""
		if captured {
			status = "COMPLETED"
			payments = `,"payments":{"captures":[{"id":"capture_fixture","status":"COMPLETED","create_time":"2026-10-15T01:00:00Z","amount":{"currency_code":"USD","value":"16.99"}}]}`
		}
		return `{"id":"order_fixture","status":"` + status + `","purchase_units":[{"custom_id":"local_order","payee":{"merchant_id":"merchant"},"amount":{"currency_code":"USD","value":"16.99"}` + payments + `}]}`
	}
	paypal.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api-m.sandbox.paypal.com" {
			t.Fatal("wrong PayPal environment")
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/oauth2/token":
			authCalls++
			return response(200, `{"access_token":"test_access","expires_in":3600}`), nil
		case "POST /v2/checkout/orders":
			if r.Header.Get("PayPal-Request-Id") != "stable_create" {
				t.Fatal("missing create idempotency")
			}
			return response(201, `{"id":"order_fixture","links":[{"rel":"approve","href":"https://www.sandbox.paypal.com/checkoutnow?token=order_fixture"}]}`), nil
		case "GET /v2/checkout/orders/order_fixture":
			return response(200, orderBody()), nil
		case "POST /v2/checkout/orders/order_fixture/capture":
			if r.Header.Get("PayPal-Request-Id") != "stable_capture" {
				t.Fatal("missing capture idempotency")
			}
			captured = true
			captures++
			return response(201, orderBody()), nil
		case "POST /v1/notifications/verify-webhook-signature":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["webhook_id"] != "webhook" || body["cert_url"] != "https://cert.invalid/cert" {
				t.Fatal("webhook verification not bound to configured app")
			}
			return response(200, `{"verification_status":"SUCCESS"}`), nil
		default:
			t.Fatalf("unexpected PayPal request %s %s", r.Method, r.URL.Path)
			return nil, ErrRejected
		}
	})
	checkout, err := paypal.CreateCheckout(context.Background(), ports.CheckoutRequest{OperationID: "stable_create", OrderID: "local_order", Environment: ports.Sandbox, Amount: domain.Money{Currency: "USD", Minor: 1699}, ReturnURL: "https://merchant.invalid/return", CancelURL: "https://merchant.invalid/cancel"})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := paypal.QueryPayment(context.Background(), checkout.ExternalID)
	if err != nil || approved.Status == ports.PaymentSucceeded {
		t.Fatal("PayPal approval treated as money received")
	}
	paid, err := paypal.CapturePayment(context.Background(), checkout.ExternalID, "stable_capture")
	if err != nil || paid.Status != ports.PaymentSucceeded || paid.ExternalID != "capture_fixture" {
		t.Fatalf("capture: %+v %v", paid, err)
	}
	if _, err := paypal.CapturePayment(context.Background(), checkout.ExternalID, "stable_capture"); err != nil || captures != 1 || authCalls != 1 {
		t.Fatal("capture or token replay was not reused")
	}
	headers := http.Header{}
	for name, value := range map[string]string{"Paypal-Auth-Algo": "SHA256withRSA", "Paypal-Cert-Url": "https://cert.invalid/cert", "Paypal-Transmission-Id": "transmission", "Paypal-Transmission-Sig": "signature", "Paypal-Transmission-Time": time.Now().Format(time.RFC3339)} {
		headers.Set(name, value)
	}
	event, err := paypal.VerifyWebhook(context.Background(), headers, []byte(`{"id":"event_fixture","event_type":"PAYMENT.CAPTURE.COMPLETED","create_time":"2026-10-15T01:00:00Z","resource":{"id":"capture_fixture","supplementary_data":{"related_ids":{"order_id":"order_fixture"}}}}`))
	if err != nil || event.ResourceID != "order_fixture" {
		t.Fatal("PayPal event correlation failed")
	}
}

func TestProviderErrorsAndExactMinorUnits(t *testing.T) {
	for _, value := range []string{"1.001", "1e3", "-1.00", "1..0"} {
		if _, err := parsePayPalAmount("USD", value); err == nil {
			t.Fatal("invalid decimal accepted")
		}
	}
	m, err := parsePayPalAmount("USD", "16.9")
	if err != nil || m.Minor != 1690 {
		t.Fatal("minor-unit conversion failed")
	}
	c := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return response(401, `{"private":"provider detail"}`), nil
	})}
	if err := request(context.Background(), c, "GET", "https://example.invalid", "", nil, nil, &struct{}{}); !errors.Is(err, ErrCredentials) {
		t.Fatal("credential failure not classified")
	}
}
