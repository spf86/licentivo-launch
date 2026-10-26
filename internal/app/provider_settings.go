package app

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"licentivo-launch/internal/storage"
)

type LicensingProviderInput struct {
	Provider string `json:"provider"`
}

func (s *Service) activeLicensingProvider(tx *gorm.DB) (string, error) {
	var settings storage.InstanceSettings
	if err := tx.Select("licensing_provider").First(&settings, "id = 1").Error; err != nil {
		return "", err
	}
	if settings.LicensingProvider == CustomLicensing {
		return CustomLicensing, nil
	}
	return "licentivo", nil
}

// All provider changes and new orders lock the same existing singleton row.
// This also serializes first-time configuration, when no account exists yet.
func lockProviderSettings(tx *gorm.DB, strength string) (storage.InstanceSettings, error) {
	var settings storage.InstanceSettings
	err := tx.Clauses(clause.Locking{Strength: strength}).First(&settings, "id = 1").Error
	return settings, err
}

func (s *Service) normalizeProviderSettings(ctx context.Context) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		settings, err := lockProviderSettings(tx, "UPDATE")
		if err != nil {
			return err
		}
		if settings.LicensingProvider == "" {
			// Preserve installations that already use only custom licensing.
			var providers []string
			if err := tx.Model(&storage.ProductLicensingBinding{}).Distinct("provider").Pluck("provider", &providers).Error; err != nil {
				return err
			}
			selected := "licentivo"
			if len(providers) == 1 && providers[0] == CustomLicensing {
				selected = CustomLicensing
			}
			if err := tx.Model(&settings).Update("licensing_provider", selected).Error; err != nil {
				return err
			}
		}
		// Older versions permitted several providers in one environment. Keep
		// the most recently verified active account; retain all old credentials
		// and account IDs for their existing payments, refunds and webhooks.
		for _, env := range []string{"sandbox", "live"} {
			var account storage.PaymentAccount
			err := tx.Where("environment = ? AND enabled = true", env).Order("last_checked_at DESC, id DESC").First(&account).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if err := tx.Model(&storage.PaymentAccount{}).Where("environment = ? AND enabled = true AND id <> ?", env, account.ID).Update("enabled", false).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) SaveLicensingProvider(ctx context.Context, p Principal, in LicensingProviderInput) error {
	if !p.IsAdmin() {
		return ErrForbidden
	}
	if in.Provider != "licentivo" && in.Provider != CustomLicensing {
		return ErrInvalid
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		settings, err := lockProviderSettings(tx, "UPDATE")
		if err != nil {
			return err
		}
		if settings.LicensingProvider == in.Provider {
			return nil
		}
		var orders int64
		query := tx.Model(&storage.Order{})
		if in.Provider == "licentivo" {
			query = query.Where("JSON_EXTRACT(entitlement_snapshot, '$.external_licensing.connection_id') IS NOT NULL")
		} else {
			query = query.Where("JSON_EXTRACT(entitlement_snapshot, '$.external_licensing.connection_id') IS NULL")
		}
		if err := query.Count(&orders).Error; err != nil {
			return err
		}
		if orders > 0 {
			return problem(409, "LICENSING_MIGRATION_REQUIRED", "已有订单使用另一授权系统，不能直接切换；请先完成授权迁移")
		}
		if err := tx.Model(&settings).Update("licensing_provider", in.Provider).Error; err != nil {
			return err
		}
		if err := tx.Model(&storage.PlanPublication{}).Where("preview_only = false").Update("preview_only", true).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "licensing.provider.select", "instance_settings", "1", in.Provider)
	}))
}

func (s *Service) requireLicensingProvider(tx *gorm.DB, provider string) error {
	selected, err := s.activeLicensingProvider(tx)
	if err != nil {
		return err
	}
	if selected != provider {
		return problem(409, "LICENSING_PROVIDER_INACTIVE", "该授权系统未启用，请到系统设置的授权系统页面切换")
	}
	return nil
}
