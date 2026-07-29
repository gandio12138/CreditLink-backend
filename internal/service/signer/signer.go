package signer

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/creditlink/backend/internal/models"
	"github.com/creditlink/backend/internal/repository"
	"github.com/creditlink/backend/internal/service/credit"
	"github.com/creditlink/backend/internal/service/price"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
)

var (
	ErrInvalidPrivateKey  = errors.New("无效的私钥")
	ErrInvalidLTV         = errors.New("无效的LTV值")
	ErrRateLimitExceeded  = errors.New("请求频率超限")
	ErrInsufficientCredit = errors.New("信用分不足")
	ErrAmountExceedsLimit = errors.New("金额超出信用额度")
	ErrUnsupportedMarket  = errors.New("不支持的借贷市场")
	ErrPriceUnavailable   = errors.New("价格预言机不可用")
	ErrInvalidAmount      = errors.New("无效的借款金额")
)

// EIP-712 类型哈希
var (
	// keccak256("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)")
	DOMAIN_TYPEHASH = crypto.Keccak256Hash([]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"))

	// keccak256("Credit(address user,bytes32 market,uint256 ltv,uint256 amountCap,uint256 nonce,uint256 deadline)")
	CREDIT_TYPEHASH = crypto.Keccak256Hash([]byte("Credit(address user,bytes32 market,uint256 ltv,uint256 amountCap,uint256 nonce,uint256 deadline)"))
)

// SignatureRequest 表示信用签名请求
type SignatureRequest struct {
	User      common.Address `json:"user"`
	Market    string         `json:"market"` // 例如: "USDT", "ETH"
	Amount    *big.Int       `json:"amount"`
	IPAddress string         `json:"-"`
	UserAgent string         `json:"-"`
}

// SignatureResponse 表示签名响应
type SignatureResponse struct {
	Signature   string `json:"signature"`
	Market      string `json:"market"`
	LTV         int    `json:"ltv"`       // 基点
	AmountCap   string `json:"amountCap"` // 目标 token raw amount
	Nonce       uint64 `json:"nonce"`
	Deadline    int64  `json:"deadline"`
	CreditScore int    `json:"creditScore"`
	CreditTier  string `json:"creditTier"`
	RequestID   string `json:"requestId"`
}

// Service EIP-712签名服务
type Service struct {
	privateKey       *ecdsa.PrivateKey
	chainID          *big.Int
	lendingPool      common.Address
	domainSeparator  common.Hash
	signatureTTL     time.Duration
	rateLimitPerUser int
	rateLimitPerIP   int

	creditEngine  *credit.Engine
	signatureRepo *repository.SignatureRepository
	userRepo      *repository.UserRepository
	priceService  *price.Service
}

// Config 签名服务配置
type Config struct {
	PrivateKeyHex    string
	ChainID          int64
	LendingPoolAddr  string
	SignatureTTL     int // 秒
	RateLimitPerUser int
	RateLimitPerIP   int
}

// NewService 创建新的签名服务实例
func NewService(
	cfg *Config,
	creditEngine *credit.Engine,
	signatureRepo *repository.SignatureRepository,
	userRepo *repository.UserRepository,
	priceServices ...*price.Service,
) (*Service, error) {
	if cfg.PrivateKeyHex == "" {
		return nil, ErrInvalidPrivateKey
	}

	// 移除0x前缀（如果有）
	privateKeyHex := strings.TrimPrefix(cfg.PrivateKeyHex, "0x")
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPrivateKey, err)
	}

	lendingPool := common.HexToAddress(cfg.LendingPoolAddr)

	var priceService *price.Service
	if len(priceServices) > 0 {
		priceService = priceServices[0]
	}
	s := &Service{
		privateKey:       privateKey,
		chainID:          big.NewInt(cfg.ChainID),
		lendingPool:      lendingPool,
		signatureTTL:     time.Duration(cfg.SignatureTTL) * time.Second,
		rateLimitPerUser: cfg.RateLimitPerUser,
		rateLimitPerIP:   cfg.RateLimitPerIP,
		creditEngine:     creditEngine,
		signatureRepo:    signatureRepo,
		userRepo:         userRepo,
		priceService:     priceService,
	}

	s.domainSeparator = s.computeDomainSeparator()

	return s, nil
}

// computeDomainSeparator 计算EIP-712域分隔符
func (s *Service) computeDomainSeparator() common.Hash {
	nameHash := crypto.Keccak256Hash([]byte("CreditLink"))
	versionHash := crypto.Keccak256Hash([]byte("1"))

	data := make([]byte, 0, 160)
	data = append(data, DOMAIN_TYPEHASH.Bytes()...)
	data = append(data, nameHash.Bytes()...)
	data = append(data, versionHash.Bytes()...)
	data = append(data, common.LeftPadBytes(s.chainID.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(s.lendingPool.Bytes(), 32)...)

	return crypto.Keccak256Hash(data)
}

// Sign 生成信用授权的EIP-712签名
func (s *Service) Sign(ctx context.Context, req *SignatureRequest) (*SignatureResponse, error) {
	wallet := strings.ToLower(req.User.Hex())

	// 检查频率限制
	if err := s.checkRateLimits(ctx, wallet, req.IPAddress); err != nil {
		return nil, err
	}

	// 计算信用分
	creditScore, _, err := s.creditEngine.CalculateScore(ctx, wallet)
	if err != nil {
		return nil, fmt.Errorf("计算信用分失败: %w", err)
	}

	// 检查用户是否有借款资格（D等级不能借款）
	if creditScore.Tier == "D" {
		return nil, ErrInsufficientCredit
	}

	// 确定授权的LTV和额度上限
	ltv := creditScore.MaxLTV
	usdAmountCap, ok := new(big.Int).SetString(creditScore.MaxBorrow, 10)
	if !ok {
		return nil, fmt.Errorf("解析USD信用额度失败")
	}
	if req.Amount == nil || req.Amount.Sign() <= 0 {
		return nil, ErrInvalidAmount
	}
	if s.priceService == nil {
		return nil, fmt.Errorf("%w: %w", ErrPriceUnavailable, price.ErrPriceReaderUnavailable)
	}
	amountCap, assetInfo, err := s.priceService.ConvertUSDToTokenAmount(ctx, usdAmountCap, req.Market)
	if err != nil {
		if errors.Is(err, price.ErrAssetNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrUnsupportedMarket, req.Market)
		}
		return nil, fmt.Errorf("%w: %w", ErrPriceUnavailable, err)
	}
	canonicalMarket := assetInfo.Symbol

	// 检查请求金额是否超出限制
	if req.Amount.Cmp(amountCap) > 0 {
		return nil, ErrAmountExceedsLimit
	}

	// 在数据库中原子预留 nonce。预留后即使后续失败也不会复用。
	nonce, err := s.signatureRepo.AllocateNonce(ctx, wallet)
	if err != nil {
		return nil, fmt.Errorf("预留nonce失败: %w", err)
	}

	// 计算签名截止时间
	deadline := time.Now().Add(s.signatureTTL).Unix()
	marketHash := crypto.Keccak256Hash([]byte(canonicalMarket))

	// 构建结构哈希
	structHash := s.buildStructHash(req.User, marketHash, big.NewInt(int64(ltv)), amountCap, nonce, deadline)

	// 构建摘要
	digest := s.buildDigest(structHash)

	// 签名
	sig, err := crypto.Sign(digest.Bytes(), s.privateKey)
	if err != nil {
		return nil, fmt.Errorf("签名失败: %w", err)
	}

	// 调整v值以符合以太坊标准（27或28）
	sig[64] += 27

	// 生成请求ID
	requestID := uuid.New().String()

	// 获取用户用于记录日志
	user, err := s.userRepo.GetOrCreate(ctx, wallet)
	if err != nil {
		return nil, err
	}

	// 记录签名日志
	signatureLog := &models.SignatureLog{
		RequestID:     requestID,
		UserID:        user.ID,
		WalletAddress: wallet,
		Market:        canonicalMarket,
		AuthorizedLTV: ltv,
		AmountCap:     amountCap.String(),
		Nonce:         nonce,
		Deadline:      deadline,
		Signature:     fmt.Sprintf("0x%x", sig),
		IPAddress:     req.IPAddress,
		UserAgent:     req.UserAgent,
		CreditScore:   creditScore.Score,
		Status:        models.SignatureIssued,
	}

	if err := s.signatureRepo.Create(ctx, signatureLog); err != nil {
		return nil, fmt.Errorf("记录签名日志失败: %w", err)
	}

	return &SignatureResponse{
		Signature:   fmt.Sprintf("0x%x", sig),
		Market:      canonicalMarket,
		LTV:         ltv,
		AmountCap:   amountCap.String(),
		Nonce:       nonce,
		Deadline:    deadline,
		CreditScore: creditScore.Score,
		CreditTier:  creditScore.Tier,
		RequestID:   requestID,
	}, nil
}

// buildStructHash 构建EIP-712结构哈希
func (s *Service) buildStructHash(user common.Address, market common.Hash, ltv, amountCap *big.Int, nonce uint64, deadline int64) common.Hash {
	data := make([]byte, 0, 224)
	data = append(data, CREDIT_TYPEHASH.Bytes()...)
	data = append(data, common.LeftPadBytes(user.Bytes(), 32)...)
	data = append(data, market.Bytes()...)
	data = append(data, common.LeftPadBytes(ltv.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(amountCap.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(new(big.Int).SetUint64(nonce).Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(big.NewInt(deadline).Bytes(), 32)...)

	return crypto.Keccak256Hash(data)
}

// buildDigest 构建最终签名摘要
func (s *Service) buildDigest(structHash common.Hash) common.Hash {
	data := make([]byte, 0, 66)
	data = append(data, []byte("\x19\x01")...)
	data = append(data, s.domainSeparator.Bytes()...)
	data = append(data, structHash.Bytes()...)

	return crypto.Keccak256Hash(data)
}

// checkRateLimits 检查请求是否超出频率限制
func (s *Service) checkRateLimits(ctx context.Context, wallet, ipAddress string) error {
	since := time.Now().Add(-1 * time.Minute)

	// 检查用户频率限制
	userCount, err := s.signatureRepo.GetRecentByWallet(ctx, wallet, since)
	if err != nil {
		return err
	}
	if userCount >= int64(s.rateLimitPerUser) {
		return ErrRateLimitExceeded
	}

	// 检查IP频率限制
	if ipAddress != "" {
		ipCount, err := s.signatureRepo.GetRecentByIP(ctx, ipAddress, since)
		if err != nil {
			return err
		}
		if ipCount >= int64(s.rateLimitPerIP) {
			return ErrRateLimitExceeded
		}
	}

	return nil
}

// GetSignerAddress 返回签名者地址
func (s *Service) GetSignerAddress() common.Address {
	return crypto.PubkeyToAddress(s.privateKey.PublicKey)
}

// GetDomainSeparator 返回EIP-712域分隔符
func (s *Service) GetDomainSeparator() common.Hash {
	return s.domainSeparator
}

// MarkSignatureUsed 标记签名已使用（当借款交易确认时调用）
func (s *Service) MarkSignatureUsed(ctx context.Context, requestID string) error {
	return s.signatureRepo.MarkAsUsed(ctx, requestID)
}

// CleanupExpiredSignatures 标记过期的签名
func (s *Service) CleanupExpiredSignatures(ctx context.Context) (int64, error) {
	return s.signatureRepo.MarkExpiredSignatures(ctx)
}
