package repository

import (
	"context"
	"errors"
	"time"

	"github.com/creditlink/backend/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrCreditNotFound = errors.New("信用记录不存在")
)

// CreditRepository 信用数据仓库
type CreditRepository struct {
	db *gorm.DB
}

// NewCreditRepository 创建信用数据仓库实例
func NewCreditRepository(db *gorm.DB) *CreditRepository {
	return &CreditRepository{db: db}
}

// CreateOrUpdate 创建或更新用户信用记录
func (r *CreditRepository) CreateOrUpdate(ctx context.Context, credit *models.UserCredit) error {
	now := time.Now()
	credit.LastCalculatedAt = &now

	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"score", "tier", "max_ltv", "max_borrow_limit", "last_calculated_at", "updated_at"}),
	}).Create(credit).Error
}

// GetByUserID 根据用户ID查询信用记录
func (r *CreditRepository) GetByUserID(ctx context.Context, userID uint) (*models.UserCredit, error) {
	var credit models.UserCredit
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&credit).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCreditNotFound
		}
		return nil, err
	}
	return &credit, nil
}

// UpdateScore 更新用户信用分
func (r *CreditRepository) UpdateScore(ctx context.Context, userID uint, score int, tier string, maxLTV int, maxBorrow string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&models.UserCredit{}).
		Where("user_id = ?", userID).
		Updates(map[string]any{
			"score":              score,
			"tier":               tier,
			"max_ltv":            maxLTV,
			"max_borrow_limit":   maxBorrow,
			"last_calculated_at": now,
		}).Error
}

// SaveFactors 保存或更新用户的信用因子
func (r *CreditRepository) SaveFactors(ctx context.Context, userID uint, factors []models.CreditFactor) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 删除现有因子
		if err := tx.Where("user_id = ?", userID).Delete(&models.CreditFactor{}).Error; err != nil {
			return err
		}

		// 插入新因子
		if len(factors) > 0 {
			for i := range factors {
				factors[i].UserID = userID
				factors[i].ComputedAt = time.Now()
			}
			if err := tx.Create(&factors).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// GetFactors 查询用户的信用因子
func (r *CreditRepository) GetFactors(ctx context.Context, userID uint) ([]models.CreditFactor, error) {
	var factors []models.CreditFactor
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Find(&factors).Error; err != nil {
		return nil, err
	}
	return factors, nil
}

// AddScoreHistory 添加信用分历史记录
func (r *CreditRepository) AddScoreHistory(ctx context.Context, history *models.CreditScoreHistory) error {
	return r.db.WithContext(ctx).Create(history).Error
}

// GetScoreHistory 查询用户的信用分历史
func (r *CreditRepository) GetScoreHistory(ctx context.Context, userID uint, limit int) ([]models.CreditScoreHistory, error) {
	var history []models.CreditScoreHistory
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit).
		Find(&history).Error; err != nil {
		return nil, err
	}
	return history, nil
}

// GetUsersByTier 根据信用等级查询用户
func (r *CreditRepository) GetUsersByTier(ctx context.Context, tier string, offset, limit int) ([]models.UserCredit, int64, error) {
	var credits []models.UserCredit
	var total int64

	query := r.db.WithContext(ctx).Model(&models.UserCredit{}).Where("tier = ?", tier)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Preload("User").Offset(offset).Limit(limit).Find(&credits).Error; err != nil {
		return nil, 0, err
	}

	return credits, total, nil
}

// GetCreditsNeedingRefresh 查询需要重新计算的信用记录
func (r *CreditRepository) GetCreditsNeedingRefresh(ctx context.Context, olderThan time.Duration, limit int) ([]models.UserCredit, error) {
	var credits []models.UserCredit
	threshold := time.Now().Add(-olderThan)

	if err := r.db.WithContext(ctx).
		Where("last_calculated_at IS NULL OR last_calculated_at < ?", threshold).
		Preload("User").
		Limit(limit).
		Find(&credits).Error; err != nil {
		return nil, err
	}

	return credits, nil
}
