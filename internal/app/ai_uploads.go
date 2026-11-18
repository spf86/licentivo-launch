package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/ai"
	"licentivo-launch/internal/storage"
)

type AIUploadView struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	MIME     string `json:"mime"`
	Size     int64  `json:"size"`
	URL      string `json:"url"`
}

func aiUploadView(v storage.AIUpload) AIUploadView {
	return AIUploadView{v.ID, v.Filename, v.MIME, v.Size, "/api/v1/admin/ai/uploads/" + v.ID}
}

type aiTurn struct {
	Content string   `json:"content"`
	Files   []string `json:"files,omitempty"`
	Plan    *AIPlan  `json:"plan,omitempty"`
}

func (s *Service) SaveAIUpload(ctx context.Context, p Principal, name string, data []byte) (AIUploadView, error) {
	if !p.IsAdmin() {
		return AIUploadView{}, ErrForbidden
	}
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "" || len(name) > 180 || strings.ContainsAny(name, "\x00\r\n") {
		return AIUploadView{}, ErrInvalid
	}
	extracted, err := ai.Extract(ctx, name, data)
	if err != nil {
		return AIUploadView{}, problem(400, "AI_UPLOAD_INVALID", err.Error())
	}
	id, err := newID()
	if err != nil {
		return AIUploadView{}, err
	}
	raw, err := s.vault.Seal(data, "ai-upload:"+id)
	if err != nil {
		return AIUploadView{}, ErrStorage
	}
	text, err := s.vault.Seal([]byte(extracted.Text), "ai-upload-text:"+id)
	if err != nil {
		return AIUploadView{}, ErrStorage
	}
	dir := filepath.Join(s.uploadDir, "ai-uploads")
	if os.MkdirAll(dir, 0700) != nil {
		return AIUploadView{}, ErrStorage
	}
	path := id + ".bin"
	if os.WriteFile(filepath.Join(dir, path), raw, 0600) != nil {
		return AIUploadView{}, ErrStorage
	}
	row := storage.AIUpload{ID: id, OwnerID: p.User.ID, Filename: name, MIME: extracted.MIME, Size: int64(len(data)), Path: path, TextCipher: text, CreatedAt: now(), ExpiresAt: now().Add(24 * time.Hour)}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var owner storage.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&owner, "id = ?", p.User.ID).Error; err != nil {
			return err
		}
		var quota struct {
			Count int64
			Size  int64
		}
		if tx.Model(&storage.AIUpload{}).Select("COUNT(*) AS count,COALESCE(SUM(size),0) AS size").Where("owner_id = ? AND expires_at > ?", p.User.ID, now()).Scan(&quota).Error != nil {
			return ErrStorage
		}
		if quota.Count >= 100 || quota.Size+int64(len(data)) > 64<<20 {
			return problem(409, "AI_UPLOAD_QUOTA", "附件总量已达64MB，请等待旧附件过期后再上传")
		}
		return tx.Create(&row).Error
	}); err != nil {
		_ = os.Remove(filepath.Join(dir, path))
		return AIUploadView{}, databaseError(err)
	}
	return aiUploadView(row), nil
}
func (s *Service) AIUpload(ctx context.Context, p Principal, id string) (AIArtifactDownload, error) {
	if !p.IsAdmin() {
		return AIArtifactDownload{}, ErrForbidden
	}
	var row storage.AIUpload
	if err := s.db.WithContext(ctx).First(&row, "id = ? AND owner_id = ? AND expires_at > ?", id, p.User.ID, now()).Error; err != nil {
		return AIArtifactDownload{}, databaseError(err)
	}
	data, err := s.aiUploadBytes(row)
	if err != nil {
		return AIArtifactDownload{}, err
	}
	return AIArtifactDownload{row.Filename, row.MIME, data}, nil
}
func (s *Service) aiUploadBytes(row storage.AIUpload) ([]byte, error) {
	if !integrationUUIDPattern.MatchString(row.ID) || row.Path != row.ID+".bin" {
		return nil, ErrStorage
	}
	raw, err := os.ReadFile(filepath.Join(s.uploadDir, "ai-uploads", row.Path))
	if err != nil {
		return nil, problem(404, "AI_FILE_MISSING", "附件文件不存在，请重新上传")
	}
	data, err := s.vault.Open(raw, "ai-upload:"+row.ID)
	if err != nil {
		return nil, ErrStorage
	}
	return data, nil
}
func (s *Service) DiscardAIUpload(ctx context.Context, p Principal, id string) error {
	if !p.IsAdmin() {
		return ErrForbidden
	}
	var row storage.AIUpload
	if err := s.db.WithContext(ctx).First(&row, "id = ? AND owner_id = ? AND conversation_id = ?", id, p.User.ID, "").Error; err != nil {
		return databaseError(err)
	}
	if err := s.db.WithContext(ctx).Delete(&row).Error; err != nil {
		return ErrStorage
	}
	if row.Path == row.ID+".bin" && integrationUUIDPattern.MatchString(row.ID) {
		_ = os.Remove(filepath.Join(s.uploadDir, "ai-uploads", row.Path))
	}
	return nil
}
func (s *Service) aiBindUploads(tx *gorm.DB, p Principal, conversation string, ids []string) ([]AIUploadView, error) {
	files := []AIUploadView{}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return nil, ErrInvalid
		}
		seen[id] = true
		var row storage.AIUpload
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ? AND owner_id = ? AND expires_at > ? AND (conversation_id = ? OR conversation_id = ?)", id, p.User.ID, now(), "", conversation).Error; err != nil {
			return nil, databaseError(err)
		}
		if row.ConversationID == "" {
			if err := tx.Model(&row).Update("conversation_id", conversation).Error; err != nil {
				return nil, ErrStorage
			}
		}
		files = append(files, aiUploadView(row))
	}
	return files, nil
}
func (s *Service) aiInputMessage(ctx context.Context, task storage.AITask, in aiTurn) (ai.Message, error) {
	m := ai.Message{Role: "user", Content: in.Content}
	if len(in.Files) > 4 {
		return m, ErrInvalid
	}
	for _, id := range in.Files {
		var row storage.AIUpload
		if err := s.db.WithContext(ctx).First(&row, "id = ? AND owner_id = ? AND conversation_id = ? AND expires_at > ?", id, task.OwnerID, task.ConversationID, now()).Error; err != nil {
			return m, problem(409, "AI_FILE_EXPIRED", "附件已过期或不可用，请重新上传")
		}
		if strings.HasPrefix(row.MIME, "image/") {
			data, err := s.aiUploadBytes(row)
			if err != nil {
				return m, err
			}
			m.Images = append(m.Images, ai.Image{MIME: row.MIME, Data: data})
			m.Content += "\n附件图片：" + row.Filename
		} else {
			text, err := s.vault.Open(row.TextCipher, "ai-upload-text:"+row.ID)
			if err != nil {
				return m, ErrStorage
			}
			m.Content += "\n<untrusted_file name=" + string(marshal(row.Filename)) + ">\n" + string(text) + "\n</untrusted_file>"
		}
	}
	return m, nil
}
func aiMessageFiles(raw json.RawMessage) []AIUploadView {
	out := []AIUploadView{}
	_ = json.Unmarshal(raw, &out)
	return out
}
func (s *Service) cleanupAIUploads(ctx context.Context) {
	var rows []storage.AIUpload
	if s.db.WithContext(ctx).Where("expires_at <= ?", now()).Limit(100).Find(&rows).Error != nil {
		return
	}
	for _, row := range rows {
		if !integrationUUIDPattern.MatchString(row.ID) || row.Path != row.ID+".bin" {
			continue
		}
		if err := os.Remove(filepath.Join(s.uploadDir, "ai-uploads", row.Path)); err != nil && !os.IsNotExist(err) {
			continue
		}
		_ = s.db.WithContext(ctx).Delete(&row).Error
	}
}
