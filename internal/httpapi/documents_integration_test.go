package httpapi_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"licentivo-launch/internal/app"
	"licentivo-launch/internal/httpapi"
	"licentivo-launch/internal/security"
	"licentivo-launch/internal/storage"
)

// This fixture creates its own database. It never initializes, truncates or
// changes the database named in the supplied base DSN.
func TestDocumentManagementHTTPAndSEO(t *testing.T) {
	base := os.Getenv("LAUNCH_DOCUMENT_TEST_BASE_DSN")
	if base == "" {
		t.Skip("set LAUNCH_DOCUMENT_TEST_BASE_DSN to create an isolated document test database")
	}
	cfg, err := mysql.ParseDSN(base)
	if err != nil {
		t.Fatal("invalid test DSN")
	}
	cfg.DBName = ""
	cfg.MultiStatements = false
	adminDB, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal("cannot open test server")
	}
	defer adminDB.Close()
	name := fmt.Sprintf("launch_test_documents_%d", time.Now().UnixNano())
	if _, err = adminDB.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_bin"); err != nil {
		t.Fatal("cannot create isolated test database", err)
	}
	keep := os.Getenv("LAUNCH_DOCUMENT_PREVIEW_CONFIG")
	defer func() {
		if keep == "" || t.Failed() {
			if !strings.HasPrefix(name, "launch_test_documents_") {
				t.Fatal("invalid cleanup target")
			}
			if _, e := adminDB.Exec("DROP DATABASE `" + name + "`"); e != nil {
				t.Error(e)
			}
		}
	}()
	cfg.DBName = name
	db, err := storage.Open(cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close(db)
	ctx := context.Background()
	if err = storage.Initialize(ctx, db); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{7}, 32)
	uploadDir := t.TempDir()
	if keep != "" {
		uploadDir = filepath.Join(filepath.Dir(keep), "uploads")
	}
	svc, err := app.New(ctx, db, app.Config{MasterKey: key, PublicURL: "http://localhost:18081", UploadDir: uploadDir})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	if err = db.Model(&storage.InstanceSettings{}).Where("id=1").Updates(map[string]any{"installation_completed_at": at, "brand": json.RawMessage(`{"name":"Updated Brand","theme":"forest","support_email":"support@example.invalid"}`)}).Error; err != nil {
		t.Fatal(err)
	}
	hash, err := security.HashPassword("Documents-preview-2026!")
	if err != nil {
		t.Fatal(err)
	}
	user := storage.User{ID: newFixtureID(t), Email: "owner@example.invalid", EmailKey: "owner@example.invalid", PasswordHash: hash, Status: "active"}
	if err = db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&storage.StaffAccount{UserID: user.ID, Role: "admin", Permissions: json.RawMessage(`[]`)}).Error; err != nil {
		t.Fatal(err)
	}
	token, csrf := strings.Repeat("t", 43), strings.Repeat("c", 43)
	if err = db.Create(&storage.Session{ID: newFixtureID(t), UserID: user.ID, Kind: "website", TokenHash: security.Digest(token), CSRFHash: security.Digest(csrf), ExpiresAt: at.Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	product := storage.Product{ID: newFixtureID(t), Name: "Test Editor", Slug: "test-editor", Status: "published", Content: json.RawMessage(`{"summary":"A product","features":[],"version":"1.0"}`)}
	if err = db.Create(&product).Error; err != nil {
		t.Fatal(err)
	}
	other := storage.Product{ID: newFixtureID(t), Name: "Capture Studio", Slug: "capture-studio", Status: "published", Content: product.Content}
	if err = db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	draftProduct := storage.Product{ID: newFixtureID(t), Name: "Sync Desk", Slug: "sync-desk", Status: "draft", Content: product.Content}
	if err = db.Create(&draftProduct).Error; err != nil {
		t.Fatal(err)
	}
	handler := httpapi.Handler(httpapi.Options{App: svc, PublicURL: "http://localhost:18081"})
	request := func(method, path string, body any, auth bool) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		if auth {
			r.AddCookie(&http.Cookie{Name: "launch_session", Value: token})
			r.Header.Set("X-CSRF-Token", csrf)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	var lib app.DocumentLibraryView
	change := func(c app.DocumentCommand) {
		t.Helper()
		c.Version = lib.Version
		w := request("POST", "/api/v1/admin/documents/"+product.ID, c, true)
		if w.Code != 200 {
			t.Fatalf("command %s HTTP %d: %s", c.Action, w.Code, w.Body.String())
		}
		lib = data[app.DocumentLibraryView](t, w)
	}
	if w := request("GET", "/api/v1/admin/documents/"+product.ID, nil, false); w.Code != 401 {
		t.Fatal("unauthenticated library access", w.Code)
	}
	change(app.DocumentCommand{Action: "create_group", Title: "快速开始"})
	group := lib.Groups[0].ID
	change(app.DocumentCommand{Action: "create_article", GroupID: group, Title: "安装与激活"})
	article := lib.Groups[0].Articles[0].ID
	content := app.DocumentContent{Title: "安装与激活", Slug: "installation", Body: "## 准备工作\n下载适用于当前系统的安装包。\n\n## 安装软件\n1. 打开下载的安装包。\n2. 按照提示完成安装。\n\n## 登录与激活\n使用商店账号登录。\n\n```\ntest-editor --version\n```", SEO: app.DocumentSEO{Title: "Custom search title", Description: "Public search description"}}
	change(app.DocumentCommand{Action: "save_article", ID: article, Content: content})
	change(app.DocumentCommand{Action: "publish_article", ID: article})
	private := content
	private.Body = "DRAFT_SECRET"
	private.SEO.Title = "PRIVATE_META"
	change(app.DocumentCommand{Action: "save_article", ID: article, Content: private})
	page := request("GET", "/zh-cn/products/test-editor/docs/installation", nil, false)
	for _, value := range []string{"<title>Custom search title</title>", "Public search description", `id="section-1"`, "<pre><code>", "rel=\"canonical\""} {
		if !strings.Contains(page.Body.String(), value) {
			t.Fatal("missing SSR/SEO", page.Code, value, page.Body.String())
		}
	}
	if strings.Contains(page.Body.String(), "PRIVATE") || strings.Contains(page.Body.String(), "DRAFT_SECRET") {
		t.Fatal("draft leaked into SSR")
	}
	if w := request("GET", "/api/v1/site?locale=zh-CN", nil, false); strings.Contains(w.Body.String(), "DRAFT_SECRET") {
		t.Fatal("draft leaked into public API")
	}
	if w := request("GET", "/sitemap.xml", nil, false); !strings.Contains(w.Body.String(), "/docs/installation") {
		t.Fatal("published document absent from sitemap", w.Body.String())
	}
	content.SEO.NoIndex = true
	change(app.DocumentCommand{Action: "save_article", ID: article, Content: content})
	change(app.DocumentCommand{Action: "publish_article", ID: article})
	if w := request("GET", "/zh-cn/products/test-editor/docs/installation", nil, false); w.Header().Get("X-Robots-Tag") != "noindex" {
		t.Fatal("noindex header missing")
	}
	if w := request("GET", "/sitemap.xml", nil, false); strings.Contains(w.Body.String(), "/docs/installation") {
		t.Fatal("noindex document included in sitemap")
	}
	content.SEO.NoIndex = false
	content.Slug = "installation-new"
	change(app.DocumentCommand{Action: "save_article", ID: article, Content: content})
	change(app.DocumentCommand{Action: "publish_article", ID: article})
	if w := request("GET", "/zh-cn/products/test-editor/docs/installation", nil, false); w.Code != 301 || !strings.HasSuffix(w.Header().Get("Location"), "/installation-new") {
		t.Fatal("old URL does not redirect", w.Code)
	}
	if err = db.Model(&product).Update("status", "draft").Error; err != nil {
		t.Fatal(err)
	}
	if w := request("GET", "/zh-cn/products/test-editor/docs/installation-new", nil, false); w.Code != 404 {
		t.Fatal("draft product article is public", w.Code)
	}
	if err = db.Model(&product).Update("status", "published").Error; err != nil {
		t.Fatal(err)
	}
	if w := request("POST", "/api/v1/admin/documents/"+product.ID, app.DocumentCommand{Action: "create_group", Version: 0, Title: "Stale"}, true); w.Code != 409 {
		t.Fatal("stale update allowed", w.Code)
	}
	if w := request("GET", "/api/v1/admin/documents/"+other.ID, nil, true); strings.Contains(w.Body.String(), "installation") {
		t.Fatal("documents crossed products")
	}
	if err = db.Model(&storage.StaffAccount{}).Where("user_id=?", user.ID).Update("role", "support").Error; err != nil {
		t.Fatal(err)
	}
	if w := request("GET", "/api/v1/admin/documents/"+product.ID, nil, true); w.Code != 403 {
		t.Fatal("support accessed document drafts", w.Code)
	}
	if err = db.Model(&storage.StaffAccount{}).Where("user_id=?", user.ID).Update("role", "admin").Error; err != nil {
		t.Fatal(err)
	}
	// Two articles in the SAME product must retain different SEO metadata
	// and independent indexing choices. Editing one cannot replace the other.
	change(app.DocumentCommand{Action: "create_article", GroupID: group, Title: "另一篇文档"})
	second := lib.Groups[0].Articles[len(lib.Groups[0].Articles)-1].ID
	secondContent := app.DocumentContent{Title: "另一篇文档", Slug: "another-guide", Body: "Second article body", SEO: app.DocumentSEO{Title: "Second independent SEO title", Description: "Second independent description", NoIndex: true}}
	change(app.DocumentCommand{Action: "save_article", ID: second, Content: secondContent})
	change(app.DocumentCommand{Action: "publish_article", ID: second})
	if err = db.Model(&product).Update("seo", json.RawMessage(`{"pages":{"zh-CN":{"title":"Product-only title","description":"Product-only description","no_index":true}}}`)).Error; err != nil {
		t.Fatal(err)
	}
	firstPage := request("GET", "/zh-cn/products/test-editor/docs/installation-new", nil, false)
	secondPage := request("GET", "/zh-cn/products/test-editor/docs/another-guide", nil, false)
	if !strings.Contains(firstPage.Body.String(), "<title>Custom search title</title>") || strings.Contains(firstPage.Body.String(), "Second independent") {
		t.Fatal("another article overwrote first article SEO")
	}
	if firstPage.Header().Get("X-Robots-Tag") != "" {
		t.Fatal("product-page SEO overrode article indexing")
	}
	if !strings.Contains(secondPage.Body.String(), "<title>Second independent SEO title</title>") || !strings.Contains(secondPage.Body.String(), "Second independent description") || secondPage.Header().Get("X-Robots-Tag") != "noindex" {
		t.Fatal("second article SEO was not independent")
	}
	sitemap := request("GET", "/sitemap.xml", nil, false).Body.String()
	if !strings.Contains(sitemap, "/docs/installation-new") || strings.Contains(sitemap, "/docs/another-guide") {
		t.Fatal("article indexing preferences were applied to whole product")
	}
	change(app.DocumentCommand{Action: "delete_article", ID: second})
	if err = db.Model(&product).Update("seo", nil).Error; err != nil {
		t.Fatal(err)
	}
	if keep != "" {
		content.SEO = app.DocumentSEO{}
		content.Slug = "installation"
		change(app.DocumentCommand{Action: "save_article", ID: article, Content: content})
		change(app.DocumentCommand{Action: "publish_article", ID: article})
		add := func(g, title string, publish bool) {
			change(app.DocumentCommand{Action: "create_article", GroupID: g, Title: title})
			var a app.DocumentArticle
			for _, group := range lib.Groups {
				if group.ID == g {
					a = group.Articles[len(group.Articles)-1]
				}
			}
			c := a.Draft
			c.Body = "## " + title + "\n\n这里是 " + title + " 的使用说明。"
			change(app.DocumentCommand{Action: "save_article", ID: a.ID, Content: c})
			if publish {
				change(app.DocumentCommand{Action: "publish_article", ID: a.ID})
			}
		}
		add(group, "产品介绍", true)
		change(app.DocumentCommand{Action: "move_article", ID: lib.Groups[0].Articles[1].ID, GroupID: group, BeforeID: article})
		add(group, "第一次使用", false)
		change(app.DocumentCommand{Action: "create_group", Title: "使用指南"})
		g2 := lib.Groups[1].ID
		for _, title := range []string{"项目管理", "数据导入与导出", "快捷键"} {
			add(g2, title, true)
		}
		change(app.DocumentCommand{Action: "create_group", Title: "常见问题"})
		g3 := lib.Groups[2].ID
		add(g3, "登录失败怎么办", true)
		add(g3, "如何迁移设备", false)
		private = content
		private.Body += "\n\n> 升级安装前，请先关闭正在运行的旧版本。"
		change(app.DocumentCommand{Action: "save_article", ID: article, Content: private})
		config := map[string]string{"environment": "development", "http_addr": "127.0.0.1:18081", "https_addr": "", "public_url": "http://localhost:18081", "database_dsn": cfg.FormatDSN(), "master_key": base64.StdEncoding.EncodeToString(key), "upload_dir": uploadDir}
		if err = os.MkdirAll(filepath.Dir(keep), 0700); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(config)
		if err = os.WriteFile(keep, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
