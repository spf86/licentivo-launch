package storage

import (
	"encoding/json"
	"time"
)

// The quota reservation is durable, including an uncertain remote response.
// Retrying an operation reuses its reservation and original device generation.
type DeviceOperation struct {
	ID             string          `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey"`
	EntitlementID  string          `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_device_request,priority:1"`
	IdempotencyKey string          `gorm:"type:varchar(80) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_device_request,priority:2"`
	RequestJSON    json.RawMessage `gorm:"type:json;not null"`
	RequestHash    []byte          `gorm:"type:binary(32);not null"`
	Status         string          `gorm:"type:enum('pending','running','succeeded','manual_review');not null"`
	LeaseToken     *string         `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin"`
	LeaseUntil     *time.Time      `gorm:"type:datetime(6)"`
	AvailableAt    time.Time       `gorm:"type:datetime(6);not null;index"`
	Attempts       uint64          `gorm:"type:int unsigned;not null;default:0"`
	CreatedAt      time.Time       `gorm:"type:datetime(6);not null;index"`
	FkEntitlement  *Entitlement    `gorm:"foreignKey:EntitlementID;constraint:OnDelete:RESTRICT"`
}
