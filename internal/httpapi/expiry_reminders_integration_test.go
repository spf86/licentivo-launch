package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"licentivo-launch/internal/app"
	"licentivo-launch/internal/httpapi"
	"licentivo-launch/internal/security"
	"licentivo-launch/internal/storage"
)

func TestExpiryReminderHTTPPermissionsAndQuotaContract(t *testing.T) {
	dsn := os.Getenv("LAUNCH_TEST_EXPIRY_DSN")
	if dsn == "" {
		t.Skip("set LAUNCH_TEST_EXPIRY_DSN to the isolated MySQL runtime")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(cfg.DBName, "launch_test_expiry_") {
		t.Fatal("isolated launch_test_expiry_* database required")
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	newID := func() string { v, err := security.ID(); must(err); return v }
	control, err := storage.Open(dsn)
	must(err)
	cfg.DBName = "launch_test_expiry_http_" + strings.ReplaceAll(newID(), "-", "")
	must(control.Exec("CREATE DATABASE `" + cfg.DBName + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_bin").Error)
	must(storage.Close(control))
	db, err := storage.Open(cfg.FormatDSN())
	must(err)
	defer storage.Close(db)
	must(storage.Initialize(context.Background(), db))
	recorder := &recordingMailSender{}
	config := app.Config{PublicURL: "https://merchant.example", MasterKey: bytes.Repeat([]byte{7}, 32), SetupToken: strings.Repeat("h", 43), UploadDir: t.TempDir(), MailFactory: recorder.factory}
	service, err := app.New(context.Background(), db, config)
	must(err)
	a := &testApp{t: t, db: db, key: config.MasterKey, handler: httpapi.Handler(httpapi.Options{App: service, PublicURL: config.PublicURL, SecureCookies: true})}
	a.expect(a.request("POST", "/api/v1/setup", map[string]string{"setup_token": config.SetupToken, "email": "admin@example.test", "password": "Expiry HTTP Test 2026!", "brand_name": "HTTP Test"}, nil), 200)
	admin := &browser{}
	a.expect(a.request("POST", "/api/v1/auth/login", map[string]string{"email": "admin@example.test", "password": "Expiry HTTP Test 2026!"}, admin), 200)
	var owner storage.User
	must(db.First(&owner, "email=?", "admin@example.test").Error)
	for _, role := range []string{"customer", "support", "operations"} {
		at := time.Now().UTC()
		u := storage.User{ID: newID(), Email: role + "@example.test", EmailKey: role + "@example.test", Status: "active", PasswordHash: owner.PasswordHash, EmailVerifiedAt: &at}
		must(db.Create(&u).Error)
		if role != "customer" {
			must(db.Create(&storage.StaffAccount{UserID: u.ID, Role: role, Permissions: json.RawMessage(`[]`)}).Error)
		}
		actor := &browser{}
		a.expect(a.request("POST", "/api/v1/auth/login", map[string]string{"email": u.Email, "password": "Expiry HTTP Test 2026!"}, actor), 200)
		for _, path := range []string{"/admin/mail/quotas", "/admin/expiry-reminders/settings", "/admin/expiry-reminders/preview", "/admin/expiry-reminders/jobs"} {
			a.expect(a.request("GET", "/api/v1"+path, nil, actor), 403)
		}
		for _, path := range []string{"/admin/mail/quotas", "/admin/expiry-reminders/settings", "/admin/expiry-reminders/test"} {
			a.expect(a.request("POST", "/api/v1"+path, map[string]any{}, actor), 403)
		}
	}
	a.expect(a.request("GET", "/api/v1/admin/mail/quotas", nil, nil), 401)
	q := app.MailQuotaInput{MailQuotaPolicy: app.MailQuotaPolicy{Hour: app.MailPeriodQuota{Total: 10, Reserve: 4, Limit: 6}, Day: app.MailPeriodQuota{Total: 100, Reserve: 20, Limit: 60}, Month: app.MailPeriodQuota{Total: 1000, Reserve: 200, Limit: 600}}}
	a.expect(a.request("POST", "/api/v1/admin/mail/quotas", q, admin, func(r *http.Request) { r.Header.Del("X-CSRF-Token") }), 403)
	a.expect(a.request("POST", "/api/v1/admin/mail/quotas", q, admin, func(r *http.Request) { r.Header.Set("Origin", "https://other.example") }), 403)
	saved := data[app.MailQuotaView](t, a.request("POST", "/api/v1/admin/mail/quotas", q, admin))
	if !saved.Configured || saved.Available != 6 {
		t.Fatal("quota API did not expose its actual shared allowance")
	}
	a.expect(a.request("POST", "/api/v1/admin/mail/quotas", q, admin), 409)
	q.Version = saved.Version
	q.Hour.Limit = 7
	a.expect(a.request("POST", "/api/v1/admin/mail/quotas", q, admin), 400)
	_, err = service.SaveMailSettings(context.Background(), app.Principal{User: owner, Role: "admin"}, app.MailSettingsInput{Provider: "smtp", Host: "smtp.example.test", Port: "587", Encryption: "starttls", Username: "isolated", APIKey: "isolated-password", From: "sender@example.test"})
	must(err)
	input := app.ExpiryReminderSettingsInput{Enabled: true, DefaultSend: true, ReminderDays: []int{7, 3, 1}, Locale: "zh-CN"}
	reminders := data[app.ExpiryReminderSettingsView](t, a.request("POST", "/api/v1/admin/expiry-reminders/settings", input, admin))
	if reminders.ScanIntervalSeconds != 3600 || reminders.ContactEmail != "" {
		t.Fatal("reminder defaults or scan interval are incorrect")
	}
	a.expect(a.request("GET", "/api/v1/admin/expiry-reminders/preview?locale=unsupported", nil, admin), 400)
	a.expect(a.request("GET", "/api/v1/admin/expiry-reminders/jobs?limit=1000", nil, admin), 400)
	a.expect(a.request("POST", "/api/v1/admin/expiry-reminders/settings", map[string]any{"quota": 100}, admin), 400)
	result := data[map[string]string](t, a.request("POST", "/api/v1/admin/expiry-reminders/test", app.ExpiryReminderTestInput{To: "preview@example.test", Locale: "ja"}, admin))
	if result["id"] == "" {
		t.Fatal("test message was not queued")
	}
	_, err = service.ProcessMail(context.Background(), recorder)
	must(err)
	used := data[app.MailQuotaView](t, a.request("GET", "/api/v1/admin/mail/quotas", nil, admin))
	if used.Usage["hour"].TotalUsed != 1 || used.Usage["hour"].ReminderUsed != 1 || used.Available != 5 {
		t.Fatal("HTTP test delivery bypassed global reminder accounting")
	}
	history := a.request("GET", "/api/v1/admin/expiry-reminders/jobs", nil, admin)
	a.expect(history, 200)
	for _, secret := range []string{"ciphertext", "notification_key", "lease_token", "api_key", "isolated-password"} {
		if strings.Contains(history.Body.String(), secret) {
			t.Fatal("reminder history exposed internal secrets")
		}
	}
	if !strings.Contains(history.Body.String(), "専門") && !strings.Contains(history.Body.String(), `"locale":"ja"`) {
		t.Fatal("message metadata lost the selected language")
	}
}
