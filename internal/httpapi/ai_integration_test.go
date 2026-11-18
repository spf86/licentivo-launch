package httpapi_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"licentivo-launch/internal/ai"
	"licentivo-launch/internal/app"
	"licentivo-launch/internal/httpapi"
	"licentivo-launch/internal/security"
	"licentivo-launch/internal/storage"
)

type aiRoundTrip func(*http.Request) (*http.Response, error)

func (f aiRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAIAdminWorkflow(t *testing.T) {
	base := os.Getenv("LAUNCH_AI_TEST_BASE_DSN")
	if base == "" {
		t.Skip("set LAUNCH_AI_TEST_BASE_DSN to a disposable MySQL server with CREATE DATABASE privileges")
	}
	cfg, err := mysql.ParseDSN(base)
	if err != nil || !strings.HasPrefix(cfg.DBName, "launch_test_") {
		t.Fatal("AI tests require an isolated launch_test_* server fixture")
	}
	cfg.DBName = ""
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := fmt.Sprintf("launch_test_ai_%d", time.Now().UnixNano())
	if _, err = admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_bin"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if os.Getenv("LAUNCH_AI_PREVIEW_CONFIG") == "" || t.Failed() {
			_, _ = admin.Exec("DROP DATABASE `" + name + "`")
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
		t.Fatal("AI migration", err)
	}
	var calls, videoCreates atomic.Int32
	var lastChatModel atomic.Value
	var slowStarted = make(chan struct{}, 1)
	var pngBytes bytes.Buffer
	_ = png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	client := &ai.Client{HTTP: &http.Client{Transport: aiRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		raw := []byte{}
		body := map[string]any{}
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/models"):
			body = map[string]any{"data": []map[string]string{{"id": "test-chat", "name": "Test chat"}, {"id": "test-vision", "name": "Test vision"}}}
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			var request struct {
				Model    string       `json:"model"`
				Messages []ai.Message `json:"messages"`
				Tools    []any        `json:"tools"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			lastChatModel.Store(request.Model)
			for _, m := range request.Messages {
				if strings.Contains(m.Content, "ai-private-key") {
					t.Fatal("credential leaked into model input")
				}
			}
			answer := ai.Message{Role: "assistant", Content: "已完成工具步骤，请核对文件和待确认任务。"}
			if len(request.Messages) > 0 && request.Messages[len(request.Messages)-1].Role == "user" {
				prompt := request.Messages[len(request.Messages)-1].Content
				answer.Content = ""
				callIndex := 0
				call := func(name string, args any) ai.Call {
					c := ai.Call{ID: fmt.Sprintf("call_%d", callIndex), Type: "function"}
					callIndex++
					c.Function.Name = name
					b, _ := json.Marshal(args)
					c.Function.Arguments = string(b)
					return c
				}
				switch {
				case strings.Contains(prompt, "slow-stop-test"):
					select {
					case slowStarted <- struct{}{}:
					default:
					}
					<-r.Context().Done()
					return nil, r.Context().Err()
				case strings.HasPrefix(prompt, "plan-test"):
					answer.ToolCalls = append(answer.ToolCalls, call("propose_plan", app.AIPlan{Title: "汇总经营情况", Goal: "inspect approved plan", Steps: []app.AIPlanStep{{Title: "查询经营概览", Tool: "query_system"}}}))
				case strings.HasPrefix(prompt, "create product test"):
					answer.ToolCalls = append(answer.ToolCalls, call("propose_change", map[string]any{"action": "save_product", "arguments": map[string]any{"name": "AI Product", "slug": "ai-created-product", "status": "draft", "content": app.ProductContent{Summary: "Created through confirmed AI task"}}, "summary": "创建草稿产品 AI Product"}))
				case strings.HasPrefix(prompt, "create document test:"):
					productID := strings.TrimPrefix(prompt, "create document test:")
					answer.ToolCalls = append(answer.ToolCalls, call("propose_change", map[string]any{"action": "create_document", "arguments": map[string]any{"product_id": productID, "group_title": "AI Guides", "title": "Getting Started", "slug": "ai-getting-started", "body": "# Getting started\nInstall and sign in.", "publish": false}, "summary": "创建含正文的产品使用指南草稿"}))
				case strings.HasPrefix(prompt, "create invalid document test:"):
					answer.ToolCalls = append(answer.ToolCalls, call("propose_change", map[string]any{"action": "create_document", "arguments": map[string]any{"product_id": strings.TrimPrefix(prompt, "create invalid document test:"), "group_title": "Must roll back", "title": "Bad document", "slug": "bad slug", "body": "content"}, "summary": "验证创建失败回滚"}))
				case strings.Contains(prompt, "files"):
					answer.ToolCalls = append(answer.ToolCalls, call("generate_excel", ai.OfficeInput{Filename: "销售报表", Sheets: []ai.Sheet{{Name: "Sales", Columns: []string{"Product", "Revenue"}, Rows: [][]any{{"Studio", float64(1200)}}}}}), call("generate_word", ai.OfficeInput{Filename: "产品指南", Title: "使用指南", Sections: []ai.Section{{Heading: "开始", Paragraphs: []string{"安装并登录"}}}}), call("generate_presentation", ai.OfficeInput{Filename: "产品介绍", Title: "Launch", Slides: []ai.Slide{{Title: "介绍", Bullets: []string{"独立部署的软件商店"}}}}))
				case strings.Contains(prompt, "change") || strings.Contains(prompt, "readonly"):
					answer.ToolCalls = append(answer.ToolCalls, call("propose_change", map[string]any{"action": "save_brand", "arguments": app.Brand{Name: "AI Updated Store", Tagline: "AI tested", Theme: "forest"}, "summary": "把店铺名称修改为 AI Updated Store"}))
				default:
					answer.ToolCalls = append(answer.ToolCalls, call("query_system", map[string]string{"area": "overview"}))
				}
			}
			body = map[string]any{"choices": []any{map[string]any{"message": answer}}}
		case strings.HasSuffix(r.URL.Path, "/images/generations"):
			body = map[string]any{"data": []any{map[string]string{"b64_json": base64.StdEncoding.EncodeToString(pngBytes.Bytes())}}}
		case strings.HasSuffix(r.URL.Path, "/video-synthesis"):
			videoCreates.Add(1)
			if r.Header.Get("X-DashScope-Async") != "enable" {
				t.Fatal("not async")
			}
			body = map[string]any{"output": map[string]string{"task_id": "video_test_1", "task_status": "PENDING"}}
		case strings.Contains(r.URL.Path, "/tasks/"):
			body = map[string]any{"output": map[string]string{"task_id": "video_test_1", "task_status": "SUCCEEDED", "video_url": "https://media.example.com/result.mp4"}}
		case r.URL.Host == "media.example.com":
			if r.Header.Get("Authorization") != "" {
				t.Fatal("API key forwarded to CDN")
			}
			raw = []byte("\x00\x00\x00\x18ftypisomtest-mp4-payload")
		default:
			t.Fatal("unexpected provider request", r.URL.Path)
		}
		if raw == nil || len(raw) == 0 {
			raw, _ = json.Marshal(body)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(raw))}, nil
	})}}
	key := bytes.Repeat([]byte{9}, 32)
	uploadDir := t.TempDir()
	preview := os.Getenv("LAUNCH_AI_PREVIEW_CONFIG")
	if preview != "" {
		uploadDir = filepath.Join(filepath.Dir(preview), "uploads")
	}
	svc, err := app.New(ctx, db, app.Config{MasterKey: key, PublicURL: "http://localhost:18089", UploadDir: uploadDir, AIClient: client})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	if db.Model(&storage.InstanceSettings{}).Where("id=1").Updates(map[string]any{"installation_completed_at": at}).Error != nil {
		t.Fatal("setup fixture")
	}
	hash, _ := security.HashPassword("AI-preview-password-2026!")
	users := map[string]storage.User{}
	tokens := map[string]string{}
	for _, role := range []string{"admin", "other_admin", "operations", "support", "customer"} {
		u := storage.User{ID: newFixtureID(t), Email: role + "@example.invalid", EmailKey: role + "@example.invalid", PasswordHash: hash, Status: "active"}
		if db.Create(&u).Error != nil {
			t.Fatal("user fixture")
		}
		users[role] = u
		if role != "customer" {
			staffRole := role
			if role == "other_admin" {
				staffRole = "admin"
			}
			if db.Create(&storage.StaffAccount{UserID: u.ID, Role: staffRole, Permissions: json.RawMessage(`[]`)}).Error != nil {
				t.Fatal("staff fixture")
			}
		}
		token := strings.Repeat(role[:1], 42) + fmt.Sprint(len(tokens))
		tokens[role] = token
		if db.Create(&storage.Session{ID: newFixtureID(t), UserID: u.ID, Kind: "website", TokenHash: security.Digest(token), CSRFHash: security.Digest(strings.Repeat("c", 43)), ExpiresAt: at.Add(time.Hour)}).Error != nil {
			t.Fatal("session fixture")
		}
	}
	handler := httpapi.Handler(httpapi.Options{App: svc, PublicURL: "http://localhost:18089"})
	for _, path := range []string{"/", "/admin"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), "/assets/ai-workspace.css") {
			t.Fatal("AI stylesheet missing from actual entry", path, w.Code)
		}
	}
	request := func(role, method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "/api/v1"+path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		if role != "" {
			r.AddCookie(&http.Cookie{Name: "launch_session", Value: tokens[role]})
			r.Header.Set("X-CSRF-Token", strings.Repeat("c", 43))
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	ok := func(w *httptest.ResponseRecorder) {
		t.Helper()
		if w.Code != 200 {
			t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
		}
	}
	provider := func(capability, protocol, model, url string) app.AIProviderView {
		w := request("admin", "PUT", "/admin/ai/providers", app.AIProviderInput{Name: capability, Protocol: protocol, Capability: capability, Model: model, BaseURL: url, APIKey: "ai-private-key", Enabled: true})
		ok(w)
		if strings.Contains(w.Body.String(), "ai-private-key") {
			t.Fatal("key leaked in config response")
		}
		return data[app.AIProviderView](t, w)
	}
	chatModel := provider("chat", "openai", "test-chat", "https://api.deepseek.com/v1")
	imageModel := provider("image", "openai", "gpt-image-test", "https://api.openai.com/v1")
	videoModel := provider("video", "dashscope", "test-video", "https://dashscope.aliyuncs.com/api/v1")
	var stored storage.AIProvider
	_ = db.First(&stored, "id = ?", chatModel.ID)
	if bytes.Contains(stored.Secret, []byte("ai-private-key")) {
		t.Fatal("key stored in plaintext")
	}
	w := request("admin", "POST", "/admin/ai/conversations", map[string]string{"provider_id": chatModel.ID})
	ok(w)
	c := data[app.AIConversationView](t, w)
	for _, role := range []string{"operations", "support", "customer"} {
		for _, route := range []string{"/admin/ai/providers", "/admin/ai/conversations", "/admin/ai/tasks", "/admin/ai/schedules", "/admin/ai/operations", "/admin/ai/conversations/" + c.ID, "/admin/ai/artifacts/" + newFixtureID(t)} {
			if w := request(role, "GET", route, nil); w.Code != 403 {
				t.Fatal("non-admin AI access", role, route, w.Code)
			}
		}
		if w := request(role, "POST", "/admin/ai/conversations/"+c.ID+"/messages", map[string]string{}); w.Code != 403 {
			t.Fatal("non-admin send")
		}
	}
	if w := request("", "GET", "/admin/ai/providers", nil); w.Code != 401 {
		t.Fatal("anonymous access")
	}
	if w := request("other_admin", "GET", "/admin/ai/conversations/"+c.ID, nil); w.Code != 404 {
		t.Fatal("other administrator saw conversation", w.Code)
	}
	process := func() {
		t.Helper()
		worked, err := svc.ProcessAI(ctx)
		if err != nil || !worked {
			t.Fatal("worker", worked, err)
		}
	}
	send := func(content string) app.AITaskView {
		w := request("admin", "POST", "/admin/ai/conversations/"+c.ID+"/messages", app.AIChatInput{Content: content, RequestKey: newFixtureID(t)})
		ok(w)
		return data[app.AITaskView](t, w)
	}
	filesTask := send("generate files")
	process()
	requestKey := newFixtureID(t)
	first := request("admin", "POST", "/admin/ai/conversations/"+c.ID+"/messages", app.AIChatInput{Content: "inspect idempotently", RequestKey: requestKey})
	ok(first)
	second := request("admin", "POST", "/admin/ai/conversations/"+c.ID+"/messages", app.AIChatInput{Content: "inspect idempotently", RequestKey: requestKey})
	ok(second)
	if data[app.AITaskView](t, first).ID != data[app.AITaskView](t, second).ID {
		t.Fatal("repeated send created duplicate task")
	}
	process()
	var artifacts []storage.AIArtifact
	if db.Where("task_id = ?", filesTask.ID).Find(&artifacts).Error != nil || len(artifacts) != 3 {
		t.Fatal("office generation failed", len(artifacts))
	}
	for _, a := range artifacts {
		w := request("admin", "GET", "/admin/ai/artifacts/"+a.ID, nil)
		ok(w)
		if !bytes.HasPrefix(w.Body.Bytes(), []byte("PK")) || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
			t.Fatal("invalid Office download")
		}
		if request("other_admin", "GET", "/admin/ai/artifacts/"+a.ID, nil).Code != 404 {
			t.Fatal("artifact owner isolation")
		}
		disk, _ := os.ReadFile(filepath.Join(uploadDir, "ai-artifacts", a.Path))
		if bytes.HasPrefix(disk, []byte("PK")) {
			t.Fatal("artifact not encrypted")
		}
	}
	changeTask := send("change brand")
	process()
	var proposal storage.AITask
	if db.Where("conversation_id = ? AND kind = ? AND status = ?", c.ID, "operation", "awaiting_confirmation").First(&proposal).Error != nil {
		t.Fatal("missing confirmation", changeTask.ID)
	}
	var settings storage.InstanceSettings
	db.First(&settings)
	if bytes.Contains(settings.Brand, []byte("AI Updated")) {
		t.Fatal("changed without confirmation")
	}
	if request("other_admin", "POST", "/admin/ai/tasks/"+proposal.ID+"/decision", map[string]string{"decision": "confirm"}).Code != 404 {
		t.Fatal("other admin confirmed task")
	}
	ok(request("admin", "POST", "/admin/ai/tasks/"+proposal.ID+"/decision", map[string]string{"decision": "confirm"}))
	process()
	db.First(&settings)
	if !bytes.Contains(settings.Brand, []byte("AI Updated Store")) {
		var failed storage.AITask
		db.First(&failed, "id = ?", proposal.ID)
		t.Fatal("confirmed change failed", failed.Error)
	}
	if request("admin", "POST", "/admin/ai/tasks/"+proposal.ID+"/decision", map[string]string{"decision": "confirm"}).Code != 409 {
		t.Fatal("duplicate confirmation accepted")
	}
	send("change again")
	process()
	proposal = storage.AITask{}
	db.Where("kind = ? AND status = ?", "operation", "awaiting_confirmation").First(&proposal)
	if err := svc.SaveBrand(ctx, app.Principal{User: users["admin"], Role: "admin"}, app.Brand{Name: "Manual Store", Theme: "ocean"}); err != nil {
		t.Fatal(err)
	}
	ok(request("admin", "POST", "/admin/ai/tasks/"+proposal.ID+"/decision", map[string]string{"decision": "confirm"}))
	process()
	db.First(&settings)
	if !bytes.Contains(settings.Brand, []byte("Manual Store")) {
		t.Fatal("stale preview overwrote manual changes")
	}
	media := func(cap string, p app.AIProviderView) app.AITaskView {
		w := request("admin", "POST", "/admin/ai/media", app.AIMediaInput{ConversationID: c.ID, ProviderID: p.ID, RequestKey: newFixtureID(t), Capability: cap, Filename: "素材", Prompt: "product illustration", Seconds: 5})
		ok(w)
		return data[app.AITaskView](t, w)
	}
	i := media("image", imageModel)
	before := calls.Load()
	if worked, _ := svc.ProcessAI(ctx); worked || calls.Load() != before {
		t.Fatal("unconfirmed media executed")
	}
	ok(request("admin", "POST", "/admin/ai/tasks/"+i.ID+"/decision", map[string]string{"decision": "confirm"}))
	process()
	v := media("video", videoModel)
	ok(request("admin", "POST", "/admin/ai/tasks/"+v.ID+"/decision", map[string]string{"decision": "confirm"}))
	process()
	var videoTask storage.AITask
	db.First(&videoTask, "id = ?", v.ID)
	if videoTask.Status != "submitted" || videoTask.RemoteID != "video_test_1" {
		t.Fatal("video submission not persisted", videoTask.Status, videoTask.Error)
	}
	db.Model(&videoTask).Update("available_at", at.Add(-time.Minute))
	process()
	db.First(&videoTask, "id = ?", v.ID)
	if videoTask.Status != "completed" || videoCreates.Load() != 1 {
		t.Fatal("video polling duplicated submission", videoTask.Status, videoTask.Error)
	}
	schedule, err := svc.SaveAISchedule(ctx, app.Principal{User: users["admin"], Role: "admin"}, app.AIScheduleInput{ConversationID: c.ID, ProviderID: chatModel.ID, Name: "readonly", Goal: "readonly attempt", IntervalMinutes: 15, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	db.Model(&storage.AISchedule{}).Where("id = ?", schedule.ID).Update("next_run_at", at.Add(-time.Minute))
	if err := svc.ProcessAISchedules(ctx); err != nil {
		t.Fatal(err)
	}
	process()
	var pending int64
	db.Model(&storage.AITask{}).Where("kind = ? AND status = ?", "operation", "awaiting_confirmation").Count(&pending)
	if pending != 0 {
		t.Fatal("scheduled task proposed mutation")
	}
	// Downgrading an administrator after enqueueing is checked by the worker.
	queued := send("inspect")
	db.Model(&storage.StaffAccount{}).Where("user_id = ?", users["admin"].ID).Update("role", "support")
	before = calls.Load()
	process()
	if calls.Load() != before {
		t.Fatal("revoked administrator still called AI")
	}
	db.Model(&storage.StaffAccount{}).Where("user_id = ?", users["admin"].ID).Update("role", "admin")
	var revoked storage.AITask
	db.First(&revoked, "id = ?", queued.ID)
	if revoked.Status != "failed" {
		t.Fatal("revoked task not failed")
	}
	interrupted := send("inspect interrupted")
	if db.Model(&storage.AITask{}).Where("id = ?", interrupted.ID).Updates(map[string]any{"status": "running", "lease_until": at.Add(-time.Minute)}).Error != nil {
		t.Fatal("interrupted fixture")
	}
	before = calls.Load()
	if worked, err := svc.ProcessAI(ctx); err != nil || worked || calls.Load() != before {
		t.Fatal("interrupted task replayed", err)
	}
	var recovered storage.AITask
	db.First(&recovered, "id = ?", interrupted.ID)
	if recovered.Status != "unknown" {
		t.Fatal("expired worker lease not recovered")
	}
	for _, query := range []string{"", "?kind=tools", "?status=failed", "?kind=settings&q=chat", "?from=" + time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")} {
		w := request("admin", "GET", "/admin/ai/operations"+query, nil)
		ok(w)
		if strings.Contains(w.Body.String(), "ai-private-key") {
			t.Fatal("credential leaked in AI records")
		}
		var records struct {
			Items []app.AIOperationView `json:"items"`
			Total int64                 `json:"total"`
		}
		records = data[struct {
			Items []app.AIOperationView `json:"items"`
			Total int64                 `json:"total"`
		}](t, w)
		if records.Total < 1 {
			t.Fatal("AI records missing", query, w.Body.String())
		}
		for _, record := range records.Items {
			if record.Actor == "" || record.Label == "" || record.Status == "" {
				t.Fatal("AI record lacks actor/action/result")
			}
			if strings.Contains(query, "status=failed") && record.Status != "failed" {
				t.Fatal("AI record status filter")
			}
		}
	}
	t.Run("model catalog and selected model", func(t *testing.T) {
		w := request("admin", "POST", "/admin/ai/providers/"+chatModel.ID+"/refresh", map[string]any{})
		ok(w)
		refreshed := data[app.AIProviderView](t, w)
		if len(refreshed.Models) != 2 {
			t.Fatal("model discovery", w.Body.String())
		}
		w = request("admin", "POST", "/admin/ai/conversations/"+c.ID+"/messages", app.AIChatInput{Content: "inspect selected model", Model: "test-vision", RequestKey: newFixtureID(t)})
		ok(w)
		process()
		if lastChatModel.Load() != "test-vision" {
			t.Fatal("selected model ignored")
		}
		if request("admin", "POST", "/admin/ai/conversations/"+c.ID+"/messages", app.AIChatInput{Content: "inspect", Model: "not-configured", RequestKey: newFixtureID(t)}).Code != 400 {
			t.Fatal("unlisted model accepted")
		}
	})
	t.Run("uploads extraction ownership and idempotency", func(t *testing.T) {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, _ := writer.CreateFormFile("file", "facts.csv")
		_, _ = part.Write([]byte("product,revenue\nStudio,1200"))
		writer.Close()
		r := httptest.NewRequest("POST", "/api/v1/admin/ai/uploads", &body)
		r.Header.Set("Content-Type", writer.FormDataContentType())
		r.Header.Set("X-CSRF-Token", strings.Repeat("c", 43))
		r.AddCookie(&http.Cookie{Name: "launch_session", Value: tokens["admin"]})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		ok(w)
		upload := data[app.AIUploadView](t, w)
		if request("other_admin", "GET", "/admin/ai/uploads/"+upload.ID, nil).Code != 404 {
			t.Fatal("upload owner bypass")
		}
		ok(request("admin", "GET", "/admin/ai/uploads/"+upload.ID, nil))
		var row storage.AIUpload
		db.First(&row, "id = ?", upload.ID)
		raw, _ := os.ReadFile(filepath.Join(uploadDir, "ai-uploads", row.Path))
		if bytes.Contains(raw, []byte("Studio")) || bytes.Contains(row.TextCipher, []byte("Studio")) {
			t.Fatal("upload plaintext on disk")
		}
		in := app.AIChatInput{Content: "inspect attachment", Files: []string{upload.ID}, RequestKey: newFixtureID(t)}
		first := request("admin", "POST", "/admin/ai/conversations/"+c.ID+"/messages", in)
		ok(first)
		second := request("admin", "POST", "/admin/ai/conversations/"+c.ID+"/messages", in)
		ok(second)
		if data[app.AITaskView](t, first).ID != data[app.AITaskView](t, second).ID {
			t.Fatal("duplicate file message")
		}
		process()
		if request("admin", "POST", "/admin/ai/uploads/"+upload.ID+"/discard", map[string]any{}).Code != 404 {
			t.Fatal("bound upload discarded")
		}
		other := request("other_admin", "POST", "/admin/ai/conversations", map[string]string{"provider_id": chatModel.ID})
		ok(other)
		otherID := data[app.AIConversationView](t, other).ID
		if request("other_admin", "POST", "/admin/ai/conversations/"+otherID+"/messages", app.AIChatInput{Files: []string{upload.ID}, RequestKey: newFixtureID(t)}).Code != 404 {
			t.Fatal("foreign upload bound")
		}
	})
	t.Run("plan confirmation and execution", func(t *testing.T) {
		w := request("admin", "POST", "/admin/ai/conversations/"+c.ID+"/messages", app.AIChatInput{Content: "plan-test overview", Planning: true, RequestKey: newFixtureID(t)})
		ok(w)
		process()
		var planned storage.AITask
		if db.Where("conversation_id = ? AND kind = ? AND status = ?", c.ID, "plan", "awaiting_confirmation").First(&planned).Error != nil {
			t.Fatal("no plan proposal")
		}
		before := calls.Load()
		if worked, _ := svc.ProcessAI(ctx); worked || calls.Load() != before {
			t.Fatal("unconfirmed plan executed")
		}
		ok(request("admin", "POST", "/admin/ai/tasks/"+planned.ID+"/decision", map[string]string{"decision": "confirm"}))
		process()
		db.First(&planned, "id = ?", planned.ID)
		if planned.Status != "completed" {
			t.Fatal("plan failed", planned.Error)
		}
	})
	t.Run("create product and complete document", func(t *testing.T) {
		confirm := func() storage.AITask {
			t.Helper()
			var proposal storage.AITask
			if db.Where("conversation_id = ? AND kind = ? AND status = ?", c.ID, "operation", "awaiting_confirmation").First(&proposal).Error != nil {
				t.Fatal("missing creation proposal")
			}
			ok(request("admin", "POST", "/admin/ai/tasks/"+proposal.ID+"/decision", map[string]string{"decision": "confirm"}))
			process()
			db.First(&proposal, "id = ?", proposal.ID)
			if proposal.Status != "completed" {
				t.Fatal("creation failed", proposal.Error)
			}
			return proposal
		}
		send("create product test")
		process()
		var count int64
		db.Model(&storage.Product{}).Where("slug = ?", "ai-created-product").Count(&count)
		if count != 0 {
			t.Fatal("created without confirmation")
		}
		confirm()
		var product storage.Product
		if db.First(&product, "slug = ?", "ai-created-product").Error != nil || product.Status != "draft" {
			t.Fatal("product not created as draft")
		}
		send("create document test:" + product.ID)
		process()
		confirm()
		library, err := svc.DocumentLibrary(ctx, app.Principal{User: users["admin"], Role: "admin"}, product.ID)
		if err != nil || len(library.Groups) != 1 || len(library.Groups[0].Articles) != 1 || !strings.Contains(library.Groups[0].Articles[0].Draft.Body, "Install and sign in") || library.Groups[0].Articles[0].Published != nil {
			t.Fatal("complete document not created as draft", err, library)
		}
		send("create invalid document test:" + product.ID)
		process()
		var failed storage.AITask
		db.Where("conversation_id = ? AND kind = ? AND status = ?", c.ID, "operation", "awaiting_confirmation").First(&failed)
		ok(request("admin", "POST", "/admin/ai/tasks/"+failed.ID+"/decision", map[string]string{"decision": "confirm"}))
		process()
		db.First(&failed, "id = ?", failed.ID)
		after, err := svc.DocumentLibrary(ctx, app.Principal{User: users["admin"], Role: "admin"}, product.ID)
		if err != nil || failed.Status != "failed" || len(after.Groups) != len(library.Groups) || after.Version != library.Version {
			t.Fatal("failed document left partial state", failed.Status, err)
		}
	})
	t.Run("steer stop and conversation management", func(t *testing.T) {
		queued := send("inspect before steering")
		note := app.AISteerInput{Content: "inspect with steered requirements", RequestKey: newFixtureID(t)}
		ok(request("admin", "POST", "/admin/ai/conversations/"+c.ID+"/steer", note))
		ok(request("admin", "POST", "/admin/ai/conversations/"+c.ID+"/steer", note))
		process()
		var task storage.AITask
		db.First(&task, "id = ?", queued.ID)
		if task.Status != "completed" {
			t.Fatal("steered task failed", task.Error)
		}
		stopped := send("inspect queued stop")
		ok(request("admin", "POST", "/admin/ai/tasks/"+stopped.ID+"/decision", map[string]string{"decision": "stop"}))
		before := calls.Load()
		if worked, _ := svc.ProcessAI(ctx); worked || calls.Load() != before {
			t.Fatal("stopped queue executed")
		}
		slow := send("slow-stop-test")
		finished := make(chan error, 1)
		go func() { _, err := svc.ProcessAI(ctx); finished <- err }()
		select {
		case <-slowStarted:
		case <-time.After(5 * time.Second):
			t.Fatal("worker did not start")
		}
		ok(request("admin", "POST", "/admin/ai/tasks/"+slow.ID+"/decision", map[string]string{"decision": "stop"}))
		select {
		case err := <-finished:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("active provider request not stopped")
		}
		task = storage.AITask{}
		db.First(&task, "id = ?", slow.ID)
		if task.Status != "cancelled" {
			t.Fatal("stop result", task.Status, task.Error)
		}
		ok(request("admin", "POST", "/admin/ai/conversations/"+c.ID+"/command", app.AIConversationCommand{Action: "rename", Title: "AI workflow checks"}))
		if request("other_admin", "POST", "/admin/ai/conversations/"+c.ID+"/command", app.AIConversationCommand{Action: "archive"}).Code != 404 {
			t.Fatal("foreign conversation archived")
		}
		w := request("admin", "POST", "/admin/ai/conversations", map[string]string{"provider_id": chatModel.ID})
		ok(w)
		archived := data[app.AIConversationView](t, w)
		ok(request("admin", "POST", "/admin/ai/conversations/"+archived.ID+"/command", app.AIConversationCommand{Action: "archive"}))
		if request("admin", "GET", "/admin/ai/conversations/"+archived.ID, nil).Code != 404 {
			t.Fatal("archived conversation still active")
		}
	})
	t.Run("MCP protocol permissions revocation and tasks", func(t *testing.T) {
		mcp := func(token, method string, params any) *httptest.ResponseRecorder {
			t.Helper()
			raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
			r := httptest.NewRequest("POST", "/mcp", bytes.NewReader(raw))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Accept", "application/json, text/event-stream")
			r.Header.Set("Authorization", "Bearer "+token)
			r.Header.Set("Origin", "http://localhost:18089")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			return w
		}
		create := func(propose bool, days int) (string, string) {
			t.Helper()
			w := request("admin", "POST", "/admin/ai/mcp-connections", app.AIMCPInput{Name: "Fixture client", Days: &days, AllowProposals: propose})
			ok(w)
			var created struct {
				Token      string        `json:"token"`
				Connection app.AIMCPView `json:"connection"`
			}
			created = data[struct {
				Token      string        `json:"token"`
				Connection app.AIMCPView `json:"connection"`
			}](t, w)
			if (created.Connection.ExpiresAt == nil) != (days == 0) {
				t.Fatalf("unexpected expiry for %d days", days)
			}
			if days > 0 && created.Connection.ExpiresAt.Sub(created.Connection.CreatedAt) != time.Duration(days)*24*time.Hour {
				t.Fatal("incorrect token lifetime")
			}
			return created.Token, created.Connection.ID
		}
		for _, role := range []string{"customer", "support", "operations"} {
			if request(role, "POST", "/admin/ai/mcp-connections", map[string]any{"name": "bad", "days": 7}).Code != 403 {
				t.Fatal("non-admin created MCP")
			}
		}
		for _, input := range []map[string]any{{"name": "invalid"}, {"name": "invalid", "days": nil}, {"name": "invalid", "days": -1}, {"name": "invalid", "days": 366}} {
			if response := request("admin", "POST", "/admin/ai/mcp-connections", input); response.Code != 400 {
				t.Fatalf("invalid token lifetime accepted: %d", response.Code)
			}
		}
		token, id := create(false, 0)
		if err := db.Model(&storage.AIMCPConnection{}).Where("id = ? AND expires_at IS NULL", id).Update("created_at", time.Now().UTC().AddDate(0, 0, -400)); err.Error != nil || err.RowsAffected != 1 {
			t.Fatal("permanent connection must have no stored expiry", err.Error)
		}
		ok(mcp(token, "initialize", map[string]string{"protocolVersion": "2025-06-18"}))
		w := mcp(token, "tools/list", map[string]any{})
		ok(w)
		if strings.Contains(w.Body.String(), "propose_change") {
			t.Fatal("read-only MCP exposes writes")
		}
		ok(mcp(token, "tools/call", map[string]any{"name": "query_system", "arguments": map[string]string{"area": "overview"}}))
		w = mcp(token, "tools/call", map[string]any{"name": "propose_change", "arguments": map[string]any{}})
		if !strings.Contains(w.Body.String(), `"isError":true`) {
			t.Fatal("read-only proposal accepted")
		}
		list := request("admin", "GET", "/admin/ai/mcp-connections", nil)
		ok(list)
		if strings.Contains(list.Body.String(), token) || strings.Contains(list.Body.String(), "token_hash") {
			t.Fatal("MCP token leaked in list")
		}
		ok(request("admin", "POST", "/admin/ai/mcp-connections/"+id+"/revoke", map[string]any{}))
		if mcp(token, "ping", map[string]any{}).Code != 401 {
			t.Fatal("revoked token accepted")
		}
		token, id = create(true, 0)
		w = mcp(token, "tools/call", map[string]any{"name": "propose_change", "arguments": map[string]any{"action": "save_brand", "arguments": app.Brand{Name: "MCP fixture store", Theme: "forest"}, "summary": "MCP preview only"}})
		ok(w)
		var proposal storage.AITask
		if db.Where("source_connection_id = ? AND kind = ? AND status = ?", id, "operation", "awaiting_confirmation").First(&proposal).Error != nil {
			t.Fatal("MCP proposal not queued for review", w.Body.String())
		}
		ok(request("admin", "POST", "/admin/ai/tasks/"+proposal.ID+"/decision", map[string]string{"decision": "confirm"}))
		ok(request("admin", "POST", "/admin/ai/mcp-connections/"+id+"/revoke", map[string]any{}))
		db.First(&proposal, "id = ?", proposal.ID)
		if proposal.Status != "cancelled" {
			t.Fatal("revocation left approved task queued")
		}
		token, id = create(false, 365)
		db.Model(&storage.AIMCPConnection{}).Where("id = ?", id).Update("expires_at", time.Now().Add(-time.Hour))
		if mcp(token, "ping", map[string]any{}).Code != 401 {
			t.Fatal("expired token accepted")
		}
	})
	if preview != "" {
		if db.Model(&storage.AISchedule{}).Where("owner_id = ?", users["admin"].ID).Update("enabled", false).Error != nil {
			t.Fatal("disable fixture schedules before UI preview")
		}
		config := map[string]any{"database_dsn": cfg.FormatDSN(), "master_key": base64.StdEncoding.EncodeToString(key), "upload_dir": uploadDir, "public_url": "http://localhost:18089", "http_addr": "127.0.0.1:18089", "admin_email": users["admin"].Email}
		raw, _ := json.MarshalIndent(config, "", "  ")
		if os.WriteFile(preview, raw, 0600) != nil {
			t.Fatal("preview config")
		}
		t.Log("preview fixture written")
	}
}
