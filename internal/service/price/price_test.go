package price

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/creditlink/backend/internal/models"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

const (
	testAsset  = "0x1111111111111111111111111111111111111111"
	testOracle = "0x2222222222222222222222222222222222222222"
)

type stubRiskRepository struct {
	params []models.CurrentRiskParams
	err    error
}

func (r *stubRiskRepository) GetAllRiskParams(context.Context) ([]models.CurrentRiskParams, error) {
	return r.params, r.err
}

type stubPriceReader struct {
	price         *big.Int
	err           error
	oracleAddress string
	assetAddress  string
	calls         int
}

func (r *stubPriceReader) ReadPrice(_ context.Context, oracleAddress, assetAddress string) (*big.Int, error) {
	r.calls++
	r.oracleAddress = oracleAddress
	r.assetAddress = assetAddress
	if r.err != nil {
		return nil, r.err
	}
	return new(big.Int).Set(r.price), nil
}

func TestLoadAssetsRejectsMissingOracleWithoutFallback(t *testing.T) {
	repo := &stubRiskRepository{params: []models.CurrentRiskParams{{
		AssetAddress: testAsset,
		AssetSymbol:  "USDC",
		Decimals:     6,
	}}}
	service := NewService(repo, &stubPriceReader{price: big.NewInt(1e18)})

	err := service.LoadAssets(context.Background())
	require.ErrorIs(t, err, ErrOracleNotConfigured)
	_, err = service.GetPrice(context.Background(), testAsset)
	require.ErrorIs(t, err, ErrPriceReaderUnavailable)
}

func TestLoadAssetsRejectsDuplicateSymbols(t *testing.T) {
	repo := &stubRiskRepository{params: []models.CurrentRiskParams{
		{
			AssetAddress:       testAsset,
			AssetSymbol:        "USDC",
			Decimals:           6,
			PriceOracleAddress: testOracle,
		},
		{
			AssetAddress:       "0x4444444444444444444444444444444444444444",
			AssetSymbol:        " usdc ",
			Decimals:           18,
			PriceOracleAddress: testOracle,
		},
	}}
	service := NewService(repo, &stubPriceReader{price: big.NewInt(1e18)})

	err := service.LoadAssets(context.Background())
	require.ErrorContains(t, err, "duplicate asset symbol USDC")
	_, err = service.GetPrice(context.Background(), "USDC")
	require.ErrorIs(t, err, ErrPriceReaderUnavailable)
}

func TestGetPriceReadsConfiguredOracleEveryTime(t *testing.T) {
	repo := &stubRiskRepository{params: []models.CurrentRiskParams{{
		AssetAddress:       testAsset,
		AssetSymbol:        "USDC",
		Decimals:           6,
		PriceOracleAddress: testOracle,
	}}}
	reader := &stubPriceReader{price: big.NewInt(1234000000000000000)}
	service := NewService(repo, reader)
	require.NoError(t, service.LoadAssets(context.Background()))

	actual, err := service.GetPrice(context.Background(), "usdc")
	require.NoError(t, err)
	require.Equal(t, "1234000000000000000", actual.String())
	require.Equal(t, strings.ToLower(testOracle), reader.oracleAddress)
	require.Equal(t, strings.ToLower(testAsset), reader.assetAddress)

	reader.price = big.NewInt(2e18)
	actual, err = service.GetPrice(context.Background(), testAsset)
	require.NoError(t, err)
	require.Equal(t, "2000000000000000000", actual.String())
}

func TestGetPriceReturnsUnknownAssetAndReadFailures(t *testing.T) {
	readErr := errors.New("eth_call failed")
	repo := &stubRiskRepository{params: []models.CurrentRiskParams{{
		AssetAddress:       testAsset,
		AssetSymbol:        "USDC",
		Decimals:           6,
		PriceOracleAddress: testOracle,
	}}}
	service := NewService(repo, &stubPriceReader{err: readErr})
	require.NoError(t, service.LoadAssets(context.Background()))

	_, err := service.GetPrice(context.Background(), "UNKNOWN")
	require.ErrorIs(t, err, ErrAssetNotFound)
	_, err = service.GetPrice(context.Background(), testAsset)
	require.ErrorIs(t, err, ErrOracleReadFailed)
	require.ErrorIs(t, err, readErr)
}

func TestGetPriceReturnsUnavailableWhenRPCReaderIsMissing(t *testing.T) {
	repo := &stubRiskRepository{params: []models.CurrentRiskParams{{
		AssetAddress:       testAsset,
		AssetSymbol:        "USDC",
		Decimals:           6,
		PriceOracleAddress: testOracle,
	}}}
	service := NewService(repo)
	require.NoError(t, service.LoadAssets(context.Background()))

	_, err := service.GetPrice(context.Background(), testAsset)
	require.ErrorIs(t, err, ErrPriceReaderUnavailable)
}

func TestNewEthereumOraclePriceReaderRejectsMissingRPC(t *testing.T) {
	_, err := NewEthereumOraclePriceReader(context.Background(), "")
	require.ErrorIs(t, err, ErrPriceReaderUnavailable)
}

func TestConvertToUSDUsesTokenAndOracleDecimals(t *testing.T) {
	repo := &stubRiskRepository{params: []models.CurrentRiskParams{{
		AssetAddress:       testAsset,
		AssetSymbol:        "USDC",
		Decimals:           6,
		PriceOracleAddress: testOracle,
	}}}
	service := NewService(repo, &stubPriceReader{price: big.NewInt(1234000000000000000)})
	require.NoError(t, service.LoadAssets(context.Background()))

	actual, err := service.ConvertToUSD(context.Background(), big.NewInt(2500000), testAsset)
	require.NoError(t, err)
	require.Equal(t, "3.085", actual.Text('f', 3))
}

func TestConvertToUSDValuesUsesOneLivePriceForAllAmounts(t *testing.T) {
	repo := &stubRiskRepository{params: []models.CurrentRiskParams{{
		AssetAddress:       testAsset,
		AssetSymbol:        "USDC",
		Decimals:           6,
		PriceOracleAddress: testOracle,
	}}}
	reader := &stubPriceReader{price: big.NewInt(2e18)}
	service := NewService(repo, reader)
	require.NoError(t, service.LoadAssets(context.Background()))

	values, err := service.ConvertToUSDValues(context.Background(), testAsset, big.NewInt(2500000), big.NewInt(0))
	require.NoError(t, err)
	require.Equal(t, "5.00", values[0].Text('f', 2))
	require.Zero(t, values[1].Sign())
	require.Equal(t, 1, reader.calls)
}

type stubContractCaller struct {
	response []byte
	err      error
	call     ethereum.CallMsg
}

func (c *stubContractCaller) CallContract(_ context.Context, call ethereum.CallMsg, _ *big.Int) ([]byte, error) {
	c.call = call
	return c.response, c.err
}

func TestEthereumOraclePriceReaderDecodesUint256(t *testing.T) {
	priceValue, ok := new(big.Int).SetString("2500123456789000000000", 10)
	require.True(t, ok)
	encodedPrice := common.LeftPadBytes(priceValue.Bytes(), 32)
	caller := &stubContractCaller{response: encodedPrice}
	reader, err := newEthereumOraclePriceReader(caller)
	require.NoError(t, err)

	actual, err := reader.ReadPrice(context.Background(), testOracle, testAsset)
	require.NoError(t, err)
	require.Equal(t, "2500123456789000000000", actual.String())
	require.Equal(t, common.HexToAddress(testOracle), *caller.call.To)
	require.Len(t, caller.call.Data, 36)
}

func TestEthereumOraclePriceReaderRejectsInvalidResponse(t *testing.T) {
	reader, err := newEthereumOraclePriceReader(&stubContractCaller{response: []byte{1, 2, 3}})
	require.NoError(t, err)

	_, err = reader.ReadPrice(context.Background(), testOracle, testAsset)
	require.Error(t, err)
}
