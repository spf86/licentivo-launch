package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/ai"
	"licentivo-launch/internal/storage"
)

type AIPlanStep struct {
	Title  string `json:"title"`
	Tool   string `json:"tool"`
	Action string `json:"action,omitempty"`
}
type AIPlan struct {
	Title string       `json:"title"`
	Goal  string       `json:"goal"`
	Steps []AIPlanStep `json:"steps"`
}

var errAIStopped = errors.New("任务已停止；已完成的结果和待确认事项已保留")

func (s *Service) aiValidatePlan(plan AIPlan) error {
	if strings.TrimSpace(plan.Title) == "" || utf8.RuneCountInString(plan.Title) > 120 || strings.TrimSpace(plan.Goal) == "" || len(plan.Goal) > 16000 || len(plan.Steps) < 1 || len(plan.Steps) > 12 {
		return ErrInvalid
	}
	available := map[string]bool{}
	for _, t := range s.aiTools(false) {
		available[t.Name] = true
	}
	for _, step := range plan.Steps {
		if strings.TrimSpace(step.Title) == "" || utf8.RuneCountInString(step.Title) > 180 || !available[step.Tool] {
			return ErrInvalid
		}
		if step.Tool == "propose_change" {
			if _, ok := s.aiActions()[step.Action]; !ok {
				return ErrInvalid
			}
		} else if step.Action != "" {
			return ErrInvalid
		}
	}
	return nil
}
func (s *Service) aiProposePlan(ctx context.Context, p Principal, parent storage.AITask, plan AIPlan) (any, error) {
	if parent.Kind != "planning" || parent.ReadOnly {
		return nil, ErrForbidden
	}
	if err := s.aiValidatePlan(plan); err != nil {
		return nil, err
	}
	var input aiTurn
	if ai.Decode(parent.Input, &input) != nil {
		return nil, ErrInvalid
	}
	input.Content = plan.Goal
	input.Plan = &plan
	id, err := newID()
	if err != nil {
		return nil, err
	}
	task := storage.AITask{ID: id, OwnerID: p.User.ID, ConversationID: parent.ConversationID, ProviderID: parent.ProviderID, Model: parent.Model, RequestKey: id, Kind: "plan", Title: plan.Title, Input: marshal(input), Result: marshal(map[string]any{}), Status: "awaiting_confirmation", AvailableAt: now(), CreatedAt: now(), UpdatedAt: now()}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var c storage.AIConversation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&c, "id = ? AND owner_id = ? AND archived = ?", parent.ConversationID, p.User.ID, false).Error; err != nil {
			return err
		}
		if err := tx.Create(&task).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "ai.plan.proposed", "ai_task", id, plan.Title)
	})
	return map[string]any{"task_id": id, "status": "awaiting_confirmation", "plan": plan, "message": "请先审阅并确认计划；系统变更和媒体生成仍会分别请求确认。"}, databaseError(err)
}
func (s *Service) aiToolsForTask(task storage.AITask) []ai.Tool {
	tools := s.aiTools(task.ReadOnly)
	if task.Kind == "planning" {
		out := []ai.Tool{}
		for _, t := range tools {
			if t.Name == "query_system" || t.Name == "management_help" {
				out = append(out, t)
			}
		}
		return append(out, ai.Tool{Name: "propose_plan", Description: "生成分步骤的管理计划，等待管理员确认后再执行。每步指定现有工具名称；propose_change 必须填写 management_help 中的 action。只规划，不创建文件或变更。", Parameters: aiTypeSchema[AIPlan]()})
	}
	if task.Kind == "plan" {
		var in aiTurn
		if ai.Decode(task.Input, &in) != nil || in.Plan == nil {
			return nil
		}
		allowed := map[string]bool{"query_system": true, "management_help": true}
		for _, step := range in.Plan.Steps {
			allowed[step.Tool] = true
		}
		out := []ai.Tool{}
		for _, t := range tools {
			if allowed[t.Name] {
				out = append(out, t)
			}
		}
		return out
	}
	return tools
}
func (s *Service) aiToolAllowed(task storage.AITask, call ai.Call) bool {
	allowed := false
	for _, t := range s.aiToolsForTask(task) {
		if t.Name == call.Function.Name {
			allowed = true
		}
	}
	if !allowed {
		return false
	}
	if task.Kind == "plan" && call.Function.Name == "propose_change" {
		var in aiTurn
		var proposal aiProposal
		if ai.Decode(task.Input, &in) != nil || in.Plan == nil || ai.Decode([]byte(call.Function.Arguments), &proposal) != nil {
			return false
		}
		for _, step := range in.Plan.Steps {
			if step.Tool == "propose_change" && step.Action == proposal.Action {
				return true
			}
		}
		return false
	}
	return true
}

type AIConversationCommand struct {
	Action string `json:"action"`
	Title  string `json:"title,omitempty"`
}

func (s *Service) ChangeAIConversation(ctx context.Context, p Principal, id string, in AIConversationCommand) error {
	if !p.IsAdmin() {
		return ErrForbidden
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var c storage.AIConversation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&c, "id = ? AND owner_id = ? AND archived = ?", id, p.User.ID, false).Error; err != nil {
			return err
		}
		switch in.Action {
		case "rename":
			in.Title = strings.TrimSpace(in.Title)
			if in.Title == "" || utf8.RuneCountInString(in.Title) > 160 {
				return ErrInvalid
			}
			c.Title = in.Title
		case "archive":
			c.Archived = true
			if err := tx.Model(&storage.AIMCPConnection{}).Where("conversation_id = ? AND revoked_at IS NULL", id).Update("revoked_at", now()).Error; err != nil {
				return err
			}
			if err := tx.Model(&storage.AITask{}).Where("conversation_id = ? AND status IN ?", id, []string{"queued", "awaiting_confirmation"}).Updates(map[string]any{"status": "cancelled", "updated_at": now()}).Error; err != nil {
				return err
			}
			if err := tx.Model(&storage.AITask{}).Where("conversation_id = ? AND kind IN ? AND status = ?", id, []string{"chat", "planning", "plan"}, "running").Update("cancel_requested", true).Error; err != nil {
				return err
			}
			if err := tx.Model(&storage.AISchedule{}).Where("conversation_id = ?", id).Update("enabled", false).Error; err != nil {
				return err
			}
		default:
			return ErrInvalid
		}
		c.UpdatedAt = now()
		if err := tx.Save(&c).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "ai.conversation."+in.Action, "ai_conversation", id, c.Title)
	}))
}

type AISteerInput struct {
	Content    string `json:"content"`
	RequestKey string `json:"request_key"`
}

func (s *Service) SteerAIConversation(ctx context.Context, p Principal, id string, in AISteerInput) (AITaskView, error) {
	if !p.IsAdmin() {
		return AITaskView{}, ErrForbidden
	}
	in.Content = strings.TrimSpace(in.Content)
	if in.Content == "" || len(in.Content) > 8000 || len(in.RequestKey) < 16 || len(in.RequestKey) > 100 {
		return AITaskView{}, ErrInvalid
	}
	var task storage.AITask
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var c storage.AIConversation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&c, "id = ? AND owner_id = ? AND archived = ?", id, p.User.ID, false).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("conversation_id = ? AND kind IN ? AND status IN ?", id, []string{"chat", "planning", "plan"}, []string{"queued", "running"}).Order("created_at,id").First(&task).Error; err != nil {
			return err
		}
		var notes []aiSteering
		if len(task.Steering) > 0 && json.Unmarshal(task.Steering, &notes) != nil {
			return ErrStorage
		}
		key := aiRequestKey(p.User.ID, in.RequestKey)
		for _, note := range notes {
			if note.Key == key {
				if note.Content != in.Content {
					return problem(409, "AI_REQUEST_CONFLICT", "消息标识已被使用")
				}
				return nil
			}
		}
		if len(notes) >= 20 {
			return problem(409, "AI_STEERING_LIMIT", "补充消息较多，请等待当前任务结束")
		}
		notes = append(notes, aiSteering{key, in.Content})
		if task.CancelRequested {
			return problem(409, "AI_STOPPING", "任务正在停止，请稍后新建请求")
		}
		if err := tx.Model(&task).Updates(map[string]any{"steering": marshal(notes), "updated_at": now()}).Error; err != nil {
			return err
		}
		mid, err := newID()
		if err != nil {
			return err
		}
		if err := tx.Create(&storage.AIMessage{ID: mid, ConversationID: id, TaskID: task.ID, Role: "user", Content: in.Content, CreatedAt: now()}).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "ai.chat.steered", "ai_task", task.ID, "已补充任务要求")
	})
	return aiTaskView(task), databaseError(err)
}

type aiSteering struct {
	Key     string `json:"key"`
	Content string `json:"content"`
}

func (s *Service) aiTaskControl(ctx context.Context, id string) (storage.AITask, []aiSteering, error) {
	var task storage.AITask
	if err := s.db.WithContext(ctx).First(&task, "id = ?", id).Error; err != nil {
		return task, nil, ErrStorage
	}
	if task.CancelRequested || task.Status == "cancelled" {
		return task, nil, errAIStopped
	}
	var notes []aiSteering
	if len(task.Steering) > 0 && json.Unmarshal(task.Steering, &notes) != nil {
		return task, nil, ErrStorage
	}
	return task, notes, nil
}
