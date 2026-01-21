package models

import (
	"time"

	"gorm.io/gorm"
)

// User represents a user in the system
type User struct {
	ID            uint           `gorm:"primaryKey" json:"id"`
	WalletAddress string         `gorm:"type:varchar(42);uniqueIndex;not null" json:"walletAddress"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`

	// Relations
	Credit       *UserCredit      `gorm:"foreignKey:UserID" json:"credit,omitempty"`
	Factors      []CreditFactor   `gorm:"foreignKey:UserID" json:"factors,omitempty"`
	Activities   []LoanActivity   `gorm:"foreignKey:UserID" json:"activities,omitempty"`
	SignatureLogs []SignatureLog  `gorm:"foreignKey:UserID" json:"signatureLogs,omitempty"`
}

// TableName specifies the table name for User
func (User) TableName() string {
	return "users"
}

// UserCredit represents a user's credit score and tier
type UserCredit struct {
	ID               uint       `gorm:"primaryKey" json:"id"`
	UserID           uint       `gorm:"uniqueIndex;not null" json:"userId"`
	Score            int        `gorm:"default:500" json:"score"`
	Tier             string     `gorm:"type:varchar(2);default:'C'" json:"tier"` // S/A/B/C/D
	MaxLTV           int        `gorm:"default:7000" json:"maxLtv"`              // basis points
	MaxBorrowLimit   string     `gorm:"type:decimal(65,0);default:0" json:"maxBorrowLimit"`
	LastCalculatedAt *time.Time `json:"lastCalculatedAt"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`

	// Relations
	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// TableName specifies the table name for UserCredit
func (UserCredit) TableName() string {
	return "user_credits"
}

// CreditFactor represents individual factors contributing to credit score
type CreditFactor struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	UserID       uint      `gorm:"index;not null" json:"userId"`
	FactorKey    string    `gorm:"type:varchar(50);not null" json:"factorKey"`
	FactorValue  float64   `gorm:"type:decimal(18,4)" json:"factorValue"`
	Weight       float64   `gorm:"type:decimal(8,4)" json:"weight"`
	Contribution int       `gorm:"default:0" json:"contribution"` // contribution to total score
	ComputedAt   time.Time `json:"computedAt"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`

	// Relations
	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// TableName specifies the table name for CreditFactor
func (CreditFactor) TableName() string {
	return "credit_factors"
}

// UniqueIndex for CreditFactor (user_id, factor_key)
func (CreditFactor) BeforeCreate(tx *gorm.DB) error {
	return nil
}
