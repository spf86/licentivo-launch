package app

import (
	"context"
	"encoding/json"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/ai"
	"licentivo-launch/internal/storage"
)

func aiModels(row storage.AIProvider) []ai.Model {
	models := []ai.Model{}
	_ = json.Unmarshal(row.Models, &models)
	if len(models) == 0 && row.Model != "" {
		models = append(models, ai.Model{ID: row.Model, Name: row.Model})
	}
	return models
}
func aiSelectModel(row storage.AIProvider, model string) (string, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		model = row.Model
	}
	for _, m := range aiModels(row) {
		if m.ID == model {
			return model, nil
		}
	}
	return "", problem(400, "AI_MODEL_INVALID", "请从该服务的模型列表中选择模型，或先在模型配置中添加")
}
func (s *Service) aiSelectedProvider(ctx context.Context, id, capability, model string) (storage.AIProvider, ai.Provider, error) {
	row, p, err := s.aiProvider(ctx, id, capability)
	if err != nil {
		return row, p, err
	}
	p.Model, err = aiSelectModel(row, model)
	return row, p, err
}
func (s *Service) RefreshAIModels(ctx context.Context, p Principal, id string) (AIProviderView, error) {
	if !p.IsAdmin() {
		return AIProviderView{}, ErrForbidden
	}
	var row storage.AIProvider
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", id).Error; err != nil {
			return err
		}
		key, err := s.vault.Open(row.Secret, "ai-provider:"+row.ID)
		if err != nil {
			return ErrStorage
		}
		models, err := s.aiClient().Models(ctx, ai.Provider{Protocol: row.Protocol, Capability: row.Capability, BaseURL: row.BaseURL, Model: row.Model, Key: string(key)})
		if err != nil {
			return problem(502, "AI_MODEL_DISCOVERY_FAILED", err.Error())
		}
		row.Models = marshal(models)
		found := false
		for _, m := range models {
			if m.ID == row.Model {
				found = true
			}
		}
		if !found {
			row.Model = models[0].ID
		}
		row.UpdatedAt = now()
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "ai.models.refreshed", "ai_provider", id, "刷新模型列表："+row.Name)
	})
	return aiProviderView(row), databaseError(err)
}
