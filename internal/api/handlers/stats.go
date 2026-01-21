package handlers

import (
	"math/big"
	"net/http"

	"github.com/creditlink/backend/internal/repository"
	"github.com/gin-gonic/gin"
)

// StatsHandler 平台统计处理器
type StatsHandler struct {
	activityRepo *repository.ActivityRepository
	userRepo     *repository.UserRepository
	riskRepo     *repository.RiskRepository
}

// NewStatsHandler 创建统计处理器
func NewStatsHandler(
	activityRepo *repository.ActivityRepository,
	userRepo *repository.UserRepository,
	riskRepo *repository.RiskRepository,
) *StatsHandler {
	return &StatsHandler{
		activityRepo: activityRepo,
		userRepo:     userRepo,
		riskRepo:     riskRepo,
	}
}

// PlatformStats 平台统计数据
type PlatformStats struct {
	TotalValueLocked string `json:"totalValueLocked"` // 总锁仓价值 (USD)
	TotalDeposits    string `json:"totalDeposits"`    // 总存款 (USD)
	TotalBorrows     string `json:"totalBorrows"`     // 总借款 (USD)
	ActiveUsers      int64  `json:"activeUsers"`      // 活跃用户数
}

// MarketData 市场数据
type MarketData struct {
	Symbol         string `json:"symbol"`
	Name           string `json:"name"`
	TotalSupply    string `json:"totalSupply"`    // 总存款量
	TotalBorrow    string `json:"totalBorrow"`    // 总借款量
	SupplyAPY      string `json:"supplyAPY"`      // 存款年化收益率
	BorrowAPR      string `json:"borrowAPR"`      // 借款年化利率
	LTV            int    `json:"ltv"`            // 贷款价值比
	LiquidationLTV int    `json:"liquidationLtv"` // 清算阈值
	UtilizationRate string `json:"utilizationRate"` // 资金利用率
}

// GetPlatformStats 获取平台统计数据
// @Summary 获取平台统计
// @Description 获取平台总锁仓价值、存款、借款和活跃用户数
// @Tags stats
// @Produce json
// @Success 200 {object} PlatformStats
// @Router /stats/platform [get]
func (h *StatsHandler) GetPlatformStats(c *gin.Context) {
	ctx := c.Request.Context()

	// 获取活跃用户数
	activeUsers, err := h.userRepo.CountActiveUsers(ctx)
	if err != nil {
		activeUsers = 0
	}

	// 获取风险参数（包含资产decimals）
	riskParams, _ := h.riskRepo.GetAllRiskParams(ctx)
	assetDecimals := make(map[string]int)
	for _, p := range riskParams {
		assetDecimals[p.AssetAddress] = int(p.Decimals)
	}

	// 获取存款和借款统计
	depositStats, err := h.activityRepo.GetTotalsByActionType(ctx, "DEPOSIT")
	if err != nil {
		depositStats = make(map[string]*big.Int)
	}

	borrowStats, err := h.activityRepo.GetTotalsByActionType(ctx, "BORROW")
	if err != nil {
		borrowStats = make(map[string]*big.Int)
	}

	withdrawStats, err := h.activityRepo.GetTotalsByActionType(ctx, "WITHDRAW")
	if err != nil {
		withdrawStats = make(map[string]*big.Int)
	}

	repayStats, err := h.activityRepo.GetTotalsByActionType(ctx, "REPAY")
	if err != nil {
		repayStats = make(map[string]*big.Int)
	}

	// 计算净存款和净借款(转换为USD)
	// 净存款 = 总存款 - 总提款
	// 净借款 = 总借款 - 总还款
	totalDepositsUSD := big.NewFloat(0)
	totalBorrowsUSD := big.NewFloat(0)

	for asset, amount := range depositStats {
		withdrawn := withdrawStats[asset]
		if withdrawn == nil {
			withdrawn = big.NewInt(0)
		}
		net := new(big.Int).Sub(amount, withdrawn)
		if net.Sign() > 0 {
			usd := convertToUSD(net, assetDecimals[asset], asset)
			totalDepositsUSD.Add(totalDepositsUSD, usd)
		}
	}

	for asset, amount := range borrowStats {
		repaid := repayStats[asset]
		if repaid == nil {
			repaid = big.NewInt(0)
		}
		net := new(big.Int).Sub(amount, repaid)
		if net.Sign() > 0 {
			usd := convertToUSD(net, assetDecimals[asset], asset)
			totalBorrowsUSD.Add(totalBorrowsUSD, usd)
		}
	}

	// TVL = 净存款 - 净借款
	tvlUSD := new(big.Float).Sub(totalDepositsUSD, totalBorrowsUSD)
	if tvlUSD.Sign() < 0 {
		tvlUSD = big.NewFloat(0)
	}

	stats := PlatformStats{
		TotalValueLocked: formatUSDFloat(tvlUSD),
		TotalDeposits:    formatUSDFloat(totalDepositsUSD),
		TotalBorrows:     formatUSDFloat(totalBorrowsUSD),
		ActiveUsers:      activeUsers,
	}

	c.JSON(http.StatusOK, stats)
}

// GetMarketsData 获取所有市场数据
// @Summary 获取市场数据
// @Description 获取所有支持资产的市场数据
// @Tags stats
// @Produce json
// @Success 200 {array} MarketData
// @Router /stats/markets [get]
func (h *StatsHandler) GetMarketsData(c *gin.Context) {
	ctx := c.Request.Context()

	// 获取所有风险参数
	riskParams, err := h.riskRepo.GetAllRiskParams(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取风险参数失败"})
		return
	}

	// 获取各资产的存借款统计
	depositStats, _ := h.activityRepo.GetTotalsByActionType(ctx, "DEPOSIT")
	borrowStats, _ := h.activityRepo.GetTotalsByActionType(ctx, "BORROW")
	withdrawStats, _ := h.activityRepo.GetTotalsByActionType(ctx, "WITHDRAW")
	repayStats, _ := h.activityRepo.GetTotalsByActionType(ctx, "REPAY")

	markets := make([]MarketData, 0, len(riskParams))

	for _, params := range riskParams {
		// 计算净存款和净借款
		deposited := depositStats[params.AssetAddress]
		if deposited == nil {
			deposited = big.NewInt(0)
		}
		withdrawn := withdrawStats[params.AssetAddress]
		if withdrawn == nil {
			withdrawn = big.NewInt(0)
		}
		netDeposit := new(big.Int).Sub(deposited, withdrawn)
		if netDeposit.Sign() < 0 {
			netDeposit = big.NewInt(0)
		}

		borrowed := borrowStats[params.AssetAddress]
		if borrowed == nil {
			borrowed = big.NewInt(0)
		}
		repaid := repayStats[params.AssetAddress]
		if repaid == nil {
			repaid = big.NewInt(0)
		}
		netBorrow := new(big.Int).Sub(borrowed, repaid)
		if netBorrow.Sign() < 0 {
			netBorrow = big.NewInt(0)
		}

		// 计算资金利用率
		utilizationRate := "0"
		if netDeposit.Sign() > 0 {
			rate := new(big.Int).Mul(netBorrow, big.NewInt(10000))
			rate.Div(rate, netDeposit)
			utilizationRate = formatPercent(rate.Int64())
		}

		// 根据利用率计算 APY/APR (简化的利率模型)
		supplyAPY, borrowAPR := calculateRates(utilizationRate)

		markets = append(markets, MarketData{
			Symbol:          params.AssetSymbol,
			Name:            getAssetName(params.AssetSymbol),
			TotalSupply:     formatTokenAmount(netDeposit, int(params.Decimals)),
			TotalBorrow:     formatTokenAmount(netBorrow, int(params.Decimals)),
			SupplyAPY:       supplyAPY,
			BorrowAPR:       borrowAPR,
			LTV:             params.BaseLTV,
			LiquidationLTV:  params.LiquidationThreshold,
			UtilizationRate: utilizationRate,
		})
	}

	c.JSON(http.StatusOK, gin.H{"markets": markets})
}

// 辅助函数：将资产数量转换为 USD
// 使用简化的价格假设：USDT/USDC=$1, WETH=$2500, WBTC=$45000
func convertToUSD(amount *big.Int, decimals int, assetAddress string) *big.Float {
	if amount == nil || amount.Sign() == 0 || decimals == 0 {
		return big.NewFloat(0)
	}

	// 获取资产价格 (简化：从地址推断资产类型)
	price := 1.0
	// 根据地址前缀判断资产类型（简化处理）
	if len(assetAddress) > 3 {
		prefix := assetAddress[2:3] // 0x后的第一个字符
		switch prefix {
		case "3": // WETH
			price = 2500.0
		case "4": // WBTC
			price = 45000.0
		default: // USDT, USDC
			price = 1.0
		}
	}

	// 计算: (amount / 10^decimals) * price
	amountFloat := new(big.Float).SetInt(amount)
	divisor := new(big.Float).SetFloat64(float64(pow10(decimals)))
	normalized := new(big.Float).Quo(amountFloat, divisor)
	return new(big.Float).Mul(normalized, big.NewFloat(price))
}

// 辅助函数：计算10的n次方
func pow10(n int) int64 {
	result := int64(1)
	for i := 0; i < n; i++ {
		result *= 10
	}
	return result
}

// 辅助函数：格式化 big.Float 为 USD 字符串
func formatUSDFloat(amount *big.Float) string {
	if amount == nil || amount.Sign() == 0 {
		return "0"
	}
	f, _ := amount.Float64()
	return big.NewFloat(f).Text('f', 2)
}

// 辅助函数：格式化为 USD 字符串 (遗留函数)
func formatUSD(amount *big.Int) string {
	if amount == nil || amount.Sign() == 0 {
		return "0"
	}
	// 假设 6 位小数
	divisor := big.NewInt(1000000)
	usd := new(big.Int).Div(amount, divisor)
	return usd.String()
}

// 辅助函数：格式化代币数量
func formatTokenAmount(amount *big.Int, decimals int) string {
	if amount == nil || amount.Sign() == 0 {
		return "0"
	}
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	result := new(big.Int).Div(amount, divisor)
	return result.String()
}

// 辅助函数：格式化百分比
func formatPercent(bps int64) string {
	// bps 是基点 (10000 = 100%)
	return big.NewFloat(float64(bps) / 100).String()
}

// 辅助函数：获取资产名称
func getAssetName(symbol string) string {
	names := map[string]string{
		"USDT": "Tether USD",
		"USDC": "USD Coin",
		"WETH": "Wrapped Ether",
		"WBTC": "Wrapped Bitcoin",
	}
	if name, ok := names[symbol]; ok {
		return name
	}
	return symbol
}

// 辅助函数：根据利用率计算利率 (简化的利率曲线)
func calculateRates(utilizationRateStr string) (supplyAPY, borrowAPR string) {
	// 基础利率模型:
	// - 基础借款利率: 2%
	// - 斜率: 利用率每增加1%，借款利率增加0.1%
	// - 存款 APY = 借款 APR * 利用率 * (1 - 储备金率)

	// 默认值
	baseRate := 2.0      // 基础利率 2%
	slope := 0.1         // 斜率
	reserveFactor := 0.1 // 储备金率 10%

	// 解析利用率
	var utilRate float64
	utilFloat, ok := new(big.Float).SetString(utilizationRateStr)
	if ok {
		utilRate, _ = utilFloat.Float64()
		utilRate = utilRate / 100 // 转换为小数
	}

	// 计算借款 APR
	borrowRate := baseRate + (utilRate * slope * 100)

	// 计算存款 APY
	supplyRate := borrowRate * utilRate * (1 - reserveFactor)

	return formatRate(supplyRate), formatRate(borrowRate)
}

// 辅助函数：格式化利率
func formatRate(rate float64) string {
	return big.NewFloat(rate).Text('f', 2)
}
