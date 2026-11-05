package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"licentivo-launch/internal/mailer"
	"licentivo-launch/internal/security"
	"licentivo-launch/internal/storage"
)

type expiryRecordingSender struct {
	mu       sync.Mutex
	messages []mailer.Message
	fail     bool
}

func (r *expiryRecordingSender) Send(_ context.Context, m mailer.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = append(r.messages, m)
	if r.fail {
		return mailer.ErrDelivery
	}
	return nil
}
func (r *expiryRecordingSender) snapshot() []mailer.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]mailer.Message(nil), r.messages...)
}

func expiryTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("LAUNCH_TEST_EXPIRY_DSN")
	if dsn == "" {
		t.Skip("set LAUNCH_TEST_EXPIRY_DSN to the isolated launch_test_expiry_* MySQL runtime")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(cfg.DBName, "launch_test_expiry_") {
		t.Fatal("expiry tests require their own launch_test_expiry_* database")
	}
	control, err := storage.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close(control)
	id, err := security.ID()
	if err != nil {
		t.Fatal(err)
	}
	name := "launch_test_expiry_" + strings.ReplaceAll(id, "-", "")
	if err = control.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_bin").Error; err != nil {
		t.Fatal(err)
	}
	cfg.DBName = name
	db, err := storage.Open(cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close(db) })
	if err = storage.Initialize(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	t.Log("isolated database:", name)
	return db
}

type expiryFixture struct {
	t          *testing.T
	db         *gorm.DB
	s          *Service
	admin      Principal
	sender     *expiryRecordingSender
	config     Config
	product    storage.Product
	version    storage.PlanVersion
	connection storage.LicentivoConnection
}

func newExpiryFixture(t *testing.T) *expiryFixture {
	t.Helper()
	db := expiryTestDatabase(t)
	recorder := &expiryRecordingSender{}
	config := Config{PublicURL: "http://127.0.0.1:18096", MasterKey: bytes.Repeat([]byte{6}, 32), SetupToken: strings.Repeat("x", 43), UploadDir: t.TempDir(), MailFactory: func(mailer.Config) (mailer.Sender, error) { return recorder, nil }}
	s, err := New(context.Background(), db, config)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Bootstrap(context.Background(), BootstrapInput{Token: config.SetupToken, Email: "administrator@example.test", Password: "Expiry Test Password 123!", BrandName: "星河软件"}); err != nil {
		t.Fatal(err)
	}
	f := &expiryFixture{t: t, db: db, s: s, sender: recorder, config: config}
	f.must(db.First(&f.admin.User, "email=?", "administrator@example.test").Error)
	f.admin.Role = "admin"
	_, err = s.SaveMailSettings(context.Background(), f.admin, MailSettingsInput{Provider: "smtp", Host: "mail.example.test", Port: "587", Username: "test-sender", Encryption: "starttls", From: "sender@example.test", APIKey: "isolated-test-password"})
	f.must(err)
	f.product = storage.Product{ID: f.id(), Slug: "atlas-pro", Name: "Atlas Pro", Content: json.RawMessage(`{}`), Status: "published"}
	f.must(db.Create(&f.product).Error)
	plan := storage.Plan{ID: f.id(), ProductID: f.product.ID, Name: "专业版年付", Status: "published", AllowPurchase: true, AllowRenewal: true}
	f.must(db.Create(&plan).Error)
	unit, count := "year", uint64(1)
	f.version = storage.PlanVersion{ID: f.id(), PlanID: plan.ID, ProductID: f.product.ID, Version: 1, Currency: "USD", AmountMinor: 1200, PurchaseType: "fixed_term", TermUnit: &unit, TermCount: &count, EntitlementSnapshot: json.RawMessage(`{"features":[],"max_devices":2}`), SnapshotHash: make([]byte, 32)}
	f.must(db.Create(&f.version).Error)
	secret := storage.SecretRecord{ID: f.id(), Purpose: "expiry-fixture", KeyID: "test", Ciphertext: []byte{1}}
	f.must(db.Create(&secret).Error)
	policy := f.id()
	catalog := marshal(map[string]any{"products": map[string]any{"items": []map[string]string{{"id": f.product.ID, "name": f.product.Name}}}, "policies": map[string]any{"items": []map[string]string{{"id": policy, "product_id": f.product.ID, "name": "专业版设备授权"}}}})
	f.connection = storage.LicentivoConnection{ID: f.id(), BaseURL: "https://licentivo.example.test", CredentialSecretID: secret.ID, Capabilities: json.RawMessage(`{}`), Catalog: catalog}
	f.must(db.Create(&f.connection).Error)
	f.must(db.Create(&storage.PlanLicenseMapping{PlanVersionID: f.version.ID, ConnectionID: f.connection.ID, RemoteProductID: f.product.ID, RemoteProductName: f.product.Name, RemotePolicyID: policy, RemotePolicyName: "专业版设备授权", RemotePolicyVersion: 1}).Error)
	return f
}
func (f *expiryFixture) must(err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatal(err)
	}
}
func (f *expiryFixture) id() string { f.t.Helper(); id, err := security.ID(); f.must(err); return id }
func (f *expiryFixture) reset() {
	f.t.Helper()
	f.must(f.db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&storage.EmailOutbox{}).Error)
	f.must(f.db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&storage.MailQuotaWindow{}).Error)
	f.must(f.db.Model(&storage.Entitlement{}).Where("1=1").Update("manual_hold", true).Error)
	f.must(f.db.Model(&storage.ExpiryReminderSchedule{}).Where("id=1").Updates(map[string]any{"next_run_at": now().Add(-time.Hour), "lease_token": nil, "lease_until": nil}).Error)
	f.sender.mu.Lock()
	f.sender.messages = nil
	f.sender.fail = false
	f.sender.mu.Unlock()
}
func (f *expiryFixture) quotas(hour MailPeriodQuota) {
	f.t.Helper()
	old, err := f.s.MailQuotas(context.Background(), f.admin)
	f.must(err)
	_, err = f.s.SaveMailQuotas(context.Background(), f.admin, MailQuotaInput{MailQuotaPolicy: MailQuotaPolicy{Hour: hour, Day: MailPeriodQuota{Total: 10000, Reserve: 100, Limit: 9000}, Month: MailPeriodQuota{Total: 100000, Reserve: 1000, Limit: 90000}}, Version: old.Version})
	f.must(err)
}
func (f *expiryFixture) settings(enabled bool) {
	f.t.Helper()
	old, err := f.s.ExpiryReminderSettings(context.Background(), f.admin)
	f.must(err)
	_, err = f.s.SaveExpiryReminderSettings(context.Background(), f.admin, ExpiryReminderSettingsInput{Enabled: enabled, DefaultSend: true, ReminderDays: []int{1, 3, 7}, Locale: "zh-CN", Version: old.Version})
	f.must(err)
}
func (f *expiryFixture) customer(end time.Time) (storage.Entitlement, storage.EntitlementCoverage) {
	f.t.Helper()
	at := now()
	start := at.Add(-24 * time.Hour)
	user := storage.User{ID: f.id(), Email: "customer-" + f.id() + "@example.test", PasswordHash: f.admin.User.PasswordHash, Status: "active", EmailVerifiedAt: &at}
	user.EmailKey = user.Email
	f.must(f.db.Create(&user).Error)
	order := storage.Order{ID: f.id(), UserID: user.ID, ProductID: f.product.ID, PlanVersionID: f.version.ID, Environment: "live", Action: "purchase", Status: "paid", IdempotencyKey: f.id(), RequestHash: make([]byte, 32), Currency: "USD", AmountMinor: 1200, PriceSnapshot: json.RawMessage(`{"plan_name":"专业版年付"}`), EntitlementSnapshot: f.version.EntitlementSnapshot, PaidAt: &start, ExpiresAt: at.Add(time.Hour)}
	f.must(f.db.Create(&order).Error)
	ref := f.id()
	e := storage.Entitlement{ID: f.id(), UserID: user.ID, ProductID: f.product.ID, Environment: "live", DesiredVersion: 1, AppliedVersion: 1, Status: "active", LaunchPurchaseRef: &ref, ValidFrom: &start, ValidUntil: &end, Snapshot: f.version.EntitlementSnapshot}
	f.must(f.db.Create(&e).Error)
	coverage := storage.EntitlementCoverage{ID: f.id(), EntitlementID: e.ID, SourceOrderID: order.ID, ValidFrom: start, ValidUntil: &end, ContractAnchor: start, Snapshot: f.version.EntitlementSnapshot}
	f.must(f.db.Create(&coverage).Error)
	return e, coverage
}
func (f *expiryFixture) testMail(n int) {
	f.t.Helper()
	for i := 0; i < n; i++ {
		_, err := f.s.TestExpiryReminder(context.Background(), f.admin, ExpiryReminderTestInput{To: "preview-" + f.id() + "@example.test", Locale: "zh-CN"})
		f.must(err)
	}
}
func (f *expiryFixture) process(n int) {
	f.t.Helper()
	for i := 0; i < n; i++ {
		_, err := f.s.ProcessMail(context.Background(), f.sender)
		f.must(err)
	}
}
func (f *expiryFixture) count(status string) int64 {
	f.t.Helper()
	var n int64
	f.must(f.db.Model(&storage.EmailOutbox{}).Where("status=?", status).Count(&n).Error)
	return n
}

func TestExpiryRemindersDatabase(t *testing.T) {
	f := newExpiryFixture(t)
	ctx := context.Background()
	t.Run("default_off_contact_blank_and_admin_only", func(t *testing.T) {
		v, err := f.s.ExpiryReminderSettings(ctx, f.admin)
		f.must(err)
		if v.Enabled || v.ContactEmail != "" {
			t.Fatal("new sites must not enable reminders or infer a merchant contact")
		}
		_, err = f.s.SaveExpiryReminderSettings(ctx, f.admin, ExpiryReminderSettingsInput{Enabled: true, DefaultSend: true, ReminderDays: []int{7}, Locale: "zh-CN"})
		var business *Error
		if !errors.As(err, &business) || business.Code != "REMINDER_QUOTA_REQUIRED" {
			t.Fatalf("must require an explicit site-wide mail quota: %v", err)
		}
		for _, role := range []string{"customer", "support", "operations"} {
			p := f.admin
			p.Role = role
			if _, err = f.s.SaveMailQuotas(ctx, p, MailQuotaInput{}); !errors.Is(err, ErrForbidden) {
				t.Fatal("non-administrator changed the site-wide mail quota")
			}
		}
		preview, err := f.s.PreviewExpiryReminder(ctx, f.admin, "zh-CN", "")
		f.must(err)
		if preview.ReplyTo != "" || strings.Contains(preview.Body, f.admin.User.Email) || strings.Contains(preview.Body, "sender@example.test") {
			t.Fatal("merchant login/sender leaked into the customer email")
		}
	})
	t.Run("concurrent_workers_share_one_reminder_budget", func(t *testing.T) {
		f.reset()
		f.quotas(MailPeriodQuota{Total: 10, Reserve: 5, Limit: 2})
		f.settings(true)
		for i := 0; i < 8; i++ {
			f.customer(now().Add(6 * 24 * time.Hour))
		}
		f.must(f.s.RunExpiryReminders(ctx))
		if f.count("pending") != 8 {
			t.Fatal("expected reminders for eight different customers")
		}
		other, err := New(ctx, f.db, f.config)
		f.must(err)
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				s := f.s
				if i%2 == 1 {
					s = other
				}
				_, err := s.ProcessMail(ctx, f.sender)
				errs <- err
			}(i)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			f.must(err)
		}
		if got := len(f.sender.snapshot()); got > 2 {
			t.Fatalf("two workers consumed separate budgets: got %d deliveries, maximum 2", got)
		}
		// SKIP LOCKED may leave a job for the next worker tick. Drain those
		// selections too, and prove they still cannot spend a separate budget.
		f.process(8)
		if len(f.sender.snapshot()) != 2 {
			t.Fatal("later worker ticks bypassed the shared reminder budget")
		}
		q, err := f.s.MailQuotas(ctx, f.admin)
		f.must(err)
		if q.Usage["hour"].ReminderUsed != 2 || q.Usage["hour"].TotalUsed != 2 {
			t.Fatalf("bad shared counters: %+v", q.Usage)
		}
		var waiting []storage.EmailOutbox
		f.must(f.db.Where("status='pending'").Find(&waiting).Error)
		if len(waiting) != 6 {
			t.Fatalf("expected 6 queued messages, got %d", len(waiting))
		}
		for _, row := range waiting {
			if row.Attempts != 0 || row.LastErrorCode == nil || *row.LastErrorCode != "MAIL_QUOTA_WAIT" {
				t.Fatal("waiting spent a retry attempt or was treated as a delivery failure")
			}
		}
	})
	t.Run("reserve_pool_stays_available_for_registration_and_password_reset", func(t *testing.T) {
		f.reset()
		f.quotas(MailPeriodQuota{Total: 10, Reserve: 4, Limit: 6})
		period := quotaPeriods(now())[2]
		f.must(f.db.Create(&storage.MailQuotaWindow{ID: period.id, EndsAt: period.end, TotalAttempts: 5}).Error)
		f.testMail(2)
		f.process(2)
		if len(f.sender.snapshot()) != 1 {
			t.Fatal("a reminder consumed the four-message protected pool")
		}
		registered := "registration-" + f.id() + "@example.test"
		f.must(f.s.Register(ctx, registered, "Customer Password 123!"))
		f.must(f.s.RequestEmail(ctx, f.admin.User.Email, "reset"))
		f.process(2)
		messages := f.sender.snapshot()
		if len(messages) != 3 {
			t.Fatalf("registration/reset did not send while reminders were paused: %d messages", len(messages))
		}
		if messages[1].To != registered || messages[2].To != f.admin.User.Email {
			t.Fatal("registration and password-reset mail did not take priority")
		}
		q, err := f.s.MailQuotas(ctx, f.admin)
		f.must(err)
		if q.Usage["hour"].TotalUsed != 8 || q.Usage["hour"].ReminderUsed != 1 {
			t.Fatalf("protected traffic was counted as reminder traffic: %+v", q.Usage["hour"])
		}
		f.process(2)
		if len(f.sender.snapshot()) != 3 {
			t.Fatal("deferred reminders stole the remaining protected capacity")
		}
	})
	t.Run("new_security_mail_precedes_old_reminders", func(t *testing.T) {
		f.reset()
		f.quotas(MailPeriodQuota{Total: 100, Reserve: 20, Limit: 50})
		f.testMail(3)
		f.must(f.db.Model(&storage.EmailOutbox{}).Where("priority=?", expiryMailPriority).Update("available_at", now().Add(-time.Hour)).Error)
		f.must(f.s.queueCustomerNotification(f.db, f.admin.User.ID, "Transaction receipt", "A payment was confirmed."))
		registered := "priority-" + f.id() + "@example.test"
		f.must(f.s.Register(ctx, registered, "Customer Password 123!"))
		f.must(f.s.RequestEmail(ctx, f.admin.User.Email, "reset"))
		f.process(2)
		messages := f.sender.snapshot()
		if len(messages) != 2 || messages[0].To != registered || messages[1].To != f.admin.User.Email {
			t.Fatal("old reminder backlog blocked newer registration/reset mail")
		}
		f.process(1)
		if len(f.sender.snapshot()) != 3 || f.sender.snapshot()[2].Subject != "Transaction receipt" {
			t.Fatal("transactional mail did not precede the reminder backlog")
		}
		f.process(1)
		if len(f.sender.snapshot()) != 4 {
			t.Fatal("reminders failed to resume after higher-priority mail")
		}
	})
	t.Run("changing_limits_keeps_usage_and_rejects_stale_versions", func(t *testing.T) {
		before, err := f.s.MailQuotas(ctx, f.admin)
		f.must(err)
		input := MailQuotaInput{MailQuotaPolicy: before.MailQuotaPolicy, Version: before.Version}
		input.Hour.Limit = 60
		after, err := f.s.SaveMailQuotas(ctx, f.admin, input)
		f.must(err)
		if after.Usage["hour"].TotalUsed != before.Usage["hour"].TotalUsed {
			t.Fatal("saving a mail quota reset its usage")
		}
		if _, err = f.s.SaveMailQuotas(ctx, f.admin, input); err == nil {
			t.Fatal("stale quota version overwrote the administrator's newer settings")
		}
	})
	t.Run("failed_reminder_attempts_are_budgeted_but_waiting_is_not_a_retry", func(t *testing.T) {
		f.reset()
		f.quotas(MailPeriodQuota{Total: 10, Reserve: 4, Limit: 2})
		f.testMail(1)
		f.sender.mu.Lock()
		f.sender.fail = true
		f.sender.mu.Unlock()
		f.process(1)
		f.must(f.db.Model(&storage.EmailOutbox{}).Where("kind='expiry_test'").Update("available_at", now().Add(-time.Second)).Error)
		f.process(1)
		f.must(f.db.Model(&storage.EmailOutbox{}).Where("kind='expiry_test'").Update("available_at", now().Add(-time.Second)).Error)
		f.process(1)
		var row storage.EmailOutbox
		f.must(f.db.First(&row, "kind='expiry_test'").Error)
		if row.Attempts != 2 || len(f.sender.snapshot()) != 2 || row.Status != "pending" {
			t.Fatalf("quota waiting consumed an SMTP attempt: %+v", row)
		}
	})
	t.Run("hourly_scan_deduplicates_and_respects_individual_preferences", func(t *testing.T) {
		f.reset()
		f.quotas(MailPeriodQuota{Total: 100, Reserve: 20, Limit: 50})
		f.settings(true)
		first, _ := f.customer(now().Add(6 * 24 * time.Hour))
		second, _ := f.customer(now().Add(6 * 24 * time.Hour))
		third, _ := f.customer(now().Add(6 * 24 * time.Hour))
		_, err := f.s.SaveExpiryReminderPreference(ctx, f.admin, second.ID, ExpiryReminderPreferenceInput{Mode: "on", Locale: "ja"})
		f.must(err)
		_, err = f.s.SaveExpiryReminderPreference(ctx, f.admin, third.ID, ExpiryReminderPreferenceInput{Mode: "off"})
		f.must(err)
		f.must(f.s.RunExpiryReminders(ctx))
		if f.count("pending") != 2 {
			t.Fatal("scanner ignored per-authorization preferences")
		}
		f.must(f.s.RunExpiryReminders(ctx))
		if f.count("pending") != 2 {
			t.Fatal("second process/startup repeated an hourly scan")
		}
		f.must(f.db.Model(&storage.ExpiryReminderSchedule{}).Where("id=1").Update("next_run_at", now().Add(-time.Second)).Error)
		other, err := New(ctx, f.db, f.config)
		f.must(err)
		var scanWorkers sync.WaitGroup
		scanErrors := make(chan error, 2)
		for _, instance := range []*Service{f.s, other} {
			scanWorkers.Add(1)
			go func(s *Service) { defer scanWorkers.Done(); scanErrors <- s.RunExpiryReminders(ctx) }(instance)
		}
		scanWorkers.Wait()
		close(scanErrors)
		for err := range scanErrors {
			f.must(err)
		}
		if f.count("pending") != 2 {
			t.Fatal("same expiry/day was queued twice")
		}
		f.process(2)
		messages := f.sender.snapshot()
		if len(messages) != 2 {
			t.Fatal("eligible customer reminders were not delivered")
		}
		var firstUser, secondUser storage.User
		f.must(f.db.First(&firstUser, "id=?", first.UserID).Error)
		f.must(f.db.First(&secondUser, "id=?", second.UserID).Error)
		for _, message := range messages {
			if !strings.Contains(message.Subject, "Atlas Pro") || !strings.Contains(message.Subject, "专业版设备授权") {
				t.Fatal("customer email did not identify its product and policy")
			}
			if message.To == secondUser.Email && !strings.Contains(message.Body, "有効期限") {
				t.Fatal("individual customer language was ignored")
			}
			if message.To != firstUser.Email && message.To != secondUser.Email {
				t.Fatal("email sent to an unrelated customer")
			}
		}
		// Once delivered, later hourly scans must not recreate the same window.
		for scan := 0; scan < 2; scan++ {
			f.must(f.db.Model(&storage.ExpiryReminderSchedule{}).Where("id=1").Update("next_run_at", now().Add(-time.Second)).Error)
			f.must(other.RunExpiryReminders(ctx))
			f.process(4)
			if len(f.sender.snapshot()) != 2 || f.count("pending") != 0 || f.count("sent") != 2 {
				t.Fatal("later hourly scan resent an already delivered reminder window")
			}
		}
	})
	t.Run("renewal_and_disabled_preferences_cancel_queued_messages", func(t *testing.T) {
		f.reset()
		f.settings(true)
		first, coverage := f.customer(now().Add(6 * 24 * time.Hour))
		second, _ := f.customer(now().Add(6 * 24 * time.Hour))
		f.must(f.s.RunExpiryReminders(ctx))
		if f.count("pending") != 2 {
			t.Fatal("setup did not queue reminders")
		}
		newEnd := now().Add(36 * 24 * time.Hour)
		f.must(f.db.Model(&storage.EntitlementCoverage{}).Where("id=?", coverage.ID).Update("valid_until", newEnd).Error)
		f.must(f.db.Model(&storage.Entitlement{}).Where("id=?", first.ID).Update("valid_until", newEnd).Error)
		_, err := f.s.SaveExpiryReminderPreference(ctx, f.admin, second.ID, ExpiryReminderPreferenceInput{Mode: "off"})
		f.must(err)
		f.process(4)
		if len(f.sender.snapshot()) != 0 || f.count("cancelled") != 2 {
			t.Fatal("renewed or opted-out customers received stale reminders")
		}
	})
	t.Run("daily_limit_also_protects_other_mail", func(t *testing.T) {
		f.reset()
		f.quotas(MailPeriodQuota{Total: 100, Reserve: 20, Limit: 50})
		v, err := f.s.MailQuotas(ctx, f.admin)
		f.must(err)
		v.Day = MailPeriodQuota{Total: 10, Reserve: 4, Limit: 1}
		_, err = f.s.SaveMailQuotas(ctx, f.admin, MailQuotaInput{MailQuotaPolicy: v.MailQuotaPolicy, Version: v.Version})
		f.must(err)
		f.testMail(2)
		f.process(2)
		if len(f.sender.snapshot()) != 1 {
			t.Fatal("hourly allowance bypassed the site-wide daily limit")
		}
		registered := "daily-" + f.id() + "@example.test"
		f.must(f.s.Register(ctx, registered, "Customer Password 123!"))
		f.process(1)
		if len(f.sender.snapshot()) != 2 || f.sender.snapshot()[1].To != registered {
			t.Fatal("daily reminder exhaustion blocked registration")
		}
	})
	t.Run("monthly_reserve_is_shared_even_when_hour_and_day_allow_more", func(t *testing.T) {
		f.reset()
		f.quotas(MailPeriodQuota{Total: 100, Reserve: 20, Limit: 50})
		v, err := f.s.MailQuotas(ctx, f.admin)
		f.must(err)
		v.Month = MailPeriodQuota{Total: 10, Reserve: 4, Limit: 6}
		_, err = f.s.SaveMailQuotas(ctx, f.admin, MailQuotaInput{MailQuotaPolicy: v.MailQuotaPolicy, Version: v.Version})
		f.must(err)
		period := quotaPeriods(now())[0]
		f.must(f.db.Create(&storage.MailQuotaWindow{ID: period.id, TotalAttempts: 5, EndsAt: period.end}).Error)
		f.testMail(3)
		f.process(3)
		if len(f.sender.snapshot()) != 1 {
			t.Fatal("month-level protected pool was consumed by reminders")
		}
		f.must(f.s.RequestEmail(ctx, f.admin.User.Email, "reset"))
		f.process(1)
		if len(f.sender.snapshot()) != 2 || f.sender.snapshot()[1].To != f.admin.User.Email {
			t.Fatal("monthly reminder protection blocked password recovery")
		}
	})
	t.Run("connection_tests_also_consume_the_shared_total", func(t *testing.T) {
		f.reset()
		f.quotas(MailPeriodQuota{Total: 3, Reserve: 1, Limit: 2})
		for i := 0; i < 3; i++ {
			_, err := f.s.CheckMail(ctx, f.admin, MailCheckInput{To: "smtp-check@example.test"})
			f.must(err)
		}
		_, err := f.s.CheckMail(ctx, f.admin, MailCheckInput{To: "smtp-check@example.test"})
		var business *Error
		if !errors.As(err, &business) || business.Code != "MAIL_QUOTA_WAIT" || len(f.sender.snapshot()) != 3 {
			t.Fatal("SMTP connection check bypassed the site-wide total")
		}
		q, err := f.s.MailQuotas(ctx, f.admin)
		f.must(err)
		if q.Usage["hour"].TotalUsed != 3 || q.Usage["hour"].ReminderUsed != 0 {
			t.Fatal("manual SMTP checks were not recorded correctly")
		}
	})
}
