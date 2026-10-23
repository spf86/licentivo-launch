package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/storage"
)

var integrationUUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type IntegrationInput struct {
	ProductID   string `json:"product_id"`
	Environment string `json:"environment"`
	Language    string `json:"language"`
}

type IntegrationCheck struct {
	Code    string `json:"code"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type IntegrationConfig struct {
	SchemaVersion      int    `json:"schema_version"`
	MerchantOrigin     string `json:"merchant_origin"`
	LaunchProductID    string `json:"launch_product_id"`
	LicentivoProductID string `json:"licentivo_product_id"`
	Environment        string `json:"environment"`
}

// PublicProductConfig intentionally omits license keys, device state, cache
// paths and management credentials. Only these fields can enter a download.
type PublicProductConfig struct {
	BaseURL     string            `json:"base_url"`
	ProductID   string            `json:"product_id"`
	TrustedKeys map[string]string `json:"trusted_keys"`
	AppVersion  string            `json:"app_version,omitempty"`
}

type IntegrationReport struct {
	Ready         bool                 `json:"ready"`
	Config        IntegrationConfig    `json:"config"`
	Checks        []IntegrationCheck   `json:"checks"`
	ProductConfig *PublicProductConfig `json:"product_config,omitempty"`
	Language      string               `json:"language"`
	ExamplePath   string               `json:"example_path"`
	CheckedAt     time.Time            `json:"checked_at"`
}

// Validate only the public fields read from the mapped official product.
func validatePublicProductConfig(raw []byte, productID string) (*PublicProductConfig, error) {
	var in struct {
		BaseURL     string            `json:"base_url"`
		ProductID   string            `json:"product_id"`
		TrustedKeys map[string]string `json:"trusted_keys"`
		AppVersion  string            `json:"app_version"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&in) != nil || decoder.Decode(&struct{}{}) != io.EOF || in.ProductID == "" || in.ProductID != productID || len(in.AppVersion) > 80 {
		return nil, problem(400, "PRODUCT_CONFIG_INVALID", "产品公开参数无效；映射必须匹配，且不得包含客户凭据或未知字段")
	}
	if in.BaseURL == "" {
		in.BaseURL = LicentivoServiceURL
	}
	if strings.TrimRight(in.BaseURL, "/") != LicentivoServiceURL {
		return nil, problem(400, "PRODUCT_CONFIG_ORIGIN_MISMATCH", "授权服务地址必须与当前 Licentivo 官方连接一致")
	}
	if len(in.TrustedKeys) < 1 || len(in.TrustedKeys) > 32 {
		return nil, problem(400, "PRODUCT_CONFIG_KEYS_INVALID", "产品必须包含有效的签名验证公钥")
	}
	for id, encoded := range in.TrustedKeys {
		key, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
		if id == "" || len(id) > 128 || strings.IndexFunc(id, func(r rune) bool { return r < 32 || r == 127 }) >= 0 || err != nil || len(key) != 32 || base64.RawURLEncoding.EncodeToString(key) != encoded {
			return nil, problem(400, "PRODUCT_CONFIG_KEYS_INVALID", "签名公钥必须使用官方提供的 32 字节 Ed25519 Base64URL 格式")
		}
	}
	return &PublicProductConfig{BaseURL: LicentivoServiceURL, ProductID: in.ProductID, TrustedKeys: in.TrustedKeys, AppVersion: in.AppVersion}, nil
}

func integrationExamplePath(language string) string {
	return map[string]string{
		"go": "examples/go/main.go", "python": "examples/python/main.py", "csharp": "examples/csharp/Program.cs", "java": "examples/java/Main.java", "javascript": "examples/javascript/main.mjs", "rust": "examples/rust/src/main.rs", "ruby": "examples/ruby/main.rb", "c": "examples/c/main.c", "cpp": "examples/cpp/main.cpp", "web": "examples/web-backend/main.go",
	}[language]
}

func (s *Service) CheckIntegration(ctx context.Context, p Principal, in IntegrationInput) (IntegrationReport, error) {
	var report IntegrationReport
	if !p.IsAdmin() {
		return report, ErrForbidden
	}
	if !integrationUUIDPattern.MatchString(in.ProductID) || (in.Environment != "live" && in.Environment != "sandbox") {
		return report, ErrInvalid
	}
	if integrationExamplePath(in.Language) == "" {
		return report, ErrInvalid
	}
	var product storage.Product
	if err := s.db.WithContext(ctx).First(&product, "id = ?", in.ProductID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return report, ErrNotFound
		}
		return report, ErrStorage
	}
	report = IntegrationReport{Ready: true, Language: in.Language, CheckedAt: now(), Checks: []IntegrationCheck{}, Config: IntegrationConfig{SchemaVersion: 1, MerchantOrigin: s.PublicURL(), LaunchProductID: product.ID, Environment: in.Environment}}
	check := func(code, status, message string) {
		report.Checks = append(report.Checks, IntegrationCheck{code, status, message})
		if status == "failed" {
			report.Ready = false
		}
	}
	origin, originErr := url.Parse(report.Config.MerchantOrigin)
	if originErr != nil || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || (origin.Path != "" && origin.Path != "/") || (origin.Scheme != "https" && !(in.Environment == "sandbox" && origin.Scheme == "http" && (origin.Hostname() == "localhost" || origin.Hostname() == "127.0.0.1" || origin.Hostname() == "::1"))) {
		check("MERCHANT_ORIGIN_INVALID", "failed", "请在网站设置中配置 HTTPS 公开访问地址；本机 HTTP 仅用于测试环境")
	} else {
		check("MERCHANT_ORIGIN_READY", "ready", "商户访问地址有效")
	}
	var publications []storage.PlanPublication
	if s.db.WithContext(ctx).Table("plan_publications").Select("plan_publications.*").Joins("JOIN plans ON plans.id = plan_publications.plan_id").Where("plans.product_id = ? AND plans.status = 'published' AND plan_publications.environment = ?", product.ID, in.Environment).Order("plan_publications.plan_id").Find(&publications).Error != nil {
		return report, ErrStorage
	}
	if len(publications) == 0 {
		check("PUBLISHED_PLAN_REQUIRED", "failed", "该环境还没有已发布套餐，请先在销售套餐中选择 Licentivo 策略并发布")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	providers := map[string]ports.LicensingProvider{}
	keyChecks := map[string]bool{}
	for _, publication := range publications {
		var version storage.PlanVersion
		var mapping storage.PlanLicenseMapping
		if s.db.WithContext(ctx).First(&version, "id = ?", publication.VersionID).Error != nil {
			return report, ErrStorage
		}
		if publication.PreviewOnly {
			check("PLAN_PREVIEW_ONLY", "failed", "套餐 "+version.PlanID+" 仅供预览，请完成授权映射并发布可购买版本")
		}
		if err := s.db.WithContext(ctx).First(&mapping, "plan_version_id = ?", version.ID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				check("MAPPING_REQUIRED", "failed", "套餐 "+version.PlanID+" 缺少 Licentivo 授权映射")
				continue
			}
			return report, ErrStorage
		}
		if report.Config.LicentivoProductID == "" {
			report.Config.LicentivoProductID = mapping.RemoteProductID
		} else if report.Config.LicentivoProductID != mapping.RemoteProductID {
			check("PRODUCT_MAPPING_CONFLICT", "failed", "同一 Launch 产品的套餐映射到了不同 Licentivo 产品，请创建一致的套餐新版本")
			continue
		}
		provider := providers[mapping.ConnectionID]
		if provider == nil {
			var connection storage.LicentivoConnection
			if s.db.WithContext(ctx).First(&connection, "id = ?", mapping.ConnectionID).Error != nil {
				return report, ErrStorage
			}
			var err error
			provider, err = s.licensingProvider(ctx, connection)
			if err != nil {
				check("LICENSE_CONNECTION_FAILED", "failed", "无法连接 Licentivo，请检查授权连接后重试")
				continue
			}
			caps, err := provider.Capabilities(ctx)
			if err != nil || caps.ValidateSales() != nil {
				check("LICENSE_CONNECTION_FAILED", "failed", "Licentivo 连接或管理权限检查未通过，请在授权连接中重新验证")
				continue
			}
			providers[mapping.ConnectionID] = provider
		}
		if err := checkPolicy(ctx, provider, version, mapping, ""); err != nil {
			check("POLICY_MAPPING_MISMATCH", "failed", "套餐 "+version.PlanID+" 的远程策略检查未通过，请检查连接与策略版本；策略有变更时请创建匹配的新版本")
			continue
		}
		check("POLICY_MAPPING_READY", "ready", fmt.Sprintf("套餐 %s：Licentivo 产品与策略版本 %d 已在线验证", version.PlanID, mapping.RemotePolicyVersion))
		if !keyChecks[mapping.ConnectionID] {
			keyChecks[mapping.ConnectionID] = true
			reader, supported := provider.(ports.ProductPublicKeyProvider)
			if !supported {
				check("PUBLIC_KEYS_UNAVAILABLE", "failed", "授权连接暂不支持自动读取签名公钥，请检查 Licentivo 连接")
				continue
			}
			keys, err := reader.ProductPublicKeys(ctx, mapping.RemoteProductID)
			if err != nil {
				check("PUBLIC_KEYS_UNAVAILABLE", "failed", "无法自动读取产品签名公钥，请检查 Licentivo 连接和产品签名密钥状态")
				continue
			}
			raw := marshal(PublicProductConfig{BaseURL: LicentivoServiceURL, ProductID: mapping.RemoteProductID, TrustedKeys: keys})
			public, err := validatePublicProductConfig(raw, mapping.RemoteProductID)
			if err != nil {
				check("PUBLIC_KEYS_INVALID", "failed", "Licentivo 返回的产品公钥格式无效，暂时无法生成接入代码")
				continue
			}
			report.ProductConfig = public
			check("PUBLIC_KEYS_READY", "ready", "已自动读取对应授权产品的签名公钥")
		}
	}
	if report.ProductConfig == nil {
		check("PRODUCT_SETUP_REQUIRED", "failed", "请先为该软件发布映射到 Licentivo 产品的套餐；接入信息将自动读取")
	}
	report.ExamplePath = integrationExamplePath(in.Language)
	return report, nil
}
