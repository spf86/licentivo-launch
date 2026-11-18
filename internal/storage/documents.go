package storage

import (
	"encoding/json"
	"time"
)

// DocumentLibrary is independently versioned from product settings. Its JSON
// document contains ordered groups and separate draft/published article copies.
type DocumentLibrary struct {
	ProductID string          `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey;autoIncrement:false"`
	Version   uint64          `gorm:"not null;default:0"`
	Content   json.RawMessage `gorm:"type:json;not null"`
	UpdatedAt time.Time       `gorm:"type:datetime(6);not null"`
	FkProduct *Product        `gorm:"foreignKey:ProductID;references:ID;constraint:fk_document_library_product,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (DocumentLibrary) TableName() string { return "document_libraries" }
