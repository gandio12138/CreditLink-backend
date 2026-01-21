package credit

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDetermineTier tests tier determination based on credit score boundaries
func TestDetermineTier(t *testing.T) {
	testCases := []struct {
		name     string
		score    int
		expected string
	}{
		// Boundary conditions for tier D (0-399)
		{"Score 0 - D tier minimum", 0, "D"},
		{"Score 1 - D tier", 1, "D"},
		{"Score 199 - D tier middle", 199, "D"},
		{"Score 399 - D tier maximum", 399, "D"},

		// Boundary conditions for tier C (400-599)
		{"Score 400 - C tier minimum (boundary)", 400, "C"},
		{"Score 401 - C tier", 401, "C"},
		{"Score 500 - C tier middle", 500, "C"},
		{"Score 599 - C tier maximum", 599, "C"},

		// Boundary conditions for tier B (600-799)
		{"Score 600 - B tier minimum (boundary)", 600, "B"},
		{"Score 601 - B tier", 601, "B"},
		{"Score 700 - B tier middle", 700, "B"},
		{"Score 799 - B tier maximum", 799, "B"},

		// Boundary conditions for tier A (800-899)
		{"Score 800 - A tier minimum (boundary)", 800, "A"},
		{"Score 801 - A tier", 801, "A"},
		{"Score 850 - A tier middle", 850, "A"},
		{"Score 899 - A tier maximum", 899, "A"},

		// Boundary conditions for tier S (900-1000)
		{"Score 900 - S tier minimum (boundary)", 900, "S"},
		{"Score 901 - S tier", 901, "S"},
		{"Score 950 - S tier middle", 950, "S"},
		{"Score 1000 - S tier maximum", 1000, "S"},

		// Edge cases - below minimum
		{"Score -1 - below minimum", -1, "D"},
		{"Score -100 - far below minimum", -100, "D"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tier := determineTier(tc.score)
			assert.Equal(t, tc.expected, tier)
		})
	}
}

// TestClamp tests the clamp function for boundary conditions
func TestClamp(t *testing.T) {
	testCases := []struct {
		name     string
		value    int
		minVal   int
		maxVal   int
		expected int
	}{
		// Normal cases
		{"Value in range - middle", 500, 0, 1000, 500},
		{"Value in range - at min", 0, 0, 1000, 0},
		{"Value in range - at max", 1000, 0, 1000, 1000},

		// Below minimum
		{"Value below min", -1, 0, 1000, 0},
		{"Value far below min", -500, 0, 1000, 0},

		// Above maximum
		{"Value above max", 1001, 0, 1000, 1000},
		{"Value far above max", 2000, 0, 1000, 1000},

		// Edge cases
		{"Zero range", 5, 0, 0, 0},
		{"Negative range", -50, -100, -10, -50},
		{"Value at negative min", -100, -100, -10, -100},
		{"Value at negative max", -10, -100, -10, -10},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := clamp(tc.value, tc.minVal, tc.maxVal)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestMin tests the min helper function
func TestMin(t *testing.T) {
	testCases := []struct {
		name     string
		a        int
		b        int
		expected int
	}{
		{"a < b", 5, 10, 5},
		{"a > b", 10, 5, 5},
		{"a == b", 5, 5, 5},
		{"negative a < b", -10, 5, -10},
		{"negative a > b", 5, -10, -10},
		{"both negative", -5, -10, -10},
		{"zero and positive", 0, 5, 0},
		{"zero and negative", 0, -5, -5},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := min(tc.a, tc.b)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestTierConfig tests tier configuration validity
func TestTierConfig(t *testing.T) {
	// Verify all expected tiers exist
	expectedTiers := []string{"S", "A", "B", "C", "D"}
	for _, tier := range expectedTiers {
		_, exists := TierConfig[tier]
		assert.True(t, exists, "Tier %s should exist in TierConfig", tier)
	}

	// Verify tier hierarchy (min scores are in descending order)
	assert.GreaterOrEqual(t, TierConfig["S"].MinScore, TierConfig["A"].MinScore, "S tier should have higher min score than A")
	assert.GreaterOrEqual(t, TierConfig["A"].MinScore, TierConfig["B"].MinScore, "A tier should have higher min score than B")
	assert.GreaterOrEqual(t, TierConfig["B"].MinScore, TierConfig["C"].MinScore, "B tier should have higher min score than C")
	assert.GreaterOrEqual(t, TierConfig["C"].MinScore, TierConfig["D"].MinScore, "C tier should have higher min score than D")

	// Verify LTV hierarchy (higher tiers get higher LTV)
	assert.GreaterOrEqual(t, TierConfig["S"].LTV, TierConfig["A"].LTV, "S tier should have higher LTV than A")
	assert.GreaterOrEqual(t, TierConfig["A"].LTV, TierConfig["B"].LTV, "A tier should have higher LTV than B")
	assert.GreaterOrEqual(t, TierConfig["B"].LTV, TierConfig["C"].LTV, "B tier should have higher LTV than C")
	assert.GreaterOrEqual(t, TierConfig["C"].LTV, TierConfig["D"].LTV, "C tier should have higher LTV than D")

	// Verify D tier has 0 LTV (no borrowing allowed)
	assert.Equal(t, 0, TierConfig["D"].LTV, "D tier should have 0 LTV")
	assert.Equal(t, "0", TierConfig["D"].MaxBorrow, "D tier should have 0 max borrow")
}

// TestTierConfigLTVValues tests specific LTV values match PRD specifications
func TestTierConfigLTVValues(t *testing.T) {
	testCases := []struct {
		tier         string
		expectedLTV  int
		description  string
	}{
		{"S", 9500, "S tier should have 95% LTV"},
		{"A", 9000, "A tier should have 90% LTV"},
		{"B", 8000, "B tier should have 80% LTV"},
		{"C", 7000, "C tier should have 70% LTV"},
		{"D", 0, "D tier should have 0% LTV"},
	}

	for _, tc := range testCases {
		t.Run(tc.tier, func(t *testing.T) {
			assert.Equal(t, tc.expectedLTV, TierConfig[tc.tier].LTV, tc.description)
		})
	}
}

// TestTierConfigMinScores tests min score boundaries match PRD
func TestTierConfigMinScores(t *testing.T) {
	testCases := []struct {
		tier        string
		minScore    int
		description string
	}{
		{"S", 900, "S tier min score should be 900"},
		{"A", 800, "A tier min score should be 800"},
		{"B", 600, "B tier min score should be 600"},
		{"C", 400, "C tier min score should be 400"},
		{"D", 0, "D tier min score should be 0"},
	}

	for _, tc := range testCases {
		t.Run(tc.tier, func(t *testing.T) {
			assert.Equal(t, tc.minScore, TierConfig[tc.tier].MinScore, tc.description)
		})
	}
}

// TestBaseScore tests that base score is set correctly
func TestBaseScore(t *testing.T) {
	assert.Equal(t, 500, BaseScore, "Base score should be 500")
}

// TestCreditScoreStruct tests CreditScore struct
func TestCreditScoreStruct(t *testing.T) {
	score := CreditScore{
		Score:     850,
		Tier:      "A",
		MaxLTV:    9000,
		MaxBorrow: "200000000000000000000000",
	}

	assert.Equal(t, 850, score.Score)
	assert.Equal(t, "A", score.Tier)
	assert.Equal(t, 9000, score.MaxLTV)
	assert.Equal(t, "200000000000000000000000", score.MaxBorrow)
}

// TestCreditFactorsStruct tests CreditFactors struct initialization
func TestCreditFactorsStruct(t *testing.T) {
	factors := CreditFactors{
		// Internal factors
		RepaymentBonus:     100,
		LiquidationPenalty: -200,
		BorrowHistoryBonus: 50,
		LoyaltyBonus:       30,
		HealthFactorBonus:  20,
		// External factors
		WalletAgeBonus: 40,
		ActivityBonus:  20,
		DiversityBonus: 30,
		NetWorthBonus:  40,
		// Risk factors
		BlacklistPenalty:     -50,
		HighFrequencyPenalty: -30,
		AssetDeclinePenalty:  -40,
		BotBehaviorPenalty:   0,
	}

	// Verify internal factors
	assert.Equal(t, 100, factors.RepaymentBonus)
	assert.Equal(t, -200, factors.LiquidationPenalty)
	assert.Equal(t, 50, factors.BorrowHistoryBonus)
	assert.Equal(t, 30, factors.LoyaltyBonus)
	assert.Equal(t, 20, factors.HealthFactorBonus)

	// Verify external factors
	assert.Equal(t, 40, factors.WalletAgeBonus)
	assert.Equal(t, 20, factors.ActivityBonus)
	assert.Equal(t, 30, factors.DiversityBonus)
	assert.Equal(t, 40, factors.NetWorthBonus)

	// Verify risk factors
	assert.Equal(t, -50, factors.BlacklistPenalty)
	assert.Equal(t, -30, factors.HighFrequencyPenalty)
	assert.Equal(t, -40, factors.AssetDeclinePenalty)
	assert.Equal(t, 0, factors.BotBehaviorPenalty)

	// Calculate total contribution
	totalInternal := factors.RepaymentBonus + factors.LiquidationPenalty +
		factors.BorrowHistoryBonus + factors.LoyaltyBonus + factors.HealthFactorBonus
	totalExternal := factors.WalletAgeBonus + factors.ActivityBonus +
		factors.DiversityBonus + factors.NetWorthBonus
	totalRisk := factors.BlacklistPenalty + factors.HighFrequencyPenalty +
		factors.AssetDeclinePenalty + factors.BotBehaviorPenalty

	assert.Equal(t, 0, totalInternal, "Total internal with liquidation should be 0")
	assert.Equal(t, 130, totalExternal, "Total external should be 130")
	assert.Equal(t, -120, totalRisk, "Total risk should be -120")
}

// TestScoreBoundaries tests that calculated scores would be clamped correctly
func TestScoreBoundaries(t *testing.T) {
	testCases := []struct {
		name        string
		baseScore   int
		bonuses     int
		penalties   int
		expectedMin int
		expectedMax int
	}{
		{
			name:        "Normal case",
			baseScore:   500,
			bonuses:     200,
			penalties:   -50,
			expectedMin: 0,
			expectedMax: 1000,
		},
		{
			name:        "Maximum bonuses",
			baseScore:   500,
			bonuses:     600,
			penalties:   0,
			expectedMin: 0,
			expectedMax: 1000,
		},
		{
			name:        "Maximum penalties",
			baseScore:   500,
			bonuses:     0,
			penalties:   -600,
			expectedMin: 0,
			expectedMax: 1000,
		},
		{
			name:        "Extreme bonuses exceeding max",
			baseScore:   500,
			bonuses:     1000,
			penalties:   0,
			expectedMin: 0,
			expectedMax: 1000,
		},
		{
			name:        "Extreme penalties below min",
			baseScore:   500,
			bonuses:     0,
			penalties:   -1000,
			expectedMin: 0,
			expectedMax: 1000,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rawScore := tc.baseScore + tc.bonuses + tc.penalties
			clampedScore := clamp(rawScore, 0, 1000)

			assert.GreaterOrEqual(t, clampedScore, tc.expectedMin)
			assert.LessOrEqual(t, clampedScore, tc.expectedMax)
		})
	}
}

// TestTierDeterminationWithScoreBoundaries tests tier transition at exact boundaries
func TestTierDeterminationWithScoreBoundaries(t *testing.T) {
	// Test exact boundary transitions
	boundaries := []struct {
		belowScore     int
		belowTier      string
		atScore        int
		atTier         string
	}{
		{399, "D", 400, "C"},
		{599, "C", 600, "B"},
		{799, "B", 800, "A"},
		{899, "A", 900, "S"},
	}

	for _, b := range boundaries {
		t.Run(b.atTier+"_boundary", func(t *testing.T) {
			// Just below boundary
			assert.Equal(t, b.belowTier, determineTier(b.belowScore),
				"Score %d should be tier %s", b.belowScore, b.belowTier)

			// Exactly at boundary
			assert.Equal(t, b.atTier, determineTier(b.atScore),
				"Score %d should be tier %s", b.atScore, b.atTier)
		})
	}
}

// TestLTVCalculation tests that LTV is correctly assigned based on tier
func TestLTVCalculation(t *testing.T) {
	testCases := []struct {
		score       int
		expectedLTV int
	}{
		{950, 9500}, // S tier
		{900, 9500}, // S tier boundary
		{850, 9000}, // A tier
		{800, 9000}, // A tier boundary
		{700, 8000}, // B tier
		{600, 8000}, // B tier boundary
		{500, 7000}, // C tier
		{400, 7000}, // C tier boundary
		{300, 0},    // D tier
		{0, 0},      // D tier minimum
	}

	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			tier := determineTier(tc.score)
			ltv := TierConfig[tier].LTV
			assert.Equal(t, tc.expectedLTV, ltv, "Score %d (tier %s) should have LTV %d", tc.score, tier, tc.expectedLTV)
		})
	}
}

// BenchmarkDetermineTier benchmarks tier determination performance
func BenchmarkDetermineTier(b *testing.B) {
	scores := []int{0, 100, 399, 400, 500, 599, 600, 700, 799, 800, 850, 899, 900, 950, 1000}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, score := range scores {
			determineTier(score)
		}
	}
}

// BenchmarkClamp benchmarks clamp function performance
func BenchmarkClamp(b *testing.B) {
	values := []int{-100, 0, 500, 1000, 1500}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, v := range values {
			clamp(v, 0, 1000)
		}
	}
}
