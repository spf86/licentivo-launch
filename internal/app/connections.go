package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"

	"gorm.io/gorm"

	"licentivo-launch/internal/adapters/licentivo"
	"licentivo-launch/internal/payments"
	"licentivo-launch/internal/ports"
	"licentivo-launch/internal/storage"
)

type PaymentSettings struct {
	Provider      string `json:"provider"`
	Environment   string `json:"environment"`
	MerchantRef   string `json:"merchant_ref"`
	APIKey        string `json:"api_key,omitempty"`
	WebhookSecret string `json:"webhook_secret,omitempty"`
	APIVersion    string `json:"api_version"`
	ClientID      string `json:"client_id,omitempty"`
	ClientSecret  string `json:"client_secret,omitempty"`
	WebhookID     string `json:"webhook_id,omitempty"`
	PrivateKey    string `json:"private_key,omitempty"`
	PublicKey     string `json:"public_key,omitempty"`
	ClientToken   string `json:"client_token,omitempty"`
	ProductID     string `json:"product_id,omitempty"`
}

func provider(config PaymentSettings) (ports.PaymentProvider, error) {
	if config.Provider == "stripe" {
		return payments.NewStripe(payments.StripeConfig{APIKey: config.APIKey, WebhookSecret: config.WebhookSecret, MerchantRef: config.MerchantRef, APIVersion: config.APIVersion, Environment: ports.Environment(config.Environment)})
	}
	if config.Provider == "paypal" {
		return payments.NewPayPal(payments.PayPalConfig{ClientID: config.ClientID, ClientSecret: config.ClientSecret, MerchantRef: config.MerchantRef, WebhookID: config.WebhookID, Environment: ports.Environment(config.Environment)})
	}
	if config.Provider == "waffo" {
		return payments.NewWaffo(payments.WaffoConfig{APIKey: config.APIKey, MerchantRef: config.MerchantRef, PrivateKey: config.PrivateKey, PublicKey: config.PublicKey, Environment: ports.Environment(config.Environment)})
	}
	if config.Provider == "paddle" {
		return payments.NewPaddle(payments.PaddleConfig{APIKey: config.APIKey, WebhookSecret: config.WebhookSecret, MerchantRef: config.MerchantRef, ClientToken: config.ClientToken, ProductID: config.ProductID, Environment: ports.Environment(config.Environment)})
	}
	return nil, ErrInvalid
}
func (s *Service) paymentProvider(ctx context.Context, account storage.PaymentAccount) (ports.PaymentProvider, error) {
	if s.config.PaymentFactory != nil {
		return s.config.PaymentFactory(ctx, account)
	}
	plain, err := s.readSecret(ctx, account.CredentialSecretID)
	if err != nil {
		return nil, err
	}
	var config PaymentSettings
	if json.Unmarshal(plain, &config) != nil || config.Provider != account.Provider || config.Environment != account.Environment || config.MerchantRef != account.MerchantRef {
		return nil, ErrStorage
	}
	return provider(config)
}
func (s *Service) readSecret(ctx context.Context, id string) ([]byte, error) {
	var record storage.SecretRecord
	if s.db.WithContext(ctx).First(&record, "id = ?", id).Error != nil {
		return nil, ErrStorage
	}
	plain, err := s.vault.Open(record.Ciphertext, "secret:"+id)
	if err != nil {
		return nil, ErrStorage
	}
	return plain, nil
}
func (s *Service) createSecret(tx *gorm.DB, purpose string, value []byte) (string, error) {
	id, err := newID()
	if err != nil {
		return "", err
	}
	sealed, err := s.vault.Seal(value, "secret:"+id)
	if err != nil {
		return "", ErrStorage
	}
	err = tx.Create(&storage.SecretRecord{ID: id, Purpose: purpose, KeyID: "master-v1", Ciphertext: sealed}).Error
	return id, err
}
func (s *Service) SavePaymentSettings(ctx context.Context, p Principal, in PaymentSettings) error {
	if !p.IsAdmin() {
		return ErrForbidden
	}
	if in.Environment != "sandbox" && in.Environment != "live" {
		return ErrInvalid
	}
	// Editing a saved account never exposes its credentials to the browser.
	// Empty secret fields retain their encrypted values for this exact account.
	var previous storage.PaymentAccount
	findErr := s.db.WithContext(ctx).First(&previous, "provider = ? AND environment = ? AND merchant_ref = ?", in.Provider, in.Environment, in.MerchantRef).Error
	if findErr == nil {
		raw, err := s.readSecret(ctx, previous.CredentialSecretID)
		if err != nil {
			return err
		}
		var saved PaymentSettings
		if json.Unmarshal(raw, &saved) != nil {
			return ErrStorage
		}
		for _, pair := range [][2]*string{{&in.APIKey, &saved.APIKey}, {&in.WebhookSecret, &saved.WebhookSecret}, {&in.ClientID, &saved.ClientID}, {&in.ClientSecret, &saved.ClientSecret}, {&in.PrivateKey, &saved.PrivateKey}, {&in.PublicKey, &saved.PublicKey}, {&in.ClientToken, &saved.ClientToken}} {
			if *pair[0] == "" {
				*pair[0] = *pair[1]
			}
		}
	} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return ErrStorage
	}
	client, err := provider(in)
	if err != nil {
		return problem(400, "PAYMENT_CONFIG_INVALID", "支付配置格式无效，请检查渠道、环境与凭证")
	}
	verifier, ok := client.(interface{ VerifyAccount(context.Context) error })
	if !ok {
		return ErrStorage
	}
	if verifier.VerifyAccount(ctx) != nil {
		return problem(400, "PAYMENT_CONNECTION_FAILED", "支付凭证或收款商户检查未通过")
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		secret, err := s.createSecret(tx, "payment_credentials", marshal(in))
		if err != nil {
			return err
		}
		var account storage.PaymentAccount
		err = tx.First(&account, "provider = ? AND environment = ? AND merchant_ref = ?", in.Provider, in.Environment, in.MerchantRef).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if account.ID == "" {
			id, err := newID()
			if err != nil {
				return err
			}
			checked := now()
			account = storage.PaymentAccount{ID: id, Provider: in.Provider, Environment: in.Environment, MerchantRef: in.MerchantRef, CredentialSecretID: secret, Enabled: true, LastCheckedAt: &checked}
			if err := tx.Create(&account).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Model(&account).Updates(map[string]any{"credential_secret_id": secret, "enabled": true, "last_checked_at": now(), "last_check_error": nil}).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&storage.PaymentAccount{}).Where("provider = ? AND environment = ? AND id <> ?", in.Provider, in.Environment, account.ID).Update("enabled", false).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "payment.configure", "payment_account", account.ID, "")
	}))
}

type LicenseSettings struct {
	WebhookSecret string `json:"webhook_secret"`
	APIKey        string `json:"api_key"`
}

const (
	licentivoConnectionID = "00000000-0000-4000-8000-000000000001"
	LicentivoServiceURL   = "https://www.licentivo.com"
)

type LicentivoSetupProvider interface {
	Products(context.Context) (json.RawMessage, error)
	Policies(context.Context) (json.RawMessage, error)
	Capabilities(context.Context) (ports.LicensingCapabilities, error)
}

func (s *Service) licentivoSetupProvider(apiKey string) (LicentivoSetupProvider, error) {
	if s.config.LicensingSetupFactory != nil {
		return s.config.LicensingSetupFactory(apiKey)
	}
	return licentivo.NewClient(licentivo.ClientConfig{BaseURL: LicentivoServiceURL, APIKey: apiKey})
}

func (s *Service) licensingProvider(ctx context.Context, connection storage.LicentivoConnection) (ports.LicensingProvider, error) {
	if s.config.LicensingFactory != nil {
		return s.config.LicensingFactory(ctx, connection)
	}
	key, err := s.readSecret(ctx, connection.CredentialSecretID)
	if err != nil {
		return nil, err
	}
	return licentivo.NewClient(licentivo.ClientConfig{BaseURL: LicentivoServiceURL, APIKey: string(key)})
}
func (s *Service) SaveLicenseSettings(ctx context.Context, p Principal, in LicenseSettings) (map[string]any, error) {
	if !p.IsAdmin() {
		return nil, ErrForbidden
	}
	if in.WebhookSecret != "" && (len(in.WebhookSecret) < 32 || len(in.WebhookSecret) > 256) {
		return nil, ErrInvalid
	}
	var saved storage.LicentivoConnection
	savedErr := s.db.WithContext(ctx).Order("created_at,id").First(&saved).Error
	if savedErr != nil && !errors.Is(savedErr, gorm.ErrRecordNotFound) {
		return nil, ErrStorage
	}
	apiKey := in.APIKey
	if apiKey == "" {
		if saved.ID == "" {
			return nil, problem(400, "LICENSE_API_KEY_REQUIRED", "首次连接需要填写 Licentivo API Key")
		}
		plain, err := s.readSecret(ctx, saved.CredentialSecretID)
		if err != nil {
			return nil, err
		}
		apiKey = string(plain)
	}
	client, err := s.licentivoSetupProvider(apiKey)
	if err != nil {
		return nil, problem(400, "LICENSE_CONFIG_INVALID", "Licentivo API Key 格式无效")
	}
	products, err := client.Products(ctx)
	if err != nil {
		return nil, problem(400, "LICENSE_CONNECTION_FAILED", "无法从 Licentivo 读取产品，请检查 API Key 是否有效")
	}
	policies, err := client.Policies(ctx)
	if err != nil {
		return nil, problem(400, "LICENSE_CONNECTION_FAILED", "授权服务连接或凭证检查未通过")
	}
	caps, capsErr := client.Capabilities(ctx)
	catalog := marshal(map[string]json.RawMessage{"products": products, "policies": policies})
	id := licentivoConnectionID
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		secret := saved.CredentialSecretID
		if in.APIKey != "" {
			var err error
			secret, err = s.createSecret(tx, "licentivo_api_key", []byte(in.APIKey))
			if err != nil {
				return err
			}
		}
		var connection storage.LicentivoConnection
		findErr := tx.Order("created_at,id").First(&connection).Error
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		if connection.ID != "" {
			id = connection.ID
		} else {
			connection.ID = id
		}
		connection.BaseURL = LicentivoServiceURL
		connection.CredentialSecretID = secret
		connection.Capabilities = marshal(caps)
		connection.Catalog = catalog
		checked := now()
		connection.LastCheckedAt = &checked
		connection.LastCheckError = nil
		if in.WebhookSecret != "" {
			webhook, err := s.createSecret(tx, "licentivo_webhook", []byte(in.WebhookSecret))
			if err != nil {
				return err
			}
			connection.WebhookSecretID = &webhook
		}
		connection.ContractVersion = nil
		if capsErr == nil && caps.ValidateSales() == nil {
			version := "licentivo-management-v1"
			if caps.AbsoluteValidity {
				version = "sales-entitlements-v1"
			}
			connection.ContractVersion = &version
		}
		if findErr == nil {
			if err := tx.Save(&connection).Error; err != nil {
				return err
			}
		} else if err := tx.Create(&connection).Error; err != nil {
			return err
		}
		return audit(tx, p.User.ID, "licentivo.configure", "licentivo_connection", id, "")
	})
	if err != nil {
		return nil, databaseError(err)
	}
	return map[string]any{"id": id, "connected": true, "sales_contract_ready": capsErr == nil && caps.ValidateSales() == nil, "capabilities": caps}, nil
}
func (s *Service) Connections(ctx context.Context, p Principal) (map[string]any, error) {
	if !p.IsAdmin() {
		return nil, ErrForbidden
	}
	var accounts []storage.PaymentAccount
	var connection storage.LicentivoConnection
	connectionErr := s.db.WithContext(ctx).Order("created_at,id").First(&connection).Error
	if s.db.WithContext(ctx).Order("environment,provider").Find(&accounts).Error != nil || connectionErr != nil && !errors.Is(connectionErr, gorm.ErrRecordNotFound) {
		return nil, ErrStorage
	}
	pay := make([]map[string]any, 0, len(accounts))
	licenses := make([]map[string]any, 0, 1)
	for _, v := range accounts {
		var settings PaymentSettings
		if raw, err := s.readSecret(ctx, v.CredentialSecretID); err == nil {
			_ = json.Unmarshal(raw, &settings)
		}
		pay = append(pay, map[string]any{
			"id": v.ID, "provider": v.Provider, "environment": v.Environment,
			"merchant_ref": v.MerchantRef, "enabled": v.Enabled,
			"webhook_url":     s.PublicURL() + "/api/v1/webhooks/payments/" + v.ID,
			"last_checked_at": v.LastCheckedAt, "last_check_error": v.LastCheckError,
			"api_version": settings.APIVersion, "product_id": settings.ProductID, "webhook_id": settings.WebhookID,
			"checkout_page_url": s.PublicURL() + "/pay/paddle/" + v.ID,
		})
	}
	if connection.ID != "" {
		contract := connection.ContractVersion
		capabilities := json.RawMessage(connection.Capabilities)
		// Connections saved by older Launch builds may have been marked as
		// unsupported because they probed a proposed endpoint rather than the
		// management API Licentivo actually exposes. Present the current adapter
		// capability immediately; saving again persists the upgraded marker.
		if provider, err := s.licensingProvider(ctx, connection); err == nil {
			if current, err := provider.Capabilities(ctx); err == nil && current.ValidateSales() == nil {
				capabilities = marshal(current)
				if contract == nil {
					version := "licentivo-management-v1"
					contract = &version
				}
			}
		}
		licenses = append(licenses, map[string]any{"id": connection.ID, "base_url": LicentivoServiceURL, "contract_version": contract, "capabilities": capabilities, "catalog": json.RawMessage(connection.Catalog), "last_checked_at": connection.LastCheckedAt})
	}
	var rows []storage.PlanLicenseMapping
	if s.db.WithContext(ctx).Order("plan_version_id").Find(&rows).Error != nil {
		return nil, ErrStorage
	}
	mappings := make([]map[string]any, 0, len(rows))
	for _, v := range rows {
		mappings = append(mappings, map[string]any{"version_id": v.PlanVersionID, "product_id": v.RemoteProductID, "policy_id": v.RemotePolicyID, "policy_version": v.RemotePolicyVersion})
	}
	return map[string]any{"payments": pay, "licensing": licenses, "mappings": mappings, "licentivo_url": LicentivoServiceURL}, nil
}

type MappingInput struct {
	VersionID string `json:"version_id"`
	PolicyID  string `json:"policy_id"`
}

type licentivoCatalogPolicy struct {
	ID               string   `json:"id"`
	ProductID        string   `json:"product_id"`
	Name             string   `json:"name"`
	Mode             string   `json:"mode"`
	DurationDays     *int     `json:"duration_days"`
	MaxDevices       uint64   `json:"max_devices"`
	Features         []string `json:"features"`
	HeartbeatSeconds uint64   `json:"heartbeat_seconds"`
	OfflineMode      string   `json:"offline_mode"`
	OfflineSeconds   uint64   `json:"offline_seconds"`
	Version          uint64   `json:"version"`
}

type licentivoCatalogSnapshot struct {
	Products struct {
		Items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	} `json:"products"`
	Policies struct {
		Items []licentivoCatalogPolicy `json:"items"`
	} `json:"policies"`
}

func policyPlanSnapshot(policy ports.PolicyDefinition) PlanSnapshot {
	features := append([]string(nil), policy.Features...)
	if features == nil {
		features = []string{}
	}
	return PlanSnapshot{
		OfflineMode: policy.OfflineMode, HeartbeatSeconds: policy.HeartbeatSeconds,
		OfflineSeconds: policy.OfflineSeconds, SessionSeconds: policy.SessionSeconds,
		SelfReleaseLimit: policy.SelfReleaseLimit, Features: features,
		MaxDevices: policy.MaxDevices, LicenseModel: policy.LicenseModel,
	}
}

func (s *Service) SaveMapping(ctx context.Context, p Principal, in MappingInput) error {
	if !p.CanManage() {
		return ErrForbidden
	}
	if in.VersionID == "" || in.PolicyID == "" {
		return ErrInvalid
	}
	var connection storage.LicentivoConnection
	if s.db.WithContext(ctx).Order("created_at,id").First(&connection).Error != nil {
		return problem(409, "LICENSE_CONNECTION_REQUIRED", "请先连接 Licentivo")
	}
	var catalog licentivoCatalogSnapshot
	if json.Unmarshal(connection.Catalog, &catalog) != nil {
		return problem(409, "LICENSE_CATALOG_REFRESH_REQUIRED", "Licentivo 策略目录需要刷新，请重新验证 API Key")
	}
	productIDs := make(map[string]string, len(catalog.Products.Items))
	for _, product := range catalog.Products.Items {
		productIDs[product.ID] = product.Name
	}
	var selected licentivoCatalogPolicy
	for _, policy := range catalog.Policies.Items {
		if policy.ID == in.PolicyID {
			selected = policy
			break
		}
	}
	if selected.ProductID == "" || selected.Version < 1 || productIDs[selected.ProductID] == "" {
		return problem(409, "LICENSE_POLICY_NOT_AVAILABLE", "该授权策略已不可用，请刷新 Licentivo 策略列表")
	}
	client, err := s.licensingProvider(ctx, connection)
	if err != nil {
		return ErrStorage
	}
	caps, err := client.Capabilities(ctx)
	if err != nil || caps.ValidateSales() != nil {
		return problem(409, "LICENSE_CONTRACT_UNSUPPORTED", "Licentivo 管理接口不可用，请确认 API Key 具有读写权限")
	}
	policyProvider, ok := client.(ports.PolicyCatalogProvider)
	if !ok {
		return problem(409, "LICENSE_POLICY_DETAILS_UNAVAILABLE", "无法读取 Licentivo 策略详情，请刷新连接后重试")
	}
	definition, err := policyProvider.PolicyDefinition(ctx, in.PolicyID)
	if err != nil || definition.ID != selected.ID || definition.ProductID != selected.ProductID || definition.Version != selected.Version {
		return problem(409, "LICENSE_POLICY_CHANGED", "Licentivo 策略已经更新，请刷新策略列表后重新选择")
	}
	snapshot := policyPlanSnapshot(definition)
	if snapshot.MaxDevices < 1 || snapshot.MaxDevices > 10000 || (snapshot.LicenseModel != "node_locked" && snapshot.LicenseModel != "floating") {
		return problem(409, "LICENSE_POLICY_INVALID", "Licentivo 策略配置不完整，请先在 Licentivo 中修正")
	}
	snapshotBytes := marshal(snapshot)
	snapshotHash := sha256.Sum256(snapshotBytes)
	var version storage.PlanVersion
	if s.db.WithContext(ctx).First(&version, "id = ?", in.VersionID).Error != nil {
		return ErrNotFound
	}
	version.EntitlementSnapshot = snapshotBytes
	version.SnapshotHash = snapshotHash[:]
	mapping := storage.PlanLicenseMapping{RemoteProductID: selected.ProductID, RemoteProductName: productIDs[selected.ProductID], RemotePolicyID: in.PolicyID, RemotePolicyName: selected.Name, RemotePolicyVersion: selected.Version}
	if err := checkPolicy(ctx, client, version, mapping, ""); err != nil {
		return err
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing int64
		if tx.Model(&storage.PlanPublication{}).Where("version_id = ? AND preview_only = false", in.VersionID).Count(&existing).Error != nil {
			return ErrStorage
		}
		if existing > 0 {
			return problem(409, "PUBLISHED_MAPPING_IMMUTABLE", "已经开始销售的套餐不能直接更换授权，请创建套餐新版本")
		}
		if err := tx.Model(&storage.PlanVersion{}).Where("id = ?", in.VersionID).Updates(map[string]any{"entitlement_snapshot": snapshotBytes, "snapshot_hash": snapshotHash[:]}).Error; err != nil {
			return err
		}
		if err := tx.Where("plan_version_id = ?", in.VersionID).Delete(&storage.PlanLicenseMapping{}).Error; err != nil {
			return err
		}
		return tx.Create(&storage.PlanLicenseMapping{PlanVersionID: in.VersionID, ConnectionID: connection.ID, RemoteProductID: selected.ProductID, RemoteProductName: productIDs[selected.ProductID], RemotePolicyID: in.PolicyID, RemotePolicyName: selected.Name, RemotePolicyVersion: selected.Version}).Error
	}))
}
