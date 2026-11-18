package app

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/storage"
)

type DocumentSEO struct {
	Title         string `json:"title"`
	Description   string `json:"description"`
	ShareImageURL string `json:"share_image_url"`
	NoIndex       bool   `json:"no_index"`
}
type DocumentContent struct {
	Title string      `json:"title"`
	Slug  string      `json:"slug"`
	Body  string      `json:"body"`
	SEO   DocumentSEO `json:"seo"`
}
type DocumentArticle struct {
	ID          string           `json:"id"`
	Draft       DocumentContent  `json:"draft"`
	Published   *DocumentContent `json:"published,omitempty"`
	Aliases     []string         `json:"aliases,omitempty"`
	UpdatedAt   time.Time        `json:"updated_at"`
	PublishedAt *time.Time       `json:"published_at,omitempty"`
}
type DocumentGroup struct {
	ID       string            `json:"id"`
	Title    string            `json:"title"`
	Articles []DocumentArticle `json:"articles"`
}
type DocumentLibraryView struct {
	Version uint64          `json:"version"`
	Groups  []DocumentGroup `json:"groups"`
}
type PublishedDocument struct {
	ID string `json:"id"`
	DocumentContent
	PublishedAt time.Time `json:"published_at"`
}
type PublishedDocumentGroup struct {
	ID       string              `json:"id"`
	Title    string              `json:"title"`
	Articles []PublishedDocument `json:"articles"`
}
type DocumentCommand struct {
	Action   string          `json:"action"`
	Version  uint64          `json:"version"`
	ID       string          `json:"id"`
	GroupID  string          `json:"group_id"`
	BeforeID string          `json:"before_id"`
	Title    string          `json:"title"`
	Content  DocumentContent `json:"content"`
}

func readDocumentLibrary(raw []byte) (DocumentLibraryView, error) {
	v := DocumentLibraryView{Groups: []DocumentGroup{}}
	if len(raw) > 0 && json.Unmarshal(raw, &v.Groups) != nil {
		return v, ErrStorage
	}
	if v.Groups == nil {
		v.Groups = []DocumentGroup{}
	}
	return v, nil
}

func (s *Service) DocumentLibrary(ctx context.Context, p Principal, productID string) (DocumentLibraryView, error) {
	if !p.CanManage() {
		return DocumentLibraryView{}, ErrForbidden
	}
	var product storage.Product
	if err := s.db.WithContext(ctx).Select("id").First(&product, "id = ?", productID).Error; err != nil {
		return DocumentLibraryView{}, databaseError(err)
	}
	var row storage.DocumentLibrary
	err := s.db.WithContext(ctx).First(&row, "product_id = ?", productID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return DocumentLibraryView{Groups: []DocumentGroup{}}, nil
	}
	if err != nil {
		return DocumentLibraryView{}, ErrStorage
	}
	v, err := readDocumentLibrary(row.Content)
	v.Version = row.Version
	return v, err
}

func (s *Service) ChangeDocumentLibrary(ctx context.Context, p Principal, productID string, in DocumentCommand) (DocumentLibraryView, error) {
	if !p.CanManage() {
		return DocumentLibraryView{}, ErrForbidden
	}
	var out DocumentLibraryView
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lock the product even for a first library insert, serializing creators.
		var product storage.Product
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&product, "id = ?", productID).Error; err != nil {
			return err
		}
		var row storage.DocumentLibrary
		err := tx.First(&row, "product_id = ?", productID).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		out, err = readDocumentLibrary(row.Content)
		if err != nil {
			return err
		}
		if row.Version != in.Version {
			return problem(409, "DOCUMENT_VERSION_CONFLICT", "文档已被其他窗口更新，请复制未保存内容后刷新页面")
		}
		if err = applyDocumentCommand(&out, in); err != nil {
			return err
		}
		out.Version = row.Version + 1
		row.ProductID, row.Version, row.Content, row.UpdatedAt = productID, out.Version, marshal(out.Groups), now()
		if len(row.Content) > 8<<20 {
			return problem(413, "DOCUMENT_LIBRARY_TOO_LARGE", "该产品文档内容过大，请缩减正文，图片请通过上传方式插入")
		}
		if err = tx.Save(&row).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "document."+in.Action, "product", productID, "")
	})
	return out, databaseError(err)
}

var documentSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,79}$`)

func validDocumentContent(c DocumentContent) bool {
	if !utf8.ValidString(c.Title+c.Body+c.SEO.Title+c.SEO.Description) || strings.TrimSpace(c.Title) == "" || utf8.RuneCountInString(c.Title) > 160 || len(c.Body) > 256000 || len(c.SEO.Title) > 512 || len(c.SEO.Description) > 2000 || !documentSlugPattern.MatchString(c.Slug) {
		return false
	}
	return c.SEO.ShareImageURL == "" || safeDocumentURL(c.SEO.ShareImageURL, true)
}
func applyDocumentCommand(v *DocumentLibraryView, in DocumentCommand) error {
	g := slices.IndexFunc(v.Groups, func(g DocumentGroup) bool { return g.ID == in.GroupID })
	if in.Action == "create_group" {
		title := strings.TrimSpace(in.Title)
		if title == "" || !utf8.ValidString(title) || utf8.RuneCountInString(title) > 160 || len(v.Groups) >= 100 {
			return ErrInvalid
		}
		id, err := newID()
		if err != nil {
			return err
		}
		v.Groups = append(v.Groups, DocumentGroup{ID: id, Title: title, Articles: []DocumentArticle{}})
		return nil
	}
	if in.Action == "rename_group" || in.Action == "delete_group" || in.Action == "move_group" {
		if g < 0 {
			return ErrNotFound
		}
		switch in.Action {
		case "rename_group":
			title := strings.TrimSpace(in.Title)
			if title == "" || !utf8.ValidString(title) || utf8.RuneCountInString(title) > 160 {
				return ErrInvalid
			}
			v.Groups[g].Title = title
		case "delete_group":
			if len(v.Groups[g].Articles) > 0 {
				return problem(400, "DOCUMENT_GROUP_NOT_EMPTY", "请先移走或删除该分组中的文章")
			}
			v.Groups = slices.Delete(v.Groups, g, g+1)
		case "move_group":
			item := v.Groups[g]
			v.Groups = slices.Delete(v.Groups, g, g+1)
			at := len(v.Groups)
			if in.BeforeID != "" {
				at = slices.IndexFunc(v.Groups, func(g DocumentGroup) bool { return g.ID == in.BeforeID })
				if at < 0 {
					return ErrInvalid
				}
			}
			v.Groups = slices.Insert(v.Groups, at, item)
		}
		return nil
	}
	if in.Action == "create_article" {
		if g < 0 {
			return ErrNotFound
		}
		count := 0
		for _, group := range v.Groups {
			count += len(group.Articles)
		}
		if count >= 500 {
			return ErrInvalid
		}
		id, err := newID()
		if err != nil {
			return err
		}
		title := strings.TrimSpace(in.Title)
		if title == "" {
			title = "未命名文章"
		}
		if utf8.RuneCountInString(title) > 160 {
			return ErrInvalid
		}
		v.Groups[g].Articles = append(v.Groups[g].Articles, DocumentArticle{ID: id, Draft: DocumentContent{Title: title, Slug: "article-" + strings.ReplaceAll(id, "-", "")[:12]}, UpdatedAt: now()})
		return nil
	}
	from, index := -1, -1
	for gi, group := range v.Groups {
		for ai, a := range group.Articles {
			if a.ID == in.ID {
				from, index = gi, ai
			}
		}
	}
	if from < 0 {
		return ErrNotFound
	}
	a := &v.Groups[from].Articles[index]
	switch in.Action {
	case "save_article":
		in.Content.Title = strings.TrimSpace(in.Content.Title)
		in.Content.Slug = strings.TrimSpace(in.Content.Slug)
		in.Content.SEO.Title = strings.TrimSpace(in.Content.SEO.Title)
		in.Content.SEO.Description = strings.TrimSpace(in.Content.SEO.Description)
		in.Content.SEO.ShareImageURL = strings.TrimSpace(in.Content.SEO.ShareImageURL)
		if !validDocumentContent(in.Content) {
			return ErrInvalid
		}
		for _, group := range v.Groups {
			for _, other := range group.Articles {
				if other.ID != a.ID && (other.Draft.Slug == in.Content.Slug || (other.Published != nil && other.Published.Slug == in.Content.Slug) || slices.Contains(other.Aliases, in.Content.Slug)) {
					return problem(409, "DOCUMENT_SLUG_TAKEN", "该文章链接已被使用，请换一个地址")
				}
			}
		}
		a.Draft = in.Content
		a.UpdatedAt = now()
	case "publish_article":
		if !validDocumentContent(a.Draft) || strings.TrimSpace(a.Draft.Body) == "" {
			return problem(400, "DOCUMENT_BODY_REQUIRED", "请填写正文后再发布")
		}
		if a.Published != nil && a.Published.Slug != a.Draft.Slug && !slices.Contains(a.Aliases, a.Published.Slug) {
			a.Aliases = append(a.Aliases, a.Published.Slug)
		}
		c := a.Draft
		at := now()
		a.Published = &c
		a.PublishedAt = &at
	case "unpublish_article":
		a.Published = nil
		a.PublishedAt = nil
	case "delete_article":
		v.Groups[from].Articles = slices.Delete(v.Groups[from].Articles, index, index+1)
	case "move_article":
		if g < 0 {
			return ErrNotFound
		}
		item := *a
		v.Groups[from].Articles = slices.Delete(v.Groups[from].Articles, index, index+1)
		at := len(v.Groups[g].Articles)
		if in.BeforeID != "" {
			at = slices.IndexFunc(v.Groups[g].Articles, func(a DocumentArticle) bool { return a.ID == in.BeforeID })
			if at < 0 {
				return ErrInvalid
			}
		}
		v.Groups[g].Articles = slices.Insert(v.Groups[g].Articles, at, item)
	default:
		return ErrInvalid
	}
	return nil
}

func publishedDocuments(raw []byte) ([]PublishedDocumentGroup, error) {
	v, err := readDocumentLibrary(raw)
	if err != nil {
		return nil, err
	}
	out := []PublishedDocumentGroup{}
	for _, g := range v.Groups {
		group := PublishedDocumentGroup{ID: g.ID, Title: g.Title, Articles: []PublishedDocument{}}
		for _, a := range g.Articles {
			if a.Published != nil && a.PublishedAt != nil {
				group.Articles = append(group.Articles, PublishedDocument{ID: a.ID, DocumentContent: *a.Published, PublishedAt: *a.PublishedAt})
			}
		}
		if len(group.Articles) > 0 {
			out = append(out, group)
		}
	}
	return out, nil
}

func (s *Service) publicDocumentGroups(ctx context.Context, productID string) ([]PublishedDocumentGroup, error) {
	var row storage.DocumentLibrary
	err := s.db.WithContext(ctx).First(&row, "product_id = ?", productID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return []PublishedDocumentGroup{}, nil
	}
	if err != nil {
		return nil, ErrStorage
	}
	return publishedDocuments(row.Content)
}

func (s *Service) ResolveDocumentSlug(ctx context.Context, productID, slug string) (string, error) {
	var row storage.DocumentLibrary
	err := s.db.WithContext(ctx).First(&row, "product_id = ?", productID).Error
	if err != nil {
		return "", databaseError(err)
	}
	v, err := readDocumentLibrary(row.Content)
	if err != nil {
		return "", err
	}
	for _, g := range v.Groups {
		for _, a := range g.Articles {
			if a.Published != nil && (a.Published.Slug == slug || slices.Contains(a.Aliases, slug)) {
				return a.Published.Slug, nil
			}
		}
	}
	return "", ErrNotFound
}
