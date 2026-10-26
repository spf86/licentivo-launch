package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/adapters/externallicensing"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/storage"
)

const CustomLicensing = "custom_http"

// ExternalSaleRoute is frozen in each paid coverage snapshot. No secret or
// endpoint can be redirected by editing a draft or selecting another product.
type ExternalSaleRoute struct {
	ConnectionID  string `json:"connection_id"`
	ConnectionRef string `json:"connection_ref"`
	Environment   string `json:"environment"`
	ProductRef    string `json:"product_ref"`
	PlanRef       string `json:"plan_ref"`
}

func externalRoute(raw []byte) *ExternalSaleRoute {
	var snapshot PlanSnapshot
	if json.Unmarshal(raw, &snapshot) != nil {
		return nil
	}
	return snapshot.External
}
func validEnvironment(env string) bool { return env == "sandbox" || env == "live" }

type ExternalConnectionInput struct {
	Provider            string   `json:"provider"`
	Name                string   `json:"name"`
	BaseURL             string   `json:"base_url"`
	Environment         string   `json:"environment"`
	ConnectionRef       string   `json:"connection_ref"`
	KeyID               string   `json:"key_id"`
	SigningKey          string   `json:"signing_key,omitempty"`
	AllowPrivateNetwork bool     `json:"allow_private_network"`
	AllowLocalHTTP      bool     `json:"allow_local_http"`
	ActivationOrigins   []string `json:"activation_origins"`
}
type ExternalConnectionView struct {
	ID                  string                     `json:"id"`
	Provider            string                     `json:"provider"`
	Name                string                     `json:"name"`
	BaseURL             string                     `json:"base_url"`
	Environment         string                     `json:"environment"`
	ConnectionRef       string                     `json:"connection_ref"`
	KeyID               string                     `json:"key_id"`
	CredentialStored    bool                       `json:"credential_stored"`
	AllowPrivateNetwork bool                       `json:"allow_private_network"`
	AllowLocalHTTP      bool                       `json:"allow_local_http"`
	ActivationOrigins   []string                   `json:"activation_origins"`
	Capabilities        ports.ExternalCapabilities `json:"capabilities"`
	LastCheckedAt       *time.Time                 `json:"last_checked_at"`
	LastCheckError      *string                    `json:"last_check_error"`
}

func externalConnectionView(c storage.ExternalLicensingConnection) ExternalConnectionView {
	var caps ports.ExternalCapabilities
	_ = json.Unmarshal(c.Capabilities, &caps)
	origins := []string{}
	_ = json.Unmarshal(c.ActivationOrigins, &origins)
	return ExternalConnectionView{c.ID, CustomLicensing, c.Name, c.BaseURL, c.Environment, c.ConnectionRef, c.KeyID, true, c.AllowPrivateNetwork, c.AllowLocalHTTP, origins, caps, c.LastCheckedAt, c.LastCheckError}
}
func (s *Service) externalClient(ctx context.Context, c storage.ExternalLicensingConnection) (ports.ExternalLicensing, error) {
	if s.config.ExternalLicensingFactory != nil {
		return s.config.ExternalLicensingFactory(ctx, c)
	}
	key, err := s.readSecret(ctx, c.CredentialSecretID)
	if err != nil {
		return nil, err
	}
	return externallicensing.New(externallicensing.Config{BaseURL: c.BaseURL, ConnectionRef: c.ConnectionRef, Environment: c.Environment, KeyID: c.KeyID, Key: key, AllowPrivateNetwork: c.AllowPrivateNetwork, AllowLocalHTTP: c.AllowLocalHTTP})
}
func (s *Service) SaveExternalConnection(ctx context.Context, p Principal, id string, in ExternalConnectionInput) (ExternalConnectionView, error) {
	if !p.IsAdmin() {
		return ExternalConnectionView{}, ErrForbidden
	}
	if in.Provider != CustomLicensing || !validEnvironment(in.Environment) || len(strings.TrimSpace(in.Name)) < 2 || len(in.Name) > 160 || len(in.BaseURL) > 512 || !ports.ExternalReference.MatchString(in.ConnectionRef) || len(in.KeyID) > 64 || !ports.ExternalReference.MatchString(in.KeyID) || len(in.ActivationOrigins) > 16 {
		return ExternalConnectionView{}, ErrInvalid
	}
	origins := []string{}
	for _, origin := range in.ActivationOrigins {
		u, e := url.Parse(origin)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return ExternalConnectionView{}, problem(400, "ACTIVATION_ORIGIN_INVALID", "激活入口来源必须是 HTTPS origin")
		}
		origins = append(origins, "https://"+strings.ToLower(u.Host))
	}
	var saved storage.ExternalLicensingConnection
	if id != "" {
		if !integrationUUIDPattern.MatchString(id) {
			return ExternalConnectionView{}, ErrInvalid
		}
		if err := s.db.WithContext(ctx).First(&saved, "id = ?", id).Error; err != nil {
			return ExternalConnectionView{}, databaseError(err)
		}
	}
	var key []byte
	var err error
	if in.SigningKey != "" {
		key, err = base64.StdEncoding.Strict().DecodeString(in.SigningKey)
		if err != nil || len(key) < 32 || len(key) > 128 {
			return ExternalConnectionView{}, problem(400, "SIGNING_KEY_INVALID", "签名密钥须为 32–128 个随机字节的标准 Base64 编码")
		}
	} else if saved.ID != "" {
		key, err = s.readSecret(ctx, saved.CredentialSecretID)
		if err != nil {
			return ExternalConnectionView{}, err
		}
	} else {
		return ExternalConnectionView{}, problem(400, "SIGNING_KEY_REQUIRED", "首次连接需要签名密钥")
	}
	base := strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	client, err := externallicensing.New(externallicensing.Config{BaseURL: base, ConnectionRef: in.ConnectionRef, Environment: in.Environment, KeyID: in.KeyID, Key: key, AllowPrivateNetwork: in.AllowPrivateNetwork, AllowLocalHTTP: in.AllowLocalHTTP})
	if err != nil {
		return ExternalConnectionView{}, problem(400, "EXTERNAL_CONFIG_INVALID", "请检查 HTTPS 地址、环境、网络访问选项和签名配置")
	}
	caps, err := client.Capabilities(ctx)
	if err != nil {
		return ExternalConnectionView{}, problem(400, "EXTERNAL_CONNECTION_FAILED", "自定义授权服务的鉴权、环境或协议检查未通过")
	}
	if id == "" {
		id, err = newID()
		if err != nil {
			return ExternalConnectionView{}, err
		}
	}
	row := storage.ExternalLicensingConnection{ID: id, Name: strings.TrimSpace(in.Name), BaseURL: base, Environment: in.Environment, ConnectionRef: in.ConnectionRef, KeyID: in.KeyID, AllowPrivateNetwork: in.AllowPrivateNetwork, AllowLocalHTTP: in.AllowLocalHTTP, ActivationOrigins: marshal(origins), Capabilities: marshal(caps), CreatedAt: now(), UpdatedAt: now()}
	checked := now()
	row.LastCheckedAt = &checked
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if saved.ID != "" {
			var locked storage.ExternalLicensingConnection
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, "id = ?", id).Error; err != nil {
				return err
			}
			if !locked.UpdatedAt.Equal(saved.UpdatedAt) {
				return problem(409, "CONNECTION_CHANGED", "连接刚刚更新，请刷新后重试")
			}
			if locked.BaseURL != base || locked.Environment != in.Environment || locked.ConnectionRef != in.ConnectionRef || locked.AllowPrivateNetwork != in.AllowPrivateNetwork || locked.AllowLocalHTTP != in.AllowLocalHTTP {
				return problem(409, "CONNECTION_ROUTE_IMMUTABLE", "地址、环境及网络策略请使用新连接；历史任务保留原接收方")
			}
			row.CreatedAt = locked.CreatedAt
		}
		secret, e := s.createSecret(tx, "external_licensing_hmac", key)
		if e != nil {
			return e
		}
		row.CredentialSecretID = secret
		if e := tx.Omit(clause.Associations).Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error; e != nil {
			return e
		}
		return audit(tx, p.User.ID, "licensing.connection.save", "external_licensing_connection", id, "")
	})
	if err != nil {
		return ExternalConnectionView{}, databaseError(err)
	}
	return externalConnectionView(row), nil
}

func (s *Service) CheckExternalConnection(ctx context.Context, p Principal, id string) (ExternalConnectionView, error) {
	if !p.IsAdmin() {
		return ExternalConnectionView{}, ErrForbidden
	}
	var row storage.ExternalLicensingConnection
	if err := s.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		return ExternalConnectionView{}, databaseError(err)
	}
	client, err := s.externalClient(ctx, row)
	var caps ports.ExternalCapabilities
	if err == nil {
		caps, err = client.Capabilities(ctx)
	}
	at := now()
	changes := map[string]any{"last_checked_at": at, "last_check_error": nil}
	if err != nil {
		message := "自定义授权服务检查失败"
		row.LastCheckError = &message
		changes["last_check_error"] = message
	} else {
		row.Capabilities = marshal(caps)
		row.LastCheckError = nil
		changes["capabilities"] = row.Capabilities
	}
	row.LastCheckedAt = &at
	if e := s.db.WithContext(ctx).Model(&row).Updates(changes).Error; e != nil {
		return ExternalConnectionView{}, ErrStorage
	}
	return externalConnectionView(row), nil
}
func (s *Service) LicensingConnections(ctx context.Context, p Principal) (map[string]any, error) {
	if !p.IsAdmin() {
		return nil, ErrForbidden
	}
	var rows []storage.ExternalLicensingConnection
	var bindings []storage.ProductLicensingBinding
	var mappings []storage.ExternalPlanMapping
	if s.db.WithContext(ctx).Order("created_at,id").Find(&rows).Error != nil || s.db.WithContext(ctx).Find(&bindings).Error != nil || s.db.WithContext(ctx).Find(&mappings).Error != nil {
		return nil, ErrStorage
	}
	views := []ExternalConnectionView{}
	for _, row := range rows {
		views = append(views, externalConnectionView(row))
	}
	safeBindings := []map[string]any{}
	for _, row := range bindings {
		safeBindings = append(safeBindings, map[string]any{"product_id": row.ProductID, "environment": row.Environment, "provider": row.Provider, "connection_id": row.ConnectionID})
	}
	safeMappings := []map[string]any{}
	for _, row := range mappings {
		safeMappings = append(safeMappings, map[string]any{"version_id": row.PlanVersionID, "environment": row.Environment, "connection_id": row.ConnectionID, "product_ref": row.ProductRef, "plan_ref": row.PlanRef})
	}
	selected, err := s.activeLicensingProvider(s.db.WithContext(ctx))
	if err != nil {
		return nil, ErrStorage
	}
	return map[string]any{"connections": views, "bindings": safeBindings, "mappings": safeMappings, "default_provider": selected}, nil
}

type ExternalMappingInput struct {
	VersionID  string `json:"version_id"`
	ProductRef string `json:"product_ref"`
	PlanRef    string `json:"plan_ref"`
}
type ProductLicensingInput struct {
	Provider     string                 `json:"provider"`
	Environment  string                 `json:"environment"`
	ConnectionID string                 `json:"connection_id,omitempty"`
	Mappings     []ExternalMappingInput `json:"mappings"`
}

func (s *Service) productLicensing(tx *gorm.DB, product, env string) (storage.ProductLicensingBinding, error) {
	selected, err := s.activeLicensingProvider(tx)
	if err != nil {
		return storage.ProductLicensingBinding{}, err
	}
	row := storage.ProductLicensingBinding{ProductID: product, Environment: env, Provider: selected}
	err = tx.First(&row, "product_id = ? AND environment = ?", product, env).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && row.Provider != selected {
		return storage.ProductLicensingBinding{ProductID: product, Environment: env, Provider: selected}, nil
	}
	return row, err
}
func (s *Service) SaveProductLicensing(ctx context.Context, p Principal, product string, in ProductLicensingInput) error {
	if !p.IsAdmin() {
		return ErrForbidden
	}
	if !integrationUUIDPattern.MatchString(product) || !validEnvironment(in.Environment) || (in.Provider != "licentivo" && in.Provider != CustomLicensing) || len(in.Mappings) > 100 {
		return ErrInvalid
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockProviderSettings(tx, "SHARE"); err != nil {
			return err
		}
		if err := s.requireLicensingProvider(tx, in.Provider); err != nil {
			return err
		}
		var parent storage.Product
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&parent, "id = ?", product).Error; err != nil {
			return err
		}
		binding, err := s.productLicensing(tx, product, in.Environment)
		if err != nil {
			return err
		}
		var connectionID *string
		if in.Provider == CustomLicensing {
			var connection storage.ExternalLicensingConnection
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&connection, "id = ? AND environment = ?", in.ConnectionID, in.Environment).Error; err != nil {
				return problem(409, "EXTERNAL_CONNECTION_REQUIRED", "请选择对应环境的自定义授权连接")
			}
			connectionID = &connection.ID
		}
		changed := binding.Provider != in.Provider || (binding.ConnectionID == nil) != (connectionID == nil) || (binding.ConnectionID != nil && connectionID != nil && *binding.ConnectionID != *connectionID)
		if changed {
			var count int64
			if err := tx.Model(&storage.Order{}).Where("product_id = ? AND environment = ?", product, in.Environment).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return problem(409, "LICENSING_MIGRATION_REQUIRED", "已有订单的商品不能直接切换授权提供方或连接，请创建新商品或执行专门迁移")
			}
			if err := tx.Model(&storage.PlanPublication{}).Where("environment = ? AND plan_id IN (SELECT id FROM plans WHERE product_id = ?)", in.Environment, product).Update("preview_only", true).Error; err != nil {
				return err
			}
		}
		seen := map[string]bool{}
		productRef := ""
		for _, input := range in.Mappings {
			if productRef == "" {
				productRef = input.ProductRef
			} else if productRef != input.ProductRef {
				return problem(409, "PRODUCT_MAPPING_CONFLICT", "同一商品和环境的外部商品引用必须一致")
			}
		}
		if in.Provider == CustomLicensing && productRef != "" {
			var previousMappings []storage.ExternalPlanMapping
			if err := tx.Table("external_plan_mappings m").Select("m.*").Joins("JOIN plan_versions v ON v.id = m.plan_version_id").Where("v.product_id = ? AND m.environment = ? AND m.connection_id = ?", product, in.Environment, in.ConnectionID).Find(&previousMappings).Error; err != nil {
				return err
			}
			for _, previous := range previousMappings {
				if previous.ProductRef != productRef {
					return problem(409, "PRODUCT_MAPPING_CONFLICT", "同一商品和环境的外部商品引用必须与已有映射一致")
				}
			}
		}
		for _, input := range in.Mappings {
			if in.Provider != CustomLicensing || !integrationUUIDPattern.MatchString(input.VersionID) || !ports.ExternalReference.MatchString(input.ProductRef) || !ports.ExternalReference.MatchString(input.PlanRef) || seen[input.VersionID] {
				return ErrInvalid
			}
			seen[input.VersionID] = true
			var version storage.PlanVersion
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&version, "id = ? AND product_id = ?", input.VersionID, product).Error; err != nil {
				return err
			}
			var previous storage.ExternalPlanMapping
			e := tx.First(&previous, "plan_version_id = ? AND environment = ?", version.ID, in.Environment).Error
			if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
				return e
			}
			if previous.PlanVersionID != "" && (previous.ConnectionID != in.ConnectionID || previous.ProductRef != input.ProductRef || previous.PlanRef != input.PlanRef) {
				var count int64
				if err := tx.Model(&storage.PlanPublication{}).Where("version_id = ? AND environment = ? AND preview_only = false", version.ID, in.Environment).Count(&count).Error; err != nil {
					return err
				}
				if count > 0 {
					return problem(409, "PUBLISHED_MAPPING_IMMUTABLE", "销售中的套餐映射不可更换，请创建新版本")
				}
			}
			mapping := storage.ExternalPlanMapping{PlanVersionID: version.ID, Environment: in.Environment, ConnectionID: in.ConnectionID, ProductRef: input.ProductRef, PlanRef: input.PlanRef}
			if err := tx.Omit(clause.Associations).Clauses(clause.OnConflict{UpdateAll: true}).Create(&mapping).Error; err != nil {
				return err
			}
		}
		binding.Provider = in.Provider
		binding.ConnectionID = connectionID
		binding.UpdatedAt = now()
		if err := tx.Omit(clause.Associations).Clauses(clause.OnConflict{UpdateAll: true}).Create(&binding).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "product.licensing.configure", "product", product, in.Environment)
	}))
}

func (s *Service) saleMapping(ctx context.Context, tx *gorm.DB, version storage.PlanVersion, env string, check bool) (storage.PlanLicenseMapping, *ExternalSaleRoute, error) {
	binding, err := s.productLicensing(tx, version.ProductID, env)
	if err != nil {
		return storage.PlanLicenseMapping{}, nil, err
	}
	var mapping storage.PlanLicenseMapping
	if binding.Provider != CustomLicensing {
		if err := tx.First(&mapping, "plan_version_id = ?", version.ID).Error; err != nil {
			return mapping, nil, problem(409, "LICENSE_MAPPING_REQUIRED", "请先为该套餐配置 Licentivo 授权策略")
		}
		if check {
			err = s.registerLaunchPlan(ctx, env, version, mapping, true)
		}
		return mapping, nil, err
	}
	var selected storage.ExternalPlanMapping
	var connection storage.ExternalLicensingConnection
	if binding.ConnectionID == nil || tx.First(&selected, "plan_version_id = ? AND environment = ? AND connection_id = ?", version.ID, env, binding.ConnectionID).Error != nil || tx.First(&connection, "id = ? AND environment = ?", selected.ConnectionID, env).Error != nil {
		return mapping, nil, problem(409, "EXTERNAL_MAPPING_REQUIRED", "请先为对应环境保存自定义授权连接及套餐映射")
	}
	var caps ports.ExternalCapabilities
	if json.Unmarshal(connection.Capabilities, &caps) != nil || caps.Validate() != nil || caps.ConnectionRef != connection.ConnectionRef || caps.Environment != env {
		return mapping, nil, ErrStorage
	}
	if check {
		client, e := s.externalClient(ctx, connection)
		if e != nil {
			return mapping, nil, e
		}
		caps, e = client.Capabilities(ctx)
		if e != nil {
			return mapping, nil, problem(503, "EXTERNAL_CONNECTION_UNAVAILABLE", "自定义授权服务暂不可用，请稍后重试")
		}
	}
	if !slices.Contains(caps.PurchaseTypes, version.PurchaseType) {
		return mapping, nil, problem(409, "PURCHASE_TYPE_UNSUPPORTED", "自定义授权服务不支持该购买类型")
	}
	route := &ExternalSaleRoute{connection.ID, connection.ConnectionRef, env, selected.ProductRef, selected.PlanRef}
	mapping = storage.PlanLicenseMapping{PlanVersionID: version.ID, ConnectionID: connection.ID, RemoteProductID: selected.ProductRef, RemotePolicyID: selected.PlanRef, RemotePolicyVersion: 1}
	return mapping, route, nil
}
func externalPlanSnapshot(raw []byte, route *ExternalSaleRoute) []byte {
	if route == nil {
		return raw
	}
	var snapshot PlanSnapshot
	_ = json.Unmarshal(raw, &snapshot)
	snapshot.External = route
	snapshot.LicenseModel = "external"
	snapshot.MaxDevices = 0
	snapshot.Features = []string{}
	snapshot.OfflineMode = "external"
	return marshal(snapshot)
}

func (s *Service) publishExternalPlan(ctx context.Context, p Principal, candidate storage.PlanVersion, env string) error {
	mapping, route, err := s.saleMapping(ctx, s.db.WithContext(ctx), candidate, env, true)
	if err != nil {
		return err
	}
	if route == nil {
		return ErrStorage
	}
	var accounts int64
	if err := s.db.WithContext(ctx).Model(&storage.PaymentAccount{}).Where("environment = ? AND enabled = true", env).Count(&accounts).Error; err != nil {
		return ErrStorage
	}
	preview := accounts == 0
	if env == "live" && preview {
		return problem(409, "SALES_CONNECTION_REQUIRED", "正式发布前需要配置正式收款渠道和授权连接")
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockProviderSettings(tx, "SHARE"); err != nil {
			return err
		}
		var product storage.Product
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&product, "id = ? AND status = 'published'", candidate.ProductID).Error; err != nil {
			return problem(409, "PRODUCT_NOT_PUBLISHED", "请先发布产品")
		}
		var plan storage.Plan
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&plan, "id = ?", candidate.PlanID).Error; err != nil {
			return err
		}
		var version storage.PlanVersion
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&version, "id = ? AND plan_id = ?", candidate.ID, plan.ID).Error; err != nil {
			return err
		}
		current, currentRoute, err := s.saleMapping(ctx, tx, version, env, false)
		if err != nil {
			return err
		}
		if currentRoute == nil || current.ConnectionID != mapping.ConnectionID || current.RemoteProductID != mapping.RemoteProductID || current.RemotePolicyID != mapping.RemotePolicyID {
			return problem(409, "MAPPING_CHANGED", "授权配置刚刚更新，请重新发布")
		}
		at := now()
		publication := storage.PlanPublication{PlanID: plan.ID, VersionID: version.ID, Environment: env, PreviewOnly: preview, PublishedAt: at}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "plan_id"}, {Name: "environment"}}, DoUpdates: clause.AssignmentColumns([]string{"version_id", "preview_only", "published_at"})}).Create(&publication).Error; err != nil {
			return err
		}
		if err := tx.Model(&version).Where("published_at IS NULL").Update("published_at", at).Error; err != nil {
			return err
		}
		if err := tx.Model(&plan).Update("status", "published").Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "plan.publish_"+env, "plan", plan.ID, CustomLicensing)
	}))
}

func (s *Service) rejectExternalRuntime(ctx context.Context, product, env string) error {
	if !validEnvironment(env) || !integrationUUIDPattern.MatchString(product) {
		return ErrInvalid
	}
	binding, err := s.productLicensing(s.db.WithContext(ctx), product, env)
	if err != nil {
		return databaseError(err)
	}
	if binding.Provider == CustomLicensing {
		return problem(409, "CUSTOM_LICENSING_SDK_REQUIRED", "该商品使用商户自有授权系统，请使用其授权交付与 SDK；不支持 Licentivo 身份或设备接口")
	}
	return nil
}

func (s *Service) CustomerDelivery(ctx context.Context, p Principal, product, env string) (ports.ExternalDelivery, error) {
	if !validEnvironment(env) || !integrationUUIDPattern.MatchString(product) {
		return ports.ExternalDelivery{}, ErrInvalid
	}
	if err := s.requireVerifiedEmail(ctx, s.db, p.User); err != nil {
		return ports.ExternalDelivery{}, err
	}
	var purchase storage.Entitlement
	if err := s.db.WithContext(ctx).First(&purchase, "user_id = ? AND product_id = ? AND environment = ?", p.User.ID, product, env).Error; err != nil {
		return ports.ExternalDelivery{}, databaseError(err)
	}
	route := externalRoute(purchase.Snapshot)
	if route == nil {
		return ports.ExternalDelivery{}, problem(409, "LICENSING_DELIVERY_UNSUPPORTED", "该商品使用 Licentivo 账号授权，请按其应用接入流程使用")
	}
	if purchase.ManualHold || purchase.Status != "active" || purchase.AppliedVersion == 0 || purchase.AppliedVersion != purchase.DesiredVersion || purchase.ValidFrom == nil || purchase.ValidFrom.After(now()) || (purchase.ValidUntil != nil && !purchase.ValidUntil.After(now())) {
		return ports.ExternalDelivery{}, problem(409, "ENTITLEMENT_NOT_READY", "当前购买未就绪、已失效或暂停，暂不能取得交付")
	}
	var connection storage.ExternalLicensingConnection
	if s.db.WithContext(ctx).First(&connection, "id = ? AND environment = ?", route.ConnectionID, env).Error != nil {
		return ports.ExternalDelivery{}, ErrStorage
	}
	client, err := s.externalClient(ctx, connection)
	if err != nil {
		return ports.ExternalDelivery{}, err
	}
	delivery, err := client.Delivery(ctx, purchaseReference(purchase))
	if err != nil {
		return ports.ExternalDelivery{}, problem(503, "EXTERNAL_DELIVERY_PENDING", "授权交付暂不可用，请稍后重试")
	}
	if delivery.ConnectionRef != route.ConnectionRef || delivery.Environment != env || delivery.EntitlementRef != purchaseReference(purchase) || delivery.Version != purchase.AppliedVersion {
		return ports.ExternalDelivery{}, problem(503, "EXTERNAL_DELIVERY_INVALID", "授权交付与当前购买不一致")
	}
	if delivery.Status != "ready" {
		if (delivery.Status != "pending" && delivery.Status != "unavailable") || delivery.Delivery != nil {
			return ports.ExternalDelivery{}, ErrStorage
		}
		return delivery, nil
	}
	var caps ports.ExternalCapabilities
	_ = json.Unmarshal(connection.Capabilities, &caps)
	value := delivery.Delivery
	if value == nil || !slices.Contains(caps.DeliveryTypes, value.Type) || value.Value == "" || len(value.Value) > 4096 || (value.ExpiresAt != nil && !value.ExpiresAt.After(now())) {
		return ports.ExternalDelivery{}, problem(503, "EXTERNAL_DELIVERY_INVALID", "授权交付格式无效或已过期")
	}
	if value.Type == "activation_url" {
		u, e := url.Parse(value.Value)
		origins := []string{}
		_ = json.Unmarshal(connection.ActivationOrigins, &origins)
		if e != nil || u.Scheme != "https" || u.User != nil || !slices.Contains(origins, "https://"+strings.ToLower(u.Host)) {
			return ports.ExternalDelivery{}, problem(503, "EXTERNAL_DELIVERY_INVALID", "激活入口来源未获商家允许")
		}
	}
	// Recheck after the network round trip: a concurrent refund must not make
	// an old delivery response available through Launch.
	var current storage.Entitlement
	if s.db.WithContext(ctx).First(&current, "id = ?", purchase.ID).Error != nil {
		return ports.ExternalDelivery{}, ErrStorage
	}
	if current.Status != "active" || current.ManualHold || current.DesiredVersion != purchase.DesiredVersion || current.AppliedVersion != purchase.AppliedVersion || purchaseReference(current) != purchaseReference(purchase) || current.ValidFrom == nil || current.ValidFrom.After(now()) || (current.ValidUntil != nil && !current.ValidUntil.After(now())) {
		return ports.ExternalDelivery{}, problem(409, "ENTITLEMENT_NOT_READY", "购买状态已变化，请刷新后重试")
	}
	var currentUser storage.User
	if s.db.WithContext(ctx).First(&currentUser, "id = ? AND status = 'active'", p.User.ID).Error != nil {
		return ports.ExternalDelivery{}, ErrUnauthorized
	}
	if p.Session.ID != "" {
		var session storage.Session
		if s.db.WithContext(ctx).First(&session, "id = ? AND user_id = ? AND revoked_at IS NULL AND expires_at > ?", p.Session.ID, p.User.ID, now()).Error != nil {
			return ports.ExternalDelivery{}, ErrUnauthorized
		}
	}
	return delivery, nil
}
