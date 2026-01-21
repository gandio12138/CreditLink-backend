package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/creditlink/backend/internal/api/middleware"
	"github.com/creditlink/backend/internal/api/routes"
	"github.com/creditlink/backend/internal/config"
	"github.com/creditlink/backend/internal/database"
	"github.com/creditlink/backend/internal/indexer"
	"github.com/creditlink/backend/internal/repository"
	"github.com/creditlink/backend/internal/service/credit"
	"github.com/creditlink/backend/internal/service/signer"
	"github.com/gin-gonic/gin"
)

func main() {
	// 加载配置
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	log.Printf("CreditLink 后端服务启动中，端口: %s...", cfg.Server.Port)

	// 初始化数据库连接
	db, err := database.Connect(&cfg.Database)
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}

	// 运行数据库迁移
	if err := database.AutoMigrate(db); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}

	// 初始化 JWT
	middleware.InitJWT(&middleware.JWTConfig{
		Secret:     cfg.JWT.Secret,
		ExpireHour: cfg.JWT.ExpireHour,
	})

	// 初始化数据仓库
	userRepo := repository.NewUserRepository(db)
	creditRepo := repository.NewCreditRepository(db)
	activityRepo := repository.NewActivityRepository(db)
	signatureRepo := repository.NewSignatureRepository(db)
	riskRepo := repository.NewRiskRepository(db)

	// 初始化服务
	creditEngine := credit.NewEngine(userRepo, creditRepo, activityRepo)

	var signerService *signer.Service
	if cfg.Signer.PrivateKey != "" {
		signerService, err = signer.NewService(
			&signer.Config{
				PrivateKeyHex:    cfg.Signer.PrivateKey,
				ChainID:          cfg.Chain.ChainID,
				LendingPoolAddr:  cfg.Chain.LendingPool,
				SignatureTTL:     cfg.Signer.SignatureTTL,
				RateLimitPerUser: cfg.Signer.RateLimitPerUser,
				RateLimitPerIP:   cfg.Signer.RateLimitPerIP,
			},
			creditEngine,
			signatureRepo,
			userRepo,
		)
		if err != nil {
			log.Printf("警告: 初始化签名服务失败: %v", err)
		} else {
			log.Printf("签名服务已初始化，签名者地址: %s", signerService.GetSignerAddress().Hex())
		}
	} else {
		log.Println("警告: 未配置签名私钥，签名接口将无法使用")
	}

	// 初始化 Gin
	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()

	// CORS 跨域中间件
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	})

	// 设置路由
	deps := &routes.Dependencies{
		UserRepo:      userRepo,
		CreditRepo:    creditRepo,
		ActivityRepo:  activityRepo,
		SignatureRepo: signatureRepo,
		RiskRepo:      riskRepo,
		CreditEngine:  creditEngine,
		SignerService: signerService,
	}
	routes.SetupRoutes(r, deps)

	// 创建 HTTP 服务器
	srv := &http.Server{
		Addr:    ":" + cfg.Server.Port,
		Handler: r,
	}

	// 在后台启动索引器
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if cfg.Chain.RPCURL != "" && cfg.Chain.LendingPool != "" {
		idxr, err := indexer.NewIndexer(
			&indexer.Config{
				RPCURL:         cfg.Chain.RPCURL,
				ChainID:        cfg.Chain.ChainID,
				LendingPool:    cfg.Chain.LendingPool,
				StartBlock:     cfg.Chain.StartBlock,
				BlockBatchSize: 10, // Alchemy 免费版限制最大 10 个区块
				PollInterval:   12 * time.Second,
			},
			userRepo,
			activityRepo,
			riskRepo,
		)
		if err != nil {
			log.Printf("警告: 初始化索引器失败: %v", err)
		} else {
			go func() {
				if err := idxr.Start(ctx); err != nil {
					log.Printf("索引器错误: %v", err)
				}
			}()
			log.Println("索引器已启动")
		}
	}

	// 在 goroutine 中启动服务器
	go func() {
		log.Printf("服务器监听地址: http://localhost:%s", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("启动服务器失败: %v", err)
		}
	}()

	// 优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("正在关闭服务器...")

	// 给未完成的请求 5 秒时间完成
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("服务器被强制关闭: %v", err)
	}

	// 关闭数据库连接
	if err := database.Close(); err != nil {
		log.Printf("关闭数据库连接出错: %v", err)
	}

	log.Println("服务器已退出")
}
