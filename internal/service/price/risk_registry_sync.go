package price

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/creditlink/backend/internal/models"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

const riskRegistryABI = `[
  {"inputs":[],"name":"getAssetsList","outputs":[{"internalType":"address[]","name":"","type":"address[]"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"internalType":"address","name":"asset","type":"address"}],"name":"getRiskParams","outputs":[{"components":[{"internalType":"uint16","name":"baseLtv","type":"uint16"},{"internalType":"uint16","name":"liquidationThreshold","type":"uint16"},{"internalType":"uint16","name":"liquidationBonus","type":"uint16"},{"internalType":"uint16","name":"closeFactor","type":"uint16"},{"internalType":"uint256","name":"borrowCap","type":"uint256"},{"internalType":"uint256","name":"supplyCap","type":"uint256"},{"internalType":"address","name":"priceOracle","type":"address"},{"internalType":"uint8","name":"decimals","type":"uint8"},{"internalType":"bool","name":"borrowEnabled","type":"bool"},{"internalType":"bool","name":"collateralEnabled","type":"bool"},{"internalType":"uint32","name":"version","type":"uint32"},{"internalType":"uint64","name":"updatedAt","type":"uint64"}],"internalType":"struct IRiskRegistry.RiskParams","name":"","type":"tuple"}],"stateMutability":"view","type":"function"}
]`

const erc20MetadataABI = `[{"inputs":[],"name":"symbol","outputs":[{"internalType":"string","name":"","type":"string"}],"stateMutability":"view","type":"function"}]`

type chainRiskParams struct {
	BaseLtv              uint16
	LiquidationThreshold uint16
	LiquidationBonus     uint16
	CloseFactor          uint16
	BorrowCap            *big.Int
	SupplyCap            *big.Int
	PriceOracle          common.Address
	Decimals             uint8
	BorrowEnabled        bool
	CollateralEnabled    bool
	Version              uint32
	UpdatedAt            uint64
}

// RiskRegistryReader reads the canonical asset metadata and risk parameters from chain.
type RiskRegistryReader interface {
	ReadRiskParams(ctx context.Context, registryAddress string) ([]models.CurrentRiskParams, error)
}

type riskParamsWriter interface {
	ReplaceRiskParams(ctx context.Context, params []models.CurrentRiskParams) error
}

type blockPinnedContractCaller interface {
	contractCaller
	BlockNumber(ctx context.Context) (uint64, error)
}

// EthereumRiskRegistryReader uses read-only eth_call requests.
type EthereumRiskRegistryReader struct {
	caller      blockPinnedContractCaller
	registryABI abi.ABI
	erc20ABI    abi.ABI
}

func NewEthereumRiskRegistryReader(ctx context.Context, rpcURL string) (*EthereumRiskRegistryReader, error) {
	if strings.TrimSpace(rpcURL) == "" {
		return nil, fmt.Errorf("%w: chain.rpc_url is empty", ErrPriceReaderUnavailable)
	}
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, fmt.Errorf("connect RiskRegistry RPC: %w", err)
	}
	return newEthereumRiskRegistryReader(client)
}

func newEthereumRiskRegistryReader(caller blockPinnedContractCaller) (*EthereumRiskRegistryReader, error) {
	registryContractABI, err := abi.JSON(strings.NewReader(riskRegistryABI))
	if err != nil {
		return nil, fmt.Errorf("parse RiskRegistry ABI: %w", err)
	}
	erc20ContractABI, err := abi.JSON(strings.NewReader(erc20MetadataABI))
	if err != nil {
		return nil, fmt.Errorf("parse ERC20 metadata ABI: %w", err)
	}
	return &EthereumRiskRegistryReader{caller: caller, registryABI: registryContractABI, erc20ABI: erc20ContractABI}, nil
}

func (r *EthereumRiskRegistryReader) ReadRiskParams(ctx context.Context, registryAddress string) ([]models.CurrentRiskParams, error) {
	if r == nil || r.caller == nil {
		return nil, ErrPriceReaderUnavailable
	}
	if !common.IsHexAddress(registryAddress) || common.HexToAddress(registryAddress) == (common.Address{}) {
		return nil, fmt.Errorf("invalid RiskRegistry address")
	}
	registry := common.HexToAddress(registryAddress)
	block, err := r.caller.BlockNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("read RiskRegistry snapshot block: %w", err)
	}
	blockNumber := new(big.Int).SetUint64(block)

	assetsData, err := r.call(ctx, blockNumber, registry, r.registryABI, "getAssetsList")
	if err != nil {
		return nil, fmt.Errorf("read RiskRegistry assets: %w", err)
	}
	assetsValues, err := r.registryABI.Unpack("getAssetsList", assetsData)
	if err != nil {
		return nil, fmt.Errorf("decode RiskRegistry assets: %w", err)
	}
	if len(assetsValues) != 1 {
		return nil, fmt.Errorf("decode RiskRegistry assets: expected 1 value, got %d", len(assetsValues))
	}
	assets, ok := assetsValues[0].([]common.Address)
	if !ok {
		return nil, fmt.Errorf("decode RiskRegistry assets: invalid address list")
	}
	if len(assets) == 0 {
		return nil, fmt.Errorf("RiskRegistry has no configured assets")
	}

	params := make([]models.CurrentRiskParams, 0, len(assets))
	for _, asset := range assets {
		if asset == (common.Address{}) {
			return nil, fmt.Errorf("RiskRegistry contains zero asset address")
		}
		paramsData, err := r.call(ctx, blockNumber, registry, r.registryABI, "getRiskParams", asset)
		if err != nil {
			return nil, fmt.Errorf("read risk params for %s: %w", asset.Hex(), err)
		}
		values, err := r.registryABI.Unpack("getRiskParams", paramsData)
		if err != nil {
			return nil, fmt.Errorf("decode risk params for %s: %w", asset.Hex(), err)
		}
		if len(values) != 1 {
			return nil, fmt.Errorf("decode risk params for %s: expected 1 value, got %d", asset.Hex(), len(values))
		}
		chainParams, ok := abi.ConvertType(values[0], new(chainRiskParams)).(*chainRiskParams)
		if !ok || chainParams == nil || chainParams.BorrowCap == nil || chainParams.SupplyCap == nil {
			return nil, fmt.Errorf("decode risk params for %s: invalid tuple", asset.Hex())
		}
		if chainParams.PriceOracle == (common.Address{}) {
			return nil, fmt.Errorf("risk params for %s contain zero price oracle", asset.Hex())
		}

		symbolData, err := r.call(ctx, blockNumber, asset, r.erc20ABI, "symbol")
		if err != nil {
			return nil, fmt.Errorf("read symbol for %s: %w", asset.Hex(), err)
		}
		symbolValues, err := r.erc20ABI.Unpack("symbol", symbolData)
		if err != nil {
			return nil, fmt.Errorf("decode symbol for %s: %w", asset.Hex(), err)
		}
		if len(symbolValues) != 1 {
			return nil, fmt.Errorf("decode symbol for %s: expected 1 value, got %d", asset.Hex(), len(symbolValues))
		}
		symbol, ok := symbolValues[0].(string)
		if !ok || strings.TrimSpace(symbol) == "" {
			return nil, fmt.Errorf("decode symbol for %s: empty symbol", asset.Hex())
		}

		params = append(params, models.CurrentRiskParams{
			AssetAddress:         strings.ToLower(asset.Hex()),
			AssetSymbol:          symbol,
			Decimals:             chainParams.Decimals,
			PriceOracleAddress:   strings.ToLower(chainParams.PriceOracle.Hex()),
			BaseLTV:              int(chainParams.BaseLtv),
			LiquidationThreshold: int(chainParams.LiquidationThreshold),
			LiquidationBonus:     int(chainParams.LiquidationBonus),
			CloseFactor:          int(chainParams.CloseFactor),
			BorrowCap:            chainParams.BorrowCap.String(),
			SupplyCap:            chainParams.SupplyCap.String(),
			BorrowEnabled:        chainParams.BorrowEnabled,
			CollateralEnabled:    chainParams.CollateralEnabled,
			Version:              int(chainParams.Version),
			UpdatedAt:            time.Unix(int64(chainParams.UpdatedAt), 0),
		})
	}
	return params, nil
}

func (r *EthereumRiskRegistryReader) call(ctx context.Context, blockNumber *big.Int, target common.Address, contractABI abi.ABI, method string, args ...interface{}) ([]byte, error) {
	data, err := contractABI.Pack(method, args...)
	if err != nil {
		return nil, fmt.Errorf("pack %s: %w", method, err)
	}
	return r.caller.CallContract(ctx, ethereum.CallMsg{To: &target, Data: data}, blockNumber)
}

// SyncRiskParams atomically mirrors the canonical RiskRegistry snapshot into the database.
func SyncRiskParams(ctx context.Context, writer riskParamsWriter, reader RiskRegistryReader, registryAddress string) error {
	if writer == nil || reader == nil {
		return fmt.Errorf("risk params sync dependencies unavailable")
	}
	params, err := reader.ReadRiskParams(ctx, registryAddress)
	if err != nil {
		return err
	}
	return writer.ReplaceRiskParams(ctx, params)
}
