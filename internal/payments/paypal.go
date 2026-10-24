package payments

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"licentivo-launch/internal/domain"
	"licentivo-launch/internal/ports"
)

type PayPalConfig struct {
	ClientID, ClientSecret, MerchantRef, WebhookID string
	Environment                                    ports.Environment
}
type PayPal struct {
	config  PayPalConfig
	http    *http.Client
	base    string
	mu      sync.Mutex
	token   string
	expires time.Time
}

func NewPayPal(config PayPalConfig) (*PayPal, error) {
	if config.ClientID == "" || config.ClientSecret == "" || !validID(config.MerchantRef) || !validID(config.WebhookID) || (config.Environment != ports.Sandbox && config.Environment != ports.Live) {
		return nil, ErrCredentials
	}
	base := "https://api-m.sandbox.paypal.com"
	if config.Environment == ports.Live {
		base = "https://api-m.paypal.com"
	}
	return &PayPal{config: config, http: client(), base: base}, nil
}
func (p *PayPal) accessToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if time.Now().Before(p.expires) {
		return p.token, nil
	}
	var result struct {
		Token   string `json:"access_token"`
		Expires int    `json:"expires_in"`
	}
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(p.config.ClientID+":"+p.config.ClientSecret))
	if err := request(ctx, p.http, "POST", p.base+"/v1/oauth2/token", "application/x-www-form-urlencoded", []byte("grant_type=client_credentials"), map[string]string{"Authorization": auth}, &result); err != nil {
		return "", err
	}
	if result.Token == "" || result.Expires < 30 {
		return "", ErrCredentials
	}
	p.token = result.Token
	p.expires = time.Now().Add(time.Duration(result.Expires-20) * time.Second)
	return p.token, nil
}
func (p *PayPal) call(ctx context.Context, method, path, operation string, body any, out any) error {
	token, err := p.accessToken(ctx)
	if err != nil {
		return err
	}
	headers := map[string]string{"Authorization": "Bearer " + token, "Prefer": "return=representation"}
	if operation != "" {
		headers["PayPal-Request-Id"] = operation
	}
	var raw []byte
	if body != nil {
		raw = encoded(body)
	}
	return request(ctx, p.http, method, p.base+path, "application/json", raw, headers, out)
}
func (p *PayPal) VerifyAccount(ctx context.Context) error { _, err := p.accessToken(ctx); return err }
func paypalAmount(m domain.Money) map[string]string {
	return map[string]string{"currency_code": m.Currency, "value": fmt.Sprintf("%d.%02d", m.Minor/100, m.Minor%100)}
}
func parsePayPalAmount(currency, value string) (domain.Money, error) {
	if len(value) > 15 || strings.Trim(value, "0123456789.") != "" {
		return domain.Money{}, ErrVerification
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" {
		return domain.Money{}, ErrVerification
	}
	fraction := "00"
	if len(parts) == 2 {
		fraction = parts[1]
		if len(fraction) > 2 || len(fraction) == 0 {
			return domain.Money{}, ErrVerification
		}
		fraction += strings.Repeat("0", 2-len(fraction))
	}
	minor, err := strconv.ParseInt(parts[0]+fraction, 10, 64)
	m := domain.Money{Currency: currency, Minor: minor}
	if err != nil || !validAmount(m) {
		return domain.Money{}, ErrVerification
	}
	return m, nil
}

type paypalCapture struct {
	ID      string    `json:"id"`
	Status  string    `json:"status"`
	Created time.Time `json:"create_time"`
	Amount  struct {
		Currency string `json:"currency_code"`
		Value    string `json:"value"`
	} `json:"amount"`
}
type paypalOrder struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Units  []struct {
		Reference string `json:"custom_id"`
		Payee     struct {
			Merchant string `json:"merchant_id"`
		} `json:"payee"`
		Amount struct {
			Currency string `json:"currency_code"`
			Value    string `json:"value"`
		} `json:"amount"`
		Payments struct {
			Captures []paypalCapture `json:"captures"`
		} `json:"payments"`
	} `json:"purchase_units"`
	Links []struct {
		Rel string `json:"rel"`
		URL string `json:"href"`
	} `json:"links"`
}

func (p *PayPal) CreateCheckout(ctx context.Context, in ports.CheckoutRequest) (ports.Checkout, error) {
	if !validID(in.OperationID) || !validID(in.OrderID) || !validAmount(in.Amount) || in.Environment != p.config.Environment {
		return ports.Checkout{}, ErrRejected
	}
	body := map[string]any{"intent": "CAPTURE", "purchase_units": []any{map[string]any{"custom_id": in.OrderID, "invoice_id": in.OrderID, "description": in.Description, "amount": paypalAmount(in.Amount), "payee": map[string]string{"merchant_id": p.config.MerchantRef}}}, "application_context": map[string]string{"return_url": in.ReturnURL, "cancel_url": in.CancelURL, "shipping_preference": "NO_SHIPPING", "user_action": "PAY_NOW"}}
	var order paypalOrder
	if err := p.call(ctx, "POST", "/v2/checkout/orders", in.OperationID, body, &order); err != nil {
		return ports.Checkout{}, err
	}
	for _, link := range order.Links {
		if link.Rel != "approve" && link.Rel != "payer-action" {
			continue
		}
		u, err := url.Parse(link.URL)
		expected := "www.sandbox.paypal.com"
		if p.config.Environment == ports.Live {
			expected = "www.paypal.com"
		}
		if err != nil || u.Scheme != "https" || u.Host != expected || !validID(order.ID) {
			return ports.Checkout{}, ErrVerification
		}
		return ports.Checkout{ExternalID: order.ID, RedirectURL: link.URL}, nil
	}
	return ports.Checkout{}, ErrVerification
}
func (p *PayPal) view(order paypalOrder) (ports.Payment, error) {
	if len(order.Units) != 1 || order.Units[0].Payee.Merchant != p.config.MerchantRef {
		return ports.Payment{}, ErrVerification
	}
	unit := order.Units[0]
	amount, err := parsePayPalAmount(unit.Amount.Currency, unit.Amount.Value)
	if err != nil {
		return ports.Payment{}, err
	}
	result := ports.Payment{OrderID: unit.Reference, MerchantRef: unit.Payee.Merchant, Environment: p.config.Environment, Amount: amount, Status: ports.PaymentPending}
	if order.Status == "COMPLETED" {
		if len(unit.Payments.Captures) != 1 {
			return ports.Payment{}, ErrVerification
		}
		capture := unit.Payments.Captures[0]
		actual, err := parsePayPalAmount(capture.Amount.Currency, capture.Amount.Value)
		if err != nil || actual != amount || capture.Status != "COMPLETED" || capture.Created.IsZero() || !validID(capture.ID) {
			return ports.Payment{}, ErrVerification
		}
		at := capture.Created.UTC()
		result.ExternalID, result.Status, result.PaidAt = capture.ID, ports.PaymentSucceeded, &at
	}
	return result, nil
}
func (p *PayPal) QueryPayment(ctx context.Context, id string) (ports.Payment, error) {
	if !validID(id) {
		return ports.Payment{}, ErrRejected
	}
	var order paypalOrder
	if err := p.call(ctx, "GET", "/v2/checkout/orders/"+url.PathEscape(id), "", nil, &order); err != nil {
		return ports.Payment{}, err
	}
	if order.ID != id {
		return ports.Payment{}, ErrVerification
	}
	return p.view(order)
}
func (p *PayPal) CapturePayment(ctx context.Context, id, operation string) (ports.Payment, error) {
	if !validID(id) || !validID(operation) {
		return ports.Payment{}, ErrRejected
	}
	var order paypalOrder
	if err := p.call(ctx, "GET", "/v2/checkout/orders/"+url.PathEscape(id), "", nil, &order); err != nil {
		return ports.Payment{}, err
	}
	if _, err := p.view(order); err != nil {
		return ports.Payment{}, err
	}
	if order.ID != id {
		return ports.Payment{}, ErrVerification
	}
	if order.Status != "APPROVED" {
		return p.view(order)
	}
	var captured paypalOrder
	if err := p.call(ctx, "POST", "/v2/checkout/orders/"+url.PathEscape(id)+"/capture", operation, map[string]any{}, &captured); err != nil {
		return ports.Payment{}, err
	}
	// Re-query supplies complete payee and amount fields for verification.
	return p.QueryPayment(ctx, id)
}

type paypalRefund struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Amount struct {
		Currency string `json:"currency_code"`
		Value    string `json:"value"`
	} `json:"amount"`
	Links []struct {
		Rel string `json:"rel"`
		URL string `json:"href"`
	} `json:"links"`
}

func paypalRefundView(r paypalRefund, paymentID string) (ports.Refund, error) {
	amount, err := parsePayPalAmount(r.Amount.Currency, r.Amount.Value)
	if err != nil {
		return ports.Refund{}, err
	}
	status := ports.PaymentPending
	if r.Status == "COMPLETED" {
		status = ports.PaymentSucceeded
	}
	if r.Status == "FAILED" || r.Status == "CANCELLED" {
		status = ports.PaymentFailed
	}
	if paymentID == "" {
		for _, link := range r.Links {
			if link.Rel == "up" {
				u, err := url.Parse(link.URL)
				if err == nil && strings.HasPrefix(u.Path, "/v2/payments/captures/") {
					paymentID = strings.TrimPrefix(u.Path, "/v2/payments/captures/")
				}
			}
		}
	}
	if !validID(paymentID) || !validID(r.ID) {
		return ports.Refund{}, ErrVerification
	}
	return ports.Refund{ExternalID: r.ID, PaymentID: paymentID, Amount: amount, Status: status}, nil
}
func (p *PayPal) Refund(ctx context.Context, in ports.RefundRequest) (ports.Refund, error) {
	if !validID(in.PaymentID) || !validID(in.OperationID) || !validAmount(in.Amount) {
		return ports.Refund{}, ErrRejected
	}
	var result paypalRefund
	if err := p.call(ctx, "POST", "/v2/payments/captures/"+url.PathEscape(in.PaymentID)+"/refund", in.OperationID, map[string]any{"amount": paypalAmount(in.Amount)}, &result); err != nil {
		return ports.Refund{}, err
	}
	return paypalRefundView(result, in.PaymentID)
}
func (p *PayPal) QueryRefund(ctx context.Context, id string) (ports.Refund, error) {
	if !validID(id) {
		return ports.Refund{}, ErrRejected
	}
	var result paypalRefund
	if err := p.call(ctx, "GET", "/v2/payments/refunds/"+url.PathEscape(id), "", nil, &result); err != nil {
		return ports.Refund{}, err
	}
	return paypalRefundView(result, "")
}
func (p *PayPal) VerifyWebhook(ctx context.Context, headers http.Header, body []byte) (ports.VerifiedEvent, error) {
	if len(body) == 0 || len(body) > 1<<20 {
		return ports.VerifiedEvent{}, ErrVerification
	}
	var event struct {
		ID       string    `json:"id"`
		Kind     string    `json:"event_type"`
		Created  time.Time `json:"create_time"`
		Resource struct {
			ID     string `json:"id"`
			Amount struct {
				Currency string `json:"currency_code"`
				Value    string `json:"value"`
			} `json:"amount"`
			Related struct {
				IDs struct {
					OrderID string `json:"order_id"`
				} `json:"related_ids"`
			} `json:"supplementary_data"`
		} `json:"resource"`
	}
	if json.Unmarshal(body, &event) != nil || !validID(event.ID) {
		return ports.VerifiedEvent{}, ErrVerification
	}
	verification := map[string]any{"webhook_id": p.config.WebhookID, "webhook_event": json.RawMessage(body)}
	for field, header := range map[string]string{"auth_algo": "Paypal-Auth-Algo", "cert_url": "Paypal-Cert-Url", "transmission_id": "Paypal-Transmission-Id", "transmission_sig": "Paypal-Transmission-Sig", "transmission_time": "Paypal-Transmission-Time"} {
		if len(headers.Values(header)) != 1 || headers.Get(header) == "" {
			return ports.VerifiedEvent{}, ErrVerification
		}
		verification[field] = headers.Get(header)
	}
	var result struct {
		Status string `json:"verification_status"`
	}
	if err := p.call(ctx, "POST", "/v1/notifications/verify-webhook-signature", "", verification, &result); err != nil {
		return ports.VerifiedEvent{}, err
	}
	if result.Status != "SUCCESS" {
		return ports.VerifiedEvent{}, ErrVerification
	}
	resource := event.Resource.ID
	if event.Resource.Related.IDs.OrderID != "" && (event.Kind == "CHECKOUT.ORDER.APPROVED" || event.Kind == "PAYMENT.CAPTURE.COMPLETED") {
		resource = event.Resource.Related.IDs.OrderID
	}
	out := ports.VerifiedEvent{ID: event.ID, Kind: event.Kind, ResourceID: resource, Environment: p.config.Environment, OccurredAt: event.Created.UTC()}
	if event.Kind == "PAYMENT.CAPTURE.REVERSED" {
		amount, err := parsePayPalAmount(event.Resource.Amount.Currency, event.Resource.Amount.Value)
		if err != nil {
			return ports.VerifiedEvent{}, err
		}
		out.ReversalAmount = &amount
	}
	return out, nil
}

// NormalizeVerifiedPayPalReversal only normalizes a body whose signature was
// already verified, including an authenticated event restored from storage.
func NormalizeVerifiedPayPalReversal(event ports.VerifiedEvent, body []byte) (ports.VerifiedEvent, error) {
	var in struct {
		Resource struct {
			ID     string `json:"id"`
			Amount struct {
				Currency string `json:"currency_code"`
				Value    string `json:"value"`
			} `json:"amount"`
		} `json:"resource"`
	}
	if event.Kind != "PAYMENT.CAPTURE.REVERSED" || json.Unmarshal(body, &in) != nil || !validID(in.Resource.ID) {
		return event, ErrVerification
	}
	amount, err := parsePayPalAmount(in.Resource.Amount.Currency, in.Resource.Amount.Value)
	if err != nil {
		return event, err
	}
	event.ResourceID = in.Resource.ID
	event.ReversalAmount = &amount
	return event, nil
}
