package storage

import (
	"encoding/json"
	"time"
)

// AI state is local to this merchant instance. Credentials and artifact bytes
// are encrypted with the instance vault; APIs return purpose-built views.
type AIProvider struct {
	Models     json.RawMessage `gorm:"type:json"`
	ID         string          `gorm:"type:char(36);primaryKey"`
	Name       string          `gorm:"type:varchar(100);not null"`
	Protocol   string          `gorm:"type:varchar(24);not null"`
	Capability string          `gorm:"type:varchar(16);not null;index"`
	BaseURL    string          `gorm:"type:varchar(500);not null"`
	Model      string          `gorm:"type:varchar(150);not null"`
	Secret     []byte          `gorm:"type:blob;not null"`
	Enabled    bool            `gorm:"not null"`
	UpdatedAt  time.Time       `gorm:"type:datetime(6);not null"`
}

type AIConversation struct {
	Model      string    `gorm:"type:varchar(150);not null;default:''"`
	Archived   bool      `gorm:"not null;default:false"`
	ID         string    `gorm:"type:char(36);primaryKey"`
	OwnerID    string    `gorm:"type:char(36);not null;index"`
	ProviderID string    `gorm:"type:char(36);not null"`
	Title      string    `gorm:"type:varchar(160);not null"`
	CreatedAt  time.Time `gorm:"type:datetime(6);not null"`
	UpdatedAt  time.Time `gorm:"type:datetime(6);not null"`
}

type AIMessage struct {
	Files          json.RawMessage `gorm:"type:json"`
	ID             string          `gorm:"type:char(36);primaryKey"`
	ConversationID string          `gorm:"type:char(36);not null;index"`
	TaskID         string          `gorm:"type:char(36);not null;index"`
	Role           string          `gorm:"type:varchar(16);not null"`
	Content        string          `gorm:"type:mediumtext;not null"`
	CreatedAt      time.Time       `gorm:"type:datetime(6);not null;index"`
}

type AITask struct {
	Model               string          `gorm:"type:varchar(150);not null;default:''"`
	SourceConnectionID  string          `gorm:"type:char(36);not null;default:'';index"`
	CancelRequested     bool            `gorm:"not null;default:false"`
	Steering            json.RawMessage `gorm:"type:json"`
	ID                  string          `gorm:"type:char(36);primaryKey"`
	OwnerID             string          `gorm:"type:char(36);not null;index"`
	ConversationID      string          `gorm:"type:char(36);not null;index"`
	ProviderID          string          `gorm:"type:char(36);not null"`
	ProviderFingerprint string          `gorm:"type:char(64);not null;default:''"`
	RequestKey          string          `gorm:"type:varchar(100);not null;uniqueIndex"`
	Kind                string          `gorm:"type:varchar(24);not null"`
	Title               string          `gorm:"type:varchar(160);not null"`
	Action              string          `gorm:"type:varchar(80);not null"`
	Input               json.RawMessage `gorm:"type:json;not null"`
	Result              json.RawMessage `gorm:"type:json;not null"`
	Status              string          `gorm:"type:varchar(32);not null;index"`
	ReadOnly            bool            `gorm:"not null"`
	Error               string          `gorm:"type:varchar(500);not null"`
	RemoteID            string          `gorm:"type:varchar(200);not null"`
	Progress            int             `gorm:"not null"`
	RunID               string          `gorm:"type:char(36);not null"`
	LeaseUntil          *time.Time      `gorm:"type:datetime(6)"`
	AvailableAt         time.Time       `gorm:"type:datetime(6);not null;index"`
	CreatedAt           time.Time       `gorm:"type:datetime(6);not null"`
	UpdatedAt           time.Time       `gorm:"type:datetime(6);not null"`
}

type AIArtifact struct {
	ID             string    `gorm:"type:char(36);primaryKey"`
	OwnerID        string    `gorm:"type:char(36);not null;index"`
	ConversationID string    `gorm:"type:char(36);not null;index"`
	TaskID         string    `gorm:"type:char(36);not null;index"`
	Filename       string    `gorm:"type:varchar(240);not null"`
	MIME           string    `gorm:"type:varchar(120);not null"`
	Size           int64     `gorm:"not null"`
	Path           string    `gorm:"type:varchar(100);not null"`
	CreatedAt      time.Time `gorm:"type:datetime(6);not null"`
}

type AISchedule struct {
	Spec            json.RawMessage `gorm:"type:json"`
	Model           string          `gorm:"type:varchar(150);not null;default:''"`
	ID              string          `gorm:"type:char(36);primaryKey"`
	OwnerID         string          `gorm:"type:char(36);not null;index"`
	ConversationID  string          `gorm:"type:char(36);not null"`
	ProviderID      string          `gorm:"type:char(36);not null"`
	Name            string          `gorm:"type:varchar(100);not null"`
	Goal            string          `gorm:"type:text;not null"`
	IntervalMinutes int             `gorm:"not null"`
	Enabled         bool            `gorm:"not null"`
	NextRunAt       time.Time       `gorm:"type:datetime(6);not null;index"`
	UpdatedAt       time.Time       `gorm:"type:datetime(6);not null"`
}

type AIUpload struct {
	ID             string    `gorm:"type:char(36);primaryKey"`
	OwnerID        string    `gorm:"type:char(36);not null;index"`
	ConversationID string    `gorm:"type:char(36);not null;default:'';index"`
	Filename       string    `gorm:"type:varchar(240);not null"`
	MIME           string    `gorm:"type:varchar(120);not null"`
	Size           int64     `gorm:"not null"`
	Path           string    `gorm:"type:varchar(100);not null"`
	TextCipher     []byte    `gorm:"type:mediumblob"`
	CreatedAt      time.Time `gorm:"type:datetime(6);not null"`
	ExpiresAt      time.Time `gorm:"type:datetime(6);not null;index"`
}

type AIMCPConnection struct {
	ID             string     `gorm:"type:char(36);primaryKey"`
	OwnerID        string     `gorm:"type:char(36);not null;index"`
	ConversationID string     `gorm:"type:char(36);not null"`
	Name           string     `gorm:"type:varchar(100);not null"`
	TokenHash      []byte     `gorm:"type:binary(32);not null;uniqueIndex"`
	AllowProposals bool       `gorm:"not null"`
	CreatedAt      time.Time  `gorm:"type:datetime(6);not null"`
	ExpiresAt      *time.Time `gorm:"type:datetime(6)"`
	RevokedAt      *time.Time `gorm:"type:datetime(6)"`
}
