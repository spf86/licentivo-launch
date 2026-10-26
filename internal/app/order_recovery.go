package app

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/storage"
)

// Reuse the original verified settlement under the same lock order as payment
// processing. This never creates another checkout or moves money.
func (s *Service) recoverReviewedRenewal(tx *gorm.DB, id string) (bool, error) {
	var order storage.Order
	if err := tx.First(&order, "id = ?", id).Error; err != nil {
		return false, err
	}
	var user storage.User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", order.UserID).Error; err != nil {
		return false, err
	}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", id).Error; err != nil {
		return false, err
	}
	if !order.FulfillmentReview {
		return false, nil
	}
	review := problem(409, "FULFILLMENT_REVIEW_REQUIRED", "付款已记录，但权益变化仍需商家核对")
	if order.Status != "paid" || order.Action != "renew" || order.PaidAt == nil {
		return false, review
	}
	var payment storage.Payment
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&payment, "order_id = ?", id).Error; err != nil {
		return false, err
	}
	if payment.Status != "succeeded" || payment.ExternalTransactionID == nil || *payment.ExternalTransactionID == "" || payment.PaidAt == nil || !payment.PaidAt.Equal(*order.PaidAt) || payment.Currency != order.Currency || payment.AmountMinor != order.AmountMinor || payment.Environment != order.Environment {
		return false, review
	}
	var count int64
	if err := tx.Model(&storage.EntitlementCoverage{}).Where("source_order_id = ?", id).Count(&count).Error; err != nil {
		return false, err
	}
	if count != 0 {
		return false, review
	}
	if err := s.applyPaidCoverage(tx, order, *payment.PaidAt); err != nil {
		return false, err
	}
	if err := tx.First(&order, "id = ?", id).Error; err != nil {
		return false, err
	}
	if order.FulfillmentReview {
		return false, review
	}
	return true, nil
}
