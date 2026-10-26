package app

import (
	"context"
	"encoding/json"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

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
		if _, err := lockProviderSettings(tx, "UPDATE"); err != nil {
			return err
		}
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
		if err := tx.Model(&storage.PaymentAccount{}).Where("environment = ? AND id <> ?", in.Environment, account.ID).Update("enabled", false).Error; err != nil {
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
	if err := s.syncLaunchTrust(ctx, apiKey); err != nil {
		return nil, err
	}
	contract := ports.LaunchBridgeVersion
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&storage.LicentivoConnection{}).Where("id = ?", id).Update("contract_version", contract).Error; err != nil {
			return err
		}
		return tx.Model(&storage.SyncTask{}).Where("launch_connection_id IS NULL AND status <> 'succeeded'").Updates(map[string]any{"status": "manual_review", "last_error_code": "LEGACY_PURCHASE_REVIEW_REQUIRED", "lease_token": nil, "lease_until": nil}).Error
	}); err != nil {
		return nil, ErrStorage
	}
	return map[string]any{"id": id, "connected": true, "account_protocol_ready": true, "sales_contract_ready": true, "contract_version": contract}, nil
}
func (s *Service) Connections(ctx context.Context, p Principal) (map[string]any, error) {
	if !p.IsAdmin() {
		return nil, ErrForbidden
	}
	selected, err := s.activeLicensingProvider(s.db.WithContext(ctx))
	if err != nil {
		return nil, ErrStorage
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
		if selected == "licentivo" {
			if provider, err := s.licensingProvider(ctx, connection); err == nil {
				if current, err := provider.Capabilities(ctx); err == nil && current.ValidateSales() == nil {
					capabilities = marshal(current)
					if contract == nil {
						version := "licentivo-management-v1"
						contract = &version
					}
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
	return map[string]any{"payments": pay, "licensing": licenses, "mappings": mappings, "licentivo_url": LicentivoServiceURL, "licensing_provider": selected}, nil
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

func (s *Service) SaveMapping(ctx context.Context, p Principal, in MappingInput) error {
	if !p.CanManage() {
		return ErrForbidden
	}
	if !integrationUUIDPattern.MatchString(in.VersionID) || !integrationUUIDPattern.MatchString(in.PolicyID) {
		return ErrInvalid
	}
	var connection storage.LicentivoConnection
	if s.db.WithContext(ctx).First(&connection, "contract_version = ?", ports.LaunchBridgeVersion).Error != nil {
		return bridgePending()
	}
	var catalog licentivoCatalogSnapshot
	if json.Unmarshal(connection.Catalog, &catalog) != nil {
		return problem(409, "LICENSE_CATALOG_REFRESH_REQUIRED", "请刷新 Licentivo 产品和策略目录")
	}
	names := map[string]string{}
	for _, product := range catalog.Products.Items {
		names[product.ID] = product.Name
	}
	var selected licentivoCatalogPolicy
	for _, policy := range catalog.Policies.Items {
		if policy.ID == in.PolicyID {
			selected = policy
			break
		}
	}
	if selected.ProductID == "" || selected.Version == 0 || names[selected.ProductID] == "" {
		return problem(409, "LICENSE_POLICY_NOT_AVAILABLE", "请刷新 Licentivo 策略目录后重新选择")
	}
	var target storage.PlanVersion
	if err := s.db.WithContext(ctx).Select("id", "product_id").First(&target, "id = ?", in.VersionID).Error; err != nil {
		return databaseError(err)
	}
	return databaseError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockProviderSettings(tx, "SHARE"); err != nil {
			return err
		}
		if err := s.requireLicensingProvider(tx, "licentivo"); err != nil {
			return err
		}
		// Lock the common parent before any consistent reads. Locking only the
		// two different version rows allowed both transactions to see no conflict.
		var product storage.Product
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&product, "id = ?", target.ProductID).Error; err != nil {
			return err
		}
		var version storage.PlanVersion
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&version, "id = ? AND product_id = ?", in.VersionID, product.ID).Error; err != nil {
			return err
		}
		mapping := storage.PlanLicenseMapping{PlanVersionID: version.ID, ConnectionID: connection.ID, RemoteProductID: selected.ProductID, RemoteProductName: names[selected.ProductID], RemotePolicyID: selected.ID, RemotePolicyName: selected.Name, RemotePolicyVersion: selected.Version}
		var published int64
		if err := tx.Model(&storage.PlanPublication{}).Where("version_id = ? AND preview_only = false", version.ID).Count(&published).Error; err != nil {
			return err
		}
		if published > 0 {
			var saved storage.PlanLicenseMapping
			if tx.First(&saved, "plan_version_id = ?", version.ID).Error != nil || saved.ConnectionID != mapping.ConnectionID || saved.RemoteProductID != mapping.RemoteProductID || saved.RemotePolicyID != mapping.RemotePolicyID || saved.RemotePolicyVersion != mapping.RemotePolicyVersion {
				return problem(409, "PUBLISHED_MAPPING_IMMUTABLE", "已经开始销售的套餐不能直接更换授权，请创建套餐新版本")
			}
		}
		remoteProduct, err := productRemoteID(tx, product, version.ID)
		if err != nil {
			return err
		}
		if remoteProduct != "" && remoteProduct != mapping.RemoteProductID {
			return productMappingConflict()
		}
		for _, environment := range []string{"sandbox", "live"} {
			if err := s.registerLaunchPlan(ctx, environment, version, mapping, false); err != nil {
				return err
			}
		}
		policy, err := s.mappingPolicy(ctx, mapping)
		if err != nil {
			return err
		}
		display := marshal(policyDisplaySnapshot(policy))
		if err := tx.Model(&version).Updates(map[string]any{"entitlement_snapshot": display, "snapshot_hash": securityDigest(display)}).Error; err != nil {
			return err
		}
		if product.LicentivoProductID == nil {
			if err := tx.Model(&product).Update("licentivo_product_id", mapping.RemoteProductID).Error; err != nil {
				return err
			}
		}
		return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&mapping).Error
	}))
}
