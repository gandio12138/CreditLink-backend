package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/creditlink/backend/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrRiskParamsNotFound = errors.New("风险参数不存在")
	ErrPositionNotFound   = errors.New("持仓记录不存在")
)

// RiskRepository 风险数据仓库
type RiskRepository struct {
	db *gorm.DB
}

// NewRiskRepository 创建风险数据仓库实例
func NewRiskRepository(db *gorm.DB) *RiskRepository {
	return &RiskRepository{db: db}
}

// ========== 风险参数相关 ==========

// GetRiskParams 获取资产的风险参数
func (r *RiskRepository) GetRiskParams(ctx context.Context, assetAddress string) (*models.CurrentRiskParams, error) {
	var params models.CurrentRiskParams
	assetAddress = strings.ToLower(assetAddress)
	if err := r.db.WithContext(ctx).Where("asset_address = ?", assetAddress).First(&params).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRiskParamsNotFound
		}
		return nil, err
	}
	return &params, nil
}

// GetAllRiskParams 获取所有风险参数
func (r *RiskRepository) GetAllRiskParams(ctx context.Context) ([]models.CurrentRiskParams, error) {
	var params []models.CurrentRiskParams
	if err := r.db.WithContext(ctx).Find(&params).Error; err != nil {
		return nil, err
	}
	return params, nil
}

// GetEnabledAssets 获取启用借款的资产
func (r *RiskRepository) GetEnabledAssets(ctx context.Context) ([]models.CurrentRiskParams, error) {
	var params []models.CurrentRiskParams
	if err := r.db.WithContext(ctx).Where("borrow_enabled = ?", true).Find(&params).Error; err != nil {
		return nil, err
	}
	return params, nil
}

// SaveRiskParams 保存或更新风险参数
func (r *RiskRepository) SaveRiskParams(ctx context.Context, params *models.CurrentRiskParams) error {
	return saveRiskParams(r.db.WithContext(ctx), params)
}

// ReplaceRiskParams atomically mirrors a complete on-chain RiskRegistry snapshot.
func (r *RiskRepository) ReplaceRiskParams(ctx context.Context, params []models.CurrentRiskParams) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		addresses := make([]string, 0, len(params))
		for i := range params {
			params[i].AssetAddress = strings.ToLower(params[i].AssetAddress)
			addresses = append(addresses, params[i].AssetAddress)
			if err := saveRiskParams(tx, &params[i]); err != nil {
				return err
			}
		}

		query := tx.Model(&models.CurrentRiskParams{})
		if len(addresses) == 0 {
			return query.Where("id > ?", 0).Delete(&models.CurrentRiskParams{}).Error
		}
		return query.Where("asset_address NOT IN ?", addresses).Delete(&models.CurrentRiskParams{}).Error
	})
}

func saveRiskParams(db *gorm.DB, params *models.CurrentRiskParams) error {
	params.AssetAddress = strings.ToLower(params.AssetAddress)
	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "asset_address"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"asset_symbol", "decimals", "price_oracle_address", "base_ltv",
			"liquidation_threshold", "liquidation_bonus", "close_factor",
			"borrow_cap", "supply_cap", "borrow_enabled", "collateral_enabled",
			"version", "updated_at",
		}),
	}).Create(params).Error
}

// AddRiskParamsHistory 添加风险参数变更历史
func (r *RiskRepository) AddRiskParamsHistory(ctx context.Context, history *models.RiskParamsHistory) error {
	history.AssetAddress = strings.ToLower(history.AssetAddress)
	return r.db.WithContext(ctx).Create(history).Error
}

// GetRiskParamsHistory 查询资产的风险参数历史
func (r *RiskRepository) GetRiskParamsHistory(ctx context.Context, assetAddress string, limit int) ([]models.RiskParamsHistory, error) {
	var history []models.RiskParamsHistory
	assetAddress = strings.ToLower(assetAddress)
	if err := r.db.WithContext(ctx).
		Where("asset_address = ?", assetAddress).
		Order("version DESC").
		Limit(limit).
		Find(&history).Error; err != nil {
		return nil, err
	}
	return history, nil
}

// ========== 用户持仓相关 ==========

// GetUserPosition 获取用户在某资产的持仓
func (r *RiskRepository) GetUserPosition(ctx context.Context, userID uint, assetAddress string) (*models.UserPosition, error) {
	var position models.UserPosition
	assetAddress = strings.ToLower(assetAddress)
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND asset_address = ?", userID, assetAddress).
		First(&position).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPositionNotFound
		}
		return nil, err
	}
	return &position, nil
}

// GetUserPositions 获取用户的所有持仓
func (r *RiskRepository) GetUserPositions(ctx context.Context, userID uint) ([]models.UserPosition, error) {
	var positions []models.UserPosition
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Find(&positions).Error; err != nil {
		return nil, err
	}
	return positions, nil
}

// SaveUserPosition 保存或更新用户持仓
func (r *RiskRepository) SaveUserPosition(ctx context.Context, position *models.UserPosition) error {
	position.AssetAddress = strings.ToLower(position.AssetAddress)
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "asset_address"}},
		DoUpdates: clause.AssignmentColumns([]string{"deposit_amount", "borrow_amount", "last_updated_at", "updated_at"}),
	}).Create(position).Error
}

// GetBorrowersWithDebt 获取有未偿债务的用户
func (r *RiskRepository) GetBorrowersWithDebt(ctx context.Context) ([]models.UserPosition, error) {
	var positions []models.UserPosition
	if err := r.db.WithContext(ctx).
		Where("borrow_amount > ?", "0").
		Preload("User").
		Find(&positions).Error; err != nil {
		return nil, err
	}
	return positions, nil
}

// ========== 索引器状态相关 ==========

// GetIndexerState 获取合约的索引器状态
func (r *RiskRepository) GetIndexerState(ctx context.Context, contractAddress string) (*models.IndexerState, error) {
	var state models.IndexerState
	contractAddress = strings.ToLower(contractAddress)
	if err := r.db.WithContext(ctx).Where("contract_address = ?", contractAddress).First(&state).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil // 未找到是正常的，将从配置的区块开始
		}
		return nil, err
	}
	return &state, nil
}

// SaveIndexerState 保存索引器状态
func (r *RiskRepository) SaveIndexerState(ctx context.Context, state *models.IndexerState) error {
	state.ContractAddress = strings.ToLower(state.ContractAddress)
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "contract_address"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_block_number", "last_block_hash", "updated_at"}),
	}).Create(state).Error
}

// ========== 协议收入相关 ==========

// GetProtocolRevenue 获取指定日期的协议收入
func (r *RiskRepository) GetProtocolRevenue(ctx context.Context, date string) (*models.ProtocolRevenue, error) {
	var revenue models.ProtocolRevenue
	if err := r.db.WithContext(ctx).Where("DATE(date) = ?", date).First(&revenue).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &revenue, nil
}

// SaveProtocolRevenue 保存或更新协议收入
func (r *RiskRepository) SaveProtocolRevenue(ctx context.Context, revenue *models.ProtocolRevenue) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "date"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"interest_revenue", "liquidation_revenue", "flashloan_revenue", "total_revenue", "updated_at",
		}),
	}).Create(revenue).Error
}
