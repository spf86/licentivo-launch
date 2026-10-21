package httpapi_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/go-sql-driver/mysql"
	"licentivo-launch/internal/app"
	"licentivo-launch/internal/httpapi"
	"licentivo-launch/internal/storage"
)

func TestInitialAdministratorMustChangePasswordBeforeAdministration(t *testing.T) {
	dsn := os.Getenv("LAUNCH_TEST_BOOTSTRAP_DATABASE_DSN")
	if dsn == "" {
		t.Skip("set LAUNCH_TEST_BOOTSTRAP_DATABASE_DSN to a fresh launch_test_* database")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(cfg.DBName, "launch_test_") {
		t.Fatal("bootstrap tests require an isolated launch_test_* database")
	}
	db, err := storage.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close(db)
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE()").Scan(&count).Error; err != nil || count != 0 {
		t.Fatal("bootstrap tests require a fresh database")
	}
	ctx := context.Background()
	if err := storage.Initialize(ctx, db); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{19}, 32)
	service, err := app.New(ctx, db, app.Config{PublicURL: "https://merchant.example", MasterKey: key, UploadDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if created, err := service.EnsureInitialAdministrator(ctx, "not-an-email", "tiny"); err == nil || created {
		t.Fatal("invalid initial credentials accepted")
	}
	var wg sync.WaitGroup
	results, failures := make(chan bool, 2), make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			created, err := service.EnsureInitialAdministrator(ctx, "", "")
			results <- created
			failures <- err
		})
	}
	wg.Wait()
	createdCount := 0
	for range 2 {
		if err := <-failures; err != nil {
			t.Fatal(err)
		}
		if <-results {
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Fatalf("expected one initial account, got %d", createdCount)
	}
	if err := db.Model(&storage.User{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("initial account duplicated")
	}
	a := &testApp{t, httpapi.Handler(httpapi.Options{App: service, PublicURL: "https://merchant.example"}), db, key}
	admin := &browser{}
	login := a.request("POST", "/api/v1/auth/login", map[string]string{"email": app.DefaultAdminEmail, "password": app.DefaultAdminPassword, "kind": "website"}, admin)
	a.expect(login, 200)
	grant := data[app.SessionGrant](t, login)
	if !grant.User.PasswordChangeRequired || grant.User.FullAccess {
		t.Fatal("initial account bypassed the password-change requirement")
	}
	if strings.Contains(login.Body.String(), app.DefaultAdminPassword) {
		t.Fatal("login response exposed the password")
	}
	a.expect(a.request("GET", "/api/v1/me", nil, admin), 200)
	blocked := a.request("GET", "/api/v1/admin/dashboard", nil, admin)
	a.expect(blocked, 403)
	if !strings.Contains(blocked.Body.String(), "PASSWORD_CHANGE_REQUIRED") {
		t.Fatal("administration not gated on first password change")
	}
	a.expect(a.request("GET", "/api/v1/me/sessions", nil, admin), 403)
	if _, err := service.Login(ctx, app.DefaultAdminEmail, app.DefaultAdminPassword, "desktop"); err == nil {
		t.Fatal("initial password yielded a desktop session")
	}
	a.expect(a.request("POST", "/api/v1/me/password", map[string]string{"current_password": app.DefaultAdminPassword, "new_password": app.DefaultAdminPassword}, admin), 400)
	a.expect(a.request("POST", "/api/v1/me/password", map[string]string{"current_password": app.DefaultAdminPassword, "new_password": "short"}, admin), 400)
	const replacement = "changed-administrator-password-for-tests"
	a.expect(a.request("POST", "/api/v1/me/password", map[string]string{"current_password": app.DefaultAdminPassword, "new_password": replacement}, admin), 200)
	a.expect(a.request("GET", "/api/v1/me", nil, admin), 401)
	if _, err := service.Login(ctx, app.DefaultAdminEmail, app.DefaultAdminPassword, "website"); err == nil {
		t.Fatal("initial password remained valid")
	}
	login = a.request("POST", "/api/v1/auth/login", map[string]string{"email": app.DefaultAdminEmail, "password": replacement}, admin)
	a.expect(login, 200)
	grant = data[app.SessionGrant](t, login)
	if grant.User.PasswordChangeRequired || !grant.User.FullAccess {
		t.Fatal("changed password did not unlock administration")
	}
	a.expect(a.request("GET", "/api/v1/admin/dashboard", nil, admin), 200)
	if created, err := service.EnsureInitialAdministrator(ctx, "another@example.com", "replacement-password-for-tests"); err != nil || created {
		t.Fatal("restart replaced an installed administrator")
	}
	if _, err := service.Login(ctx, app.DefaultAdminEmail, replacement, "website"); err != nil {
		t.Fatal("restart reset the administrator password")
	}
	a.expect(a.request("POST", "/api/v1/setup", app.BootstrapInput{}, nil), 410)
}
