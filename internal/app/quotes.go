package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"gorm.io/gorm"
	"licentivo-launch/internal/domain"
	"licentivo-launch/internal/payments"
	"licentivo-launch/internal/storage"
)

type QuoteInput struct {
	ProductID       string `json:"product_id"`
	PlanVersionID   string `json:"plan_version_id"`
	Environment     string `json:"environment"`
	Action          string `json:"action"`
	PurchasePeriods uint64 `json:"purchase_periods,omitempty"`
}

// The authenticated token binds the quote to its owner and entitlement version.
// No client-supplied amount or date is trusted during checkout.
type saleQuote struct {
	UserID            string     `json:"user_id"`
	EntitlementID     string     `json:"entitlement_id"`
	Input             QuoteInput `json:"input"`
	ExpectedVersion   uint64     `json:"expected_version"`
	BaseCoverageID    string     `json:"base_coverage_id"`
	Currency          string     `json:"currency"`
	AmountMinor       int64      `json:"amount_minor"`
	From              time.Time  `json:"valid_from"`
	Until             *time.Time `json:"valid_until"`
	Anchor            time.Time  `json:"anchor"`
	AddedPeriods      uint64     `json:"added_periods"`
	Periods           uint64     `json:"periods"`
	UpgradePriceMinor *int64     `json:"upgrade_price_minor,omitempty"`
	ExpiresAt         time.Time  `json:"expires_at"`
}
type QuoteView struct {
	CurrentPlan     PlanView   `json:"current_plan"`
	Token           string     `json:"quote_token"`
	Action          string     `json:"action"`
	Currency        string     `json:"currency"`
	AmountMinor     int64      `json:"amount_minor"`
	PurchasePeriods uint64     `json:"purchase_periods"`
	ValidFrom       time.Time  `json:"valid_from"`
	ValidUntil      *time.Time `json:"valid_until"`
	ExpiresAt       time.Time  `json:"expires_at"`
	Plan            PlanView   `json:"plan"`
}

func sameTerm(a, b storage.PlanVersion) bool {
	if a.PurchaseType != b.PurchaseType || a.Currency != b.Currency {
		return false
	}
	if a.PurchaseType == "perpetual" {
		return true
	}
	return a.TermUnit != nil && b.TermUnit != nil && a.TermCount != nil && b.TermCount != nil && *a.TermUnit == *b.TermUnit && *a.TermCount == *b.TermCount
}
func (s *Service) Quote(ctx context.Context, p Principal, in QuoteInput, requestedLocale string) (QuoteView, error) {
	if in.Action != "renew" && in.Action != "upgrade" || in.Environment != "sandbox" && in.Environment != "live" {
		return QuoteView{}, ErrInvalid
	}
	if in.PurchasePeriods == 0 {
		in.PurchasePeriods = 1
	}
	if in.Action == "upgrade" && in.PurchasePeriods != 1 {
		return QuoteView{}, problem(400, "PURCHASE_PERIOD_INVALID", "升级不需要选择续费期限")
	}
	var e storage.Entitlement
	if s.db.WithContext(ctx).First(&e, "user_id = ? AND product_id = ? AND environment = ?", p.User.ID, in.ProductID, in.Environment).Error != nil {
		return QuoteView{}, ErrNotFound
	}
	if e.ManualHold || e.Status == "revoked" || e.DesiredVersion == 0 || e.AppliedVersion != e.DesiredVersion {
		return QuoteView{}, problem(409, "ENTITLEMENT_NOT_READY", "当前权益尚未同步或需商家处理")
	}
	var target storage.PlanVersion
	var plan storage.Plan
	if s.db.WithContext(ctx).First(&target, "id = ? AND product_id = ?", in.PlanVersionID, in.ProductID).Error != nil || s.db.WithContext(ctx).First(&plan, "id = ?", target.PlanID).Error != nil {
		return QuoteView{}, ErrNotFound
	}
	var publication storage.PlanPublication
	if s.db.WithContext(ctx).First(&publication, "plan_id = ? AND environment = ? AND version_id = ? AND preview_only = false", plan.ID, in.Environment, target.ID).Error != nil {
		return QuoteView{}, problem(409, "PLAN_NOT_FOR_SALE", "请选择已开放销售的套餐版本")
	}
	rows, err := s.coverageRows(s.db.WithContext(ctx), e.ID)
	if err != nil {
		return QuoteView{}, ErrStorage
	}
	at := now()
	current := selectCoverage(rows, at)
	if current == nil {
		current = latestBase(rows)
		if current != nil && current.coverage.ValidUntil != nil {
			if last := selectCoverage(rows, current.coverage.ValidUntil.Add(-time.Microsecond)); last != nil {
				current = last
			}
		}
	}
	if current == nil {
		return QuoteView{}, problem(409, "NO_PAID_COVERAGE", "没有可续费或升级的已付权益")
	}
	var old storage.PlanVersion
	if s.db.WithContext(ctx).First(&old, "id = ?", current.order.PlanVersionID).Error != nil {
		return QuoteView{}, ErrStorage
	}
	var oldPlan storage.Plan
	if s.db.WithContext(ctx).First(&oldPlan, "id = ?", old.PlanID).Error != nil {
		return QuoteView{}, ErrStorage
	}
	if !sameTerm(old, target) {
		return QuoteView{}, problem(409, "INCOMPATIBLE_PLAN", "仅支持相同币种、购买类型及期限的续费或升级")
	}
	mapping, _, mappingErr := s.saleMapping(ctx, s.db.WithContext(ctx), target, in.Environment, true)
	if mappingErr != nil {
		return QuoteView{}, mappingErr
	}
	q := saleQuote{UserID: p.User.ID, EntitlementID: e.ID, Input: in, ExpectedVersion: e.DesiredVersion, Currency: target.Currency, AmountMinor: target.AmountMinor, From: at, Anchor: at, AddedPeriods: 1, Periods: 1, ExpiresAt: at.Add(15 * time.Minute)}
	if in.Action == "renew" {
		if !plan.AllowRenewal || old.PlanID != target.PlanID || target.PurchaseType != "fixed_term" || current.mapping.ConnectionID != mapping.ConnectionID || current.mapping.RemoteProductID != mapping.RemoteProductID || current.mapping.RemotePolicyID != mapping.RemotePolicyID || current.mapping.RemotePolicyVersion != mapping.RemotePolicyVersion {
			return QuoteView{}, problem(409, "RENEWAL_UNAVAILABLE", "该套餐暂不能按原权益续费，请联系商家或选择升级")
		}
		tail := latestBase(rows)
		if tail == nil || tail.coverage.ValidUntil == nil {
			return QuoteView{}, ErrInvalid
		}
		amount, err := purchaseAmount(target, in.PurchasePeriods)
		if err != nil {
			return QuoteView{}, err
		}
		q.AmountMinor = amount
		q.AddedPeriods = in.PurchasePeriods
		q.Periods = in.PurchasePeriods
		q.BaseCoverageID = tail.coverage.ID
		if tail.coverage.ValidUntil.After(at) {
			q.From = *tail.coverage.ValidUntil
			q.Anchor = tail.coverage.ContractAnchor
			if tail.coverage.PaidPeriods == nil {
				return QuoteView{}, ErrStorage
			}
			q.Periods = *tail.coverage.PaidPeriods + in.PurchasePeriods
		}
		end, err := (domain.Schedule{Anchor: q.Anchor, Unit: domain.TermUnit(*target.TermUnit), Interval: int(*target.TermCount), Periods: int(q.Periods)}).End()
		if err != nil {
			return QuoteView{}, ErrInvalid
		}
		q.Until = &end
	} else {
		if plan.Status != "published" || !plan.AllowPurchase || target.AmountMinor <= old.AmountMinor {
			return QuoteView{}, problem(409, "UPGRADE_UNAVAILABLE", "目标套餐价格必须高于原套餐且允许购买")
		}
		if current.coverage.ValidUntil != nil && !current.coverage.ValidUntil.After(at) {
			return QuoteView{}, problem(409, "UPGRADE_EXPIRED", "请先续费，再申请升级")
		}
		base := current
		if current.coverage.BaseCoverageID != nil {
			base = findCoverage(rows, *current.coverage.BaseCoverageID)
		}
		if base == nil {
			return QuoteView{}, ErrStorage
		}
		q.BaseCoverageID = base.coverage.ID
		q.Until = base.coverage.ValidUntil
		q.Anchor = base.coverage.ContractAnchor
		if base.coverage.PaidPeriods != nil {
			q.Periods = *base.coverage.PaidPeriods
		}
		if target.PurchaseType == "perpetual" {
			if target.PermanentUpgradeMinor == nil {
				return QuoteView{}, problem(409, "UPGRADE_PRICE_REQUIRED", "商家尚未设置永久套餐的固定升级价")
			}
			q.AmountMinor = *target.PermanentUpgradeMinor
		} else {
			money, fullPrice, err := upgradeAmount(current, base, target, at)
			if err != nil {
				return QuoteView{}, err
			}
			q.AmountMinor = money.Minor
			q.UpgradePriceMinor = &fullPrice
		}
	}
	if !payments.SupportsAmount(domain.Money{Currency: q.Currency, Minor: q.AmountMinor}) {
		return QuoteView{}, ErrInvalid
	}
	sealed, err := s.vault.Seal(marshal(q), "sale-quote:v1")
	if err != nil {
		return QuoteView{}, ErrStorage
	}
	view := QuoteView{CurrentPlan: planView(oldPlan, old), Token: base64.RawURLEncoding.EncodeToString(sealed), Action: in.Action, Currency: q.Currency, AmountMinor: q.AmountMinor, PurchasePeriods: q.AddedPeriods, ValidFrom: q.From, ValidUntil: q.Until, ExpiresAt: q.ExpiresAt, Plan: planView(plan, target)}
	source, locale := requestedContentLocale(s.db.WithContext(ctx), requestedLocale)
	view.CurrentPlan, _ = s.localizedPlanView(ctx, view.CurrentPlan, locale, source)
	view.Plan, _ = s.localizedPlanView(ctx, view.Plan, locale, source)
	return view, nil
}

// Price the exact interval bought by its source order. PaidPeriods is cumulative
// for calendar anchoring after renewals, and is not the number sold by that order.
// An upgrade charge is already prorated; subsequent upgrades use the full target
// price saved in that quote rather than prorating the collected charge again.
func upgradeAmount(current, base *coverageRow, target storage.PlanVersion, at time.Time) (domain.Money, int64, error) {
	if base == nil || current == nil || base.coverage.ValidUntil == nil {
		return domain.Money{}, 0, ErrStorage
	}
	periods := base.order.PurchasedPeriods
	if periods == 0 {
		periods = 1
	}
	fullPrice, err := purchaseAmount(target, periods)
	if err != nil {
		return domain.Money{}, 0, err
	}
	currentPrice := base.order.AmountMinor
	if current.coverage.BaseCoverageID != nil {
		var previous saleQuote
		if json.Unmarshal(current.order.QuoteSnapshot, &previous) != nil {
			return domain.Money{}, 0, ErrStorage
		}
		if previous.UpgradePriceMinor != nil {
			currentPrice = *previous.UpgradePriceMinor
		} else {
			// Quotes issued before the full price was saved still refer to an
			// immutable plan version containing the original period discounts.
			currentPrice, err = purchaseAmount(current.version, periods)
			if err != nil {
				return domain.Money{}, 0, err
			}
		}
	}
	money, err := domain.UpgradeDifference(domain.Money{Currency: base.order.Currency, Minor: currentPrice}, domain.Money{Currency: target.Currency, Minor: fullPrice}, base.coverage.ValidFrom, *base.coverage.ValidUntil, at)
	if err != nil {
		return domain.Money{}, 0, ErrInvalid
	}
	return money, fullPrice, nil
}

func (s *Service) OwnedPlans(ctx context.Context, p Principal, productID, environment, requestedLocale string) ([]PlanView, error) {
	var e storage.Entitlement
	if s.db.WithContext(ctx).First(&e, "user_id = ? AND product_id = ? AND environment = ?", p.User.ID, productID, environment).Error != nil {
		return nil, ErrNotFound
	}
	var publications []storage.PlanPublication
	if s.db.WithContext(ctx).Where("environment = ? AND preview_only = false", environment).Find(&publications).Error != nil {
		return nil, ErrStorage
	}
	source, locale := requestedContentLocale(s.db.WithContext(ctx), requestedLocale)
	out := []PlanView{}
	for _, publication := range publications {
		var plan storage.Plan
		var version storage.PlanVersion
		if s.db.WithContext(ctx).First(&plan, "id = ? AND product_id = ?", publication.PlanID, productID).Error != nil {
			continue
		}
		if plan.Status != "published" && !plan.AllowRenewal {
			continue
		}
		if s.db.WithContext(ctx).First(&version, "id = ?", publication.VersionID).Error != nil {
			return nil, ErrStorage
		}
		view, _ := s.localizedPlanView(ctx, planView(plan, version), locale, source)
		out = append(out, view)
	}
	return out, nil
}
func (s *Service) readQuote(token string, p Principal, in CheckoutInput) (*saleQuote, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) > 8192 {
		return nil, ErrInvalid
	}
	plain, err := s.vault.Open(raw, "sale-quote:v1")
	if err != nil {
		return nil, ErrInvalid
	}
	var q saleQuote
	if json.Unmarshal(plain, &q) != nil || q.UserID != p.User.ID || q.Input.ProductID != in.ProductID || q.Input.PlanVersionID != in.PlanVersionID || q.Input.Environment != in.Environment || q.Input.Action != in.Action || q.Input.PurchasePeriods != in.PurchasePeriods {
		return nil, ErrInvalid
	}
	if !q.ExpiresAt.After(now()) {
		return nil, problem(409, "QUOTE_EXPIRED", "报价已过期，请重新报价")
	}
	return &q, nil
}

// Kept in one place so every quote and ledger calculation uses paid snapshots.
type coverageRow struct {
	coverage storage.EntitlementCoverage
	order    storage.Order
	version  storage.PlanVersion
	mapping  storage.PlanLicenseMapping
}

func (s *Service) coverageRows(tx *gorm.DB, id string) ([]coverageRow, error) {
	var rows []storage.EntitlementCoverage
	if err := tx.Where("entitlement_id = ?", id).Order("created_at,id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]coverageRow, 0, len(rows))
	for _, c := range rows {
		r := coverageRow{coverage: c}
		if err := tx.First(&r.order, "id = ?", c.SourceOrderID).Error; err != nil {
			return nil, err
		}
		if err := tx.First(&r.version, "id = ?", r.order.PlanVersionID).Error; err != nil {
			return nil, err
		}
		if route := externalRoute(c.Snapshot); route != nil {
			r.mapping = storage.PlanLicenseMapping{PlanVersionID: r.version.ID, ConnectionID: route.ConnectionID, RemoteProductID: route.ProductRef, RemotePolicyID: route.PlanRef, RemotePolicyVersion: 1}
		} else if err := tx.First(&r.mapping, "plan_version_id = ?", r.version.ID).Error; err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}
func findCoverage(rows []coverageRow, id string) *coverageRow {
	for i := range rows {
		if rows[i].coverage.ID == id {
			return &rows[i]
		}
	}
	return nil
}
func latestBase(rows []coverageRow) *coverageRow {
	var best *coverageRow
	for i := range rows {
		c := &rows[i]
		if c.coverage.VoidedAt != nil || c.coverage.BaseCoverageID != nil {
			continue
		}
		if best == nil || c.coverage.ValidFrom.After(best.coverage.ValidFrom) {
			best = c
		}
	}
	return best
}
func selectCoverage(rows []coverageRow, at time.Time) *coverageRow {
	var base *coverageRow
	for i := range rows {
		c := &rows[i]
		if c.coverage.VoidedAt != nil || c.coverage.BaseCoverageID != nil || c.coverage.ValidFrom.After(at) || c.coverage.ValidUntil != nil && !c.coverage.ValidUntil.After(at) {
			continue
		}
		if base == nil || c.coverage.ValidFrom.After(base.coverage.ValidFrom) {
			base = c
		}
	}
	if base == nil {
		return nil
	}
	best := base
	for i := range rows {
		c := &rows[i]
		if c.coverage.VoidedAt == nil && c.coverage.BaseCoverageID != nil && *c.coverage.BaseCoverageID == base.coverage.ID && !c.coverage.ValidFrom.After(at) && (c.coverage.ValidUntil == nil || c.coverage.ValidUntil.After(at)) && (best == base || c.coverage.CreatedAt.After(best.coverage.CreatedAt) || c.coverage.CreatedAt.Equal(best.coverage.CreatedAt) && c.coverage.ID > best.coverage.ID) {
			best = c
		}
	}
	return best
}
