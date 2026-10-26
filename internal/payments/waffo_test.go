package payments

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"licentivo-launch/internal/domain"
	"licentivo-launch/internal/ports"
)

func testRSASign(t *testing.T, key *rsa.PrivateKey, raw []byte) string {
	t.Helper()
	sum := sha256.Sum256(raw)
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(sig)
}
func newWaffoFixture(t *testing.T) (*Waffo, *rsa.PrivateKey, *rsa.PrivateKey) {
	t.Helper()
	merchant, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	server, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	private, _ := x509.MarshalPKCS8PrivateKey(merchant)
	public, _ := x509.MarshalPKIXPublicKey(&server.PublicKey)
	w, err := NewWaffo(WaffoConfig{APIKey: "waffo_test", MerchantRef: "merchant_fixture", PrivateKey: base64.StdEncoding.EncodeToString(private), PublicKey: base64.StdEncoding.EncodeToString(public), Environment: ports.Sandbox})
	if err != nil {
		t.Fatal(err)
	}
	return w, merchant, server
}
func TestWaffoSignedCheckoutPaymentAndAsynchronousRefund(t *testing.T) {
	w, merchant, server := newWaffoFixture(t)
	ctx := context.Background()
	refundStatus := "REFUND_IN_PROGRESS"
	orderStatus := "PAY_SUCCESS"
	merchantRef := "merchant_fixture"
	signed := true
	refundPosts := 0
	requestID := "123456781234423482341234567890ab"
	orderID := "12345678-1234-4234-8234-1234567890ab"
	w.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api-sandbox.waffo.com" || r.Header.Get("X-API-KEY") != "waffo_test" || r.Header.Get("X-API-VERSION") != "1.0.0" {
			t.Fatal("incorrect Waffo environment or authentication")
		}
		raw, _ := io.ReadAll(r.Body)
		sum := sha256.Sum256(raw)
		sig, _ := base64.StdEncoding.DecodeString(r.Header.Get("X-SIGNATURE"))
		if rsa.VerifyPKCS1v15(&merchant.PublicKey, crypto.SHA256, sum[:], sig) != nil {
			t.Fatal("request was not RSA-signed")
		}
		var in map[string]any
		if json.Unmarshal(raw, &in) != nil {
			t.Fatal("invalid request JSON")
		}
		var data any
		switch r.URL.Path {
		case "/api/v1/merchantconfig/inquiry":
			data = map[string]string{"merchantId": merchantRef}
		case "/api/v1/order/create":
			if in["paymentRequestId"] != requestID || in["orderAmount"] != "16.99" || in["orderCurrency"] != "USD" || in["userCurrency"] != "USD" || in["merchantOrderId"] != orderID || in["notifyUrl"] != "https://merchant.invalid/webhook" {
				t.Fatal("checkout lost exact amount, stable identity or callback")
			}
			data = map[string]string{"acquiringOrderId": "A123", "paymentRequestId": requestID, "merchantOrderId": orderID, "orderAction": `{"actionType":"REDIRECT","webUrl":"https://cashier-sandbox.waffo.com/pay/A123"}`}
		case "/api/v1/order/inquiry":
			data = map[string]any{"acquiringOrderId": "A123", "merchantOrderId": orderID, "paymentRequestId": requestID, "orderCurrency": "USD", "orderAmount": "16.99", "orderStatus": orderStatus, "orderCompletedAt": "2026-10-22T10:00:00Z", "merchantInfo": map[string]string{"merchantId": merchantRef}}
		case "/api/v1/order/refund":
			refundPosts++
			if in["refundRequestId"] != requestID || in["refundAmount"] != "16.99" || in["acquiringOrderId"] != "A123" {
				t.Fatal("incorrect Waffo refund identity")
			}
			data = map[string]string{"refundRequestId": requestID, "acquiringOrderId": "A123", "refundStatus": refundStatus}
		case "/api/v1/refund/inquiry":
			data = map[string]string{"refundRequestId": requestID, "acquiringOrderId": "A123", "refundAmount": "16.99", "refundStatus": refundStatus}
		default:
			t.Fatalf("unexpected Waffo path %s", r.URL.Path)
		}
		body := encoded(map[string]any{"code": "0", "data": data})
		result := response(200, string(body))
		if signed {
			result.Header.Set("X-SIGNATURE", testRSASign(t, server, body))
		} else {
			result.Header.Set("X-SIGNATURE", "invalid")
		}
		return result, nil
	})
	if err := w.VerifyAccount(ctx); err != nil {
		t.Fatal(err)
	}
	in := ports.CheckoutRequest{OperationID: orderID, OrderID: orderID, Environment: ports.Sandbox, Amount: domain.Money{Currency: "USD", Minor: 1699}, Description: "Pro", CustomerID: "customer", CustomerEmail: "buyer@example.invalid", ReturnURL: "https://merchant.invalid/account", NotifyURL: "https://merchant.invalid/webhook", RequestedAt: time.Now().UTC()}
	checkout, err := w.CreateCheckout(ctx, in)
	if err != nil || checkout.ExternalID != "A123" {
		t.Fatalf("checkout: %+v %v", checkout, err)
	}
	paid, err := w.QueryPayment(ctx, "A123")
	if err != nil || paid.Status != ports.PaymentSucceeded || paid.PaidAt == nil {
		t.Fatalf("payment: %+v %v", paid, err)
	}
	// These are the public Waffo SDK statuses, shared by inquiry and webhook
	// responses. Authorization alone must never activate software access.
	for _, state := range []string{"PAY_IN_PROGRESS", "AUTHORIZATION_REQUIRED", "AUTHED_WAITING_CAPTURE", "CAPTURE_IN_PROGRESS"} {
		orderStatus = state
		payment, err := w.QueryPayment(ctx, "A123")
		if err != nil || payment.Status != ports.PaymentPending || payment.PaidAt != nil {
			t.Fatalf("payment state %s: %+v %v", state, payment, err)
		}
	}
	orderStatus = "ORDER_CLOSE"
	if payment, err := w.QueryPayment(ctx, "A123"); err != nil || payment.Status != ports.PaymentFailed {
		t.Fatalf("closed payment: %+v %v", payment, err)
	}
	orderStatus = "PAY_SUCCESS"
	refund, err := w.Refund(ctx, ports.RefundRequest{OperationID: orderID, PaymentID: "A123", Amount: in.Amount})
	if err != nil || refund.Status != ports.PaymentPending || refund.ExternalID != requestID {
		t.Fatalf("submitted refund: %+v %v", refund, err)
	}
	refundStatus = "ORDER_FULLY_REFUNDED"
	refund, err = w.QueryRefund(ctx, requestID)
	if err != nil || refund.Status != ports.PaymentSucceeded || refund.PaymentID != "A123" || refund.Amount != in.Amount {
		t.Fatal("confirmed refund was not correlated")
	}
	beforePosts := refundPosts
	recovery := ports.RefundRequest{OperationID: orderID, PaymentID: "A123", Amount: in.Amount, RequestedAt: time.Now().Add(-72 * time.Hour)}
	if found, err := w.RecoverRefund(ctx, recovery); err != nil || found.ExternalID != requestID || found.Status != ports.PaymentSucceeded {
		t.Fatal("read-only recovery beyond POST window failed", err)
	}
	recovery.Amount.Minor++
	if _, err := w.RecoverRefund(ctx, recovery); err == nil {
		t.Fatal("mismatched recovery receipt accepted")
	}
	if refundPosts != beforePosts {
		t.Fatal("read-only recovery submitted another refund")
	}
	refundStatus = "ORDER_REFUND_FAILED"
	if refund, err := w.QueryRefund(ctx, requestID); err != nil || refund.Status != ports.PaymentFailed {
		t.Fatalf("failed refund: %+v %v", refund, err)
	}
	refundStatus = "ORDER_PARTIALLY_REFUNDED"
	if refund, err := w.QueryRefund(ctx, requestID); err != nil || refund.Status != ports.PaymentUnknown {
		t.Fatalf("partial refund was accepted as a full refund: %+v %v", refund, err)
	}
	merchantRef = "another_merchant"
	if _, err := w.QueryPayment(ctx, "A123"); err != ErrVerification {
		t.Fatal("wrong merchant was accepted")
	}
	merchantRef = "merchant_fixture"
	signed = false
	if err := w.VerifyAccount(ctx); err != ErrVerification {
		t.Fatal("forged response signature was accepted")
	}
	for _, link := range []string{"https://waffo.com.evil.invalid/pay", "javascript:alert(1)", "https://cashier.waffo.com:443/pay"} {
		if _, err := w.checkout(waffoOrder{ID: "A123", Action: string(encoded(map[string]string{"webUrl": link}))}); err != ErrVerification {
			t.Fatal("unsafe redirect accepted")
		}
	}
}
func TestWaffoWebhookRawSignatureDeduplicationAndAcknowledgement(t *testing.T) {
	w, merchant, server := newWaffoFixture(t)
	raw := []byte(`{"eventType":"REFUND_NOTIFICATION","result":{"refundRequestId":"ref_123","acquiringOrderId":"A123","refundUpdatedAt":"2026-10-22T10:00:00Z"}}`)
	headers := http.Header{}
	headers.Set("X-SIGNATURE", testRSASign(t, server, raw))
	event, err := w.VerifyWebhook(context.Background(), headers, raw)
	if err != nil || event.ResourceID != "ref_123" || event.Kind != "REFUND_NOTIFICATION" {
		t.Fatal("refund callback not parsed")
	}
	repeated, _ := w.VerifyWebhook(context.Background(), headers, raw)
	if event.ID != repeated.ID {
		t.Fatal("retry deduplication changed")
	}
	if _, err := w.VerifyWebhook(context.Background(), headers, append(raw, ' ')); err != ErrVerification {
		t.Fatal("modified body accepted")
	}
	if _, err := w.VerifyWebhook(context.Background(), http.Header{}, raw); err != ErrVerification {
		t.Fatal("unsigned callback accepted")
	}
	body, ack, err := w.WebhookAcknowledgement()
	if err != nil || string(body) != `{"message":"success"}` {
		t.Fatal("invalid raw acknowledgement")
	}
	sum := sha256.Sum256(body)
	sig, _ := base64.StdEncoding.DecodeString(ack.Get("X-SIGNATURE"))
	if rsa.VerifyPKCS1v15(&merchant.PublicKey, crypto.SHA256, sum[:], sig) != nil {
		t.Fatal("acknowledgement was not signed")
	}
	other := []byte(strings.ReplaceAll(string(raw), "REFUND_NOTIFICATION", "UNKNOWN_NOTIFICATION"))
	headers.Set("X-SIGNATURE", testRSASign(t, server, other))
	if _, err := w.VerifyWebhook(context.Background(), headers, other); err != ErrVerification {
		t.Fatal("unknown callback accepted as a refund")
	}
}

func TestWaffoRefundRecoversLostSubmissionAfterOrderStateChanges(t *testing.T) {
	for _, state := range []struct {
		order, refund string
		want          ports.PaymentStatus
	}{
		{"REFUND_IN_PROGRESS", "REFUND_IN_PROGRESS", ports.PaymentPending},
		{"ORDER_FULLY_REFUNDED", "ORDER_FULLY_REFUNDED", ports.PaymentSucceeded},
	} {
		t.Run(state.order, func(t *testing.T) {
			w, _, server := newWaffoFixture(t)
			request := ports.RefundRequest{OperationID: "12345678-1234-4234-8234-1234567890ab", PaymentID: "A123", Amount: domain.Money{Currency: "USD", Minor: 1699}, RequestedAt: time.Now().UTC()}
			requestID := strings.ReplaceAll(request.OperationID, "-", "")
			orderStatus := "PAY_SUCCESS"
			creates, inquiries := 0, 0
			transport := roundTrip(func(r *http.Request) (*http.Response, error) {
				raw, _ := io.ReadAll(r.Body)
				var in map[string]string
				if err := json.Unmarshal(raw, &in); err != nil {
					t.Fatal(err)
				}
				var data any
				switch r.URL.Path {
				case "/api/v1/order/inquiry":
					data = map[string]any{"acquiringOrderId": "A123", "merchantOrderId": "local_order", "orderCurrency": "USD", "orderAmount": "16.99", "orderStatus": orderStatus, "orderCompletedAt": "2026-10-22T10:00:00Z", "merchantInfo": map[string]string{"merchantId": "merchant_fixture"}}
				case "/api/v1/order/refund":
					creates++
					if creates != 1 || in["refundRequestId"] != requestID || in["acquiringOrderId"] != request.PaymentID {
						t.Fatal("refund was duplicated or changed identity")
					}
					// Waffo processes the request, but both the mutation response
					// and the immediate recovery query are lost to the caller.
					orderStatus = state.order
					return nil, io.ErrUnexpectedEOF
				case "/api/v1/refund/inquiry":
					inquiries++
					if in["refundRequestId"] != requestID {
						t.Fatal("recovery did not use the persisted operation ID")
					}
					if inquiries == 1 {
						return nil, io.ErrUnexpectedEOF
					}
					data = map[string]string{"refundRequestId": requestID, "acquiringOrderId": "A123", "refundAmount": "16.99", "refundStatus": state.refund}
				default:
					t.Fatalf("unexpected Waffo path %s", r.URL.Path)
				}
				body := encoded(map[string]any{"code": "0", "data": data})
				result := response(200, string(body))
				result.Header.Set("X-SIGNATURE", testRSASign(t, server, body))
				return result, nil
			})
			w.http.Transport = transport
			if _, err := w.Refund(context.Background(), request); err != ErrUncertain {
				t.Fatalf("lost submission should remain uncertain: %v", err)
			}
			// A new adapter represents a process restart with the same durable
			// request and no remembered external refund identifier.
			restarted, err := NewWaffo(w.config)
			if err != nil {
				t.Fatal(err)
			}
			restarted.http.Transport = transport
			found, err := restarted.Refund(context.Background(), request)
			if err != nil || found.ExternalID != requestID || found.PaymentID != request.PaymentID || found.Amount != request.Amount || found.Status != state.want {
				t.Fatalf("lost refund was not recovered: %+v %v", found, err)
			}
			if creates != 1 || inquiries != 2 {
				t.Fatalf("expected one submission and two inquiries, got %d and %d", creates, inquiries)
			}
		})
	}
}
