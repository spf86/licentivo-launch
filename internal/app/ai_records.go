package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"licentivo-launch/internal/storage"
)

type AIOperationQuery struct {
	Query, Kind, Status, From, To string
	Limit, Offset                 int
}
type AIOperationView struct {
	ID             string    `json:"id"`
	Actor          string    `json:"actor"`
	Action         string    `json:"action"`
	Label          string    `json:"label"`
	Summary        string    `json:"summary"`
	Status         string    `json:"status"`
	ObjectID       string    `json:"object_id"`
	ConversationID string    `json:"conversation_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

var aiRecordLabels = map[string]string{
	"ai.models.refreshed": "刷新模型列表", "ai.plan.proposed": "生成任务计划", "ai.tool.propose_plan": "规划执行步骤", "ai.chat.steered": "补充任务要求", "ai.task.stop": "请求停止任务", "ai.task.cancelled": "任务已停止", "ai.conversation.rename": "重命名会话", "ai.conversation.archive": "归档会话", "ai.mcp.created": "创建 MCP 连接", "ai.mcp.revoked": "撤销 MCP 连接", "ai.tool.mcp_get_task": "MCP 查询任务",
	"ai.provider.saved": "保存模型配置", "ai.schedule.saved": "保存定时任务", "ai.change.proposed": "提出系统变更", "ai.media.proposed": "提出媒体生成任务", "ai.chat.queued": "发送 AI 消息",
	"ai.task.confirm": "确认执行任务", "ai.task.cancel": "取消任务", "ai.task.completed": "任务执行完成", "ai.task.failed": "任务执行失败",
	"ai.task.interrupted": "任务中断待核对", "ai.task.resumed": "恢复视频任务查询",
	"ai.tool.query_system": "查询系统数据", "ai.tool.management_help": "读取管理操作说明", "ai.tool.propose_change": "准备系统变更",
	"ai.tool.generate_excel": "生成 Excel", "ai.tool.generate_word": "生成 Word", "ai.tool.generate_presentation": "生成 PowerPoint", "ai.tool.generate_image": "准备图片生成", "ai.tool.generate_video": "准备视频生成",
}

func (s *Service) aiRecordTool(ctx context.Context, actor, taskID, tool string, failed bool, summary string) error {
	id, err := newID()
	if err != nil {
		return err
	}
	request, err := newID()
	if err != nil {
		return err
	}
	status := "completed"
	if failed {
		status = "failed"
	}
	if len(summary) > 450 {
		summary = string([]rune(summary)[:min(140, len([]rune(summary)))])
	}
	row := storage.AuditEntry{ID: id, ActorID: &actor, Action: "ai.tool." + tool, ObjectType: "ai_task", ObjectID: taskID, RequestID: request, Reason: &summary, SafeMetadata: marshal(map[string]string{"status": status}), CreatedAt: now()}
	return databaseError(s.db.WithContext(ctx).Create(&row).Error)
}

func (s *Service) AIOperations(ctx context.Context, p Principal, in AIOperationQuery) (map[string]any, error) {
	if !p.IsAdmin() {
		return nil, ErrForbidden
	}
	if in.Limit < 1 || in.Limit > 100 || in.Offset < 0 || in.Offset > 1000000 || len(in.Query) > 254 {
		return nil, ErrInvalid
	}
	q := s.db.WithContext(ctx).Table("audit_entries AS a").Joins("LEFT JOIN users AS u ON u.id = a.actor_id").Joins("LEFT JOIN ai_tasks AS t ON a.object_type = 'ai_task' AND t.id = a.object_id").Where("a.action LIKE ?", "ai.%")
	if in.Query != "" {
		pattern := "%" + literalSearch(strings.ToLower(strings.TrimSpace(in.Query))) + "%"
		q = q.Where("(LOWER(u.email) LIKE ? ESCAPE '!' OR LOWER(a.reason) LIKE ? ESCAPE '!' OR LOWER(a.action) LIKE ? ESCAPE '!')", pattern, pattern, pattern)
	}
	switch in.Kind {
	case "":
	case "tools":
		q = q.Where("a.action LIKE ?", "ai.tool.%")
	case "tasks":
		q = q.Where("a.action LIKE ? OR a.action IN ?", "ai.task.%", []string{"ai.change.proposed", "ai.media.proposed", "ai.plan.proposed", "ai.chat.queued", "ai.chat.steered"})
	case "settings":
		q = q.Where("a.action IN ?", []string{"ai.provider.saved", "ai.models.refreshed", "ai.schedule.saved", "ai.mcp.created", "ai.mcp.revoked"})
	default:
		return nil, ErrInvalid
	}
	statusSQL := "CASE WHEN a.action = 'ai.task.failed' THEN 'failed' WHEN a.action = 'ai.task.interrupted' THEN 'unknown' WHEN a.action = 'ai.task.resumed' THEN 'submitted' WHEN a.action IN ('ai.task.cancel','ai.task.cancelled') THEN 'cancelled' WHEN a.action IN ('ai.change.proposed','ai.media.proposed','ai.plan.proposed') THEN 'awaiting_confirmation' WHEN a.action IN ('ai.task.confirm','ai.chat.queued','ai.task.stop','ai.chat.steered') THEN 'queued' ELSE COALESCE(JSON_UNQUOTE(JSON_EXTRACT(a.safe_metadata, '$.status')), 'completed') END"
	if in.Status != "" {
		switch in.Status {
		case "completed", "failed", "cancelled", "awaiting_confirmation", "queued", "unknown", "submitted":
		default:
			return nil, ErrInvalid
		}
		q = q.Where("("+statusSQL+") = ?", in.Status)
	}
	for key, value := range map[string]string{"from": in.From, "to": in.To} {
		if value == "" {
			continue
		}
		at, err := time.Parse("2006-01-02", value)
		if err != nil {
			return nil, ErrInvalid
		}
		location, _ := time.LoadLocation("Asia/Shanghai")
		at = time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, location).UTC()
		if key == "from" {
			q = q.Where("a.created_at >= ?", at)
		} else {
			q = q.Where("a.created_at < ?", at.Add(24*time.Hour))
		}
	}
	var count int64
	if q.Count(&count).Error != nil {
		return nil, ErrStorage
	}
	var rows []struct {
		ID, Actor, Action, ObjectID     string
		Reason                          *string
		Status, ConversationID, OwnerID string
		CreatedAt                       time.Time
		Metadata                        json.RawMessage
	}
	if q.Select("a.id,COALESCE(u.email,'系统') AS actor,a.action,a.object_id,a.reason,"+statusSQL+" AS status,COALESCE(t.conversation_id,'') AS conversation_id,COALESCE(t.owner_id,'') AS owner_id,a.created_at").Order("a.created_at DESC,a.id DESC").Limit(in.Limit).Offset(in.Offset).Scan(&rows).Error != nil {
		return nil, ErrStorage
	}
	out := []AIOperationView{}
	for _, r := range rows {
		label := aiRecordLabels[r.Action]
		if label == "" {
			label = "AI 工具调用"
		}
		reason := ""
		if r.Reason != nil {
			reason = *r.Reason
		}
		conversation := ""
		if r.OwnerID == p.User.ID {
			conversation = r.ConversationID
		}
		out = append(out, AIOperationView{r.ID, r.Actor, r.Action, label, reason, r.Status, r.ObjectID, conversation, r.CreatedAt})
	}
	return map[string]any{"items": out, "total": count, "limit": in.Limit, "offset": in.Offset}, nil
}
