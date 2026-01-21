package indexer

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"strings"
	"time"

	"github.com/creditlink/backend/internal/models"
	"github.com/creditlink/backend/internal/repository"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Event signatures
var (
	DepositEventSig     = crypto.Keccak256Hash([]byte("Deposit(address,address,uint256)"))
	WithdrawEventSig    = crypto.Keccak256Hash([]byte("Withdraw(address,address,uint256)"))
	BorrowEventSig      = crypto.Keccak256Hash([]byte("Borrow(address,address,uint256,uint256,uint256)"))
	RepayEventSig       = crypto.Keccak256Hash([]byte("Repay(address,address,uint256)"))
	LiquidationEventSig = crypto.Keccak256Hash([]byte("Liquidation(address,address,address,address,uint256,uint256,uint256,uint256)"))
)

// Config for the indexer
type Config struct {
	RPCURL         string
	ChainID        int64
	LendingPool    string
	StartBlock     uint64
	BlockBatchSize uint64
	PollInterval   time.Duration
}

// Indexer indexes on-chain events
type Indexer struct {
	client       *ethclient.Client
	lendingPool  common.Address
	startBlock   uint64
	batchSize    uint64
	pollInterval time.Duration

	userRepo     *repository.UserRepository
	activityRepo *repository.ActivityRepository
	riskRepo     *repository.RiskRepository
}

// NewIndexer creates a new indexer
func NewIndexer(
	cfg *Config,
	userRepo *repository.UserRepository,
	activityRepo *repository.ActivityRepository,
	riskRepo *repository.RiskRepository,
) (*Indexer, error) {
	client, err := ethclient.Dial(cfg.RPCURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RPC: %w", err)
	}

	batchSize := cfg.BlockBatchSize
	if batchSize == 0 {
		batchSize = 1000
	}

	pollInterval := cfg.PollInterval
	if pollInterval == 0 {
		pollInterval = 12 * time.Second
	}

	return &Indexer{
		client:       client,
		lendingPool:  common.HexToAddress(cfg.LendingPool),
		startBlock:   cfg.StartBlock,
		batchSize:    batchSize,
		pollInterval: pollInterval,
		userRepo:     userRepo,
		activityRepo: activityRepo,
		riskRepo:     riskRepo,
	}, nil
}

// Start starts the indexer
func (i *Indexer) Start(ctx context.Context) error {
	log.Printf("Starting indexer for LendingPool at %s", i.lendingPool.Hex())

	// Get last indexed block
	state, err := i.riskRepo.GetIndexerState(ctx, i.lendingPool.Hex())
	if err != nil {
		return err
	}

	var fromBlock uint64
	if state != nil {
		fromBlock = state.LastBlockNumber + 1
	} else {
		fromBlock = i.startBlock
	}

	log.Printf("Starting indexing from block %d", fromBlock)

	// Index historical blocks
	if err := i.indexHistorical(ctx, fromBlock); err != nil {
		return err
	}

	// Start real-time indexing
	return i.watchNewBlocks(ctx)
}

// indexHistorical indexes historical blocks
func (i *Indexer) indexHistorical(ctx context.Context, fromBlock uint64) error {
	latestBlock, err := i.client.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("failed to get latest block: %w", err)
	}

	log.Printf("Indexing historical blocks from %d to %d", fromBlock, latestBlock)

	for currentBlock := fromBlock; currentBlock <= latestBlock; currentBlock += i.batchSize {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		toBlock := currentBlock + i.batchSize - 1
		if toBlock > latestBlock {
			toBlock = latestBlock
		}

		if err := i.indexBlockRange(ctx, currentBlock, toBlock); err != nil {
			log.Printf("Error indexing blocks %d-%d: %v", currentBlock, toBlock, err)
			continue
		}

		// Save progress
		if err := i.saveProgress(ctx, toBlock); err != nil {
			log.Printf("Error saving progress: %v", err)
		}
	}

	log.Println("Historical indexing complete")
	return nil
}

// watchNewBlocks watches for new blocks in real-time
func (i *Indexer) watchNewBlocks(ctx context.Context) error {
	ticker := time.NewTicker(i.pollInterval)
	defer ticker.Stop()

	var lastBlock uint64

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			latestBlock, err := i.client.BlockNumber(ctx)
			if err != nil {
				log.Printf("Error getting latest block: %v", err)
				continue
			}

			if lastBlock == 0 {
				state, _ := i.riskRepo.GetIndexerState(ctx, i.lendingPool.Hex())
				if state != nil {
					lastBlock = state.LastBlockNumber
				}
			}

			if latestBlock > lastBlock {
				if err := i.indexBlockRange(ctx, lastBlock+1, latestBlock); err != nil {
					log.Printf("Error indexing blocks %d-%d: %v", lastBlock+1, latestBlock, err)
					continue
				}
				if err := i.saveProgress(ctx, latestBlock); err != nil {
					log.Printf("Error saving progress: %v", err)
				}
				lastBlock = latestBlock
			}
		}
	}
}

// indexBlockRange indexes events in a block range
func (i *Indexer) indexBlockRange(ctx context.Context, fromBlock, toBlock uint64) error {
	query := ethereum.FilterQuery{
		FromBlock: big.NewInt(int64(fromBlock)),
		ToBlock:   big.NewInt(int64(toBlock)),
		Addresses: []common.Address{i.lendingPool},
		Topics: [][]common.Hash{{
			DepositEventSig,
			WithdrawEventSig,
			BorrowEventSig,
			RepayEventSig,
			LiquidationEventSig,
		}},
	}

	logs, err := i.client.FilterLogs(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to filter logs: %w", err)
	}

	for _, vLog := range logs {
		if err := i.processLog(ctx, vLog); err != nil {
			log.Printf("Error processing log: %v", err)
		}
	}

	return nil
}

// processLog processes a single log entry
func (i *Indexer) processLog(ctx context.Context, vLog types.Log) error {
	switch vLog.Topics[0] {
	case DepositEventSig:
		return i.handleDeposit(ctx, vLog)
	case WithdrawEventSig:
		return i.handleWithdraw(ctx, vLog)
	case BorrowEventSig:
		return i.handleBorrow(ctx, vLog)
	case RepayEventSig:
		return i.handleRepay(ctx, vLog)
	case LiquidationEventSig:
		return i.handleLiquidation(ctx, vLog)
	}
	return nil
}

// handleDeposit handles Deposit events
func (i *Indexer) handleDeposit(ctx context.Context, vLog types.Log) error {
	if len(vLog.Topics) < 3 {
		return fmt.Errorf("invalid deposit event")
	}

	user := common.BytesToAddress(vLog.Topics[1].Bytes())
	asset := common.BytesToAddress(vLog.Topics[2].Bytes())
	amount := new(big.Int).SetBytes(vLog.Data)

	return i.saveActivity(ctx, vLog, user, asset, amount, models.ActionDeposit, nil)
}

// handleWithdraw handles Withdraw events
func (i *Indexer) handleWithdraw(ctx context.Context, vLog types.Log) error {
	if len(vLog.Topics) < 3 {
		return fmt.Errorf("invalid withdraw event")
	}

	user := common.BytesToAddress(vLog.Topics[1].Bytes())
	asset := common.BytesToAddress(vLog.Topics[2].Bytes())
	amount := new(big.Int).SetBytes(vLog.Data)

	return i.saveActivity(ctx, vLog, user, asset, amount, models.ActionWithdraw, nil)
}

// handleBorrow handles Borrow events
func (i *Indexer) handleBorrow(ctx context.Context, vLog types.Log) error {
	if len(vLog.Topics) < 3 {
		return fmt.Errorf("invalid borrow event")
	}

	user := common.BytesToAddress(vLog.Topics[1].Bytes())
	asset := common.BytesToAddress(vLog.Topics[2].Bytes())

	// Parse data: amount (32) + ltv (32) + nonce (32)
	if len(vLog.Data) < 96 {
		return fmt.Errorf("invalid borrow event data")
	}

	amount := new(big.Int).SetBytes(vLog.Data[0:32])
	ltv := new(big.Int).SetBytes(vLog.Data[32:64]).Int64()
	nonce := new(big.Int).SetBytes(vLog.Data[64:96]).Uint64()

	ltvInt := int(ltv)
	extras := map[string]interface{}{
		"ltv":   &ltvInt,
		"nonce": &nonce,
	}

	return i.saveActivity(ctx, vLog, user, asset, amount, models.ActionBorrow, extras)
}

// handleRepay handles Repay events
func (i *Indexer) handleRepay(ctx context.Context, vLog types.Log) error {
	if len(vLog.Topics) < 3 {
		return fmt.Errorf("invalid repay event")
	}

	user := common.BytesToAddress(vLog.Topics[1].Bytes())
	asset := common.BytesToAddress(vLog.Topics[2].Bytes())
	amount := new(big.Int).SetBytes(vLog.Data)

	return i.saveActivity(ctx, vLog, user, asset, amount, models.ActionRepay, nil)
}

// handleLiquidation handles Liquidation events
func (i *Indexer) handleLiquidation(ctx context.Context, vLog types.Log) error {
	if len(vLog.Topics) < 4 {
		return fmt.Errorf("invalid liquidation event")
	}

	liquidator := common.BytesToAddress(vLog.Topics[1].Bytes())
	borrower := common.BytesToAddress(vLog.Topics[2].Bytes())
	collateralAsset := common.BytesToAddress(vLog.Topics[3].Bytes())

	// Parse data: debtAsset (32) + debtCovered (32) + collateralSeized (32) + ...
	if len(vLog.Data) < 64 {
		return fmt.Errorf("invalid liquidation event data")
	}

	debtAsset := common.BytesToAddress(vLog.Data[0:32])
	debtCovered := new(big.Int).SetBytes(vLog.Data[32:64])

	liquidatorStr := liquidator.Hex()
	collateralStr := collateralAsset.Hex()
	debtCoveredStr := debtCovered.String()

	extras := map[string]interface{}{
		"liquidator":  &liquidatorStr,
		"collateral":  &collateralStr,
		"debtCovered": &debtCoveredStr,
	}

	// Amount is the debt covered
	return i.saveActivity(ctx, vLog, borrower, debtAsset, debtCovered, models.ActionLiquidation, extras)
}

// saveActivity saves a loan activity to the database
func (i *Indexer) saveActivity(
	ctx context.Context,
	vLog types.Log,
	user common.Address,
	asset common.Address,
	amount *big.Int,
	actionType models.ActionType,
	extras map[string]interface{},
) error {
	// Get or create user
	userModel, err := i.userRepo.GetOrCreate(ctx, strings.ToLower(user.Hex()))
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	// Get block timestamp
	block, err := i.client.BlockByNumber(ctx, big.NewInt(int64(vLog.BlockNumber)))
	if err != nil {
		return fmt.Errorf("failed to get block: %w", err)
	}
	blockTime := time.Unix(int64(block.Time()), 0)

	activity := &models.LoanActivity{
		UserID:         userModel.ID,
		TxHash:         vLog.TxHash.Hex(),
		ActionType:     actionType,
		AssetAddress:   strings.ToLower(asset.Hex()),
		Amount:         amount.String(),
		BlockNumber:    vLog.BlockNumber,
		BlockTimestamp: blockTime,
	}

	// Add extras
	if extras != nil {
		if ltv, ok := extras["ltv"].(*int); ok {
			activity.LTV = ltv
		}
		if nonce, ok := extras["nonce"].(*uint64); ok {
			activity.Nonce = nonce
		}
		if liquidator, ok := extras["liquidator"].(*string); ok {
			activity.Liquidator = liquidator
		}
		if collateral, ok := extras["collateral"].(*string); ok {
			activity.Collateral = collateral
		}
		if debtCovered, ok := extras["debtCovered"].(*string); ok {
			activity.DebtCovered = debtCovered
		}
	}

	if err := i.activityRepo.Create(ctx, activity); err != nil {
		if err == repository.ErrActivityExists {
			return nil // Already indexed
		}
		return fmt.Errorf("failed to save activity: %w", err)
	}

	log.Printf("Indexed %s event: user=%s, asset=%s, amount=%s, block=%d",
		actionType, user.Hex(), asset.Hex(), amount.String(), vLog.BlockNumber)

	return nil
}

// saveProgress saves the indexer progress
func (i *Indexer) saveProgress(ctx context.Context, blockNumber uint64) error {
	state := &models.IndexerState{
		ContractAddress: strings.ToLower(i.lendingPool.Hex()),
		LastBlockNumber: blockNumber,
		UpdatedAt:       time.Now(),
	}
	return i.riskRepo.SaveIndexerState(ctx, state)
}

// Stop stops the indexer
func (i *Indexer) Stop() {
	if i.client != nil {
		i.client.Close()
	}
}
