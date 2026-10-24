package storage

import "time"

// Only identity trust and routing metadata; no license or device permission.
type LaunchTrustConnection struct {
	Environment string    `gorm:"type:varchar(16) CHARACTER SET ascii COLLATE ascii_bin;primaryKey"`
	RemoteID    string    `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	Issuer      string    `gorm:"type:varchar(255);not null"`
	Audience    string    `gorm:"type:varchar(255);not null"`
	UpdatedAt   time.Time `gorm:"type:datetime(6);not null"`
}
