package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/creditlink/backend/internal/models"
	"gorm.io/gorm"
)

var (
	ErrUserNotFound      = errors.New("用户不存在")
	ErrUserAlreadyExists = errors.New("用户已存在")
)

// UserRepository 用户数据仓库
type UserRepository struct {
	db *gorm.DB
}

// NewUserRepository 创建用户数据仓库实例
func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

// Create 创建新用户
func (r *UserRepository) Create(ctx context.Context, user *models.User) error {
	user.WalletAddress = strings.ToLower(user.WalletAddress)
	if err := r.db.WithContext(ctx).Create(user).Error; err != nil {
		if strings.Contains(err.Error(), "Duplicate entry") {
			return ErrUserAlreadyExists
		}
		return err
	}
	return nil
}

// GetByID 根据ID查询用户
func (r *UserRepository) GetByID(ctx context.Context, id uint) (*models.User, error) {
	var user models.User
	if err := r.db.WithContext(ctx).First(&user, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &user, nil
}

// GetByWallet 根据钱包地址查询用户
func (r *UserRepository) GetByWallet(ctx context.Context, wallet string) (*models.User, error) {
	var user models.User
	wallet = strings.ToLower(wallet)
	if err := r.db.WithContext(ctx).Where("wallet_address = ?", wallet).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &user, nil
}

// GetOrCreate 获取用户，如果不存在则创建
func (r *UserRepository) GetOrCreate(ctx context.Context, wallet string) (*models.User, error) {
	user, err := r.GetByWallet(ctx, wallet)
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, ErrUserNotFound) {
		return nil, err
	}

	// 创建新用户
	user = &models.User{WalletAddress: strings.ToLower(wallet)}
	if err := r.Create(ctx, user); err != nil {
		if errors.Is(err, ErrUserAlreadyExists) {
			// 并发情况：其他goroutine已创建该用户
			return r.GetByWallet(ctx, wallet)
		}
		return nil, err
	}
	return user, nil
}

// GetWithCredit 查询用户及其信用信息
func (r *UserRepository) GetWithCredit(ctx context.Context, wallet string) (*models.User, error) {
	var user models.User
	wallet = strings.ToLower(wallet)
	if err := r.db.WithContext(ctx).
		Preload("Credit").
		Where("wallet_address = ?", wallet).
		First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &user, nil
}

// GetWithFactors 查询用户及其信用因子
func (r *UserRepository) GetWithFactors(ctx context.Context, wallet string) (*models.User, error) {
	var user models.User
	wallet = strings.ToLower(wallet)
	if err := r.db.WithContext(ctx).
		Preload("Credit").
		Preload("Factors").
		Where("wallet_address = ?", wallet).
		First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &user, nil
}

// List 分页查询用户列表
func (r *UserRepository) List(ctx context.Context, offset, limit int) ([]models.User, int64, error) {
	var users []models.User
	var total int64

	if err := r.db.WithContext(ctx).Model(&models.User{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := r.db.WithContext(ctx).Offset(offset).Limit(limit).Find(&users).Error; err != nil {
		return nil, 0, err
	}

	return users, total, nil
}

// CountActiveUsers 统计活跃用户数量（有过交易记录的用户）
func (r *UserRepository) CountActiveUsers(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.User{}).
		Where("id IN (SELECT DISTINCT user_id FROM loan_activities)").
		Count(&count).Error
	return count, err
}
