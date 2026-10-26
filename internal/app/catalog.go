package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"licentivo-launch/internal/domain"
	"licentivo-launch/internal/storage"
)

type Brand struct {
	Name         string `json:"name"`
	Tagline      string `json:"tagline"`
	Description  string `json:"description"`
	SupportEmail string `json:"support_email"`
	LogoDataURL  string `json:"logo_data_url,omitempty"`
	Theme        string `json:"theme"`
}

var brandThemes = map[string]bool{
	"forest": true, "ocean": true, "indigo": true, "amber": true,
	"rose": true, "graphite": true, "teal": true, "violet": true,
}

func validateLogoDataURL(value string) error {
	if value == "" {
		return nil
	}
	parts := strings.SplitN(value, ",", 2)
	if len(parts) != 2 {
		return ErrInvalid
	}
	allowed := map[string]bool{
		"data:image/png;base64": true, "data:image/jpeg;base64": true, "data:image/webp;base64": true,
	}
	if !allowed[parts[0]] {
		return problem(400, "LOGO_FORMAT_INVALID", "Logo 只支持 PNG、JPEG 或 WebP")
	}
	raw, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil || len(raw) == 0 || len(raw) > 128<<10 {
		return problem(400, "LOGO_SIZE_INVALID", "Logo 文件须小于 128 KB")
	}
	validMagic := false
	switch parts[0] {
	case "data:image/png;base64":
		validMagic = len(raw) >= 8 && string(raw[:8]) == "\x89PNG\r\n\x1a\n"
	case "data:image/jpeg;base64":
		validMagic = len(raw) >= 3 && raw[0] == 0xff && raw[1] == 0xd8 && raw[2] == 0xff
	case "data:image/webp;base64":
		validMagic = len(raw) >= 12 && string(raw[:4]) == "RIFF" && string(raw[8:12]) == "WEBP"
	}
	if !validMagic {
		return problem(400, "LOGO_CONTENT_INVALID", "Logo 内容与声明的图片格式不一致")
	}
	return nil
}

type ProductContent struct {
	Summary              string            `json:"summary"`
	Description          string            `json:"description"`
	Features             []string          `json:"features"`
	Requirements         string            `json:"requirements"`
	PlatformRequirements map[string]string `json:"platform_requirements,omitempty"`
	InstallationGuide    string            `json:"installation_guide,omitempty"`
	UpgradeGuide         string            `json:"upgrade_guide,omitempty"`
	UninstallGuide       string            `json:"uninstall_guide,omitempty"`
	TroubleshootingGuide string            `json:"troubleshooting_guide,omitempty"`
	TermsOfService       string            `json:"terms_of_service,omitempty"`
	PrivacyPolicy        string            `json:"privacy_policy,omitempty"`
	RefundPolicy         string            `json:"refund_policy,omitempty"`
	InvoiceGuide         string            `json:"invoice_guide,omitempty"`
	LegalTaxNotice       string            `json:"legal_tax_notice,omitempty"`
	DownloadURL          string            `json:"download_url"`
	Version              string            `json:"version"`
}

type ProductView struct {
	ID        string                `json:"id"`
	Name      string                `json:"name"`
	Slug      string                `json:"slug"`
	Status    string                `json:"status"`
	Content   ProductContent        `json:"content"`
	Downloads []ProductDownloadView `json:"downloads"`
}
type PlanSnapshot struct {
	External         *ExternalSaleRoute `json:"external_licensing,omitempty"`
	OfflineMode      string             `json:"offline_mode"`
	HeartbeatSeconds uint64             `json:"heartbeat_seconds"`
	OfflineSeconds   uint64             `json:"offline_seconds"`
	SessionSeconds   uint64             `json:"session_seconds"`
	SelfReleaseLimit uint64             `json:"self_release_limit"`
	Features         []string           `json:"features"`
	MaxDevices       uint64             `json:"max_devices"`
	LicenseModel     string             `json:"license_model"`
	OfflineSummary   string             `json:"offline_summary"`
}
type PlanMarketing struct {
	Summary    string   `json:"summary"`
	Highlights []string `json:"highlights"`
}
type PurchaseOption struct {
	Periods     uint64 `json:"periods"`
	AmountMinor int64  `json:"amount_minor"`
	Enabled     bool   `json:"enabled"`
}
type PlanView struct {
	PermanentUpgradeMinor *int64           `json:"permanent_upgrade_minor,omitempty"`
	ID                    string           `json:"id"`
	ProductID             string           `json:"product_id"`
	VersionID             string           `json:"version_id"`
	Version               uint64           `json:"version"`
	Name                  string           `json:"name"`
	Summary               string           `json:"summary"`
	Highlights            []string         `json:"highlights"`
	Status                string           `json:"status"`
	Currency              string           `json:"currency"`
	AmountMinor           int64            `json:"amount_minor"`
	PurchaseType          string           `json:"purchase_type"`
	TermUnit              *string          `json:"term_unit"`
	TermCount             *uint64          `json:"term_count"`
	PurchaseOptions       []PurchaseOption `json:"purchase_options"`
	Snapshot              PlanSnapshot     `json:"snapshot"`
	AllowPurchase         bool             `json:"allow_purchase"`
	AllowRenewal          bool             `json:"allow_renewal"`
	Published             bool             `json:"published"`
	PreviewOnly           bool             `json:"preview_only"`
}

type PlanVersionPublication struct {
	Environment string    `json:"environment"`
	PreviewOnly bool      `json:"preview_only"`
	Active      bool      `json:"active"`
	PublishedAt time.Time `json:"published_at"`
}

type PlanVersionHistory struct {
	PlanView
	CreatedAt         time.Time                `json:"created_at"`
	FirstPublishedAt  *time.Time               `json:"first_published_at,omitempty"`
	ActiveOrderCount  int64                    `json:"active_order_count"`
	RemoteProductID   string                   `json:"remote_product_id,omitempty"`
	RemoteProductName string                   `json:"remote_product_name,omitempty"`
	RemotePolicyID    string                   `json:"remote_policy_id,omitempty"`
	RemotePolicyName  string                   `json:"remote_policy_name,omitempty"`
	RemotePolicyVer   uint64                   `json:"remote_policy_version,omitempty"`
	Publications      []PlanVersionPublication `json:"publications"`
}

func (s *Service) PlanVersions(ctx context.Context, p Principal, planID string) ([]PlanVersionHistory, error) {
	if !p.CanManage() {
		return nil, ErrForbidden
	}
	var plan storage.Plan
	if err := s.db.WithContext(ctx).First(&plan, "id = ?", planID).Error; err != nil {
		return nil, databaseError(err)
	}
	var versions []storage.PlanVersion
	if err := s.db.WithContext(ctx).Where("plan_id = ?", planID).Order("version DESC").Find(&versions).Error; err != nil {
		return nil, ErrStorage
	}
	var activePublications []storage.PlanPublication
	if err := s.db.WithContext(ctx).Where("plan_id = ?", planID).Find(&activePublications).Error; err != nil {
		return nil, ErrStorage
	}
	activeByEnvironment := map[string]string{}
	for _, publication := range activePublications {
		activeByEnvironment[publication.Environment] = publication.VersionID
	}
	type catalogNames struct {
		products map[string]string
		policies map[string]string
	}
	namesByConnection := map[string]catalogNames{}
	var connections []storage.LicentivoConnection
	if err := s.db.WithContext(ctx).Find(&connections).Error; err != nil {
		return nil, ErrStorage
	}
	for _, connection := range connections {
		var catalog licentivoCatalogSnapshot
		if json.Unmarshal(connection.Catalog, &catalog) != nil {
			continue
		}
		names := catalogNames{products: map[string]string{}, policies: map[string]string{}}
		for _, product := range catalog.Products.Items {
			names.products[product.ID] = product.Name
		}
		for _, policy := range catalog.Policies.Items {
			names.policies[policy.ID] = policy.Name
		}
		namesByConnection[connection.ID] = names
	}
	activeAt := now()
	out := make([]PlanVersionHistory, 0, len(versions))
	for _, version := range versions {
		row := PlanVersionHistory{PlanView: planView(plan, version), CreatedAt: version.CreatedAt, FirstPublishedAt: version.PublishedAt, Publications: []PlanVersionPublication{}}
		var mapping storage.PlanLicenseMapping
		if err := s.db.WithContext(ctx).First(&mapping, "plan_version_id = ?", version.ID).Error; err == nil {
			row.RemoteProductID, row.RemoteProductName = mapping.RemoteProductID, mapping.RemoteProductName
			row.RemotePolicyID, row.RemotePolicyName, row.RemotePolicyVer = mapping.RemotePolicyID, mapping.RemotePolicyName, mapping.RemotePolicyVersion
			// Older mappings did not persist display names, and Licentivo names can
			// change without changing policy IDs. Prefer the latest verified
			// catalog label while keeping the immutable IDs and policy version.
			if names, ok := namesByConnection[mapping.ConnectionID]; ok {
				if name := names.products[mapping.RemoteProductID]; name != "" {
					row.RemoteProductName = name
				}
				if name := names.policies[mapping.RemotePolicyID]; name != "" {
					row.RemotePolicyName = name
				}
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrStorage
		}
		if err := s.db.WithContext(ctx).Model(&storage.EntitlementCoverage{}).
			Joins("JOIN orders ON orders.id = entitlement_coverages.source_order_id").
			Where("orders.plan_version_id = ? AND entitlement_coverages.voided_at IS NULL AND entitlement_coverages.valid_from <= ? AND (entitlement_coverages.valid_until IS NULL OR entitlement_coverages.valid_until > ?)", version.ID, activeAt, activeAt).
			Distinct("entitlement_coverages.source_order_id").Count(&row.ActiveOrderCount).Error; err != nil {
			return nil, ErrStorage
		}
		if version.PublishedAt != nil {
			for _, publication := range activePublications {
				if publication.VersionID == version.ID {
					row.Publications = append(row.Publications, PlanVersionPublication{Environment: publication.Environment, PreviewOnly: publication.PreviewOnly, Active: activeByEnvironment[publication.Environment] == version.ID, PublishedAt: publication.PublishedAt})
				}
			}
		}
		out = append(out, row)
	}
	return out, nil
}

func (s *Service) RollbackPlan(ctx context.Context, p Principal, planID, versionID, environment string) error {
	if !p.CanManage() {
		return ErrForbidden
	}
	if err := s.PublishPlan(ctx, p, planID, versionID, environment); err != nil {
		return err
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return audit(tx, p.User.ID, "plan.rollback_"+environment, "plan_version", versionID, "")
	}))
}

type SiteView struct {
	MailEnabled               bool              `json:"mail_enabled"`
	EmailVerificationRequired bool              `json:"email_verification_required"`
	Installed                 bool              `json:"installed"`
	Brand                     Brand             `json:"brand"`
	Product                   *ProductView      `json:"product"`
	Products                  []ProductView     `json:"products"`
	Plans                     []PlanView        `json:"plans"`
	Environment               string            `json:"environment"`
	SalesEnabled              bool              `json:"sales_enabled"`
	PaymentProviders          []string          `json:"payment_providers"`
	Locale                    string            `json:"locale"`
	SourceLocale              string            `json:"source_locale"`
	SupportedLocales          []LocaleOption    `json:"supported_locales"`
	TranslationReady          bool              `json:"translation_ready"`
	UI                        map[string]string `json:"ui"`
}

func (s *Service) Site(ctx context.Context, environment, productSlug, requestedLocale string) (SiteView, error) {
	if environment != "live" && environment != "sandbox" {
		return SiteView{}, ErrInvalid
	}
	var settings storage.InstanceSettings
	if err := s.db.WithContext(ctx).First(&settings, "id = ?", 1).Error; err != nil {
		return SiteView{}, ErrStorage
	}
	source, locale := requestedContentLocale(s.db.WithContext(ctx), requestedLocale)
	view := SiteView{Installed: settings.InstallationCompletedAt != nil, Products: []ProductView{}, Plans: []PlanView{}, PaymentProviders: []string{}, Environment: environment, Locale: locale, SourceLocale: source, SupportedLocales: supportedLocales(), TranslationReady: true, UI: builtInUIStrings(locale)}
	mailRequired, err := emailVerificationRequired(s.db.WithContext(ctx))
	if err != nil {
		return view, err
	}
	view.EmailVerificationRequired = mailRequired
	view.MailEnabled, err = s.mailConfigured(ctx)
	if err != nil {
		return view, err
	}
	if json.Unmarshal(settings.Brand, &view.Brand) != nil {
		return view, ErrStorage
	}
	if locale != source {
		translated, ok := s.localizedPayload(ctx, "brand", "1", locale, brandTranslationPayload(view.Brand))
		if ok {
			view.Brand.Name, view.Brand.Tagline, view.Brand.Description = translated.Name, translated.Tagline, translated.Description
		} else {
			view.TranslationReady = false
		}
	}
	if view.Brand.Theme == "" {
		view.Brand.Theme = "forest"
	}
	var accounts []storage.PaymentAccount
	if err := s.db.WithContext(ctx).Where("environment = ? AND enabled = true", environment).Order("provider").Find(&accounts).Error; err != nil {
		return view, ErrStorage
	}
	for _, account := range accounts {
		view.PaymentProviders = append(view.PaymentProviders, account.Provider)
	}
	var products []storage.Product
	if err := s.db.WithContext(ctx).Where("status = 'published'").Order("created_at,id").Find(&products).Error; err != nil {
		return view, ErrStorage
	}
	selected := -1
	for _, product := range products {
		var content ProductContent
		_ = json.Unmarshal(product.Content, &content)
		productView := ProductView{ID: product.ID, Name: product.Name, Slug: product.Slug, Status: product.Status, Content: content}
		if err := s.attachProductDownloads(ctx, &productView); err != nil {
			return view, err
		}
		if translated, ok := s.localizedProductView(ctx, productView, locale, source); ok {
			productView = translated
		} else {
			view.TranslationReady = false
		}
		view.Products = append(view.Products, productView)
		index := len(view.Products) - 1
		if productSlug != "" && product.Slug == productSlug {
			selected = index
		} else if productSlug == "" && settings.DefaultProductID != nil && product.ID == *settings.DefaultProductID {
			selected = index
		}
	}
	if productSlug != "" && selected < 0 {
		return view, ErrNotFound
	}
	if selected < 0 && len(view.Products) > 0 {
		selected = 0
	}
	if selected < 0 {
		return view, nil
	}
	view.Product = &view.Products[selected]
	var publications []storage.PlanPublication
	if err := s.db.WithContext(ctx).Where("environment = ?", environment).Order("published_at").Find(&publications).Error; err != nil {
		return view, ErrStorage
	}
	for _, publication := range publications {
		var plan storage.Plan
		var version storage.PlanVersion
		if err := s.db.WithContext(ctx).First(&plan, "id = ? AND product_id = ? AND status = 'published'", publication.PlanID, view.Product.ID).Error; err != nil {
			continue
		}
		if err := s.db.WithContext(ctx).First(&version, "id = ? AND plan_id = ?", publication.VersionID, plan.ID).Error; err != nil {
			return view, ErrStorage
		}
		v := planView(plan, version)
		if translated, ok := s.localizedPlanView(ctx, v, locale, source); ok {
			v = translated
		} else {
			view.TranslationReady = false
		}
		v.Published = true
		v.PreviewOnly = publication.PreviewOnly
		binding, bindingErr := s.productLicensing(s.db.WithContext(ctx), plan.ProductID, environment)
		if bindingErr != nil {
			return view, ErrStorage
		}
		if binding.Provider == CustomLicensing {
			v.Snapshot.LicenseModel = "external"
			v.Snapshot.MaxDevices = 0
			v.Snapshot.Features = []string{}
			v.Snapshot.OfflineMode = "external"
		}
		if !publication.PreviewOnly && plan.AllowPurchase {
			view.SalesEnabled = true
		}
		view.Plans = append(view.Plans, v)
	}
	return view, nil
}

func (s *Service) SaveBrand(ctx context.Context, p Principal, in Brand) error {
	if !p.IsAdmin() {
		return ErrForbidden
	}
	if len(strings.TrimSpace(in.Name)) < 2 || len(in.Name) > 160 || len(in.Tagline) > 240 || len(in.Description) > 8000 {
		return ErrInvalid
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Theme == "" {
		in.Theme = "forest"
	}
	if !brandThemes[in.Theme] {
		return problem(400, "THEME_INVALID", "请选择系统提供的网站主题")
	}
	if err := validateLogoDataURL(in.LogoDataURL); err != nil {
		return err
	}
	if in.SupportEmail != "" {
		if _, err := email(in.SupportEmail); err != nil {
			return err
		}
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&storage.InstanceSettings{}).Where("id = ?", 1).Update("brand", marshal(in)).Error; err != nil {
			return err
		}
		if err := s.enqueueTranslation(tx, "brand", "1", sourceLocale(tx), brandTranslationPayload(in), false); err != nil {
			return err
		}
		return audit(tx, p.User.ID, "brand.save", "instance", "1", "")
	}))
}

func (s *Service) SaveProduct(ctx context.Context, p Principal, in ProductView) (ProductView, error) {
	if !p.CanManage() {
		return ProductView{}, ErrForbidden
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Slug = strings.TrimSpace(in.Slug)
	if len(in.Name) < 2 || len(in.Name) > 160 || len(in.Slug) < 2 || len(in.Slug) > 80 || strings.Trim(in.Slug, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" || len(in.Content.Summary) > 500 || len(in.Content.Description) > 16000 || len(in.Content.Requirements) > 4000 || len(in.Content.Version) > 80 || len(in.Content.Features) > 30 {
		return ProductView{}, ErrInvalid
	}
	if in.Status != "draft" && in.Status != "published" && in.Status != "archived" {
		return ProductView{}, ErrInvalid
	}
	features := make([]string, 0, len(in.Content.Features))
	for _, feature := range in.Content.Features {
		feature = strings.TrimSpace(feature)
		if len(feature) > 200 {
			return ProductView{}, ErrInvalid
		}
		if feature != "" {
			features = append(features, feature)
		}
	}
	in.Content.Features = features
	in.Content.Summary = strings.TrimSpace(in.Content.Summary)
	in.Content.Description = strings.TrimSpace(in.Content.Description)
	in.Content.Requirements = strings.TrimSpace(in.Content.Requirements)
	in.Content.Version = strings.TrimSpace(in.Content.Version)
	documents := []*string{
		&in.Content.InstallationGuide, &in.Content.UpgradeGuide, &in.Content.UninstallGuide,
		&in.Content.TroubleshootingGuide, &in.Content.TermsOfService, &in.Content.PrivacyPolicy,
		&in.Content.RefundPolicy, &in.Content.InvoiceGuide, &in.Content.LegalTaxNotice,
	}
	for _, document := range documents {
		*document = strings.TrimSpace(*document)
		if len(*document) > 16000 {
			return ProductView{}, problem(400, "PRODUCT_DOCUMENT_TOO_LONG", "每份产品文档不能超过 16000 个字符")
		}
		if strings.Contains(*document, "](upload:") {
			return ProductView{}, problem(400, "PRODUCT_DOCUMENT_IMAGE_PENDING", "文档图片尚未完成上传，请重新插入后保存")
		}
	}
	platformRequirements := make(map[string]string, len(in.Content.PlatformRequirements))
	for platform, requirement := range in.Content.PlatformRequirements {
		if _, ok := productPlatforms[platform]; !ok {
			return ProductView{}, problem(400, "PLATFORM_INVALID", "系统要求包含不支持的平台")
		}
		requirement = strings.TrimSpace(requirement)
		if len(requirement) > 4000 {
			return ProductView{}, problem(400, "PLATFORM_REQUIREMENTS_TOO_LONG", "每个平台的系统要求不能超过 4000 个字符")
		}
		if requirement != "" {
			platformRequirements[platform] = requirement
		}
	}
	if len(platformRequirements) == 0 {
		in.Content.PlatformRequirements = nil
	} else {
		in.Content.PlatformRequirements = platformRequirements
	}
	if in.Content.DownloadURL != "" {
		u, err := url.Parse(in.Content.DownloadURL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
			return ProductView{}, problem(400, "DOWNLOAD_URL_INVALID", "下载链接必须是 HTTPS 地址")
		}
	}
	var product storage.Product
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var settings storage.InstanceSettings
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&settings, "id = ?", 1).Error; err != nil {
			return err
		}
		if in.ID == "" {
			id, err := newID()
			if err != nil {
				return err
			}
			product = storage.Product{ID: id, Slug: in.Slug, Name: in.Name, Content: marshal(in.Content), Status: in.Status}
			if err := tx.Create(&product).Error; err != nil {
				return err
			}
			if settings.DefaultProductID == nil {
				if err := tx.Model(&settings).Update("default_product_id", id).Error; err != nil {
					return err
				}
			}
		} else {
			if err := tx.First(&product, "id = ?", in.ID).Error; err != nil {
				return err
			}
			if err := tx.Model(&product).Updates(map[string]any{"slug": in.Slug, "name": in.Name, "content": marshal(in.Content), "status": in.Status}).Error; err != nil {
				return err
			}
		}
		if err := s.enqueueTranslation(tx, "product", product.ID, sourceLocale(tx), productTranslationPayload(ProductView{ID: product.ID, Name: in.Name, Slug: in.Slug, Status: in.Status, Content: in.Content}), false); err != nil {
			return err
		}
		return audit(tx, p.User.ID, "product.save", "product", product.ID, "")
	})
	if err != nil {
		return ProductView{}, databaseError(err)
	}
	in.ID = product.ID
	return in, nil
}

func (s *Service) SetDefaultProduct(ctx context.Context, p Principal, productID string) error {
	if !p.CanManage() {
		return ErrForbidden
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var product storage.Product
		if err := tx.First(&product, "id = ?", productID).Error; err != nil {
			return err
		}
		if product.Status != "published" {
			return problem(409, "PRODUCT_NOT_PUBLISHED", "默认产品需要先设为发布状态")
		}
		if err := tx.Model(&storage.InstanceSettings{}).Where("id = ?", 1).Update("default_product_id", productID).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "product.set_default", "product", productID, "")
	}))
}

func (s *Service) AdminCatalog(ctx context.Context, p Principal) (map[string]any, error) {
	if !p.CanManage() {
		return nil, ErrForbidden
	}
	var settings storage.InstanceSettings
	if err := s.db.WithContext(ctx).First(&settings, "id = ?", 1).Error; err != nil {
		return nil, ErrStorage
	}
	var brand Brand
	_ = json.Unmarshal(settings.Brand, &brand)
	if brand.Theme == "" {
		brand.Theme = "forest"
	}
	mailConfigured, err := s.mailConfigured(ctx)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"brand": brand, "product": nil, "products": []ProductView{}, "plans": []PlanView{}, "default_product_id": settings.DefaultProductID, "mail_configured": mailConfigured}
	var products []storage.Product
	if err := s.db.WithContext(ctx).Order("created_at,id").Find(&products).Error; err != nil {
		return nil, ErrStorage
	}
	productViews := make([]ProductView, 0, len(products))
	for _, product := range products {
		var content ProductContent
		_ = json.Unmarshal(product.Content, &content)
		view := ProductView{ID: product.ID, Name: product.Name, Slug: product.Slug, Status: product.Status, Content: content}
		if err := s.attachProductDownloads(ctx, &view); err != nil {
			return nil, err
		}
		productViews = append(productViews, view)
		if settings.DefaultProductID != nil && product.ID == *settings.DefaultProductID {
			result["product"] = view
		}
	}
	result["products"] = productViews
	var plans []storage.Plan
	if err := s.db.WithContext(ctx).Order("product_id,display_order,created_at").Find(&plans).Error; err != nil {
		return nil, ErrStorage
	}
	views := make([]PlanView, 0, len(plans))
	for _, plan := range plans {
		var version storage.PlanVersion
		if err := s.db.WithContext(ctx).Where("plan_id = ?", plan.ID).Order("version DESC").First(&version).Error; err != nil {
			return nil, ErrStorage
		}
		v := planView(plan, version)
		var published int64
		_ = s.db.WithContext(ctx).Model(&storage.PlanPublication{}).Where("version_id = ?", version.ID).Count(&published).Error
		v.Published = published > 0
		views = append(views, v)
	}
	result["plans"] = views
	return result, nil
}

func planView(plan storage.Plan, version storage.PlanVersion) PlanView {
	var snapshot PlanSnapshot
	var marketing PlanMarketing
	_ = json.Unmarshal(version.EntitlementSnapshot, &snapshot)
	_ = json.Unmarshal(version.Marketing, &marketing)
	if marketing.Highlights == nil {
		marketing.Highlights = []string{}
	}
	options := purchaseOptions(version)
	return PlanView{ID: plan.ID, ProductID: plan.ProductID, VersionID: version.ID, Version: version.Version, Name: plan.Name, Summary: marketing.Summary, Highlights: marketing.Highlights, Status: plan.Status, Currency: version.Currency, AmountMinor: version.AmountMinor, PurchaseType: version.PurchaseType, TermUnit: version.TermUnit, TermCount: version.TermCount, PurchaseOptions: options, Snapshot: snapshot, AllowPurchase: plan.AllowPurchase, AllowRenewal: plan.AllowRenewal, PermanentUpgradeMinor: version.PermanentUpgradeMinor}
}

func defaultPlanSnapshot() PlanSnapshot {
	// A sales plan can be drafted before Licentivo is connected. The complete
	// snapshot is replaced from the selected Licentivo policy in SaveMapping;
	// these values are never presented to administrators as editable settings.
	return PlanSnapshot{OfflineMode: "none", Features: []string{}, MaxDevices: 1, LicenseModel: "node_locked"}
}

func (s *Service) SavePlan(ctx context.Context, p Principal, in PlanView) (PlanView, error) {
	if !p.CanManage() {
		return PlanView{}, ErrForbidden
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Summary = strings.TrimSpace(in.Summary)
	highlights := make([]string, 0, len(in.Highlights))
	for _, value := range in.Highlights {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if len(value) > 480 {
			return PlanView{}, ErrInvalid
		}
		highlights = append(highlights, value)
	}
	in.Highlights = highlights
	if len(in.Name) < 2 || len(in.Name) > 160 || len(in.Summary) > 1500 || len(in.Highlights) > 12 || (domain.Money{Currency: in.Currency, Minor: in.AmountMinor}).Validate() != nil {
		return PlanView{}, ErrInvalid
	}
	if in.PermanentUpgradeMinor != nil && (*in.PermanentUpgradeMinor <= 0 || *in.PermanentUpgradeMinor > in.AmountMinor || in.PurchaseType != "perpetual") {
		return PlanView{}, ErrInvalid
	}
	if in.PurchaseType == "perpetual" {
		in.TermUnit = nil
		in.TermCount = nil
		in.PurchaseOptions = []PurchaseOption{}
	} else if in.PurchaseType == "fixed_term" {
		if in.TermUnit == nil || in.TermCount == nil || *in.TermUnit != "month" || *in.TermCount != 1 {
			return PlanView{}, ErrInvalid
		}
		var err error
		in.PurchaseOptions, err = normalizePurchaseOptions(in.PurchaseOptions, in.AmountMinor, in.Currency)
		if err != nil {
			return PlanView{}, err
		}
		in.AmountMinor = in.PurchaseOptions[0].AmountMinor
	} else {
		return PlanView{}, ErrInvalid
	}
	var plan storage.Plan
	var version storage.PlanVersion
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		snapshot := defaultPlanSnapshot()
		var settings storage.InstanceSettings
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&settings, "id = ?", 1).Error; err != nil {
			return err
		}
		productID := in.ProductID
		if productID == "" && settings.DefaultProductID != nil {
			productID = *settings.DefaultProductID
		}
		if productID == "" {
			return problem(409, "PRODUCT_REQUIRED", "请先创建产品")
		}
		var product storage.Product
		if err := tx.First(&product, "id = ?", productID).Error; err != nil {
			return err
		}
		var next uint64 = 1
		if in.ID == "" {
			id, err := newID()
			if err != nil {
				return err
			}
			plan = storage.Plan{ID: id, ProductID: productID, Name: in.Name, Status: "draft", AllowPurchase: in.AllowPurchase, AllowRenewal: in.AllowRenewal}
			if err := tx.Create(&plan).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&plan, "id = ? AND product_id = ?", in.ID, productID).Error; err != nil {
				return err
			}
			var previous storage.PlanVersion
			if err := tx.Where("plan_id = ?", plan.ID).Order("version DESC").First(&previous).Error; err != nil {
				return err
			}
			if json.Unmarshal(previous.EntitlementSnapshot, &snapshot) != nil {
				return ErrStorage
			}
			if snapshot.Features == nil {
				snapshot.Features = []string{}
			}
			if in.Version != previous.Version {
				return problem(409, "VERSION_CONFLICT", "套餐已被更新，请刷新后重新编辑")
			}
			next = previous.Version + 1
			if err := tx.Model(&plan).Updates(map[string]any{"name": in.Name, "allow_purchase": in.AllowPurchase, "allow_renewal": in.AllowRenewal}).Error; err != nil {
				return err
			}
		}
		id, err := newID()
		if err != nil {
			return err
		}
		snapshotBytes := marshal(snapshot)
		sum := sha256.Sum256(snapshotBytes)
		version = storage.PlanVersion{ID: id, PlanID: plan.ID, ProductID: plan.ProductID, Version: next, Currency: in.Currency, AmountMinor: in.AmountMinor, PurchaseType: in.PurchaseType, TermUnit: in.TermUnit, TermCount: in.TermCount, EntitlementSnapshot: snapshotBytes, Marketing: marshal(PlanMarketing{Summary: in.Summary, Highlights: in.Highlights}), PurchaseOptions: marshal(in.PurchaseOptions), SnapshotHash: sum[:], PermanentUpgradeMinor: in.PermanentUpgradeMinor}
		if err := tx.Create(&version).Error; err != nil {
			return err
		}
		plan.Name, plan.AllowPurchase, plan.AllowRenewal = in.Name, in.AllowPurchase, in.AllowRenewal
		if err := s.enqueuePlanVersions(tx, plan, sourceLocale(tx), false); err != nil {
			return err
		}
		return audit(tx, p.User.ID, "plan.draft", "plan", plan.ID, "")
	})
	if err != nil {
		return PlanView{}, databaseError(err)
	}
	plan.Name = in.Name
	plan.AllowPurchase = in.AllowPurchase
	plan.AllowRenewal = in.AllowRenewal
	return planView(plan, version), nil
}

func (s *Service) PublishPlan(ctx context.Context, p Principal, planID, versionID, environment string) error {
	if !p.CanManage() {
		return ErrForbidden
	}
	if environment != "sandbox" && environment != "live" {
		return ErrInvalid
	}
	var candidate storage.PlanVersion
	if err := s.db.WithContext(ctx).First(&candidate, "id = ? AND plan_id = ?", versionID, planID).Error; err != nil {
		return databaseError(err)
	}
	previewOnly := true
	binding, bindingErr := s.productLicensing(s.db.WithContext(ctx), candidate.ProductID, environment)
	if bindingErr != nil {
		return databaseError(bindingErr)
	}
	if binding.Provider == CustomLicensing {
		return s.publishExternalPlan(ctx, p, candidate, environment)
	}
	var mapping storage.PlanLicenseMapping
	var connection storage.LicentivoConnection
	if s.db.WithContext(ctx).First(&mapping, "plan_version_id = ?", versionID).Error == nil && s.db.WithContext(ctx).First(&connection, "id = ?", mapping.ConnectionID).Error == nil {
		var product storage.Product
		if err := s.db.WithContext(ctx).First(&product, "id = ?", candidate.ProductID).Error; err != nil {
			return databaseError(err)
		}
		remoteProduct, err := productRemoteID(s.db.WithContext(ctx), product, "")
		if err != nil {
			return databaseError(err)
		}
		if remoteProduct != "" && remoteProduct != mapping.RemoteProductID {
			return productMappingConflict()
		}
		var accounts int64
		if s.registerLaunchPlan(ctx, environment, candidate, mapping, true) == nil && s.db.WithContext(ctx).Model(&storage.PaymentAccount{}).Where("environment = ? AND enabled = true", environment).Count(&accounts).Error == nil && accounts > 0 {
			previewOnly = false
		}
	}

	if environment == "live" && previewOnly {
		return problem(409, "SALES_CONNECTION_REQUIRED", "正式发布前，请配置正式收款，并为套餐选择可自动发放的 Licentivo 授权")
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var plan storage.Plan
		if _, err := lockProviderSettings(tx, "SHARE"); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&plan, "id = ?", planID).Error; err != nil {
			return err
		}
		var version storage.PlanVersion
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&version, "id = ? AND plan_id = ?", versionID, planID).Error; err != nil {
			return err
		}
		if mapping.PlanVersionID != "" {
			var current storage.PlanLicenseMapping
			if tx.First(&current, "plan_version_id = ?", versionID).Error != nil || current.ConnectionID != mapping.ConnectionID || current.RemoteProductID != mapping.RemoteProductID || current.RemotePolicyID != mapping.RemotePolicyID || current.RemotePolicyVersion != mapping.RemotePolicyVersion {
				return problem(409, "MAPPING_CHANGED", "套餐映射刚刚更新，请核对后重新发布")
			}
		}
		var product storage.Product
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&product, "id = ? AND status = 'published'", plan.ProductID).Error; err != nil {
			return problem(409, "PRODUCT_NOT_PUBLISHED", "请先发布产品")
		}
		currentBinding, bindingErr := s.productLicensing(tx, product.ID, environment)
		if bindingErr != nil {
			return bindingErr
		}
		if currentBinding.Provider == CustomLicensing {
			return problem(409, "MAPPING_CHANGED", "授权提供方刚刚更新，请重新发布")
		}
		if mapping.PlanVersionID != "" {
			remoteProduct, err := productRemoteID(tx, product, "")
			if err != nil {
				return err
			}
			if remoteProduct != "" && remoteProduct != mapping.RemoteProductID {
				return productMappingConflict()
			}
		}
		at := now()
		publication := storage.PlanPublication{PlanID: planID, Environment: environment, VersionID: versionID, PreviewOnly: previewOnly, PublishedAt: at}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "plan_id"}, {Name: "environment"}}, DoUpdates: clause.AssignmentColumns([]string{"version_id", "preview_only", "published_at"})}).Create(&publication).Error; err != nil {
			return err
		}
		if err := tx.Model(&version).Where("published_at IS NULL").Update("published_at", at).Error; err != nil {
			return err
		}
		if err := tx.Model(&plan).Update("status", "published").Error; err != nil {
			return err
		}
		action := "plan.publish_preview"
		if environment == "live" {
			action = "plan.publish_live"
		} else if !previewOnly {
			action = "plan.publish_sandbox"
		}
		return audit(tx, p.User.ID, action, "plan", planID, "")
	}))
}

func (s *Service) ArchivePlan(ctx context.Context, p Principal, id string) error {
	if !p.CanManage() {
		return ErrForbidden
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&storage.Plan{}).Where("id = ?", id).Updates(map[string]any{"status": "archived", "allow_purchase": false})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		return audit(tx, p.User.ID, "plan.archive", "plan", id, "")
	}))
}

type Dashboard struct {
	Users          int64           `json:"users"`
	Orders         int64           `json:"orders"`
	PendingMail    int64           `json:"pending_mail"`
	FailedMail     int64           `json:"failed_mail"`
	MailConfigured bool            `json:"mail_configured"`
	Readiness      []ReadinessItem `json:"readiness"`
	ServerTime     time.Time       `json:"server_time"`
}

type ReadinessItem struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Ready  bool   `json:"ready"`
	Detail string `json:"detail"`
	Target string `json:"target"`
}

func (s *Service) Dashboard(ctx context.Context, p Principal) (Dashboard, error) {
	if p.Role == "customer" {
		return Dashboard{}, ErrForbidden
	}
	mailConfigured, err := s.mailConfigured(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	v := Dashboard{MailConfigured: mailConfigured, ServerTime: now()}
	if s.db.WithContext(ctx).Model(&storage.User{}).
		Where("NOT EXISTS (SELECT 1 FROM staff_accounts WHERE staff_accounts.user_id = users.id)").
		Count(&v.Users).Error != nil {
		return v, ErrStorage
	}
	queries := []struct {
		model any
		where string
		count *int64
	}{{&storage.Order{}, "environment = 'live'", &v.Orders}, {&storage.EmailOutbox{}, "status IN ('pending','running','retrying')", &v.PendingMail}, {&storage.EmailOutbox{}, "status = 'manual_review'", &v.FailedMail}}
	for _, query := range queries {
		if s.db.WithContext(ctx).Model(query.model).Where(query.where).Count(query.count).Error != nil {
			return v, ErrStorage
		}
	}
	counts := map[string]int64{}
	checks := []struct {
		key   string
		model any
		where string
	}{
		{"products", &storage.Product{}, "status = 'published'"},
		{"licensing", &storage.LicentivoConnection{}, "1 = 1"},
		{"mappings", &storage.PlanLicenseMapping{}, "1 = 1"},
		{"sandbox_payments", &storage.PaymentAccount{}, "environment = 'sandbox' AND enabled = true"},
		{"live_payments", &storage.PaymentAccount{}, "environment = 'live' AND enabled = true"},
		{"translation", &storage.TranslationSettings{}, "1 = 1"},
		{"live_plans", &storage.PlanPublication{}, "environment = 'live' AND preview_only = false"},
	}
	for _, check := range checks {
		var count int64
		if s.db.WithContext(ctx).Model(check.model).Where(check.where).Count(&count).Error != nil {
			return v, ErrStorage
		}
		counts[check.key] = count
	}
	licenseReady := counts["licensing"] > 0 && counts["mappings"] > 0
	var externalConnections, externalMappings int64
	if s.db.WithContext(ctx).Model(&storage.ExternalLicensingConnection{}).Where("last_check_error IS NULL").Count(&externalConnections).Error != nil || s.db.WithContext(ctx).Model(&storage.ExternalPlanMapping{}).Count(&externalMappings).Error != nil {
		return v, ErrStorage
	}
	selectedProvider, err := s.activeLicensingProvider(s.db.WithContext(ctx))
	if err != nil {
		return v, ErrStorage
	}
	if selectedProvider == CustomLicensing {
		licenseReady = externalConnections > 0 && externalMappings > 0
	}
	v.Readiness = []ReadinessItem{
		{Key: "product", Label: "公开产品", Ready: counts["products"] > 0, Detail: "发布至少一个带介绍和下载信息的软件产品", Target: "#/admin/product"},
		{Key: "mail", Label: "邮件（可选）", Ready: true, Detail: "未启用邮件时允许直接注册；需要邮件时可配置 Resend 或 SMTP", Target: "#/admin/settings/mail"},
		{Key: "licensing", Label: "授权服务", Ready: licenseReady, Detail: "配置 Licentivo 或自定义授权服务，并完成商品套餐映射", Target: "#/admin/settings/licensing"},
		{Key: "sandbox_payment", Label: "测试收款", Ready: counts["sandbox_payments"] > 0, Detail: "配置支付渠道，并完成一次测试付款和退款", Target: "#/admin/settings/payments"},
		{Key: "translation", Label: "DeepL 多语言", Ready: counts["translation"] > 0, Detail: "连接 DeepL，自动生成其余九种语言", Target: "#/admin/settings"},
		{Key: "live_payment", Label: "正式收款", Ready: counts["live_payments"] > 0, Detail: "验证并保存正式收款凭证和 Webhook", Target: "#/admin/settings"},
		{Key: "live_plan", Label: "正式套餐", Ready: counts["live_plans"] > 0, Detail: "完成外部服务配置后，将套餐发布到正式环境", Target: "#/admin/plans"},
	}
	return v, nil
}
