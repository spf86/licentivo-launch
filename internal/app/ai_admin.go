package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/ai"
	"licentivo-launch/internal/storage"
)

type AIProviderInput struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Protocol   string `json:"protocol"`
	Capability string `json:"capability"`
	BaseURL    string `json:"base_url"`
	Model      string `json:"model"`
	APIKey     string `json:"api_key"`
	Enabled    bool   `json:"enabled"`
}
type AIProviderView struct {
	Models           []ai.Model `json:"models"`
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Protocol         string     `json:"protocol"`
	Capability       string     `json:"capability"`
	BaseURL          string     `json:"base_url"`
	Model            string     `json:"model"`
	Enabled          bool       `json:"enabled"`
	CredentialStored bool       `json:"credential_stored"`
}

func aiProviderView(v storage.AIProvider) AIProviderView {
	return AIProviderView{Models: aiModels(v), ID: v.ID, Name: v.Name, Protocol: v.Protocol, Capability: v.Capability, BaseURL: v.BaseURL, Model: v.Model, Enabled: v.Enabled, CredentialStored: len(v.Secret) > 0}
}
func aiProviderFingerprint(v storage.AIProvider) string {
	return aiHash(marshal(struct {
		Protocol, Capability, BaseURL, Model string
		Secret                               []byte
	}{v.Protocol, v.Capability, v.BaseURL, v.Model, v.Secret}))
}
func (s *Service) aiClient() *ai.Client {
	if s.config.AIClient != nil {
		return s.config.AIClient
	}
	return s.aiTransport
}

func (s *Service) AIProviders(ctx context.Context, p Principal) ([]AIProviderView, error) {
	if !p.IsAdmin() {
		return nil, ErrForbidden
	}
	var rows []storage.AIProvider
	if err := s.db.WithContext(ctx).Order("updated_at,id").Find(&rows).Error; err != nil {
		return nil, ErrStorage
	}
	out := make([]AIProviderView, 0, len(rows))
	for _, v := range rows {
		out = append(out, aiProviderView(v))
	}
	return out, nil
}
func (s *Service) SaveAIProvider(ctx context.Context, p Principal, in AIProviderInput) (AIProviderView, error) {
	if !p.IsAdmin() {
		return AIProviderView{}, ErrForbidden
	}
	in.Name = strings.TrimSpace(in.Name)
	in.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	in.Model = strings.TrimSpace(in.Model)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 100 || len(in.APIKey) > 4096 || strings.ContainsAny(in.APIKey, "\r\n") {
		return AIProviderView{}, ErrInvalid
	}
	validationModel := in.Model
	if validationModel == "" && in.Capability == "chat" {
		validationModel = "catalog"
	}
	if err := ai.Validate(ai.Provider{Protocol: in.Protocol, Capability: in.Capability, BaseURL: in.BaseURL, Model: validationModel}); err != nil {
		return AIProviderView{}, problem(400, "AI_CONFIG_INVALID", err.Error())
	}
	var row storage.AIProvider
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if in.ID != "" {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", in.ID).Error; err != nil {
				return err
			}
			// Keeping a key while changing its destination could disclose it.
			if in.APIKey == "" && (row.BaseURL != in.BaseURL || row.Protocol != in.Protocol) {
				return problem(400, "AI_KEY_REQUIRED", "更换 API 地址或协议时请重新填写密钥")
			}
		} else {
			var err error
			row.ID, err = newID()
			if err != nil {
				return err
			}
		}
		if in.APIKey != "" {
			sealed, err := s.vault.Seal([]byte(in.APIKey), "ai-provider:"+row.ID)
			if err != nil {
				return err
			}
			row.Secret = sealed
		}
		if len(row.Secret) == 0 {
			return problem(400, "AI_KEY_REQUIRED", "请填写服务商 API Key")
		}
		if in.Model == "" {
			key, err := s.vault.Open(row.Secret, "ai-provider:"+row.ID)
			if err != nil {
				return ErrStorage
			}
			models, err := s.aiClient().Models(ctx, ai.Provider{Protocol: in.Protocol, Capability: in.Capability, BaseURL: in.BaseURL, Model: "catalog", Key: string(key)})
			if err != nil {
				return problem(502, "AI_MODEL_DISCOVERY_FAILED", err.Error())
			}
			row.Models = marshal(models)
			in.Model = models[0].ID
		} else {
			models := aiModels(row)
			if row.BaseURL != in.BaseURL || row.Protocol != in.Protocol || row.Capability != in.Capability {
				models = []ai.Model{}
			}
			found := false
			for _, m := range models {
				if m.ID == in.Model {
					found = true
				}
			}
			if !found {
				models = append(models, ai.Model{ID: in.Model, Name: in.Model})
			}
			row.Models = marshal(models)
		}
		row.Name, row.Protocol, row.Capability, row.BaseURL, row.Model, row.Enabled, row.UpdatedAt = in.Name, in.Protocol, in.Capability, in.BaseURL, in.Model, in.Enabled, now()
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "ai.provider.saved", "ai_provider", row.ID, "配置模型："+row.Name)
	})
	return aiProviderView(row), databaseError(err)
}

func (s *Service) aiProvider(ctx context.Context, id, capability string) (storage.AIProvider, ai.Provider, error) {
	var row storage.AIProvider
	q := s.db.WithContext(ctx).Where("enabled = ? AND capability = ?", true, capability)
	if id != "" {
		q = q.Where("id = ?", id)
	}
	if q.Order("updated_at,id").First(&row).Error != nil {
		return row, ai.Provider{}, problem(409, "AI_MODEL_UNAVAILABLE", "请先配置并启用对应能力的 AI 模型")
	}
	key, err := s.vault.Open(row.Secret, "ai-provider:"+row.ID)
	if err != nil {
		return row, ai.Provider{}, ErrStorage
	}
	return row, ai.Provider{Protocol: row.Protocol, Capability: row.Capability, BaseURL: row.BaseURL, Model: row.Model, Key: string(key)}, nil
}

type AIConversationView struct {
	Model      string    `json:"model"`
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	ProviderID string    `json:"provider_id"`
	UpdatedAt  time.Time `json:"updated_at"`
}
type AIMessageView struct {
	Files     []AIUploadView `json:"files"`
	ID        string         `json:"id"`
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	TaskID    string         `json:"task_id"`
	CreatedAt time.Time      `json:"created_at"`
}
type AIArtifactView struct {
	ID       string `json:"id"`
	TaskID   string `json:"task_id"`
	Filename string `json:"filename"`
	MIME     string `json:"mime"`
	Size     int64  `json:"size"`
	URL      string `json:"url"`
}
type AITaskView struct {
	CancelRequested bool            `json:"cancel_requested"`
	ID              string          `json:"id"`
	ConversationID  string          `json:"conversation_id"`
	Kind            string          `json:"kind"`
	Title           string          `json:"title"`
	Action          string          `json:"action"`
	Input           json.RawMessage `json:"input"`
	Result          json.RawMessage `json:"result"`
	Status          string          `json:"status"`
	Error           string          `json:"error"`
	Progress        int             `json:"progress"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

func aiTaskView(t storage.AITask) AITaskView {
	return AITaskView{t.CancelRequested, t.ID, t.ConversationID, t.Kind, t.Title, t.Action, t.Input, t.Result, t.Status, t.Error, t.Progress, t.CreatedAt, t.UpdatedAt}
}
func (s *Service) AITask(ctx context.Context, p Principal, id string) (map[string]any, error) {
	if !p.IsAdmin() {
		return nil, ErrForbidden
	}
	var task storage.AITask
	if err := s.db.WithContext(ctx).First(&task, "id = ? AND owner_id = ?", id, p.User.ID).Error; err != nil {
		return nil, databaseError(err)
	}
	var files []storage.AIArtifact
	if s.db.WithContext(ctx).Where("task_id = ? AND owner_id = ?", id, p.User.ID).Order("created_at,id").Find(&files).Error != nil {
		return nil, ErrStorage
	}
	views := []AIArtifactView{}
	for _, f := range files {
		views = append(views, aiArtifactView(f))
	}
	return map[string]any{"task": aiTaskView(task), "artifacts": views}, nil
}
func (s *Service) AIConversations(ctx context.Context, p Principal) ([]AIConversationView, error) {
	if !p.IsAdmin() {
		return nil, ErrForbidden
	}
	var rows []storage.AIConversation
	if s.db.WithContext(ctx).Where("owner_id = ? AND archived = ?", p.User.ID, false).Order("updated_at DESC,id").Limit(100).Find(&rows).Error != nil {
		return nil, ErrStorage
	}
	out := []AIConversationView{}
	for _, r := range rows {
		out = append(out, AIConversationView{r.Model, r.ID, r.Title, r.ProviderID, r.UpdatedAt})
	}
	return out, nil
}
func (s *Service) CreateAIConversation(ctx context.Context, p Principal, provider string) (AIConversationView, error) {
	if !p.IsAdmin() {
		return AIConversationView{}, ErrForbidden
	}
	capability := "chat"
	if provider != "" {
		var configured storage.AIProvider
		if err := s.db.WithContext(ctx).Select("id,capability").First(&configured, "id = ?", provider).Error; err != nil {
			return AIConversationView{}, databaseError(err)
		}
		capability = configured.Capability
	}
	row, _, err := s.aiProvider(ctx, provider, capability)
	if err != nil {
		return AIConversationView{}, err
	}
	id, err := newID()
	if err != nil {
		return AIConversationView{}, err
	}
	c := storage.AIConversation{ID: id, OwnerID: p.User.ID, ProviderID: row.ID, Title: "新对话", CreatedAt: now(), UpdatedAt: now()}
	return AIConversationView{c.Model, c.ID, c.Title, c.ProviderID, c.UpdatedAt}, databaseError(s.db.WithContext(ctx).Create(&c).Error)
}
func (s *Service) AIConversation(ctx context.Context, p Principal, id string) (map[string]any, error) {
	if !p.IsAdmin() {
		return nil, ErrForbidden
	}
	var c storage.AIConversation
	if err := s.db.WithContext(ctx).First(&c, "id = ? AND owner_id = ? AND archived = ?", id, p.User.ID, false).Error; err != nil {
		return nil, databaseError(err)
	}
	var msgs []storage.AIMessage
	var artifacts []storage.AIArtifact
	var tasks []storage.AITask
	db := s.db.WithContext(ctx)
	if db.Where("conversation_id = ?", id).Order("created_at DESC,id DESC").Limit(200).Find(&msgs).Error != nil || db.Where("conversation_id = ? AND owner_id = ?", id, p.User.ID).Order("created_at,id").Limit(200).Find(&artifacts).Error != nil || db.Where("conversation_id = ? AND owner_id = ?", id, p.User.ID).Order("created_at DESC,id DESC").Limit(100).Find(&tasks).Error != nil {
		return nil, ErrStorage
	}
	slices.Reverse(msgs)
	mv := []AIMessageView{}
	av := []AIArtifactView{}
	tv := []AITaskView{}
	for _, m := range msgs {
		mv = append(mv, AIMessageView{aiMessageFiles(m.Files), m.ID, m.Role, m.Content, m.TaskID, m.CreatedAt})
	}
	for _, a := range artifacts {
		av = append(av, aiArtifactView(a))
	}
	for _, t := range tasks {
		tv = append(tv, aiTaskView(t))
	}
	return map[string]any{"conversation": AIConversationView{c.Model, c.ID, c.Title, c.ProviderID, c.UpdatedAt}, "messages": mv, "artifacts": av, "tasks": tv}, nil
}

type AIChatInput struct {
	Model      string   `json:"model"`
	Files      []string `json:"files,omitempty"`
	Planning   bool     `json:"planning,omitempty"`
	Content    string   `json:"content"`
	ProviderID string   `json:"provider_id"`
	RequestKey string   `json:"request_key"`
}

func aiRequestKey(owner, key string) string {
	sum := sha256.Sum256([]byte(owner + "\x00" + key))
	return hex.EncodeToString(sum[:])
}
func (s *Service) SendAIMessage(ctx context.Context, p Principal, id string, in AIChatInput) (AITaskView, error) {
	if !p.IsAdmin() {
		return AITaskView{}, ErrForbidden
	}
	in.Content = strings.TrimSpace(in.Content)
	if (in.Content == "" && len(in.Files) == 0) || len(in.Content) > 16000 || len(in.RequestKey) < 16 || len(in.RequestKey) > 100 || len(in.Files) > 4 {
		return AITaskView{}, ErrInvalid
	}
	var task storage.AITask
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var c storage.AIConversation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&c, "id = ? AND owner_id = ? AND archived = ?", id, p.User.ID, false).Error; err != nil {
			return err
		}
		key := aiRequestKey(p.User.ID, in.RequestKey)
		err := tx.First(&task, "request_key = ?", key).Error
		if err == nil {
			var previous aiTurn
			if task.ConversationID != id || ai.Decode(task.Input, &previous) != nil || previous.Content != in.Content || !slices.Equal(previous.Files, in.Files) || (in.ProviderID != "" && in.ProviderID != task.ProviderID) || (in.Model != "" && in.Model != task.Model) || ((task.Kind == "planning") != in.Planning) {
				return problem(409, "AI_REQUEST_CONFLICT", "请求标识已用于其他消息")
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var pending int64
		if err := tx.Model(&storage.AITask{}).Where("conversation_id = ? AND kind IN ? AND status IN ?", id, []string{"chat", "planning", "plan"}, []string{"queued", "running"}).Count(&pending).Error; err != nil {
			return err
		}
		if pending > 0 {
			return problem(409, "AI_BUSY", "当前对话正在处理，请等待完成")
		}
		provider := in.ProviderID
		if provider == "" {
			provider = c.ProviderID
		}
		pr, selected, err := s.aiSelectedProvider(ctx, provider, "chat", in.Model)
		if err != nil {
			return err
		}
		taskID, err := newID()
		if err != nil {
			return err
		}
		title := string([]rune(in.Content)[:min(60, utf8.RuneCountInString(in.Content))])
		if title == "" {
			title = "分析上传文件"
		}
		files, err := s.aiBindUploads(tx, p, id, in.Files)
		if err != nil {
			return err
		}
		kind := "chat"
		if in.Planning {
			kind = "planning"
		}
		task = storage.AITask{ID: taskID, OwnerID: p.User.ID, ConversationID: id, ProviderID: pr.ID, Model: selected.Model, RequestKey: key, Kind: kind, Title: title, Input: marshal(aiTurn{Content: in.Content, Files: in.Files}), Result: marshal(map[string]any{}), Status: "queued", AvailableAt: now(), CreatedAt: now(), UpdatedAt: now()}
		if err := tx.Create(&task).Error; err != nil {
			return err
		}
		messageID, err := newID()
		if err != nil {
			return err
		}
		if err := tx.Create(&storage.AIMessage{ID: messageID, ConversationID: id, TaskID: task.ID, Role: "user", Content: in.Content, Files: marshal(files), CreatedAt: now()}).Error; err != nil {
			return err
		}
		updates := map[string]any{"updated_at": now(), "provider_id": pr.ID, "model": selected.Model}
		if c.Title == "新对话" {
			updates["title"] = title
		}
		if err := tx.Model(&c).Updates(updates).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "ai.chat.queued", "ai_task", task.ID, title)
	})
	return aiTaskView(task), databaseError(err)
}

func (s *Service) AITasks(ctx context.Context, p Principal, status string, limit, offset int, search ...string) (map[string]any, error) {
	if !p.IsAdmin() {
		return nil, ErrForbidden
	}
	if limit < 1 || limit > 100 || offset < 0 || offset > 1000000 {
		return nil, ErrInvalid
	}
	q := s.db.WithContext(ctx).Model(&storage.AITask{}).Where("owner_id = ?", p.User.ID)
	if len(search) > 0 && search[0] != "" {
		if len(search[0]) > 254 {
			return nil, ErrInvalid
		}
		q = q.Where("title LIKE ? ESCAPE '!'", "%"+literalSearch(strings.TrimSpace(search[0]))+"%")
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var count int64
	var rows []storage.AITask
	if q.Count(&count).Error != nil || q.Order("created_at DESC,id DESC").Limit(limit).Offset(offset).Find(&rows).Error != nil {
		return nil, ErrStorage
	}
	out := []AITaskView{}
	for _, t := range rows {
		out = append(out, aiTaskView(t))
	}
	var counts []struct {
		Status string
		Count  int64
	}
	if s.db.WithContext(ctx).Model(&storage.AITask{}).Select("status,COUNT(*) AS count").Where("owner_id = ?", p.User.ID).Group("status").Scan(&counts).Error != nil {
		return nil, ErrStorage
	}
	stats := map[string]int64{}
	for _, c := range counts {
		stats[c.Status] = c.Count
	}
	return map[string]any{"items": out, "total": count, "limit": limit, "offset": offset, "stats": stats}, nil
}
func (s *Service) DecideAITask(ctx context.Context, p Principal, id, decision string) (AITaskView, error) {
	if !p.IsAdmin() {
		return AITaskView{}, ErrForbidden
	}
	var t storage.AITask
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var initial storage.AITask
		if err := tx.Select("id,conversation_id").First(&initial, "id = ? AND owner_id = ?", id, p.User.ID).Error; err != nil {
			return err
		}
		var conversation storage.AIConversation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&conversation, "id = ? AND owner_id = ? AND archived = ?", initial.ConversationID, p.User.ID, false).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&t, "id = ? AND owner_id = ?", id, p.User.ID).Error; err != nil {
			return err
		}
		switch decision {
		case "confirm":
			if t.Status != "awaiting_confirmation" {
				return problem(409, "AI_TASK_STATE", "任务状态已变化，请刷新")
			}
			if time.Since(t.CreatedAt) > 24*time.Hour {
				return problem(409, "AI_PREVIEW_EXPIRED", "变更预览已过期，请重新提出请求")
			}
			if t.Kind == "plan" {
				var active int64
				if err := tx.Model(&storage.AITask{}).Where("conversation_id = ? AND kind IN ? AND status IN ?", t.ConversationID, []string{"chat", "plan", "planning"}, []string{"queued", "running"}).Count(&active).Error; err != nil {
					return err
				}
				if active > 0 {
					return problem(409, "AI_BUSY", "请等待当前对话完成后再执行计划")
				}
			}
			t.Status = "queued"
			t.AvailableAt = now()
		case "cancel":
			if t.Status != "awaiting_confirmation" && t.Status != "queued" {
				return problem(409, "AI_TASK_STATE", "该任务已经开始或结束，不能取消")
			}
			t.Status = "cancelled"
		case "stop":
			if t.Kind != "chat" && t.Kind != "planning" && t.Kind != "plan" {
				return problem(409, "AI_CANNOT_STOP", "该任务已提交给执行服务，请等待结果")
			}
			if t.Status != "running" && t.Status != "queued" {
				return problem(409, "AI_TASK_STATE", "任务已经结束，请刷新")
			}
			t.CancelRequested = true
			if t.Status == "queued" {
				t.Status = "cancelled"
			}
		default:
			return ErrInvalid
		}
		t.UpdatedAt = now()
		if err := tx.Save(&t).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "ai.task."+decision, "ai_task", id, t.Title)
	})
	return aiTaskView(t), databaseError(err)
}

func aiArtifactView(a storage.AIArtifact) AIArtifactView {
	return AIArtifactView{a.ID, a.TaskID, a.Filename, a.MIME, a.Size, "/api/v1/admin/ai/artifacts/" + a.ID}
}

type AIArtifactDownload struct {
	Filename, MIME string
	Data           []byte
}

func (s *Service) AIArtifact(ctx context.Context, p Principal, id string) (AIArtifactDownload, error) {
	if !p.IsAdmin() {
		return AIArtifactDownload{}, ErrForbidden
	}
	var a storage.AIArtifact
	if err := s.db.WithContext(ctx).First(&a, "id = ? AND owner_id = ?", id, p.User.ID).Error; err != nil {
		return AIArtifactDownload{}, databaseError(err)
	}
	if a.Path != a.ID+".bin" || !integrationUUIDPattern.MatchString(a.ID) {
		return AIArtifactDownload{}, ErrStorage
	}
	raw, err := os.ReadFile(filepath.Join(s.uploadDir, "ai-artifacts", a.Path))
	if err != nil {
		return AIArtifactDownload{}, problem(404, "AI_FILE_MISSING", "文件已不可用，请重新生成")
	}
	data, err := s.vault.Open(raw, "ai-artifact:"+a.ID)
	if err != nil {
		return AIArtifactDownload{}, ErrStorage
	}
	return AIArtifactDownload{a.Filename, a.MIME, data}, nil
}

type AIScheduleInput struct {
	Spec            AIScheduleSpec `json:"spec,omitempty"`
	Model           string         `json:"model,omitempty"`
	ID              string         `json:"id"`
	ConversationID  string         `json:"conversation_id"`
	ProviderID      string         `json:"provider_id"`
	Name            string         `json:"name"`
	Goal            string         `json:"goal"`
	IntervalMinutes int            `json:"interval_minutes"`
	Enabled         bool           `json:"enabled"`
}
type AIScheduleView struct {
	AIScheduleInput
	NextRunAt time.Time `json:"next_run_at"`
}

func (s *Service) AISchedules(ctx context.Context, p Principal) ([]AIScheduleView, error) {
	if !p.IsAdmin() {
		return nil, ErrForbidden
	}
	var rows []storage.AISchedule
	if s.db.WithContext(ctx).Where("owner_id = ?", p.User.ID).Order("updated_at DESC,id").Limit(100).Find(&rows).Error != nil {
		return nil, ErrStorage
	}
	out := []AIScheduleView{}
	for _, v := range rows {
		out = append(out, AIScheduleView{AIScheduleInput{Spec: aiScheduleSpec(v.Spec), Model: v.Model, ID: v.ID, ConversationID: v.ConversationID, ProviderID: v.ProviderID, Name: v.Name, Goal: v.Goal, IntervalMinutes: v.IntervalMinutes, Enabled: v.Enabled}, v.NextRunAt})
	}
	return out, nil
}
func (s *Service) SaveAISchedule(ctx context.Context, p Principal, in AIScheduleInput) (AIScheduleView, error) {
	if !p.IsAdmin() {
		return AIScheduleView{}, ErrForbidden
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Goal = strings.TrimSpace(in.Goal)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 100 || in.Goal == "" || len(in.Goal) > 8000 {
		return AIScheduleView{}, ErrInvalid
	}
	next, err := aiNextSchedule(in.Spec, in.IntervalMinutes, now())
	if err != nil && in.Spec.Frequency == "once" && !in.Enabled {
		next, err = time.Parse(time.RFC3339, in.Spec.OnceAt)
	}
	if err != nil {
		return AIScheduleView{}, problem(400, "AI_SCHEDULE_INVALID", "请填写有效的执行频率、时区和未来时间")
	}
	pr, selected, err := s.aiSelectedProvider(ctx, in.ProviderID, "chat", in.Model)
	if err != nil {
		return AIScheduleView{}, err
	}
	in.ProviderID, in.Model = pr.ID, selected.Model
	var row storage.AISchedule
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var c storage.AIConversation
		if err := tx.First(&c, "id = ? AND owner_id = ? AND archived = ?", in.ConversationID, p.User.ID, false).Error; err != nil {
			return err
		}
		if in.ID != "" {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ? AND owner_id = ?", in.ID, p.User.ID).Error; err != nil {
				return err
			}
		} else {
			var count int64
			if tx.Model(&storage.AISchedule{}).Where("owner_id = ?", p.User.ID).Count(&count).Error != nil {
				return ErrStorage
			}
			if count >= 100 {
				return problem(409, "AI_SCHEDULE_LIMIT", "最多保存100条定时任务")
			}
			var err error
			row.ID, err = newID()
			if err != nil {
				return err
			}
		}
		row.OwnerID, row.ConversationID, row.ProviderID, row.Name, row.Goal, row.IntervalMinutes, row.Enabled = p.User.ID, c.ID, in.ProviderID, in.Name, in.Goal, in.IntervalMinutes, in.Enabled
		row.Model, row.Spec = in.Model, marshal(in.Spec)
		row.NextRunAt = next
		row.UpdatedAt = now()
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "ai.schedule.saved", "ai_schedule", row.ID, row.Name)
	})
	in.ID = row.ID
	return AIScheduleView{in, row.NextRunAt}, databaseError(err)
}
