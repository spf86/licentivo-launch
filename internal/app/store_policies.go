package app

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"licentivo-launch/internal/storage"
)

var storePolicyKeys = []string{"terms", "refunds", "privacy"}

func storePolicyTitle(key string) string {
	return map[string]string{"terms": "服务条款", "refunds": "退款政策", "privacy": "隐私告知"}[key]
}

type PolicyContent struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type storePolicyRecord struct {
	Draft            PolicyContent  `json:"draft"`
	Version          uint64         `json:"version"`
	UpdatedAt        *time.Time     `json:"updated_at,omitempty"`
	Published        *PolicyContent `json:"published,omitempty"`
	PublishedVersion uint64         `json:"published_version"`
	PublishedAt      *time.Time     `json:"published_at,omitempty"`
}

type StorePolicyView struct {
	Key string `json:"key"`
	storePolicyRecord
	HasDraftChanges bool `json:"has_draft_changes"`
}

type PublishedPolicy struct {
	Key string `json:"key"`
	PolicyContent
	Version     uint64    `json:"version"`
	PublishedAt time.Time `json:"published_at"`
}

type PolicyInput struct {
	PolicyContent
	Version uint64 `json:"version"`
}

func readStorePolicies(raw []byte) (map[string]storePolicyRecord, error) {
	rows := map[string]storePolicyRecord{}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, ErrStorage
		}
	}
	if rows == nil {
		rows = map[string]storePolicyRecord{}
	}
	for _, key := range storePolicyKeys {
		row := rows[key]
		if row.Draft.Title == "" {
			row.Draft.Title = storePolicyTitle(key)
		}
		rows[key] = row
	}
	return rows, nil
}

func publishedStorePolicies(raw []byte) ([]PublishedPolicy, error) {
	rows, err := readStorePolicies(raw)
	if err != nil {
		return nil, err
	}
	result := []PublishedPolicy{}
	for _, key := range storePolicyKeys {
		row := rows[key]
		if row.Published != nil && row.PublishedAt != nil {
			result = append(result, PublishedPolicy{Key: key, PolicyContent: *row.Published, Version: row.PublishedVersion, PublishedAt: *row.PublishedAt})
		}
	}
	return result, nil
}

func storePolicyView(key string, row storePolicyRecord) StorePolicyView {
	return StorePolicyView{Key: key, storePolicyRecord: row, HasDraftChanges: row.Version > row.PublishedVersion}
}

func (s *Service) StorePolicies(ctx context.Context, p Principal) ([]StorePolicyView, error) {
	if !p.IsAdmin() {
		return nil, ErrForbidden
	}
	var settings storage.InstanceSettings
	if err := s.db.WithContext(ctx).Select("store_policies").First(&settings, "id = 1").Error; err != nil {
		return nil, ErrStorage
	}
	rows, err := readStorePolicies(settings.StorePolicies)
	if err != nil {
		return nil, err
	}
	views := []StorePolicyView{}
	for _, key := range storePolicyKeys {
		views = append(views, storePolicyView(key, rows[key]))
	}
	return views, nil
}

func (s *Service) PublishedPolicy(ctx context.Context, key string) (PublishedPolicy, error) {
	if storePolicyTitle(key) == "" {
		return PublishedPolicy{}, ErrNotFound
	}
	var settings storage.InstanceSettings
	if err := s.db.WithContext(ctx).Select("store_policies").First(&settings, "id = 1").Error; err != nil {
		return PublishedPolicy{}, ErrStorage
	}
	policies, err := publishedStorePolicies(settings.StorePolicies)
	if err != nil {
		return PublishedPolicy{}, err
	}
	for _, policy := range policies {
		if policy.Key == key {
			return policy, nil
		}
	}
	return PublishedPolicy{}, ErrNotFound
}

func (s *Service) SaveStorePolicy(ctx context.Context, p Principal, key string, in PolicyInput, publish bool) (StorePolicyView, error) {
	if !p.IsAdmin() {
		return StorePolicyView{}, ErrForbidden
	}
	in.Title, in.Body = strings.TrimSpace(in.Title), strings.TrimSpace(in.Body)
	if storePolicyTitle(key) == "" || !utf8.ValidString(in.Title) || !utf8.ValidString(in.Body) || in.Title == "" || utf8.RuneCountInString(in.Title) > 160 || utf8.RuneCountInString(in.Body) > 16000 {
		return StorePolicyView{}, ErrInvalid
	}
	if publish && in.Body == "" {
		return StorePolicyView{}, problem(400, "POLICY_CONTENT_REQUIRED", "请填写正文内容后再发布")
	}
	var view StorePolicyView
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var settings storage.InstanceSettings
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&settings, "id = 1").Error; err != nil {
			return err
		}
		rows, err := readStorePolicies(settings.StorePolicies)
		if err != nil {
			return err
		}
		row := rows[key]
		if row.Version != in.Version {
			return problem(409, "POLICY_VERSION_CONFLICT", "该政策已被其他管理员更新，请复制当前内容后刷新页面再保存")
		}
		at := now()
		if row.Version == 0 || row.Draft != in.PolicyContent {
			row.Draft = in.PolicyContent
			row.Version++
			row.UpdatedAt = &at
		}
		action := "store_policy.draft"
		if publish {
			content := row.Draft
			row.Published = &content
			row.PublishedVersion = row.Version
			row.PublishedAt = &at
			action = "store_policy.publish"
		}
		rows[key] = row
		if err := tx.Model(&settings).Update("store_policies", marshal(rows)).Error; err != nil {
			return err
		}
		if err := audit(tx, p.User.ID, action, "store_policy", key, strconv.FormatUint(row.Version, 10)); err != nil {
			return err
		}
		view = storePolicyView(key, row)
		return nil
	})
	return view, databaseError(err)
}
