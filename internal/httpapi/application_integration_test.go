package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"licentivo-launch/internal/app"
	"licentivo-launch/internal/httpapi"
	"licentivo-launch/internal/mailer"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/security"
	"licentivo-launch/internal/storage"
)

type browser struct{ cookies map[string]*http.Cookie }
type testApp struct {
	t       *testing.T
	handler http.Handler
	db      *gorm.DB
	key     []byte
}

type fakeTranslator struct{}

type recordingMailSender struct {
	mu       sync.Mutex
	configs  []mailer.Config
	messages []mailer.Message
}

func (s *recordingMailSender) factory(config mailer.Config) (mailer.Sender, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configs = append(s.configs, config)
	return s, nil
}

func (s *recordingMailSender) Send(_ context.Context, message mailer.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, message)
	return nil
}

func (fakeTranslator) Usage(context.Context) (ports.TranslationUsage, error) {
	return ports.TranslationUsage{CharacterCount: 123, CharacterLimit: 500000}, nil
}

func (fakeTranslator) Translate(_ context.Context, _ string, target string, texts []string) ([]string, error) {
	result := make([]string, len(texts))
	for i, value := range texts {
		result[i] = target + " · " + value
	}
	return result, nil
}

func (a *testApp) request(method, path string, body any, actor *browser, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	a.t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	r.RemoteAddr = "192.0.2.10:4000"
	if method != "GET" {
		r.Header.Set("Origin", "https://merchant.example")
		r.Header.Set("Content-Type", "application/json")
	}
	if actor != nil {
		for _, cookie := range actor.cookies {
			r.AddCookie(cookie)
		}
		if csrf := actor.cookies["launch_csrf"]; csrf != nil {
			r.Header.Set("X-CSRF-Token", csrf.Value)
		}
	}
	for _, fn := range mutate {
		fn(r)
	}
	w := httptest.NewRecorder()
	a.handler.ServeHTTP(w, r)
	if actor != nil {
		if actor.cookies == nil {
			actor.cookies = map[string]*http.Cookie{}
		}
		for _, cookie := range w.Result().Cookies() {
			actor.cookies[cookie.Name] = cookie
		}
	}
	return w
}
func (a *testApp) upload(path, version, fileName string, content []byte, actor *browser) *httptest.ResponseRecorder {
	a.t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("version", version)
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		a.t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		a.t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		a.t.Fatal(err)
	}
	r := httptest.NewRequest("POST", path, &body)
	r.RemoteAddr = "192.0.2.10:4000"
	r.Header.Set("Origin", "https://merchant.example")
	r.Header.Set("Content-Type", writer.FormDataContentType())
	for _, cookie := range actor.cookies {
		r.AddCookie(cookie)
	}
	if csrf := actor.cookies["launch_csrf"]; csrf != nil {
		r.Header.Set("X-CSRF-Token", csrf.Value)
	}
	w := httptest.NewRecorder()
	a.handler.ServeHTTP(w, r)
	return w
}
func (a *testApp) expect(w *httptest.ResponseRecorder, status int) {
	a.t.Helper()
	if w.Code != status {
		a.t.Fatalf("HTTP %d, want %d: %s", w.Code, status, w.Body.String())
	}
}
func data[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	if w.Code < 200 || w.Code >= 300 {
		t.Fatalf("expected successful JSON response, HTTP %d: %s", w.Code, w.Body.String())
	}
	var result struct {
		Data T `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatal("invalid JSON response")
	}
	return result.Data
}
func (a *testApp) mailToken(kind, address string) string {
	a.t.Helper()
	var rows []storage.EmailOutbox
	if a.db.Order("created_at DESC,id DESC").Find(&rows).Error != nil {
		a.t.Fatal("outbox read failed")
	}
	vault, _ := security.NewVault(a.key)
	for _, row := range rows {
		plain, err := vault.Open(row.Ciphertext, "email-outbox:"+row.ID)
		if err != nil {
			a.t.Fatal(err)
		}
		var message mailer.Message
		_ = json.Unmarshal(plain, &message)
		if message.To != address {
			continue
		}
		for _, line := range strings.Split(message.Body, "\r\n") {
			u, err := url.Parse(line)
			if err != nil || u.Host != "merchant.example" {
				continue
			}
			params, _ := url.ParseQuery(u.Fragment)
			if params.Get("action") == kind {
				return params.Get("token")
			}
		}
	}
	a.t.Fatal("expected encrypted email not found")
	return ""
}

func TestApplicationFlows(t *testing.T) {
	dsn := os.Getenv("LAUNCH_TEST_APP_DATABASE_DSN")
	if dsn == "" {
		t.Skip("set LAUNCH_TEST_APP_DATABASE_DSN to a fresh launch_test_* database")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(cfg.DBName, "launch_test_") {
		t.Fatal("test database must be launch_test_*")
	}
	db, err := storage.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close(db)
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE()").Scan(&count).Error; err != nil || count != 0 {
		t.Fatal("application test requires a fresh database")
	}
	if err := storage.Initialize(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{9}, 32)
	setupToken := strings.Repeat("s", 43)
	mailRecorder := &recordingMailSender{}
	appConfig := app.Config{PublicURL: "https://merchant.example", MasterKey: key, SetupToken: setupToken, UploadDir: t.TempDir(), MailFactory: mailRecorder.factory, TranslationFactory: func(string, string) (ports.Translator, error) { return fakeTranslator{}, nil }}
	newHandler := func() http.Handler {
		svc, err := app.New(context.Background(), db, appConfig)
		if err != nil {
			t.Fatal(err)
		}
		return httpapi.Handler(httpapi.Options{App: svc, PublicURL: "https://merchant.example", SecureCookies: true})
	}
	a := &testApp{t, newHandler(), db, key}
	admin := &browser{}
	customer := &browser{}
	bootstrap := map[string]any{"setup_token": setupToken, "email": "owner@example.invalid", "password": "owner-password-for-tests", "brand_name": "Test Merchant"}
	bad := map[string]any{"setup_token": "invalid", "email": "owner@example.invalid", "password": "owner-password-for-tests", "brand_name": "Test Merchant"}
	a.expect(a.request("POST", "/api/v1/setup", bad, nil), 403)
	a.expect(a.request("POST", "/api/v1/setup", bootstrap, nil), 200)
	a.expect(a.request("POST", "/api/v1/setup", bootstrap, nil), 409)
	var administrator storage.StaffAccount
	if a.db.First(&administrator, "role = ?", "admin").Error != nil {
		t.Fatal("administrator role was not created")
	}
	var administratorPermissions []string
	if json.Unmarshal(administrator.Permissions, &administratorPermissions) != nil || len(administratorPermissions) != 1 || administratorPermissions[0] != "*" {
		t.Fatal("administrator does not have the complete permission scope")
	}
	a.expect(a.request("POST", "/api/v1/auth/login", map[string]string{"email": "owner@example.invalid", "password": "owner-password-for-tests"}, admin), 200)
	if cookie := admin.cookies["launch_session"]; cookie == nil || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatal("session cookie protections missing")
	}
	adminMe := data[map[string]json.RawMessage](t, a.request("GET", "/api/v1/me", nil, admin))
	var adminSession string
	_ = json.Unmarshal(adminMe["session_id"], &adminSession)
	var adminView app.UserView
	_ = json.Unmarshal(adminMe["user"], &adminView)
	if adminView.Role != "admin" || !adminView.FullAccess {
		t.Fatal("administrator session does not report full access")
	}
	brand := app.Brand{Name: "Updated Brand", SupportEmail: "support@example.invalid"}
	a.expect(a.request("PUT", "/api/v1/admin/brand", brand, admin, func(r *http.Request) { r.Header.Del("X-CSRF-Token") }), 403)
	a.expect(a.request("PUT", "/api/v1/admin/brand", brand, admin, func(r *http.Request) { r.Header.Set("Origin", "https://attacker.invalid") }), 403)
	a.expect(a.request("PUT", "/api/v1/admin/brand", brand, admin), 200)
	product := app.ProductView{Name: "Test Editor", Slug: "test-editor", Status: "published", Content: app.ProductContent{Summary: "A product", Features: []string{"Export"}, DownloadURL: "https://example.invalid/download"}}
	productResponse := a.request("PUT", "/api/v1/admin/product", product, admin)
	a.expect(productResponse, 200)
	savedProduct := data[app.ProductView](t, productResponse)
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	a.expect(a.upload("/api/v1/admin/products/"+savedProduct.ID+"/document-images", "", "manual.svg", png, admin), 400)
	documentImageResponse := a.upload("/api/v1/admin/products/"+savedProduct.ID+"/document-images", "", "installation.png", png, admin)
	a.expect(documentImageResponse, 200)
	documentImage := data[app.ProductDocumentImageView](t, documentImageResponse)
	if documentImage.ContentType != "image/png" || documentImage.SizeBytes != uint64(len(png)) || !strings.HasPrefix(documentImage.URL, "/api/v1/product-document-images/") {
		t.Fatalf("uploaded document image not returned correctly: %+v", documentImage)
	}
	servedImage := a.request("GET", documentImage.URL, nil, nil)
	a.expect(servedImage, 200)
	if !bytes.Equal(servedImage.Body.Bytes(), png) || !strings.Contains(servedImage.Header().Get("Content-Disposition"), "inline") || servedImage.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("document image was not served inline with safe headers")
	}
	a.expect(a.upload("/api/v1/admin/products/"+savedProduct.ID+"/downloads/windows", "2.4.1", "editor.txt", []byte("wrong type"), admin), 400)
	installer := []byte("MZ test installer payload")
	uploadResponse := a.upload("/api/v1/admin/products/"+savedProduct.ID+"/downloads/windows", "2.4.1", "Test Editor Setup.exe", installer, admin)
	a.expect(uploadResponse, 200)
	windowsDownload := data[app.ProductDownloadView](t, uploadResponse)
	expectedInstallerHash := sha256.Sum256(installer)
	if windowsDownload.Platform != "windows" || windowsDownload.Source != "upload" || windowsDownload.Version != "2.4.1" || windowsDownload.SizeBytes != uint64(len(installer)) || windowsDownload.SHA256 != hex.EncodeToString(expectedInstallerHash[:]) {
		t.Fatalf("uploaded Windows build not returned correctly: %+v", windowsDownload)
	}
	androidResponse := a.request("PUT", "/api/v1/admin/products/"+savedProduct.ID+"/downloads/android", app.ProductDownloadInput{Source: "external", Version: "2.4.0", URL: "https://downloads.example.invalid/editor.apk"}, admin)
	a.expect(androidResponse, 200)
	catalogResponse := a.request("GET", "/api/v1/admin/catalog", nil, admin)
	a.expect(catalogResponse, 200)
	catalog := data[map[string]json.RawMessage](t, catalogResponse)
	var catalogProducts []app.ProductView
	if json.Unmarshal(catalog["products"], &catalogProducts) != nil || len(catalogProducts) != 1 || len(catalogProducts[0].Downloads) != 2 {
		t.Fatalf("platform builds missing from catalog: %+v", catalogProducts)
	}
	downloadResponse := a.request("GET", windowsDownload.URL, nil, nil)
	a.expect(downloadResponse, 200)
	if !bytes.Equal(downloadResponse.Body.Bytes(), installer) || !strings.Contains(downloadResponse.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("uploaded installer was not served as an attachment")
	}
	unit, period := "month", uint64(1)
	planInput := app.PlanView{Name: "Pro", Summary: "适合专业用户", Highlights: []string{"支持批量导出", "优先更新"}, Currency: "USD", AmountMinor: 1299, PurchaseType: "fixed_term", TermUnit: &unit, TermCount: &period, PurchaseOptions: []app.PurchaseOption{{Periods: 1, AmountMinor: 1299, Enabled: true}, {Periods: 3, AmountMinor: 3499, Enabled: true}, {Periods: 6, AmountMinor: 6499, Enabled: true}, {Periods: 12, AmountMinor: 11999, Enabled: true}}, AllowPurchase: true, AllowRenewal: true}
	w := a.request("POST", "/api/v1/admin/plans", planInput, admin)
	a.expect(w, 200)
	plan := data[app.PlanView](t, w)
	if len(plan.PurchaseOptions) != 4 || plan.PurchaseOptions[0].Periods != 1 || plan.PurchaseOptions[1].Periods != 3 || plan.PurchaseOptions[1].AmountMinor != 3499 || plan.PurchaseOptions[2].Periods != 6 || plan.PurchaseOptions[3].Periods != 12 {
		t.Fatalf("monthly purchase options were not created: %+v", plan.PurchaseOptions)
	}
	a.expect(a.request("POST", "/api/v1/admin/plans/"+plan.ID+"/publish", map[string]string{"version_id": plan.VersionID, "environment": "sandbox"}, admin), 200)
	emptyMailView := data[app.MailSettingsView](t, a.request("GET", "/api/v1/admin/mail/settings", nil, admin))
	if emptyMailView.Configured || emptyMailView.Provider != "resend" || emptyMailView.Host != "smtp.resend.com" || emptyMailView.Port != "465" || emptyMailView.Username != "resend" || emptyMailView.Encryption != "implicit_tls" {
		t.Fatalf("fixed Resend settings missing: %+v", emptyMailView)
	}
	a.expect(a.request("POST", "/api/v1/admin/mail/check", map[string]any{}, admin), 409)
	a.expect(a.request("POST", "/api/v1/admin/mail/settings", app.MailSettingsInput{Provider: "resend", From: "mail@example.invalid", APIKey: "re_test_key_for_launch", Port: "465", Encryption: "starttls"}, admin), 400)
	mailInput := app.MailSettingsInput{Provider: "resend", From: "mail@example.invalid", APIKey: "re_test_key_for_launch", Host: "smtp-alt.example.invalid", Port: "587", Username: "merchant-resend", Encryption: "starttls"}
	a.expect(a.request("POST", "/api/v1/admin/mail/settings", mailInput, admin), 200)
	mailView := data[app.MailSettingsView](t, a.request("GET", "/api/v1/admin/mail/settings", nil, admin))
	if !mailView.Configured || !mailView.CredentialStored || mailView.Provider != "resend" || mailView.From != mailInput.From || mailView.Host != mailInput.Host || mailView.Port != "587" || mailView.Username != mailInput.Username || mailView.Encryption != "starttls" {
		t.Fatalf("Resend settings were not returned safely: %+v", mailView)
	}
	mailCheck := data[app.MailCheckResult](t, a.request("POST", "/api/v1/admin/mail/check", app.MailCheckInput{To: "deliverability@example.invalid"}, admin))
	if mailCheck.SentTo != "deliverability@example.invalid" {
		t.Fatalf("test mail was sent to the wrong administrator: %+v", mailCheck)
	}
	mailRecorder.mu.Lock()
	if len(mailRecorder.configs) != 1 || len(mailRecorder.messages) != 1 || mailRecorder.configs[0].Host != mailInput.Host || mailRecorder.configs[0].Port != "587" || mailRecorder.configs[0].Username != mailInput.Username || mailRecorder.configs[0].ImplicitTLS || mailRecorder.configs[0].Password != mailInput.APIKey || mailRecorder.messages[0].To != "deliverability@example.invalid" || !strings.Contains(mailRecorder.messages[0].Subject, "mail delivery test") || !strings.HasPrefix(mailRecorder.messages[0].IdempotencyKey, "launch-mail-check/") {
		mailRecorder.mu.Unlock()
		t.Fatal("Resend connection check did not use the selected secure configuration")
	}
	mailRecorder.mu.Unlock()
	a.expect(a.request("GET", "/api/v1/admin/mail/settings", nil, customer), 401)
	a.expect(a.request("POST", "/api/v1/admin/mail/check", map[string]any{}, customer), 401)
	var savedMail storage.MailSettings
	var mailSecret storage.SecretRecord
	if db.First(&savedMail, "id = ?", 1).Error != nil || db.First(&mailSecret, "id = ?", savedMail.CredentialSecretID).Error != nil || bytes.Contains(mailSecret.Ciphertext, []byte(mailInput.APIKey)) {
		t.Fatal("Resend credential was not encrypted")
	}
	translationInput := app.TranslationSettingsInput{Edition: "free", SourceLocale: "zh-CN", APIKey: "test-deepl-key"}
	a.expect(a.request("POST", "/api/v1/admin/translations", translationInput, admin), 200)
	a.expect(a.request("GET", "/api/v1/admin/translations", nil, customer), 401)
	worker, err := app.New(context.Background(), db, appConfig)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		handled, processErr := worker.ProcessTranslation(context.Background())
		if processErr != nil {
			t.Fatal(processErr)
		}
		if !handled {
			break
		}
	}
	translationStatus := data[app.TranslationStatus](t, a.request("GET", "/api/v1/admin/translations", nil, admin))
	if !translationStatus.Configured || translationStatus.Total < 3 || translationStatus.Completed != translationStatus.Total || len(translationStatus.Supported) != 10 {
		t.Fatalf("translation queue did not complete: %+v", translationStatus)
	}
	var translationSettings storage.TranslationSettings
	var translationSecret storage.SecretRecord
	if db.First(&translationSettings, "id = ?", 1).Error != nil || db.First(&translationSecret, "id = ?", translationSettings.CredentialSecretID).Error != nil || bytes.Contains(translationSecret.Ciphertext, []byte("test-deepl-key")) {
		t.Fatal("DeepL credential was not encrypted")
	}
	english := data[app.SiteView](t, a.request("GET", "/api/v1/site?environment=sandbox&locale=en", nil, nil))
	if !english.TranslationReady || english.Locale != "en" || !strings.HasPrefix(english.Brand.Name, "EN-US · ") || english.Product == nil || !strings.HasPrefix(english.Product.Name, "EN-US · ") || len(english.Plans) != 1 || !strings.HasPrefix(english.Plans[0].Name, "EN-US · ") || !strings.HasPrefix(english.Plans[0].Summary, "EN-US · ") || len(english.Plans[0].Highlights) != 2 || !strings.HasPrefix(english.Plans[0].Highlights[0], "EN-US · ") || english.UI["nav.products"] != "Products" {
		t.Fatalf("localized catalog was not served: %+v", english)
	}
	sandbox := data[app.SiteView](t, a.request("GET", "/api/v1/site?environment=sandbox", nil, nil))
	if len(sandbox.Plans) != 1 || sandbox.Plans[0].AmountMinor != 1299 || !sandbox.Plans[0].PreviewOnly {
		t.Fatal("sandbox publication failed")
	}
	live := data[app.SiteView](t, a.request("GET", "/api/v1/site", nil, nil))
	if len(live.Plans) != 0 {
		t.Fatal("preview leaked into live catalog")
	}
	plan.AmountMinor = 1999
	w = a.request("POST", "/api/v1/admin/plans", plan, admin)
	a.expect(w, 200)
	next := data[app.PlanView](t, w)
	for i := 0; i < 20; i++ {
		handled, processErr := worker.ProcessTranslation(context.Background())
		if processErr != nil {
			t.Fatal(processErr)
		}
		if !handled {
			break
		}
	}
	sandbox = data[app.SiteView](t, a.request("GET", "/api/v1/site?environment=sandbox", nil, nil))
	if sandbox.Plans[0].AmountMinor != 1299 {
		t.Fatal("draft overwrote published price")
	}
	versions := data[[]app.PlanVersionHistory](t, a.request("GET", "/api/v1/admin/plans/"+plan.ID+"/versions", nil, admin))
	if len(versions) != 2 || versions[0].Version != 2 || versions[1].Version != 1 || versions[1].AmountMinor != 1299 {
		t.Fatalf("plan version history was incomplete: %+v", versions)
	}
	a.expect(a.request("POST", "/api/v1/admin/plans/"+plan.ID+"/rollback", map[string]string{"version_id": plan.VersionID, "environment": "sandbox"}, admin), 200)
	sandbox = data[app.SiteView](t, a.request("GET", "/api/v1/site?environment=sandbox", nil, nil))
	if len(sandbox.Plans) != 1 || sandbox.Plans[0].VersionID != plan.VersionID || sandbox.Plans[0].AmountMinor != 1299 {
		t.Fatal("sandbox rollback did not reactivate the historical immutable version")
	}
	a.expect(a.request("POST", "/api/v1/admin/plans", plan, admin), 409)
	a.expect(a.request("POST", "/api/v1/admin/plans/"+next.ID+"/publish", map[string]string{"version_id": next.VersionID, "environment": "live"}, admin), 409)
	register := map[string]string{"email": "customer@example.invalid", "password": "customer-password-for-tests"}
	w = a.request("POST", "/api/v1/auth/register", register, nil)
	a.expect(w, 200)
	firstResponse := data[map[string]any](t, w)
	w = a.request("POST", "/api/v1/auth/register", register, nil)
	a.expect(w, 200)
	secondResponse := data[map[string]any](t, w)
	if firstResponse["message"] != secondResponse["message"] {
		t.Fatal("registration enumerated existing accounts")
	}
	a.expect(a.request("POST", "/api/v1/auth/login", register, customer), 200)
	a.expect(a.request("GET", "/api/v1/admin/catalog", nil, customer), 403)
	a.expect(a.request("DELETE", "/api/v1/me/sessions/"+adminSession, nil, customer), 404)
	token := a.mailToken("verify", "customer@example.invalid")
	a.expect(a.request("POST", "/api/v1/auth/complete-email", map[string]string{"token": token}, nil), 200)
	a.expect(a.request("POST", "/api/v1/auth/complete-email", map[string]string{"token": token}, nil), 400)
	me := data[struct {
		User app.UserView `json:"user"`
	}](t, a.request("GET", "/api/v1/me", nil, customer))
	if !me.User.Verified {
		t.Fatal("verification did not update account")
	}
	// A new application instance must restore both the installation and sessions.
	a.handler = newHandler()
	a.expect(a.request("GET", "/api/v1/me", nil, customer), 200)
	a.expect(a.request("POST", "/api/v1/me/email", map[string]string{"email": "new@example.invalid", "password": register["password"]}, customer), 200)
	changeToken := a.mailToken("verify", "new@example.invalid")
	a.expect(a.request("POST", "/api/v1/auth/complete-email", map[string]string{"token": changeToken}, nil), 200)
	a.expect(a.request("GET", "/api/v1/me", nil, customer), 401)
	register["email"] = "new@example.invalid"
	a.expect(a.request("POST", "/api/v1/auth/login", register, customer), 200)
	me = data[struct {
		User app.UserView `json:"user"`
	}](t, a.request("GET", "/api/v1/me", nil, customer))
	a.expect(a.request("POST", "/api/v1/admin/users/"+me.User.ID+"/status", map[string]string{"status": "disabled", "reason": "Test suspension"}, admin), 200)
	a.expect(a.request("GET", "/api/v1/me", nil, customer), 401)
	a.expect(a.request("POST", "/api/v1/auth/login", register, customer), 401)
	a.expect(a.request("POST", "/api/v1/admin/users/"+me.User.ID+"/status", map[string]string{"status": "active", "reason": "Test restoration"}, admin), 200)
	a.expect(a.request("POST", "/api/v1/auth/login", register, customer), 200)
	a.expect(a.request("POST", "/api/v1/auth/email", map[string]string{"email": register["email"], "kind": "reset"}, nil), 200)
	resetToken := a.mailToken("reset", register["email"])
	a.expect(a.request("POST", "/api/v1/auth/complete-email", map[string]string{"token": resetToken, "password": "new-password-after-reset"}, nil), 200)
	a.expect(a.request("GET", "/api/v1/me", nil, customer), 401)
	a.expect(a.request("POST", "/api/v1/auth/login", register, customer), 401)
	register["password"] = "new-password-after-reset"
	a.expect(a.request("POST", "/api/v1/auth/login", register, customer), 200)
	var user storage.User
	if db.First(&user, "id = ?", me.User.ID).Error != nil || user.PasswordHash == register["password"] || !strings.HasPrefix(user.PasswordHash, "$argon2id$") {
		t.Fatal("password stored incorrectly")
	}
	invoiceOrderID, _ := security.ID()
	paidAt := time.Now().UTC().Truncate(time.Microsecond)
	invoiceOrder := storage.Order{ID: invoiceOrderID, UserID: user.ID, ProductID: plan.ProductID, PlanVersionID: plan.VersionID, Environment: "live", Action: "purchase", Status: "paid", IdempotencyKey: "invoice-test-order-0001", RequestHash: bytes.Repeat([]byte{7}, 32), Currency: "USD", AmountMinor: 1299, PurchasedPeriods: 1, PriceSnapshot: json.RawMessage(`{"plan_name":"Pro"}`), EntitlementSnapshot: json.RawMessage(`{}`), ExpiresAt: paidAt.Add(24 * time.Hour), PaidAt: &paidAt, CreatedAt: paidAt}
	if db.Create(&invoiceOrder).Error != nil {
		t.Fatal("could not create paid invoice fixture")
	}
	a.expect(a.request("POST", "/api/v1/orders/"+invoiceOrderID+"/invoice", app.InvoiceInput{Type: "company", Title: "Test Company", Email: user.Email}, customer), 400)
	w = a.request("POST", "/api/v1/orders/"+invoiceOrderID+"/invoice", app.InvoiceInput{Type: "personal", Title: "Test Customer", Email: user.Email}, customer)
	a.expect(w, 200)
	invoice := data[app.InvoiceView](t, w)
	if invoice.Status != "requested" || invoice.OrderID != invoiceOrderID {
		t.Fatalf("invoice request was not created: %+v", invoice)
	}
	adminInvoices := data[[]app.InvoiceView](t, a.request("GET", "/api/v1/admin/invoices", nil, admin))
	if len(adminInvoices) != 1 || adminInvoices[0].CustomerEmail != user.Email {
		t.Fatalf("administrator did not receive invoice request: %+v", adminInvoices)
	}
	a.expect(a.request("POST", "/api/v1/admin/invoices/"+invoice.ID, app.InvoiceDecision{Status: "issued", URL: "https://example.invalid/invoices/" + invoice.ID, Note: "issued"}, admin), 200)
	customerInvoices := data[[]app.InvoiceView](t, a.request("GET", "/api/v1/me/invoices", nil, customer))
	if len(customerInvoices) != 1 || customerInvoices[0].Status != "issued" || customerInvoices[0].InvoiceURL == "" {
		t.Fatalf("issued invoice was not visible to customer: %+v", customerInvoices)
	}
	var outbox []storage.EmailOutbox
	_ = db.Find(&outbox).Error
	for _, row := range outbox {
		if bytes.Contains(row.Ciphertext, []byte("token=")) {
			t.Fatal("plaintext credential in outbox")
		}
	}
	svc, err := app.New(context.Background(), db, app.Config{PublicURL: "https://merchant.example", MasterKey: key})
	if err != nil {
		t.Fatal(err)
	}
	sender := &capturingSender{fail: true}
	handled, err := svc.ProcessMail(context.Background(), sender)
	if err != nil || !handled {
		t.Fatal("mail claim failed")
	}
	var pending storage.EmailOutbox
	if db.Where("status = 'retrying'").First(&pending).Error != nil {
		t.Fatal("failed email was not retained")
	}
	if db.Model(&pending).Update("available_at", time.Now().Add(-time.Minute)).Error != nil {
		t.Fatal("mail fixture failed")
	}
	sender.fail = false
	for i := 0; i < 20; i++ {
		handled, err := svc.ProcessMail(context.Background(), sender)
		if err != nil {
			t.Fatal(err)
		}
		if !handled {
			break
		}
	}
	if len(sender.messages) != len(outbox) {
		t.Fatalf("outbox did not drain: got %d messages, want %d", len(sender.messages), len(outbox))
	}
	testCommerce(a, admin, customer, register, next)
	// Keep the long-running preview honest: the isolated fixture used a fake
	// DeepL key and translator, so it must not look like a real connection.
	if db.Where("1 = 1").Delete(&storage.ContentTranslation{}).Error != nil || db.Where("1 = 1").Delete(&storage.TranslationTask{}).Error != nil || db.Delete(&storage.TranslationSettings{}, "id = ?", 1).Error != nil || db.Delete(&storage.SecretRecord{}, "id = ?", translationSettings.CredentialSecretID).Error != nil {
		t.Fatal("translation fixture cleanup failed")
	}
}

type capturingSender struct {
	fail     bool
	messages []mailer.Message
}

func (s *capturingSender) Send(ctx context.Context, message mailer.Message) error {
	if s.fail {
		return errors.New("simulated delivery failure")
	}
	s.messages = append(s.messages, message)
	return nil
}
