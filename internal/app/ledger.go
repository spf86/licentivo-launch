package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/domain"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/storage"
)

func (s *Service) applyPaidCoverage(tx *gorm.DB, order storage.Order, paidAt time.Time) error {
	var e storage.Entitlement
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&e, "user_id = ? AND product_id = ? AND environment = ?", order.UserID, order.ProductID, order.Environment).Error; err != nil {
		return err
	}
	// Money is already recorded. An obsolete change must not overwrite later rights.
	review := func() error { return tx.Model(&order).Update("fulfillment_review", true).Error }
	if e.ManualHold || e.Status == "suspended" {
		return review()
	}
	var q saleQuote
	if order.Action != "purchase" {
		if json.Unmarshal(order.QuoteSnapshot, &q) != nil || q.ExpectedVersion != order.ExpectedEntitlementVersion || q.EntitlementID != e.ID {
			return review()
		}
	}
	if e.DesiredVersion != order.ExpectedEntitlementVersion {
		allowed, err := s.renewalAfterNaturalExpiry(tx, e, order, q, paidAt)
		if err != nil {
			return err
		}
		if !allowed {
			return review()
		}
	}
	if e.Status == "revoked" {
		if order.Action != "purchase" {
			return review()
		}
		ref, err := newID()
		if err != nil {
			return err
		}
		e.LaunchPurchaseRef = &ref
		if err := tx.Model(&e).Update("launch_purchase_ref", ref).Error; err != nil {
			return err
		}
	}
	var version storage.PlanVersion
	if err := tx.First(&version, "id = ?", order.PlanVersionID).Error; err != nil {
		return err
	}
	id, err := newID()
	if err != nil {
		return err
	}
	one := uint64(1)
	paidPeriods := order.PurchasedPeriods
	if paidPeriods == 0 {
		paidPeriods = one
	}
	c := storage.EntitlementCoverage{ID: id, EntitlementID: e.ID, SourceOrderID: order.ID, ValidFrom: paidAt, ContractAnchor: paidAt, TermUnit: version.TermUnit, TermCount: version.TermCount, PaidPeriods: &paidPeriods, Snapshot: order.EntitlementSnapshot, CreatedAt: now()}
	if order.Action != "purchase" {
		var base storage.EntitlementCoverage
		if tx.First(&base, "id = ? AND entitlement_id = ? AND voided_at IS NULL", q.BaseCoverageID, e.ID).Error != nil {
			return review()
		}
		c.ContractAnchor = q.Anchor
		c.PaidPeriods = &q.Periods
		if order.Action == "upgrade" {
			if q.Until != nil && !q.Until.After(paidAt) {
				return review()
			}
			c.BaseCoverageID = &base.ID
			c.ValidUntil = q.Until
		} else {
			c.ValidFrom = q.From
			// A renewal paid after the old period ended starts when money actually arrived.
			if paidAt.After(c.ValidFrom) {
				c.ValidFrom = paidAt
				c.ContractAnchor = paidAt
				periods := q.AddedPeriods
				if periods == 0 {
					periods = one
				}
				c.PaidPeriods = &periods
			}
		}
	}
	if order.Action != "upgrade" && version.PurchaseType == "fixed_term" {
		if version.TermUnit == nil || version.TermCount == nil || c.PaidPeriods == nil {
			return ErrStorage
		}
		until, err := (domain.Schedule{Anchor: c.ContractAnchor, Unit: domain.TermUnit(*version.TermUnit), Interval: int(*version.TermCount), Periods: int(*c.PaidPeriods)}).End()
		if err != nil {
			return err
		}
		c.ValidUntil = &until
	}
	if err := tx.Create(&c).Error; err != nil {
		return err
	}
	if err := tx.Model(&order).Update("fulfillment_review", false).Error; err != nil {
		return err
	}
	return s.recomputeEntitlement(tx, &e, &order.ID, true)
}

// Expiry transports a new version without changing paid coverage. Only accept a
// stale renewal if the complete task history and unchanged ledger prove that is
// the sole intervening change. Refunds, holds, migrations and plan changes fail
// closed; the verified payment time, not the callback/recovery time, sets dates.
func (s *Service) renewalAfterNaturalExpiry(tx *gorm.DB, e storage.Entitlement, order storage.Order, q saleQuote, paidAt time.Time) (bool, error) {
	if order.Action != "renew" || e.ManualHold || e.Status == "revoked" || e.LaunchPurchaseRef == nil || q.ExpectedVersion == 0 || q.ExpectedVersion >= e.DesiredVersion || paidAt.After(q.ExpiresAt) {
		return false, nil
	}
	if q.UserID != order.UserID || q.Input.Action != order.Action || q.Input.ProductID != order.ProductID || q.Input.PlanVersionID != order.PlanVersionID || q.Input.Environment != order.Environment || q.Currency != order.Currency || q.AmountMinor != order.AmountMinor || q.AddedPeriods != order.PurchasedPeriods || order.QuoteExpiresAt == nil || !q.ExpiresAt.Equal(*order.QuoteExpiresAt) {
		return false, nil
	}
	rows, err := s.coverageRows(tx, e.ID)
	if err != nil {
		return false, err
	}
	tail := latestBase(rows)
	if tail == nil || tail.coverage.ID != q.BaseCoverageID || tail.coverage.ValidUntil == nil || tail.coverage.ValidUntil.After(now()) {
		return false, nil
	}
	if tail.coverage.PaidPeriods == nil || q.AddedPeriods == 0 || q.Periods != *tail.coverage.PaidPeriods+q.AddedPeriods || !q.From.Equal(*tail.coverage.ValidUntil) || !q.Anchor.Equal(tail.coverage.ContractAnchor) {
		return false, nil
	}
	var tasks []storage.SyncTask
	if err := tx.Where("entitlement_id = ? AND target_version >= ? AND target_version <= ?", e.ID, q.ExpectedVersion, e.DesiredVersion).Order("target_version").Find(&tasks).Error; err != nil {
		return false, err
	}
	if uint64(len(tasks)) != e.DesiredVersion-q.ExpectedVersion+1 {
		return false, nil
	}
	var original ports.CommerceSnapshot
	for i, task := range tasks {
		var snapshot ports.CommerceSnapshot
		if json.Unmarshal(task.RequestJSON, &snapshot) != nil || snapshot.Validate() != nil || !bytes.Equal(task.RequestHash, securityDigest(marshal(snapshot))) || snapshot.Version != q.ExpectedVersion+uint64(i) || snapshot.EventID != task.OperationID || snapshot.EntitlementRef != purchaseReference(e) || snapshot.AccountRef != s.subject(e.UserID) || snapshot.LaunchProductID != e.ProductID || snapshot.Environment != e.Environment || !sameTime(snapshot.ValidUntil, tail.coverage.ValidUntil) {
			return false, nil
		}
		if i == 0 {
			if snapshot.Status() != "active" || snapshot.Type == "entitlement.migrated" {
				return false, nil
			}
			original = snapshot
		} else if task.SourceOrderID != nil || snapshot.Type != "entitlement.expired" || task.ExpectedVersion != snapshot.Version-1 || snapshot.PlanRef != tail.version.ID || !snapshot.ValidFrom.Equal(tail.coverage.ValidFrom) {
			return false, nil
		}
	}
	lastActive := selectCoverage(rows, original.ValidUntil.Add(-time.Microsecond))
	if lastActive == nil || lastActive.version.ID != original.PlanRef {
		return false, nil
	}
	for _, row := range rows {
		if row.coverage.CreatedAt.After(order.CreatedAt) || row.coverage.VoidedAt != nil && row.coverage.VoidedAt.After(tasks[0].CreatedAt) {
			return false, nil
		}
	}
	return true, nil
}

func sameCoveragePolicy(a, b *coverageRow) bool {
	return a != nil && b != nil && a.mapping.ConnectionID == b.mapping.ConnectionID && a.mapping.RemoteProductID == b.mapping.RemoteProductID && a.mapping.RemotePolicyID == b.mapping.RemotePolicyID && a.mapping.RemotePolicyVersion == b.mapping.RemotePolicyVersion
}

// Rebuild the absolute purchased period from immutable, non-voided order coverage.
// Upgrade overlays depend on their base interval; refunding the overlay reveals
// the base, while refunding that base also makes its overlays inapplicable.
func (s *Service) recomputeEntitlement(tx *gorm.DB, e *storage.Entitlement, source *string, force bool) error {
	return s.recomputePurchaseAt(tx, e, source, force, now())
}

func (s *Service) recomputePurchaseAt(tx *gorm.DB, e *storage.Entitlement, source *string, force bool, at time.Time) error {
	rows, err := s.coverageRows(tx, e.ID)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	chosen := selectCoverage(rows, at)
	desired := "active"
	var next *time.Time
	var currentID *string
	if chosen == nil {
		for i := range rows {
			r := &rows[i]
			if r.coverage.VoidedAt == nil && r.coverage.BaseCoverageID == nil && r.coverage.ValidFrom.After(at) && (chosen == nil || r.coverage.ValidFrom.Before(chosen.coverage.ValidFrom)) {
				chosen = r
			}
		}
		if chosen != nil {
			v := chosen.coverage.ValidFrom
			next = &v
		} else {
			desired = "expired"
			chosen = latestBase(rows)
			if chosen == nil {
				desired = "revoked"
				chosen = &rows[len(rows)-1]
			}
		}
	}
	from, until := chosen.coverage.ValidFrom, chosen.coverage.ValidUntil
	if chosen.coverage.VoidedAt == nil {
		id := chosen.coverage.ID
		currentID = &id
	}
	if desired == "active" && !from.After(at) {
		var bounds []time.Time
		for _, r := range rows {
			if r.coverage.VoidedAt != nil {
				continue
			}
			if r.coverage.ValidFrom.After(at) {
				bounds = append(bounds, r.coverage.ValidFrom)
			}
			if r.coverage.ValidUntil != nil && r.coverage.ValidUntil.After(at) {
				bounds = append(bounds, *r.coverage.ValidUntil)
			}
		}
		sort.Slice(bounds, func(i, j int) bool { return bounds[i].Before(bounds[j]) })
		for _, b := range bounds {
			after := selectCoverage(rows, b)
			if !sameCoveragePolicy(chosen, after) {
				v := b
				until = &v
				next = &v
				break
			}
			until = after.coverage.ValidUntil
		}
	}
	if e.ManualHold && desired != "revoked" {
		desired = "suspended"
	}
	var prior storage.SyncTask
	var previous ports.CommerceSnapshot
	if err := tx.Where("entitlement_id = ?", e.ID).Order("target_version DESC").First(&prior).Error; err == nil {
		if json.Unmarshal(prior.RequestJSON, &previous) != nil {
			return ErrStorage
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if !force && previous.EventID != "" && previous.Status() == desired && previous.ValidFrom.Equal(from) && sameTime(previous.ValidUntil, until) && previous.PlanRef == chosen.version.ID && previous.EntitlementRef == purchaseReference(*e) {
		return tx.Model(e).Updates(map[string]any{"next_transition_at": next, "current_coverage_id": currentID}).Error
	}
	operation, err := newID()
	if err != nil {
		return err
	}
	var trust storage.LaunchTrustConnection
	if err := tx.First(&trust, "environment = ?", e.Environment).Error; err != nil || trust.Issuer != s.PublicURL() {
		return bridgePending()
	}
	c := ports.CommerceSnapshot{EventID: operation, AccountRef: s.subject(e.UserID), Environment: e.Environment, LaunchProductID: e.ProductID, EntitlementRef: purchaseReference(*e), PlanRef: chosen.version.ID, Version: e.DesiredVersion + 1, ValidFrom: from, ValidUntil: until}
	switch desired {
	case "revoked":
		c.Type = "entitlement.refunded"
	case "expired":
		c.Type = "entitlement.expired"
	case "suspended":
		c.Type = "entitlement.suspended"
	default:
		c.Type = "entitlement.resumed"
		if e.DesiredVersion == 0 || previous.Type == "entitlement.refunded" {
			c.Type = "entitlement.created"
		}
		if source != nil {
			var order storage.Order
			if err := tx.First(&order, "id = ?", *source).Error; err != nil {
				return err
			}
			if order.Status == "paid" || order.PaidAt != nil {
				switch order.Action {
				case "renew":
					c.Type = "entitlement.renewed"
				case "upgrade":
					c.Type = "entitlement.upgraded"
				}
			}
		}
	}
	if e.LaunchPurchaseRef == nil && e.AppliedVersion > 0 {
		var binding storage.LicenseBinding
		if err := tx.First(&binding, "entitlement_id = ? AND replaced_at IS NULL", e.ID).Error; err != nil {
			return problem(409, "LEGACY_PURCHASE_REVIEW_REQUIRED", "旧购买记录需要核对后迁移")
		}
		if binding.ConnectionID != chosen.mapping.ConnectionID {
			return problem(409, "LICENSE_CONNECTION_CONFLICT", "旧购买记录的授权连接不一致")
		}
		c.Type, c.LegacyLicenseID = "entitlement.migrated", binding.RemoteLicenseID
	}
	if err := c.Validate(); err != nil {
		return ErrStorage
	}
	if err := tx.Create(&storage.SyncTask{OperationID: operation, EntitlementID: e.ID, SourceOrderID: source, ConnectionID: &chosen.mapping.ConnectionID, LaunchConnectionID: &trust.RemoteID, ExpectedVersion: e.DesiredVersion, TargetVersion: c.Version, RequestJSON: marshal(c), RequestHash: securityDigest(marshal(c)), Status: "pending"}).Error; err != nil {
		return err
	}
	e.LaunchPurchaseRef = &c.EntitlementRef
	return tx.Model(e).Updates(map[string]any{"launch_purchase_ref": c.EntitlementRef, "desired_version": c.Version, "status": "pending", "valid_from": from, "valid_until": until, "snapshot": chosen.coverage.Snapshot, "current_coverage_id": currentID, "next_transition_at": next}).Error
}

func (s *Service) ProcessCoverageTransitions(ctx context.Context) error {
	var rows []storage.Entitlement
	if err := s.db.WithContext(ctx).Where("next_transition_at <= ? AND status != 'revoked'", now()).Limit(20).Find(&rows).Error; err != nil {
		return ErrStorage
	}
	for _, e := range rows {
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var user storage.User
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", e.UserID).Error; err != nil {
				return err
			}
			var current storage.Entitlement
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", e.ID).Error; err != nil {
				return err
			}
			if current.NextTransitionAt == nil || current.NextTransitionAt.After(now()) {
				return nil
			}
			return s.recomputeEntitlement(tx, &current, nil, false)
		})
		if err != nil {
			return databaseError(err)
		}
	}
	return nil
}
