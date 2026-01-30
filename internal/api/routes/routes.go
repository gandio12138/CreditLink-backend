package routes

import (
	"github.com/creditlink/backend/internal/api/handlers"
	"github.com/creditlink/backend/internal/api/middleware"
	"github.com/creditlink/backend/internal/repository"
	"github.com/creditlink/backend/internal/service/credit"
	"github.com/creditlink/backend/internal/service/price"
	"github.com/creditlink/backend/internal/service/signer"
	"github.com/gin-gonic/gin"
)

// Dependencies 存储所有处理器所需的依赖
type Dependencies struct {
	UserRepo      *repository.UserRepository      // 用户仓储
	CreditRepo    *repository.CreditRepository    // 信用仓储
	ActivityRepo  *repository.ActivityRepository  // 活动记录仓储
	SignatureRepo *repository.SignatureRepository // 签名仓储
	RiskRepo      *repository.RiskRepository      // 风险参数仓储

	CreditEngine  *credit.Engine   // 信用评分引擎
	SignerService *signer.Service  // 签名服务
	PriceService  *price.Service   // 价格服务
}

// SetupRoutes 配置所有API路由
func SetupRoutes(r *gin.Engine, deps *Dependencies) {
	// 健康检查接口
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// API v1 版本
	v1 := r.Group("/api/v1")
	{
		// 认证接口 (公开)
		authHandler := handlers.NewAuthHandler(deps.UserRepo)
		v1.GET("/auth/nonce", authHandler.GetNonce)   // 获取登录nonce
		v1.POST("/auth/login", authHandler.Login)     // 登录

		// 风险参数接口 (公开)
		riskHandler := handlers.NewRiskHandler(deps.RiskRepo)
		v1.GET("/risk/params", riskHandler.GetRiskParams)           // 获取所有风险参数
		v1.GET("/risk/params/:asset", riskHandler.GetAssetRiskParams) // 获取特定资产风险参数

		// 统计接口 (公开)
		statsHandler := handlers.NewStatsHandler(deps.ActivityRepo, deps.UserRepo, deps.RiskRepo)
		v1.GET("/stats/platform", statsHandler.GetPlatformStats) // 获取平台统计
		v1.GET("/stats/markets", statsHandler.GetMarketsData)    // 获取市场数据

		// 受保护路由 (需要JWT认证)
		protected := v1.Group("")
		protected.Use(middleware.JWTAuth())
		{
			// 信用相关接口
			creditHandler := handlers.NewCreditHandler(deps.CreditEngine, deps.SignerService, deps.CreditRepo, deps.UserRepo)
			protected.POST("/credit/sign", creditHandler.Sign)              // 请求信用借款签名
			protected.GET("/user/credit", creditHandler.GetCredit)          // 获取用户信用信息
			protected.POST("/user/credit/refresh", creditHandler.RefreshCredit) // 刷新信用分

			// 用户相关接口
			userHandler := handlers.NewUserHandler(deps.UserRepo, deps.ActivityRepo, deps.RiskRepo, deps.CreditEngine, deps.PriceService)
			protected.GET("/user/account", userHandler.GetAccount)       // 获取账户数据
			protected.GET("/user/positions", userHandler.GetPositions)   // 获取持仓数据
			protected.GET("/user/activities", userHandler.GetActivities) // 获取活动记录
		}
	}

	// WebSocket接口
	ws := r.Group("/api/v1/ws")
	{
		wsHandler := handlers.NewWSHandler()
		ws.GET("/health-alert", wsHandler.HealthAlert) // 健康因子预警WebSocket
	}
}
