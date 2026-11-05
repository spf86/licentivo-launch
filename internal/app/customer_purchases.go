package app

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"licentivo-launch/internal/storage"
)

type CustomerPurchaseSummary struct {
	ProductCount int                       `json:"product_count"`
	Products     []CustomerPurchaseProduct `json:"products"`
	LastPaidAt   *time.Time                `json:"last_paid_at"`
	NextExpiry   *time.Time                `json:"next_expiry"`
	ExpiryState  string                    `json:"expiry_state"`
}
type CustomerPurchaseProduct struct {
	ReminderMode       string     `json:"reminder_mode"`
	ReminderLocale     string     `json:"reminder_locale"`
	ID                 string     `json:"id"`
	ProductID          string     `json:"product_id"`
	ProductName        string     `json:"product_name"`
	PlanName           string     `json:"plan_name"`
	PlanVersion        uint64     `json:"plan_version"`
	SourceOrderID      string     `json:"source_order_id,omitempty"`
	PaidAt             *time.Time `json:"paid_at"`
	ValidFrom          *time.Time `json:"valid_from"`
	ValidUntil         *time.Time `json:"valid_until"`
	PurchasedUntil     *time.Time `json:"purchased_until"`
	Permanent          bool       `json:"permanent"`
	PurchasedPermanent bool       `json:"purchased_permanent"`
	DatesKnown         bool       `json:"dates_known"`
	Status             string     `json:"status"`
	Synchronized       bool       `json:"synchronized"`
}
type CustomerProductPage struct {
	Items  []CustomerPurchaseProduct `json:"items"`
	Total  int                       `json:"total"`
	Limit  int                       `json:"limit"`
	Offset int                       `json:"offset"`
}
type CustomerPurchaseOrder struct {
	OrderView
	ValidFrom        *time.Time `json:"valid_from"`
	ValidUntil       *time.Time `json:"valid_until"`
	Permanent        bool       `json:"permanent"`
	Voided           bool       `json:"voided"`
	CoverageRecorded bool       `json:"coverage_recorded"`
}
type CustomerPurchaseOrderPage struct {
	Items  []CustomerPurchaseOrder `json:"items"`
	Total  int64                   `json:"total"`
	Limit  int                     `json:"limit"`
	Offset int                     `json:"offset"`
}

func customerEnvironment(environment string) (string, error) {
	if environment == "" {
		return "live", nil
	}
	if environment != "live" && environment != "sandbox" {
		return "", ErrInvalid
	}
	return environment, nil
}
func (s *Service) managedCustomer(ctx context.Context, p Principal, id string) error {
	if p.Role != "admin" && p.Role != "operations" && p.Role != "support" {
		return ErrForbidden
	}
	var count int64
	err := s.db.WithContext(ctx).Model(&storage.User{}).Where("id=? AND NOT EXISTS (SELECT 1 FROM staff_accounts sa WHERE sa.user_id=users.id)", id).Count(&count).Error
	if err != nil {
		return ErrStorage
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}
func historicalPlanName(snapshot json.RawMessage, fallback string) string {
	var v struct {
		PlanName string `json:"plan_name"`
	}
	if json.Unmarshal(snapshot, &v) == nil && v.PlanName != "" {
		return v.PlanName
	}
	return fallback
}

// Read a page's customers in batches, never fetch licensing secrets or issue
// remote authorizations. Coverage selection uses the same ledger rules as sales.
func (s *Service) customerPurchaseProducts(ctx context.Context, ids []string, environment string) (map[string][]CustomerPurchaseProduct, map[string]*time.Time, error) {
	products := map[string][]CustomerPurchaseProduct{}
	lastPaid := map[string]*time.Time{}
	if len(ids) == 0 {
		return products, lastPaid, nil
	}
	db := s.db.WithContext(ctx)
	var entitlements []storage.Entitlement
	if err := db.Select("id,user_id,product_id,status,manual_hold,valid_from,valid_until,desired_version,applied_version,launch_purchase_ref").Where("user_id IN ? AND environment=?", ids, environment).Find(&entitlements).Error; err != nil {
		return nil, nil, ErrStorage
	}
	reminderSettings, err := readExpiryReminderSettings(db)
	if err != nil {
		return nil, nil, databaseError(err)
	}
	reminderIDs := make([]string, 0, len(entitlements))
	for _, e := range entitlements {
		reminderIDs = append(reminderIDs, e.ID)
	}
	var reminderRows []storage.ExpiryReminderPreference
	if len(reminderIDs) > 0 {
		if err := db.Where("entitlement_id IN ?", reminderIDs).Find(&reminderRows).Error; err != nil {
			return nil, nil, ErrStorage
		}
	}
	reminderPreferences := map[string]storage.ExpiryReminderPreference{}
	for _, pref := range reminderRows {
		reminderPreferences[pref.EntitlementID] = pref
	}
	type coverageData struct {
		storage.EntitlementCoverage
		PlanVersionID string
		PlanName      string
		Version       uint64
		PurchaseType  string
		PaidAt        *time.Time
		PriceSnapshot json.RawMessage
	}
	var covers []coverageData
	err = db.Table("entitlement_coverages AS c").Select("c.id,c.entitlement_id,c.source_order_id,c.base_coverage_id,c.valid_from,c.valid_until,c.voided_at,c.created_at,o.plan_version_id,o.paid_at,o.price_snapshot,pl.name AS plan_name,pv.version,pv.purchase_type").Joins("JOIN entitlements e ON e.id=c.entitlement_id").Joins("JOIN orders o ON o.id=c.source_order_id").Joins("JOIN plan_versions pv ON pv.id=o.plan_version_id").Joins("JOIN plans pl ON pl.id=pv.plan_id").Where("e.user_id IN ? AND e.environment=?", ids, environment).Order("c.created_at,c.id").Scan(&covers).Error
	if err != nil {
		return nil, nil, ErrStorage
	}
	byEnt := map[string][]coverageRow{}
	details := map[string]coverageData{}
	for _, c := range covers {
		byEnt[c.EntitlementID] = append(byEnt[c.EntitlementID], coverageRow{coverage: c.EntitlementCoverage, version: storage.PlanVersion{ID: c.PlanVersionID, PurchaseType: c.PurchaseType}})
		details[c.ID] = c
	}
	type latestOrder struct {
		storage.Order
		PlanName     string
		Version      uint64
		PurchaseType string
	}
	var paid []latestOrder
	err = db.Table("orders o").Select("o.id,o.user_id,o.product_id,o.paid_at,o.price_snapshot,o.plan_version_id,pl.name AS plan_name,pv.version,pv.purchase_type").Joins("JOIN plan_versions pv ON pv.id=o.plan_version_id").Joins("JOIN plans pl ON pl.id=pv.plan_id").Where("o.user_id IN ? AND o.environment=? AND o.paid_at IS NOT NULL", ids, environment).Where("NOT EXISTS (SELECT 1 FROM orders newer WHERE newer.user_id=o.user_id AND newer.product_id=o.product_id AND newer.environment=o.environment AND newer.paid_at IS NOT NULL AND (newer.paid_at>o.paid_at OR (newer.paid_at=o.paid_at AND newer.id>o.id)))").Scan(&paid).Error
	if err != nil {
		return nil, nil, ErrStorage
	}
	latest := map[string]latestOrder{}
	for _, o := range paid {
		latest[o.UserID+"/"+o.ProductID] = o
		if lastPaid[o.UserID] == nil || lastPaid[o.UserID].Before(*o.PaidAt) {
			lastPaid[o.UserID] = o.PaidAt
		}
	}
	var names []struct{ ID, Name string }
	productIDs := make([]string, 0, len(entitlements))
	for _, e := range entitlements {
		productIDs = append(productIDs, e.ProductID)
	}
	if len(productIDs) > 0 {
		if db.Model(&storage.Product{}).Select("id,name").Where("id IN ?", productIDs).Scan(&names).Error != nil {
			return nil, nil, ErrStorage
		}
	}
	nameMap := map[string]string{}
	for _, p := range names {
		nameMap[p.ID] = p.Name
	}
	at := now()
	for _, e := range entitlements {
		rows := byEnt[e.ID]
		o, hasPaid := latest[e.UserID+"/"+e.ProductID]
		if len(rows) == 0 && e.DesiredVersion == 0 && !hasPaid {
			continue
		}
		v := CustomerPurchaseProduct{ID: e.ID, ProductID: e.ProductID, ProductName: nameMap[e.ProductID], Status: "pending", Synchronized: e.LaunchPurchaseRef != nil && e.AppliedVersion > 0 && e.AppliedVersion == e.DesiredVersion}
		v.ReminderMode, v.ReminderLocale = "inherit", reminderSettings.Locale
		if pref, ok := reminderPreferences[e.ID]; ok {
			v.ReminderMode = pref.Mode
			if pref.Locale != "" {
				v.ReminderLocale = pref.Locale
			}
		}
		chosen := selectCoverage(rows, at)
		if chosen == nil {
			for i := range rows {
				r := &rows[i]
				if r.coverage.VoidedAt == nil && r.coverage.BaseCoverageID == nil && r.coverage.ValidFrom.After(at) && (chosen == nil || r.coverage.ValidFrom.Before(chosen.coverage.ValidFrom)) {
					chosen = r
				}
			}
		}
		if chosen == nil {
			chosen = latestBase(rows)
		}
		if chosen == nil && len(rows) > 0 {
			chosen = &rows[len(rows)-1]
		}
		if chosen != nil {
			c := chosen.coverage
			d := details[c.ID]
			v.PlanName = historicalPlanName(d.PriceSnapshot, d.PlanName)
			v.PlanVersion = d.Version
			v.PaidAt = d.PaidAt
			v.SourceOrderID = c.SourceOrderID
			v.ValidFrom = &c.ValidFrom
			v.ValidUntil = c.ValidUntil
			v.Permanent = d.PurchaseType == "perpetual" && c.ValidUntil == nil
			v.DatesKnown = c.ValidUntil != nil || v.Permanent
			v.Status = "active"
			if c.VoidedAt != nil {
				v.Status = "revoked"
			} else if c.ValidFrom.After(at) {
				v.Status = "scheduled"
			} else if c.ValidUntil != nil && !c.ValidUntil.After(at) {
				v.Status = "expired"
			}
			// Extend only through contiguous non-voided purchased base intervals. Upgrade
			// overlays never manufacture an extra purchased term or bridge a gap.
			base := chosen
			if c.BaseCoverageID != nil {
				base = findCoverage(rows, *c.BaseCoverageID)
			}
			if base != nil && base.coverage.VoidedAt == nil {
				end := base.coverage.ValidUntil
				ordered := append([]coverageRow(nil), rows...)
				sort.Slice(ordered, func(i, j int) bool { return ordered[i].coverage.ValidFrom.Before(ordered[j].coverage.ValidFrom) })
				for _, r := range ordered {
					b := r.coverage
					if end == nil || b.VoidedAt != nil || b.BaseCoverageID != nil || b.ValidFrom.After(*end) || b.ValidUntil != nil && !b.ValidUntil.After(*end) {
						continue
					}
					end = b.ValidUntil
				}
				v.PurchasedUntil = end
				v.PurchasedPermanent = end == nil && base.version.PurchaseType == "perpetual"
				if end == nil {
					for _, r := range ordered {
						if r.coverage.BaseCoverageID == nil && r.coverage.VoidedAt == nil && r.coverage.ValidUntil == nil && r.version.PurchaseType == "perpetual" {
							v.PurchasedPermanent = true
						}
					}
				}
			}
		} else {
			v.ValidFrom = e.ValidFrom
			v.ValidUntil = e.ValidUntil
			v.PurchasedUntil = e.ValidUntil
			v.DatesKnown = e.ValidFrom != nil && e.ValidUntil != nil
			if hasPaid {
				v.PlanName = historicalPlanName(o.PriceSnapshot, o.PlanName)
				v.PlanVersion = o.Version
				v.PaidAt = o.PaidAt
				v.SourceOrderID = o.ID
			} else {
				v.PlanName = "非订单授予"
			}
			if v.DatesKnown {
				if e.ValidFrom.After(at) {
					v.Status = "scheduled"
				} else if !e.ValidUntil.After(at) {
					v.Status = "expired"
				} else {
					v.Status = "active"
				}
			}
		}
		if e.Status == "revoked" {
			v.Status = "revoked"
		} else if e.ManualHold || e.Status == "suspended" {
			v.Status = "suspended"
		}
		products[e.UserID] = append(products[e.UserID], v)
	}
	for id := range products {
		sort.Slice(products[id], func(i, j int) bool {
			a, b := products[id][i], products[id][j]
			if a.ProductName == b.ProductName {
				return a.ID < b.ID
			}
			return a.ProductName < b.ProductName
		})
	}
	return products, lastPaid, nil
}
func summarizeCustomerProducts(products []CustomerPurchaseProduct, last *time.Time, at time.Time) *CustomerPurchaseSummary {
	v := &CustomerPurchaseSummary{ProductCount: len(products), Products: []CustomerPurchaseProduct{}, LastPaidAt: last, ExpiryState: "not_purchased"}
	for _, p := range products {
		if len(v.Products) < 2 {
			v.Products = append(v.Products, p)
		}
		if p.Status == "active" || p.Status == "suspended" {
			end := p.PurchasedUntil
			if end == nil {
				end = p.ValidUntil
			}
			if end != nil && end.After(at) && (v.NextExpiry == nil || end.Before(*v.NextExpiry)) {
				v.NextExpiry = end
			}
			if p.PurchasedPermanent {
				v.ExpiryState = "permanent"
			}
		}
	}
	if v.NextExpiry != nil {
		v.ExpiryState = "dated"
	} else if v.ExpiryState != "permanent" && len(products) > 0 {
		v.ExpiryState = "inactive"
	}
	return v
}
func (s *Service) CustomerProducts(ctx context.Context, p Principal, id, environment string, limit, offset int) (CustomerProductPage, error) {
	out := CustomerProductPage{Items: []CustomerPurchaseProduct{}}
	if err := s.managedCustomer(ctx, p, id); err != nil {
		return out, err
	}
	env, err := customerEnvironment(environment)
	if err != nil {
		return out, err
	}
	limit, err = listBounds(limit, offset)
	if err != nil {
		return out, err
	}
	values, _, err := s.customerPurchaseProducts(ctx, []string{id}, env)
	if err != nil {
		return out, err
	}
	all := values[id]
	out.Total = len(all)
	out.Limit = limit
	out.Offset = offset
	if offset < len(all) {
		end := offset + limit
		if end > len(all) {
			end = len(all)
		}
		out.Items = all[offset:end]
	}
	return out, nil
}
func (s *Service) CustomerOrders(ctx context.Context, p Principal, id string, in AdminOrderQuery) (CustomerPurchaseOrderPage, error) {
	out := CustomerPurchaseOrderPage{Items: []CustomerPurchaseOrder{}}
	if err := s.managedCustomer(ctx, p, id); err != nil {
		return out, err
	}
	env, err := customerEnvironment(in.Environment)
	if err != nil {
		return out, err
	}
	in.Environment = env
	page, err := s.searchCustomerOrders(ctx, id, in)
	if err != nil {
		return out, err
	}
	out.Total = page.Total
	out.Limit = page.Limit
	out.Offset = page.Offset
	ids := []string{}
	for _, row := range page.Items {
		ids = append(ids, row.ID)
	}
	if len(ids) == 0 {
		return out, nil
	}
	type orderCoverage struct {
		storage.EntitlementCoverage
		ParentID       *string
		ParentVoidedAt *time.Time
	}
	var covers []orderCoverage
	if s.db.WithContext(ctx).Table("entitlement_coverages c").Select("c.source_order_id,c.valid_from,c.valid_until,c.voided_at,c.base_coverage_id,b.id AS parent_id,b.voided_at AS parent_voided_at").Joins("LEFT JOIN entitlement_coverages b ON b.id=c.base_coverage_id").Where("c.source_order_id IN ?", ids).Scan(&covers).Error != nil {
		return out, ErrStorage
	}
	cm := map[string]orderCoverage{}
	for _, c := range covers {
		cm[c.SourceOrderID] = c
	}
	var snapshots []struct {
		ID            string
		PriceSnapshot json.RawMessage
		PurchaseType  string
	}
	if s.db.WithContext(ctx).Table("orders o").Select("o.id,o.price_snapshot,pv.purchase_type").Joins("JOIN plan_versions pv ON pv.id=o.plan_version_id").Where("o.id IN ?", ids).Scan(&snapshots).Error != nil {
		return out, ErrStorage
	}
	for _, row := range page.Items {
		v := CustomerPurchaseOrder{OrderView: row}
		for _, snapshot := range snapshots {
			if snapshot.ID == row.ID {
				v.PlanName = historicalPlanName(snapshot.PriceSnapshot, row.PlanName)
				if c, ok := cm[row.ID]; ok {
					v.ValidFrom = &c.ValidFrom
					v.ValidUntil = c.ValidUntil
					v.Voided = c.VoidedAt != nil || c.BaseCoverageID != nil && (c.ParentID == nil || c.ParentVoidedAt != nil)
					v.Permanent = snapshot.PurchaseType == "perpetual" && c.ValidUntil == nil
					v.CoverageRecorded = true
				}
				break
			}
		}
		out.Items = append(out.Items, v)
	}
	return out, nil
}
