package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/storage"
)

// SEO settings are kept on the existing, backed-up instance/product records.
// Updating product content or its translations never overwrites these values.
type PageSEO struct {
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	Keywords      []string `json:"keywords"`
	CanonicalPath string   `json:"canonical_path"`
	ShareImageURL string   `json:"share_image_url"`
	NoIndex       bool     `json:"no_index"`
}

type SEODefaults struct {
	ProductTitleTemplate string   `json:"product_title_template"`
	Description          string   `json:"description"`
	ShareImageURL        string   `json:"share_image_url"`
	Keywords             []string `json:"keywords"`
}

type siteSEOSettings struct {
	Defaults       SEODefaults            `json:"defaults"`
	LocaleDefaults map[string]SEODefaults `json:"locale_defaults,omitempty"`
	Pages          map[string]PageSEO     `json:"pages"`
}

type productSEOSettings struct {
	Pages   map[string]PageSEO `json:"pages"`
	Aliases []string           `json:"aliases,omitempty"`
}

type SEOInput struct {
	ProductID string       `json:"product_id"`
	Locale    string       `json:"locale"`
	Page      PageSEO      `json:"page"`
	Defaults  *SEODefaults `json:"defaults,omitempty"`
}

type SEOCoverage struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Slug       string `json:"slug"`
	Configured bool   `json:"configured"`
	NoIndex    bool   `json:"no_index"`
}

type SEOEditorView struct {
	PublicURL    string         `json:"public_url"`
	Locale       string         `json:"locale"`
	SourceLocale string         `json:"source_locale"`
	Locales      []LocaleOption `json:"locales"`
	Page         PageSEO        `json:"page"`
	Defaults     SEODefaults    `json:"defaults"`
	Product      *ProductView   `json:"product,omitempty"`
	Brand        Brand          `json:"brand"`
	Products     []SEOCoverage  `json:"products"`
}

func readSiteSEO(raw json.RawMessage) siteSEOSettings {
	v := siteSEOSettings{}
	_ = json.Unmarshal(raw, &v)
	if v.Pages == nil {
		v.Pages = map[string]PageSEO{}
	}
	if v.LocaleDefaults == nil {
		v.LocaleDefaults = map[string]SEODefaults{}
	}
	if v.Defaults.ProductTitleTemplate == "" {
		v.Defaults.ProductTitleTemplate = "{产品名称} | {品牌名称}"
	}
	return v
}

// Legacy global text belongs to the site's source language. A different
// market must not inherit that language's keywords, copy, or title template.
func seoDefaultsForLocale(v siteSEOSettings, locale, source string) SEODefaults {
	defaults, ok := v.LocaleDefaults[locale]
	if !ok {
		defaults = v.Defaults
		if locale != source {
			defaults.Description, defaults.Keywords = "", []string{}
			defaults.ProductTitleTemplate = "{product} | {brand}"
		}
	}
	if defaults.ProductTitleTemplate == "" {
		defaults.ProductTitleTemplate = "{product} | {brand}"
	}
	return defaults
}

func readProductSEO(raw json.RawMessage) productSEOSettings {
	v := productSEOSettings{}
	_ = json.Unmarshal(raw, &v)
	if v.Pages == nil {
		v.Pages = map[string]PageSEO{}
	}
	return v
}

func cleanSEOKeywords(values []string) ([]string, error) {
	if len(values) > 30 {
		return nil, ErrInvalid
	}
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if !utf8.ValidString(value) || utf8.RuneCountInString(value) > 80 || strings.ContainsAny(value, "\r\n\x00") {
			return nil, ErrInvalid
		}
		if value != "" && !seen[strings.ToLower(value)] {
			out = append(out, value)
			seen[strings.ToLower(value)] = true
		}
	}
	return out, nil
}

func validSEOImage(value string) bool {
	if value == "" {
		return true
	}
	u, err := url.Parse(value)
	return err == nil && len(value) <= 2048 && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Fragment == ""
}

func normalizeSEOPage(v PageSEO) (PageSEO, error) {
	v.Title, v.Description = strings.TrimSpace(v.Title), strings.TrimSpace(v.Description)
	v.CanonicalPath, v.ShareImageURL = strings.TrimSpace(v.CanonicalPath), strings.TrimSpace(v.ShareImageURL)
	if !utf8.ValidString(v.Title+v.Description) || utf8.RuneCountInString(v.Title) > 160 || utf8.RuneCountInString(v.Description) > 500 || strings.ContainsAny(v.Title, "\r\n\x00") || !validSEOImage(v.ShareImageURL) {
		return v, ErrInvalid
	}
	if v.CanonicalPath != "" {
		u, err := url.Parse(v.CanonicalPath)
		if err != nil || len(v.CanonicalPath) > 512 || !strings.HasPrefix(v.CanonicalPath, "/") || strings.HasPrefix(v.CanonicalPath, "//") || u.Host != "" || u.Scheme != "" || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(v.CanonicalPath, "\\\r\n\x00") {
			return v, ErrInvalid
		}
	}
	var err error
	v.Keywords, err = cleanSEOKeywords(v.Keywords)
	return v, err
}

func (s *Service) SEOSettings(ctx context.Context, p Principal, productID, locale string) (SEOEditorView, error) {
	var out SEOEditorView
	if !p.CanManage() || (productID == "" && !p.IsAdmin()) {
		return out, ErrForbidden
	}
	var settings storage.InstanceSettings
	if s.db.WithContext(ctx).First(&settings, "id = ?", 1).Error != nil {
		return out, ErrStorage
	}
	out.SourceLocale = sourceLocale(s.db.WithContext(ctx))
	if locale == "" {
		locale = out.SourceLocale
	}
	if canonicalLocale(locale) == "" {
		return out, ErrInvalid
	}
	out.Locale, out.Locales, out.PublicURL = canonicalLocale(locale), supportedLocales(), s.PublicURL()
	site := readSiteSEO(settings.SEOSettings)
	out.Defaults, out.Page, out.Products = seoDefaultsForLocale(site, out.Locale, out.SourceLocale), site.Pages[out.Locale], []SEOCoverage{}
	_ = json.Unmarshal(settings.Brand, &out.Brand)
	if out.Locale != out.SourceLocale {
		if translated, ok := s.localizedPayload(ctx, "brand", "1", out.Locale, brandTranslationPayload(out.Brand)); ok {
			out.Brand.Name, out.Brand.Tagline, out.Brand.Description = translated.Name, translated.Tagline, translated.Description
		}
	}
	if productID != "" {
		var product storage.Product
		if err := s.db.WithContext(ctx).First(&product, "id = ?", productID).Error; err != nil {
			return out, databaseError(err)
		}
		var content ProductContent
		_ = json.Unmarshal(product.Content, &content)
		out.Product = &ProductView{ID: product.ID, Name: product.Name, Slug: product.Slug, Status: product.Status, Content: content}
		if translated, ok := s.localizedProductView(ctx, *out.Product, out.Locale, out.SourceLocale); ok {
			out.Product = &translated
		}
		out.Page = readProductSEO(product.SEO).Pages[out.Locale]
	} else {
		var products []storage.Product
		if s.db.WithContext(ctx).Select("id,name,slug,seo").Where("status = 'published'").Order("created_at,id").Find(&products).Error != nil {
			return out, ErrStorage
		}
		for _, row := range products {
			page := readProductSEO(row.SEO).Pages[out.Locale]
			out.Products = append(out.Products, SEOCoverage{ID: row.ID, Name: row.Name, Slug: row.Slug, Configured: page.Title != "" && page.Description != "", NoIndex: page.NoIndex})
		}
	}
	return out, nil
}

func (s *Service) SaveSEOSettings(ctx context.Context, p Principal, in SEOInput) (SEOEditorView, error) {
	if !p.CanManage() || (in.ProductID == "" && !p.IsAdmin()) || (in.Defaults != nil && !p.IsAdmin()) {
		return SEOEditorView{}, ErrForbidden
	}
	if canonicalLocale(in.Locale) == "" {
		return SEOEditorView{}, ErrInvalid
	}
	in.Locale = canonicalLocale(in.Locale)
	page, err := normalizeSEOPage(in.Page)
	if err != nil {
		return SEOEditorView{}, err
	}
	if in.Defaults != nil {
		v := in.Defaults
		v.ProductTitleTemplate, v.Description, v.ShareImageURL = strings.TrimSpace(v.ProductTitleTemplate), strings.TrimSpace(v.Description), strings.TrimSpace(v.ShareImageURL)
		if v.ProductTitleTemplate == "" {
			v.ProductTitleTemplate = "{产品名称} | {品牌名称}"
		}
		if utf8.RuneCountInString(v.ProductTitleTemplate) > 200 || utf8.RuneCountInString(v.Description) > 500 || strings.ContainsAny(v.ProductTitleTemplate, "\r\n\x00") || !validSEOImage(v.ShareImageURL) {
			return SEOEditorView{}, ErrInvalid
		}
		v.Keywords, err = cleanSEOKeywords(v.Keywords)
		if err != nil {
			return SEOEditorView{}, err
		}
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if in.ProductID == "" {
			var settings storage.InstanceSettings
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&settings, "id = ?", 1).Error; err != nil {
				return err
			}
			v := readSiteSEO(settings.SEOSettings)
			v.Pages[in.Locale] = page
			if in.Defaults != nil {
				v.LocaleDefaults[in.Locale] = *in.Defaults
				if in.Locale == sourceLocale(tx) {
					v.Defaults = *in.Defaults
				}
			}
			if err := tx.Model(&settings).Update("seo_settings", marshal(v)).Error; err != nil {
				return err
			}
		} else {
			if in.Defaults != nil {
				return ErrInvalid
			}
			var product storage.Product
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&product, "id = ?", in.ProductID).Error; err != nil {
				return err
			}
			v := readProductSEO(product.SEO)
			v.Pages[in.Locale] = page
			if err := tx.Model(&product).Update("seo", marshal(v)).Error; err != nil {
				return err
			}
		}
		return audit(tx, p.User.ID, "seo.save", "public_page", in.ProductID, in.Locale)
	})
	if err != nil {
		return SEOEditorView{}, databaseError(err)
	}
	return s.SEOSettings(ctx, p, in.ProductID, in.Locale)
}

// The catalog's instance lock serializes slug reservations and renames.
func reserveProductSlug(tx *gorm.DB, slug, id string) error {
	var products []storage.Product
	if err := tx.Select("id,slug,seo").Find(&products).Error; err != nil {
		return err
	}
	for _, row := range products {
		if row.ID == id {
			continue
		}
		if row.Slug == slug {
			return problem(409, "PRODUCT_SLUG_RESERVED", "页面简称已被其他产品或旧链接使用")
		}
		for _, alias := range readProductSEO(row.SEO).Aliases {
			if alias == slug {
				return problem(409, "PRODUCT_SLUG_RESERVED", "页面简称已被其他产品或旧链接使用")
			}
		}
	}
	return nil
}

func (s *Service) ResolvePublicSlug(ctx context.Context, slug string) (string, error) {
	var current storage.Product
	err := s.db.WithContext(ctx).Select("id,slug").Where("slug = ? AND status = 'published'", slug).First(&current).Error
	if err == nil {
		return current.Slug, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ErrStorage
	}
	var products []storage.Product
	if s.db.WithContext(ctx).Select("slug,seo").Where("status = 'published'").Find(&products).Error != nil {
		return "", ErrStorage
	}
	for _, row := range products {
		for _, alias := range readProductSEO(row.SEO).Aliases {
			if alias == slug {
				return row.Slug, nil
			}
		}
	}
	return "", ErrNotFound
}

func PublicLocale(value string) string     { return canonicalLocale(value) }
func PublicLocalePath(value string) string { return strings.ToLower(canonicalLocale(value)) }

type PublicSEOMetadata struct {
	PageSEO
	Canonical             string `json:"canonical"`
	CompatibilityKeywords string `json:"compatibility_keywords,omitempty"`
}

func (s *Service) PublicSEOMetadata(ctx context.Context, site SiteView, kind, document string) (PublicSEOMetadata, error) {
	var settings storage.InstanceSettings
	if s.db.WithContext(ctx).First(&settings, "id = ?", 1).Error != nil {
		return PublicSEOMetadata{}, ErrStorage
	}
	global := readSiteSEO(settings.SEOSettings)
	defaults := seoDefaultsForLocale(global, site.Locale, site.SourceLocale)
	page := global.Pages[site.Locale]
	path := "/" + PublicLocalePath(site.Locale) + "/"
	title, description := site.Brand.Name, site.Brand.Description
	if kind != "home" && site.Product != nil {
		var product storage.Product
		if s.db.WithContext(ctx).Select("seo").First(&product, "id = ?", site.Product.ID).Error != nil {
			return PublicSEOMetadata{}, ErrStorage
		}
		page = readProductSEO(product.SEO).Pages[site.Locale]
		path += "products/" + site.Product.Slug
		title = strings.NewReplacer("{产品名称}", site.Product.Name, "{品牌名称}", site.Brand.Name, "{product}", site.Product.Name, "{brand}", site.Brand.Name).Replace(defaults.ProductTitleTemplate)
		description = site.Product.Content.Summary
		if kind == "download" {
			path += "/download"
			title = site.UI["common.download"] + " · " + site.Product.Name + " | " + site.Brand.Name
		}
		if kind == "docs" {
			path += "/docs"
			title = site.UI["docs.title"] + " · " + site.Product.Name + " | " + site.Brand.Name
			if document != "" {
				path += "/" + document
			}
		}
	}
	if page.Title == "" || kind == "download" || kind == "docs" {
		page.Title = title
	}
	if page.Description == "" {
		page.Description = description
	}
	if page.Description == "" {
		page.Description = defaults.Description
	}
	if page.ShareImageURL == "" {
		page.ShareImageURL = defaults.ShareImageURL
	}
	if len(page.Keywords) == 0 {
		page.Keywords = defaults.Keywords
	}
	canonical := s.PublicURL() + path
	if page.CanonicalPath != "" && (kind == "home" || kind == "product") {
		canonical = s.PublicURL() + page.CanonicalPath
	}
	if !site.TranslationReady && site.Locale != site.SourceLocale {
		page.NoIndex = true
		canonical = s.PublicURL() + strings.Replace(path, "/"+PublicLocalePath(site.Locale)+"/", "/"+PublicLocalePath(site.SourceLocale)+"/", 1)
	}
	if !site.Installed || site.Environment != "live" {
		page.NoIndex = true
	}
	return PublicSEOMetadata{PageSEO: page, Canonical: canonical, CompatibilityKeywords: strings.Join(page.Keywords, ", ")}, nil
}
