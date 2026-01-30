package price

import (
	"context"
	"math/big"
	"strings"
	"sync"

	"github.com/creditlink/backend/internal/models"
	"github.com/creditlink/backend/internal/repository"
)

// AssetInfo contains asset metadata and price
type AssetInfo struct {
	Address  string
	Symbol   string
	Name     string
	Decimals uint8
	Price    float64 // USD price
}

// Service provides asset price and metadata lookups
type Service struct {
	riskRepo    *repository.RiskRepository
	assetCache  map[string]*AssetInfo // address -> info
	symbolCache map[string]*AssetInfo // symbol -> info
	mu          sync.RWMutex
}

// NewService creates a new price service
func NewService(riskRepo *repository.RiskRepository) *Service {
	s := &Service{
		riskRepo:    riskRepo,
		assetCache:  make(map[string]*AssetInfo),
		symbolCache: make(map[string]*AssetInfo),
	}
	return s
}

// LoadAssets loads asset information from risk params
func (s *Service) LoadAssets(ctx context.Context) error {
	params, err := s.riskRepo.GetAllRiskParams(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, p := range params {
		info := &AssetInfo{
			Address:  strings.ToLower(p.AssetAddress),
			Symbol:   p.AssetSymbol,
			Name:     getAssetName(p.AssetSymbol),
			Decimals: p.Decimals,
			Price:    getHardcodedPrice(p.AssetSymbol),
		}
		s.assetCache[info.Address] = info
		s.symbolCache[strings.ToUpper(p.AssetSymbol)] = info
	}

	return nil
}

// GetAssetByAddress returns asset info by address
func (s *Service) GetAssetByAddress(address string) *AssetInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.assetCache[strings.ToLower(address)]
}

// GetAssetBySymbol returns asset info by symbol
func (s *Service) GetAssetBySymbol(symbol string) *AssetInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.symbolCache[strings.ToUpper(symbol)]
}

// GetPrice returns the USD price for an asset
func (s *Service) GetPrice(addressOrSymbol string) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Try by address first
	if info, ok := s.assetCache[strings.ToLower(addressOrSymbol)]; ok {
		return info.Price
	}
	// Try by symbol
	if info, ok := s.symbolCache[strings.ToUpper(addressOrSymbol)]; ok {
		return info.Price
	}
	// Default to stablecoin price
	return 1.0
}

// GetSymbol returns the symbol for an asset address
func (s *Service) GetSymbol(address string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if info, ok := s.assetCache[strings.ToLower(address)]; ok {
		return info.Symbol
	}
	return ""
}

// GetDecimals returns the decimals for an asset
func (s *Service) GetDecimals(addressOrSymbol string) uint8 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Try by address first
	if info, ok := s.assetCache[strings.ToLower(addressOrSymbol)]; ok {
		return info.Decimals
	}
	// Try by symbol
	if info, ok := s.symbolCache[strings.ToUpper(addressOrSymbol)]; ok {
		return info.Decimals
	}
	// Default to 18 decimals
	return 18
}

// ConvertToUSD converts a token amount to USD value
func (s *Service) ConvertToUSD(amount *big.Int, addressOrSymbol string) *big.Float {
	if amount == nil || amount.Sign() == 0 {
		return big.NewFloat(0)
	}

	decimals := s.GetDecimals(addressOrSymbol)
	price := s.GetPrice(addressOrSymbol)

	// amount / 10^decimals * price
	amountFloat := new(big.Float).SetInt(amount)
	divisor := new(big.Float).SetFloat64(pow10(int(decimals)))
	normalized := new(big.Float).Quo(amountFloat, divisor)
	return new(big.Float).Mul(normalized, big.NewFloat(price))
}

// ConvertToUSDFromString converts a string amount to USD value
func (s *Service) ConvertToUSDFromString(amountStr string, addressOrSymbol string) *big.Float {
	amount, ok := new(big.Int).SetString(amountStr, 10)
	if !ok || amount.Sign() == 0 {
		return big.NewFloat(0)
	}
	return s.ConvertToUSD(amount, addressOrSymbol)
}

// FormatUSD formats a big.Float as a USD string
func FormatUSD(amount *big.Float) string {
	if amount == nil || amount.Sign() == 0 {
		return "0"
	}
	f, _ := amount.Float64()
	return big.NewFloat(f).Text('f', 2)
}

// GetAllAssets returns all loaded assets
func (s *Service) GetAllAssets() []*AssetInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	assets := make([]*AssetInfo, 0, len(s.assetCache))
	for _, info := range s.assetCache {
		assets = append(assets, info)
	}
	return assets
}

// CalculateHealthFactor calculates health factor from positions and risk params
func (s *Service) CalculateHealthFactor(
	positions []models.UserPosition,
	riskParams map[string]*models.CurrentRiskParams,
) *big.Float {
	totalCollateralETH := big.NewFloat(0)
	totalDebtETH := big.NewFloat(0)

	for _, pos := range positions {
		params, ok := riskParams[strings.ToLower(pos.AssetAddress)]
		if !ok {
			continue
		}

		// Calculate deposit value in USD
		depositAmount, _ := new(big.Int).SetString(pos.DepositAmount, 10)
		if depositAmount != nil && depositAmount.Sign() > 0 {
			depositUSD := s.ConvertToUSD(depositAmount, pos.AssetAddress)
			// Apply liquidation threshold
			threshold := big.NewFloat(float64(params.LiquidationThreshold) / 10000)
			collateralValue := new(big.Float).Mul(depositUSD, threshold)
			totalCollateralETH.Add(totalCollateralETH, collateralValue)
		}

		// Calculate borrow value in USD
		borrowAmount, _ := new(big.Int).SetString(pos.BorrowAmount, 10)
		if borrowAmount != nil && borrowAmount.Sign() > 0 {
			borrowUSD := s.ConvertToUSD(borrowAmount, pos.AssetAddress)
			totalDebtETH.Add(totalDebtETH, borrowUSD)
		}
	}

	// Health Factor = Total Collateral (with threshold) / Total Debt
	if totalDebtETH.Sign() == 0 {
		return big.NewFloat(0) // No debt means infinite health factor (represented as 0)
	}

	return new(big.Float).Quo(totalCollateralETH, totalDebtETH)
}

// CalculateAvailableBorrow calculates the available borrow amount
func (s *Service) CalculateAvailableBorrow(
	positions []models.UserPosition,
	riskParams map[string]*models.CurrentRiskParams,
	maxLTV int, // from credit score, in basis points
) *big.Float {
	totalCollateralUSD := big.NewFloat(0)
	totalDebtUSD := big.NewFloat(0)

	for _, pos := range positions {
		// Calculate deposit value in USD
		depositAmount, _ := new(big.Int).SetString(pos.DepositAmount, 10)
		if depositAmount != nil && depositAmount.Sign() > 0 {
			depositUSD := s.ConvertToUSD(depositAmount, pos.AssetAddress)
			totalCollateralUSD.Add(totalCollateralUSD, depositUSD)
		}

		// Calculate borrow value in USD
		borrowAmount, _ := new(big.Int).SetString(pos.BorrowAmount, 10)
		if borrowAmount != nil && borrowAmount.Sign() > 0 {
			borrowUSD := s.ConvertToUSD(borrowAmount, pos.AssetAddress)
			totalDebtUSD.Add(totalDebtUSD, borrowUSD)
		}
	}

	// Available Borrow = (Total Collateral * Max LTV) - Total Debt
	ltvMultiplier := big.NewFloat(float64(maxLTV) / 10000)
	maxBorrow := new(big.Float).Mul(totalCollateralUSD, ltvMultiplier)
	available := new(big.Float).Sub(maxBorrow, totalDebtUSD)

	if available.Sign() < 0 {
		return big.NewFloat(0)
	}

	return available
}

// Helper: get hardcoded USD price for development
func getHardcodedPrice(symbol string) float64 {
	prices := map[string]float64{
		"USDT": 1.0,
		"USDC": 1.0,
		"DAI":  1.0,
		"WETH": 2500.0,
		"ETH":  2500.0,
		"WBTC": 45000.0,
		"BTC":  45000.0,
	}
	if price, ok := prices[strings.ToUpper(symbol)]; ok {
		return price
	}
	return 1.0 // Default stablecoin price
}

// Helper: get asset name from symbol
func getAssetName(symbol string) string {
	names := map[string]string{
		"USDT": "Tether USD",
		"USDC": "USD Coin",
		"DAI":  "Dai Stablecoin",
		"WETH": "Wrapped Ether",
		"ETH":  "Ethereum",
		"WBTC": "Wrapped Bitcoin",
	}
	if name, ok := names[strings.ToUpper(symbol)]; ok {
		return name
	}
	return symbol
}

// Helper: calculate 10^n
func pow10(n int) float64 {
	result := 1.0
	for i := 0; i < n; i++ {
		result *= 10
	}
	return result
}
