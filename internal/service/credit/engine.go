package credit

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/creditlink/backend/internal/models"
	"github.com/creditlink/backend/internal/repository"
)

// BaseScore 所有用户的基础信用分
const BaseScore = 500

// TierConfig 定义每个信用等级对应的LTV和借款上限
var TierConfig = map[string]struct {
	MinScore  int
	LTV       int    // 基点 (10000 = 100%)
	MaxBorrow string // wei字符串
}{
	"S": {MinScore: 900, LTV: 9500, MaxBorrow: "500000000000000000000000"},  // $500k
	"A": {MinScore: 800, LTV: 9000, MaxBorrow: "200000000000000000000000"},  // $200k
	"B": {MinScore: 600, LTV: 8000, MaxBorrow: "50000000000000000000000"},   // $50k
	"C": {MinScore: 400, LTV: 7000, MaxBorrow: "10000000000000000000000"},   // $10k
	"D": {MinScore: 0, LTV: 0, MaxBorrow: "0"},
}

// CreditScore 表示用户的信用分和等级
type CreditScore struct {
	Score     int    `json:"score"`
	Tier      string `json:"tier"`
	MaxLTV    int    `json:"maxLtv"`    // 基点
	MaxBorrow string `json:"maxBorrow"` // wei字符串
}

// CreditFactors 表示影响信用分的各项因子
type CreditFactors struct {
	// 内部因子 (60%权重)
	RepaymentBonus     int `json:"repaymentBonus"`     // 还款奖励
	LiquidationPenalty int `json:"liquidationPenalty"` // 清算惩罚
	BorrowHistoryBonus int `json:"borrowHistoryBonus"` // 借款历史奖励
	LoyaltyBonus       int `json:"loyaltyBonus"`       // 忠诚度奖励
	HealthFactorBonus  int `json:"healthFactorBonus"`  // 健康因子奖励

	// 外部因子 (25%权重)
	WalletAgeBonus  int `json:"walletAgeBonus"`  // 钱包年龄奖励
	ActivityBonus   int `json:"activityBonus"`   // 活跃度奖励
	DiversityBonus  int `json:"diversityBonus"`  // 资产多样性奖励
	NetWorthBonus   int `json:"netWorthBonus"`   // 净资产奖励

	// 风险因子 (负分)
	BlacklistPenalty     int `json:"blacklistPenalty"`     // 黑名单惩罚
	HighFrequencyPenalty int `json:"highFrequencyPenalty"` // 高频交易惩罚
	AssetDeclinePenalty  int `json:"assetDeclinePenalty"`  // 资产急剧下降惩罚
	BotBehaviorPenalty   int `json:"botBehaviorPenalty"`   // 机器人行为惩罚
}

// Engine 信用评分引擎，负责计算用户信用分
type Engine struct {
	userRepo     *repository.UserRepository
	creditRepo   *repository.CreditRepository
	activityRepo *repository.ActivityRepository
}

// NewEngine 创建新的信用评分引擎实例
func NewEngine(
	userRepo *repository.UserRepository,
	creditRepo *repository.CreditRepository,
	activityRepo *repository.ActivityRepository,
) *Engine {
	return &Engine{
		userRepo:     userRepo,
		creditRepo:   creditRepo,
		activityRepo: activityRepo,
	}
}

// CalculateScore 计算用户的信用分
func (e *Engine) CalculateScore(ctx context.Context, walletAddress string) (*CreditScore, *CreditFactors, error) {
	walletAddress = strings.ToLower(walletAddress)

	// 获取或创建用户
	user, err := e.userRepo.GetOrCreate(ctx, walletAddress)
	if err != nil {
		return nil, nil, err
	}

	// 基础分
	score := BaseScore

	// 计算链上数据的内部因子
	internalFactors := e.calculateInternalFactors(ctx, user.ID)
	score += internalFactors.RepaymentBonus
	score += internalFactors.LiquidationPenalty // 负数
	score += internalFactors.BorrowHistoryBonus
	score += internalFactors.LoyaltyBonus
	score += internalFactors.HealthFactorBonus

	// 计算外部因子（占位符 - 将来集成DeBank/Etherscan）
	externalFactors := e.calculateExternalFactors(ctx, walletAddress)
	score += externalFactors.WalletAgeBonus
	score += externalFactors.ActivityBonus
	score += externalFactors.DiversityBonus
	score += externalFactors.NetWorthBonus

	// 计算风险因子
	riskFactors := e.calculateRiskFactors(ctx, user.ID)
	score += riskFactors.BlacklistPenalty     // 负数
	score += riskFactors.HighFrequencyPenalty // 负数
	score += riskFactors.AssetDeclinePenalty  // 负数
	score += riskFactors.BotBehaviorPenalty   // 负数

	// 对不活跃用户应用时间衰减
	score = e.applyTimeDecay(ctx, user.ID, score)

	// 将分数限制在0-1000之间
	score = clamp(score, 0, 1000)

	// 确定信用等级
	tier := determineTier(score)
	config := TierConfig[tier]

	// 合并所有因子
	factors := &CreditFactors{
		RepaymentBonus:       internalFactors.RepaymentBonus,
		LiquidationPenalty:   internalFactors.LiquidationPenalty,
		BorrowHistoryBonus:   internalFactors.BorrowHistoryBonus,
		LoyaltyBonus:         internalFactors.LoyaltyBonus,
		HealthFactorBonus:    internalFactors.HealthFactorBonus,
		WalletAgeBonus:       externalFactors.WalletAgeBonus,
		ActivityBonus:        externalFactors.ActivityBonus,
		DiversityBonus:       externalFactors.DiversityBonus,
		NetWorthBonus:        externalFactors.NetWorthBonus,
		BlacklistPenalty:     riskFactors.BlacklistPenalty,
		HighFrequencyPenalty: riskFactors.HighFrequencyPenalty,
		AssetDeclinePenalty:  riskFactors.AssetDeclinePenalty,
		BotBehaviorPenalty:   riskFactors.BotBehaviorPenalty,
	}

	return &CreditScore{
		Score:     score,
		Tier:      tier,
		MaxLTV:    config.LTV,
		MaxBorrow: config.MaxBorrow,
	}, factors, nil
}

// calculateInternalFactors 根据链上历史计算内部因子
func (e *Engine) calculateInternalFactors(ctx context.Context, userID uint) CreditFactors {
	factors := CreditFactors{}

	// 还款奖励: 每次成功还款+10分 (最高+100)
	repayments, err := e.activityRepo.GetRepaymentsByUser(ctx, userID)
	if err == nil && len(repayments) > 0 {
		bonus := len(repayments) * 10
		factors.RepaymentBonus = min(bonus, 100)
	}

	// 清算惩罚: 每次清算-100分
	liquidations, err := e.activityRepo.GetLiquidationsByUser(ctx, userID)
	if err == nil && len(liquidations) > 0 {
		factors.LiquidationPenalty = -100 * len(liquidations)
	}

	// 借款历史奖励: 每完成一次正常借款周期+5分 (最高+50)
	borrowCount, err := e.activityRepo.CountByUserAndType(ctx, userID, models.ActionBorrow)
	if err == nil && borrowCount > 0 {
		bonus := int(borrowCount) * 5
		factors.BorrowHistoryBonus = min(bonus, 50)
	}

	// 忠诚度奖励: 使用协议超过90天+30分
	firstActivity, err := e.activityRepo.GetFirstActivityByUser(ctx, userID)
	if err == nil {
		daysSinceFirst := time.Since(firstActivity.BlockTimestamp).Hours() / 24
		if daysSinceFirst >= 90 {
			factors.LoyaltyBonus = 30
		} else if daysSinceFirst >= 30 {
			factors.LoyaltyBonus = 10
		}
	}

	// 健康因子奖励: 保持良好健康因子+20分 (占位符)
	// 需要查询借贷池合约获取实际数据
	factors.HealthFactorBonus = 0

	return factors
}

// calculateExternalFactors 从外部数据源计算外部因子
func (e *Engine) calculateExternalFactors(ctx context.Context, wallet string) CreditFactors {
	factors := CreditFactors{}

	// 将来会集成DeBank、Etherscan等外部API
	// 目前使用基于首次活动时间的占位符值

	// 钱包年龄奖励: >1年+40分, >6个月+20分, >3个月+10分
	// 这是占位符 - 将来会从Etherscan查询首笔交易
	factors.WalletAgeBonus = 10 // 新钱包默认值

	// 活跃度奖励: 高活跃度+20分
	factors.ActivityBonus = 0

	// 多样性奖励: 持有超过5种代币+30分
	factors.DiversityBonus = 0

	// 净资产奖励: >$100k+40分, >$10k+20分
	factors.NetWorthBonus = 0

	return factors
}

// calculateRiskFactors 计算负面风险因子
func (e *Engine) calculateRiskFactors(ctx context.Context, userID uint) CreditFactors {
	factors := CreditFactors{}

	// 高频惩罚: 24小时内超过3次借款-30分
	since24h := time.Now().Add(-24 * time.Hour)
	recentBorrows, err := e.activityRepo.CountRecentByUserAndType(ctx, userID, models.ActionBorrow, since24h)
	if err == nil && recentBorrows > 3 {
		factors.HighFrequencyPenalty = -30
	}

	// 黑名单惩罚: 每与已知恶意地址交互一次-50分
	// 需要黑名单数据库支持
	factors.BlacklistPenalty = 0

	// 资产下降惩罚: 7天内资产下降超过50%则-40分
	// 需要历史资产追踪支持
	factors.AssetDeclinePenalty = 0

	// 机器人行为惩罚: 检测到可疑模式-100分
	factors.BotBehaviorPenalty = 0

	return factors
}

// applyTimeDecay 对不活跃用户应用信用分衰减
func (e *Engine) applyTimeDecay(ctx context.Context, userID uint, score int) int {
	lastActivity, err := e.activityRepo.GetLastActivityByUser(ctx, userID)
	if err != nil {
		return score // 无活动记录，不衰减
	}

	daysSinceActivity := time.Since(lastActivity.BlockTimestamp).Hours() / 24
	if daysSinceActivity <= 30 {
		return score // 30天内不衰减
	}

	// 每30天不活跃衰减5%
	decayMonths := int(daysSinceActivity / 30)
	decayRate := math.Pow(0.95, float64(decayMonths))
	return int(float64(score) * decayRate)
}

// SaveScore 将计算的信用分保存到数据库
func (e *Engine) SaveScore(ctx context.Context, walletAddress string, score *CreditScore, factors *CreditFactors) error {
	walletAddress = strings.ToLower(walletAddress)

	user, err := e.userRepo.GetOrCreate(ctx, walletAddress)
	if err != nil {
		return err
	}

	// 检查是否需要记录历史（分数变化时）
	var shouldRecordHistory bool
	var eventType string = "SCORE_CALCULATED"

	oldCredit, err := e.creditRepo.GetByUserID(ctx, user.ID)
	if err != nil {
		// 首次计算，记录初始分数
		shouldRecordHistory = true
		eventType = "INITIAL_SCORE"
	} else if oldCredit.Score != score.Score {
		// 分数变化，记录历史
		shouldRecordHistory = true
		if score.Score > oldCredit.Score {
			eventType = "SCORE_INCREASE"
		} else {
			eventType = "SCORE_DECREASE"
		}
	}

	// 保存信用分
	credit := &models.UserCredit{
		UserID:         user.ID,
		Score:          score.Score,
		Tier:           score.Tier,
		MaxLTV:         score.MaxLTV,
		MaxBorrowLimit: score.MaxBorrow,
	}
	if err := e.creditRepo.CreateOrUpdate(ctx, credit); err != nil {
		return err
	}

	// 记录分数历史
	if shouldRecordHistory {
		history := &models.CreditScoreHistory{
			UserID:    user.ID,
			Score:     score.Score,
			Tier:      score.Tier,
			EventType: eventType,
		}
		// 忽略历史记录错误，不影响主流程
		_ = e.creditRepo.AddScoreHistory(ctx, history)
	}

	// 保存各项因子
	factorModels := []models.CreditFactor{
		{FactorKey: "repayment_bonus", FactorValue: float64(factors.RepaymentBonus), Contribution: factors.RepaymentBonus},
		{FactorKey: "liquidation_penalty", FactorValue: float64(factors.LiquidationPenalty), Contribution: factors.LiquidationPenalty},
		{FactorKey: "borrow_history_bonus", FactorValue: float64(factors.BorrowHistoryBonus), Contribution: factors.BorrowHistoryBonus},
		{FactorKey: "loyalty_bonus", FactorValue: float64(factors.LoyaltyBonus), Contribution: factors.LoyaltyBonus},
		{FactorKey: "health_factor_bonus", FactorValue: float64(factors.HealthFactorBonus), Contribution: factors.HealthFactorBonus},
		{FactorKey: "wallet_age_bonus", FactorValue: float64(factors.WalletAgeBonus), Contribution: factors.WalletAgeBonus},
		{FactorKey: "activity_bonus", FactorValue: float64(factors.ActivityBonus), Contribution: factors.ActivityBonus},
		{FactorKey: "diversity_bonus", FactorValue: float64(factors.DiversityBonus), Contribution: factors.DiversityBonus},
		{FactorKey: "net_worth_bonus", FactorValue: float64(factors.NetWorthBonus), Contribution: factors.NetWorthBonus},
		{FactorKey: "blacklist_penalty", FactorValue: float64(factors.BlacklistPenalty), Contribution: factors.BlacklistPenalty},
		{FactorKey: "high_frequency_penalty", FactorValue: float64(factors.HighFrequencyPenalty), Contribution: factors.HighFrequencyPenalty},
		{FactorKey: "asset_decline_penalty", FactorValue: float64(factors.AssetDeclinePenalty), Contribution: factors.AssetDeclinePenalty},
		{FactorKey: "bot_behavior_penalty", FactorValue: float64(factors.BotBehaviorPenalty), Contribution: factors.BotBehaviorPenalty},
	}

	return e.creditRepo.SaveFactors(ctx, user.ID, factorModels)
}

// GetCachedScore 从数据库获取缓存的信用分
func (e *Engine) GetCachedScore(ctx context.Context, walletAddress string) (*CreditScore, error) {
	walletAddress = strings.ToLower(walletAddress)

	user, err := e.userRepo.GetByWallet(ctx, walletAddress)
	if err != nil {
		return nil, err
	}

	credit, err := e.creditRepo.GetByUserID(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	return &CreditScore{
		Score:     credit.Score,
		Tier:      credit.Tier,
		MaxLTV:    credit.MaxLTV,
		MaxBorrow: credit.MaxBorrowLimit,
	}, nil
}

// GetScoreWithFactors 获取信用分及所有贡献因子
func (e *Engine) GetScoreWithFactors(ctx context.Context, walletAddress string) (*CreditScore, []models.CreditFactor, error) {
	walletAddress = strings.ToLower(walletAddress)

	user, err := e.userRepo.GetByWallet(ctx, walletAddress)
	if err != nil {
		return nil, nil, err
	}

	credit, err := e.creditRepo.GetByUserID(ctx, user.ID)
	if err != nil {
		return nil, nil, err
	}

	factors, err := e.creditRepo.GetFactors(ctx, user.ID)
	if err != nil {
		return nil, nil, err
	}

	return &CreditScore{
		Score:     credit.Score,
		Tier:      credit.Tier,
		MaxLTV:    credit.MaxLTV,
		MaxBorrow: credit.MaxBorrowLimit,
	}, factors, nil
}

// determineTier 根据信用分确定信用等级
func determineTier(score int) string {
	switch {
	case score >= 900:
		return "S"
	case score >= 800:
		return "A"
	case score >= 600:
		return "B"
	case score >= 400:
		return "C"
	default:
		return "D"
	}
}

// clamp 将值限制在指定范围内
func clamp(value, minVal, maxVal int) int {
	if value < minVal {
		return minVal
	}
	if value > maxVal {
		return maxVal
	}
	return value
}

// min 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
