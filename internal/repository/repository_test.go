package repository

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/creditlink/backend/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupTestDB creates an in-memory SQLite database for testing
func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	// Auto-migrate all models
	err = db.AutoMigrate(
		&models.User{},
		&models.UserCredit{},
		&models.CreditFactor{},
		&models.LoanActivity{},
		&models.SignatureLog{},
		&models.SignatureNonceCounter{},
		&models.CurrentRiskParams{},
		&models.RiskParamsHistory{},
		&models.IndexerState{},
		&models.UserPosition{},
	)
	require.NoError(t, err)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	// SQLite :memory: 数据库只对单连接可见；同时也让并发单测稳定地验证数据库原子分配语句。
	sqlDB.SetMaxOpenConns(1)

	return db
}

// ==================== UserRepository Tests ====================

func TestUserRepository_Create(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	testCases := []struct {
		name        string
		wallet      string
		expectError bool
		errorType   error
	}{
		{
			name:        "Valid wallet address",
			wallet:      "0x1234567890123456789012345678901234567890",
			expectError: false,
		},
		{
			name:        "Wallet with uppercase",
			wallet:      "0xABCDEF1234567890ABCDEF1234567890ABCDEF12",
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			user := &models.User{WalletAddress: tc.wallet}
			err := repo.Create(ctx, user)

			if tc.expectError {
				assert.Error(t, err)
				if tc.errorType != nil {
					assert.ErrorIs(t, err, tc.errorType)
				}
			} else {
				assert.NoError(t, err)
				assert.NotZero(t, user.ID)
			}
		})
	}
}

func TestUserRepository_CreateDuplicate(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	wallet := "0x1234567890123456789012345678901234567890"

	// Create first user
	user1 := &models.User{WalletAddress: wallet}
	err := repo.Create(ctx, user1)
	require.NoError(t, err)

	// Try to create duplicate
	user2 := &models.User{WalletAddress: wallet}
	err = repo.Create(ctx, user2)
	assert.Error(t, err)
}

func TestUserRepository_GetByID(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	// Create a test user
	user := &models.User{WalletAddress: "0x1234567890123456789012345678901234567890"}
	err := repo.Create(ctx, user)
	require.NoError(t, err)

	t.Run("Existing user", func(t *testing.T) {
		found, err := repo.GetByID(ctx, user.ID)
		assert.NoError(t, err)
		assert.NotNil(t, found)
		assert.Equal(t, user.ID, found.ID)
	})

	t.Run("Non-existing user", func(t *testing.T) {
		found, err := repo.GetByID(ctx, 99999)
		assert.ErrorIs(t, err, ErrUserNotFound)
		assert.Nil(t, found)
	})

	t.Run("Zero ID", func(t *testing.T) {
		found, err := repo.GetByID(ctx, 0)
		assert.ErrorIs(t, err, ErrUserNotFound)
		assert.Nil(t, found)
	})
}

func TestUserRepository_GetByWallet(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	wallet := "0x1234567890123456789012345678901234567890"
	user := &models.User{WalletAddress: wallet}
	err := repo.Create(ctx, user)
	require.NoError(t, err)

	testCases := []struct {
		name        string
		wallet      string
		expectError bool
	}{
		{
			name:        "Existing wallet lowercase",
			wallet:      wallet,
			expectError: false,
		},
		{
			name:        "Existing wallet uppercase",
			wallet:      "0x1234567890123456789012345678901234567890",
			expectError: false,
		},
		{
			name:        "Non-existing wallet",
			wallet:      "0x0000000000000000000000000000000000000000",
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			found, err := repo.GetByWallet(ctx, tc.wallet)

			if tc.expectError {
				assert.ErrorIs(t, err, ErrUserNotFound)
				assert.Nil(t, found)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, found)
			}
		})
	}
}

func TestUserRepository_GetOrCreate(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	wallet := "0x1234567890123456789012345678901234567890"

	t.Run("Create new user", func(t *testing.T) {
		user, err := repo.GetOrCreate(ctx, wallet)
		assert.NoError(t, err)
		assert.NotNil(t, user)
		assert.NotZero(t, user.ID)
	})

	t.Run("Get existing user", func(t *testing.T) {
		user, err := repo.GetOrCreate(ctx, wallet)
		assert.NoError(t, err)
		assert.NotNil(t, user)
		// Should return the same user
		assert.Equal(t, uint(1), user.ID)
	})
}

func TestUserRepository_List(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	// Create multiple users
	for i := 0; i < 10; i++ {
		user := &models.User{WalletAddress: "0x" + string(rune('a'+i)) + "234567890123456789012345678901234567890"}
		err := repo.Create(ctx, user)
		require.NoError(t, err)
	}

	testCases := []struct {
		name          string
		offset        int
		limit         int
		expectedCount int
	}{
		{"First page", 0, 5, 5},
		{"Second page", 5, 5, 5},
		{"Beyond data", 10, 5, 0},
		{"All at once", 0, 20, 10},
		{"Limit 1", 0, 1, 1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			users, total, err := repo.List(ctx, tc.offset, tc.limit)
			assert.NoError(t, err)
			assert.Equal(t, int64(10), total)
			assert.Len(t, users, tc.expectedCount)
		})
	}
}

// ==================== CreditRepository Tests ====================

func TestCreditRepository_CreateOrUpdate(t *testing.T) {
	db := setupTestDB(t)
	userRepo := NewUserRepository(db)
	creditRepo := NewCreditRepository(db)
	ctx := context.Background()

	// Create a test user
	user := &models.User{WalletAddress: "0x1234567890123456789012345678901234567890"}
	err := userRepo.Create(ctx, user)
	require.NoError(t, err)

	t.Run("Create new credit", func(t *testing.T) {
		credit := &models.UserCredit{
			UserID:         user.ID,
			Score:          750,
			Tier:           "B",
			MaxLTV:         8000,
			MaxBorrowLimit: "50000000000000000000000",
		}
		err := creditRepo.CreateOrUpdate(ctx, credit)
		assert.NoError(t, err)
		assert.NotZero(t, credit.ID)
	})

	t.Run("Update existing credit", func(t *testing.T) {
		credit := &models.UserCredit{
			UserID:         user.ID,
			Score:          850,
			Tier:           "A",
			MaxLTV:         9000,
			MaxBorrowLimit: "200000000000000000000000",
		}
		err := creditRepo.CreateOrUpdate(ctx, credit)
		assert.NoError(t, err)

		// Verify update
		found, err := creditRepo.GetByUserID(ctx, user.ID)
		assert.NoError(t, err)
		assert.Equal(t, 850, found.Score)
		assert.Equal(t, "A", found.Tier)
	})
}

func TestCreditRepository_GetByUserID(t *testing.T) {
	db := setupTestDB(t)
	userRepo := NewUserRepository(db)
	creditRepo := NewCreditRepository(db)
	ctx := context.Background()

	// Create a test user with credit
	user := &models.User{WalletAddress: "0x1234567890123456789012345678901234567890"}
	err := userRepo.Create(ctx, user)
	require.NoError(t, err)

	credit := &models.UserCredit{
		UserID: user.ID,
		Score:  750,
		Tier:   "B",
	}
	err = creditRepo.CreateOrUpdate(ctx, credit)
	require.NoError(t, err)

	t.Run("Existing credit", func(t *testing.T) {
		found, err := creditRepo.GetByUserID(ctx, user.ID)
		assert.NoError(t, err)
		assert.NotNil(t, found)
		assert.Equal(t, 750, found.Score)
	})

	t.Run("Non-existing credit", func(t *testing.T) {
		found, err := creditRepo.GetByUserID(ctx, 99999)
		assert.Error(t, err)
		assert.Nil(t, found)
	})
}

func TestCreditRepository_SaveFactors(t *testing.T) {
	db := setupTestDB(t)
	userRepo := NewUserRepository(db)
	creditRepo := NewCreditRepository(db)
	ctx := context.Background()

	// Create a test user
	user := &models.User{WalletAddress: "0x1234567890123456789012345678901234567890"}
	err := userRepo.Create(ctx, user)
	require.NoError(t, err)

	factors := []models.CreditFactor{
		{FactorKey: "repayment_bonus", FactorValue: 100, Contribution: 100},
		{FactorKey: "liquidation_penalty", FactorValue: -200, Contribution: -200},
		{FactorKey: "loyalty_bonus", FactorValue: 30, Contribution: 30},
	}

	err = creditRepo.SaveFactors(ctx, user.ID, factors)
	assert.NoError(t, err)

	// Verify factors were saved
	savedFactors, err := creditRepo.GetFactors(ctx, user.ID)
	assert.NoError(t, err)
	assert.Len(t, savedFactors, 3)
}

func TestCreditRepository_GetFactors(t *testing.T) {
	db := setupTestDB(t)
	userRepo := NewUserRepository(db)
	creditRepo := NewCreditRepository(db)
	ctx := context.Background()

	// Create a test user
	user := &models.User{WalletAddress: "0x1234567890123456789012345678901234567890"}
	err := userRepo.Create(ctx, user)
	require.NoError(t, err)

	t.Run("No factors", func(t *testing.T) {
		factors, err := creditRepo.GetFactors(ctx, user.ID)
		assert.NoError(t, err)
		assert.Empty(t, factors)
	})

	t.Run("With factors", func(t *testing.T) {
		factors := []models.CreditFactor{
			{FactorKey: "test_factor", FactorValue: 50, Contribution: 50},
		}
		err := creditRepo.SaveFactors(ctx, user.ID, factors)
		require.NoError(t, err)

		found, err := creditRepo.GetFactors(ctx, user.ID)
		assert.NoError(t, err)
		assert.Len(t, found, 1)
		assert.Equal(t, "test_factor", found[0].FactorKey)
	})
}

// ==================== ActivityRepository Tests ====================

func TestActivityRepository_Create(t *testing.T) {
	db := setupTestDB(t)
	userRepo := NewUserRepository(db)
	activityRepo := NewActivityRepository(db)
	ctx := context.Background()

	// Create a test user
	user := &models.User{WalletAddress: "0x1234567890123456789012345678901234567890"}
	err := userRepo.Create(ctx, user)
	require.NoError(t, err)

	activity := &models.LoanActivity{
		UserID:         user.ID,
		ActionType:     models.ActionBorrow,
		AssetAddress:   "0xdac17f958d2ee523a2206206994597c13d831ec7",
		Amount:         "1000000000000000000",
		TxHash:         "0xabc123",
		BlockNumber:    12345678,
		BlockTimestamp: time.Now(),
	}

	err = activityRepo.Create(ctx, activity)
	assert.NoError(t, err)
	assert.NotZero(t, activity.ID)
}

func TestActivityRepository_GetByUserID(t *testing.T) {
	db := setupTestDB(t)
	userRepo := NewUserRepository(db)
	activityRepo := NewActivityRepository(db)
	ctx := context.Background()

	// Create a test user with activities
	user := &models.User{WalletAddress: "0x1234567890123456789012345678901234567890"}
	err := userRepo.Create(ctx, user)
	require.NoError(t, err)

	// Create multiple activities
	actions := []models.ActionType{models.ActionDeposit, models.ActionBorrow, models.ActionRepay}
	for i, action := range actions {
		activity := &models.LoanActivity{
			UserID:         user.ID,
			ActionType:     action,
			AssetAddress:   "0xdac17f958d2ee523a2206206994597c13d831ec7",
			Amount:         "1000000000000000000",
			TxHash:         "0xabc" + string(rune('0'+i)),
			BlockNumber:    uint64(12345678 + i),
			BlockTimestamp: time.Now(),
		}
		err := activityRepo.Create(ctx, activity)
		require.NoError(t, err)
	}

	t.Run("Get all activities", func(t *testing.T) {
		activities, total, err := activityRepo.GetByUserID(ctx, user.ID, 0, 10)
		assert.NoError(t, err)
		assert.Equal(t, int64(3), total)
		assert.Len(t, activities, 3)
	})

	t.Run("Paginated activities", func(t *testing.T) {
		activities, total, err := activityRepo.GetByUserID(ctx, user.ID, 0, 2)
		assert.NoError(t, err)
		assert.Equal(t, int64(3), total)
		assert.Len(t, activities, 2)
	})

	t.Run("Non-existing user", func(t *testing.T) {
		activities, total, err := activityRepo.GetByUserID(ctx, 99999, 0, 10)
		assert.NoError(t, err)
		assert.Equal(t, int64(0), total)
		assert.Empty(t, activities)
	})
}

func TestActivityRepository_CountByUserAndType(t *testing.T) {
	db := setupTestDB(t)
	userRepo := NewUserRepository(db)
	activityRepo := NewActivityRepository(db)
	ctx := context.Background()

	// Create a test user
	user := &models.User{WalletAddress: "0x1234567890123456789012345678901234567890"}
	err := userRepo.Create(ctx, user)
	require.NoError(t, err)

	// Create activities with different types
	for i := 0; i < 5; i++ {
		activity := &models.LoanActivity{
			UserID:         user.ID,
			ActionType:     models.ActionBorrow,
			AssetAddress:   "0xdac17f958d2ee523a2206206994597c13d831ec7",
			Amount:         "1000000000000000000",
			TxHash:         "0xborrow" + string(rune('0'+i)),
			BlockNumber:    uint64(12345678 + i),
			BlockTimestamp: time.Now(),
		}
		err := activityRepo.Create(ctx, activity)
		require.NoError(t, err)
	}

	for i := 0; i < 3; i++ {
		activity := &models.LoanActivity{
			UserID:         user.ID,
			ActionType:     models.ActionRepay,
			AssetAddress:   "0xdac17f958d2ee523a2206206994597c13d831ec7",
			Amount:         "1000000000000000000",
			TxHash:         "0xrepay" + string(rune('0'+i)),
			BlockNumber:    uint64(12346000 + i),
			BlockTimestamp: time.Now(),
		}
		err := activityRepo.Create(ctx, activity)
		require.NoError(t, err)
	}

	t.Run("Count borrows", func(t *testing.T) {
		count, err := activityRepo.CountByUserAndType(ctx, user.ID, models.ActionBorrow)
		assert.NoError(t, err)
		assert.Equal(t, int64(5), count)
	})

	t.Run("Count repays", func(t *testing.T) {
		count, err := activityRepo.CountByUserAndType(ctx, user.ID, models.ActionRepay)
		assert.NoError(t, err)
		assert.Equal(t, int64(3), count)
	})

	t.Run("Count deposits (none)", func(t *testing.T) {
		count, err := activityRepo.CountByUserAndType(ctx, user.ID, models.ActionDeposit)
		assert.NoError(t, err)
		assert.Equal(t, int64(0), count)
	})
}

// ==================== SignatureRepository Tests ====================

func TestSignatureRepository_Create(t *testing.T) {
	db := setupTestDB(t)
	userRepo := NewUserRepository(db)
	sigRepo := NewSignatureRepository(db)
	ctx := context.Background()

	// Create a test user
	user := &models.User{WalletAddress: "0x1234567890123456789012345678901234567890"}
	err := userRepo.Create(ctx, user)
	require.NoError(t, err)

	signatureLog := &models.SignatureLog{
		RequestID:     "uuid-1234-5678",
		UserID:        user.ID,
		WalletAddress: user.WalletAddress,
		Market:        "USDT",
		AuthorizedLTV: 8000,
		AmountCap:     "50000000000000000000000",
		Nonce:         1,
		Deadline:      time.Now().Add(5 * time.Minute).Unix(),
		Signature:     "0xsig123",
		Status:        models.SignatureIssued,
	}

	err = sigRepo.Create(ctx, signatureLog)
	assert.NoError(t, err)
	assert.NotZero(t, signatureLog.ID)
}

func TestSignatureRepository_AllocateNonce(t *testing.T) {
	db := setupTestDB(t)
	userRepo := NewUserRepository(db)
	sigRepo := NewSignatureRepository(db)
	ctx := context.Background()

	t.Run("new wallet starts from zero and normalizes address", func(t *testing.T) {
		walletUpper := "0xABCDEF1234567890ABCDEF1234567890ABCDEF12"

		first, err := sigRepo.AllocateNonce(ctx, walletUpper)
		require.NoError(t, err)
		second, err := sigRepo.AllocateNonce(ctx, "0xabcdef1234567890abcdef1234567890abcdef12")
		require.NoError(t, err)

		assert.Equal(t, uint64(0), first)
		assert.Equal(t, uint64(1), second)
	})

	t.Run("existing signature logs seed the counter", func(t *testing.T) {
		user := &models.User{WalletAddress: "0x1234567890123456789012345678901234567890"}
		require.NoError(t, userRepo.Create(ctx, user))

		for _, nonce := range []uint64{0, 3, 5} {
			require.NoError(t, sigRepo.Create(ctx, &models.SignatureLog{
				RequestID:     fmt.Sprintf("seed-%d", nonce),
				UserID:        user.ID,
				WalletAddress: user.WalletAddress,
				Market:        "USDT",
				Nonce:         nonce,
				Deadline:      time.Now().Add(5 * time.Minute).Unix(),
				Signature:     fmt.Sprintf("0xseed%d", nonce),
				Status:        models.SignatureIssued,
			}))
		}

		nonce, err := sigRepo.AllocateNonce(ctx, user.WalletAddress)
		require.NoError(t, err)
		assert.Equal(t, uint64(6), nonce)
	})

	t.Run("legacy mixed-case wallet seeds the normalized counter", func(t *testing.T) {
		walletLower := "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd"
		user := &models.User{WalletAddress: walletLower}
		require.NoError(t, userRepo.Create(ctx, user))

		// 绕过 repository.Create 模拟历史大小写不一致数据。
		require.NoError(t, db.Create(&models.SignatureLog{
			RequestID:     "legacy-mixed-case",
			UserID:        user.ID,
			WalletAddress: "0xABCDEFABCDEFABCDEFABCDEFABCDEFABCDEFABCD",
			Market:        "USDT",
			Nonce:         8,
			Deadline:      time.Now().Add(5 * time.Minute).Unix(),
			Signature:     "0xlegacy",
			Status:        models.SignatureIssued,
		}).Error)

		nonce, err := sigRepo.AllocateNonce(ctx, walletLower)
		require.NoError(t, err)
		assert.Equal(t, uint64(9), nonce)
	})

	t.Run("counter catches up with a higher legacy nonce", func(t *testing.T) {
		wallet := "0x8888888888888888888888888888888888888888"
		first, err := sigRepo.AllocateNonce(ctx, wallet)
		require.NoError(t, err)
		assert.Equal(t, uint64(0), first)

		user := &models.User{WalletAddress: wallet}
		require.NoError(t, userRepo.Create(ctx, user))
		require.NoError(t, sigRepo.Create(ctx, &models.SignatureLog{
			RequestID:     "legacy-higher-nonce",
			UserID:        user.ID,
			WalletAddress: wallet,
			Market:        "USDT",
			Nonce:         10,
			Deadline:      time.Now().Add(5 * time.Minute).Unix(),
			Signature:     "0xlegacy",
			Status:        models.SignatureIssued,
		}))

		next, err := sigRepo.AllocateNonce(ctx, wallet)
		require.NoError(t, err)
		assert.Equal(t, uint64(11), next)
	})

	t.Run("concurrent allocations are unique", func(t *testing.T) {
		const workers = 32
		wallet := "0x9999999999999999999999999999999999999999"
		nonces := make(chan uint64, workers)
		errs := make(chan error, workers)
		var wg sync.WaitGroup

		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				nonce, err := sigRepo.AllocateNonce(ctx, wallet)
				if err != nil {
					errs <- err
					return
				}
				nonces <- nonce
			}()
		}

		wg.Wait()
		close(nonces)
		close(errs)

		for err := range errs {
			require.NoError(t, err)
		}

		allocated := make([]uint64, 0, workers)
		for nonce := range nonces {
			allocated = append(allocated, nonce)
		}
		sort.Slice(allocated, func(i, j int) bool { return allocated[i] < allocated[j] })
		require.Len(t, allocated, workers)
		for i, nonce := range allocated {
			assert.Equal(t, uint64(i), nonce)
		}
	})
}

func TestSignatureRepository_CreateRejectsDuplicateWalletNonce(t *testing.T) {
	db := setupTestDB(t)
	userRepo := NewUserRepository(db)
	sigRepo := NewSignatureRepository(db)
	ctx := context.Background()

	user := &models.User{WalletAddress: "0x7777777777777777777777777777777777777777"}
	require.NoError(t, userRepo.Create(ctx, user))

	newLog := func(requestID string) *models.SignatureLog {
		return &models.SignatureLog{
			RequestID:     requestID,
			UserID:        user.ID,
			WalletAddress: user.WalletAddress,
			Market:        "USDT",
			Nonce:         9,
			Deadline:      time.Now().Add(5 * time.Minute).Unix(),
			Signature:     "0xsig",
			Status:        models.SignatureIssued,
		}
	}

	require.NoError(t, sigRepo.Create(ctx, newLog("first")))
	assert.Error(t, sigRepo.Create(ctx, newLog("duplicate")))
}

func TestSignatureRepository_MarkAsUsed(t *testing.T) {
	db := setupTestDB(t)
	userRepo := NewUserRepository(db)
	sigRepo := NewSignatureRepository(db)
	ctx := context.Background()

	// Create a test user
	user := &models.User{WalletAddress: "0x1234567890123456789012345678901234567890"}
	err := userRepo.Create(ctx, user)
	require.NoError(t, err)

	// Create a signature
	requestID := "uuid-test-1234"
	sig := &models.SignatureLog{
		RequestID:     requestID,
		UserID:        user.ID,
		WalletAddress: user.WalletAddress,
		Market:        "USDT",
		Nonce:         1,
		Deadline:      time.Now().Add(5 * time.Minute).Unix(),
		Signature:     "0xsig123",
		Status:        models.SignatureIssued,
	}
	err = sigRepo.Create(ctx, sig)
	require.NoError(t, err)

	t.Run("Mark as used", func(t *testing.T) {
		err := sigRepo.MarkAsUsed(ctx, requestID)
		assert.NoError(t, err)

		// Verify status changed
		var updated models.SignatureLog
		err = db.Where("request_id = ?", requestID).First(&updated).Error
		require.NoError(t, err)
		assert.Equal(t, models.SignatureUsed, updated.Status)
	})

	t.Run("Mark non-existing", func(t *testing.T) {
		err := sigRepo.MarkAsUsed(ctx, "non-existing-id")
		// Should not error, just no rows affected
		assert.NoError(t, err)
	})
}

func TestSignatureRepository_GetRecentByWallet(t *testing.T) {
	db := setupTestDB(t)
	userRepo := NewUserRepository(db)
	sigRepo := NewSignatureRepository(db)
	ctx := context.Background()

	// Create a test user
	user := &models.User{WalletAddress: "0x1234567890123456789012345678901234567890"}
	err := userRepo.Create(ctx, user)
	require.NoError(t, err)

	// Create recent signatures
	for i := 0; i < 5; i++ {
		sig := &models.SignatureLog{
			RequestID:     "uuid-recent-" + string(rune('0'+i)),
			UserID:        user.ID,
			WalletAddress: user.WalletAddress,
			Market:        "USDT",
			Nonce:         uint64(i + 1),
			Deadline:      time.Now().Add(5 * time.Minute).Unix(),
			Signature:     "0xsig" + string(rune('0'+i)),
			Status:        models.SignatureIssued,
		}
		err := sigRepo.Create(ctx, sig)
		require.NoError(t, err)
	}

	since := time.Now().Add(-1 * time.Minute)
	count, err := sigRepo.GetRecentByWallet(ctx, user.WalletAddress, since)
	assert.NoError(t, err)
	assert.Equal(t, int64(5), count)
}

// ==================== Error Type Tests ====================

func TestErrorTypes(t *testing.T) {
	assert.NotNil(t, ErrUserNotFound)
	assert.NotNil(t, ErrUserAlreadyExists)
	assert.NotNil(t, ErrCreditNotFound)
	assert.NotNil(t, ErrActivityNotFound)
	assert.NotNil(t, ErrSignatureNotFound)

	// Verify the public error messages remain stable.
	assert.Equal(t, "用户不存在", ErrUserNotFound.Error())
	assert.Equal(t, "信用记录不存在", ErrCreditNotFound.Error())
}

// ==================== Boundary Condition Tests ====================

func TestUserRepository_WalletAddressNormalization(t *testing.T) {
	db := setupTestDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	testCases := []struct {
		input    string
		expected string
	}{
		{"0x1234567890ABCDEF1234567890ABCDEF12345678", "0x1234567890abcdef1234567890abcdef12345678"},
		{"0xabcdef1234567890abcdef1234567890abcdef12", "0xabcdef1234567890abcdef1234567890abcdef12"},
	}

	for i, tc := range testCases {
		t.Run("", func(t *testing.T) {
			user := &models.User{WalletAddress: tc.input + string(rune('0'+i))}
			err := repo.Create(ctx, user)
			require.NoError(t, err)

			// Wallet should be lowercased
			found, err := repo.GetByID(ctx, user.ID)
			require.NoError(t, err)
			// Verify the wallet address is stored in lowercase
			assert.NotContains(t, found.WalletAddress, "ABCDEF")
		})
	}
}

func TestActivityRepository_EmptyResults(t *testing.T) {
	db := setupTestDB(t)
	activityRepo := NewActivityRepository(db)
	ctx := context.Background()

	// Query for non-existing user
	activities, total, err := activityRepo.GetByUserID(ctx, 99999, 0, 10)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.Empty(t, activities)
}

func TestCreditRepository_UpdateExisting(t *testing.T) {
	db := setupTestDB(t)
	userRepo := NewUserRepository(db)
	creditRepo := NewCreditRepository(db)
	ctx := context.Background()

	// Create a test user
	user := &models.User{WalletAddress: "0x1234567890123456789012345678901234567890"}
	err := userRepo.Create(ctx, user)
	require.NoError(t, err)

	// Create initial credit
	credit := &models.UserCredit{
		UserID: user.ID,
		Score:  500,
		Tier:   "C",
	}
	err = creditRepo.CreateOrUpdate(ctx, credit)
	require.NoError(t, err)
	initialID := credit.ID

	// Update credit
	updatedCredit := &models.UserCredit{
		UserID: user.ID,
		Score:  800,
		Tier:   "A",
	}
	err = creditRepo.CreateOrUpdate(ctx, updatedCredit)
	require.NoError(t, err)

	// Verify update (should have same ID or be updated)
	found, err := creditRepo.GetByUserID(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, 800, found.Score)
	assert.Equal(t, "A", found.Tier)
	assert.Equal(t, initialID, found.ID) // Same record should be updated
}

// ==================== Benchmarks ====================

func BenchmarkUserRepository_GetByWallet(b *testing.B) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	db.AutoMigrate(&models.User{})
	repo := NewUserRepository(db)
	ctx := context.Background()

	wallet := "0x1234567890123456789012345678901234567890"
	user := &models.User{WalletAddress: wallet}
	repo.Create(ctx, user)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		repo.GetByWallet(ctx, wallet)
	}
}

func BenchmarkActivityRepository_GetByUserID(b *testing.B) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	db.AutoMigrate(&models.User{}, &models.LoanActivity{})
	userRepo := NewUserRepository(db)
	activityRepo := NewActivityRepository(db)
	ctx := context.Background()

	user := &models.User{WalletAddress: "0x1234567890123456789012345678901234567890"}
	userRepo.Create(ctx, user)

	// Create some activities
	for i := 0; i < 100; i++ {
		activity := &models.LoanActivity{
			UserID:         user.ID,
			ActionType:     models.ActionBorrow,
			AssetAddress:   "0xdac17f958d2ee523a2206206994597c13d831ec7",
			Amount:         "1000000000000000000",
			TxHash:         "0x" + string(rune('a'+i%26)) + string(rune('0'+i/26)),
			BlockNumber:    uint64(12345678 + i),
			BlockTimestamp: time.Now(),
		}
		activityRepo.Create(ctx, activity)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		activityRepo.GetByUserID(ctx, user.ID, 0, 20)
	}
}
