package handlers

import (
	"net/http"

	"github.com/creditlink/backend/internal/repository"
	"github.com/gin-gonic/gin"
)

// RiskHandler handles risk-related API requests
type RiskHandler struct {
	riskRepo *repository.RiskRepository
}

// NewRiskHandler creates a new risk handler
func NewRiskHandler(riskRepo *repository.RiskRepository) *RiskHandler {
	return &RiskHandler{
		riskRepo: riskRepo,
	}
}

// AssetRiskParams represents risk parameters for an asset
type AssetRiskParams struct {
	Address              string `json:"address"`
	Symbol               string `json:"symbol"`
	Decimals             uint8  `json:"decimals"`
	BaseLTV              int    `json:"baseLtv"`
	LiquidationThreshold int    `json:"liquidationThreshold"`
	LiquidationBonus     int    `json:"liquidationBonus"`
	CloseFactor          int    `json:"closeFactor"`
	BorrowCap            string `json:"borrowCap"`
	SupplyCap            string `json:"supplyCap"`
	BorrowEnabled        bool   `json:"borrowEnabled"`
	CollateralEnabled    bool   `json:"collateralEnabled"`
}

// RiskParamsResponse represents the risk parameters response
type RiskParamsResponse struct {
	Assets []AssetRiskParams `json:"assets"`
}

// GetRiskParams handles GET /api/v1/risk/params
func (h *RiskHandler) GetRiskParams(c *gin.Context) {
	params, err := h.riskRepo.GetAllRiskParams(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	assets := make([]AssetRiskParams, len(params))
	for i, p := range params {
		assets[i] = AssetRiskParams{
			Address:              p.AssetAddress,
			Symbol:               p.AssetSymbol,
			Decimals:             p.Decimals,
			BaseLTV:              p.BaseLTV,
			LiquidationThreshold: p.LiquidationThreshold,
			LiquidationBonus:     p.LiquidationBonus,
			CloseFactor:          p.CloseFactor,
			BorrowCap:            p.BorrowCap,
			SupplyCap:            p.SupplyCap,
			BorrowEnabled:        p.BorrowEnabled,
			CollateralEnabled:    p.CollateralEnabled,
		}
	}

	c.JSON(http.StatusOK, RiskParamsResponse{Assets: assets})
}

// GetAssetRiskParams handles GET /api/v1/risk/params/:asset
func (h *RiskHandler) GetAssetRiskParams(c *gin.Context) {
	assetAddress := c.Param("asset")

	params, err := h.riskRepo.GetRiskParams(c.Request.Context(), assetAddress)
	if err != nil {
		if err == repository.ErrRiskParamsNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "asset not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, AssetRiskParams{
		Address:              params.AssetAddress,
		Symbol:               params.AssetSymbol,
		Decimals:             params.Decimals,
		BaseLTV:              params.BaseLTV,
		LiquidationThreshold: params.LiquidationThreshold,
		LiquidationBonus:     params.LiquidationBonus,
		CloseFactor:          params.CloseFactor,
		BorrowCap:            params.BorrowCap,
		SupplyCap:            params.SupplyCap,
		BorrowEnabled:        params.BorrowEnabled,
		CollateralEnabled:    params.CollateralEnabled,
	})
}
