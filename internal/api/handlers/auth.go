package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/creditlink/backend/internal/api/middleware"
	"github.com/creditlink/backend/internal/repository"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gin-gonic/gin"
)

// AuthHandler handles authentication-related API requests
type AuthHandler struct {
	userRepo *repository.UserRepository
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(userRepo *repository.UserRepository) *AuthHandler {
	return &AuthHandler{
		userRepo: userRepo,
	}
}

// NonceRequest represents the request for getting a nonce
type NonceRequest struct {
	WalletAddress string `json:"walletAddress" binding:"required"`
}

// NonceResponse represents the nonce response
type NonceResponse struct {
	Nonce   string `json:"nonce"`
	Message string `json:"message"`
}

// LoginRequest represents the login request
type LoginRequest struct {
	WalletAddress string `json:"walletAddress" binding:"required"`
	Signature     string `json:"signature" binding:"required"`
	Message       string `json:"message" binding:"required"`
}

// LoginResponse represents the login response
type LoginResponse struct {
	Token         string `json:"token"`
	WalletAddress string `json:"walletAddress"`
}

// GetNonce handles GET /api/v1/auth/nonce
func (h *AuthHandler) GetNonce(c *gin.Context) {
	wallet := c.Query("wallet")
	if wallet == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "wallet address required"})
		return
	}

	// Validate wallet address
	if !common.IsHexAddress(wallet) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid wallet address"})
		return
	}

	wallet = strings.ToLower(wallet)

	// Generate a message for signing
	// In production, you'd want to store this nonce and validate it later
	message := "Sign this message to authenticate with CreditLink.\n\nWallet: " + wallet + "\n\nThis request will not trigger a blockchain transaction or cost any gas fees."

	c.JSON(http.StatusOK, NonceResponse{
		Nonce:   wallet, // Using wallet as nonce for simplicity
		Message: message,
	})
}

// Login handles POST /api/v1/auth/login
func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate wallet address
	if !common.IsHexAddress(req.WalletAddress) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid wallet address"})
		return
	}

	wallet := strings.ToLower(req.WalletAddress)

	// Verify signature
	if !h.verifySignature(wallet, req.Message, req.Signature) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
		return
	}

	// Get or create user
	_, err := h.userRepo.GetOrCreate(c.Request.Context(), wallet)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
		return
	}

	// Generate JWT token
	token, err := middleware.GenerateToken(wallet)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, LoginResponse{
		Token:         token,
		WalletAddress: wallet,
	})
}

// verifySignature verifies an Ethereum signature
func (h *AuthHandler) verifySignature(wallet, message, signature string) bool {
	// Remove 0x prefix if present
	sig := strings.TrimPrefix(signature, "0x")

	// Decode signature
	sigBytes := common.FromHex(sig)
	if len(sigBytes) != 65 {
		return false
	}

	// Ethereum signatures have v = 27 or 28, but ecrecover expects v = 0 or 1
	if sigBytes[64] >= 27 {
		sigBytes[64] -= 27
	}

	// Hash the message with Ethereum prefix
	prefixedMessage := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message)
	hash := crypto.Keccak256Hash([]byte(prefixedMessage))

	// Recover public key
	pubKey, err := crypto.SigToPub(hash.Bytes(), sigBytes)
	if err != nil {
		return false
	}

	// Derive address from public key
	recoveredAddr := crypto.PubkeyToAddress(*pubKey)

	// Compare addresses (case-insensitive)
	return strings.EqualFold(recoveredAddr.Hex(), wallet)
}
