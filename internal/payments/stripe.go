package payments

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"licentivo-launch/internal/domain"
	"licentivo-launch/internal/ports"
)

type StripeConfig struct {
	APIKey, WebhookSecret, MerchantRef, APIVersion string
	Environment                                    ports.Environment
}
type Stripe struct {
	config StripeConfig
	http   *http.Client
	base   string
}

func NewStripe(config StripeConfig) (*Stripe, error) {
	if !validID(config.MerchantRef) || config.APIVersion == "" || (config.Environment != ports.Sandbox && config.Environment != ports.Live) || !strings.HasPrefix(config.APIKey, map[ports.Environment]string{ports.Sandbox: "sk_test_", ports.Live: "sk_live_"}[config.Environment]) || !strings.HasPrefix(config.WebhookSecret, "whsec_") {
		return nil, ErrCredentials
	}
	return &Stripe{config, client(), "https://api.stripe.com"}, nil
}
func (s *Stripe) call(ctx context.Context, method, path, operation string, form url.Values, out any) error {
	headers := map[string]string{"Authorization": "Bearer " + s.config.APIKey, "Stripe-Version": s.config.APIVersion}
	if operation != "" {
		headers["Idempotency-Key"] = operation
	}
	return request(ctx, s.http, method, s.base+path, "application/x-www-form-urlencoded", []byte(form.Encode()), headers, out)
}
func (s *Stripe) VerifyAccount(ctx context.Context) error {
	var result struct {
		ID string `json:"id"`
	}
	if err := s.call(ctx, "GET", "/v1/account", "", nil, &result); err != nil {
		return err
	}
	if result.ID != s.config.MerchantRef {
		return ErrVerification
	}
	return nil
}

type stripeSession struct {
	ID            string `json:"id"`
	URL           string `json:"url"`
	Expires       int64  `json:"expires_at"`
	Currency      string `json:"currency"`
	Total         int64  `json:"amount_total"`
	Reference     string `json:"client_reference_id"`
	Live          bool   `json:"livemode"`
	PaymentStatus string `json:"payment_status"`
	Intent        *struct {
		ID             string `json:"id"`
		Status         string `json:"status"`
		AmountReceived int64  `json:"amount_received"`
		Currency       string `json:"currency"`
		Charge         *struct {
			ID       string `json:"id"`
			Captured bool   `json:"captured"`
			Created  int64  `json:"created"`
		} `json:"latest_charge"`
	} `json:"payment_intent"`
}

func (s *Stripe) CreateCheckout(ctx context.Context, in ports.CheckoutRequest) (ports.Checkout, error) {
	if !validID(in.OperationID) || !validID(in.OrderID) || !validAmount(in.Amount) || in.Environment != s.config.Environment {
		return ports.Checkout{}, ErrRejected
	}
	form := url.Values{"mode": {"payment"}, "payment_method_types[0]": {"card"}, "client_reference_id": {in.OrderID}, "success_url": {in.ReturnURL}, "cancel_url": {in.CancelURL}, "line_items[0][quantity]": {"1"}, "line_items[0][price_data][currency]": {strings.ToLower(in.Amount.Currency)}, "line_items[0][price_data][unit_amount]": {strconv.FormatInt(in.Amount.Minor, 10)}, "line_items[0][price_data][product_data][name]": {in.Description}, "payment_intent_data[metadata][launch_order_id]": {in.OrderID}}
	var session stripeSession
	if err := s.call(ctx, "POST", "/v1/checkout/sessions", in.OperationID, form, &session); err != nil {
		return ports.Checkout{}, err
	}
	parsed, err := url.Parse(session.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "checkout.stripe.com" || session.Live != (s.config.Environment == ports.Live) || !validID(session.ID) {
		return ports.Checkout{}, ErrVerification
	}
	return ports.Checkout{ExternalID: session.ID, RedirectURL: session.URL, ExpiresAt: time.Unix(session.Expires, 0).UTC()}, nil
}
func (s *Stripe) QueryPayment(ctx context.Context, id string) (ports.Payment, error) {
	if !validID(id) {
		return ports.Payment{}, ErrRejected
	}
	var session stripeSession
	if err := s.call(ctx, "GET", "/v1/checkout/sessions/"+url.PathEscape(id)+"?expand[]=payment_intent.latest_charge", "", nil, &session); err != nil {
		return ports.Payment{}, err
	}
	if session.ID != id || session.Live != (s.config.Environment == ports.Live) || !validID(session.Reference) {
		return ports.Payment{}, ErrVerification
	}
	payment := ports.Payment{OrderID: session.Reference, MerchantRef: s.config.MerchantRef, Environment: s.config.Environment, Amount: domain.Money{Currency: strings.ToUpper(session.Currency), Minor: session.Total}, Status: ports.PaymentPending}
	if session.PaymentStatus == "paid" && session.Intent != nil {
		intent := session.Intent
		if intent.Status != "succeeded" || intent.AmountReceived != session.Total || strings.ToUpper(intent.Currency) != payment.Amount.Currency || intent.Charge == nil || !intent.Charge.Captured || intent.Charge.Created <= 0 {
			return ports.Payment{}, ErrVerification
		}
		at := time.Unix(intent.Charge.Created, 0).UTC()
		payment.Status, payment.ExternalID, payment.PaidAt = ports.PaymentSucceeded, intent.ID, &at
	}
	return payment, nil
}
func (s *Stripe) CapturePayment(ctx context.Context, id, operation string) (ports.Payment, error) {
	return s.QueryPayment(ctx, id)
}

type stripeRefund struct {
	ID       string `json:"id"`
	Intent   string `json:"payment_intent"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	Status   string `json:"status"`
}

func refundView(r stripeRefund) ports.Refund {
	status := ports.PaymentPending
	if r.Status == "succeeded" {
		status = ports.PaymentSucceeded
	}
	if r.Status == "failed" || r.Status == "canceled" {
		status = ports.PaymentFailed
	}
	return ports.Refund{ExternalID: r.ID, PaymentID: r.Intent, Amount: domain.Money{Currency: strings.ToUpper(r.Currency), Minor: r.Amount}, Status: status}
}
func (s *Stripe) Refund(ctx context.Context, in ports.RefundRequest) (ports.Refund, error) {
	if !validID(in.OperationID) || !validID(in.PaymentID) || !validAmount(in.Amount) {
		return ports.Refund{}, ErrRejected
	}
	var result stripeRefund
	err := s.call(ctx, "POST", "/v1/refunds", in.OperationID, url.Values{"payment_intent": {in.PaymentID}, "amount": {strconv.FormatInt(in.Amount.Minor, 10)}}, &result)
	return refundView(result), err
}
func (s *Stripe) QueryRefund(ctx context.Context, id string) (ports.Refund, error) {
	if !validID(id) {
		return ports.Refund{}, ErrRejected
	}
	var result stripeRefund
	err := s.call(ctx, "GET", "/v1/refunds/"+url.PathEscape(id), "", nil, &result)
	return refundView(result), err
}
func (s *Stripe) VerifyWebhook(ctx context.Context, headers http.Header, body []byte) (ports.VerifiedEvent, error) {
	if len(body) == 0 || len(body) > 1<<20 || len(headers.Values("Stripe-Signature")) != 1 {
		return ports.VerifiedEvent{}, ErrVerification
	}
	var stamp string
	var signatures []string
	for _, item := range strings.Split(headers.Get("Stripe-Signature"), ",") {
		pair := strings.SplitN(item, "=", 2)
		if len(pair) != 2 {
			continue
		}
		if pair[0] == "t" {
			if stamp != "" {
				return ports.VerifiedEvent{}, ErrVerification
			}
			stamp = pair[1]
		}
		if pair[0] == "v1" {
			signatures = append(signatures, pair[1])
		}
	}
	seconds, err := strconv.ParseInt(stamp, 10, 64)
	at := time.Unix(seconds, 0)
	if err != nil || at.Before(time.Now().Add(-5*time.Minute)) || at.After(time.Now().Add(5*time.Minute)) {
		return ports.VerifiedEvent{}, ErrVerification
	}
	mac := hmac.New(sha256.New, []byte(s.config.WebhookSecret))
	mac.Write([]byte(stamp + "."))
	mac.Write(body)
	valid := false
	for _, value := range signatures {
		provided, err := hex.DecodeString(value)
		if err == nil && hmac.Equal(provided, mac.Sum(nil)) {
			valid = true
		}
	}
	if !valid {
		return ports.VerifiedEvent{}, ErrVerification
	}
	var event struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Live    bool   `json:"livemode"`
		Created int64  `json:"created"`
		Data    struct {
			Object struct {
				ID string `json:"id"`
			} `json:"object"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &event) != nil || !validID(event.ID) || event.Live != (s.config.Environment == ports.Live) {
		return ports.VerifiedEvent{}, ErrVerification
	}
	return ports.VerifiedEvent{ID: event.ID, Kind: event.Type, ResourceID: event.Data.Object.ID, Environment: s.config.Environment, OccurredAt: time.Unix(event.Created, 0).UTC()}, nil
}
