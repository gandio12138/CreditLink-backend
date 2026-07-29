package signer

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/creditlink/backend/internal/models"
	"github.com/creditlink/backend/internal/repository"
	"github.com/creditlink/backend/internal/service/credit"
	priceservice "github.com/creditlink/backend/internal/service/price"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	amountCapUSDC   = "0x1111111111111111111111111111111111111111"
	amountCapWETH   = "0x2222222222222222222222222222222222222222"
	amountCapOracle = "0x3333333333333333333333333333333333333333"
)

type amountCapPriceReader struct {
	prices map[string]*big.Int
	err    error
	calls  int
}

func (r *amountCapPriceReader) ReadPrice(_ context.Context, _, assetAddress string) (*big.Int, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	value := r.prices[strings.ToLower(assetAddress)]
	if value == nil {
		return nil, errors.New("missing price")
	}
	return new(big.Int).Set(value), nil
}

func TestSignConvertsUSDCAndWETHUSDCapToRawTokenCap(t *testing.T) {
	tests := []struct {
		name        string
		market      string
		asset       string
		decimals    uint8
		price       string
		expectedCap string
	}{
		{name: "USDC 6 decimals", market: "usdc", asset: amountCapUSDC, decimals: 6, price: "1000000000000000000", expectedCap: "10000000000"},
		{name: "WETH 18 decimals", market: "weth", asset: amountCapWETH, decimals: 18, price: "2500000000000000000000", expectedCap: "4000000000000000000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, signatureRepo, reader := newAmountCapSigner(t, tt.asset, strings.ToUpper(tt.market), tt.decimals, tt.price, nil)
			expectedCap, ok := new(big.Int).SetString(tt.expectedCap, 10)
			require.True(t, ok)

			response, err := service.Sign(context.Background(), &SignatureRequest{
				User:   common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
				Market: tt.market,
				Amount: expectedCap,
			})
			require.NoError(t, err)
			require.Equal(t, strings.ToUpper(tt.market), response.Market)
			require.Equal(t, tt.expectedCap, response.AmountCap)
			require.Equal(t, "C", response.CreditTier)
			require.Equal(t, 1, reader.calls)
			signatureBytes, err := hex.DecodeString(strings.TrimPrefix(response.Signature, "0x"))
			require.NoError(t, err)
			signatureBytes[64] -= 27
			structHash := service.buildStructHash(
				common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
				crypto.Keccak256Hash([]byte(strings.ToUpper(tt.market))),
				big.NewInt(int64(response.LTV)),
				expectedCap,
				response.Nonce,
				response.Deadline,
			)
			publicKey, err := crypto.SigToPub(service.buildDigest(structHash).Bytes(), signatureBytes)
			require.NoError(t, err)
			require.Equal(t, service.GetSignerAddress(), crypto.PubkeyToAddress(*publicKey))

			logEntry, err := signatureRepo.GetByRequestID(context.Background(), response.RequestID)
			require.NoError(t, err)
			require.Equal(t, strings.ToUpper(tt.market), logEntry.Market)
			require.Equal(t, tt.expectedCap, logEntry.AmountCap)
		})
	}
}

func TestSignRejectsUnsupportedMarketAndOracleFailure(t *testing.T) {
	service, _, reader := newAmountCapSigner(t, amountCapUSDC, "USDC", 6, "1000000000000000000", nil)
	_, err := service.Sign(context.Background(), &SignatureRequest{
		User:   common.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		Market: "UNKNOWN",
		Amount: big.NewInt(1),
	})
	require.ErrorIs(t, err, ErrUnsupportedMarket)
	require.Zero(t, reader.calls)

	oracleErr := errors.New("eth_call failed")
	service, _, reader = newAmountCapSigner(t, amountCapUSDC, "USDC", 6, "1000000000000000000", oracleErr)
	_, err = service.Sign(context.Background(), &SignatureRequest{
		User:   common.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc"),
		Market: "USDC",
		Amount: big.NewInt(1),
	})
	require.ErrorIs(t, err, ErrPriceUnavailable)
	require.ErrorIs(t, err, oracleErr)
	require.Equal(t, 1, reader.calls)
}

func TestSignRejectsZeroAmountBeforeReadingOracle(t *testing.T) {
	service, _, reader := newAmountCapSigner(t, amountCapUSDC, "USDC", 6, "1000000000000000000", nil)
	_, err := service.Sign(context.Background(), &SignatureRequest{
		User:   common.HexToAddress("0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"),
		Market: "USDC",
		Amount: big.NewInt(0),
	})
	require.ErrorIs(t, err, ErrInvalidAmount)
	require.Zero(t, reader.calls)
}

func TestSignChecksRequestedRawAmountAgainstConvertedTokenCap(t *testing.T) {
	service, _, reader := newAmountCapSigner(t, amountCapUSDC, "USDC", 6, "1000000000000000000", nil)
	_, err := service.Sign(context.Background(), &SignatureRequest{
		User:   common.HexToAddress("0xdddddddddddddddddddddddddddddddddddddddd"),
		Market: "USDC",
		Amount: big.NewInt(10_000_000_001),
	})
	require.ErrorIs(t, err, ErrAmountExceedsLimit)
	require.Equal(t, 1, reader.calls)
}

func newAmountCapSigner(
	t *testing.T,
	assetAddress, symbol string,
	decimals uint8,
	priceValue string,
	oracleErr error,
) (*Service, *repository.SignatureRepository, *amountCapPriceReader) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.User{},
		&models.UserCredit{},
		&models.CreditFactor{},
		&models.LoanActivity{},
		&models.SignatureLog{},
		&models.SignatureNonceCounter{},
		&models.CurrentRiskParams{},
	))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })

	userRepo := repository.NewUserRepository(db)
	activityRepo := repository.NewActivityRepository(db)
	creditEngine := credit.NewEngine(userRepo, repository.NewCreditRepository(db), activityRepo)
	signatureRepo := repository.NewSignatureRepository(db)
	riskRepo := repository.NewRiskRepository(db)
	require.NoError(t, riskRepo.SaveRiskParams(context.Background(), &models.CurrentRiskParams{
		AssetAddress:       assetAddress,
		AssetSymbol:        symbol,
		Decimals:           decimals,
		PriceOracleAddress: amountCapOracle,
	}))
	priceInt, ok := new(big.Int).SetString(priceValue, 10)
	require.True(t, ok)
	reader := &amountCapPriceReader{prices: map[string]*big.Int{strings.ToLower(assetAddress): priceInt}, err: oracleErr}
	priceService := priceservice.NewService(riskRepo, reader)
	require.NoError(t, priceService.LoadAssets(context.Background()))

	service, err := NewService(&Config{
		PrivateKeyHex:    testPrivateKeyHex,
		ChainID:          1,
		LendingPoolAddr:  "0x9999999999999999999999999999999999999999",
		SignatureTTL:     300,
		RateLimitPerUser: 100,
		RateLimitPerIP:   100,
	}, creditEngine, signatureRepo, userRepo, priceService)
	require.NoError(t, err)
	return service, signatureRepo, reader
}
