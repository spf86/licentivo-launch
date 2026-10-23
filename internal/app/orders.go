package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"licentivo-launch/internal/domain"
	"licentivo-launch/internal/payments"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/security"
	"licentivo-launch/internal/storage"
)

type CheckoutInput struct {
	Action          string `json:"action,omitempty"`
	QuoteToken      string `json:"quote_token,omitempty"`
	ProductID       string `json:"product_id"`
	PlanVersionID   string `json:"plan_version_id"`
	PurchasePeriods uint64 `json:"purchase_periods,omitempty"`
	Provider        string `json:"provider"`
	Environment     string `json:"environment"`
	IdempotencyKey  string `json:"idempotency_key"`
}
type CheckoutView struct {
	OrderView
	CheckoutURL *string `json:"checkout_url,omitempty"`
	Fulfillment string  `json:"fulfillment"`
}

func (s *Service) checkoutView(ctx context.Context, order storage.Order) (CheckoutView, error) {
	orderView, err := s.orderView(ctx, order)
	if err != nil {
		return CheckoutView{}, err
	}
	v := CheckoutView{OrderView: orderView, Fulfillment: "not_paid"}
	var payment storage.Payment
	if s.db.WithContext(ctx).Where("order_id = ?", order.ID).First(&payment).Error == nil {
		v.CheckoutURL = payment.CheckoutURL
	}
	if order.Status == "paid" {
		v.Fulfillment = "pending_authorization"
		var task storage.SyncTask
		if s.db.WithContext(ctx).First(&task, "source_order_id = ?", order.ID).Error == nil && task.Status == "succeeded" {
			v.Fulfillment = "active"
		}
		if order.FulfillmentReview {
			v.Fulfillment = "manual_review"
		}
	}
	if order.Status == "refunded" {
		v.Fulfillment = "refunded"
	}
	return v, nil
}
func (s *Service) Order(ctx context.Context, p Principal, id string) (CheckoutView, error) {
	var order storage.Order
	if s.db.WithContext(ctx).First(&order, "id = ? AND user_id = ?", id, p.User.ID).Error != nil {
		return CheckoutView{}, ErrNotFound
	}
	return s.checkoutView(ctx, order)
}

func (s *Service) CreateOrder(ctx context.Context, p Principal, in CheckoutInput) (CheckoutView, error) {
	if s.RecoveryRequired(ctx) {
		return CheckoutView{}, problem(409, "RECOVERY_REVIEW_REQUIRED", "商家正在核对恢复后的交易，暂不能创建付款")
	}
	if err := s.requireVerifiedEmail(ctx, s.db, p.User); err != nil {
		return CheckoutView{}, err
	}
	if in.Action == "" {
		in.Action = "purchase"
	}
	if in.Action != "purchase" && in.Action != "renew" && in.Action != "upgrade" {
		return CheckoutView{}, ErrInvalid
	}
	purchasePeriods := in.PurchasePeriods
	if purchasePeriods == 0 {
		purchasePeriods = 1
	}
	if in.Action == "upgrade" && purchasePeriods != 1 {
		return CheckoutView{}, ErrInvalid
	}
	in.PurchasePeriods = purchasePeriods
	if in.Environment != "sandbox" && in.Environment != "live" || in.Provider != "stripe" && in.Provider != "paypal" && in.Provider != "waffo" && in.Provider != "paddle" || len(in.IdempotencyKey) < 16 || len(in.IdempotencyKey) > 80 {
		return CheckoutView{}, ErrInvalid
	}
	fingerprint := sha256.Sum256(marshal(in))
	var existing storage.Order
	if s.db.WithContext(ctx).First(&existing, "user_id = ? AND environment = ? AND idempotency_key = ?", p.User.ID, in.Environment, in.IdempotencyKey).Error == nil {
		if !bytes.Equal(existing.RequestHash, fingerprint[:]) {
			return CheckoutView{}, problem(409, "IDEMPOTENCY_CONFLICT", "请使用原请求恢复订单")
		}
		return s.resumeCheckout(ctx, existing)
	}
	var version storage.PlanVersion
	var quote *saleQuote
	if in.Action != "purchase" {
		var err error
		quote, err = s.readQuote(in.QuoteToken, p, in)
		if err != nil {
			return CheckoutView{}, err
		}
		purchasePeriods = quote.AddedPeriods
	} else if in.QuoteToken != "" {
		return CheckoutView{}, ErrInvalid
	}
	var plan storage.Plan
	var publication storage.PlanPublication
	var mapping storage.PlanLicenseMapping
	var account storage.PaymentAccount
	var connection storage.LicentivoConnection
	if s.db.WithContext(ctx).First(&version, "id = ?", in.PlanVersionID).Error != nil || s.db.WithContext(ctx).First(&plan, "id = ?", version.PlanID).Error != nil {
		return CheckoutView{}, ErrNotFound
	}
	if in.ProductID == "" || in.ProductID != version.ProductID {
		return CheckoutView{}, problem(409, "PRODUCT_PLAN_MISMATCH", "产品与套餐不匹配，请从产品页面重新进入结账")
	}
	checkoutAmount := version.AmountMinor
	if in.Action == "purchase" {
		var err error
		checkoutAmount, err = purchaseAmount(version, purchasePeriods)
		if err != nil {
			return CheckoutView{}, err
		}
	} else if quote != nil {
		checkoutAmount = quote.AmountMinor
	}
	if in.Action == "renew" {
		if !plan.AllowRenewal {
			return CheckoutView{}, problem(409, "RENEWAL_UNAVAILABLE", "商家已停止该套餐续费")
		}
	} else if plan.Status != "published" || !plan.AllowPurchase {
		return CheckoutView{}, ErrNotFound
	}
	if !payments.SupportsAmount(domain.Money{Currency: version.Currency, Minor: checkoutAmount}) {
		return CheckoutView{}, problem(400, "CURRENCY_NOT_SUPPORTED", "该套餐金额或币种尚未通过支付渠道校验")
	}
	if version.PurchaseType == "fixed_term" {
		if version.TermUnit == nil || version.TermCount == nil {
			return CheckoutView{}, ErrInvalid
		}
		periods := 1
		if in.Action == "purchase" || in.Action == "renew" {
			periods = int(purchasePeriods)
		}
		if _, err := (domain.Schedule{Anchor: now(), Unit: domain.TermUnit(*version.TermUnit), Interval: int(*version.TermCount), Periods: periods}).End(); err != nil {
			return CheckoutView{}, problem(400, "TERM_INVALID", "套餐期限超出支持范围")
		}
	}
	if s.db.WithContext(ctx).First(&publication, "plan_id = ? AND environment = ? AND version_id = ? AND preview_only = false", plan.ID, in.Environment, version.ID).Error != nil {
		return CheckoutView{}, problem(409, "PLAN_NOT_FOR_SALE", "该套餐尚未开放收款")
	}
	if s.db.WithContext(ctx).First(&mapping, "plan_version_id = ?", version.ID).Error != nil || s.db.WithContext(ctx).First(&connection, "id = ?", mapping.ConnectionID).Error != nil {
		return CheckoutView{}, problem(409, "LICENSE_MAPPING_REQUIRED", "请先在后台为该套餐选择 Licentivo 授权策略")
	}
	licenseClient, err := s.licensingProvider(ctx, connection)
	if err != nil {
		return CheckoutView{}, ErrStorage
	}
	caps, err := licenseClient.Capabilities(ctx)
	if err != nil || caps.ValidateSales() != nil {
		return CheckoutView{}, problem(409, "LICENSE_CONTRACT_UNSUPPORTED", "Licentivo 管理接口不可用，因此没有创建付款；请检查读写 API Key")
	}
	if err := checkPolicy(ctx, licenseClient, version, mapping, ""); err != nil {
		return CheckoutView{}, err
	}
	if s.db.WithContext(ctx).First(&account, "provider = ? AND environment = ? AND enabled = true", in.Provider, in.Environment).Error != nil {
		return CheckoutView{}, problem(409, "PAYMENT_NOT_CONFIGURED", "该支付渠道尚未配置")
	}
	orderID, err := newID()
	if err != nil {
		return CheckoutView{}, err
	}
	paymentID, err := newID()
	if err != nil {
		return CheckoutView{}, err
	}
	operation, err := newID()
	if err != nil {
		return CheckoutView{}, err
	}
	captureOperation, err := newID()
	if err != nil {
		return CheckoutView{}, err
	}
	var order storage.Order
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user storage.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ? AND status = 'active'", p.User.ID).Error; err != nil {
			return ErrUnauthorized
		}
		if err := s.requireVerifiedEmail(ctx, tx, user); err != nil {
			return err
		}
		if err := tx.First(&existing, "user_id = ? AND environment = ? AND idempotency_key = ?", user.ID, in.Environment, in.IdempotencyKey).Error; err == nil {
			if !bytes.Equal(existing.RequestHash, fingerprint[:]) {
				return problem(409, "IDEMPOTENCY_CONFLICT", "请使用原请求恢复订单")
			}
			order = existing
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var entitlement storage.Entitlement
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&entitlement, "user_id = ? AND product_id = ? AND environment = ?", user.ID, version.ProductID, in.Environment).Error; err == nil {
			if entitlement.ManualHold || entitlement.Status == "revoked" {
				return problem(409, "AUTHORIZATION_HELD", "当前权益需由商家处理")
			}
			if in.Action == "purchase" && entitlement.DesiredVersion > 0 {
				var count int64
				if tx.Model(&storage.EntitlementCoverage{}).Where("entitlement_id = ? AND base_coverage_id IS NULL AND voided_at IS NULL", entitlement.ID).Count(&count).Error != nil {
					return ErrStorage
				}
				if count > 0 {
					return problem(409, "EXISTING_ENTITLEMENT", "已有产品权益，请使用续费或升级入口")
				}
			}
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			if in.Action != "purchase" {
				return ErrNotFound
			}
			id, err := newID()
			if err != nil {
				return err
			}
			ref, err := security.RandomReference()
			if err != nil {
				return err
			}
			entitlement = storage.Entitlement{ID: id, UserID: user.ID, ProductID: version.ProductID, Environment: in.Environment, CustomerRef: &ref, Status: "pending", Snapshot: json.RawMessage(`{}`)}
			if err := tx.Create(&entitlement).Error; err != nil {
				return err
			}
		} else {
			return err
		}
		if quote != nil && (quote.EntitlementID != entitlement.ID || quote.ExpectedVersion != entitlement.DesiredVersion || entitlement.AppliedVersion != entitlement.DesiredVersion) {
			return problem(409, "QUOTE_STALE", "权益已发生变化，请重新报价")
		}
		var currentPlan storage.Plan
		var currentPublication storage.PlanPublication
		if tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&currentPlan, "id = ?", plan.ID).Error != nil || tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&currentPublication, "plan_id = ? AND environment = ? AND version_id = ? AND preview_only = false", plan.ID, in.Environment, version.ID).Error != nil {
			return problem(409, "PLAN_NOT_FOR_SALE", "套餐发布状态已变化，请刷新")
		}
		if in.Action == "renew" {
			if !currentPlan.AllowRenewal {
				return ErrForbidden
			}
		} else if currentPlan.Status != "published" || !currentPlan.AllowPurchase {
			return ErrForbidden
		}
		var pending storage.Order
		if tx.Where("user_id = ? AND product_id = ? AND environment = ? AND status = 'pending' AND expires_at > ?", user.ID, version.ProductID, in.Environment, now()).First(&pending).Error == nil {
			return problem(409, "PENDING_ORDER", "已有未完成订单，请先查看该订单")
		}
		at := now()
		amount := checkoutAmount
		purchasedPeriods := purchasePeriods
		var quoteJSON json.RawMessage
		var quoteExpiry *time.Time
		if quote != nil {
			amount = quote.AmountMinor
			purchasedPeriods = quote.AddedPeriods
			quoteJSON = marshal(quote)
			quoteExpiry = &quote.ExpiresAt
		}
		order = storage.Order{ID: orderID, UserID: user.ID, ProductID: version.ProductID, PlanVersionID: version.ID, Environment: in.Environment, Action: in.Action, Status: "pending", IdempotencyKey: in.IdempotencyKey, RequestHash: fingerprint[:], Currency: version.Currency, AmountMinor: amount, PurchasedPeriods: purchasedPeriods, PriceSnapshot: marshal(map[string]any{"plan_name": plan.Name, "currency": version.Currency, "amount_minor": amount, "purchase_periods": purchasedPeriods}), EntitlementSnapshot: version.EntitlementSnapshot, ExpectedEntitlementVersion: entitlement.DesiredVersion, ExpiresAt: at.Add(24 * time.Hour), CreatedAt: at, QuoteSnapshot: quoteJSON, QuoteExpiresAt: quoteExpiry}
		if err := tx.Create(&order).Error; err != nil {
			return err
		}
		request := ports.CheckoutRequest{OperationID: operation, OrderID: order.ID, Environment: ports.Environment(in.Environment), Amount: domain.Money{Currency: version.Currency, Minor: amount}, Description: plan.Name, ReturnURL: s.PublicURL() + "/account?checkout=" + order.ID, CancelURL: s.PublicURL() + "/account?checkout=" + order.ID}
		request.CustomerID, request.CustomerEmail, request.RequestedAt = user.ID, user.Email, at
		request.NotifyURL = s.PublicURL() + "/api/v1/webhooks/payments/" + account.ID
		request.HostedCheckoutURL = s.PublicURL() + "/pay/paddle/" + account.ID
		return tx.Create(&storage.Payment{ID: paymentID, OrderID: order.ID, AccountID: account.ID, Environment: in.Environment, OperationID: operation, CaptureOperationID: &captureOperation, CheckoutRequest: marshal(request), Status: "creating", Currency: version.Currency, AmountMinor: amount, CreatedAt: at}).Error
	})
	if err != nil {
		return CheckoutView{}, databaseError(err)
	}
	return s.resumeCheckout(ctx, order)
}

func (s *Service) resumeCheckout(ctx context.Context, order storage.Order) (CheckoutView, error) {
	if order.Status != "pending" {
		return s.checkoutView(ctx, order)
	}
	var payment storage.Payment
	if s.db.WithContext(ctx).First(&payment, "order_id = ?", order.ID).Error != nil {
		return CheckoutView{}, ErrStorage
	}
	if payment.CheckoutURL != nil {
		return s.checkoutView(ctx, order)
	}
	if now().After(payment.CreatedAt.Add(23 * time.Hour)) {
		return CheckoutView{}, problem(409, "PAYMENT_REVIEW_REQUIRED", "付款创建结果需要核实，请联系商家，勿重复付款")
	}
	var account storage.PaymentAccount
	if s.db.WithContext(ctx).First(&account, "id = ?", payment.AccountID).Error != nil {
		return CheckoutView{}, ErrStorage
	}
	client, err := s.paymentProvider(ctx, account)
	if err != nil {
		return CheckoutView{}, ErrStorage
	}
	var request ports.CheckoutRequest
	if json.Unmarshal(payment.CheckoutRequest, &request) != nil {
		return CheckoutView{}, ErrStorage
	}
	var checkout ports.Checkout
	var externalErr error
	if recoverable, ok := client.(ports.RecoverablePaymentProvider); ok {
		// Reserve the sole create attempt durably before sending an API that
		// has no idempotency guarantee. Concurrent requests and restarts query.
		at := now()
		reserved := s.db.WithContext(ctx).Model(&storage.Payment{}).Where("id = ? AND checkout_attempted_at IS NULL AND external_checkout_id IS NULL", payment.ID).Update("checkout_attempted_at", at)
		if reserved.Error != nil {
			return CheckoutView{}, ErrStorage
		}
		if reserved.RowsAffected == 1 {
			checkout, externalErr = client.CreateCheckout(ctx, request)
		} else {
			checkout, externalErr = recoverable.RecoverCheckout(ctx, request)
		}
	} else {
		checkout, externalErr = client.CreateCheckout(ctx, request)
	}
	if externalErr != nil {
		at := now().Add(time.Minute)
		_ = s.db.WithContext(ctx).Model(&payment).Updates(map[string]any{"status": "unknown", "next_check_at": at}).Error
		return s.checkoutView(ctx, order)
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current storage.Payment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", payment.ID).Error; err != nil {
			return err
		}
		if current.ExternalCheckoutID != nil && *current.ExternalCheckoutID != checkout.ExternalID {
			return problem(409, "PROVIDER_RESULT_CONFLICT", "付款结果需要人工核实")
		}
		changes := map[string]any{"external_checkout_id": checkout.ExternalID, "checkout_url": checkout.RedirectURL, "next_check_at": now().Add(time.Minute)}
		if current.Status != "succeeded" {
			changes["status"] = "pending"
		}
		return tx.Model(&current).Updates(changes).Error
	})
	if err != nil {
		return CheckoutView{}, databaseError(err)
	}
	return s.checkoutView(ctx, order)
}

func (s *Service) ReconcileOrder(ctx context.Context, p Principal, id string) (CheckoutView, error) {
	var order storage.Order
	if s.db.WithContext(ctx).First(&order, "id = ? AND user_id = ?", id, p.User.ID).Error != nil {
		return CheckoutView{}, ErrNotFound
	}
	if err := s.reconcilePayment(ctx, order.ID); err != nil {
		return CheckoutView{}, err
	}
	if s.db.WithContext(ctx).First(&order, "id = ?", id).Error != nil {
		return CheckoutView{}, ErrStorage
	}
	return s.checkoutView(ctx, order)
}
