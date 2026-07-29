package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/creditlink/backend/internal/api/middleware"
	"github.com/creditlink/backend/internal/service/price"
	"github.com/creditlink/backend/internal/service/signer"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestRespondPriceErrorStatus(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "missing asset configuration", err: price.ErrAssetNotFound, status: http.StatusServiceUnavailable},
		{name: "missing RPC reader", err: price.ErrPriceReaderUnavailable, status: http.StatusServiceUnavailable},
		{name: "upstream oracle call failed", err: price.ErrOracleReadFailed, status: http.StatusBadGateway},
		{name: "invalid database amount", err: price.ErrInvalidAmount, status: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(response)
			respondPriceError(ctx, tt.err)
			require.Equal(t, tt.status, response.Code)
		})
	}
}

func TestSignErrorStatus(t *testing.T) {
	require.Equal(t, http.StatusBadRequest, signErrorStatus(signer.ErrUnsupportedMarket))
	require.Equal(t, http.StatusBadGateway, signErrorStatus(fmt.Errorf("%w: %w", signer.ErrPriceUnavailable, price.ErrOracleReadFailed)))
	require.Equal(t, http.StatusServiceUnavailable, signErrorStatus(fmt.Errorf("%w: %w", signer.ErrPriceUnavailable, price.ErrPriceReaderUnavailable)))
}

func TestStatsHandlerRejectsMissingPriceService(t *testing.T) {
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/stats/platform", nil)

	(&StatsHandler{}).GetPlatformStats(ctx)

	require.Equal(t, http.StatusServiceUnavailable, response.Code)
}

func TestCreditHandlerRejectsMissingSignerService(t *testing.T) {
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/credit/sign", bytes.NewBufferString(`{"market":"USDC","amount":"1"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("walletAddress", "0x1111111111111111111111111111111111111111")

	(&CreditHandler{}).Sign(ctx)

	require.Equal(t, http.StatusServiceUnavailable, response.Code)
}

// ==================== Auth Handler Tests ====================

func TestAuthHandler_GetNonce(t *testing.T) {
	router := gin.New()
	handler := &AuthHandler{}
	router.GET("/auth/nonce", handler.GetNonce)

	testCases := []struct {
		name           string
		wallet         string
		expectedStatus int
		expectError    bool
	}{
		{
			name:           "Valid wallet address",
			wallet:         "0x1234567890123456789012345678901234567890",
			expectedStatus: http.StatusOK,
			expectError:    false,
		},
		{
			name:           "Wallet with checksum",
			wallet:         "0xAb5801a7D398351b8bE11C439e05C5B3259aeC9B",
			expectedStatus: http.StatusOK,
			expectError:    false,
		},
		{
			name:           "Missing wallet",
			wallet:         "",
			expectedStatus: http.StatusBadRequest,
			expectError:    true,
		},
		{
			name:           "Invalid wallet - too short",
			wallet:         "0x1234",
			expectedStatus: http.StatusBadRequest,
			expectError:    true,
		},
		{
			name:           "Invalid wallet - not hex",
			wallet:         "0xGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGG",
			expectedStatus: http.StatusBadRequest,
			expectError:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/auth/nonce?wallet="+tc.wallet, nil)
			resp := httptest.NewRecorder()

			router.ServeHTTP(resp, req)

			assert.Equal(t, tc.expectedStatus, resp.Code)

			if !tc.expectError {
				var response NonceResponse
				err := json.Unmarshal(resp.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.NotEmpty(t, response.Message)
			}
		})
	}
}

func TestAuthHandler_VerifySignature(t *testing.T) {
	handler := &AuthHandler{}

	// Test with known signature (this would need actual crypto verification in real tests)
	testCases := []struct {
		name      string
		wallet    string
		message   string
		signature string
		expected  bool
	}{
		{
			name:      "Invalid signature - wrong length",
			wallet:    "0x1234567890123456789012345678901234567890",
			message:   "Test message",
			signature: "0x1234",
			expected:  false,
		},
		{
			name:      "Invalid signature - not hex",
			wallet:    "0x1234567890123456789012345678901234567890",
			message:   "Test message",
			signature: "not_a_hex_signature",
			expected:  false,
		},
		{
			name:      "Empty signature",
			wallet:    "0x1234567890123456789012345678901234567890",
			message:   "Test message",
			signature: "",
			expected:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := handler.verifySignature(tc.wallet, tc.message, tc.signature)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestAuthHandler_Login_Validation(t *testing.T) {
	router := gin.New()
	handler := &AuthHandler{}
	router.POST("/auth/login", handler.Login)

	testCases := []struct {
		name           string
		request        LoginRequest
		expectedStatus int
	}{
		{
			name: "Missing wallet address",
			request: LoginRequest{
				WalletAddress: "",
				Signature:     "0x" + string(make([]byte, 130)),
				Message:       "Test message",
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "Invalid wallet address",
			request: LoginRequest{
				WalletAddress: "invalid",
				Signature:     "0x" + string(make([]byte, 130)),
				Message:       "Test message",
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "Missing signature",
			request: LoginRequest{
				WalletAddress: "0x1234567890123456789012345678901234567890",
				Signature:     "",
				Message:       "Test message",
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "Missing message",
			request: LoginRequest{
				WalletAddress: "0x1234567890123456789012345678901234567890",
				Signature:     "0x" + string(make([]byte, 130)),
				Message:       "",
			},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.request)
			req := httptest.NewRequest("POST", "/auth/login", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp := httptest.NewRecorder()

			router.ServeHTTP(resp, req)

			assert.Equal(t, tc.expectedStatus, resp.Code)
		})
	}
}

// ==================== Request/Response Struct Tests ====================

func TestNonceRequest_Struct(t *testing.T) {
	req := NonceRequest{
		WalletAddress: "0x1234567890123456789012345678901234567890",
	}
	assert.Equal(t, "0x1234567890123456789012345678901234567890", req.WalletAddress)
}

func TestNonceResponse_Struct(t *testing.T) {
	resp := NonceResponse{
		Nonce:   "0x1234",
		Message: "Sign this message",
	}
	assert.Equal(t, "0x1234", resp.Nonce)
	assert.Equal(t, "Sign this message", resp.Message)
}

func TestLoginRequest_Struct(t *testing.T) {
	req := LoginRequest{
		WalletAddress: "0x1234567890123456789012345678901234567890",
		Signature:     "0xsig123",
		Message:       "Test message",
	}
	assert.Equal(t, "0x1234567890123456789012345678901234567890", req.WalletAddress)
	assert.Equal(t, "0xsig123", req.Signature)
	assert.Equal(t, "Test message", req.Message)
}

func TestLoginResponse_Struct(t *testing.T) {
	resp := LoginResponse{
		Token:         "jwt.token.here",
		WalletAddress: "0x1234567890123456789012345678901234567890",
	}
	assert.Equal(t, "jwt.token.here", resp.Token)
	assert.Equal(t, "0x1234567890123456789012345678901234567890", resp.WalletAddress)
}

// ==================== JWT Middleware Tests ====================

func TestJWTMiddleware_MissingHeader(t *testing.T) {
	// Initialize JWT config
	middleware.InitJWT(&middleware.JWTConfig{
		Secret:     "test-secret-key-32-bytes-long!!",
		ExpireHour: 24,
	})

	router := gin.New()
	router.Use(middleware.JWTAuth())
	router.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	req := httptest.NewRequest("GET", "/protected", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusUnauthorized, resp.Code)
}

func TestJWTMiddleware_InvalidFormat(t *testing.T) {
	middleware.InitJWT(&middleware.JWTConfig{
		Secret:     "test-secret-key-32-bytes-long!!",
		ExpireHour: 24,
	})

	router := gin.New()
	router.Use(middleware.JWTAuth())
	router.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	testCases := []struct {
		name   string
		header string
	}{
		{"No Bearer prefix", "token-without-bearer"},
		{"Wrong prefix", "Basic token123"},
		{"Empty token", "Bearer "},
		{"Only Bearer", "Bearer"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/protected", nil)
			req.Header.Set("Authorization", tc.header)
			resp := httptest.NewRecorder()

			router.ServeHTTP(resp, req)

			assert.Equal(t, http.StatusUnauthorized, resp.Code)
		})
	}
}

func TestJWTMiddleware_ValidToken(t *testing.T) {
	middleware.InitJWT(&middleware.JWTConfig{
		Secret:     "test-secret-key-32-bytes-long!!",
		ExpireHour: 24,
	})

	// Generate a valid token
	token, err := middleware.GenerateToken("0x1234567890123456789012345678901234567890")
	require.NoError(t, err)

	router := gin.New()
	router.Use(middleware.JWTAuth())
	router.GET("/protected", func(c *gin.Context) {
		wallet, _ := c.Get("walletAddress")
		c.JSON(http.StatusOK, gin.H{"wallet": wallet})
	})

	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var response map[string]string
	err = json.Unmarshal(resp.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, "0x1234567890123456789012345678901234567890", response["wallet"])
}

func TestJWTMiddleware_InvalidToken(t *testing.T) {
	middleware.InitJWT(&middleware.JWTConfig{
		Secret:     "test-secret-key-32-bytes-long!!",
		ExpireHour: 24,
	})

	router := gin.New()
	router.Use(middleware.JWTAuth())
	router.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer invalid.token.here")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusUnauthorized, resp.Code)
}

func TestJWT_GenerateAndParse(t *testing.T) {
	middleware.InitJWT(&middleware.JWTConfig{
		Secret:     "test-secret-key-32-bytes-long!!",
		ExpireHour: 24,
	})

	wallet := "0x1234567890123456789012345678901234567890"
	token, err := middleware.GenerateToken(wallet)
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	claims, err := middleware.ParseToken(token)
	require.NoError(t, err)
	assert.Equal(t, wallet, claims.WalletAddress)
}

// ==================== Optional JWT Auth Tests ====================

func TestOptionalJWTAuth_WithToken(t *testing.T) {
	middleware.InitJWT(&middleware.JWTConfig{
		Secret:     "test-secret-key-32-bytes-long!!",
		ExpireHour: 24,
	})

	token, _ := middleware.GenerateToken("0x1234567890123456789012345678901234567890")

	router := gin.New()
	router.Use(middleware.OptionalJWTAuth())
	router.GET("/optional", func(c *gin.Context) {
		wallet, exists := c.Get("walletAddress")
		if exists {
			c.JSON(http.StatusOK, gin.H{"wallet": wallet, "authenticated": true})
		} else {
			c.JSON(http.StatusOK, gin.H{"authenticated": false})
		}
	})

	req := httptest.NewRequest("GET", "/optional", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var response map[string]interface{}
	json.Unmarshal(resp.Body.Bytes(), &response)
	assert.Equal(t, true, response["authenticated"])
}

func TestOptionalJWTAuth_WithoutToken(t *testing.T) {
	middleware.InitJWT(&middleware.JWTConfig{
		Secret:     "test-secret-key-32-bytes-long!!",
		ExpireHour: 24,
	})

	router := gin.New()
	router.Use(middleware.OptionalJWTAuth())
	router.GET("/optional", func(c *gin.Context) {
		_, exists := c.Get("walletAddress")
		if exists {
			c.JSON(http.StatusOK, gin.H{"authenticated": true})
		} else {
			c.JSON(http.StatusOK, gin.H{"authenticated": false})
		}
	})

	req := httptest.NewRequest("GET", "/optional", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var response map[string]interface{}
	json.Unmarshal(resp.Body.Bytes(), &response)
	assert.Equal(t, false, response["authenticated"])
}

// ==================== WebSocket Handler Tests ====================

func TestWSHandler_NewWSHandler(t *testing.T) {
	handler := NewWSHandler()
	assert.NotNil(t, handler)
	assert.NotNil(t, handler.clients)
	assert.NotNil(t, handler.broadcast)
	assert.Equal(t, 0, handler.GetConnectedClients())
}

func TestWSHandler_HealthAlertMessage(t *testing.T) {
	msg := HealthAlertMessage{
		Type:           "HEALTH_ALERT",
		User:           "0x1234567890123456789012345678901234567890",
		HealthFactor:   1.5,
		Recommendation: "Consider adding collateral",
		Timestamp:      1735689600,
	}

	assert.Equal(t, "HEALTH_ALERT", msg.Type)
	assert.Equal(t, "0x1234567890123456789012345678901234567890", msg.User)
	assert.Equal(t, 1.5, msg.HealthFactor)
	assert.Equal(t, "Consider adding collateral", msg.Recommendation)
	assert.Equal(t, int64(1735689600), msg.Timestamp)
}

func TestWSHandler_GetConnectedClients(t *testing.T) {
	handler := NewWSHandler()
	assert.Equal(t, 0, handler.GetConnectedClients())
}

// ==================== Risk Handler Tests ====================

func TestRiskHandler_Struct(t *testing.T) {
	// Test that RiskHandler can be created
	handler := NewRiskHandler(nil)
	assert.NotNil(t, handler)
}

// ==================== CORS Middleware Test ====================

func TestCORSHeaders(t *testing.T) {
	router := gin.New()

	// Add CORS middleware similar to main.go
	router.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	})

	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	t.Run("GET request includes CORS headers", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		resp := httptest.NewRecorder()

		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)
		assert.Equal(t, "*", resp.Header().Get("Access-Control-Allow-Origin"))
		assert.Contains(t, resp.Header().Get("Access-Control-Allow-Methods"), "GET")
	})

	t.Run("OPTIONS preflight request", func(t *testing.T) {
		req := httptest.NewRequest("OPTIONS", "/test", nil)
		resp := httptest.NewRecorder()

		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusNoContent, resp.Code)
		assert.Equal(t, "*", resp.Header().Get("Access-Control-Allow-Origin"))
	})
}

// ==================== Error Response Tests ====================

func TestErrorResponse_Format(t *testing.T) {
	router := gin.New()
	router.GET("/error", func(c *gin.Context) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "test error message"})
	})

	req := httptest.NewRequest("GET", "/error", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusBadRequest, resp.Code)

	var response map[string]string
	err := json.Unmarshal(resp.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, "test error message", response["error"])
}

// ==================== Benchmark Tests ====================

func BenchmarkJWTGenerateToken(b *testing.B) {
	middleware.InitJWT(&middleware.JWTConfig{
		Secret:     "test-secret-key-32-bytes-long!!",
		ExpireHour: 24,
	})

	wallet := "0x1234567890123456789012345678901234567890"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		middleware.GenerateToken(wallet)
	}
}

func BenchmarkJWTParseToken(b *testing.B) {
	middleware.InitJWT(&middleware.JWTConfig{
		Secret:     "test-secret-key-32-bytes-long!!",
		ExpireHour: 24,
	})

	token, _ := middleware.GenerateToken("0x1234567890123456789012345678901234567890")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		middleware.ParseToken(token)
	}
}

func BenchmarkGetNonce(b *testing.B) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	handler := &AuthHandler{}
	router.GET("/auth/nonce", handler.GetNonce)

	wallet := "0x1234567890123456789012345678901234567890"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/auth/nonce?wallet="+wallet, nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
	}
}
