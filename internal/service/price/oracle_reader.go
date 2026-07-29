package price

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

const priceOracleABI = `[{"inputs":[{"internalType":"address","name":"asset","type":"address"}],"name":"getPrice","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"}]`

// OraclePriceReader reads the 18-decimal USD price exposed by PriceOracleAdapter.
type OraclePriceReader interface {
	ReadPrice(ctx context.Context, oracleAddress, assetAddress string) (*big.Int, error)
}

type contractCaller interface {
	CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
}

// EthereumOraclePriceReader performs read-only eth_call requests against PriceOracleAdapter.
type EthereumOraclePriceReader struct {
	caller contractCaller
	abi    abi.ABI
}

// NewEthereumOraclePriceReader connects to the configured EVM RPC endpoint.
func NewEthereumOraclePriceReader(ctx context.Context, rpcURL string) (*EthereumOraclePriceReader, error) {
	if strings.TrimSpace(rpcURL) == "" {
		return nil, fmt.Errorf("%w: chain.rpc_url is empty", ErrPriceReaderUnavailable)
	}

	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, fmt.Errorf("%w: connect RPC: %v", ErrPriceReaderUnavailable, err)
	}
	return newEthereumOraclePriceReader(client)
}

func newEthereumOraclePriceReader(caller contractCaller) (*EthereumOraclePriceReader, error) {
	parsedABI, err := abi.JSON(strings.NewReader(priceOracleABI))
	if err != nil {
		return nil, fmt.Errorf("parse price oracle ABI: %w", err)
	}
	return &EthereumOraclePriceReader{caller: caller, abi: parsedABI}, nil
}

// ReadPrice calls getPrice(asset); the adapter normalizes valid prices to 18 decimals.
func (r *EthereumOraclePriceReader) ReadPrice(ctx context.Context, oracleAddress, assetAddress string) (*big.Int, error) {
	if r == nil || r.caller == nil {
		return nil, ErrPriceReaderUnavailable
	}
	if !common.IsHexAddress(oracleAddress) || common.HexToAddress(oracleAddress) == (common.Address{}) {
		return nil, fmt.Errorf("%w: invalid oracle address", ErrOracleNotConfigured)
	}
	if !common.IsHexAddress(assetAddress) || common.HexToAddress(assetAddress) == (common.Address{}) {
		return nil, fmt.Errorf("%w: invalid asset address", ErrAssetNotFound)
	}

	data, err := r.abi.Pack("getPrice", common.HexToAddress(assetAddress))
	if err != nil {
		return nil, fmt.Errorf("pack getPrice call: %w", err)
	}
	oracle := common.HexToAddress(oracleAddress)
	output, err := r.caller.CallContract(ctx, ethereum.CallMsg{To: &oracle, Data: data}, nil)
	if err != nil {
		return nil, fmt.Errorf("eth_call getPrice: %w", err)
	}

	values, err := r.abi.Unpack("getPrice", output)
	if err != nil {
		return nil, fmt.Errorf("decode getPrice response: %w", err)
	}
	if len(values) != 1 {
		return nil, fmt.Errorf("decode getPrice response: expected 1 value, got %d", len(values))
	}
	priceValue, ok := values[0].(*big.Int)
	if !ok || priceValue == nil || priceValue.Sign() <= 0 {
		return nil, fmt.Errorf("decode getPrice response: invalid price")
	}
	return new(big.Int).Set(priceValue), nil
}
