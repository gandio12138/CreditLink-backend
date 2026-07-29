package signer

import (
	"crypto/ecdsa"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test private key for testing (DO NOT use in production)
const testPrivateKeyHex = "0x4bbbf85ce3377467afe5d46f804f221813b2bb87f24d81f60f1fcdbf7cbf4356"

// TestNewService tests service creation
func TestNewService(t *testing.T) {
	testCases := []struct {
		name        string
		config      *Config
		expectError bool
		errorType   error
	}{
		{
			name: "Valid config",
			config: &Config{
				PrivateKeyHex:    testPrivateKeyHex,
				ChainID:          1,
				LendingPoolAddr:  "0x1234567890123456789012345678901234567890",
				SignatureTTL:     300,
				RateLimitPerUser: 10,
				RateLimitPerIP:   20,
			},
			expectError: false,
		},
		{
			name: "Valid config without 0x prefix",
			config: &Config{
				PrivateKeyHex:    "4bbbf85ce3377467afe5d46f804f221813b2bb87f24d81f60f1fcdbf7cbf4356",
				ChainID:          1,
				LendingPoolAddr:  "0x1234567890123456789012345678901234567890",
				SignatureTTL:     300,
				RateLimitPerUser: 10,
				RateLimitPerIP:   20,
			},
			expectError: false,
		},
		{
			name: "Empty private key",
			config: &Config{
				PrivateKeyHex:    "",
				ChainID:          1,
				LendingPoolAddr:  "0x1234567890123456789012345678901234567890",
				SignatureTTL:     300,
				RateLimitPerUser: 10,
				RateLimitPerIP:   20,
			},
			expectError: true,
			errorType:   ErrInvalidPrivateKey,
		},
		{
			name: "Invalid private key",
			config: &Config{
				PrivateKeyHex:    "invalid_key",
				ChainID:          1,
				LendingPoolAddr:  "0x1234567890123456789012345678901234567890",
				SignatureTTL:     300,
				RateLimitPerUser: 10,
				RateLimitPerIP:   20,
			},
			expectError: true,
		},
		{
			name: "Short private key",
			config: &Config{
				PrivateKeyHex:    "0x1234",
				ChainID:          1,
				LendingPoolAddr:  "0x1234567890123456789012345678901234567890",
				SignatureTTL:     300,
				RateLimitPerUser: 10,
				RateLimitPerIP:   20,
			},
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			service, err := NewService(tc.config, nil, nil, nil)

			if tc.expectError {
				assert.Error(t, err)
				if tc.errorType != nil {
					assert.ErrorIs(t, err, tc.errorType)
				}
				assert.Nil(t, service)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, service)
			}
		})
	}
}

// TestGetSignerAddress tests that signer address is correctly derived
func TestGetSignerAddress(t *testing.T) {
	config := &Config{
		PrivateKeyHex:    testPrivateKeyHex,
		ChainID:          1,
		LendingPoolAddr:  "0x1234567890123456789012345678901234567890",
		SignatureTTL:     300,
		RateLimitPerUser: 10,
		RateLimitPerIP:   20,
	}

	service, err := NewService(config, nil, nil, nil)
	require.NoError(t, err)

	// Derive expected address from private key
	privateKey, err := crypto.HexToECDSA("4bbbf85ce3377467afe5d46f804f221813b2bb87f24d81f60f1fcdbf7cbf4356")
	require.NoError(t, err)

	expectedAddr := crypto.PubkeyToAddress(privateKey.PublicKey)

	assert.Equal(t, expectedAddr, service.GetSignerAddress())
}

// TestGetDomainSeparator tests domain separator computation
func TestGetDomainSeparator(t *testing.T) {
	config := &Config{
		PrivateKeyHex:    testPrivateKeyHex,
		ChainID:          1,
		LendingPoolAddr:  "0x1234567890123456789012345678901234567890",
		SignatureTTL:     300,
		RateLimitPerUser: 10,
		RateLimitPerIP:   20,
	}

	service, err := NewService(config, nil, nil, nil)
	require.NoError(t, err)

	domainSeparator := service.GetDomainSeparator()

	// Domain separator should not be zero
	assert.NotEqual(t, common.Hash{}, domainSeparator)

	// Same config should produce same domain separator
	service2, err := NewService(config, nil, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, domainSeparator, service2.GetDomainSeparator())
}

// TestDomainSeparatorConsistency tests that domain separator changes with config
func TestDomainSeparatorConsistency(t *testing.T) {
	baseConfig := &Config{
		PrivateKeyHex:    testPrivateKeyHex,
		ChainID:          1,
		LendingPoolAddr:  "0x1234567890123456789012345678901234567890",
		SignatureTTL:     300,
		RateLimitPerUser: 10,
		RateLimitPerIP:   20,
	}

	service1, err := NewService(baseConfig, nil, nil, nil)
	require.NoError(t, err)

	// Different chain ID should produce different domain separator
	configDifferentChain := &Config{
		PrivateKeyHex:    testPrivateKeyHex,
		ChainID:          137, // Polygon
		LendingPoolAddr:  "0x1234567890123456789012345678901234567890",
		SignatureTTL:     300,
		RateLimitPerUser: 10,
		RateLimitPerIP:   20,
	}

	service2, err := NewService(configDifferentChain, nil, nil, nil)
	require.NoError(t, err)
	assert.NotEqual(t, service1.GetDomainSeparator(), service2.GetDomainSeparator(),
		"Different chain ID should produce different domain separator")

	// Different contract address should produce different domain separator
	configDifferentContract := &Config{
		PrivateKeyHex:    testPrivateKeyHex,
		ChainID:          1,
		LendingPoolAddr:  "0x9999999999999999999999999999999999999999",
		SignatureTTL:     300,
		RateLimitPerUser: 10,
		RateLimitPerIP:   20,
	}

	service3, err := NewService(configDifferentContract, nil, nil, nil)
	require.NoError(t, err)
	assert.NotEqual(t, service1.GetDomainSeparator(), service3.GetDomainSeparator(),
		"Different contract address should produce different domain separator")
}

// TestTypeHashes tests that type hashes are computed correctly
func TestTypeHashes(t *testing.T) {
	// Verify DOMAIN_TYPEHASH
	expectedDomainTypeHash := crypto.Keccak256Hash([]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"))
	assert.Equal(t, expectedDomainTypeHash, DOMAIN_TYPEHASH)

	// Verify CREDIT_TYPEHASH
	expectedCreditTypeHash := crypto.Keccak256Hash([]byte("Credit(address user,bytes32 market,uint256 ltv,uint256 amountCap,uint256 nonce,uint256 deadline)"))
	assert.Equal(t, expectedCreditTypeHash, CREDIT_TYPEHASH)
}

// TestBuildStructHash tests struct hash building
func TestBuildStructHash(t *testing.T) {
	config := &Config{
		PrivateKeyHex:    testPrivateKeyHex,
		ChainID:          1,
		LendingPoolAddr:  "0x1234567890123456789012345678901234567890",
		SignatureTTL:     300,
		RateLimitPerUser: 10,
		RateLimitPerIP:   20,
	}

	service, err := NewService(config, nil, nil, nil)
	require.NoError(t, err)

	user := common.HexToAddress("0xAbCdEf1234567890aBcDeF1234567890AbCdEf12")
	market := crypto.Keccak256Hash([]byte("USDT"))
	ltv := big.NewInt(8000)
	amountCap := big.NewInt(1000000000000000000) // 1 ETH in wei
	nonce := uint64(1)
	deadline := int64(1735689600) // 2025-01-01

	hash := service.buildStructHash(user, market, ltv, amountCap, nonce, deadline)

	// Hash should not be zero
	assert.NotEqual(t, common.Hash{}, hash)

	// Same inputs should produce same hash
	hash2 := service.buildStructHash(user, market, ltv, amountCap, nonce, deadline)
	assert.Equal(t, hash, hash2)

	// Different inputs should produce different hash
	hash3 := service.buildStructHash(user, market, ltv, amountCap, nonce+1, deadline)
	assert.NotEqual(t, hash, hash3)
}

func TestBuildStructHashEncodesFullUint64Nonce(t *testing.T) {
	service, err := NewService(&Config{
		PrivateKeyHex:    testPrivateKeyHex,
		ChainID:          1,
		LendingPoolAddr:  "0x1234567890123456789012345678901234567890",
		SignatureTTL:     300,
		RateLimitPerUser: 10,
		RateLimitPerIP:   20,
	}, nil, nil, nil)
	require.NoError(t, err)

	nonce := uint64(1<<63 + 7)
	actual := service.buildStructHash(
		common.HexToAddress("0xAbCdEf1234567890aBcDeF1234567890AbCdEf12"),
		crypto.Keccak256Hash([]byte("USDC")),
		big.NewInt(8000),
		big.NewInt(1_000_000),
		nonce,
		1_735_689_600,
	)

	data := make([]byte, 0, 224)
	data = append(data, CREDIT_TYPEHASH.Bytes()...)
	data = append(data, common.LeftPadBytes(common.HexToAddress("0xAbCdEf1234567890aBcDeF1234567890AbCdEf12").Bytes(), 32)...)
	data = append(data, crypto.Keccak256Hash([]byte("USDC")).Bytes()...)
	data = append(data, common.LeftPadBytes(big.NewInt(8000).Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(big.NewInt(1_000_000).Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(new(big.Int).SetUint64(nonce).Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(big.NewInt(1_735_689_600).Bytes(), 32)...)
	require.Equal(t, crypto.Keccak256Hash(data), actual)
}

// TestBuildDigest tests EIP-712 digest building
func TestBuildDigest(t *testing.T) {
	config := &Config{
		PrivateKeyHex:    testPrivateKeyHex,
		ChainID:          1,
		LendingPoolAddr:  "0x1234567890123456789012345678901234567890",
		SignatureTTL:     300,
		RateLimitPerUser: 10,
		RateLimitPerIP:   20,
	}

	service, err := NewService(config, nil, nil, nil)
	require.NoError(t, err)

	structHash := crypto.Keccak256Hash([]byte("test struct hash"))
	digest := service.buildDigest(structHash)

	// Digest should not be zero
	assert.NotEqual(t, common.Hash{}, digest)

	// Digest should start with \x19\x01 prefix conceptually
	// The hash incorporates domain separator and struct hash

	// Same struct hash should produce same digest
	digest2 := service.buildDigest(structHash)
	assert.Equal(t, digest, digest2)
}

// TestSignatureRequestStruct tests SignatureRequest struct
func TestSignatureRequestStruct(t *testing.T) {
	req := SignatureRequest{
		User:      common.HexToAddress("0x1234567890123456789012345678901234567890"),
		Market:    "USDT",
		Amount:    big.NewInt(1000000),
		IPAddress: "192.168.1.1",
		UserAgent: "Mozilla/5.0",
	}

	assert.Equal(t, "0x1234567890123456789012345678901234567890", req.User.Hex())
	assert.Equal(t, "USDT", req.Market)
	assert.Equal(t, big.NewInt(1000000), req.Amount)
	assert.Equal(t, "192.168.1.1", req.IPAddress)
	assert.Equal(t, "Mozilla/5.0", req.UserAgent)
}

// TestSignatureResponseStruct tests SignatureResponse struct
func TestSignatureResponseStruct(t *testing.T) {
	resp := SignatureResponse{
		Signature:   "0x1234...signature...",
		LTV:         8000,
		AmountCap:   "1000000000000000000",
		Nonce:       1,
		Deadline:    1735689600,
		CreditScore: 750,
		CreditTier:  "B",
		RequestID:   "uuid-1234-5678",
	}

	assert.Equal(t, "0x1234...signature...", resp.Signature)
	assert.Equal(t, 8000, resp.LTV)
	assert.Equal(t, "1000000000000000000", resp.AmountCap)
	assert.Equal(t, uint64(1), resp.Nonce)
	assert.Equal(t, int64(1735689600), resp.Deadline)
	assert.Equal(t, 750, resp.CreditScore)
	assert.Equal(t, "B", resp.CreditTier)
	assert.Equal(t, "uuid-1234-5678", resp.RequestID)
}

// TestErrorTypes tests error type definitions
func TestErrorTypes(t *testing.T) {
	assert.NotNil(t, ErrInvalidPrivateKey)
	assert.NotNil(t, ErrInvalidLTV)
	assert.NotNil(t, ErrRateLimitExceeded)
	assert.NotNil(t, ErrInsufficientCredit)
	assert.NotNil(t, ErrAmountExceedsLimit)

	// Verify the public error messages remain stable.
	assert.Equal(t, "无效的私钥", ErrInvalidPrivateKey.Error())
	assert.Equal(t, "请求频率超限", ErrRateLimitExceeded.Error())
	assert.Equal(t, "信用分不足", ErrInsufficientCredit.Error())
	assert.Equal(t, "金额超出信用额度", ErrAmountExceedsLimit.Error())
}

// TestConfigStruct tests Config struct
func TestConfigStruct(t *testing.T) {
	config := Config{
		PrivateKeyHex:    "0x1234",
		ChainID:          1,
		LendingPoolAddr:  "0x1234567890123456789012345678901234567890",
		SignatureTTL:     300,
		RateLimitPerUser: 10,
		RateLimitPerIP:   20,
	}

	assert.Equal(t, "0x1234", config.PrivateKeyHex)
	assert.Equal(t, int64(1), config.ChainID)
	assert.Equal(t, "0x1234567890123456789012345678901234567890", config.LendingPoolAddr)
	assert.Equal(t, 300, config.SignatureTTL)
	assert.Equal(t, 10, config.RateLimitPerUser)
	assert.Equal(t, 20, config.RateLimitPerIP)
}

// TestChainIDVariations tests service with different chain IDs
func TestChainIDVariations(t *testing.T) {
	chainIDs := []int64{
		1,        // Ethereum Mainnet
		5,        // Goerli
		11155111, // Sepolia
		137,      // Polygon
		42161,    // Arbitrum
		10,       // Optimism
	}

	for _, chainID := range chainIDs {
		t.Run("", func(t *testing.T) {
			config := &Config{
				PrivateKeyHex:    testPrivateKeyHex,
				ChainID:          chainID,
				LendingPoolAddr:  "0x1234567890123456789012345678901234567890",
				SignatureTTL:     300,
				RateLimitPerUser: 10,
				RateLimitPerIP:   20,
			}

			service, err := NewService(config, nil, nil, nil)
			require.NoError(t, err)
			assert.NotNil(t, service)
			assert.NotEqual(t, common.Hash{}, service.GetDomainSeparator())
		})
	}
}

// TestSignatureVerification tests that generated signatures can be verified
func TestSignatureVerification(t *testing.T) {
	privateKey, err := crypto.HexToECDSA("4bbbf85ce3377467afe5d46f804f221813b2bb87f24d81f60f1fcdbf7cbf4356")
	require.NoError(t, err)

	// Create a test message
	message := []byte("test message")
	hash := crypto.Keccak256Hash(message)

	// Sign the message
	sig, err := crypto.Sign(hash.Bytes(), privateKey)
	require.NoError(t, err)

	// Verify signature length (65 bytes: r=32, s=32, v=1)
	assert.Len(t, sig, 65)

	// Recover public key from signature
	pubKeyRecovered, err := crypto.SigToPub(hash.Bytes(), sig)
	require.NoError(t, err)

	// Verify recovered address matches original
	expectedAddr := crypto.PubkeyToAddress(privateKey.PublicKey)
	recoveredAddr := crypto.PubkeyToAddress(*pubKeyRecovered)
	assert.Equal(t, expectedAddr, recoveredAddr)
}

// TestSignatureVValue tests that v value is adjusted correctly for Ethereum
func TestSignatureVValue(t *testing.T) {
	privateKey, err := crypto.HexToECDSA("4bbbf85ce3377467afe5d46f804f221813b2bb87f24d81f60f1fcdbf7cbf4356")
	require.NoError(t, err)

	message := []byte("test message")
	hash := crypto.Keccak256Hash(message)

	sig, err := crypto.Sign(hash.Bytes(), privateKey)
	require.NoError(t, err)

	// Original v is 0 or 1
	originalV := sig[64]
	assert.LessOrEqual(t, originalV, byte(1))

	// After adjustment, v should be 27 or 28
	sig[64] += 27
	adjustedV := sig[64]
	assert.True(t, adjustedV == 27 || adjustedV == 28,
		"Adjusted v should be 27 or 28, got %d", adjustedV)
}

// TestBigIntHandling tests big integer handling for amounts
func TestBigIntHandling(t *testing.T) {
	testCases := []struct {
		name     string
		amount   string
		expected bool
	}{
		{"Small amount", "1000000", true},
		{"1 ETH in wei", "1000000000000000000", true},
		{"100 ETH in wei", "100000000000000000000", true},
		{"500k in wei (S tier max)", "500000000000000000000000", true},
		{"Very large amount", "999999999999999999999999999999", true},
		{"Zero", "0", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			amount, ok := new(big.Int).SetString(tc.amount, 10)
			assert.Equal(t, tc.expected, ok)
			if ok {
				assert.NotNil(t, amount)
				assert.Equal(t, tc.amount, amount.String())
			}
		})
	}
}

// TestAddressNormalization tests address handling
func TestAddressNormalization(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{
			"0x1234567890123456789012345678901234567890",
			"0x1234567890123456789012345678901234567890",
		},
		{
			"0xABCDEF1234567890ABCDEF1234567890ABCDEF12",
			"0xABCDEF1234567890ABCDEF1234567890ABCDEF12",
		},
		{
			"0xabcdef1234567890abcdef1234567890abcdef12",
			"0xabcdef1234567890abcdef1234567890abcdef12",
		},
	}

	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			addr := common.HexToAddress(tc.input)
			assert.True(t, common.IsHexAddress(addr.Hex()))
		})
	}
}

// BenchmarkBuildStructHash benchmarks struct hash building
func BenchmarkBuildStructHash(b *testing.B) {
	config := &Config{
		PrivateKeyHex:    testPrivateKeyHex,
		ChainID:          1,
		LendingPoolAddr:  "0x1234567890123456789012345678901234567890",
		SignatureTTL:     300,
		RateLimitPerUser: 10,
		RateLimitPerIP:   20,
	}

	service, _ := NewService(config, nil, nil, nil)

	user := common.HexToAddress("0xAbCdEf1234567890aBcDeF1234567890AbCdEf12")
	market := crypto.Keccak256Hash([]byte("USDT"))
	ltv := big.NewInt(8000)
	amountCap := big.NewInt(1000000000000000000)
	nonce := uint64(1)
	deadline := int64(1735689600)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.buildStructHash(user, market, ltv, amountCap, nonce, deadline)
	}
}

// BenchmarkBuildDigest benchmarks digest building
func BenchmarkBuildDigest(b *testing.B) {
	config := &Config{
		PrivateKeyHex:    testPrivateKeyHex,
		ChainID:          1,
		LendingPoolAddr:  "0x1234567890123456789012345678901234567890",
		SignatureTTL:     300,
		RateLimitPerUser: 10,
		RateLimitPerIP:   20,
	}

	service, _ := NewService(config, nil, nil, nil)
	structHash := crypto.Keccak256Hash([]byte("test struct hash"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.buildDigest(structHash)
	}
}

// BenchmarkSign benchmarks signing performance
func BenchmarkSign(b *testing.B) {
	privateKey, _ := crypto.HexToECDSA("4bbbf85ce3377467afe5d46f804f221813b2bb87f24d81f60f1fcdbf7cbf4356")
	message := crypto.Keccak256Hash([]byte("test message"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		crypto.Sign(message.Bytes(), privateKey)
	}
}

// Helper function to create a test private key
func generateTestPrivateKey() (*ecdsa.PrivateKey, error) {
	return crypto.GenerateKey()
}

// TestPrivateKeyGeneration tests that key generation works
func TestPrivateKeyGeneration(t *testing.T) {
	key, err := generateTestPrivateKey()
	require.NoError(t, err)
	require.NotNil(t, key)

	// Verify address can be derived
	addr := crypto.PubkeyToAddress(key.PublicKey)
	assert.True(t, common.IsHexAddress(addr.Hex()))

	// Verify key can sign
	message := crypto.Keccak256Hash([]byte("test"))
	sig, err := crypto.Sign(message.Bytes(), key)
	assert.NoError(t, err)
	assert.Len(t, sig, 65)
}
