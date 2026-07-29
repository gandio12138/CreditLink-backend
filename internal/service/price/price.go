package price

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/creditlink/backend/internal/models"
	"github.com/ethereum/go-ethereum/common"
)

var (
	ErrAssetNotFound          = errors.New("asset metadata not found")
	ErrOracleNotConfigured    = errors.New("price oracle not configured")
	ErrPriceReaderUnavailable = errors.New("price reader unavailable")
	ErrOracleReadFailed       = errors.New("price oracle read failed")
	ErrInvalidAmount          = errors.New("invalid token amount")
)

type riskParamsRepository interface {
	GetAllRiskParams(ctx context.Context) ([]models.CurrentRiskParams, error)
}

// AssetInfo contains asset metadata required for live oracle reads.
type AssetInfo struct {
	Address       string
	Symbol        string
	Name          string
	Decimals      uint8
	OracleAddress string
}

// Service provides asset price and metadata lookups
type Service struct {
	riskRepo      riskParamsRepository
	priceReader   OraclePriceReader
	assetCache    map[string]*AssetInfo // address -> info
	symbolCache   map[string]*AssetInfo // symbol -> info
	metadataReady bool
	mu            sync.RWMutex
}

// NewService creates a new price service
func NewService(riskRepo riskParamsRepository, readers ...OraclePriceReader) *Service {
	var priceReader OraclePriceReader
	if len(readers) > 0 {
		priceReader = readers[0]
	}
	s := &Service{
		riskRepo:    riskRepo,
		priceReader: priceReader,
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

	assetCache := make(map[string]*AssetInfo, len(params))
	symbolCache := make(map[string]*AssetInfo, len(params))
	var validationErrors []error
	for _, p := range params {
		assetAddress := strings.ToLower(strings.TrimSpace(p.AssetAddress))
		oracleAddress := strings.ToLower(strings.TrimSpace(p.PriceOracleAddress))
		canonicalSymbol := strings.ToUpper(strings.TrimSpace(p.AssetSymbol))
		if !common.IsHexAddress(assetAddress) || common.HexToAddress(assetAddress) == (common.Address{}) {
			validationErrors = append(validationErrors, fmt.Errorf("asset %q: %w", p.AssetSymbol, ErrAssetNotFound))
			continue
		}
		if canonicalSymbol == "" {
			validationErrors = append(validationErrors, fmt.Errorf("asset %s: empty symbol: %w", assetAddress, ErrAssetNotFound))
			continue
		}
		if !common.IsHexAddress(oracleAddress) || common.HexToAddress(oracleAddress) == (common.Address{}) {
			validationErrors = append(validationErrors, fmt.Errorf("asset %s (%s): %w", p.AssetSymbol, assetAddress, ErrOracleNotConfigured))
			continue
		}
		if existing, exists := symbolCache[canonicalSymbol]; exists && existing.Address != assetAddress {
			validationErrors = append(validationErrors, fmt.Errorf("duplicate asset symbol %s for %s and %s", canonicalSymbol, existing.Address, assetAddress))
			continue
		}
		info := &AssetInfo{
			Address:       assetAddress,
			Symbol:        strings.TrimSpace(p.AssetSymbol),
			Name:          getAssetName(p.AssetSymbol),
			Decimals:      p.Decimals,
			OracleAddress: oracleAddress,
		}
		assetCache[info.Address] = info
		symbolCache[canonicalSymbol] = info
	}

	validationErr := errors.Join(validationErrors...)
	s.mu.Lock()
	if validationErr != nil {
		s.assetCache = make(map[string]*AssetInfo)
		s.symbolCache = make(map[string]*AssetInfo)
		s.metadataReady = false
	} else {
		s.assetCache = assetCache
		s.symbolCache = symbolCache
		s.metadataReady = true
	}
	s.mu.Unlock()
	return validationErr
}

// InvalidateAssets fails closed after metadata synchronization errors.
func (s *Service) InvalidateAssets() {
	s.mu.Lock()
	s.assetCache = make(map[string]*AssetInfo)
	s.symbolCache = make(map[string]*AssetInfo)
	s.metadataReady = false
	s.mu.Unlock()
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

// GetPrice returns the adapter's live 18-decimal USD price for an asset.
func (s *Service) GetPrice(ctx context.Context, addressOrSymbol string) (*big.Int, error) {
	info, err := s.getAsset(addressOrSymbol)
	if err != nil {
		return nil, err
	}
	if s.priceReader == nil {
		return nil, ErrPriceReaderUnavailable
	}
	priceValue, err := s.priceReader.ReadPrice(ctx, info.OracleAddress, info.Address)
	if err != nil {
		return nil, fmt.Errorf("%w for %s: %w", ErrOracleReadFailed, info.Symbol, err)
	}
	if priceValue == nil || priceValue.Sign() <= 0 {
		return nil, fmt.Errorf("%w for %s: non-positive price", ErrOracleReadFailed, info.Symbol)
	}
	return priceValue, nil
}

// ConvertUSDToTokenAmount converts an 18-decimal USD wad into token raw units,
// rounding down so the signed cap never exceeds the USD credit limit.
func (s *Service) ConvertUSDToTokenAmount(ctx context.Context, usdWad *big.Int, addressOrSymbol string) (*big.Int, *AssetInfo, error) {
	if usdWad == nil || usdWad.Sign() < 0 {
		return nil, nil, ErrInvalidAmount
	}
	info, err := s.getAsset(addressOrSymbol)
	if err != nil {
		return nil, nil, err
	}
	priceValue, err := s.GetPrice(ctx, info.Address)
	if err != nil {
		return nil, nil, err
	}
	tokenScale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(info.Decimals)), nil)
	tokenAmount := new(big.Int).Mul(new(big.Int).Set(usdWad), tokenScale)
	tokenAmount.Div(tokenAmount, priceValue)
	return tokenAmount, info, nil
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
func (s *Service) GetDecimals(addressOrSymbol string) (uint8, error) {
	info, err := s.getAsset(addressOrSymbol)
	if err != nil {
		return 0, err
	}
	return info.Decimals, nil
}

func (s *Service) getAsset(addressOrSymbol string) (*AssetInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.metadataReady {
		return nil, ErrPriceReaderUnavailable
	}

	// Try by address first
	if info, ok := s.assetCache[strings.ToLower(addressOrSymbol)]; ok {
		copyInfo := *info
		return &copyInfo, nil
	}
	// Try by symbol
	if info, ok := s.symbolCache[strings.ToUpper(addressOrSymbol)]; ok {
		copyInfo := *info
		return &copyInfo, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrAssetNotFound, addressOrSymbol)
}

// ConvertToUSD converts a token amount to USD value
func (s *Service) ConvertToUSD(ctx context.Context, amount *big.Int, addressOrSymbol string) (*big.Float, error) {
	values, err := s.ConvertToUSDValues(ctx, addressOrSymbol, amount)
	if err != nil {
		return nil, err
	}
	return values[0], nil
}

// ConvertToUSDValues converts multiple token amounts with one consistent live oracle price.
func (s *Service) ConvertToUSDValues(ctx context.Context, addressOrSymbol string, amounts ...*big.Int) ([]*big.Float, error) {
	if len(amounts) == 0 {
		return nil, ErrInvalidAmount
	}
	decimals, err := s.GetDecimals(addressOrSymbol)
	if err != nil {
		return nil, err
	}
	priceValue, err := s.GetPrice(ctx, addressOrSymbol)
	if err != nil {
		return nil, err
	}

	precision := uint(256)
	tokenScale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	priceScale := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	denominator := new(big.Float).SetPrec(precision).SetInt(new(big.Int).Mul(tokenScale, priceScale))
	values := make([]*big.Float, 0, len(amounts))
	for _, amount := range amounts {
		if amount == nil || amount.Sign() < 0 {
			return nil, ErrInvalidAmount
		}
		if amount.Sign() == 0 {
			values = append(values, big.NewFloat(0))
			continue
		}
		numerator := new(big.Float).SetPrec(precision).SetInt(new(big.Int).Mul(new(big.Int).Set(amount), priceValue))
		values = append(values, new(big.Float).SetPrec(precision).Quo(numerator, denominator))
	}
	return values, nil
}

// ConvertToUSDFromString converts a string amount to USD value
func (s *Service) ConvertToUSDFromString(ctx context.Context, amountStr string, addressOrSymbol string) (*big.Float, error) {
	amount, ok := new(big.Int).SetString(amountStr, 10)
	if !ok || amount.Sign() < 0 {
		return nil, ErrInvalidAmount
	}
	return s.ConvertToUSD(ctx, amount, addressOrSymbol)
}

// FormatUSD formats a big.Float as a USD string
func FormatUSD(amount *big.Float) string {
	if amount == nil || amount.Sign() == 0 {
		return "0"
	}
	return amount.Text('f', 2)
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
	ctx context.Context,
	positions []models.UserPosition,
	riskParams map[string]*models.CurrentRiskParams,
) (*big.Float, error) {
	totalCollateralETH := big.NewFloat(0)
	totalDebtETH := big.NewFloat(0)

	for _, pos := range positions {
		params, ok := riskParams[strings.ToLower(pos.AssetAddress)]
		if !ok {
			continue
		}

		// Calculate deposit value in USD
		depositAmount, ok := new(big.Int).SetString(pos.DepositAmount, 10)
		if !ok {
			return nil, ErrInvalidAmount
		}
		if depositAmount != nil && depositAmount.Sign() > 0 {
			depositUSD, err := s.ConvertToUSD(ctx, depositAmount, pos.AssetAddress)
			if err != nil {
				return nil, err
			}
			// Apply liquidation threshold
			threshold := big.NewFloat(float64(params.LiquidationThreshold) / 10000)
			collateralValue := new(big.Float).Mul(depositUSD, threshold)
			totalCollateralETH.Add(totalCollateralETH, collateralValue)
		}

		// Calculate borrow value in USD
		borrowAmount, ok := new(big.Int).SetString(pos.BorrowAmount, 10)
		if !ok {
			return nil, ErrInvalidAmount
		}
		if borrowAmount != nil && borrowAmount.Sign() > 0 {
			borrowUSD, err := s.ConvertToUSD(ctx, borrowAmount, pos.AssetAddress)
			if err != nil {
				return nil, err
			}
			totalDebtETH.Add(totalDebtETH, borrowUSD)
		}
	}

	// Health Factor = Total Collateral (with threshold) / Total Debt
	if totalDebtETH.Sign() == 0 {
		return big.NewFloat(0), nil // No debt means infinite health factor (represented as 0)
	}

	return new(big.Float).Quo(totalCollateralETH, totalDebtETH), nil
}

// CalculateAvailableBorrow calculates the available borrow amount
func (s *Service) CalculateAvailableBorrow(
	ctx context.Context,
	positions []models.UserPosition,
	riskParams map[string]*models.CurrentRiskParams,
	maxLTV int, // from credit score, in basis points
) (*big.Float, error) {
	totalCollateralUSD := big.NewFloat(0)
	totalDebtUSD := big.NewFloat(0)

	for _, pos := range positions {
		// Calculate deposit value in USD
		depositAmount, ok := new(big.Int).SetString(pos.DepositAmount, 10)
		if !ok {
			return nil, ErrInvalidAmount
		}
		if depositAmount != nil && depositAmount.Sign() > 0 {
			depositUSD, err := s.ConvertToUSD(ctx, depositAmount, pos.AssetAddress)
			if err != nil {
				return nil, err
			}
			totalCollateralUSD.Add(totalCollateralUSD, depositUSD)
		}

		// Calculate borrow value in USD
		borrowAmount, ok := new(big.Int).SetString(pos.BorrowAmount, 10)
		if !ok {
			return nil, ErrInvalidAmount
		}
		if borrowAmount != nil && borrowAmount.Sign() > 0 {
			borrowUSD, err := s.ConvertToUSD(ctx, borrowAmount, pos.AssetAddress)
			if err != nil {
				return nil, err
			}
			totalDebtUSD.Add(totalDebtUSD, borrowUSD)
		}
	}

	// Available Borrow = (Total Collateral * Max LTV) - Total Debt
	ltvMultiplier := big.NewFloat(float64(maxLTV) / 10000)
	maxBorrow := new(big.Float).Mul(totalCollateralUSD, ltvMultiplier)
	available := new(big.Float).Sub(maxBorrow, totalDebtUSD)

	if available.Sign() < 0 {
		return big.NewFloat(0), nil
	}

	return available, nil
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
