package handlers

import (
	"errors"
	"math/big"
	"net/http"
	"strings"

	"github.com/creditlink/backend/internal/repository"
	"github.com/creditlink/backend/internal/service/credit"
	"github.com/creditlink/backend/internal/service/price"
	"github.com/gin-gonic/gin"
)

// UserHandler handles user-related API requests
type UserHandler struct {
	userRepo     *repository.UserRepository
	activityRepo *repository.ActivityRepository
	riskRepo     *repository.RiskRepository
	creditEngine *credit.Engine
	priceService *price.Service
}

// NewUserHandler creates a new user handler
func NewUserHandler(
	userRepo *repository.UserRepository,
	activityRepo *repository.ActivityRepository,
	riskRepo *repository.RiskRepository,
	creditEngine *credit.Engine,
	priceService *price.Service,
) *UserHandler {
	return &UserHandler{
		userRepo:     userRepo,
		activityRepo: activityRepo,
		riskRepo:     riskRepo,
		creditEngine: creditEngine,
		priceService: priceService,
	}
}

// AccountResponse represents user account data
type AccountResponse struct {
	TotalCollateralUSD string `json:"totalCollateralUSD"`
	TotalDebtUSD       string `json:"totalDebtUSD"`
	AvailableBorrowUSD string `json:"availableBorrowUSD"`
	CurrentLTV         int    `json:"currentLtv"`
	HealthFactor       string `json:"healthFactor"`
}

// PositionResponse represents user positions
type PositionResponse struct {
	Deposits []PositionItem `json:"deposits"`
	Borrows  []PositionItem `json:"borrows"`
}

// PositionItem represents a single position
type PositionItem struct {
	Asset    string `json:"asset"`
	Symbol   string `json:"symbol"`
	Amount   string `json:"amount"`
	ValueUSD string `json:"valueUsd"`
}

// ActivityResponse represents a loan activity
type ActivityResponse struct {
	TxHash     string `json:"txHash"`
	ActionType string `json:"actionType"`
	Asset      string `json:"asset"`
	Symbol     string `json:"symbol"`
	Amount     string `json:"amount"`
	Timestamp  int64  `json:"timestamp"`
}

// GetAccount handles GET /api/v1/user/account
func (h *UserHandler) GetAccount(c *gin.Context) {
	// Get wallet address from context
	walletAddr, exists := c.Get("walletAddress")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	wallet := walletAddr.(string)

	// Get user
	user, err := h.userRepo.GetByWallet(c.Request.Context(), wallet)
	if err != nil {
		if err == repository.ErrUserNotFound {
			// Return empty account for new users
			c.JSON(http.StatusOK, AccountResponse{
				TotalCollateralUSD: "0",
				TotalDebtUSD:       "0",
				AvailableBorrowUSD: "0",
				CurrentLTV:         0,
				HealthFactor:       "0",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Get user positions
	positions, err := h.riskRepo.GetUserPositions(c.Request.Context(), user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Get all risk params for LTV calculation
	riskParams, err := h.riskRepo.GetAllRiskParams(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Build risk params map
	riskParamsMap := make(map[string]*struct {
		BaseLTV              int
		LiquidationThreshold int
	})
	for i := range riskParams {
		riskParamsMap[strings.ToLower(riskParams[i].AssetAddress)] = &struct {
			BaseLTV              int
			LiquidationThreshold int
		}{
			BaseLTV:              riskParams[i].BaseLTV,
			LiquidationThreshold: riskParams[i].LiquidationThreshold,
		}
	}

	// Calculate totals using price service
	totalCollateralUSD := big.NewFloat(0)
	totalDebtUSD := big.NewFloat(0)
	weightedCollateral := big.NewFloat(0) // Collateral * Liquidation Threshold

	for _, pos := range positions {
		params := riskParamsMap[strings.ToLower(pos.AssetAddress)]

		// Calculate deposit value
		depositAmount, _ := new(big.Int).SetString(pos.DepositAmount, 10)
		if depositAmount != nil && depositAmount.Sign() > 0 {
			depositUSD, err := h.priceService.ConvertToUSD(c.Request.Context(), depositAmount, pos.AssetAddress)
			if err != nil {
				respondPriceError(c, err)
				return
			}
			totalCollateralUSD.Add(totalCollateralUSD, depositUSD)

			// Apply liquidation threshold for health factor
			if params != nil {
				threshold := big.NewFloat(float64(params.LiquidationThreshold) / 10000)
				weighted := new(big.Float).Mul(depositUSD, threshold)
				weightedCollateral.Add(weightedCollateral, weighted)
			}
		}

		// Calculate borrow value
		borrowAmount, _ := new(big.Int).SetString(pos.BorrowAmount, 10)
		if borrowAmount != nil && borrowAmount.Sign() > 0 {
			borrowUSD, err := h.priceService.ConvertToUSD(c.Request.Context(), borrowAmount, pos.AssetAddress)
			if err != nil {
				respondPriceError(c, err)
				return
			}
			totalDebtUSD.Add(totalDebtUSD, borrowUSD)
		}
	}

	// Calculate health factor: weightedCollateral / totalDebt
	healthFactor := "0" // 0 means infinite (no debt)
	if totalDebtUSD.Sign() > 0 {
		hf := new(big.Float).Quo(weightedCollateral, totalDebtUSD)
		healthFactor = hf.Text('f', 2)
	}

	// Calculate current LTV: (totalDebt / totalCollateral) * 10000
	currentLTV := 0
	if totalCollateralUSD.Sign() > 0 {
		ltvFloat := new(big.Float).Quo(totalDebtUSD, totalCollateralUSD)
		ltvFloat.Mul(ltvFloat, big.NewFloat(10000))
		ltvInt, _ := ltvFloat.Int64()
		currentLTV = int(ltvInt)
	}

	// Get user's max LTV from credit score
	maxLTV := 7000 // Default 70%
	if h.creditEngine != nil {
		score, _, err := h.creditEngine.CalculateScore(c.Request.Context(), wallet)
		if err == nil && score != nil {
			maxLTV = score.MaxLTV
		}
	}

	// Calculate available borrow: (totalCollateral * maxLTV / 10000) - totalDebt
	availableBorrowUSD := big.NewFloat(0)
	if totalCollateralUSD.Sign() > 0 {
		maxBorrow := new(big.Float).Mul(totalCollateralUSD, big.NewFloat(float64(maxLTV)/10000))
		availableBorrowUSD = new(big.Float).Sub(maxBorrow, totalDebtUSD)
		if availableBorrowUSD.Sign() < 0 {
			availableBorrowUSD = big.NewFloat(0)
		}
	}

	c.JSON(http.StatusOK, AccountResponse{
		TotalCollateralUSD: price.FormatUSD(totalCollateralUSD),
		TotalDebtUSD:       price.FormatUSD(totalDebtUSD),
		AvailableBorrowUSD: price.FormatUSD(availableBorrowUSD),
		CurrentLTV:         currentLTV,
		HealthFactor:       healthFactor,
	})
}

// GetPositions handles GET /api/v1/user/positions
func (h *UserHandler) GetPositions(c *gin.Context) {
	// Get wallet address from context
	walletAddr, exists := c.Get("walletAddress")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	wallet := walletAddr.(string)

	// Get user
	user, err := h.userRepo.GetByWallet(c.Request.Context(), wallet)
	if err != nil {
		if err == repository.ErrUserNotFound {
			c.JSON(http.StatusOK, PositionResponse{
				Deposits: []PositionItem{},
				Borrows:  []PositionItem{},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Get user positions
	positions, err := h.riskRepo.GetUserPositions(c.Request.Context(), user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	deposits := make([]PositionItem, 0)
	borrows := make([]PositionItem, 0)

	for _, p := range positions {
		symbol := h.priceService.GetSymbol(p.AssetAddress)
		if symbol == "" {
			symbol = "UNKNOWN"
		}

		if p.DepositAmount != "0" {
			depositAmount, _ := new(big.Int).SetString(p.DepositAmount, 10)
			valueUSD, err := h.priceService.ConvertToUSD(c.Request.Context(), depositAmount, p.AssetAddress)
			if err != nil {
				respondPriceError(c, err)
				return
			}

			deposits = append(deposits, PositionItem{
				Asset:    p.AssetAddress,
				Symbol:   symbol,
				Amount:   p.DepositAmount,
				ValueUSD: price.FormatUSD(valueUSD),
			})
		}

		if p.BorrowAmount != "0" {
			borrowAmount, _ := new(big.Int).SetString(p.BorrowAmount, 10)
			valueUSD, err := h.priceService.ConvertToUSD(c.Request.Context(), borrowAmount, p.AssetAddress)
			if err != nil {
				respondPriceError(c, err)
				return
			}

			borrows = append(borrows, PositionItem{
				Asset:    p.AssetAddress,
				Symbol:   symbol,
				Amount:   p.BorrowAmount,
				ValueUSD: price.FormatUSD(valueUSD),
			})
		}
	}

	c.JSON(http.StatusOK, PositionResponse{
		Deposits: deposits,
		Borrows:  borrows,
	})
}

func respondPriceError(c *gin.Context, err error) {
	status := http.StatusBadGateway
	if errors.Is(err, price.ErrAssetNotFound) ||
		errors.Is(err, price.ErrOracleNotConfigured) ||
		errors.Is(err, price.ErrPriceReaderUnavailable) {
		status = http.StatusServiceUnavailable
	} else if errors.Is(err, price.ErrInvalidAmount) {
		status = http.StatusInternalServerError
	}
	c.JSON(status, gin.H{"error": "价格预言机不可用"})
}

// GetActivities handles GET /api/v1/user/activities
func (h *UserHandler) GetActivities(c *gin.Context) {
	// Get wallet address from context
	walletAddr, exists := c.Get("walletAddress")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	wallet := walletAddr.(string)

	// Get user
	user, err := h.userRepo.GetByWallet(c.Request.Context(), wallet)
	if err != nil {
		if err == repository.ErrUserNotFound {
			c.JSON(http.StatusOK, gin.H{"activities": []ActivityResponse{}})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Get activities
	activities, _, err := h.activityRepo.GetByUserID(c.Request.Context(), user.ID, 0, 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result := make([]ActivityResponse, len(activities))
	for i, a := range activities {
		symbol := h.priceService.GetSymbol(a.AssetAddress)
		if symbol == "" {
			symbol = "UNKNOWN"
		}

		result[i] = ActivityResponse{
			TxHash:     a.TxHash,
			ActionType: string(a.ActionType),
			Asset:      a.AssetAddress,
			Symbol:     symbol,
			Amount:     a.Amount,
			Timestamp:  a.BlockTimestamp.Unix(),
		}
	}

	c.JSON(http.StatusOK, gin.H{"activities": result})
}
