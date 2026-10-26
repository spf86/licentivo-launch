package storage

import (
	"encoding/json"
	"time"
)

// Separate tables preserve existing Licentivo IDs and foreign keys.
type ExternalLicensingConnection struct {
	ID                  string          `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false"`
	Name                string          `gorm:"type:varchar(160);not null"`
	BaseURL             string          `gorm:"type:varchar(512);not null"`
	Environment         string          `gorm:"type:varchar(16) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	ConnectionRef       string          `gorm:"type:varchar(128) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	KeyID               string          `gorm:"type:varchar(64) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	CredentialSecretID  string          `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	AllowPrivateNetwork bool            `gorm:"not null;default:false"`
	AllowLocalHTTP      bool            `gorm:"not null;default:false"`
	ActivationOrigins   json.RawMessage `gorm:"type:json;not null"`
	Capabilities        json.RawMessage `gorm:"type:json;not null"`
	LastCheckedAt       *time.Time      `gorm:"type:datetime(6)"`
	LastCheckError      *string         `gorm:"type:varchar(160)"`
	CreatedAt           time.Time       `gorm:"type:datetime(6)"`
	UpdatedAt           time.Time       `gorm:"type:datetime(6)"`
	FkExternalSecret    *SecretRecord   `gorm:"foreignKey:CredentialSecretID;references:ID;constraint:fk_external_licensing_secret,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}
type ProductLicensingBinding struct {
	ProductID            string                       `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false"`
	Environment          string                       `gorm:"type:varchar(16) CHARACTER SET ascii COLLATE ascii_bin;primaryKey"`
	Provider             string                       `gorm:"type:varchar(32) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	ConnectionID         *string                      `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin"`
	UpdatedAt            time.Time                    `gorm:"type:datetime(6)"`
	FkLicensingProduct   *Product                     `gorm:"foreignKey:ProductID;references:ID;constraint:fk_product_licensing_product,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
	FkExternalConnection *ExternalLicensingConnection `gorm:"foreignKey:ConnectionID;references:ID;constraint:fk_product_external_connection,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}
type ExternalPlanMapping struct {
	PlanVersionID        string                       `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false"`
	Environment          string                       `gorm:"type:varchar(16) CHARACTER SET ascii COLLATE ascii_bin;primaryKey"`
	ConnectionID         string                       `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	ProductRef           string                       `gorm:"type:varchar(128) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	PlanRef              string                       `gorm:"type:varchar(128) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	FkExternalPlan       *PlanVersion                 `gorm:"foreignKey:PlanVersionID;references:ID;constraint:fk_external_mapping_plan,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
	FkExternalConnection *ExternalLicensingConnection `gorm:"foreignKey:ConnectionID;references:ID;constraint:fk_external_mapping_connection,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}
