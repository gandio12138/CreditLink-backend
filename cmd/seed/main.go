package main

import (
	"fmt"
	"log"
	"time"

	"github.com/creditlink/backend/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	dsn := "host=198.46.172.149 user=postgres password=1q2w3e! dbname=postgres port=5432 sslmode=disable"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("连接数据库失败:", err)
	}

	log.Println("开始插入测试数据...")

	// 插入风险参数到 current_risk_params 表
	riskParams := []models.CurrentRiskParams{
		{AssetAddress: "0x1234567890abcdef1234567890abcdef12345678", AssetSymbol: "USDT", Decimals: 6, BaseLTV: 75, LiquidationThreshold: 80, LiquidationBonus: 5, CloseFactor: 50, BorrowCap: "1000000000000", SupplyCap: "2000000000000", BorrowEnabled: true, CollateralEnabled: true},
		{AssetAddress: "0x2234567890abcdef1234567890abcdef12345678", AssetSymbol: "USDC", Decimals: 6, BaseLTV: 75, LiquidationThreshold: 80, LiquidationBonus: 5, CloseFactor: 50, BorrowCap: "1000000000000", SupplyCap: "2000000000000", BorrowEnabled: true, CollateralEnabled: true},
		{AssetAddress: "0x3234567890abcdef1234567890abcdef12345678", AssetSymbol: "WETH", Decimals: 18, BaseLTV: 70, LiquidationThreshold: 75, LiquidationBonus: 8, CloseFactor: 50, BorrowCap: "500000000000000000000", SupplyCap: "1000000000000000000000", BorrowEnabled: true, CollateralEnabled: true},
		{AssetAddress: "0x4234567890abcdef1234567890abcdef12345678", AssetSymbol: "WBTC", Decimals: 8, BaseLTV: 70, LiquidationThreshold: 75, LiquidationBonus: 8, CloseFactor: 50, BorrowCap: "10000000000", SupplyCap: "20000000000", BorrowEnabled: true, CollateralEnabled: true},
	}

	for _, rp := range riskParams {
		result := db.Where("asset_address = ?", rp.AssetAddress).FirstOrCreate(&rp)
		if result.Error != nil {
			log.Printf("插入风险参数失败 %s: %v", rp.AssetSymbol, result.Error)
		} else {
			log.Printf("风险参数已插入/存在: %s", rp.AssetSymbol)
		}
	}

	// 创建测试用户 (地址必须是42字符: 0x + 40 hex)
	users := []models.User{
		{WalletAddress: "0x1111111111111111111111111111111111111111"},
		{WalletAddress: "0x2222222222222222222222222222222222222222"},
		{WalletAddress: "0x3333333333333333333333333333333333333333"},
		{WalletAddress: "0x4444444444444444444444444444444444444444"},
		{WalletAddress: "0x5555555555555555555555555555555555555555"},
	}

	for _, u := range users {
		result := db.Where("wallet_address = ?", u.WalletAddress).FirstOrCreate(&u)
		if result.Error != nil {
			log.Printf("插入用户失败: %v", result.Error)
		} else {
			log.Printf("用户已插入/存在: %s (ID: %d)", u.WalletAddress, u.ID)
		}
	}

	// 查询所有用户ID
	var allUsers []models.User
	db.Find(&allUsers)
	if len(allUsers) < 5 {
		log.Fatal("用户数量不足")
	}

	// 插入借贷活动（模拟数据）
	now := time.Now()
	activities := []models.LoanActivity{
		// 存款活动 - USDT
		{UserID: allUsers[0].ID, TxHash: "0xseed0001", ActionType: models.ActionDeposit, AssetAddress: "0x1234567890abcdef1234567890abcdef12345678", Amount: "5000000000", BlockNumber: 1000001, BlockTimestamp: now.Add(-72 * time.Hour)},
		{UserID: allUsers[1].ID, TxHash: "0xseed0002", ActionType: models.ActionDeposit, AssetAddress: "0x1234567890abcdef1234567890abcdef12345678", Amount: "3000000000", BlockNumber: 1000002, BlockTimestamp: now.Add(-60 * time.Hour)},
		// 存款活动 - USDC
		{UserID: allUsers[2].ID, TxHash: "0xseed0003", ActionType: models.ActionDeposit, AssetAddress: "0x2234567890abcdef1234567890abcdef12345678", Amount: "8000000000", BlockNumber: 1000003, BlockTimestamp: now.Add(-48 * time.Hour)},
		// 存款活动 - WETH
		{UserID: allUsers[3].ID, TxHash: "0xseed0004", ActionType: models.ActionDeposit, AssetAddress: "0x3234567890abcdef1234567890abcdef12345678", Amount: "2000000000000000000", BlockNumber: 1000004, BlockTimestamp: now.Add(-36 * time.Hour)},
		// 存款活动 - WBTC
		{UserID: allUsers[4].ID, TxHash: "0xseed0005", ActionType: models.ActionDeposit, AssetAddress: "0x4234567890abcdef1234567890abcdef12345678", Amount: "50000000", BlockNumber: 1000005, BlockTimestamp: now.Add(-24 * time.Hour)},
		// 更多存款
		{UserID: allUsers[0].ID, TxHash: "0xseed0006", ActionType: models.ActionDeposit, AssetAddress: "0x2234567890abcdef1234567890abcdef12345678", Amount: "2000000000", BlockNumber: 1000006, BlockTimestamp: now.Add(-20 * time.Hour)},
		{UserID: allUsers[1].ID, TxHash: "0xseed0007", ActionType: models.ActionDeposit, AssetAddress: "0x3234567890abcdef1234567890abcdef12345678", Amount: "1500000000000000000", BlockNumber: 1000007, BlockTimestamp: now.Add(-18 * time.Hour)},
		// 借款活动
		{UserID: allUsers[0].ID, TxHash: "0xseed0010", ActionType: models.ActionBorrow, AssetAddress: "0x1234567890abcdef1234567890abcdef12345678", Amount: "2000000000", BlockNumber: 1000010, BlockTimestamp: now.Add(-15 * time.Hour)},
		{UserID: allUsers[1].ID, TxHash: "0xseed0011", ActionType: models.ActionBorrow, AssetAddress: "0x2234567890abcdef1234567890abcdef12345678", Amount: "1500000000", BlockNumber: 1000011, BlockTimestamp: now.Add(-12 * time.Hour)},
		{UserID: allUsers[2].ID, TxHash: "0xseed0012", ActionType: models.ActionBorrow, AssetAddress: "0x1234567890abcdef1234567890abcdef12345678", Amount: "1000000000", BlockNumber: 1000012, BlockTimestamp: now.Add(-10 * time.Hour)},
		// 还款活动
		{UserID: allUsers[0].ID, TxHash: "0xseed0020", ActionType: models.ActionRepay, AssetAddress: "0x1234567890abcdef1234567890abcdef12345678", Amount: "500000000", BlockNumber: 1000020, BlockTimestamp: now.Add(-5 * time.Hour)},
	}

	for _, a := range activities {
		result := db.Where("tx_hash = ?", a.TxHash).FirstOrCreate(&a)
		if result.Error != nil {
			log.Printf("插入活动失败 %s: %v", a.TxHash, result.Error)
		} else {
			log.Printf("活动已插入/存在: %s - %s", a.TxHash, a.ActionType)
		}
	}

	log.Println("测试数据插入完成!")

	// 验证数据
	var riskCount int64
	db.Model(&models.CurrentRiskParams{}).Count(&riskCount)
	log.Printf("风险参数数量: %d", riskCount)

	var totalDeposits int64
	db.Model(&models.LoanActivity{}).Where("action_type = ?", "DEPOSIT").Count(&totalDeposits)
	log.Printf("总存款记录数: %d", totalDeposits)

	var totalBorrows int64
	db.Model(&models.LoanActivity{}).Where("action_type = ?", "BORROW").Count(&totalBorrows)
	log.Printf("总借款记录数: %d", totalBorrows)

	var activeUsers int64
	db.Model(&models.User{}).Count(&activeUsers)
	log.Printf("用户数: %d", activeUsers)

	fmt.Println("\n刷新页面查看效果!")
}
