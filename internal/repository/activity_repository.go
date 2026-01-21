package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/creditlink/backend/internal/models"
	"gorm.io/gorm"
)

var (
	ErrActivityNotFound = errors.New("活动记录不存在")
	ErrActivityExists   = errors.New("活动记录已存在")
)

// ActivityRepository 借贷活动数据仓库
type ActivityRepository struct {
	db *gorm.DB
}

// NewActivityRepository 创建活动数据仓库实例
func NewActivityRepository(db *gorm.DB) *ActivityRepository {
	return &ActivityRepository{db: db}
}

// Create 创建借贷活动记录
func (r *ActivityRepository) Create(ctx context.Context, activity *models.LoanActivity) error {
	if err := r.db.WithContext(ctx).Create(activity).Error; err != nil {
		if strings.Contains(err.Error(), "Duplicate entry") {
			return ErrActivityExists
		}
		return err
	}
	return nil
}

// GetByTxHash 根据交易哈希查询活动
func (r *ActivityRepository) GetByTxHash(ctx context.Context, txHash string) (*models.LoanActivity, error) {
	var activity models.LoanActivity
	if err := r.db.WithContext(ctx).Where("tx_hash = ?", txHash).First(&activity).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrActivityNotFound
		}
		return nil, err
	}
	return &activity, nil
}

// GetByUserID 查询用户的活动记录
func (r *ActivityRepository) GetByUserID(ctx context.Context, userID uint, offset, limit int) ([]models.LoanActivity, int64, error) {
	var activities []models.LoanActivity
	var total int64

	query := r.db.WithContext(ctx).Model(&models.LoanActivity{}).Where("user_id = ?", userID)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Order("block_timestamp DESC").Offset(offset).Limit(limit).Find(&activities).Error; err != nil {
		return nil, 0, err
	}

	return activities, total, nil
}

// GetByUserAndType 根据用户和操作类型查询活动
func (r *ActivityRepository) GetByUserAndType(ctx context.Context, userID uint, actionType models.ActionType, offset, limit int) ([]models.LoanActivity, int64, error) {
	var activities []models.LoanActivity
	var total int64

	query := r.db.WithContext(ctx).Model(&models.LoanActivity{}).
		Where("user_id = ? AND action_type = ?", userID, actionType)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Order("block_timestamp DESC").Offset(offset).Limit(limit).Find(&activities).Error; err != nil {
		return nil, 0, err
	}

	return activities, total, nil
}

// CountByUserAndType 统计用户某类型操作的数量
func (r *ActivityRepository) CountByUserAndType(ctx context.Context, userID uint, actionType models.ActionType) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&models.LoanActivity{}).
		Where("user_id = ? AND action_type = ?", userID, actionType).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// CountRecentByUserAndType 统计用户在时间窗口内某类型操作的数量
func (r *ActivityRepository) CountRecentByUserAndType(ctx context.Context, userID uint, actionType models.ActionType, since time.Time) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&models.LoanActivity{}).
		Where("user_id = ? AND action_type = ? AND block_timestamp >= ?", userID, actionType, since).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// GetLastActivityByUser 查询用户最近的活动
func (r *ActivityRepository) GetLastActivityByUser(ctx context.Context, userID uint) (*models.LoanActivity, error) {
	var activity models.LoanActivity
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("block_timestamp DESC").
		First(&activity).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrActivityNotFound
		}
		return nil, err
	}
	return &activity, nil
}

// GetLiquidationsByUser 查询用户的清算记录
func (r *ActivityRepository) GetLiquidationsByUser(ctx context.Context, userID uint) ([]models.LoanActivity, error) {
	var activities []models.LoanActivity
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND action_type = ?", userID, models.ActionLiquidation).
		Order("block_timestamp DESC").
		Find(&activities).Error; err != nil {
		return nil, err
	}
	return activities, nil
}

// GetRepaymentsByUser 查询用户的还款记录
func (r *ActivityRepository) GetRepaymentsByUser(ctx context.Context, userID uint) ([]models.LoanActivity, error) {
	var activities []models.LoanActivity
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND action_type = ?", userID, models.ActionRepay).
		Order("block_timestamp DESC").
		Find(&activities).Error; err != nil {
		return nil, err
	}
	return activities, nil
}

// GetFirstActivityByUser 查询用户的第一次活动（用于计算忠诚度）
func (r *ActivityRepository) GetFirstActivityByUser(ctx context.Context, userID uint) (*models.LoanActivity, error) {
	var activity models.LoanActivity
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("block_timestamp ASC").
		First(&activity).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrActivityNotFound
		}
		return nil, err
	}
	return &activity, nil
}

// GetActivitiesByBlockRange 查询区块范围内的活动
func (r *ActivityRepository) GetActivitiesByBlockRange(ctx context.Context, startBlock, endBlock uint64) ([]models.LoanActivity, error) {
	var activities []models.LoanActivity
	if err := r.db.WithContext(ctx).
		Where("block_number >= ? AND block_number <= ?", startBlock, endBlock).
		Order("block_number ASC").
		Find(&activities).Error; err != nil {
		return nil, err
	}
	return activities, nil
}
