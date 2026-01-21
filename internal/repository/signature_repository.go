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
	ErrSignatureNotFound = errors.New("签名记录不存在")
)

// SignatureRepository 签名日志数据仓库
type SignatureRepository struct {
	db *gorm.DB
}

// NewSignatureRepository 创建签名数据仓库实例
func NewSignatureRepository(db *gorm.DB) *SignatureRepository {
	return &SignatureRepository{db: db}
}

// Create 创建签名日志
func (r *SignatureRepository) Create(ctx context.Context, log *models.SignatureLog) error {
	log.WalletAddress = strings.ToLower(log.WalletAddress)
	return r.db.WithContext(ctx).Create(log).Error
}

// GetByRequestID 根据请求ID查询签名日志
func (r *SignatureRepository) GetByRequestID(ctx context.Context, requestID string) (*models.SignatureLog, error) {
	var log models.SignatureLog
	if err := r.db.WithContext(ctx).Where("request_id = ?", requestID).First(&log).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSignatureNotFound
		}
		return nil, err
	}
	return &log, nil
}

// GetByWalletAndNonce 根据钱包地址和nonce查询签名日志
func (r *SignatureRepository) GetByWalletAndNonce(ctx context.Context, wallet string, nonce uint64) (*models.SignatureLog, error) {
	var log models.SignatureLog
	wallet = strings.ToLower(wallet)
	if err := r.db.WithContext(ctx).
		Where("wallet_address = ? AND nonce = ?", wallet, nonce).
		First(&log).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSignatureNotFound
		}
		return nil, err
	}
	return &log, nil
}

// GetLatestNonceByWallet 获取钱包的最新nonce
func (r *SignatureRepository) GetLatestNonceByWallet(ctx context.Context, wallet string) (uint64, error) {
	var log models.SignatureLog
	wallet = strings.ToLower(wallet)
	if err := r.db.WithContext(ctx).
		Where("wallet_address = ?", wallet).
		Order("nonce DESC").
		First(&log).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil // 还没有签名，nonce从0开始
		}
		return 0, err
	}
	return log.Nonce + 1, nil // 返回下一个预期的nonce
}

// MarkAsUsed 标记签名已使用
func (r *SignatureRepository) MarkAsUsed(ctx context.Context, requestID string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&models.SignatureLog{}).
		Where("request_id = ?", requestID).
		Updates(map[string]any{
			"status":  models.SignatureUsed,
			"used_at": now,
		}).Error
}

// MarkAsExpired 标记签名已过期
func (r *SignatureRepository) MarkAsExpired(ctx context.Context, requestID string) error {
	return r.db.WithContext(ctx).Model(&models.SignatureLog{}).
		Where("request_id = ?", requestID).
		Update("status", models.SignatureExpired).Error
}

// MarkExpiredSignatures 批量标记过期签名
func (r *SignatureRepository) MarkExpiredSignatures(ctx context.Context) (int64, error) {
	now := time.Now().Unix()
	result := r.db.WithContext(ctx).Model(&models.SignatureLog{}).
		Where("status = ? AND deadline < ?", models.SignatureIssued, now).
		Update("status", models.SignatureExpired)
	return result.RowsAffected, result.Error
}

// GetByUserID 查询用户的签名日志
func (r *SignatureRepository) GetByUserID(ctx context.Context, userID uint, offset, limit int) ([]models.SignatureLog, int64, error) {
	var logs []models.SignatureLog
	var total int64

	query := r.db.WithContext(ctx).Model(&models.SignatureLog{}).Where("user_id = ?", userID)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&logs).Error; err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

// GetRecentByWallet 查询钱包最近的签名数量（用于频率限制）
func (r *SignatureRepository) GetRecentByWallet(ctx context.Context, wallet string, since time.Time) (int64, error) {
	var count int64
	wallet = strings.ToLower(wallet)
	if err := r.db.WithContext(ctx).Model(&models.SignatureLog{}).
		Where("wallet_address = ? AND created_at >= ?", wallet, since).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// GetRecentByIP 查询IP最近的签名数量（用于频率限制）
func (r *SignatureRepository) GetRecentByIP(ctx context.Context, ip string, since time.Time) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&models.SignatureLog{}).
		Where("ip_address = ? AND created_at >= ?", ip, since).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}
