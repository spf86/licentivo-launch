package app

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"licentivo-launch/internal/storage"
)

// SoftwareStatus contains actionable account state, never a license credential.
// Runtime permission must still be checked by the Licentivo SDK.
type SoftwareStatus struct {
	ProductID         string     `json:"product_id"`
	Environment       string     `json:"environment"`
	Status            string     `json:"status"`
	Code              string     `json:"code"`
	Message           string     `json:"message"`
	NextAction        string     `json:"next_action"`
	AccountURL        string     `json:"account_url"`
	ValidFrom         *time.Time `json:"valid_from"`
	ValidUntil        *time.Time `json:"valid_until"`
	Synchronized      bool       `json:"synchronized"`
	RetryAfterSeconds int        `json:"retry_after_seconds"`
	MaxRetryAttempts  int        `json:"max_retry_attempts"`
}

func entitlementStatus(row storage.Entitlement, at time.Time) SoftwareStatus {
	v := SoftwareStatus{ProductID: row.ProductID, Environment: row.Environment, ValidFrom: row.ValidFrom, ValidUntil: row.ValidUntil, Synchronized: row.AppliedVersion > 0 && row.AppliedVersion == row.DesiredVersion}
	switch {
	case row.ManualHold || row.Status == "revoked":
		v.Status, v.Code, v.Message, v.NextAction = "suspended", "ENTITLEMENT_SUSPENDED", "软件授权已暂停，请联系商家查看原因", "contact_support"
	case row.Status == "expired" || row.ValidUntil != nil && !row.ValidUntil.After(at):
		v.Status, v.Code, v.Message, v.NextAction = "expired", "ENTITLEMENT_EXPIRED", "软件授权已到期，请前往账号中心续费", "renew"
	case row.Status == "suspended":
		v.Status, v.Code, v.Message, v.NextAction = "suspended", "ENTITLEMENT_SUSPENDED", "软件授权已暂停，请联系商家查看原因", "contact_support"
	case row.Status != "active" || !v.Synchronized || row.ValidFrom == nil || row.ValidFrom.After(at):
		v.Status, v.Code, v.Message, v.NextAction = "pending", "ENTITLEMENT_PENDING", "软件授权正在开通或同步，请稍后重试；持续未完成时请联系商家", "retry"
		v.RetryAfterSeconds, v.MaxRetryAttempts = 3, 5
	default:
		v.Status, v.Code, v.Message, v.NextAction = "ready", "ENTITLEMENT_READY", "软件授权已就绪", "activate"
	}
	return v
}

func unissuedSoftwareStatus(productID, environment string, paid bool) SoftwareStatus {
	if paid {
		return entitlementStatus(storage.Entitlement{ProductID: productID, Environment: environment}, now())
	}
	return SoftwareStatus{ProductID: productID, Environment: environment, Status: "not_purchased", Code: "ENTITLEMENT_NOT_PURCHASED", Message: "该账号在所选环境中尚无已确认付款，请完成购买或检查登录账号", NextAction: "purchase"}
}

func (s *Service) softwareStatus(ctx context.Context, p Principal, productID, environment string) (SoftwareStatus, storage.Entitlement, error) {
	var row storage.Entitlement
	if p.Session.Kind != "desktop" {
		return SoftwareStatus{}, row, ErrForbidden
	}
	if !integrationUUIDPattern.MatchString(productID) || (environment != "sandbox" && environment != "live") {
		return SoftwareStatus{}, row, ErrInvalid
	}
	if err := s.requireVerifiedEmail(ctx, s.db, p.User); err != nil {
		return SoftwareStatus{}, row, err
	}
	err := s.db.WithContext(ctx).First(&row, "user_id = ? AND product_id = ? AND environment = ?", p.User.ID, productID, environment).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return SoftwareStatus{}, row, ErrStorage
	}
	var view SoftwareStatus
	if errors.Is(err, gorm.ErrRecordNotFound) || (!row.ManualHold && row.Status == "pending" && row.AppliedVersion == 0 && row.DesiredVersion == 0 && row.ValidFrom == nil) {
		// Checkout creates an empty entitlement before payment. Only confirmed
		// payment warrants issuance retries; abandoned checkout is not a license.
		// Payment can also be confirmed before a missing row is recovered.
		var paid int64
		if s.db.WithContext(ctx).Model(&storage.Order{}).Where("user_id = ? AND product_id = ? AND environment = ? AND status = 'paid'", p.User.ID, productID, environment).Count(&paid).Error != nil {
			return view, row, ErrStorage
		}
		view = unissuedSoftwareStatus(productID, environment, paid > 0)
	} else {
		view = entitlementStatus(row, now())
	}
	view.AccountURL = s.PublicURL() + "/#/account"
	return view, row, nil
}

func (s *Service) SoftwareEntitlementStatus(ctx context.Context, p Principal, productID, environment string) (SoftwareStatus, error) {
	view, row, err := s.softwareStatus(ctx, p, productID, environment)
	if err != nil {
		return view, err
	}
	if view.Status == "ready" {
		var count int64
		if s.db.WithContext(ctx).Model(&storage.LicenseBinding{}).Where("entitlement_id = ? AND replaced_at IS NULL", row.ID).Count(&count).Error != nil {
			return view, ErrStorage
		}
		if count == 0 {
			view.Status, view.Code, view.Message, view.NextAction = "pending", "ENTITLEMENT_PENDING", "软件凭证正在恢复，请稍后重试；持续未完成时请联系商家", "retry"
			view.RetryAfterSeconds, view.MaxRetryAttempts = 3, 5
		}
	}
	return view, nil
}
