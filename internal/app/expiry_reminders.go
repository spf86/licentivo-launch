package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/mailer"
	"licentivo-launch/internal/storage"
)

func eligibleExpiryReminder(days []int, end, at time.Time) int {
	if !end.After(at) {
		return 0
	}
	remaining := int(math.Ceil(end.Sub(at).Hours() / 24))
	chosen := 0
	for _, day := range days {
		if remaining <= day && (chosen == 0 || day < chosen) {
			chosen = day
		}
	}
	return chosen
}

// Reuse the sales ledger's coverage selection, including contiguous advance
// renewals and voided refunds. SDK lease deadlines are never an expiry source.
func (s *Service) expiryDetails(db *gorm.DB, id string, at time.Time) (expiryMailDetails, string, error) {
	d := expiryMailDetails{EntitlementID: id}
	var e storage.Entitlement
	if err := db.First(&e, "id=?", id).Error; err != nil {
		return d, "", err
	}
	d.Environment = e.Environment
	var user storage.User
	if err := db.First(&user, "id=?", e.UserID).Error; err != nil {
		return d, "", err
	}
	d.CustomerEmail = user.Email
	var instance storage.InstanceSettings
	if err := db.First(&instance, "id=1").Error; err != nil {
		return d, "", err
	}
	var brand struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(instance.Brand, &brand); err != nil {
		return d, "", err
	}
	d.Brand = brand.Name
	if d.Brand == "" {
		d.Brand = "Licentivo Launch"
	}
	reader := &Service{db: db}
	products, _, err := reader.customerPurchaseProducts(db.Statement.Context, []string{e.UserID}, e.Environment)
	if err != nil {
		return d, "", err
	}
	var product *CustomerPurchaseProduct
	for i := range products[e.UserID] {
		if products[e.UserID][i].ID == id {
			product = &products[e.UserID][i]
			break
		}
	}
	if product == nil {
		return d, "REMINDER_COVERAGE_UNAVAILABLE", nil
	}
	d.ProductName, d.PlanName, d.EndsAt = product.ProductName, product.PlanName, product.PurchasedUntil
	if d.EndsAt == nil {
		d.EndsAt = product.ValidUntil
	}
	if product.Permanent || product.PurchasedPermanent {
		return d, "REMINDER_PERMANENT", nil
	}
	if d.EndsAt == nil || !product.DatesKnown {
		return d, "REMINDER_EXPIRY_UNKNOWN", nil
	}
	if !d.EndsAt.After(at) {
		return d, "REMINDER_EXPIRED", nil
	}
	if product.Status != "active" || e.Status != "active" || e.ManualHold || e.AppliedVersion == 0 || e.AppliedVersion != e.DesiredVersion {
		return d, "REMINDER_AUTHORIZATION_UNAVAILABLE", nil
	}
	if user.Status != "active" {
		return d, "REMINDER_CUSTOMER_UNAVAILABLE", nil
	}
	if _, err := email(user.Email); err != nil {
		return d, "REMINDER_EMAIL_UNAVAILABLE", nil
	}
	var order storage.Order
	if err := db.First(&order, "id=?", product.SourceOrderID).Error; err != nil {
		return d, "", err
	}
	// If continuous advance purchases extend beyond the current plan, identify
	// the actual last purchased plan that will end at this final deadline.
	if product.ValidUntil != nil && product.ValidUntil.Before(*d.EndsAt) {
		var tail storage.EntitlementCoverage
		if err := db.Where("entitlement_id=? AND voided_at IS NULL AND base_coverage_id IS NULL AND valid_until=?", id, *d.EndsAt).Order("valid_from DESC,created_at DESC,id DESC").First(&tail).Error; err != nil {
			return d, "", err
		}
		if err := db.First(&order, "id=?", tail.SourceOrderID).Error; err != nil {
			return d, "", err
		}
	}
	if order.UserID != e.UserID || order.ProductID != e.ProductID || order.Environment != e.Environment || order.PaidAt == nil {
		return d, "REMINDER_COVERAGE_UNAVAILABLE", nil
	}
	var version storage.PlanVersion
	var plan storage.Plan
	if err := db.First(&version, "id=?", order.PlanVersionID).Error; err != nil {
		return d, "", err
	}
	if err := db.First(&plan, "id=?", version.PlanID).Error; err != nil {
		return d, "", err
	}
	d.PlanName = historicalPlanName(order.PriceSnapshot, plan.Name)
	var mapping storage.PlanLicenseMapping
	err = db.First(&mapping, "plan_version_id=?", version.ID).Error
	if err == nil {
		d.PolicyName = mapping.RemotePolicyName
		var connection storage.LicentivoConnection
		if err := db.First(&connection, "id=?", mapping.ConnectionID).Error; err != nil {
			return d, "", err
		}
		var catalog licentivoCatalogSnapshot
		if json.Unmarshal(connection.Catalog, &catalog) == nil {
			for _, policy := range catalog.Policies.Items {
				if policy.ID == mapping.RemotePolicyID && policy.Name != "" {
					d.PolicyName = policy.Name
					break
				}
			}
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return d, "", err
	}
	if d.PolicyName == "" {
		return d, "REMINDER_POLICY_NAME_UNAVAILABLE", nil
	}
	return d, "", nil
}

func (s *Service) expiryDeliveryState(db *gorm.DB, id string, settings ExpiryReminderSettingsView, at time.Time) (ExpiryReminderPreferenceView, error) {
	v := ExpiryReminderPreferenceView{}
	var err error
	v.Preference, err = readExpiryPreference(db, id)
	if err != nil {
		return v, err
	}
	v.Details, v.Reason, err = s.expiryDetails(db, id, at)
	if err != nil {
		return v, err
	}
	v.Locale = settings.Locale
	if v.Preference.Locale != "" {
		v.Locale = v.Preference.Locale
	}
	if !validExpiryLocale(v.Locale) {
		return v, ErrStorage
	}
	if !settings.Enabled || v.Preference.Mode == "off" || v.Preference.Mode == "inherit" && !settings.DefaultSend {
		v.Reason = "REMINDER_DISABLED"
	}
	v.Enabled = v.Reason == ""
	return v, nil
}

func (s *Service) enqueueExpiryReminder(tx *gorm.DB, details expiryMailDetails, settings ExpiryReminderSettingsView, locale string, day int, at time.Time) error {
	message := makeExpiryMail(details, settings, locale, day)
	id, err := newID()
	if err != nil {
		return err
	}
	sealed, err := s.vault.Seal(marshal(message), "email-outbox:"+id)
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(details.EntitlementID + ":" + details.EndsAt.UTC().Format(time.RFC3339Nano) + ":" + strconv.Itoa(day)))
	key := hex.EncodeToString(digest[:])
	row := storage.EmailOutbox{ID: id, Ciphertext: sealed, Status: "pending", Priority: expiryMailPriority, Kind: "expiry_reminder", EntitlementID: &details.EntitlementID, ReminderEnd: details.EndsAt, ReminderDays: day, NotificationKey: &key, RecipientEmail: details.CustomerEmail, NotificationLocale: locale, ProductName: details.ProductName, PlanName: details.PlanName, PolicyName: details.PolicyName, AvailableAt: at, CreatedAt: at}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
}

// RunExpiryReminders claims a persisted hourly slot, so additional processes
// and restarts cannot multiply a site's scans or message budget.
func (s *Service) RunExpiryReminders(ctx context.Context) error {
	if s.RecoveryRequired(ctx) {
		return nil
	}
	var at time.Time
	if err := s.db.WithContext(ctx).Raw("SELECT UTC_TIMESTAMP(6)").Scan(&at).Error; err != nil {
		return ErrStorage
	}
	settings, err := readExpiryReminderSettings(s.db.WithContext(ctx))
	if err != nil {
		return databaseError(err)
	}
	if !settings.Enabled {
		return nil
	}
	mailReady, err := mailEnabled(s.db.WithContext(ctx))
	if err != nil || !mailReady {
		return err
	}
	quota, err := loadMailQuota(s.db.WithContext(ctx))
	if err != nil || !quota.Configured {
		return databaseError(err)
	}
	token, err := newID()
	if err != nil {
		return err
	}
	claimed := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row := storage.ExpiryReminderSchedule{ID: 1, NextRunAt: at}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id=1").Error; err != nil {
			return err
		}
		if row.NextRunAt.After(at) || row.LeaseUntil != nil && row.LeaseUntil.After(at) {
			return nil
		}
		claimed = true
		return tx.Model(&row).Updates(map[string]any{"next_run_at": at.Add(time.Hour), "lease_token": token, "lease_until": at.Add(10 * time.Minute), "last_started_at": at, "last_error_code": ""}).Error
	})
	if err != nil || !claimed {
		return databaseError(err)
	}
	scanErr := s.scanExpiryReminders(ctx, at)
	code := ""
	if scanErr != nil {
		code = "REMINDER_SCAN_FAILED"
	}
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	updateErr := s.db.WithContext(finish).Model(&storage.ExpiryReminderSchedule{}).Where("id=1 AND lease_token=?", token).Updates(map[string]any{"lease_token": nil, "lease_until": nil, "last_error_code": code}).Error
	if scanErr != nil {
		return scanErr
	}
	return databaseError(updateErr)
}

func (s *Service) scanExpiryReminders(ctx context.Context, at time.Time) error {
	last := ""
	var firstErr error
	for {
		var rows []storage.Entitlement
		// Paginate independently of the current entitlement end: a later purchase
		// can extend continuous coverage beyond the presently active plan.
		if err := s.db.WithContext(ctx).Select("id").Where("environment='live' AND status='active' AND manual_hold=false AND id>?", last).Order("id").Limit(100).Find(&rows).Error; err != nil {
			return databaseError(err)
		}
		for _, row := range rows {
			err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				var entitlement storage.Entitlement
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&entitlement, "id=?", row.ID).Error; err != nil {
					return err
				}
				current, err := readExpiryReminderSettings(tx)
				if err != nil {
					return err
				}
				state, err := s.expiryDeliveryState(tx, row.ID, current, at)
				if err != nil {
					return err
				}
				if !state.Enabled || state.Details.EndsAt == nil {
					return nil
				}
				day := eligibleExpiryReminder(current.ReminderDays, *state.Details.EndsAt, at)
				if day == 0 {
					return nil
				}
				return s.enqueueExpiryReminder(tx, state.Details, current, state.Locale, day, at)
			})
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				// One damaged historical purchase must not prevent other customers
				// from being scanned. Expose the failure in the settings status.
				if firstErr == nil {
					firstErr = err
				}
			}
		}
		if len(rows) < 100 {
			return databaseError(firstErr)
		}
		last = rows[len(rows)-1].ID
	}
}

func (s *Service) RunExpiryReminderWorker(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		job, cancel := context.WithTimeout(ctx, 5*time.Minute)
		_ = s.RunExpiryReminders(job)
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Refresh content and consent immediately before SMTP, including each retry.
func (s *Service) prepareExpiryReminder(ctx context.Context, row storage.EmailOutbox) (mailer.Message, string, error) {
	var message mailer.Message
	if s.RecoveryRequired(ctx) {
		return message, "REMINDER_RECOVERY_REQUIRED", nil
	}
	if row.EntitlementID == nil || row.ReminderEnd == nil {
		return message, "REMINDER_AUTHORIZATION_UNAVAILABLE", nil
	}
	settings, err := readExpiryReminderSettings(s.db.WithContext(ctx))
	if err != nil {
		return message, "", err
	}
	v, err := s.expiryDeliveryState(s.db.WithContext(ctx), *row.EntitlementID, settings, now())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return message, "REMINDER_AUTHORIZATION_UNAVAILABLE", nil
	}
	if err != nil {
		return message, "", err
	}
	if !v.Enabled {
		return message, v.Reason, nil
	}
	if v.Details.EndsAt == nil || !v.Details.EndsAt.Equal(*row.ReminderEnd) {
		return message, "REMINDER_COVERAGE_CHANGED", nil
	}
	if !strings.EqualFold(v.Details.CustomerEmail, row.RecipientEmail) {
		return message, "REMINDER_RECIPIENT_CHANGED", nil
	}
	if eligibleExpiryReminder(settings.ReminderDays, *v.Details.EndsAt, now()) != row.ReminderDays {
		return message, "REMINDER_WINDOW_CHANGED", nil
	}
	message = makeExpiryMail(v.Details, settings, v.Locale, row.ReminderDays)
	err = s.db.WithContext(ctx).Model(&storage.EmailOutbox{}).Where("id=? AND lease_token=?", row.ID, row.LeaseToken).Updates(map[string]any{"notification_locale": v.Locale, "product_name": v.Details.ProductName, "plan_name": v.Details.PlanName, "policy_name": v.Details.PolicyName}).Error
	return message, "", err
}

type ExpiryReminderPreview struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
	ReplyTo string `json:"reply_to"`
	Locale  string `json:"locale"`
}

func (s *Service) PreviewExpiryReminder(ctx context.Context, p Principal, locale, id string) (ExpiryReminderPreview, error) {
	if !p.IsAdmin() {
		return ExpiryReminderPreview{}, ErrForbidden
	}
	settings, err := readExpiryReminderSettings(s.db.WithContext(ctx))
	if err != nil {
		return ExpiryReminderPreview{}, databaseError(err)
	}
	if locale == "" {
		locale = settings.Locale
	}
	if !validExpiryLocale(locale) {
		return ExpiryReminderPreview{}, problem(400, "REMINDER_LANGUAGE_INVALID", "请选择支持的邮件语言")
	}
	end := now().Add(7 * 24 * time.Hour)
	details := expiryMailDetails{ProductName: "Atlas Pro", PlanName: "专业版年付", PolicyName: "专业版设备授权", CustomerEmail: "customer@example.com", Brand: "Licentivo Launch", EndsAt: &end}
	if id != "" {
		state, err := s.expiryDeliveryState(s.db.WithContext(ctx), id, settings, now())
		if err != nil {
			return ExpiryReminderPreview{}, databaseError(err)
		}
		details = state.Details
		if details.PolicyName == "" || details.EndsAt == nil {
			return ExpiryReminderPreview{}, problem(409, "REMINDER_PREVIEW_UNAVAILABLE", "这张授权的策略名称或到期时间尚不可用")
		}
	}
	day := settings.ReminderDays[len(settings.ReminderDays)-1]
	if details.EndsAt != nil {
		if chosen := eligibleExpiryReminder(settings.ReminderDays, *details.EndsAt, now()); chosen > 0 {
			day = chosen
		}
	}
	message := makeExpiryMail(details, settings, locale, day)
	return ExpiryReminderPreview{To: message.To, Subject: message.Subject, Body: message.Body, ReplyTo: message.ReplyTo, Locale: locale}, nil
}

type ExpiryReminderTestInput struct {
	To     string `json:"to"`
	Locale string `json:"locale"`
}

func (s *Service) TestExpiryReminder(ctx context.Context, p Principal, in ExpiryReminderTestInput) (map[string]string, error) {
	if !p.IsAdmin() {
		return nil, ErrForbidden
	}
	to, err := email(in.To)
	if err != nil {
		return nil, ErrInvalid
	}
	preview, err := s.PreviewExpiryReminder(ctx, p, in.Locale, "")
	if err != nil {
		return nil, err
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	sealed, err := s.vault.Seal(marshal(mailer.Message{To: to, Subject: "[TEST] " + preview.Subject, Body: preview.Body, ReplyTo: preview.ReplyTo}), "email-outbox:"+id)
	if err != nil {
		return nil, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
		row := storage.EmailOutbox{ID: id, Ciphertext: sealed, Status: "pending", Kind: "expiry_test", Priority: expiryMailPriority, RecipientEmail: to, NotificationLocale: preview.Locale, ProductName: "Atlas Pro", PlanName: "专业版年付", PolicyName: "专业版设备授权", AvailableAt: now(), CreatedAt: now()}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "expiry_reminder.test", "email_outboxes", id, "")
	})
	if err != nil {
		return nil, databaseError(err)
	}
	return map[string]string{"id": id, "status": "pending"}, nil
}

type ExpiryReminderJob struct {
	ID                 string     `json:"id"`
	EntitlementID      *string    `json:"entitlement_id"`
	Kind               string     `json:"kind"`
	Status             string     `json:"status"`
	Attempts           uint64     `json:"attempts"`
	RecipientEmail     string     `json:"recipient_email"`
	NotificationLocale string     `json:"locale"`
	ReminderDays       int        `json:"reminder_days"`
	ProductName        string     `json:"product_name"`
	PlanName           string     `json:"plan_name"`
	PolicyName         string     `json:"policy_name"`
	LastErrorCode      *string    `json:"reason"`
	CreatedAt          time.Time  `json:"created_at"`
	SentAt             *time.Time `json:"sent_at"`
	AvailableAt        time.Time  `json:"available_at"`
}

func (s *Service) ExpiryReminderJobs(ctx context.Context, p Principal, limit, offset int) (map[string]any, error) {
	if !p.IsAdmin() {
		return nil, ErrForbidden
	}
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, ErrInvalid
	}
	db := s.db.WithContext(ctx).Model(&storage.EmailOutbox{}).Where("kind IN ('expiry_reminder','expiry_test')")
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, ErrStorage
	}
	items := []ExpiryReminderJob{}
	if err := db.Select("id,entitlement_id,kind,status,attempts,recipient_email,notification_locale,reminder_days,product_name,plan_name,policy_name,last_error_code,created_at,sent_at,available_at").Order("created_at DESC,id DESC").Limit(limit).Offset(offset).Scan(&items).Error; err != nil {
		return nil, ErrStorage
	}
	return map[string]any{"items": items, "total": total, "limit": limit, "offset": offset}, nil
}
