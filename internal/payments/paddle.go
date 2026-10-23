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

type PaddleConfig struct {
	APIKey, WebhookSecret, MerchantRef, ClientToken, ProductID string
	Environment                                                ports.Environment
}
type Paddle struct {
	config PaddleConfig
	http   *http.Client
	base   string
}

func paddleID(id, prefix string) bool {
	return strings.HasPrefix(id, prefix) && len(id) == len(prefix)+26 && strings.Trim(id[len(prefix):], "abcdefghijklmnopqrstuvwxyz0123456789") == ""
}
func NewPaddle(c PaddleConfig) (*Paddle, error) {
	keyPrefix, tokenPrefix, base := "pdl_sdbx_apikey_", "test_", "https://sandbox-api.paddle.com"
	if c.Environment == ports.Live {
		keyPrefix, tokenPrefix, base = "pdl_live_apikey_", "live_", "https://api.paddle.com"
	} else if c.Environment != ports.Sandbox {
		return nil, ErrCredentials
	}
	if !strings.HasPrefix(c.APIKey, keyPrefix) || strings.ContainsAny(c.APIKey, "\r\n") || c.WebhookSecret == "" || !strings.HasPrefix(c.ClientToken, tokenPrefix) || !paddleID(c.MerchantRef, "slr_") || !paddleID(c.ProductID, "pro_") {
		return nil, ErrCredentials
	}
	return &Paddle{c, client(), base}, nil
}
func (p *Paddle) call(ctx context.Context, method, path string, in, out any) error {
	var raw []byte
	if in != nil {
		raw = encoded(in)
	}
	return request(ctx, p.http, method, p.base+path, "application/json", raw, map[string]string{"Authorization": "Bearer " + p.config.APIKey, "Paddle-Version": "1"}, out)
}
func (p *Paddle) VerifyAccount(ctx context.Context) error {
	var root struct {
		Data struct {
			API struct {
				Environment string `json:"environment"`
			} `json:"api"`
			Caller struct {
				AuthType string `json:"auth_type"`
				Account  *struct {
					ID string `json:"id"`
				} `json:"account"`
				Permissions []string `json:"permissions"`
			} `json:"caller"`
		} `json:"data"`
	}
	if err := p.call(ctx, "GET", "/", nil, &root); err != nil {
		return err
	}
	environment := "sandbox"
	if p.config.Environment == ports.Live {
		environment = "production"
	}
	if root.Data.Caller.Account == nil || root.Data.Caller.Account.ID != p.config.MerchantRef || root.Data.Caller.AuthType != "api_key" || root.Data.API.Environment != environment {
		return ErrVerification
	}
	for _, need := range []string{"transaction.read", "transaction.write", "adjustment.read", "adjustment.write", "product.read"} {
		found := false
		for _, actual := range root.Data.Caller.Permissions {
			if actual == need {
				found = true
			}
		}
		if !found {
			return ErrCredentials
		}
	}
	var product struct {
		Data struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := p.call(ctx, "GET", "/products/"+p.config.ProductID, nil, &product); err != nil {
		return err
	}
	if product.Data.ID != p.config.ProductID || product.Data.Status != "active" {
		return ErrVerification
	}
	return nil
}

type paddleMetadata struct {
	OrderID     string `json:"launch_order_id"`
	OperationID string `json:"launch_operation_id"`
	MerchantRef string `json:"launch_merchant_ref"`
	Environment string `json:"launch_environment"`
}
type paddleTransaction struct {
	ID             string         `json:"id"`
	Status         string         `json:"status"`
	Currency       string         `json:"currency_code"`
	CollectionMode string         `json:"collection_mode"`
	Custom         paddleMetadata `json:"custom_data"`
	Checkout       struct {
		URL string `json:"url"`
	} `json:"checkout"`
	Details struct {
		Totals struct {
			Total      string `json:"total"`
			GrandTotal string `json:"grand_total"`
			Credit     string `json:"credit"`
		} `json:"totals"`
	} `json:"details"`
	Payments []struct {
		Status     string     `json:"status"`
		Amount     string     `json:"amount"`
		CapturedAt *time.Time `json:"captured_at"`
	} `json:"payments"`
}

func (p *Paddle) owns(t paddleTransaction) bool {
	return paddleID(t.ID, "txn_") && validID(t.Custom.OrderID) && validID(t.Custom.OperationID) && t.Custom.MerchantRef == p.config.MerchantRef && t.Custom.Environment == string(p.config.Environment) && t.CollectionMode == "automatic"
}
func (p *Paddle) checkout(t paddleTransaction, in ports.CheckoutRequest) (ports.Checkout, error) {
	if !p.owns(t) || t.Custom.OrderID != in.OrderID || t.Custom.OperationID != in.OperationID {
		return ports.Checkout{}, ErrVerification
	}
	base, err := url.Parse(in.HostedCheckoutURL)
	if err != nil {
		return ports.Checkout{}, ErrVerification
	}
	link, err := url.Parse(t.Checkout.URL)
	if err != nil || link.User != nil || link.Scheme != base.Scheme || link.Host != base.Host || link.Path != base.Path || link.Query().Get("_ptxn") != t.ID {
		return ports.Checkout{}, ErrVerification
	}
	return ports.Checkout{ExternalID: t.ID, RedirectURL: t.Checkout.URL}, nil
}
func (p *Paddle) CreateCheckout(ctx context.Context, in ports.CheckoutRequest) (ports.Checkout, error) {
	if in.Environment != p.config.Environment || !validAmount(in.Amount) || !validID(in.OperationID) || !validID(in.OrderID) {
		return ports.Checkout{}, ErrRejected
	}
	u, err := url.Parse(in.HostedCheckoutURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Scheme != "https" && !(p.config.Environment == ports.Sandbox && u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1")) {
		return ports.Checkout{}, ErrRejected
	}
	body := map[string]any{
		"collection_mode": "automatic", "currency_code": in.Amount.Currency,
		"custom_data": paddleMetadata{in.OrderID, in.OperationID, p.config.MerchantRef, string(p.config.Environment)},
		"checkout":    map[string]string{"url": in.HostedCheckoutURL},
		"items": []any{map[string]any{"quantity": 1, "price": map[string]any{
			"description": in.Description, "name": in.Description, "product_id": p.config.ProductID,
			"billing_cycle": nil, "tax_mode": "internal",
			"unit_price": map[string]string{"amount": strconv.FormatInt(in.Amount.Minor, 10), "currency_code": in.Amount.Currency},
			"quantity":   map[string]int{"minimum": 1, "maximum": 1},
		}}},
	}
	var result struct {
		Data paddleTransaction `json:"data"`
	}
	// Paddle does not implement client-supplied idempotency keys. The service
	// persists the attempt before this POST and never blindly repeats it.
	if err := p.call(ctx, "POST", "/transactions", body, &result); err != nil {
		return ports.Checkout{}, err
	}
	return p.checkout(result.Data, in)
}
func (p *Paddle) transaction(ctx context.Context, id string) (paddleTransaction, error) {
	if !paddleID(id, "txn_") {
		return paddleTransaction{}, ErrRejected
	}
	var result struct {
		Data paddleTransaction `json:"data"`
	}
	if err := p.call(ctx, "GET", "/transactions/"+id, nil, &result); err != nil {
		return paddleTransaction{}, err
	}
	if result.Data.ID != id || !p.owns(result.Data) {
		return paddleTransaction{}, ErrVerification
	}
	return result.Data, nil
}
func paddleMoney(currency, amount string) (domain.Money, error) {
	if amount == "" || strings.Trim(amount, "0123456789") != "" {
		return domain.Money{}, ErrVerification
	}
	minor, err := strconv.ParseInt(amount, 10, 64)
	money := domain.Money{Currency: currency, Minor: minor}
	if err != nil || !validAmount(money) {
		return domain.Money{}, ErrVerification
	}
	return money, nil
}
func (p *Paddle) QueryPayment(ctx context.Context, id string) (ports.Payment, error) {
	t, err := p.transaction(ctx, id)
	if err != nil {
		return ports.Payment{}, err
	}
	money, err := paddleMoney(t.Currency, t.Details.Totals.Total)
	if err != nil {
		return ports.Payment{}, err
	}
	result := ports.Payment{ExternalID: t.ID, OrderID: t.Custom.OrderID, MerchantRef: p.config.MerchantRef, Environment: p.config.Environment, Amount: money, Status: ports.PaymentPending}
	switch t.Status {
	case "completed":
		// An invoice or an approval is not proof of a captured payment. Credits
		// and altered totals must not silently fulfill a fixed-amount order.
		if t.Details.Totals.GrandTotal != t.Details.Totals.Total || t.Details.Totals.Credit != "0" {
			return ports.Payment{}, ErrVerification
		}
		var captured int64
		for _, payment := range t.Payments {
			if payment.Status != "captured" {
				continue
			}
			part, err := paddleMoney(t.Currency, payment.Amount)
			if err != nil || payment.CapturedAt == nil || part.Minor > money.Minor-captured {
				return ports.Payment{}, ErrVerification
			}
			captured += part.Minor
			if result.PaidAt == nil || payment.CapturedAt.After(*result.PaidAt) {
				result.PaidAt = payment.CapturedAt
			}
		}
		if captured != money.Minor || result.PaidAt == nil {
			return ports.Payment{}, ErrVerification
		}
		result.Status = ports.PaymentSucceeded
	case "canceled":
		result.Status = ports.PaymentFailed
	case "draft", "ready", "billed", "paid", "past_due":
	default:
		result.Status = ports.PaymentUnknown
	}
	return result, nil
}
func (p *Paddle) CapturePayment(ctx context.Context, id, _ string) (ports.Payment, error) {
	return p.QueryPayment(ctx, id)
}

type paddleAdjustment struct {
	ID            string `json:"id"`
	TransactionID string `json:"transaction_id"`
	Action        string `json:"action"`
	Type          string `json:"type"`
	Reason        string `json:"reason"`
	Currency      string `json:"currency_code"`
	Status        string `json:"status"`
	Totals        struct {
		Total string `json:"total"`
	} `json:"totals"`
}

func paddleRefundReason(operation string) string {
	return "Licentivo Launch full refund / " + operation
}
func (p *Paddle) refundView(ctx context.Context, a paddleAdjustment) (ports.Refund, error) {
	if !paddleID(a.ID, "adj_") || !paddleID(a.TransactionID, "txn_") || a.Action != "refund" || a.Type != "full" {
		return ports.Refund{}, ErrVerification
	}
	money, err := paddleMoney(a.Currency, a.Totals.Total)
	if err != nil {
		return ports.Refund{}, err
	}
	payment, err := p.QueryPayment(ctx, a.TransactionID)
	if err != nil {
		return ports.Refund{}, err
	}
	if payment.Amount != money || payment.Status != ports.PaymentSucceeded {
		return ports.Refund{}, ErrVerification
	}
	status := ports.PaymentUnknown
	switch a.Status {
	case "approved":
		status = ports.PaymentSucceeded
	case "pending_approval":
		status = ports.PaymentPending
	case "rejected", "reversed":
		status = ports.PaymentFailed
	}
	return ports.Refund{ExternalID: a.ID, PaymentID: a.TransactionID, Amount: money, Status: status}, nil
}
func (p *Paddle) Refund(ctx context.Context, in ports.RefundRequest) (ports.Refund, error) {
	if !validID(in.OperationID) || !validAmount(in.Amount) {
		return ports.Refund{}, ErrRejected
	}
	payment, err := p.QueryPayment(ctx, in.PaymentID)
	if err != nil {
		return ports.Refund{}, err
	}
	if payment.Status != ports.PaymentSucceeded || payment.Amount != in.Amount {
		return ports.Refund{}, ErrVerification
	}
	var result struct {
		Data paddleAdjustment `json:"data"`
	}
	if err := p.call(ctx, "POST", "/adjustments", map[string]string{"action": "refund", "type": "full", "transaction_id": in.PaymentID, "reason": paddleRefundReason(in.OperationID)}, &result); err != nil {
		return ports.Refund{}, err
	}
	if result.Data.TransactionID != in.PaymentID || result.Data.Reason != paddleRefundReason(in.OperationID) {
		return ports.Refund{}, ErrVerification
	}
	return p.refundView(ctx, result.Data)
}
func (p *Paddle) QueryRefund(ctx context.Context, id string) (ports.Refund, error) {
	if !paddleID(id, "adj_") {
		return ports.Refund{}, ErrRejected
	}
	var result struct {
		Data paddleAdjustment `json:"data"`
	}
	if err := p.call(ctx, "GET", "/adjustments/"+id, nil, &result); err != nil {
		return ports.Refund{}, err
	}
	if result.Data.ID != id {
		return ports.Refund{}, ErrVerification
	}
	return p.refundView(ctx, result.Data)
}

type paddlePagination struct {
	Pagination struct {
		Next    string `json:"next"`
		HasMore bool   `json:"has_more"`
	} `json:"pagination"`
}

func (p *Paddle) nextPage(meta paddlePagination, path string) (string, error) {
	if !meta.Pagination.HasMore {
		return "", nil
	}
	u, err := url.Parse(meta.Pagination.Next)
	if err != nil || u.Scheme != "https" || u.Host != strings.TrimPrefix(p.base, "https://") || u.Path != path || u.User != nil {
		return "", ErrVerification
	}
	return u.RequestURI(), nil
}
func (p *Paddle) RecoverCheckout(ctx context.Context, in ports.CheckoutRequest) (ports.Checkout, error) {
	path := "/transactions?per_page=30&order_by=created_at%5BDESC%5D"
	if !in.RequestedAt.IsZero() {
		path += "&created_at%5BGTE%5D=" + url.QueryEscape(in.RequestedAt.Add(-5*time.Minute).UTC().Format(time.RFC3339))
	}
	var found *paddleTransaction
	for page := 0; path != "" && page < 20; page++ {
		var result struct {
			Data []paddleTransaction `json:"data"`
			Meta paddlePagination    `json:"meta"`
		}
		if err := p.call(ctx, "GET", path, nil, &result); err != nil {
			return ports.Checkout{}, err
		}
		for _, transaction := range result.Data {
			if transaction.Custom.OrderID == in.OrderID && transaction.Custom.OperationID == in.OperationID {
				if found != nil && found.ID != transaction.ID {
					return ports.Checkout{}, ErrVerification
				}
				copy := transaction
				found = &copy
			}
		}
		var err error
		path, err = p.nextPage(result.Meta, "/transactions")
		if err != nil {
			return ports.Checkout{}, err
		}
	}
	if found == nil || path != "" {
		return ports.Checkout{}, ErrUncertain
	}
	return p.checkout(*found, in)
}
func (p *Paddle) RecoverRefund(ctx context.Context, in ports.RefundRequest) (ports.Refund, error) {
	path := "/adjustments?per_page=30&transaction_id=" + url.QueryEscape(in.PaymentID)
	var found *paddleAdjustment
	for page := 0; path != "" && page < 20; page++ {
		var result struct {
			Data []paddleAdjustment `json:"data"`
			Meta paddlePagination   `json:"meta"`
		}
		if err := p.call(ctx, "GET", path, nil, &result); err != nil {
			return ports.Refund{}, err
		}
		for _, a := range result.Data {
			if a.TransactionID == in.PaymentID && a.Reason == paddleRefundReason(in.OperationID) {
				if found != nil && found.ID != a.ID {
					return ports.Refund{}, ErrVerification
				}
				copy := a
				found = &copy
			}
		}
		var err error
		path, err = p.nextPage(result.Meta, "/adjustments")
		if err != nil {
			return ports.Refund{}, err
		}
	}
	if found == nil || path != "" {
		return ports.Refund{}, ErrUncertain
	}
	refund, err := p.refundView(ctx, *found)
	if err == nil && refund.Amount != in.Amount {
		return ports.Refund{}, ErrVerification
	}
	return refund, err
}
func (p *Paddle) VerifyWebhook(_ context.Context, headers http.Header, body []byte) (ports.VerifiedEvent, error) {
	var timestamp string
	var signatures []string
	for _, part := range strings.Split(headers.Get("Paddle-Signature"), ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		if key == "ts" {
			if timestamp != "" {
				return ports.VerifiedEvent{}, ErrVerification
			}
			timestamp = value
		}
		if key == "h1" {
			signatures = append(signatures, value)
		}
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || time.Since(time.Unix(seconds, 0)) > 5*time.Second || time.Until(time.Unix(seconds, 0)) > 5*time.Second {
		return ports.VerifiedEvent{}, ErrVerification
	}
	mac := hmac.New(sha256.New, []byte(p.config.WebhookSecret))
	mac.Write([]byte(timestamp + ":"))
	mac.Write(body)
	valid := false
	for _, signature := range signatures {
		decoded, err := hex.DecodeString(signature)
		if err == nil && hmac.Equal(decoded, mac.Sum(nil)) {
			valid = true
		}
	}
	if !valid {
		return ports.VerifiedEvent{}, ErrVerification
	}
	var event struct {
		ID   string    `json:"event_id"`
		Kind string    `json:"event_type"`
		At   time.Time `json:"occurred_at"`
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &event) != nil || !paddleID(event.ID, "evt_") || event.At.IsZero() {
		return ports.VerifiedEvent{}, ErrVerification
	}
	prefix := "txn_"
	if strings.HasPrefix(event.Kind, "adjustment.") {
		prefix = "adj_"
	}
	if !paddleID(event.Data.ID, prefix) {
		return ports.VerifiedEvent{}, ErrVerification
	}
	return ports.VerifiedEvent{ID: event.ID, Kind: event.Kind, ResourceID: event.Data.ID, Environment: p.config.Environment, OccurredAt: event.At}, nil
}
