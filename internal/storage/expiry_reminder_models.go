package storage

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

// A single site-wide policy. SMTP credentials and quota policy are deliberately
// independent, so changing a budget never rotates a sender's credentials.
type MailQuotaSetting struct {
	ID        uint8     `gorm:"primaryKey;autoIncrement:false"`
	Policy    string    `gorm:"type:text;not null"`
	Version   string    `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	UpdatedAt time.Time `gorm:"type:datetime(6);not null"`
}

type MailQuotaWindow struct {
	ID               string    `gorm:"type:varchar(32) CHARACTER SET ascii COLLATE ascii_bin;primaryKey"`
	TotalAttempts    int64     `gorm:"not null;default:0"`
	ReminderAttempts int64     `gorm:"not null;default:0"`
	EndsAt           time.Time `gorm:"type:datetime(6);not null;index"`
}

type ExpiryReminderSetting struct {
	ID           uint8     `gorm:"primaryKey;autoIncrement:false"`
	Enabled      bool      `gorm:"not null"`
	DefaultSend  bool      `gorm:"not null"`
	ReminderDays string    `gorm:"type:varchar(120);not null"`
	Locale       string    `gorm:"type:varchar(16);not null"`
	ContactEmail string    `gorm:"type:varchar(254);not null;default:''"`
	Version      string    `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	UpdatedAt    time.Time `gorm:"type:datetime(6);not null"`
}

type ExpiryReminderPreference struct {
	EntitlementID string       `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey" json:"entitlement_id"`
	Mode          string       `gorm:"type:varchar(16);not null" json:"mode"`
	Locale        string       `gorm:"type:varchar(16);not null;default:''" json:"locale"`
	Version       string       `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null" json:"version"`
	UpdatedAt     time.Time    `gorm:"type:datetime(6);not null" json:"updated_at"`
	Entitlement   *Entitlement `gorm:"foreignKey:EntitlementID;constraint:fk_expiry_preference_entitlement,OnDelete:RESTRICT" json:"-"`
}

// A database lease coordinates hourly scans across every instance of one site.
type ExpiryReminderSchedule struct {
	ID            uint8      `gorm:"primaryKey;autoIncrement:false"`
	NextRunAt     time.Time  `gorm:"type:datetime(6);not null"`
	LeaseToken    *string    `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin"`
	LeaseUntil    *time.Time `gorm:"type:datetime(6)"`
	LastStartedAt *time.Time `gorm:"type:datetime(6)"`
	LastErrorCode string     `gorm:"type:varchar(80);not null;default:''"`
}

func ensureEmailOutboxStatus(db *gorm.DB) error {
	var column string
	if err := db.Raw("SELECT COLUMN_TYPE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='email_outboxes' AND COLUMN_NAME='status'").Scan(&column).Error; err != nil {
		return err
	}
	if strings.Contains(column, "'cancelled'") {
		return nil
	}
	return db.Exec("ALTER TABLE email_outboxes MODIFY status ENUM('pending','running','retrying','sent','manual_review','cancelled') NOT NULL").Error
}
