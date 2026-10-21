package storage

import "time"

type EmailCredential struct {
	ID          string     `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey"`
	UserID      string     `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;index"`
	Kind        string     `gorm:"type:enum('verify','reset','change_email');not null"`
	TokenHash   []byte     `gorm:"type:binary(32);not null;uniqueIndex"`
	TargetEmail string     `gorm:"type:varchar(254);not null"`
	ExpiresAt   time.Time  `gorm:"type:datetime(6);not null"`
	UsedAt      *time.Time `gorm:"type:datetime(6)"`
	CreatedAt   time.Time  `gorm:"type:datetime(6);not null"`
	User        *User      `gorm:"foreignKey:UserID;constraint:OnDelete:RESTRICT"`
}

// MailSettings stores the selected delivery provider without exposing its
// credential. The credential is encrypted in SecretRecord with the instance
// master key, just like payment and licensing credentials.
type MailSettings struct {
	ID                 uint8         `gorm:"primaryKey;autoIncrement:false"`
	Provider           string        `gorm:"type:enum('resend');not null"`
	From               string        `gorm:"column:from_address;type:varchar(254);not null"`
	Host               string        `gorm:"type:varchar(253) CHARACTER SET ascii COLLATE ascii_bin;not null;default:'smtp.resend.com'"`
	Port               string        `gorm:"type:varchar(5) CHARACTER SET ascii COLLATE ascii_bin;not null;default:'465'"`
	Username           string        `gorm:"type:varchar(254);not null;default:'resend'"`
	Encryption         string        `gorm:"type:enum('implicit_tls','starttls');not null;default:'implicit_tls'"`
	CredentialSecretID string        `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	Enabled            bool          `gorm:"not null;default:true"`
	LastCheckedAt      *time.Time    `gorm:"type:datetime(6)"`
	LastCheckError     *string       `gorm:"type:varchar(160)"`
	UpdatedAt          time.Time     `gorm:"type:datetime(6);not null"`
	Credential         *SecretRecord `gorm:"foreignKey:CredentialSecretID;constraint:fk_mail_settings_secret,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (MailSettings) TableName() string { return "mail_settings" }

type EmailOutbox struct {
	ID            string     `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey"`
	Ciphertext    []byte     `gorm:"type:mediumblob;not null"`
	Status        string     `gorm:"type:enum('pending','running','retrying','sent','manual_review');not null;index:ix_mail_claim,priority:1"`
	Attempts      uint64     `gorm:"not null;default:0"`
	AvailableAt   time.Time  `gorm:"type:datetime(6);not null;index:ix_mail_claim,priority:2"`
	LeaseToken    *string    `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin"`
	LeaseUntil    *time.Time `gorm:"type:datetime(6)"`
	LastErrorCode *string    `gorm:"type:varchar(80) CHARACTER SET ascii COLLATE ascii_bin"`
	CreatedAt     time.Time  `gorm:"type:datetime(6);not null"`
	SentAt        *time.Time `gorm:"type:datetime(6)"`
}

// PlanPublication keeps public versions separate from newer unpublished drafts.
type PlanPublication struct {
	PlanID      string       `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey"`
	Environment string       `gorm:"type:enum('sandbox','live');primaryKey"`
	VersionID   string       `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	PreviewOnly bool         `gorm:"not null"`
	PublishedAt time.Time    `gorm:"type:datetime(6);not null"`
	Plan        *Plan        `gorm:"foreignKey:PlanID;constraint:OnDelete:RESTRICT"`
	Version     *PlanVersion `gorm:"foreignKey:VersionID;constraint:OnDelete:RESTRICT"`
}

// InvoiceRequest tracks the customer's tax-invoice request separately from
// the immutable payment record. Issued files stay with the original order.
type InvoiceRequest struct {
	ID             string     `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;primaryKey"`
	OrderID        string     `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex"`
	UserID         string     `gorm:"type:char(36) CHARACTER SET ascii COLLATE ascii_bin;not null;index"`
	InvoiceType    string     `gorm:"type:enum('personal','company');not null"`
	Title          string     `gorm:"type:varchar(200);not null"`
	TaxNumber      *string    `gorm:"type:varchar(64) CHARACTER SET ascii COLLATE ascii_bin"`
	RecipientEmail string     `gorm:"type:varchar(254);not null"`
	Status         string     `gorm:"type:enum('requested','issued','rejected');not null;default:'requested';index"`
	InvoiceURL     *string    `gorm:"type:text"`
	AdminNote      *string    `gorm:"type:varchar(500)"`
	CreatedAt      time.Time  `gorm:"type:datetime(6);not null"`
	UpdatedAt      time.Time  `gorm:"type:datetime(6);not null"`
	IssuedAt       *time.Time `gorm:"type:datetime(6)"`
	Order          *Order     `gorm:"foreignKey:OrderID;constraint:fk_invoice_order,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
	User           *User      `gorm:"foreignKey:UserID;constraint:fk_invoice_user,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (InvoiceRequest) TableName() string { return "invoice_requests" }

// RuntimeStatus is a single-row operational heartbeat. It lets the readiness
// page report what workers and backups actually did, instead of only checking
// whether configuration rows exist.
type RuntimeStatus struct {
	ID                uint8      `gorm:"primaryKey;autoIncrement:false"`
	CommerceHeartbeat *time.Time `gorm:"type:datetime(6)"`
	MailHeartbeat     *time.Time `gorm:"type:datetime(6)"`
	LastBackupAt      *time.Time `gorm:"type:datetime(6)"`
	UpdatedAt         time.Time  `gorm:"type:datetime(6);not null"`
}

func (RuntimeStatus) TableName() string { return "runtime_status" }
