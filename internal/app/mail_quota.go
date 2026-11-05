package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/storage"
)

const expiryMailPriority = 20

type MailPeriodQuota struct {
	Total   int64 `json:"total"`
	Reserve int64 `json:"reserve"`
	Limit   int64 `json:"limit"`
}

type MailQuotaPolicy struct {
	Hour  MailPeriodQuota `json:"hour"`
	Day   MailPeriodQuota `json:"day"`
	Month MailPeriodQuota `json:"month"`
}

type MailQuotaInput struct {
	MailQuotaPolicy
	Version string `json:"version"`
}

type MailQuotaUsage struct {
	TotalUsed    int64     `json:"total_used"`
	ReminderUsed int64     `json:"reminder_used"`
	ResetsAt     time.Time `json:"resets_at"`
}

type MailQuotaView struct {
	MailQuotaPolicy
	Version    string                    `json:"version"`
	Configured bool                      `json:"configured"`
	Usage      map[string]MailQuotaUsage `json:"usage"`
	Available  int64                     `json:"reminder_available"`
}

func validateMailQuotas(policy MailQuotaPolicy) error {
	for _, q := range []MailPeriodQuota{policy.Hour, policy.Day, policy.Month} {
		if q.Total < 2 || q.Total > 10000000 || q.Reserve < 1 || q.Reserve >= q.Total || q.Limit < 0 || q.Limit > q.Total-q.Reserve {
			return problem(400, "MAIL_QUOTA_INVALID", "保留额度必须小于全站总预算，到期提醒上限不能占用保留额度；请填写 0–10000000 内的整数")
		}
	}
	return nil
}

func quotaPeriods(at time.Time) []struct {
	name string
	id   string
	end  time.Time
} {
	at = at.UTC()
	day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	month := time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, time.UTC)
	return []struct {
		name, id string
		end      time.Time
	}{
		{"month", "month:" + month.Format("200601"), month.AddDate(0, 1, 0)},
		{"day", "day:" + day.Format("20060102"), day.AddDate(0, 0, 1)},
		{"hour", "hour:" + at.Format("2006010215"), at.Truncate(time.Hour).Add(time.Hour)},
	}
}

func loadMailQuota(db *gorm.DB) (MailQuotaView, error) {
	v := MailQuotaView{Usage: map[string]MailQuotaUsage{}}
	var row storage.MailQuotaSetting
	err := db.First(&row, "id=1").Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	if err = json.Unmarshal([]byte(row.Policy), &v.MailQuotaPolicy); err != nil {
		return v, err
	}
	v.Version, v.Configured = row.Version, true
	return v, validateMailQuotas(v.MailQuotaPolicy)
}

func quotaPolicyFor(p MailQuotaPolicy, period string) MailPeriodQuota {
	switch period {
	case "hour":
		return p.Hour
	case "day":
		return p.Day
	default:
		return p.Month
	}
}

func remainingReminderQuota(q MailPeriodQuota, u MailQuotaUsage) int64 {
	return max(0, min(q.Total-u.TotalUsed-q.Reserve, q.Limit-u.ReminderUsed))
}

func (s *Service) MailQuotas(ctx context.Context, p Principal) (MailQuotaView, error) {
	if !p.IsAdmin() {
		return MailQuotaView{}, ErrForbidden
	}
	db := s.db.WithContext(ctx)
	v, err := loadMailQuota(db)
	if err != nil {
		return v, databaseError(err)
	}
	var at time.Time
	if err = db.Raw("SELECT UTC_TIMESTAMP(6)").Scan(&at).Error; err != nil {
		return v, ErrStorage
	}
	v.Available = 10000000
	for _, period := range quotaPeriods(at) {
		var counter storage.MailQuotaWindow
		if err = db.First(&counter, "id=?", period.id).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return v, ErrStorage
		}
		u := MailQuotaUsage{TotalUsed: counter.TotalAttempts, ReminderUsed: counter.ReminderAttempts, ResetsAt: period.end}
		v.Usage[period.name] = u
		v.Available = min(v.Available, remainingReminderQuota(quotaPolicyFor(v.MailQuotaPolicy, period.name), u))
	}
	if !v.Configured {
		v.Available = 0
	}
	return v, nil
}

func (s *Service) SaveMailQuotas(ctx context.Context, p Principal, in MailQuotaInput) (MailQuotaView, error) {
	if !p.IsAdmin() {
		return MailQuotaView{}, ErrForbidden
	}
	if err := validateMailQuotas(in.MailQuotaPolicy); err != nil {
		return MailQuotaView{}, err
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var instance storage.InstanceSettings
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&instance, "id=1").Error; err != nil {
			return err
		}
		old, err := loadMailQuota(tx.Clauses(clause.Locking{Strength: "UPDATE"}))
		if err != nil {
			return err
		}
		if old.Version != in.Version {
			return problem(409, "MAIL_QUOTA_CHANGED", "邮件额度已被修改，请刷新后重试")
		}
		version, err := newID()
		if err != nil {
			return err
		}
		raw, err := json.Marshal(in.MailQuotaPolicy)
		if err != nil {
			return err
		}
		row := storage.MailQuotaSetting{ID: 1, Policy: string(raw), Version: version, UpdatedAt: now()}
		if err = tx.Save(&row).Error; err != nil {
			return err
		}
		// An increased budget may unblock waiting mail. Counters are never reset.
		if err = tx.Model(&storage.EmailOutbox{}).Where("status IN ('pending','retrying') AND last_error_code='MAIL_QUOTA_WAIT'").Update("available_at", gorm.Expr("UTC_TIMESTAMP(6)")).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "mail.quotas", "mail_settings", "1", "")
	})
	if err != nil {
		return MailQuotaView{}, databaseError(err)
	}
	return s.MailQuotas(ctx, p)
}

// Reserve before contacting SMTP. Every sender shares these locked site-wide
// counters. Failed/uncertain SMTP attempts also consume budget conservatively.
// Merely waiting consumes neither quota nor a retry attempt.
func (s *Service) reserveMailQuota(ctx context.Context, low bool) (time.Time, error) {
	var waitUntil time.Time
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		v, err := loadMailQuota(tx.Clauses(clause.Locking{Strength: "UPDATE"}))
		if err != nil {
			return err
		}
		var at time.Time
		if err = tx.Raw("SELECT UTC_TIMESTAMP(6)").Scan(&at).Error; err != nil {
			return err
		}
		if low && !v.Configured {
			waitUntil = at.Add(time.Hour)
			return nil
		}
		periods := quotaPeriods(at)
		rows := make([]storage.MailQuotaWindow, len(periods))
		for i, period := range periods {
			row := storage.MailQuotaWindow{ID: period.id, EndsAt: period.end}
			if err = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
				return err
			}
			if err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&rows[i], "id=?", period.id).Error; err != nil {
				return err
			}
			if !v.Configured {
				continue
			}
			q := quotaPolicyFor(v.MailQuotaPolicy, period.name)
			blocked := rows[i].TotalAttempts >= q.Total
			if low {
				blocked = blocked || remainingReminderQuota(q, MailQuotaUsage{TotalUsed: rows[i].TotalAttempts, ReminderUsed: rows[i].ReminderAttempts}) == 0
			}
			if blocked && period.end.After(waitUntil) {
				waitUntil = period.end
			}
		}
		if low && waitUntil.IsZero() {
			var higher int64
			if err = tx.Model(&storage.EmailOutbox{}).Where("priority<? AND ((status IN ('pending','retrying') AND available_at<=?) OR (status='running' AND lease_until>=?))", expiryMailPriority, at, at).Limit(1).Count(&higher).Error; err != nil {
				return err
			}
			if higher > 0 {
				waitUntil = at.Add(5 * time.Second)
			}
		}
		if !waitUntil.IsZero() {
			return nil
		}
		for _, row := range rows {
			changes := map[string]any{"total_attempts": gorm.Expr("total_attempts+1")}
			if low {
				changes["reminder_attempts"] = gorm.Expr("reminder_attempts+1")
			}
			if err = tx.Model(&storage.MailQuotaWindow{}).Where("id=?", row.ID).Updates(changes).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return waitUntil, databaseError(err)
}
