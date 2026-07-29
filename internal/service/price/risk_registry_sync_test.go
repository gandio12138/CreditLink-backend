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

const testRegistry = "0x3333333333333333333333333333333333333333"

type routingContractCaller struct {
	responses       map[string][]byte
	err             error
	block           uint64
	requestedBlocks []*big.Int
}

func (c *routingContractCaller) BlockNumber(context.Context) (uint64, error) {
	if c.err != nil {
		return 0, c.err
	}
	return c.block, nil
}

func (c *routingContractCaller) CallContract(_ context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	if c.err != nil {
		return nil, c.err
	}
	c.requestedBlocks = append(c.requestedBlocks, new(big.Int).Set(blockNumber))
	if len(call.Data) < 4 {
		return nil, errors.New("missing selector")
	}
	return c.responses[string(call.Data[:4])], nil
}

func TestEthereumRiskRegistryReaderLoadsAssetsParamsAndSymbol(t *testing.T) {
	caller := &routingContractCaller{responses: make(map[string][]byte), block: 1234}
	reader, err := newEthereumRiskRegistryReader(caller)
	require.NoError(t, err)

	assetsOutput, err := reader.registryABI.Methods["getAssetsList"].Outputs.Pack([]common.Address{common.HexToAddress(testAsset)})
	require.NoError(t, err)
	riskOutput, err := reader.registryABI.Methods["getRiskParams"].Outputs.Pack(chainRiskParams{
		BaseLtv:              7500,
		LiquidationThreshold: 8000,
		LiquidationBonus:     500,
		CloseFactor:          5000,
		BorrowCap:            big.NewInt(1_000_000),
		SupplyCap:            big.NewInt(2_000_000),
		PriceOracle:          common.HexToAddress(testOracle),
		Decimals:             6,
		BorrowEnabled:        true,
		CollateralEnabled:    true,
		Version:              3,
		UpdatedAt:            1_700_000_000,
	})
	require.NoError(t, err)
	symbolOutput, err := reader.erc20ABI.Methods["symbol"].Outputs.Pack("USDC")
	require.NoError(t, err)
	caller.responses[string(reader.registryABI.Methods["getAssetsList"].ID)] = assetsOutput
	caller.responses[string(reader.registryABI.Methods["getRiskParams"].ID)] = riskOutput
	caller.responses[string(reader.erc20ABI.Methods["symbol"].ID)] = symbolOutput

	params, err := reader.ReadRiskParams(context.Background(), testRegistry)
	require.NoError(t, err)
	require.Len(t, params, 1)
	require.Equal(t, "USDC", params[0].AssetSymbol)
	require.Equal(t, uint8(6), params[0].Decimals)
	require.Equal(t, 7500, params[0].BaseLTV)
	require.Equal(t, "1000000", params[0].BorrowCap)
	require.Equal(t, strings.ToLower(testOracle), params[0].PriceOracleAddress)
	require.Equal(t, 3, params[0].Version)
	require.Len(t, caller.requestedBlocks, 3)
	for _, requestedBlock := range caller.requestedBlocks {
		require.Equal(t, "1234", requestedBlock.String())
	}
}

func TestEthereumRiskRegistryReaderRejectsEmptyRegistry(t *testing.T) {
	caller := &routingContractCaller{responses: make(map[string][]byte), block: 1234}
	reader, err := newEthereumRiskRegistryReader(caller)
	require.NoError(t, err)
	assetsOutput, err := reader.registryABI.Methods["getAssetsList"].Outputs.Pack([]common.Address{})
	require.NoError(t, err)
	caller.responses[string(reader.registryABI.Methods["getAssetsList"].ID)] = assetsOutput

	_, err = reader.ReadRiskParams(context.Background(), testRegistry)
	require.ErrorContains(t, err, "no configured assets")
}

type stubRiskParamsWriter struct {
	saved []models.CurrentRiskParams
	err   error
}

func (w *stubRiskParamsWriter) ReplaceRiskParams(_ context.Context, params []models.CurrentRiskParams) error {
	if w.err != nil {
		return w.err
	}
	w.saved = append(w.saved, params...)
	return nil
}

type stubRiskRegistryReader struct {
	params []models.CurrentRiskParams
	err    error
}

func (r *stubRiskRegistryReader) ReadRiskParams(context.Context, string) ([]models.CurrentRiskParams, error) {
	return r.params, r.err
}

func TestSyncRiskParamsPersistsOnChainMetadata(t *testing.T) {
	writer := &stubRiskParamsWriter{}
	reader := &stubRiskRegistryReader{params: []models.CurrentRiskParams{{
		AssetAddress:       testAsset,
		AssetSymbol:        "USDC",
		Decimals:           6,
		PriceOracleAddress: testOracle,
	}}}

	err := SyncRiskParams(context.Background(), writer, reader, testRegistry)
	require.NoError(t, err)
	require.Len(t, writer.saved, 1)
	require.Equal(t, strings.ToLower(testAsset), writer.saved[0].AssetAddress)
	require.Equal(t, strings.ToLower(testOracle), writer.saved[0].PriceOracleAddress)
}
