package storage

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

type PaymentReversal struct {
	ID                string    `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey"`
	AccountID         string    `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_payment_reversal,priority:1"`
	ExternalID        string    `gorm:"type:varchar(160) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_payment_reversal,priority:2"`
	PaymentID         string    `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;index"`
	SourceEventID     string    `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	AmountMinor       int64     `gorm:"type:bigint;not null"`
	Currency          string    `gorm:"type:char(3) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	CreatedAt         time.Time `gorm:"type:datetime(6);not null"`
	FkReversalPayment *Payment  `gorm:"foreignKey:PaymentID,AccountID;references:ID,AccountID;constraint:fk_reversal_payment,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

// GORM's MySQL AutoMigrate does not expand an existing enum. Upgrade the
// commercial order state explicitly, preserving all rows in existing installs.
func ensurePaymentReversalStatus(db *gorm.DB) error {
	var columnType string
	if err := db.Raw("SELECT COLUMN_TYPE FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='orders' AND column_name='status'").Scan(&columnType).Error; err != nil {
		return err
	}
	if !strings.Contains(columnType, "'reversed'") {
		return db.Migrator().AlterColumn(&Order{}, "Status")
	}
	return nil
}
