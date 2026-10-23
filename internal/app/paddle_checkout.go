package app

import (
	"context"
	"encoding/json"
	"strings"

	"licentivo-launch/internal/storage"
)

// PaddleCheckoutPage contains only the publishable client token. Server API
// credentials and webhook secrets must never be included in this view.
type PaddleCheckoutPage struct {
	ClientToken, Environment, TransactionID, ReturnURL string
}

func (s *Service) PaddleCheckout(ctx context.Context, accountID, transactionID string) (PaddleCheckoutPage, error) {
	if len(transactionID) != 30 || !strings.HasPrefix(transactionID, "txn_") {
		return PaddleCheckoutPage{}, ErrNotFound
	}
	var account storage.PaymentAccount
	var payment storage.Payment
	var order storage.Order
	if s.db.WithContext(ctx).First(&account, "id = ? AND provider = 'paddle'", accountID).Error != nil ||
		s.db.WithContext(ctx).First(&payment, "account_id = ? AND external_checkout_id = ?", account.ID, transactionID).Error != nil ||
		s.db.WithContext(ctx).First(&order, "id = ?", payment.OrderID).Error != nil {
		return PaddleCheckoutPage{}, ErrNotFound
	}
	if order.Status != "pending" || !order.ExpiresAt.After(now()) {
		return PaddleCheckoutPage{}, problem(409, "CHECKOUT_CLOSED", "该订单已完成或已过期，请返回账号中心")
	}
	raw, err := s.readSecret(ctx, account.CredentialSecretID)
	if err != nil {
		return PaddleCheckoutPage{}, err
	}
	var config PaymentSettings
	if json.Unmarshal(raw, &config) != nil || config.Provider != "paddle" || config.Environment != account.Environment || config.MerchantRef != account.MerchantRef {
		return PaddleCheckoutPage{}, ErrStorage
	}
	return PaddleCheckoutPage{ClientToken: config.ClientToken, Environment: account.Environment, TransactionID: transactionID, ReturnURL: s.PublicURL() + "/account?checkout=" + order.ID}, nil
}
