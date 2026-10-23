package payments

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/url"
	"strings"
	"time"

	"licentivo-launch/internal/ports"
)

// Waffo implements the Waffo Payment API, not the separate Pancake API.
type WaffoConfig struct {
	APIKey, MerchantRef, PrivateKey, PublicKey string
	Environment                                ports.Environment
}
type Waffo struct {
	config     WaffoConfig
	http       *http.Client
	base       string
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
}

func rsaDER(value string) ([]byte, error) {
	if block, _ := pem.Decode([]byte(value)); block != nil {
		return block.Bytes, nil
	}
	return base64.StdEncoding.DecodeString(strings.Join(strings.Fields(value), ""))
}
func NewWaffo(c WaffoConfig) (*Waffo, error) {
	if c.APIKey == "" || strings.ContainsAny(c.APIKey, "\r\n") || !validID(c.MerchantRef) || c.Environment != ports.Sandbox && c.Environment != ports.Live {
		return nil, ErrCredentials
	}
	privateDER, err := rsaDER(c.PrivateKey)
	if err != nil {
		return nil, ErrCredentials
	}
	private, err := x509.ParsePKCS8PrivateKey(privateDER)
	if err != nil {
		private, err = x509.ParsePKCS1PrivateKey(privateDER)
	}
	key, ok := private.(*rsa.PrivateKey)
	if err != nil || !ok || key.N.BitLen() < 2048 || key.Validate() != nil {
		return nil, ErrCredentials
	}
	publicDER, err := rsaDER(c.PublicKey)
	if err != nil {
		return nil, ErrCredentials
	}
	public, err := x509.ParsePKIXPublicKey(publicDER)
	if err != nil {
		public, err = x509.ParsePKCS1PublicKey(publicDER)
	}
	pub, ok := public.(*rsa.PublicKey)
	if err != nil || !ok || pub.N.BitLen() < 2048 {
		return nil, ErrCredentials
	}
	base := "https://api-sandbox.waffo.com/api/v1"
	if c.Environment == ports.Live {
		base = "https://api.waffo.com/api/v1"
	}
	return &Waffo{c, client(), base, key, pub}, nil
}
func (w *Waffo) sign(raw []byte) (string, error) {
	sum := sha256.Sum256(raw)
	sig, err := rsa.SignPKCS1v15(rand.Reader, w.privateKey, crypto.SHA256, sum[:])
	return base64.StdEncoding.EncodeToString(sig), err
}
func (w *Waffo) verify(raw []byte, signature string) bool {
	sig, err := base64.StdEncoding.DecodeString(signature)
	sum := sha256.Sum256(raw)
	return err == nil && rsa.VerifyPKCS1v15(w.publicKey, crypto.SHA256, sum[:], sig) == nil
}
func (w *Waffo) call(ctx context.Context, path string, in, out any) error {
	raw := encoded(in)
	sig, err := w.sign(raw)
	if err != nil {
		return ErrCredentials
	}
	body, headers, err := requestRaw(ctx, w.http, "POST", w.base+path, "application/json", raw, map[string]string{"X-API-KEY": w.config.APIKey, "X-SIGNATURE": sig, "X-API-VERSION": "1.0.0"})
	if err != nil {
		return err
	}
	if signature := headers.Get("X-SIGNATURE"); signature != "" && !w.verify(body, signature) {
		return ErrVerification
	}
	var result struct {
		Code string          `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &result) != nil || result.Code == "E0001" {
		return ErrUncertain
	}
	if result.Code != "0" {
		return ErrRejected
	}
	if out != nil && json.Unmarshal(result.Data, out) != nil {
		return ErrUncertain
	}
	return nil
}
func (w *Waffo) VerifyAccount(ctx context.Context) error {
	var data struct {
		MerchantID string `json:"merchantId"`
	}
	if err := w.call(ctx, "/merchantconfig/inquiry", map[string]string{"merchantId": w.config.MerchantRef}, &data); err != nil {
		return err
	}
	if data.MerchantID != w.config.MerchantRef {
		return ErrVerification
	}
	return nil
}

type waffoOrder struct {
	ID          string `json:"acquiringOrderId"`
	OrderID     string `json:"merchantOrderId"`
	RequestID   string `json:"paymentRequestId"`
	Action      string `json:"orderAction"`
	Status      string `json:"orderStatus"`
	Currency    string `json:"orderCurrency"`
	Amount      string `json:"orderAmount"`
	CompletedAt string `json:"orderCompletedAt"`
	Merchant    struct {
		ID string `json:"merchantId"`
	} `json:"merchantInfo"`
}

func (w *Waffo) checkout(data waffoOrder) (ports.Checkout, error) {
	var action struct {
		URL string `json:"webUrl"`
	}
	if json.Unmarshal([]byte(data.Action), &action) != nil || !validID(data.ID) {
		return ports.Checkout{}, ErrVerification
	}
	u, err := url.Parse(action.URL)
	// Hosted cashiers use Waffo-controlled domains. Never redirect to an
	// arbitrary payment-method deep link from a server API response.
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || !strings.HasSuffix(u.Hostname(), ".waffo.com") {
		return ports.Checkout{}, ErrVerification
	}
	return ports.Checkout{ExternalID: data.ID, RedirectURL: action.URL}, nil
}
func (w *Waffo) CreateCheckout(ctx context.Context, in ports.CheckoutRequest) (ports.Checkout, error) {
	if in.Environment != w.config.Environment || !validAmount(in.Amount) || !validID(in.OperationID) || !validID(in.OrderID) {
		return ports.Checkout{}, ErrRejected
	}
	requested := in.RequestedAt
	if requested.IsZero() {
		return ports.Checkout{}, ErrRejected
	}
	requestID := strings.ReplaceAll(in.OperationID, "-", "")
	body := map[string]any{
		"paymentRequestId": requestID, "merchantOrderId": in.OrderID,
		"orderCurrency": in.Amount.Currency, "userCurrency": in.Amount.Currency, "orderAmount": paypalAmount(in.Amount)["value"],
		"orderDescription": in.Description, "orderRequestedAt": requested.UTC().Format(time.RFC3339Nano),
		"successRedirectUrl": in.ReturnURL, "failedRedirectUrl": in.CancelURL, "cancelRedirectUrl": in.CancelURL, "notifyUrl": in.NotifyURL,
		"merchantInfo": map[string]string{"merchantId": w.config.MerchantRef},
		"userInfo":     map[string]string{"userId": in.CustomerID, "userEmail": in.CustomerEmail, "userTerminal": "WEB"},
		"goodsInfo":    map[string]string{"goodsName": in.Description, "goodsUrl": in.ReturnURL},
		"paymentInfo":  map[string]string{"productName": "ONE_TIME_PAYMENT"},
	}
	var data waffoOrder
	if err := w.call(ctx, "/order/create", body, &data); err != nil {
		// The request ID is stable across retries, including a lost response.
		if queryErr := w.call(ctx, "/order/inquiry", map[string]string{"paymentRequestId": requestID}, &data); queryErr != nil {
			return ports.Checkout{}, err
		}
	}
	if data.RequestID != requestID || data.OrderID != in.OrderID {
		return ports.Checkout{}, ErrVerification
	}
	return w.checkout(data)
}
func (w *Waffo) QueryPayment(ctx context.Context, id string) (ports.Payment, error) {
	if !validID(id) {
		return ports.Payment{}, ErrRejected
	}
	var data waffoOrder
	if err := w.call(ctx, "/order/inquiry", map[string]string{"acquiringOrderId": id}, &data); err != nil {
		return ports.Payment{}, err
	}
	money, err := parsePayPalAmount(data.Currency, data.Amount)
	if err != nil || !validAmount(money) || data.ID != id || !validID(data.OrderID) || data.Merchant.ID != w.config.MerchantRef {
		return ports.Payment{}, ErrVerification
	}
	p := ports.Payment{ExternalID: data.ID, OrderID: data.OrderID, MerchantRef: w.config.MerchantRef, Environment: w.config.Environment, Amount: money, Status: ports.PaymentUnknown}
	switch data.Status {
	case "PAY_SUCCESS":
		at, err := time.Parse(time.RFC3339Nano, data.CompletedAt)
		if err != nil {
			return ports.Payment{}, ErrVerification
		}
		p.Status, p.PaidAt = ports.PaymentSucceeded, &at
	case "ORDER_CLOSE":
		p.Status = ports.PaymentFailed
	case "PAY_IN_PROGRESS", "AUTHORIZATION_REQUIRED", "AUTHED_WAITING_CAPTURE", "CAPTURE_IN_PROGRESS":
		p.Status = ports.PaymentPending
	}
	return p, nil
}
func (w *Waffo) CapturePayment(ctx context.Context, id, _ string) (ports.Payment, error) {
	return w.QueryPayment(ctx, id)
}

type waffoRefund struct {
	RequestID string `json:"refundRequestId"`
	PaymentID string `json:"acquiringOrderId"`
	Amount    string `json:"refundAmount"`
	Status    string `json:"refundStatus"`
}

func (w *Waffo) Refund(ctx context.Context, in ports.RefundRequest) (ports.Refund, error) {
	if !validID(in.PaymentID) || !validID(in.OperationID) || !validAmount(in.Amount) {
		return ports.Refund{}, ErrRejected
	}
	id := strings.ReplaceAll(in.OperationID, "-", "")
	payment, err := w.QueryPayment(ctx, in.PaymentID)
	if err != nil {
		return ports.Refund{}, err
	}
	if payment.Amount != in.Amount {
		return ports.Refund{}, ErrVerification
	}
	if payment.Status != ports.PaymentSucceeded {
		// A previous submission may already have moved the order into a
		// refund state before its response was lost. Recover that operation
		// by its stable ID; never submit another refund for a non-paid order.
		found, err := w.QueryRefund(ctx, id)
		if err != nil {
			return ports.Refund{}, err
		}
		if found.PaymentID != in.PaymentID || found.Amount != in.Amount {
			return ports.Refund{}, ErrVerification
		}
		return found, nil
	}
	var data waffoRefund
	requestedAt := in.RequestedAt
	if requestedAt.IsZero() {
		requestedAt = time.Now().UTC()
	}
	err = w.call(ctx, "/order/refund", map[string]string{"refundRequestId": id, "acquiringOrderId": in.PaymentID, "merchantId": w.config.MerchantRef, "requestedAt": requestedAt.UTC().Format(time.RFC3339Nano), "refundAmount": paypalAmount(in.Amount)["value"], "refundReason": "Customer requested a full refund"}, &data)
	if err != nil {
		if found, queryErr := w.QueryRefund(ctx, id); queryErr == nil {
			return found, nil
		}
		return ports.Refund{}, err
	}
	// Query after submission: an accepted refund is not necessarily completed.
	return w.QueryRefund(ctx, id)
}
func (w *Waffo) QueryRefund(ctx context.Context, id string) (ports.Refund, error) {
	if !validID(id) {
		return ports.Refund{}, ErrRejected
	}
	var data waffoRefund
	if err := w.call(ctx, "/refund/inquiry", map[string]string{"refundRequestId": id}, &data); err != nil {
		return ports.Refund{}, err
	}
	if data.RequestID != id || !validID(data.PaymentID) {
		return ports.Refund{}, ErrVerification
	}
	payment, err := w.QueryPayment(ctx, data.PaymentID)
	if err != nil {
		return ports.Refund{}, err
	}
	money, err := parsePayPalAmount(payment.Amount.Currency, data.Amount)
	if err != nil || !validAmount(money) {
		return ports.Refund{}, ErrVerification
	}
	status := ports.PaymentUnknown
	switch data.Status {
	case "ORDER_FULLY_REFUNDED":
		status = ports.PaymentSucceeded
	case "ORDER_REFUND_FAILED":
		status = ports.PaymentFailed
	case "REFUND_IN_PROGRESS":
		status = ports.PaymentPending
	}
	return ports.Refund{ExternalID: id, PaymentID: data.PaymentID, Amount: money, Status: status}, nil
}
func (w *Waffo) VerifyWebhook(_ context.Context, headers http.Header, raw []byte) (ports.VerifiedEvent, error) {
	if !w.verify(raw, headers.Get("X-SIGNATURE")) {
		return ports.VerifiedEvent{}, ErrVerification
	}
	var data struct {
		Kind   string `json:"eventType"`
		Result struct {
			OrderID         string `json:"acquiringOrderId"`
			RefundID        string `json:"refundRequestId"`
			UpdatedAt       string `json:"orderUpdatedAt"`
			RefundUpdatedAt string `json:"refundUpdatedAt"`
		} `json:"result"`
	}
	if json.Unmarshal(raw, &data) != nil {
		return ports.VerifiedEvent{}, ErrVerification
	}
	id, updated := data.Result.OrderID, data.Result.UpdatedAt
	if data.Kind == "REFUND_NOTIFICATION" {
		id, updated = data.Result.RefundID, data.Result.RefundUpdatedAt
	}
	if data.Kind != "PAYMENT_NOTIFICATION" && data.Kind != "REFUND_NOTIFICATION" || !validID(id) {
		return ports.VerifiedEvent{}, ErrVerification
	}
	// Waffo has no event ID. A digest of the signed bytes deduplicates retries
	// while allowing subsequent status changes for the same payment/refund.
	sum := sha256.Sum256(raw)
	at, _ := time.Parse(time.RFC3339Nano, updated)
	return ports.VerifiedEvent{ID: "waffo_" + hex.EncodeToString(sum[:]), Kind: data.Kind, ResourceID: id, Environment: w.config.Environment, OccurredAt: at}, nil
}
func (w *Waffo) WebhookAcknowledgement() ([]byte, http.Header, error) {
	body := []byte(`{"message":"success"}`)
	sig, err := w.sign(body)
	return body, http.Header{"X-Signature": []string{sig}}, err
}
