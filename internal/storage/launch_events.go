package storage

import (
	"encoding/json"
	"time"
)

// LaunchEventStream is locked in the same transaction as the underlying fact.
// Ordering is per customer, so one customer's outage cannot block other users.
type LaunchEventStream struct {
	Subject  string `gorm:"type:char(64) CHARACTER SET ascii COLLATE ascii_bin;primaryKey"`
	Sequence uint64 `gorm:"type:bigint unsigned;not null"`
}

type LaunchOutbox struct {
	Results       json.RawMessage `gorm:"type:json"`
	RevokedBefore *time.Time      `gorm:"type:datetime(6)"`
	ID            string          `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey"`
	FactKey       string          `gorm:"type:varchar(160) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex"`
	Subject       string          `gorm:"type:char(64) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_launch_event_sequence,priority:1"`
	Sequence      uint64          `gorm:"type:bigint unsigned;not null;uniqueIndex:uq_launch_event_sequence,priority:2"`
	Kind          string          `gorm:"type:varchar(64) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	Body          json.RawMessage `gorm:"type:json;not null"`
	Status        string          `gorm:"type:varchar(24) CHARACTER SET ascii COLLATE ascii_bin;not null;index:ix_launch_outbox_claim,priority:1"`
	Attempts      uint64          `gorm:"type:int unsigned;not null"`
	AvailableAt   time.Time       `gorm:"type:datetime(6);not null;index:ix_launch_outbox_claim,priority:2"`
	LeaseToken    *string         `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin"`
	LeaseUntil    *time.Time      `gorm:"type:datetime(6)"`
	LastErrorCode *string         `gorm:"type:varchar(64) CHARACTER SET ascii COLLATE ascii_bin"`
	CreatedAt     time.Time       `gorm:"type:datetime(6);not null"`
	DeliveredAt   *time.Time      `gorm:"type:datetime(6)"`
}

func (LaunchOutbox) TableName() string { return "launch_outbox" }
