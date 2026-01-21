package models

import (
	"time"
)

// RiskParamsHistory tracks historical changes to risk parameters
type RiskParamsHistory struct {
	ID                   uint      `gorm:"primaryKey" json:"id"`
	AssetAddress         string    `gorm:"type:varchar(42);not null;index" json:"assetAddress"`
	AssetSymbol          string    `gorm:"type:varchar(20)" json:"assetSymbol"`
	BaseLTV              int       `json:"baseLtv"`              // basis points
	LiquidationThreshold int       `json:"liquidationThreshold"` // basis points
	LiquidationBonus     int       `json:"liquidationBonus"`     // basis points
	CloseFactor          int       `json:"closeFactor"`          // basis points
	BorrowCap            string    `gorm:"type:decimal(65,0)" json:"borrowCap"`
	SupplyCap            string    `gorm:"type:decimal(65,0)" json:"supplyCap"`
	BorrowEnabled        bool      `gorm:"default:true" json:"borrowEnabled"`
	CollateralEnabled    bool      `gorm:"default:true" json:"collateralEnabled"`
	Version              int       `gorm:"index" json:"version"`
	UpdatedBy            string    `gorm:"type:varchar(42)" json:"updatedBy"`
	TxHash               string    `gorm:"type:varchar(66)" json:"txHash"`
	CreatedAt            time.Time `json:"createdAt"`
}

// TableName specifies the table name for RiskParamsHistory
func (RiskParamsHistory) TableName() string {
	return "risk_params_history"
}

// CurrentRiskParams represents the current active risk parameters for an asset
type CurrentRiskParams struct {
	ID                   uint      `gorm:"primaryKey" json:"id"`
	AssetAddress         string    `gorm:"type:varchar(42);uniqueIndex;not null" json:"assetAddress"`
	AssetSymbol          string    `gorm:"type:varchar(20)" json:"assetSymbol"`
	Decimals             uint8     `gorm:"default:18" json:"decimals"`
	PriceOracleAddress   string    `gorm:"type:varchar(42)" json:"priceOracleAddress"`
	BaseLTV              int       `json:"baseLtv"`              // basis points
	LiquidationThreshold int       `json:"liquidationThreshold"` // basis points
	LiquidationBonus     int       `json:"liquidationBonus"`     // basis points
	CloseFactor          int       `json:"closeFactor"`          // basis points
	BorrowCap            string    `gorm:"type:decimal(65,0)" json:"borrowCap"`
	SupplyCap            string    `gorm:"type:decimal(65,0)" json:"supplyCap"`
	BorrowEnabled        bool      `gorm:"default:true" json:"borrowEnabled"`
	CollateralEnabled    bool      `gorm:"default:true" json:"collateralEnabled"`
	Version              int       `gorm:"default:1" json:"version"`
	CreatedAt            time.Time `json:"createdAt"`
	UpdatedAt            time.Time `json:"updatedAt"`
}

// TableName specifies the table name for CurrentRiskParams
func (CurrentRiskParams) TableName() string {
	return "current_risk_params"
}

// ProtocolRevenue tracks daily protocol revenue
type ProtocolRevenue struct {
	ID                  uint      `gorm:"primaryKey" json:"id"`
	Date                time.Time `gorm:"type:date;uniqueIndex;not null" json:"date"`
	InterestRevenue     string    `gorm:"type:decimal(65,0);default:0" json:"interestRevenue"`
	LiquidationRevenue  string    `gorm:"type:decimal(65,0);default:0" json:"liquidationRevenue"`
	FlashloanRevenue    string    `gorm:"type:decimal(65,0);default:0" json:"flashloanRevenue"`
	TotalRevenue        string    `gorm:"type:decimal(65,0);default:0" json:"totalRevenue"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

// TableName specifies the table name for ProtocolRevenue
func (ProtocolRevenue) TableName() string {
	return "protocol_revenue"
}

// IndexerState tracks the indexer's progress
type IndexerState struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	ContractAddress string    `gorm:"type:varchar(42);uniqueIndex;not null" json:"contractAddress"`
	LastBlockNumber uint64    `gorm:"default:0" json:"lastBlockNumber"`
	LastBlockHash   string    `gorm:"type:varchar(66)" json:"lastBlockHash"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// TableName specifies the table name for IndexerState
func (IndexerState) TableName() string {
	return "indexer_state"
}

// UserPosition tracks user's current position in the lending pool
type UserPosition struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	UserID          uint      `gorm:"index;not null" json:"userId"`
	AssetAddress    string    `gorm:"type:varchar(42);not null;index" json:"assetAddress"`
	DepositAmount   string    `gorm:"type:decimal(65,0);default:0" json:"depositAmount"`
	BorrowAmount    string    `gorm:"type:decimal(65,0);default:0" json:"borrowAmount"`
	LastUpdatedAt   time.Time `json:"lastUpdatedAt"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`

	// Relations
	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// TableName specifies the table name for UserPosition
func (UserPosition) TableName() string {
	return "user_positions"
}
