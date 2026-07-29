package handlers

import (
	"errors"
	"math/big"
	"net/http"
	"strconv"

	"github.com/creditlink/backend/internal/repository"
	"github.com/creditlink/backend/internal/service/credit"
	"github.com/creditlink/backend/internal/service/price"
	"github.com/creditlink/backend/internal/service/signer"
	"github.com/ethereum/go-ethereum/common"
	"github.com/gin-gonic/gin"
)

// CreditHandler handles credit-related API requests
type CreditHandler struct {
	creditEngine  *credit.Engine
	signerService *signer.Service
	creditRepo    *repository.CreditRepository
	userRepo      *repository.UserRepository
}

// NewCreditHandler creates a new credit handler
func NewCreditHandler(
	creditEngine *credit.Engine,
	signerService *signer.Service,
	creditRepo *repository.CreditRepository,
	userRepo *repository.UserRepository,
) *CreditHandler {
	return &CreditHandler{
		creditEngine:  creditEngine,
		signerService: signerService,
		creditRepo:    creditRepo,
		userRepo:      userRepo,
	}
}

// SignRequest represents the request body for credit signing
type SignRequest struct {
	Market string `json:"market" binding:"required"` // e.g., "USDT", "ETH"
	Amount string `json:"amount" binding:"required"` // wei string
}

// SignResponse represents the signature response
type SignResponse struct {
	Signature   string `json:"signature"`
	Market      string `json:"market"`
	LTV         int    `json:"ltv"`
	AmountCap   string `json:"amountCap"`
	Nonce       string `json:"nonce"`
	Deadline    int64  `json:"deadline"`
	CreditScore int    `json:"creditScore"`
	CreditTier  string `json:"creditTier"`
}

// CreditInfoResponse represents user credit information
type CreditInfoResponse struct {
	Score     int                 `json:"score"`
	Tier      string              `json:"tier"`
	MaxLTV    int                 `json:"maxLtv"`
	MaxBorrow string              `json:"maxBorrow"`
	Factors   []CreditFactorInfo  `json:"factors"`
	History   []CreditHistoryItem `json:"history"`
}

type CreditFactorInfo struct {
	Key          string  `json:"key"`
	Value        float64 `json:"value"`
	Contribution int     `json:"contribution"`
}

type CreditHistoryItem struct {
	Timestamp int64  `json:"timestamp"`
	Score     int    `json:"score"`
	Event     string `json:"event"`
}

// Sign handles POST /api/v1/credit/sign
func (h *CreditHandler) Sign(c *gin.Context) {
	if h.signerService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "signing service unavailable"})
		return
	}

	var req SignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get wallet address from context (set by JWT middleware)
	walletAddr, exists := c.Get("walletAddress")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	wallet := walletAddr.(string)

	// Parse amount
	amount, ok := new(big.Int).SetString(req.Amount, 10)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid amount format"})
		return
	}

	// Create signature request
	signReq := &signer.SignatureRequest{
		User:      common.HexToAddress(wallet),
		Market:    req.Market,
		Amount:    amount,
		IPAddress: c.ClientIP(),
		UserAgent: c.GetHeader("User-Agent"),
	}

	// Sign
	resp, err := h.signerService.Sign(c.Request.Context(), signReq)
	if err != nil {
		c.JSON(signErrorStatus(err), gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, SignResponse{
		Signature:   resp.Signature,
		Market:      resp.Market,
		LTV:         resp.LTV,
		AmountCap:   resp.AmountCap,
		Nonce:       strconv.FormatUint(resp.Nonce, 10),
		Deadline:    resp.Deadline,
		CreditScore: resp.CreditScore,
		CreditTier:  resp.CreditTier,
	})
}

func signErrorStatus(err error) int {
	if errors.Is(err, signer.ErrRateLimitExceeded) {
		return http.StatusTooManyRequests
	}
	if errors.Is(err, signer.ErrInsufficientCredit) {
		return http.StatusForbidden
	}
	if errors.Is(err, signer.ErrAmountExceedsLimit) || errors.Is(err, signer.ErrUnsupportedMarket) || errors.Is(err, signer.ErrInvalidAmount) {
		return http.StatusBadRequest
	}
	if errors.Is(err, signer.ErrPriceUnavailable) {
		if errors.Is(err, price.ErrPriceReaderUnavailable) || errors.Is(err, price.ErrOracleNotConfigured) {
			return http.StatusServiceUnavailable
		}
		return http.StatusBadGateway
	}
	return http.StatusInternalServerError
}

// GetCredit handles GET /api/v1/user/credit
func (h *CreditHandler) GetCredit(c *gin.Context) {
	// Get wallet address from context
	walletAddr, exists := c.Get("walletAddress")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	wallet := walletAddr.(string)

	// Calculate fresh score
	score, factors, err := h.creditEngine.CalculateScore(c.Request.Context(), wallet)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Save the calculated score (this also records history if score changed)
	if err := h.creditEngine.SaveScore(c.Request.Context(), wallet, score, factors); err != nil {
		// Log but don't fail the request
		c.Error(err)
	}

	// Build response
	factorInfos := []CreditFactorInfo{
		{Key: "repayment_bonus", Value: float64(factors.RepaymentBonus), Contribution: factors.RepaymentBonus},
		{Key: "liquidation_penalty", Value: float64(factors.LiquidationPenalty), Contribution: factors.LiquidationPenalty},
		{Key: "borrow_history_bonus", Value: float64(factors.BorrowHistoryBonus), Contribution: factors.BorrowHistoryBonus},
		{Key: "loyalty_bonus", Value: float64(factors.LoyaltyBonus), Contribution: factors.LoyaltyBonus},
		{Key: "health_factor_bonus", Value: float64(factors.HealthFactorBonus), Contribution: factors.HealthFactorBonus},
		{Key: "wallet_age_bonus", Value: float64(factors.WalletAgeBonus), Contribution: factors.WalletAgeBonus},
		{Key: "activity_bonus", Value: float64(factors.ActivityBonus), Contribution: factors.ActivityBonus},
		{Key: "diversity_bonus", Value: float64(factors.DiversityBonus), Contribution: factors.DiversityBonus},
		{Key: "net_worth_bonus", Value: float64(factors.NetWorthBonus), Contribution: factors.NetWorthBonus},
	}

	// Filter out zero contributions
	nonZeroFactors := make([]CreditFactorInfo, 0)
	for _, f := range factorInfos {
		if f.Contribution != 0 {
			nonZeroFactors = append(nonZeroFactors, f)
		}
	}

	// Fetch credit score history
	historyItems := []CreditHistoryItem{}
	user, err := h.userRepo.GetByWallet(c.Request.Context(), wallet)
	if err == nil {
		history, err := h.creditRepo.GetScoreHistory(c.Request.Context(), user.ID, 20)
		if err == nil {
			for _, h := range history {
				historyItems = append(historyItems, CreditHistoryItem{
					Timestamp: h.CreatedAt.Unix(),
					Score:     h.Score,
					Event:     h.EventType,
				})
			}
		}
	}

	c.JSON(http.StatusOK, CreditInfoResponse{
		Score:     score.Score,
		Tier:      score.Tier,
		MaxLTV:    score.MaxLTV,
		MaxBorrow: score.MaxBorrow,
		Factors:   nonZeroFactors,
		History:   historyItems,
	})
}

// RefreshCredit handles POST /api/v1/user/credit/refresh
func (h *CreditHandler) RefreshCredit(c *gin.Context) {
	// Get wallet address from context
	walletAddr, exists := c.Get("walletAddress")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	wallet := walletAddr.(string)

	// Recalculate and save score
	score, factors, err := h.creditEngine.CalculateScore(c.Request.Context(), wallet)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := h.creditEngine.SaveScore(c.Request.Context(), wallet, score, factors); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"score":   score.Score,
		"tier":    score.Tier,
		"message": "Credit score refreshed successfully",
	})
}
