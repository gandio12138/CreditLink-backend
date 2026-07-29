package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/creditlink/backend/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRiskRepositoryReplaceRiskParamsPrunesStaleAssets(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRiskRepository(db)
	ctx := context.Background()

	require.NoError(t, repo.ReplaceRiskParams(ctx, []models.CurrentRiskParams{
		{AssetAddress: "0x1111111111111111111111111111111111111111", AssetSymbol: "USDC", PriceOracleAddress: "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{AssetAddress: "0x2222222222222222222222222222222222222222", AssetSymbol: "WETH", PriceOracleAddress: "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}))
	require.NoError(t, repo.ReplaceRiskParams(ctx, []models.CurrentRiskParams{
		{AssetAddress: "0x1111111111111111111111111111111111111111", AssetSymbol: "USDC", PriceOracleAddress: "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
	}))

	params, err := repo.GetAllRiskParams(ctx)
	require.NoError(t, err)
	require.Len(t, params, 1)
	require.Equal(t, "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", params[0].PriceOracleAddress)
}

func TestRiskRepositoryReplaceRiskParamsIsAtomic(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRiskRepository(db)
	ctx := context.Background()
	original := models.CurrentRiskParams{AssetAddress: "0x1111111111111111111111111111111111111111", AssetSymbol: "USDC"}
	require.NoError(t, repo.SaveRiskParams(ctx, &original))

	err := repo.ReplaceRiskParams(ctx, []models.CurrentRiskParams{
		{AssetAddress: "0x2222222222222222222222222222222222222222", AssetSymbol: "WETH"},
		{AssetAddress: "0x2222222222222222222222222222222222222222", AssetSymbol: "DUPLICATE"},
	})
	// Upsert makes duplicate addresses valid; force a transaction failure and verify rollback.
	require.NoError(t, err)
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:fail_replace", func(tx *gorm.DB) {
		if symbol, ok := tx.Statement.Dest.(*models.CurrentRiskParams); ok && symbol.AssetSymbol == "FAIL" {
			tx.AddError(errors.New("forced failure"))
		}
	}))
	err = repo.ReplaceRiskParams(ctx, []models.CurrentRiskParams{{
		AssetAddress: "0x3333333333333333333333333333333333333333",
		AssetSymbol:  "FAIL",
	}})
	require.Error(t, err)

	params, err := repo.GetAllRiskParams(ctx)
	require.NoError(t, err)
	require.Len(t, params, 1)
	require.Equal(t, "0x2222222222222222222222222222222222222222", params[0].AssetAddress)
}
