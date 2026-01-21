package handlers

import (
	"net/http"

	"github.com/creditlink/backend/internal/repository"
	"github.com/gin-gonic/gin"
)

// UserHandler handles user-related API requests
type UserHandler struct {
	userRepo     *repository.UserRepository
	activityRepo *repository.ActivityRepository
	riskRepo     *repository.RiskRepository
}

// NewUserHandler creates a new user handler
func NewUserHandler(
	userRepo *repository.UserRepository,
	activityRepo *repository.ActivityRepository,
	riskRepo *repository.RiskRepository,
) *UserHandler {
	return &UserHandler{
		userRepo:     userRepo,
		activityRepo: activityRepo,
		riskRepo:     riskRepo,
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
	Asset   string `json:"asset"`
	Symbol  string `json:"symbol"`
	Amount  string `json:"amount"`
	ValueUSD string `json:"valueUsd"`
}

// ActivityResponse represents a loan activity
type ActivityResponse struct {
	TxHash     string `json:"txHash"`
	ActionType string `json:"actionType"`
	Asset      string `json:"asset"`
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

	// Calculate totals (in production, would need price oracle)
	// For now, return raw values
	var totalDeposit, totalBorrow string
	for _, p := range positions {
		if p.DepositAmount != "0" {
			totalDeposit = p.DepositAmount
		}
		if p.BorrowAmount != "0" {
			totalBorrow = p.BorrowAmount
		}
	}

	c.JSON(http.StatusOK, AccountResponse{
		TotalCollateralUSD: totalDeposit,
		TotalDebtUSD:       totalBorrow,
		AvailableBorrowUSD: "0", // Would need calculation with LTV
		CurrentLTV:         0,
		HealthFactor:       "0", // Would need calculation
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
		if p.DepositAmount != "0" {
			deposits = append(deposits, PositionItem{
				Asset:    p.AssetAddress,
				Symbol:   "", // Would need to look up
				Amount:   p.DepositAmount,
				ValueUSD: "0", // Would need price oracle
			})
		}
		if p.BorrowAmount != "0" {
			borrows = append(borrows, PositionItem{
				Asset:    p.AssetAddress,
				Symbol:   "",
				Amount:   p.BorrowAmount,
				ValueUSD: "0",
			})
		}
	}

	c.JSON(http.StatusOK, PositionResponse{
		Deposits: deposits,
		Borrows:  borrows,
	})
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
		result[i] = ActivityResponse{
			TxHash:     a.TxHash,
			ActionType: string(a.ActionType),
			Asset:      a.AssetAddress,
			Amount:     a.Amount,
			Timestamp:  a.BlockTimestamp.Unix(),
		}
	}

	c.JSON(http.StatusOK, gin.H{"activities": result})
}
