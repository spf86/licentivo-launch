// Package ports defines the external contracts. Concrete payment adapters are
// delivered and sandbox-tested in M2; declaring an interface is not support.
package ports

import (
	"context"
	"net/http"
	"time"

	"licentivo-launch/internal/domain"
)

type Environment string

const (
	Sandbox Environment = "sandbox"
	Live    Environment = "live"
)

type PaymentStatus string

const (
	PaymentPending   PaymentStatus = "pending"
	PaymentSucceeded PaymentStatus = "succeeded"
	PaymentFailed    PaymentStatus = "failed"
	PaymentUnknown   PaymentStatus = "unknown"
)

type CheckoutRequest struct {
	OperationID       string
	OrderID           string
	Environment       Environment
	Amount            domain.Money
	Description       string
	ReturnURL         string
	CancelURL         string
	CustomerID        string
	CustomerEmail     string
	NotifyURL         string
	HostedCheckoutURL string
	RequestedAt       time.Time
}

// RecoverablePaymentProvider has create endpoints without provider-enforced
// idempotency. Callers must persist an attempt before submitting, and use these
// read-only methods after any ambiguous result, including process restarts.
type RecoverablePaymentProvider interface {
	RecoverCheckout(context.Context, CheckoutRequest) (Checkout, error)
	RecoverRefund(context.Context, RefundRequest) (Refund, error)
}

type Checkout struct {
	ExternalID  string
	RedirectURL string
	ExpiresAt   time.Time
}

type Payment struct {
	ExternalID  string
	OrderID     string
	MerchantRef string
	Environment Environment
	Amount      domain.Money
	Status      PaymentStatus
	PaidAt      *time.Time
}

type RefundRequest struct {
	OperationID string
	PaymentID   string
	Amount      domain.Money
	RequestedAt time.Time
}

type Refund struct {
	ExternalID string
	PaymentID  string
	Amount     domain.Money
	Status     PaymentStatus
}

// VerifiedEvent is trusted only after signature and configured merchant/env
// checks. Money and ownership still must be matched with the local order.
type VerifiedEvent struct {
	ID             string
	Kind           string
	ResourceID     string
	Environment    Environment
	OccurredAt     time.Time
	ReversalAmount *domain.Money `json:",omitempty"`
}

type PaymentProvider interface {
	CreateCheckout(context.Context, CheckoutRequest) (Checkout, error)
	QueryPayment(context.Context, string) (Payment, error)
	CapturePayment(context.Context, string, string) (Payment, error)
	Refund(context.Context, RefundRequest) (Refund, error)
	QueryRefund(context.Context, string) (Refund, error)
	VerifyWebhook(context.Context, http.Header, []byte) (VerifiedEvent, error)
}

// Reversals are commercial payment facts, never runtime permission decisions.
// Providers re-query the original resource using the merchant's credentials.
type PaymentReversal struct {
	ExternalID, PaymentID, CheckoutID, OrderID, MerchantRef string
	Environment                                             Environment
	Amount, OriginalAmount                                  domain.Money
	PaidAt                                                  *time.Time
	Confirmed                                               bool
}
type PaymentReversalProvider interface {
	QueryPaymentReversal(context.Context, VerifiedEvent) (PaymentReversal, error)
}

// RefundRecoveryProvider reads a previously submitted operation without money
// movement. It is deliberately independent of checkout recovery support.
type RefundRecoveryProvider interface {
	RecoverRefund(context.Context, RefundRequest) (Refund, error)
}
