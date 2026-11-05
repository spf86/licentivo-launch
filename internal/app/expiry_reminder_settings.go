package app

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/storage"
)

type ExpiryReminderSettingsView struct {
	Enabled             bool       `json:"enabled"`
	DefaultSend         bool       `json:"default_send"`
	ReminderDays        []int      `json:"reminder_days"`
	Locale              string     `json:"locale"`
	ContactEmail        string     `json:"contact_email"`
	Version             string     `json:"version"`
	ScanIntervalSeconds int        `json:"scan_interval_seconds"`
	NextCheckAt         *time.Time `json:"next_check_at,omitempty"`
	LastScanError       string     `json:"last_scan_error,omitempty"`
}

type ExpiryReminderSettingsInput struct {
	Enabled      bool   `json:"enabled"`
	DefaultSend  bool   `json:"default_send"`
	ReminderDays []int  `json:"reminder_days"`
	Locale       string `json:"locale"`
	ContactEmail string `json:"contact_email"`
	Version      string `json:"version"`
}

func readExpiryReminderSettings(db *gorm.DB) (ExpiryReminderSettingsView, error) {
	v := ExpiryReminderSettingsView{DefaultSend: true, ReminderDays: []int{1, 3, 7}, Locale: "zh-CN", ScanIntervalSeconds: 3600}
	var row storage.ExpiryReminderSetting
	err := db.First(&row, "id=1").Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	v.Enabled, v.DefaultSend, v.Locale, v.ContactEmail, v.Version = row.Enabled, row.DefaultSend, row.Locale, row.ContactEmail, row.Version
	if err = json.Unmarshal([]byte(row.ReminderDays), &v.ReminderDays); err != nil {
		return v, err
	}
	if !validExpiryLocale(v.Locale) {
		return v, ErrStorage
	}
	return v, nil
}

func (s *Service) ExpiryReminderSettings(ctx context.Context, p Principal) (ExpiryReminderSettingsView, error) {
	if !p.IsAdmin() {
		return ExpiryReminderSettingsView{}, ErrForbidden
	}
	v, err := readExpiryReminderSettings(s.db.WithContext(ctx))
	if err != nil {
		return v, databaseError(err)
	}
	var schedule storage.ExpiryReminderSchedule
	if err = s.db.WithContext(ctx).First(&schedule, "id=1").Error; err == nil {
		v.NextCheckAt, v.LastScanError = &schedule.NextRunAt, schedule.LastErrorCode
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return v, ErrStorage
	}
	return v, nil
}

func normalizeExpiryReminderDays(days []int) ([]int, error) {
	if len(days) < 1 || len(days) > 10 {
		return nil, problem(400, "REMINDER_DAYS_INVALID", "提醒天数须为 1–90 的整数，最多 10 个，且不能重复")
	}
	days = slices.Clone(days)
	sort.Ints(days)
	for i, day := range days {
		if day < 1 || day > 90 || i > 0 && day == days[i-1] {
			return nil, problem(400, "REMINDER_DAYS_INVALID", "提醒天数须为 1–90 的整数，最多 10 个，且不能重复")
		}
	}
	return days, nil
}

func cancelExpiryJobs(db *gorm.DB, reason string) error {
	return db.Model(&storage.EmailOutbox{}).Where("kind='expiry_reminder' AND status IN ('pending','retrying','manual_review')").Updates(map[string]any{"status": "cancelled", "last_error_code": reason, "ciphertext": []byte{}, "notification_key": nil, "lease_until": nil, "lease_token": nil}).Error
}

func (s *Service) SaveExpiryReminderSettings(ctx context.Context, p Principal, in ExpiryReminderSettingsInput) (ExpiryReminderSettingsView, error) {
	if !p.IsAdmin() {
		return ExpiryReminderSettingsView{}, ErrForbidden
	}
	days, err := normalizeExpiryReminderDays(in.ReminderDays)
	if err != nil {
		return ExpiryReminderSettingsView{}, err
	}
	if !validExpiryLocale(in.Locale) {
		return ExpiryReminderSettingsView{}, problem(400, "REMINDER_LANGUAGE_INVALID", "请选择支持的邮件语言")
	}
	in.ContactEmail = strings.TrimSpace(in.ContactEmail)
	if in.ContactEmail != "" {
		if in.ContactEmail, err = email(in.ContactEmail); err != nil {
			return ExpiryReminderSettingsView{}, problem(400, "REMINDER_CONTACT_INVALID", "请填写有效的商家联系邮箱，或留空")
		}
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var instance storage.InstanceSettings
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&instance, "id=1").Error; err != nil {
			return err
		}
		old, err := readExpiryReminderSettings(tx.Clauses(clause.Locking{Strength: "UPDATE"}))
		if err != nil {
			return err
		}
		if old.Version != in.Version {
			return problem(409, "REMINDER_SETTINGS_CHANGED", "到期提醒设置已改变，请刷新后重试")
		}
		if in.Enabled {
			quota, err := loadMailQuota(tx)
			if err != nil {
				return err
			}
			if !quota.Configured {
				return problem(409, "REMINDER_QUOTA_REQUIRED", "请先在邮件设置中保存全站发送上限和保留额度")
			}
			enabled, err := mailEnabled(tx)
			if err != nil {
				return err
			}
			if !enabled {
				return problem(409, "REMINDER_MAIL_REQUIRED", "请先在邮件设置中启用邮件服务")
			}
		}
		version, err := newID()
		if err != nil {
			return err
		}
		raw, _ := json.Marshal(days)
		row := storage.ExpiryReminderSetting{ID: 1, Enabled: in.Enabled, DefaultSend: in.DefaultSend, ReminderDays: string(raw), Locale: in.Locale, ContactEmail: in.ContactEmail, Version: version, UpdatedAt: now()}
		if err = tx.Save(&row).Error; err != nil {
			return err
		}
		if !in.Enabled || !slices.Equal(days, old.ReminderDays) {
			if err = cancelExpiryJobs(tx, "REMINDER_SETTINGS_CHANGED"); err != nil {
				return err
			}
		} else if !in.DefaultSend {
			if err = cancelExpiryJobs(tx.Where("entitlement_id NOT IN (SELECT entitlement_id FROM expiry_reminder_preferences WHERE mode='on')"), "REMINDER_DISABLED"); err != nil {
				return err
			}
		}
		return audit(tx, p.User.ID, "expiry_reminder.configure", "mail_settings", "1", "")
	})
	if err != nil {
		return ExpiryReminderSettingsView{}, databaseError(err)
	}
	return s.ExpiryReminderSettings(ctx, p)
}

func readExpiryPreference(db *gorm.DB, id string) (storage.ExpiryReminderPreference, error) {
	v := storage.ExpiryReminderPreference{EntitlementID: id, Mode: "inherit"}
	err := db.First(&v, "entitlement_id=?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return v, nil
	}
	return v, err
}

type ExpiryReminderPreferenceView struct {
	Preference storage.ExpiryReminderPreference `json:"preference"`
	Details    expiryMailDetails                `json:"details"`
	Locale     string                           `json:"locale"`
	Enabled    bool                             `json:"enabled"`
	Reason     string                           `json:"reason"`
}

func canReadExpiryPreferences(p Principal) bool {
	return !p.User.PasswordChangeRequired && (p.Role == "admin" || p.Role == "operations" || p.Role == "support")
}

func (s *Service) ExpiryReminderPreference(ctx context.Context, p Principal, id string) (ExpiryReminderPreferenceView, error) {
	if !canReadExpiryPreferences(p) {
		return ExpiryReminderPreferenceView{}, ErrForbidden
	}
	settings, err := readExpiryReminderSettings(s.db.WithContext(ctx))
	if err != nil {
		return ExpiryReminderPreferenceView{}, databaseError(err)
	}
	v, err := s.expiryDeliveryState(s.db.WithContext(ctx), id, settings, now())
	return v, databaseError(err)
}

type ExpiryReminderPreferenceInput struct {
	Mode    string `json:"mode"`
	Locale  string `json:"locale"`
	Version string `json:"version"`
}

func (s *Service) SaveExpiryReminderPreference(ctx context.Context, p Principal, id string, in ExpiryReminderPreferenceInput) (ExpiryReminderPreferenceView, error) {
	if !p.CanManage() {
		return ExpiryReminderPreferenceView{}, ErrForbidden
	}
	if in.Mode != "inherit" && in.Mode != "on" && in.Mode != "off" {
		return ExpiryReminderPreferenceView{}, ErrInvalid
	}
	if in.Locale != "" && !validExpiryLocale(in.Locale) {
		return ExpiryReminderPreferenceView{}, problem(400, "REMINDER_LANGUAGE_INVALID", "请选择支持的邮件语言")
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var entitlement storage.Entitlement
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&entitlement, "id=?", id).Error; err != nil {
			return err
		}
		old, err := readExpiryPreference(tx.Clauses(clause.Locking{Strength: "UPDATE"}), id)
		if err != nil {
			return err
		}
		if old.Version != in.Version {
			return problem(409, "REMINDER_PREFERENCE_CHANGED", "这张授权的提醒设置已改变，请刷新后重试")
		}
		version, err := newID()
		if err != nil {
			return err
		}
		row := storage.ExpiryReminderPreference{EntitlementID: id, Mode: in.Mode, Locale: in.Locale, Version: version, UpdatedAt: now()}
		if err = tx.Omit(clause.Associations).Save(&row).Error; err != nil {
			return err
		}
		if in.Mode == "off" {
			if err = cancelExpiryJobs(tx.Where("entitlement_id=?", id), "REMINDER_DISABLED"); err != nil {
				return err
			}
		}
		return audit(tx, p.User.ID, "expiry_reminder.preference", "entitlements", id, "")
	})
	if err != nil {
		return ExpiryReminderPreferenceView{}, databaseError(err)
	}
	return s.ExpiryReminderPreference(ctx, p, id)
}
