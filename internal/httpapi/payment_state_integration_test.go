package httpapi_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"licentivo-launch/internal/app"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/storage"
)

type delayedCheckoutProvider struct {
	*paymentDouble
	delayNext atomic.Bool
	entered   chan struct{}
	release   chan struct{}
}

func (p *delayedCheckoutProvider) CreateCheckout(ctx context.Context, in ports.CheckoutRequest) (ports.Checkout, error) {
	if p.delayNext.CompareAndSwap(true, false) {
		close(p.entered)
		select {
		case <-p.release:
			return ports.Checkout{}, errors.New("checkout response timed out")
		case <-ctx.Done():
			return ports.Checkout{}, ctx.Err()
		}
	}
	return p.paymentDouble.CreateCheckout(ctx, in)
}

func testCheckoutStateTransitions(t *testing.T, a *testApp, config app.Config, input app.CheckoutInput) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	provider := &delayedCheckoutProvider{paymentDouble: newPaymentDouble(t), entered: make(chan struct{}), release: make(chan struct{})}
	config.PaymentFactory = func(context.Context, storage.PaymentAccount) (ports.PaymentProvider, error) { return provider, nil }
	service, err := app.New(ctx, a.db, config)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	email := "checkout-state@example.invalid"
	customer := app.Principal{User: storage.User{ID: newFixtureID(t), Email: email, EmailKey: email, EmailVerifiedAt: &at, Status: "active"}}
	if err := a.db.Create(&customer.User).Error; err != nil {
		t.Fatal(err)
	}
	input.IdempotencyKey = "cancelled-checkout-operation"
	order, err := service.CreateOrder(ctx, customer, input)
	if err != nil {
		t.Fatal(err)
	}
	checkoutID := "cs_test_" + order.ID
	original := provider.payments[checkoutID]
	mismatched := original
	mismatched.Status, mismatched.OrderID = ports.PaymentFailed, newFixtureID(t)
	provider.payments[checkoutID] = mismatched
	_, err = service.ReconcileOrder(ctx, customer, order.ID)
	var verification *app.Error
	if !errors.As(err, &verification) || verification.Code != "PAYMENT_VERIFICATION_FAILED" {
		t.Fatalf("mismatched cancellation was accepted: %v", err)
	}
	provider.payments[checkoutID] = original
	for _, status := range []ports.PaymentStatus{ports.PaymentPending, ports.PaymentUnknown, ports.PaymentFailed} {
		result := provider.payments[checkoutID]
		result.Status = status
		provider.payments[checkoutID] = result
		view, err := service.ReconcileOrder(ctx, customer, order.ID)
		if err != nil {
			t.Fatal(err)
		}
		var payment storage.Payment
		if err := a.db.First(&payment, "order_id = ?", order.ID).Error; err != nil {
			t.Fatal(err)
		}
		if status == ports.PaymentFailed {
			if view.Status != "closed" || view.CheckoutURL != nil || payment.Status != "failed" || payment.NextCheckAt != nil {
				t.Fatalf("terminal result left order %s, payment %s, next check %v, checkout URL %v", view.Status, payment.Status, payment.NextCheckAt, view.CheckoutURL)
			}
		} else if view.Status != "pending" || payment.NextCheckAt == nil {
			t.Fatalf("retryable %s left order %s, next check %v", status, view.Status, payment.NextCheckAt)
		}
	}

	// The closed order must allow a fresh checkout immediately. Delay one of
	// two overlapping submissions until its peer has already settled payment.
	input.IdempotencyKey = "replacement-checkout-operation"
	provider.delayNext.Store(true)
	type outcome struct {
		order app.CheckoutView
		err   error
	}
	slow := make(chan outcome, 1)
	go func() {
		view, err := service.CreateOrder(ctx, customer, input)
		slow <- outcome{view, err}
	}()
	select {
	case <-provider.entered:
	case result := <-slow:
		t.Fatalf("replacement checkout did not reach provider: %v", result.err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	replacement, err := service.CreateOrder(ctx, customer, input)
	if err != nil {
		t.Fatal(err)
	}
	provider.markPaid("cs_test_" + replacement.ID)
	paid, err := service.ReconcileOrder(ctx, customer, replacement.ID)
	if err != nil || paid.Status != "paid" {
		t.Fatalf("initial settlement: %s, %v", paid.Status, err)
	}
	close(provider.release)
	result := <-slow
	if result.err != nil || result.order.Status != "paid" {
		t.Fatalf("late timeout returned %s, %v", result.order.Status, result.err)
	}
	var settled storage.Payment
	if err := a.db.First(&settled, "order_id = ?", replacement.ID).Error; err != nil {
		t.Fatal(err)
	}
	if settled.Status != "succeeded" || settled.NextCheckAt != nil {
		t.Fatalf("late timeout replaced settlement: %s, next check %v", settled.Status, settled.NextCheckAt)
	}
	// A receipt written by an older release may already have been marked
	// unknown. Reconcile it without issuing a second confirmation or coverage.
	if err := a.db.Model(&settled).Update("status", "unknown").Error; err != nil {
		t.Fatal(err)
	}
	paid, err = service.ReconcileOrder(ctx, customer, replacement.ID)
	if err != nil || paid.Status != "paid" || paid.Fulfillment == "manual_review" {
		t.Fatalf("reconciling prior settlement: %s, %s, %v", paid.Status, paid.Fulfillment, err)
	}
	var confirmations int64
	if err := a.db.Model(&storage.AuditEntry{}).Where("action = 'payment.confirmed' AND object_id = ?", replacement.ID).Count(&confirmations).Error; err != nil || confirmations != 1 {
		t.Fatalf("expected one settlement confirmation, got %d: %v", confirmations, err)
	}
}
