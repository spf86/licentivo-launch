package app

import (
	"bytes"
	"context"
	"encoding/hex"
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

func (s *Service) applyPaidCoverage(tx *gorm.DB, user storage.User, order storage.Order, paidAt time.Time) error {
	var e storage.Entitlement
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&e, "user_id = ? AND product_id = ? AND environment = ?", order.UserID, order.ProductID, order.Environment).Error; err != nil {
		return err
	}
	// Money is already recorded. An obsolete change must not overwrite later rights.
	review := func() error { return tx.Model(&order).Update("fulfillment_review", true).Error }
	if e.Status == "revoked" || e.DesiredVersion != order.ExpectedEntitlementVersion {
		return review()
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
		var q saleQuote
		if json.Unmarshal(order.QuoteSnapshot, &q) != nil || q.ExpectedVersion != e.DesiredVersion || q.EntitlementID != e.ID {
			return review()
		}
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
	if user.Status != "active" {
		e.ManualHold = true
		if err := tx.Model(&e).Updates(map[string]any{"manual_hold": true, "hold_reason": "账号停用"}).Error; err != nil {
			return err
		}
	}
	return s.recomputeEntitlement(tx, &e, &order.ID, true)
}

func sameCoveragePolicy(a, b *coverageRow) bool {
	return a != nil && b != nil && bytes.Equal(a.version.SnapshotHash, b.version.SnapshotHash) && a.mapping.ConnectionID == b.mapping.ConnectionID && a.mapping.RemoteProductID == b.mapping.RemoteProductID && a.mapping.RemotePolicyID == b.mapping.RemotePolicyID && a.mapping.RemotePolicyVersion == b.mapping.RemotePolicyVersion
}

// Rebuild the desired absolute grant from immutable, non-voided coverage.
// Upgrade overlays depend on their base interval; refunding the overlay reveals
// the base, while refunding that base also makes its overlays inapplicable.
func (s *Service) recomputeEntitlement(tx *gorm.DB, e *storage.Entitlement, source *string, force bool) error {
	if e.Status == "revoked" {
		return nil
	}
	rows, err := s.coverageRows(tx, e.ID)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	at := now()
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
			desired = "suspended"
			chosen = latestBase(rows)
			if chosen == nil {
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
	if e.ManualHold {
		desired = "suspended"
	}
	var prior storage.SyncTask
	var previous ports.EntitlementCommand
	if err := tx.Where("entitlement_id = ?", e.ID).Order("target_version DESC").First(&prior).Error; err == nil {
		if json.Unmarshal(prior.RequestJSON, &previous) != nil {
			return ErrStorage
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if !force && previous.OperationID != "" && previous.DesiredStatus == desired && previous.ValidFrom.Equal(from) && sameTime(previous.ValidUntil, until) && previous.PolicyID == chosen.mapping.RemotePolicyID && previous.PolicyVersion == chosen.mapping.RemotePolicyVersion && previous.SnapshotHash == hex.EncodeToString(chosen.version.SnapshotHash) {
		return tx.Model(e).Updates(map[string]any{"next_transition_at": next, "current_coverage_id": currentID}).Error
	}
	if e.CustomerRef == nil {
		return ErrStorage
	}
	operation, err := newID()
	if err != nil {
		return err
	}
	c := ports.EntitlementCommand{OperationID: operation, CustomerRef: *e.CustomerRef, ProductID: chosen.mapping.RemoteProductID, PolicyID: chosen.mapping.RemotePolicyID, PolicyVersion: chosen.mapping.RemotePolicyVersion, SnapshotHash: hex.EncodeToString(chosen.version.SnapshotHash), ExpectedVersion: e.DesiredVersion, TargetVersion: e.DesiredVersion + 1, ValidFrom: from, ValidUntil: until, DesiredStatus: desired}
	var binding storage.LicenseBinding
	if err := tx.First(&binding, "entitlement_id = ? AND replaced_at IS NULL", e.ID).Error; err == nil {
		if binding.ConnectionID != chosen.mapping.ConnectionID {
			return problem(409, "LICENSE_CONNECTION_CONFLICT", "当前授权连接不可自动替换")
		}
		c.LicenseID = binding.RemoteLicenseID
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err := tx.Create(&storage.SyncTask{OperationID: operation, EntitlementID: e.ID, SourceOrderID: source, ConnectionID: &chosen.mapping.ConnectionID, ExpectedVersion: c.ExpectedVersion, TargetVersion: c.TargetVersion, RequestJSON: marshal(c), RequestHash: securityDigest(marshal(c)), Status: "pending"}).Error; err != nil {
		return err
	}
	return tx.Model(e).Updates(map[string]any{"desired_version": c.TargetVersion, "status": "pending", "valid_from": from, "valid_until": until, "snapshot": chosen.coverage.Snapshot, "current_coverage_id": currentID, "next_transition_at": next}).Error
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
