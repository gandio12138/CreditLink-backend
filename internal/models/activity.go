package models

import (
	"time"
)

// ActionType represents the type of loan activity
type ActionType string

const (
	ActionDeposit     ActionType = "DEPOSIT"
	ActionWithdraw    ActionType = "WITHDRAW"
	ActionBorrow      ActionType = "BORROW"
	ActionRepay       ActionType = "REPAY"
	ActionLiquidation ActionType = "LIQUIDATION"
)

// LoanActivity represents on-chain loan activities
type LoanActivity struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	UserID         uint       `gorm:"index;not null" json:"userId"`
	TxHash         string     `gorm:"type:varchar(66);uniqueIndex;not null" json:"txHash"`
	ActionType     ActionType `gorm:"type:varchar(20);not null;index" json:"actionType"`
	AssetAddress   string     `gorm:"type:varchar(42);not null" json:"assetAddress"`
	Amount         string     `gorm:"type:decimal(65,0);not null" json:"amount"`
	BlockNumber    uint64     `gorm:"index" json:"blockNumber"`
	BlockTimestamp time.Time  `json:"blockTimestamp"`
	CreatedAt      time.Time  `json:"createdAt"`

	// Additional data for specific action types
	LTV         *int    `gorm:"type:int" json:"ltv,omitempty"`                // for BORROW
	Nonce       *uint64 `json:"nonce,omitempty"`                              // for BORROW
	Liquidator  *string `gorm:"type:varchar(42)" json:"liquidator,omitempty"` // for LIQUIDATION
	DebtCovered *string `gorm:"type:decimal(65,0)" json:"debtCovered,omitempty"`
	Collateral  *string `gorm:"type:varchar(42)" json:"collateral,omitempty"`

	// Relations
	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// TableName specifies the table name for LoanActivity
func (LoanActivity) TableName() string {
	return "loan_activities"
}

// SignatureStatus represents the status of a signature
type SignatureStatus string

const (
	SignatureIssued  SignatureStatus = "ISSUED"
	SignatureUsed    SignatureStatus = "USED"
	SignatureExpired SignatureStatus = "EXPIRED"
	SignatureRevoked SignatureStatus = "REVOKED"
)

// SignatureLog represents audit log for issued signatures
type SignatureLog struct {
	ID            uint            `gorm:"primaryKey" json:"id"`
	RequestID     string          `gorm:"type:varchar(36);not null" json:"requestId"`
	UserID        uint            `gorm:"index;not null" json:"userId"`
	WalletAddress string          `gorm:"type:varchar(42);not null;index;uniqueIndex:uidx_signature_logs_wallet_nonce,priority:1" json:"walletAddress"`
	Market        string          `gorm:"type:varchar(10);not null" json:"market"`
	AuthorizedLTV int             `gorm:"not null" json:"authorizedLtv"`
	AmountCap     string          `gorm:"type:decimal(65,0)" json:"amountCap"`
	Nonce         uint64          `gorm:"not null;index;uniqueIndex:uidx_signature_logs_wallet_nonce,priority:2" json:"nonce"`
	Deadline      int64           `gorm:"not null" json:"deadline"`
	Signature     string          `gorm:"type:text;not null" json:"signature"`
	IPAddress     string          `gorm:"type:varchar(45)" json:"ipAddress"`
	UserAgent     string          `gorm:"type:text" json:"userAgent"`
	CreditScore   int             `json:"creditScore"`
	Status        SignatureStatus `gorm:"type:varchar(20);default:'ISSUED';index" json:"status"`
	CreatedAt     time.Time       `json:"createdAt"`
	UsedAt        *time.Time      `json:"usedAt,omitempty"`

	// Relations
	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// TableName specifies the table name for SignatureLog
func (SignatureLog) TableName() string {
	return "signature_logs"
}

// SignatureNonceCounter tracks the next nonce reserved for each wallet.
// Gaps are intentional: once reserved, a nonce is never reused even if signing later fails.
type SignatureNonceCounter struct {
	WalletAddress string    `gorm:"type:varchar(42);primaryKey" json:"walletAddress"`
	NextNonce     uint64    `gorm:"not null" json:"nextNonce"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// TableName specifies the table name for SignatureNonceCounter.
func (SignatureNonceCounter) TableName() string {
	return "signature_nonce_counters"
}

// CreditScoreHistory tracks historical credit score changes
type CreditScoreHistory struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	UserID      uint      `gorm:"index;not null" json:"userId"`
	Score       int       `gorm:"not null" json:"score"`
	Tier        string    `gorm:"type:varchar(2)" json:"tier"`
	EventType   string    `gorm:"type:varchar(50)" json:"eventType"` // BORROW, REPAY, LIQUIDATION, etc.
	EventTxHash *string   `gorm:"type:varchar(66)" json:"eventTxHash,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`

	// Relations
	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// TableName specifies the table name for CreditScoreHistory
func (CreditScoreHistory) TableName() string {
	return "credit_score_history"
}
