package database

import (
	"fmt"
	"log"
	"time"

	"github.com/creditlink/backend/internal/config"
	"github.com/creditlink/backend/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB 全局数据库连接
var DB *gorm.DB

// Connect 建立PostgreSQL数据库连接
func Connect(cfg *config.DatabaseConfig) (*gorm.DB, error) {
	// 配置GORM日志：只打印Error级别
	gormLogger := logger.Default.LogMode(logger.Error)

	db, err := gorm.Open(postgres.Open(cfg.DSN()), &gorm.Config{
		Logger: gormLogger,
	})
	if err != nil {
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}

	// 获取底层SQL连接
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("获取sql.DB失败: %w", err)
	}

	// 配置连接池
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Minute)

	// 测试连接
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("数据库连接测试失败: %w", err)
	}

	DB = db
	log.Println("数据库连接成功")

	return db, nil
}

// AutoMigrate 使用GORM运行数据库迁移
func AutoMigrate(db *gorm.DB) error {
	log.Println("正在执行数据库迁移...")

	err := db.AutoMigrate(
		// 用户相关
		&models.User{},
		&models.UserCredit{},
		&models.CreditFactor{},
		&models.CreditScoreHistory{},
		&models.UserPosition{},

		// 活动相关
		&models.LoanActivity{},
		&models.SignatureLog{},
		&models.SignatureNonceCounter{},

		// 风险相关
		&models.CurrentRiskParams{},
		&models.RiskParamsHistory{},

		// 协议相关
		&models.ProtocolRevenue{},
		&models.IndexerState{},
	)
	if err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}

	// 创建复合索引
	if err := createIndexes(db); err != nil {
		return fmt.Errorf("创建索引失败: %w", err)
	}

	log.Println("数据库迁移完成")
	return nil
}

// createIndexes 创建结构体标签中未定义的额外索引
func createIndexes(db *gorm.DB) error {
	// 信用因子复合索引 (user_id, factor_key)
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_credit_factors_user_key
		ON credit_factors(user_id, factor_key)
	`).Error; err != nil {
		log.Printf("注意: 创建索引返回: %v", err)
	}

	// 签名日志的 (wallet_address, nonce) 唯一索引由 SignatureLog 模型标签维护。

	// 用户持仓复合索引 (user_id, asset_address)
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_user_positions_user_asset
		ON user_positions(user_id, asset_address)
	`).Error; err != nil {
		log.Printf("注意: 创建索引返回: %v", err)
	}

	return nil
}

// Close 关闭数据库连接
func Close() error {
	if DB == nil {
		return nil
	}

	sqlDB, err := DB.DB()
	if err != nil {
		return err
	}

	return sqlDB.Close()
}

// GetDB 返回全局数据库连接
func GetDB() *gorm.DB {
	return DB
}
