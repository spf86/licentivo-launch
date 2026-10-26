package payments

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"licentivo-launch/internal/domain"
	"licentivo-launch/internal/ports"
)

var ErrNotReversal = errors.New("not a payment reversal")

func (s *Stripe) QueryPaymentReversal(ctx context.Context, event ports.VerifiedEvent) (ports.PaymentReversal, error) {
	if !strings.HasPrefix(event.Kind, "charge.dispute.") || !validID(event.ResourceID) {
		return ports.PaymentReversal{}, ErrNotReversal
	}
	var dispute struct {
		ID, Status, Currency string
		PaymentIntent        string `json:"payment_intent"`
		Amount               int64
		Live                 bool `json:"livemode"`
	}
	if err := s.call(ctx, "GET", "/v1/disputes/"+url.PathEscape(event.ResourceID), "", nil, &dispute); err != nil {
		return ports.PaymentReversal{}, err
	}
	if dispute.ID != event.ResourceID || dispute.Live != (s.config.Environment == ports.Live) || !validID(dispute.PaymentIntent) {
		return ports.PaymentReversal{}, ErrVerification
	}
	var intent struct {
		ID, Currency   string
		AmountReceived int64 `json:"amount_received"`
		Metadata       struct {
			OrderID string `json:"launch_order_id"`
		} `json:"metadata"`
		Charge *struct {
			Captured bool
			Created  int64
		} `json:"latest_charge"`
	}
	if err := s.call(ctx, "GET", "/v1/payment_intents/"+url.PathEscape(dispute.PaymentIntent)+"?expand[]=latest_charge", "", nil, &intent); err != nil {
		return ports.PaymentReversal{}, err
	}
	amount := domain.Money{Currency: strings.ToUpper(dispute.Currency), Minor: dispute.Amount}
	original := domain.Money{Currency: strings.ToUpper(intent.Currency), Minor: intent.AmountReceived}
	if intent.ID != dispute.PaymentIntent || !validID(intent.Metadata.OrderID) || !validAmount(amount) || !validAmount(original) || amount.Currency != original.Currency || amount.Minor > original.Minor || intent.Charge == nil || !intent.Charge.Captured || intent.Charge.Created <= 0 {
		return ports.PaymentReversal{}, ErrVerification
	}
	at := time.Unix(intent.Charge.Created, 0).UTC()
	return ports.PaymentReversal{ExternalID: dispute.ID, PaymentID: intent.ID, OrderID: intent.Metadata.OrderID, MerchantRef: s.config.MerchantRef, Environment: s.config.Environment, Amount: amount, OriginalAmount: original, PaidAt: &at, Confirmed: dispute.Status == "lost"}, nil
}

func (p *PayPal) QueryPaymentReversal(ctx context.Context, event ports.VerifiedEvent) (ports.PaymentReversal, error) {
	if event.Kind != "PAYMENT.CAPTURE.REVERSED" || !validID(event.ResourceID) {
		return ports.PaymentReversal{}, ErrNotReversal
	}
	var capture struct {
		ID     string
		Amount struct {
			Currency string `json:"currency_code"`
			Value    string
		} `json:"amount"`
		Payee struct {
			Merchant string `json:"merchant_id"`
		} `json:"payee"`
		Related struct {
			IDs struct {
				OrderID string `json:"order_id"`
			} `json:"related_ids"`
		} `json:"supplementary_data"`
		Created time.Time `json:"create_time"`
	}
	if err := p.call(ctx, "GET", "/v2/payments/captures/"+url.PathEscape(event.ResourceID), "", nil, &capture); err != nil {
		return ports.PaymentReversal{}, err
	}
	original, err := parsePayPalAmount(capture.Amount.Currency, capture.Amount.Value)
	if err != nil || capture.ID != event.ResourceID || capture.Payee.Merchant != p.config.MerchantRef || !validID(capture.Related.IDs.OrderID) || capture.Created.IsZero() || event.ReversalAmount == nil || !validAmount(*event.ReversalAmount) || event.ReversalAmount.Currency != original.Currency || event.ReversalAmount.Minor > original.Minor {
		return ports.PaymentReversal{}, ErrVerification
	}
	var order paypalOrder
	if err := p.call(ctx, "GET", "/v2/checkout/orders/"+url.PathEscape(capture.Related.IDs.OrderID), "", nil, &order); err != nil {
		return ports.PaymentReversal{}, err
	}
	if order.ID != capture.Related.IDs.OrderID || len(order.Units) != 1 || order.Units[0].Payee.Merchant != p.config.MerchantRef || !validID(order.Units[0].Reference) {
		return ports.PaymentReversal{}, ErrVerification
	}
	// The authenticated reversal event proves the money movement; GET binds it
	// to this merchant's original capture, order, amount and currency.
	return ports.PaymentReversal{ExternalID: capture.ID, PaymentID: capture.ID, CheckoutID: order.ID, OrderID: order.Units[0].Reference, MerchantRef: p.config.MerchantRef, Environment: p.config.Environment, Amount: *event.ReversalAmount, OriginalAmount: original, PaidAt: &capture.Created, Confirmed: true}, nil
}

func (p *Paddle) QueryPaymentReversal(ctx context.Context, event ports.VerifiedEvent) (ports.PaymentReversal, error) {
	if !strings.HasPrefix(event.Kind, "adjustment.") || !paddleID(event.ResourceID, "adj_") {
		return ports.PaymentReversal{}, ErrNotReversal
	}
	var result struct {
		Data paddleAdjustment `json:"data"`
	}
	if err := p.call(ctx, "GET", "/adjustments/"+event.ResourceID, nil, &result); err != nil {
		return ports.PaymentReversal{}, err
	}
	a := result.Data
	if a.ID != event.ResourceID {
		return ports.PaymentReversal{}, ErrVerification
	}
	if a.Action == "refund" {
		return ports.PaymentReversal{}, ErrNotReversal
	}
	if a.Action != "chargeback" {
		return ports.PaymentReversal{ExternalID: a.ID}, nil
	}
	money, err := paddleMoney(a.Currency, a.Totals.Total)
	if err != nil {
		return ports.PaymentReversal{}, err
	}
	payment, err := p.QueryPayment(ctx, a.TransactionID)
	if err != nil {
		return ports.PaymentReversal{}, err
	}
	if payment.Status != ports.PaymentSucceeded || payment.Amount.Currency != money.Currency || money.Minor > payment.Amount.Minor {
		return ports.PaymentReversal{}, ErrVerification
	}
	return ports.PaymentReversal{ExternalID: a.ID, PaymentID: payment.ExternalID, CheckoutID: payment.ExternalID, OrderID: payment.OrderID, MerchantRef: p.config.MerchantRef, Environment: p.config.Environment, Amount: money, OriginalAmount: payment.Amount, PaidAt: payment.PaidAt, Confirmed: a.Status == "approved"}, nil
}
