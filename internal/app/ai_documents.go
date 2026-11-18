package app

import (
	"context"
	"strings"

	"gorm.io/gorm"
)

// Create a complete article in one confirmed transaction, using the same
// versioned document commands as the editor for every individual operation.
type aiCreateDocumentInput struct {
	ProductID  string      `json:"product_id"`
	GroupID    string      `json:"group_id,omitempty"`
	GroupTitle string      `json:"group_title,omitempty"`
	Title      string      `json:"title"`
	Slug       string      `json:"slug"`
	Body       string      `json:"body"`
	SEO        DocumentSEO `json:"seo"`
	Publish    bool        `json:"publish"`
}

func (s *Service) aiCreateDocument(ctx context.Context, p Principal, in aiCreateDocumentInput) (any, error) {
	if !p.IsAdmin() {
		return nil, ErrForbidden
	}
	if strings.TrimSpace(in.Body) == "" {
		return nil, ErrInvalid
	}
	var result DocumentLibraryView
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		scoped := &Service{db: tx, vault: s.vault, config: s.config, dummyHash: s.dummyHash, uploadDir: s.uploadDir, aiTransport: s.aiTransport}
		scoped.publicURL.Store(s.PublicURL())
		library, err := scoped.DocumentLibrary(ctx, p, in.ProductID)
		if err != nil {
			return err
		}
		if in.GroupID == "" {
			if strings.TrimSpace(in.GroupTitle) == "" {
				in.GroupTitle = "使用指南"
			}
			library, err = scoped.ChangeDocumentLibrary(ctx, p, in.ProductID, DocumentCommand{Action: "create_group", Version: library.Version, Title: in.GroupTitle})
			if err != nil {
				return err
			}
			in.GroupID = library.Groups[len(library.Groups)-1].ID
		}
		library, err = scoped.ChangeDocumentLibrary(ctx, p, in.ProductID, DocumentCommand{Action: "create_article", Version: library.Version, GroupID: in.GroupID, Title: in.Title})
		if err != nil {
			return err
		}
		articleID := ""
		for _, group := range library.Groups {
			if group.ID == in.GroupID && len(group.Articles) > 0 {
				articleID = group.Articles[len(group.Articles)-1].ID
			}
		}
		if articleID == "" {
			return ErrStorage
		}
		library, err = scoped.ChangeDocumentLibrary(ctx, p, in.ProductID, DocumentCommand{Action: "save_article", Version: library.Version, ID: articleID, Content: DocumentContent{Title: in.Title, Slug: in.Slug, Body: in.Body, SEO: in.SEO}})
		if err != nil {
			return err
		}
		if in.Publish {
			library, err = scoped.ChangeDocumentLibrary(ctx, p, in.ProductID, DocumentCommand{Action: "publish_article", Version: library.Version, ID: articleID})
			if err != nil {
				return err
			}
		}
		result = library
		return nil
	})
	return result, databaseError(err)
}
func (s *Service) aiManagementHelp() map[string]any {
	out := map[string]any{}
	for name, a := range s.aiActions() {
		out[name] = map[string]any{"label": a.Label, "parameters": a.Schema}
	}
	out["save_product"].(map[string]any)["instructions"] = "创建产品时 id 留空，name 为名称，slug 使用小写字母、数字和连字符。status 默认 draft；只有明确要求发布才使用 published。content 包含 summary、description、features、version、requirements 等产品资料。编辑已有产品先读取 catalog 并保留未请求修改的 content 字段。"
	out["create_document"].(map[string]any)["instructions"] = "一次创建含正文的产品文档。先查询 catalog 取得 product_id，再查询 documents 取得已有 group_id；也可 group_id 留空并提供 group_title 新建分组。title/slug/body 必填，body 是 Markdown。publish 默认为 false；用户明确要求发布才设为 true。一个确认任务原子完成创建与保存，失败会全部回滚。"
	out["change_document"].(map[string]any)["instructions"] = "修改已有文档前先 query_system documents，并填写该文档库当前 version。command.action 支持 create_group、rename_group、delete_group、move_group、create_article、save_article、publish_article、unpublish_article、delete_article、move_article。group_id 指向分组，id 指向文章。save_article 必须传完整 content，保留原 slug、SEO 等字段。创建完整正文请优先使用 create_document。"
	out["save_ai_schedule"].(map[string]any)["instructions"] = "创建定时只读检查。conversation_id/provider_id 使用当前对话与模型，interval_minutes 为15..43200。也可设置 spec.frequency=daily/weekly/monthly/once；日历频率须 time_zone(IANA) 与 at(HH:mm)，weekly 指定 weekdays(0=周日)，monthly 指定 month_day(1..31，缺少该日期的月份跳过)，once 指定 once_at(RFC3339)。"
	return out
}
