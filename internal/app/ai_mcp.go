package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/ai"
	"licentivo-launch/internal/security"
	"licentivo-launch/internal/storage"
)

type AIMCPView struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	ConversationID string     `json:"conversation_id"`
	AllowProposals bool       `json:"allow_proposals"`
	CreatedAt      time.Time  `json:"created_at"`
	ExpiresAt      *time.Time `json:"expires_at"`
	RevokedAt      *time.Time `json:"revoked_at"`
}
type AIMCPInput struct {
	Name           string `json:"name"`
	Days           *int   `json:"days"`
	AllowProposals bool   `json:"allow_proposals"`
}

func aiMCPView(c storage.AIMCPConnection) AIMCPView {
	return AIMCPView{c.ID, c.Name, c.ConversationID, c.AllowProposals, c.CreatedAt, c.ExpiresAt, c.RevokedAt}
}
func (s *Service) AIMCPConnections(ctx context.Context, p Principal) (map[string]any, error) {
	if !p.IsAdmin() {
		return nil, ErrForbidden
	}
	var rows []storage.AIMCPConnection
	if s.db.WithContext(ctx).Where("owner_id = ?", p.User.ID).Order("created_at DESC,id").Limit(100).Find(&rows).Error != nil {
		return nil, ErrStorage
	}
	items := []AIMCPView{}
	for _, row := range rows {
		items = append(items, aiMCPView(row))
	}
	return map[string]any{"endpoint": strings.TrimRight(s.PublicURL(), "/") + "/mcp", "items": items}, nil
}
func (s *Service) CreateAIMCPConnection(ctx context.Context, p Principal, in AIMCPInput) (map[string]any, error) {
	if !p.IsAdmin() {
		return nil, ErrForbidden
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 100 || in.Days == nil || *in.Days < 0 || *in.Days > 365 {
		return nil, ErrInvalid
	}
	secret, err := security.Token()
	if err != nil {
		return nil, err
	}
	token := "mcp_" + secret
	id, err := newID()
	if err != nil {
		return nil, err
	}
	conversation, err := newID()
	if err != nil {
		return nil, err
	}
	at := now()
	var expiresAt *time.Time
	if *in.Days > 0 {
		expiry := at.AddDate(0, 0, *in.Days)
		expiresAt = &expiry
	}
	row := storage.AIMCPConnection{ID: id, OwnerID: p.User.ID, ConversationID: conversation, Name: in.Name, AllowProposals: in.AllowProposals, TokenHash: security.Digest(token), CreatedAt: at, ExpiresAt: expiresAt}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Serialize the active-connection limit for simultaneous creates by one owner.
		var owner storage.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&owner, "id = ?", p.User.ID).Error; err != nil {
			return err
		}
		var count int64
		if tx.Model(&storage.AIMCPConnection{}).Where("owner_id = ? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)", p.User.ID, at).Count(&count).Error != nil {
			return ErrStorage
		}
		if count >= 20 {
			return problem(409, "AI_MCP_LIMIT", "最多保留20个有效连接，请先撤销不用的连接")
		}
		if err := tx.Create(&storage.AIConversation{ID: conversation, OwnerID: p.User.ID, Title: "MCP · " + in.Name, CreatedAt: at, UpdatedAt: at}).Error; err != nil {
			return err
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "ai.mcp.created", "ai_mcp_connection", id, "创建外部 AI 连接："+in.Name)
	})
	if err != nil {
		return nil, databaseError(err)
	}
	return map[string]any{"connection": aiMCPView(row), "token": token, "endpoint": strings.TrimRight(s.PublicURL(), "/") + "/mcp"}, nil
}
func (s *Service) RevokeAIMCPConnection(ctx context.Context, p Principal, id string) error {
	if !p.IsAdmin() {
		return ErrForbidden
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row storage.AIMCPConnection
		if err := tx.First(&row, "id = ? AND owner_id = ?", id, p.User.ID).Error; err != nil {
			return err
		}
		if row.RevokedAt != nil {
			return nil
		}
		at := now()
		if err := tx.Model(&row).Update("revoked_at", at).Error; err != nil {
			return err
		}
		if err := tx.Model(&storage.AITask{}).Where("source_connection_id = ? AND status IN ?", id, []string{"queued", "awaiting_confirmation"}).Updates(map[string]any{"status": "cancelled", "updated_at": at}).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "ai.mcp.revoked", "ai_mcp_connection", id, "撤销外部 AI 连接："+row.Name)
	}))
}
func (s *Service) AuthenticateAIMCP(ctx context.Context, token string) (storage.AIMCPConnection, Principal, error) {
	var row storage.AIMCPConnection
	if len(token) != 47 || !strings.HasPrefix(token, "mcp_") {
		return row, Principal{}, ErrForbidden
	}
	if s.db.WithContext(ctx).First(&row, "token_hash = ? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)", security.Digest(token), now()).Error != nil {
		return row, Principal{}, ErrForbidden
	}
	p, err := s.aiActor(ctx, row.OwnerID)
	return row, p, err
}
func (s *Service) aiCheckMCP(ctx context.Context, id, owner string) error {
	var count int64
	if s.db.WithContext(ctx).Model(&storage.AIMCPConnection{}).Where("id = ? AND owner_id = ? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?) AND allow_proposals = ?", id, owner, now(), true).Count(&count).Error != nil {
		return ErrStorage
	}
	if count != 1 {
		return problem(403, "AI_MCP_REVOKED", "外部 AI 连接已失效或操作权限已撤销")
	}
	return nil
}
func (s *Service) AIMCPTools(c storage.AIMCPConnection) []map[string]any {
	out := []map[string]any{}
	for _, tool := range s.aiTools(false) {
		if tool.Name != "query_system" && tool.Name != "management_help" && !(c.AllowProposals && tool.Name == "propose_change") {
			continue
		}
		out = append(out, map[string]any{"name": tool.Name, "description": tool.Description, "inputSchema": tool.Parameters, "annotations": map[string]bool{"readOnlyHint": tool.Name != "propose_change", "destructiveHint": false, "openWorldHint": false}})
	}
	return append(out, map[string]any{"name": "get_task", "description": "核对本连接提出的任务及执行结果；系统变更必须由管理员在 Launch 后台确认。", "inputSchema": aiObject(map[string]any{"id": aiText()}, "id"), "annotations": map[string]bool{"readOnlyHint": true, "openWorldHint": false}})
}
func (s *Service) CallAIMCP(ctx context.Context, c storage.AIMCPConnection, p Principal, name string, args json.RawMessage) (any, error) {
	// Authenticate again immediately before every tool; tokens never carry a role snapshot.
	if !p.IsAdmin() || p.User.ID != c.OwnerID {
		return nil, ErrForbidden
	}
	var current storage.AIMCPConnection
	if s.db.WithContext(ctx).First(&current, "id = ? AND owner_id = ? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)", c.ID, p.User.ID, now()).Error != nil {
		return nil, ErrForbidden
	}
	if _, err := s.aiActor(ctx, p.User.ID); err != nil {
		return nil, err
	}
	allowed := false
	for _, tool := range s.AIMCPTools(current) {
		if tool["name"] == name {
			allowed = true
		}
	}
	if !allowed {
		return nil, ErrForbidden
	}
	if s.RecoveryRequired(ctx) {
		return nil, problem(409, "AI_RECOVERY_REQUIRED", "请先完成实例恢复检查")
	}
	if name == "get_task" {
		var in struct {
			ID string `json:"id"`
		}
		if ai.Decode(args, &in) != nil {
			return nil, ErrInvalid
		}
		var task storage.AITask
		if err := s.db.WithContext(ctx).First(&task, "id = ? AND owner_id = ? AND source_connection_id = ?", in.ID, p.User.ID, c.ID).Error; err != nil {
			return nil, databaseError(err)
		}
		if err := s.aiRecordTool(ctx, p.User.ID, task.ID, "mcp_get_task", false, "查询本连接任务"); err != nil {
			return nil, err
		}
		return aiTaskView(task), nil
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	lease := now().Add(2 * time.Minute)
	task := storage.AITask{ID: id, OwnerID: p.User.ID, ConversationID: c.ConversationID, SourceConnectionID: c.ID, RequestKey: id, Kind: "mcp", Title: "MCP · " + name, Input: marshal(map[string]any{"tool": name}), Result: marshal(map[string]any{}), Status: "running", RunID: id, LeaseUntil: &lease, AvailableAt: now(), CreatedAt: now(), UpdatedAt: now()}
	if s.db.WithContext(ctx).Create(&task).Error != nil {
		return nil, ErrStorage
	}
	call := ai.Call{ID: id, Type: "function"}
	call.Function.Name = name
	call.Function.Arguments = string(args)
	value, toolErr := s.aiExecuteTool(ctx, p, task, call)
	summary := "外部 AI 调用：" + name
	status := "completed"
	message := ""
	if toolErr != nil {
		status = "failed"
		message = s.aiError(toolErr)
		summary = message
	}
	// Do not retain bulk customer/query payloads in task results.
	result := map[string]any{"tool": name, "status": status}
	if name == "propose_change" && toolErr == nil {
		result["proposal"] = value
	}
	if err := s.db.WithContext(ctx).Model(&task).Where("status = ? AND run_id = ?", "running", id).Updates(map[string]any{"status": status, "lease_until": nil, "progress": 100, "result": marshal(result), "error": message, "updated_at": now()}).Error; err != nil {
		return nil, ErrStorage
	}
	if err := s.aiRecordTool(ctx, p.User.ID, id, name, toolErr != nil, summary); err != nil {
		return nil, err
	}
	return value, toolErr
}
