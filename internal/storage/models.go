// Package storage owns the local merchant database. Models are the schema source.
package storage

import (
	"encoding/json"
	"time"
)

type InstanceSettings struct {
	RecoveryRequired        bool            `gorm:"type:boolean;not null;default:false"`
	ID                      uint64          `gorm:"column:id;type:tinyint unsigned;primaryKey;autoIncrement:false;check:ck_single_instance,id = 1"`
	InstanceRef             string          `gorm:"column:instance_ref;type:char(32) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_instance_settings_instance_ref"`
	Brand                   json.RawMessage `gorm:"column:brand;type:json;not null"`
	DefaultProductID        *string         `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin"`
	PublicOrigin            *string         `gorm:"column:public_origin;type:varchar(512) CHARACTER SET ascii COLLATE ascii_bin"`
	TLSSecretID             *string         `gorm:"column:tls_secret_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin"`
	TLSMetadata             json.RawMessage `gorm:"column:tls_metadata;type:json"`
	TLSUpdatedAt            *time.Time      `gorm:"column:tls_updated_at;type:datetime(6)"`
	InstallationCompletedAt *time.Time      `gorm:"column:installation_completed_at;type:datetime(6)"`
	CreatedAt               time.Time       `gorm:"column:created_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
	UpdatedAt               time.Time       `gorm:"column:updated_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
}

func (InstanceSettings) TableName() string { return "instance_settings" }

type SecretRecord struct {
	ID         string    `gorm:"column:id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false"`
	Purpose    string    `gorm:"column:purpose;type:varchar(96) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	KeyID      string    `gorm:"column:key_id;type:varchar(64) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	Ciphertext []byte    `gorm:"column:ciphertext;type:blob;not null"`
	CreatedAt  time.Time `gorm:"column:created_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
	UpdatedAt  time.Time `gorm:"column:updated_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
}

func (SecretRecord) TableName() string { return "secret_records" }

type User struct {
	ID              string     `gorm:"column:id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false"`
	Email           string     `gorm:"column:email;type:varchar(254);not null"`
	EmailKey        string     `gorm:"column:email_key;type:varchar(254);not null;uniqueIndex:uq_users_email_key"`
	PasswordHash    string     `gorm:"column:password_hash;type:varchar(512) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	EmailVerifiedAt *time.Time `gorm:"column:email_verified_at;type:datetime(6)"`
	Status          string     `gorm:"column:status;type:enum('active','disabled','closed');not null;default:'active'"`
	DisabledReason  *string    `gorm:"column:disabled_reason;type:varchar(512)"`
	CreatedAt       time.Time  `gorm:"column:created_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
	UpdatedAt       time.Time  `gorm:"column:updated_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
}

func (User) TableName() string { return "users" }

type StaffAccount struct {
	UserID      string          `gorm:"column:user_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false"`
	Role        string          `gorm:"column:role;type:enum('admin','operations','support');not null"`
	Permissions json.RawMessage `gorm:"column:permissions;type:json;not null"`
	CreatedAt   time.Time       `gorm:"column:created_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
	FkStaffUser *User           `gorm:"foreignKey:UserID;references:ID;constraint:fk_staff_user,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (StaffAccount) TableName() string { return "staff_accounts" }

type Session struct {
	ID            string     `gorm:"column:id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false"`
	UserID        string     `gorm:"column:user_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;index:ix_sessions_user,priority:1"`
	TokenHash     []byte     `gorm:"column:token_hash;type:binary(32);not null;uniqueIndex:uq_sessions_token_hash"`
	CSRFHash      []byte     `gorm:"type:binary(32)"`
	Kind          string     `gorm:"column:kind;type:enum('website','desktop');not null"`
	ExpiresAt     time.Time  `gorm:"column:expires_at;type:datetime(6);not null"`
	RevokedAt     *time.Time `gorm:"column:revoked_at;type:datetime(6);index:ix_sessions_user,priority:2"`
	CreatedAt     time.Time  `gorm:"column:created_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
	FkSessionUser *User      `gorm:"foreignKey:UserID;references:ID;constraint:fk_session_user,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (Session) TableName() string { return "sessions" }

type Product struct {
	ID        string          `gorm:"column:id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false"`
	Slug      string          `gorm:"column:slug;type:varchar(80) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_products_slug"`
	Name      string          `gorm:"column:name;type:varchar(160);not null"`
	Content   json.RawMessage `gorm:"column:content;type:json;not null"`
	Status    string          `gorm:"column:status;type:enum('draft','published','archived');not null;default:'draft'"`
	CreatedAt time.Time       `gorm:"column:created_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
	UpdatedAt time.Time       `gorm:"column:updated_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
}

func (Product) TableName() string { return "products" }

// ProductDownload stores one independently managed build for each supported
// operating system. Uploaded installer bytes live outside MySQL; this row keeps
// the immutable storage key and the public metadata needed to serve them.
type ProductDownload struct {
	ID          string    `gorm:"column:id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false"`
	ProductID   string    `gorm:"column:product_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_product_download_platform,priority:1"`
	Platform    string    `gorm:"column:platform;type:varchar(16) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_product_download_platform,priority:2"`
	Source      string    `gorm:"column:source;type:enum('upload','external');not null"`
	Version     string    `gorm:"column:version;type:varchar(80);not null"`
	ExternalURL *string   `gorm:"column:external_url;type:varchar(2048)"`
	StorageKey  *string   `gorm:"column:storage_key;type:varchar(96) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_product_download_storage"`
	FileName    string    `gorm:"column:file_name;type:varchar(255);not null"`
	ContentType string    `gorm:"column:content_type;type:varchar(160);not null"`
	SizeBytes   uint64    `gorm:"column:size_bytes;type:bigint unsigned;not null;default:0"`
	SHA256      []byte    `gorm:"column:sha256;type:binary(32)"`
	CreatedAt   time.Time `gorm:"column:created_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
	UpdatedAt   time.Time `gorm:"column:updated_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
	FkProduct   *Product  `gorm:"foreignKey:ProductID;references:ID;constraint:fk_product_download_product,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (ProductDownload) TableName() string { return "product_downloads" }

// ProductDocumentImage stores images embedded in a product's public help
// documents. Image bytes stay in the configured upload directory while this
// row provides an unguessable public identifier and verified metadata.
type ProductDocumentImage struct {
	ID          string    `gorm:"column:id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false"`
	ProductID   string    `gorm:"column:product_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;index:ix_product_document_images_product"`
	StorageKey  string    `gorm:"column:storage_key;type:varchar(96) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_product_document_image_storage"`
	FileName    string    `gorm:"column:file_name;type:varchar(255);not null"`
	ContentType string    `gorm:"column:content_type;type:varchar(64) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	SizeBytes   uint64    `gorm:"column:size_bytes;type:bigint unsigned;not null"`
	SHA256      []byte    `gorm:"column:sha256;type:binary(32);not null"`
	CreatedAt   time.Time `gorm:"column:created_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
	UpdatedAt   time.Time `gorm:"column:updated_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
	FkProduct   *Product  `gorm:"foreignKey:ProductID;references:ID;constraint:fk_product_document_image_product,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (ProductDocumentImage) TableName() string { return "product_document_images" }

// TranslationSettings is the single merchant-owned DeepL connection. The API
// key is stored through SecretRecord so it is never exposed by read APIs.
type TranslationSettings struct {
	ID                  uint64        `gorm:"column:id;type:tinyint unsigned;primaryKey;autoIncrement:false;check:ck_single_translation_settings,id = 1"`
	Provider            string        `gorm:"column:provider;type:enum('deepl');not null;default:'deepl'"`
	Edition             string        `gorm:"column:edition;type:enum('free','pro');not null;default:'free'"`
	SourceLocale        string        `gorm:"column:source_locale;type:varchar(16) CHARACTER SET ascii COLLATE ascii_bin;not null;default:'zh-CN'"`
	CredentialSecretID  string        `gorm:"column:credential_secret_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	CharacterCount      uint64        `gorm:"column:character_count;type:bigint unsigned;not null;default:0"`
	CharacterLimit      uint64        `gorm:"column:character_limit;type:bigint unsigned;not null;default:0"`
	LastCheckedAt       *time.Time    `gorm:"column:last_checked_at;type:datetime(6)"`
	LastCheckError      *string       `gorm:"column:last_check_error;type:varchar(160)"`
	CreatedAt           time.Time     `gorm:"column:created_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
	UpdatedAt           time.Time     `gorm:"column:updated_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
	FkTranslationSecret *SecretRecord `gorm:"foreignKey:CredentialSecretID;references:ID;constraint:fk_translation_secret,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (TranslationSettings) TableName() string { return "translation_settings" }

// ContentTranslation stores a translated public-content snapshot. Generic
// resource identifiers allow the same table to cover brand, product, and plan
// content while SourceHash prevents stale text from being served.
type ContentTranslation struct {
	ResourceType string          `gorm:"column:resource_type;type:enum('site_ui','brand','product','plan_version');primaryKey;autoIncrement:false"`
	ResourceID   string          `gorm:"column:resource_id;type:varchar(64) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false"`
	Locale       string          `gorm:"column:locale;type:varchar(16) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false"`
	SourceHash   []byte          `gorm:"column:source_hash;type:binary(32);not null"`
	Payload      json.RawMessage `gorm:"column:payload;type:json;not null"`
	CreatedAt    time.Time       `gorm:"column:created_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
	UpdatedAt    time.Time       `gorm:"column:updated_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
}

func (ContentTranslation) TableName() string { return "content_translations" }

// TranslationTask makes translation automatic without making an administrator
// wait for nine external API calls while saving source content.
type TranslationTask struct {
	ID            string          `gorm:"column:id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false"`
	ResourceType  string          `gorm:"column:resource_type;type:enum('site_ui','brand','product','plan_version');not null;uniqueIndex:uq_translation_task_resource,priority:1"`
	ResourceID    string          `gorm:"column:resource_id;type:varchar(64) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_translation_task_resource,priority:2"`
	SourceLocale  string          `gorm:"column:source_locale;type:varchar(16) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	SourceHash    []byte          `gorm:"column:source_hash;type:binary(32);not null"`
	SourceJSON    json.RawMessage `gorm:"column:source_json;type:json;not null"`
	Status        string          `gorm:"column:status;type:enum('pending','running','retrying','succeeded','manual_review');not null;default:'pending';index:ix_translation_task_claim,priority:1"`
	Attempts      uint64          `gorm:"column:attempts;type:int unsigned;not null;default:0"`
	AvailableAt   time.Time       `gorm:"column:available_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6);index:ix_translation_task_claim,priority:2"`
	LeaseToken    *string         `gorm:"column:lease_token;type:char(36) CHARACTER SET ascii COLLATE ascii_bin"`
	LeaseUntil    *time.Time      `gorm:"column:lease_until;type:datetime(6);index:ix_translation_task_claim,priority:3"`
	LastErrorCode *string         `gorm:"column:last_error_code;type:varchar(80) CHARACTER SET ascii COLLATE ascii_bin"`
	CreatedAt     time.Time       `gorm:"column:created_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
	UpdatedAt     time.Time       `gorm:"column:updated_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
}

func (TranslationTask) TableName() string { return "translation_tasks" }

type LicentivoConnection struct {
	ID                 string          `gorm:"column:id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false"`
	BaseURL            string          `gorm:"column:base_url;type:varchar(512);not null"`
	CredentialSecretID string          `gorm:"column:credential_secret_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	WebhookSecretID    *string         `gorm:"column:webhook_secret_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin"`
	ContractVersion    *string         `gorm:"column:contract_version;type:varchar(64) CHARACTER SET ascii COLLATE ascii_bin"`
	Capabilities       json.RawMessage `gorm:"column:capabilities;type:json;not null"`
	Catalog            json.RawMessage `gorm:"column:catalog;type:json;not null"`
	LastCheckedAt      *time.Time      `gorm:"column:last_checked_at;type:datetime(6)"`
	LastCheckError     *string         `gorm:"column:last_check_error;type:varchar(160)"`
	CreatedAt          time.Time       `gorm:"column:created_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
	FkConnectionSecret *SecretRecord   `gorm:"foreignKey:CredentialSecretID;references:ID;constraint:fk_connection_secret,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
	FkWebhookSecret    *SecretRecord   `gorm:"foreignKey:WebhookSecretID;references:ID;constraint:fk_webhook_secret,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (LicentivoConnection) TableName() string { return "licentivo_connections" }

type Plan struct {
	ID            string    `gorm:"column:id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false;uniqueIndex:uq_plan_product,priority:1"`
	ProductID     string    `gorm:"column:product_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_plan_product,priority:2"`
	Name          string    `gorm:"column:name;type:varchar(160);not null"`
	Status        string    `gorm:"column:status;type:enum('draft','published','archived');not null;default:'draft'"`
	AllowPurchase bool      `gorm:"column:allow_purchase;type:boolean;not null;default:FALSE"`
	AllowRenewal  bool      `gorm:"column:allow_renewal;type:boolean;not null;default:FALSE"`
	DisplayOrder  int64     `gorm:"column:display_order;type:int;not null;default:0"`
	CreatedAt     time.Time `gorm:"column:created_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
	FkPlanProduct *Product  `gorm:"foreignKey:ProductID;references:ID;constraint:fk_plan_product,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (Plan) TableName() string { return "plans" }

type PlanVersion struct {
	PermanentUpgradeMinor *int64          `gorm:"type:bigint"`
	ID                    string          `gorm:"column:id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false;uniqueIndex:uq_plan_version_product,priority:1;check:ck_plan_price,amount_minor >= 0 AND version > 0"`
	PlanID                string          `gorm:"column:plan_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_plan_version,priority:1;check:ck_plan_term,(purchase_type='perpetual' AND term_unit IS NULL AND term_count IS NULL) OR (purchase_type='fixed_term' AND term_unit IS NOT NULL AND term_count IS NOT NULL AND term_count > 0)"`
	ProductID             string          `gorm:"column:product_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_plan_version_product,priority:2"`
	Version               uint64          `gorm:"column:version;type:bigint unsigned;not null;uniqueIndex:uq_plan_version,priority:2"`
	Currency              string          `gorm:"column:currency;type:char(3) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	AmountMinor           int64           `gorm:"column:amount_minor;type:bigint;not null"`
	PurchaseType          string          `gorm:"column:purchase_type;type:enum('perpetual','fixed_term');not null"`
	TermUnit              *string         `gorm:"column:term_unit;type:enum('day','month','year')"`
	TermCount             *uint64         `gorm:"column:term_count;type:int unsigned"`
	EntitlementSnapshot   json.RawMessage `gorm:"column:entitlement_snapshot;type:json;not null"`
	Marketing             json.RawMessage `gorm:"column:marketing;type:json"`
	PurchaseOptions       json.RawMessage `gorm:"column:purchase_options;type:json"`
	SnapshotHash          []byte          `gorm:"column:snapshot_hash;type:binary(32);not null"`
	PublishedAt           *time.Time      `gorm:"column:published_at;type:datetime(6)"`
	CreatedAt             time.Time       `gorm:"column:created_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
	FkVersionPlan         *Plan           `gorm:"foreignKey:PlanID,ProductID;references:ID,ProductID;constraint:fk_version_plan,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (PlanVersion) TableName() string { return "plan_versions" }

type PlanLicenseMapping struct {
	PlanVersionID       string               `gorm:"column:plan_version_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;primaryKey"`
	ConnectionID        string               `gorm:"column:connection_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	RemoteProductID     string               `gorm:"column:remote_product_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	RemoteProductName   string               `gorm:"column:remote_product_name;type:varchar(160);not null;default:''"`
	RemotePolicyID      string               `gorm:"column:remote_policy_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	RemotePolicyName    string               `gorm:"column:remote_policy_name;type:varchar(160);not null;default:''"`
	RemotePolicyVersion uint64               `gorm:"column:remote_policy_version;type:bigint unsigned;not null"`
	FkMappingPlan       *PlanVersion         `gorm:"foreignKey:PlanVersionID;references:ID;constraint:fk_mapping_plan,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
	FkMappingConnection *LicentivoConnection `gorm:"foreignKey:ConnectionID;references:ID;constraint:fk_mapping_connection,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (PlanLicenseMapping) TableName() string { return "plan_license_mappings" }

type PaymentAccount struct {
	ID                     string        `gorm:"column:id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false;uniqueIndex:uq_payment_environment,priority:1"`
	Provider               string        `gorm:"column:provider;type:enum('stripe','paypal');not null;uniqueIndex:uq_payment_merchant,priority:1"`
	Environment            string        `gorm:"column:environment;type:enum('sandbox','live');not null;uniqueIndex:uq_payment_merchant,priority:2;uniqueIndex:uq_payment_environment,priority:2"`
	MerchantRef            string        `gorm:"column:merchant_ref;type:varchar(128) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_payment_merchant,priority:3"`
	CredentialSecretID     string        `gorm:"column:credential_secret_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	WebhookSecretID        *string       `gorm:"column:webhook_secret_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin"`
	Enabled                bool          `gorm:"column:enabled;type:boolean;not null;default:FALSE"`
	LastCheckedAt          *time.Time    `gorm:"column:last_checked_at;type:datetime(6)"`
	LastCheckError         *string       `gorm:"column:last_check_error;type:varchar(160)"`
	FkPaymentSecret        *SecretRecord `gorm:"foreignKey:CredentialSecretID;references:ID;constraint:fk_payment_secret,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
	FkPaymentWebhookSecret *SecretRecord `gorm:"foreignKey:WebhookSecretID;references:ID;constraint:fk_payment_webhook_secret,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (PaymentAccount) TableName() string { return "payment_accounts" }

type Order struct {
	FulfillmentReview          bool            `gorm:"type:boolean;not null;default:false"`
	QuoteSnapshot              json.RawMessage `gorm:"type:json"`
	PurchasedPeriods           uint64          `gorm:"column:purchased_periods;type:int unsigned;not null;default:1"`
	ID                         string          `gorm:"column:id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false;uniqueIndex:uq_order_environment,priority:1;check:ck_order_amount,amount_minor >= 0"`
	UserID                     string          `gorm:"column:user_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_order_request,priority:1;index:ix_order_user,priority:1"`
	ProductID                  string          `gorm:"column:product_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	PlanVersionID              string          `gorm:"column:plan_version_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	Environment                string          `gorm:"column:environment;type:enum('sandbox','live');not null;uniqueIndex:uq_order_request,priority:2;uniqueIndex:uq_order_environment,priority:2"`
	Action                     string          `gorm:"column:action;type:enum('purchase','renew','upgrade');not null"`
	Status                     string          `gorm:"column:status;type:enum('pending','closed','paid','refunding','refunded');not null;default:'pending'"`
	IdempotencyKey             string          `gorm:"column:idempotency_key;type:varchar(80) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_order_request,priority:3"`
	RequestHash                []byte          `gorm:"column:request_hash;type:binary(32);not null"`
	Currency                   string          `gorm:"column:currency;type:char(3) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	AmountMinor                int64           `gorm:"column:amount_minor;type:bigint;not null"`
	PriceSnapshot              json.RawMessage `gorm:"column:price_snapshot;type:json;not null"`
	EntitlementSnapshot        json.RawMessage `gorm:"column:entitlement_snapshot;type:json;not null"`
	ExpectedEntitlementVersion uint64          `gorm:"column:expected_entitlement_version;type:bigint unsigned;not null"`
	QuoteExpiresAt             *time.Time      `gorm:"column:quote_expires_at;type:datetime(6)"`
	PaidAt                     *time.Time      `gorm:"column:paid_at;type:datetime(6)"`
	ExpiresAt                  time.Time       `gorm:"column:expires_at;type:datetime(6);not null"`
	CreatedAt                  time.Time       `gorm:"column:created_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6);index:ix_order_user,priority:2"`
	FkOrderUser                *User           `gorm:"foreignKey:UserID;references:ID;constraint:fk_order_user,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
	FkOrderPlan                *PlanVersion    `gorm:"foreignKey:PlanVersionID,ProductID;references:ID,ProductID;constraint:fk_order_plan,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (Order) TableName() string { return "orders" }

type Payment struct {
	CheckoutURL           *string         `gorm:"type:text"`
	CheckoutRequest       json.RawMessage `gorm:"type:json"`
	CaptureOperationID    *string         `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin"`
	NextCheckAt           *time.Time      `gorm:"type:datetime(6);index"`
	ID                    string          `gorm:"column:id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false;uniqueIndex:uq_payment_account,priority:1;check:ck_payment_amount,amount_minor >= 0"`
	OrderID               string          `gorm:"column:order_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	AccountID             string          `gorm:"column:account_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_provider_transaction,priority:1;uniqueIndex:uq_provider_checkout,priority:1;uniqueIndex:uq_payment_account,priority:2"`
	Environment           string          `gorm:"column:environment;type:enum('sandbox','live');not null"`
	OperationID           string          `gorm:"column:operation_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_payments_operation_id"`
	ExternalCheckoutID    *string         `gorm:"column:external_checkout_id;type:varchar(128) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_provider_checkout,priority:2"`
	ExternalTransactionID *string         `gorm:"column:external_transaction_id;type:varchar(128) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_provider_transaction,priority:2"`
	Status                string          `gorm:"column:status;type:enum('creating','pending','succeeded','failed','unknown');not null"`
	Currency              string          `gorm:"column:currency;type:char(3) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	AmountMinor           int64           `gorm:"column:amount_minor;type:bigint;not null"`
	PaidAt                *time.Time      `gorm:"column:paid_at;type:datetime(6)"`
	CreatedAt             time.Time       `gorm:"column:created_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
	FkPaymentOrder        *Order          `gorm:"foreignKey:OrderID,Environment;references:ID,Environment;constraint:fk_payment_order,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
	FkPaymentAccount      *PaymentAccount `gorm:"foreignKey:AccountID,Environment;references:ID,Environment;constraint:fk_payment_account,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (Payment) TableName() string { return "payments" }

type Refund struct {
	LeaseToken       *string    `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin"`
	LeaseUntil       *time.Time `gorm:"type:datetime(6)"`
	ApprovedAt       *time.Time `gorm:"type:datetime(6)"`
	ApprovedBy       *string    `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin"`
	CompletedAt      *time.Time `gorm:"type:datetime(6)"`
	NextCheckAt      *time.Time `gorm:"type:datetime(6);index"`
	Attempts         uint64     `gorm:"type:int unsigned;not null;default:0"`
	ReviewRequired   bool       `gorm:"type:boolean;not null;default:false"`
	ID               string     `gorm:"column:id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false;check:ck_refund_amount,amount_minor > 0"`
	PaymentID        string     `gorm:"column:payment_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_full_refund_payment"`
	AccountID        string     `gorm:"column:account_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_provider_refund,priority:1"`
	OperationID      string     `gorm:"column:operation_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_refunds_operation_id"`
	ExternalRefundID *string    `gorm:"column:external_refund_id;type:varchar(128) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_provider_refund,priority:2"`
	AmountMinor      int64      `gorm:"column:amount_minor;type:bigint;not null"`
	Status           string     `gorm:"column:status;type:enum('requested','pending','succeeded','failed','unknown');not null"`
	Reason           string     `gorm:"column:reason;type:varchar(512);not null"`
	CreatedAt        time.Time  `gorm:"column:created_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
	FkRefundPayment  *Payment   `gorm:"foreignKey:PaymentID,AccountID;references:ID,AccountID;constraint:fk_refund_payment,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (Refund) TableName() string { return "refunds" }

type PaymentEvent struct {
	AvailableAt     time.Time       `gorm:"type:datetime(6);not null;default:CURRENT_TIMESTAMP(6);index"`
	Attempts        uint64          `gorm:"type:int unsigned;not null;default:0"`
	LeaseToken      *string         `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin"`
	LeaseUntil      *time.Time      `gorm:"type:datetime(6)"`
	VerifiedPayload json.RawMessage `gorm:"type:json"`
	ID              string          `gorm:"column:id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false"`
	AccountID       string          `gorm:"column:account_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_payment_event,priority:1"`
	ExternalEventID string          `gorm:"column:external_event_id;type:varchar(160) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_payment_event,priority:2"`
	Kind            string          `gorm:"column:kind;type:varchar(96) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	BodyHash        []byte          `gorm:"column:body_hash;type:binary(32);not null"`
	BodyCiphertext  []byte          `gorm:"column:body_ciphertext;type:mediumblob"`
	Status          string          `gorm:"column:status;type:enum('pending','running','processed','review');not null;default:'pending'"`
	ReceivedAt      time.Time       `gorm:"column:received_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
	VerifiedAt      time.Time       `gorm:"column:verified_at;type:datetime(6);not null"`
	FkEventAccount  *PaymentAccount `gorm:"foreignKey:AccountID;references:ID;constraint:fk_event_account,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (PaymentEvent) TableName() string { return "payment_events" }

type Entitlement struct {
	LastReconciledAt      *time.Time      `gorm:"type:datetime(6);index"`
	RemoteObservedVersion uint64          `gorm:"type:bigint unsigned;not null;default:0"`
	CurrentCoverageID     *string         `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin"`
	NextTransitionAt      *time.Time      `gorm:"type:datetime(6);index"`
	CustomerRef           *string         `gorm:"type:char(32) CHARACTER SET ascii COLLATE ascii_bin"`
	ID                    string          `gorm:"column:id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false;uniqueIndex:uq_entitlement_environment,priority:1;check:ck_entitlement_versions,applied_version <= desired_version"`
	UserID                string          `gorm:"column:user_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_entitlement_owner,priority:1;check:ck_entitlement_validity,valid_until IS NULL OR (valid_from IS NOT NULL AND valid_until > valid_from)"`
	ProductID             string          `gorm:"column:product_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_entitlement_owner,priority:2"`
	Environment           string          `gorm:"column:environment;type:enum('sandbox','live');not null;uniqueIndex:uq_entitlement_owner,priority:3;uniqueIndex:uq_entitlement_environment,priority:2"`
	DesiredVersion        uint64          `gorm:"column:desired_version;type:bigint unsigned;not null;default:0"`
	AppliedVersion        uint64          `gorm:"column:applied_version;type:bigint unsigned;not null;default:0"`
	Status                string          `gorm:"column:status;type:enum('pending','active','expired','suspended','revoked');not null;default:'pending'"`
	ManualHold            bool            `gorm:"column:manual_hold;type:boolean;not null;default:FALSE"`
	HoldReason            *string         `gorm:"column:hold_reason;type:varchar(512)"`
	ValidFrom             *time.Time      `gorm:"column:valid_from;type:datetime(6)"`
	ValidUntil            *time.Time      `gorm:"column:valid_until;type:datetime(6)"`
	Snapshot              json.RawMessage `gorm:"column:snapshot;type:json;not null"`
	CreatedAt             time.Time       `gorm:"column:created_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
	UpdatedAt             time.Time       `gorm:"column:updated_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
	FkEntitlementUser     *User           `gorm:"foreignKey:UserID;references:ID;constraint:fk_entitlement_user,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
	FkEntitlementProduct  *Product        `gorm:"foreignKey:ProductID;references:ID;constraint:fk_entitlement_product,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (Entitlement) TableName() string { return "entitlements" }

type EntitlementCoverage struct {
	ID                    string               `gorm:"column:id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false;check:ck_coverage_validity,valid_until IS NULL OR valid_until > valid_from"`
	EntitlementID         string               `gorm:"column:entitlement_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	SourceOrderID         string               `gorm:"column:source_order_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_entitlement_coverages_source_order_id"`
	BaseCoverageID        *string              `gorm:"column:base_coverage_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin"`
	ValidFrom             time.Time            `gorm:"column:valid_from;type:datetime(6);not null"`
	ValidUntil            *time.Time           `gorm:"column:valid_until;type:datetime(6)"`
	ContractAnchor        time.Time            `gorm:"column:contract_anchor;type:datetime(6);not null"`
	TermUnit              *string              `gorm:"column:term_unit;type:enum('day','month','year')"`
	TermCount             *uint64              `gorm:"column:term_count;type:int unsigned"`
	PaidPeriods           *uint64              `gorm:"column:paid_periods;type:int unsigned"`
	Snapshot              json.RawMessage      `gorm:"column:snapshot;type:json;not null"`
	VoidedAt              *time.Time           `gorm:"column:voided_at;type:datetime(6)"`
	CreatedAt             time.Time            `gorm:"column:created_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
	FkCoverageEntitlement *Entitlement         `gorm:"foreignKey:EntitlementID;references:ID;constraint:fk_coverage_entitlement,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
	FkCoverageOrder       *Order               `gorm:"foreignKey:SourceOrderID;references:ID;constraint:fk_coverage_order,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
	FkUpgradeBase         *EntitlementCoverage `gorm:"foreignKey:BaseCoverageID;references:ID;constraint:fk_upgrade_base,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (EntitlementCoverage) TableName() string { return "entitlement_coverages" }

type LicenseBinding struct {
	ID                   string               `gorm:"column:id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false"`
	EntitlementID        string               `gorm:"column:entitlement_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_current_binding,priority:1"`
	ConnectionID         string               `gorm:"column:connection_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_remote_license,priority:1;uniqueIndex:uq_binding_reference,priority:1"`
	CustomerRef          string               `gorm:"column:customer_ref;type:char(32) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_binding_reference,priority:2"`
	RemoteLicenseID      string               `gorm:"column:remote_license_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_remote_license,priority:2"`
	CredentialSecretID   string               `gorm:"column:credential_secret_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	ReplacedAt           *time.Time           `gorm:"column:replaced_at;type:datetime(6)"`
	CurrentSlot          *int64               `gorm:"column:current_slot;type:tinyint GENERATED ALWAYS AS (CASE WHEN replaced_at IS NULL THEN 1 ELSE NULL END) STORED;->;uniqueIndex:uq_current_binding,priority:2;uniqueIndex:uq_binding_reference,priority:3"`
	CreatedAt            time.Time            `gorm:"column:created_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
	FkBindingEntitlement *Entitlement         `gorm:"foreignKey:EntitlementID;references:ID;constraint:fk_binding_entitlement,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
	FkBindingConnection  *LicentivoConnection `gorm:"foreignKey:ConnectionID;references:ID;constraint:fk_binding_connection,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
	FkBindingSecret      *SecretRecord        `gorm:"foreignKey:CredentialSecretID;references:ID;constraint:fk_binding_secret,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (LicenseBinding) TableName() string { return "license_bindings" }

type SyncTask struct {
	ConnectionID      *string         `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin"`
	OperationID       string          `gorm:"column:operation_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false;check:ck_task_version,target_version > 0 AND expected_version = target_version-1"`
	EntitlementID     string          `gorm:"column:entitlement_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_task_version,priority:1"`
	SourceOrderID     *string         `gorm:"column:source_order_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin"`
	ExpectedVersion   uint64          `gorm:"column:expected_version;type:bigint unsigned;not null"`
	TargetVersion     uint64          `gorm:"column:target_version;type:bigint unsigned;not null;uniqueIndex:uq_task_version,priority:2"`
	RequestJSON       json.RawMessage `gorm:"column:request_json;type:json;not null"`
	RequestHash       []byte          `gorm:"column:request_hash;type:binary(32);not null"`
	ResultCiphertext  []byte          `gorm:"column:result_ciphertext;type:mediumblob"`
	Status            string          `gorm:"column:status;type:enum('pending','running','retrying','succeeded','manual_review');not null;default:'pending';index:ix_task_claim,priority:1"`
	Attempts          uint64          `gorm:"column:attempts;type:int unsigned;not null;default:0"`
	AvailableAt       time.Time       `gorm:"column:available_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6);index:ix_task_claim,priority:2"`
	LeaseToken        *string         `gorm:"column:lease_token;type:char(36) CHARACTER SET ascii COLLATE ascii_bin"`
	LeaseUntil        *time.Time      `gorm:"column:lease_until;type:datetime(6);index:ix_task_claim,priority:3"`
	LastErrorCode     *string         `gorm:"column:last_error_code;type:varchar(80) CHARACTER SET ascii COLLATE ascii_bin"`
	CreatedAt         time.Time       `gorm:"column:created_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
	UpdatedAt         time.Time       `gorm:"column:updated_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
	FkTaskEntitlement *Entitlement    `gorm:"foreignKey:EntitlementID;references:ID;constraint:fk_task_entitlement,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
	FkTaskOrder       *Order          `gorm:"foreignKey:SourceOrderID;references:ID;constraint:fk_task_order,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (SyncTask) TableName() string { return "sync_tasks" }

type LicentivoEvent struct {
	LicenseID                string               `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	AvailableAt              time.Time            `gorm:"type:datetime(6);not null;default:CURRENT_TIMESTAMP(6);index"`
	Attempts                 uint64               `gorm:"type:int unsigned;not null;default:0"`
	ID                       string               `gorm:"column:id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false"`
	ConnectionID             string               `gorm:"column:connection_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_licentivo_event,priority:1"`
	ExternalEventID          string               `gorm:"column:external_event_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_licentivo_event,priority:2"`
	BodyHash                 []byte               `gorm:"column:body_hash;type:binary(32);not null"`
	BodyCiphertext           []byte               `gorm:"column:body_ciphertext;type:mediumblob"`
	Status                   string               `gorm:"column:status;type:enum('pending','processed','review');not null;default:'pending'"`
	ReceivedAt               time.Time            `gorm:"column:received_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6)"`
	VerifiedAt               time.Time            `gorm:"column:verified_at;type:datetime(6);not null"`
	FkLicenseEventConnection *LicentivoConnection `gorm:"foreignKey:ConnectionID;references:ID;constraint:fk_license_event_connection,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (LicentivoEvent) TableName() string { return "licentivo_events" }

type AuditEntry struct {
	ID           string          `gorm:"column:id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false"`
	ActorID      *string         `gorm:"column:actor_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin"`
	Action       string          `gorm:"column:action;type:varchar(96) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	ObjectType   string          `gorm:"column:object_type;type:varchar(64) CHARACTER SET ascii COLLATE ascii_bin;not null;index:ix_audit_object,priority:1"`
	ObjectID     string          `gorm:"column:object_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;index:ix_audit_object,priority:2"`
	RequestID    string          `gorm:"column:request_id;type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	Reason       *string         `gorm:"column:reason;type:varchar(512)"`
	SafeMetadata json.RawMessage `gorm:"column:safe_metadata;type:json;not null"`
	CreatedAt    time.Time       `gorm:"column:created_at;type:datetime(6);not null;default:CURRENT_TIMESTAMP(6);index:ix_audit_object,priority:3"`
	FkAuditActor *User           `gorm:"foreignKey:ActorID;references:ID;constraint:fk_audit_actor,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (AuditEntry) TableName() string { return "audit_entries" }

func models() []any {
	return []any{&InstanceSettings{}, &SecretRecord{}, &User{}, &StaffAccount{}, &Session{}, &Product{}, &ProductDownload{}, &ProductDocumentImage{}, &TranslationSettings{}, &ContentTranslation{}, &TranslationTask{}, &LicentivoConnection{}, &Plan{}, &PlanVersion{}, &PlanLicenseMapping{}, &PaymentAccount{}, &Order{}, &Payment{}, &Refund{}, &PaymentEvent{}, &Entitlement{}, &EntitlementCoverage{}, &LicenseBinding{}, &SyncTask{}, &LicentivoEvent{}, &AuditEntry{}, &EmailCredential{}, &MailSettings{}, &EmailOutbox{}, &PlanPublication{}, &InvoiceRequest{}, &RuntimeStatus{}, &DeviceOperation{}}
}
