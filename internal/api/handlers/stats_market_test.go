package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/creditlink/backend/internal/models"
	"github.com/creditlink/backend/internal/repository"
	priceservice "github.com/creditlink/backend/internal/service/price"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	marketTestAsset  = "0x1111111111111111111111111111111111111111"
	marketTestOracle = "0x2222222222222222222222222222222222222222"
)

type marketPriceReader struct {
	price *big.Int
	err   error
	calls int
}

func (r *marketPriceReader) ReadPrice(context.Context, string, string) (*big.Int, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	return new(big.Int).Set(r.price), nil
}

func TestStatsHandlerGetMarketsDataReturnsUSDAndTokenAmounts(t *testing.T) {
	handler, reader := newMarketStatsTestHandler(t, nil)
	router := gin.New()
	router.GET("/stats/markets", handler.GetMarketsData)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/stats/markets", nil))

	require.Equal(t, http.StatusOK, response.Code)
	var payload struct {
		Markets []MarketData `json:"markets"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
	require.Len(t, payload.Markets, 1)
	require.Equal(t, "5000.00", payload.Markets[0].TotalSupply)
	require.Equal(t, "1250.00", payload.Markets[0].TotalBorrow)
	require.Equal(t, "2", payload.Markets[0].TotalSupplyAmount)
	require.Equal(t, "0.5", payload.Markets[0].TotalBorrowAmount)
	require.Equal(t, 1, reader.calls)
}

func TestStatsHandlerGetMarketsDataFailsClosedOnOracleError(t *testing.T) {
	handler, _ := newMarketStatsTestHandler(t, errors.New("oracle unavailable"))
	router := gin.New()
	router.GET("/stats/markets", handler.GetMarketsData)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/stats/markets", nil))

	require.Equal(t, http.StatusBadGateway, response.Code)
}

func newMarketStatsTestHandler(t *testing.T, oracleErr error) (*StatsHandler, *marketPriceReader) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.LoanActivity{}, &models.CurrentRiskParams{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })

	user := models.User{WalletAddress: "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	require.NoError(t, db.Create(&user).Error)
	riskParams := models.CurrentRiskParams{
		AssetAddress:       marketTestAsset,
		AssetSymbol:        "WETH",
		Decimals:           18,
		PriceOracleAddress: marketTestOracle,
	}
	require.NoError(t, db.Create(&riskParams).Error)
	require.NoError(t, db.Create(&[]models.LoanActivity{
		{UserID: user.ID, TxHash: "0xdeposit", ActionType: models.ActionDeposit, AssetAddress: marketTestAsset, Amount: "2000000000000000000", BlockTimestamp: time.Now()},
		{UserID: user.ID, TxHash: "0xborrow", ActionType: models.ActionBorrow, AssetAddress: marketTestAsset, Amount: "500000000000000000", BlockTimestamp: time.Now()},
	}).Error)

	riskRepo := repository.NewRiskRepository(db)
	oraclePrice, ok := new(big.Int).SetString("2500000000000000000000", 10)
	require.True(t, ok)
	reader := &marketPriceReader{price: oraclePrice, err: oracleErr}
	priceService := priceservice.NewService(riskRepo, reader)
	require.NoError(t, priceService.LoadAssets(context.Background()))
	return NewStatsHandler(repository.NewActivityRepository(db), nil, riskRepo, priceService), reader
}
