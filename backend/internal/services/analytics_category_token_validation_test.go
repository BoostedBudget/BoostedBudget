package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// Issue #1626 Phase 2, review finding X2 — a REGRESSION this branch introduced, closed here.
//
// GetCategoryTotalForPeriod's display used to be MAX(t.effective_category_name), so a malformed
// `custom:` token was only ever compared as TEXT against effectiveCategoryExpr's output: it matched
// no transactions and the endpoint answered 200 with a zero total. The fix for the UUID-label bug
// replaced that MAX with a `cc.id = $5::uuid` scalar subquery, and the suffix reaches Postgres
// UNPARSED — so `category=custom:not-a-uuid` became invalid-input-syntax, which MapError classifies
// as a SERVER error. A user-supplied query string must not be able to produce a 500.
//
// ⚠ VALIDATED AT THE SERVICE BOUNDARY, not in the Huma input struct, and that is deliberate: the
// repository is the layer that casts, the analytics methods are documented as AI-tool-callable
// (CLAUDE.md), and a handler-only check would leave every non-HTTP caller free to reach the cast.
// The mapper turns the sentinel into 400 — see TestLoggingMapError_400_InvalidCategoryToken.
//
// ⚠ THE VALIDATION IS NOT "looks like a uuid". It is uuid.Parse on the SUFFIX ONLY, and only when
// the `custom:` prefix is present. A PFC token is not a UUID and must stay untouched.
func TestGetCategoryTotalForPeriod_RejectsAMalformedCustomToken(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)

	// Each rejected case must ALSO prove the repository was never reached: the whole point is that
	// the string never gets as far as the ::uuid cast.
	for _, tc := range []struct {
		name     string
		category string
	}{
		{"not a uuid at all", "custom:not-a-uuid"},
		{"empty suffix", "custom:"},
		{"a uuid with trailing junk", "custom:9f1c2b3a-0000-4000-8000-000000000001x"},
		{"sql-shaped payload", "custom:1' OR '1'='1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			mockRepo := &mockAnalyticsRepository{
				getCategoryTotalForPeriodFn: func(context.Context, string, string, time.Time, time.Time) (*domain.CategoryTotalRow, error) {
					called = true
					return &domain.CategoryTotalRow{}, nil
				},
			}
			result, err := NewAnalyticsService(mockRepo).GetCategoryTotalForPeriod(ctx, "user-1", tc.category, start, end)

			require.Error(t, err, "a malformed custom token must be rejected, not forwarded to a ::uuid cast")
			require.ErrorIs(t, err, ErrInvalidCategoryToken,
				"the sentinel is what MapError turns into 400; a bare error would still be a 500")
			assert.Nil(t, result)
			assert.False(t, called, "the repository must never see a token it would fail to cast")
		})
	}
}

// TestGetCategoryTotalForPeriod_AcceptsTheTokensItIsSupposedTo is the other half, and it is what
// stops the fix from being a blanket rejection of everything containing a colon.
func TestGetCategoryTotalForPeriod_AcceptsTheTokensItIsSupposedTo(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)

	for _, tc := range []struct{ name, category string }{
		{"a PFC primary, which is not a uuid and never was", "FOOD_AND_DRINK"},
		{"a PFC detailed", "FOOD_AND_DRINK_FAST_FOOD"},
		{"the uncategorized sentinel", "UNCATEGORIZED"},
		{"a well-formed custom token", "custom:9f1c2b3a-0000-4000-8000-000000000001"},
		{"a well-formed custom token in upper case", "custom:9F1C2B3A-0000-4000-8000-000000000001"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sawCategory string
			mockRepo := &mockAnalyticsRepository{
				getCategoryTotalForPeriodFn: func(_ context.Context, _ string, category string, _ time.Time, _ time.Time) (*domain.CategoryTotalRow, error) {
					sawCategory = category
					return &domain.CategoryTotalRow{Total: 12.5, TransactionCount: 2}, nil
				},
			}
			result, err := NewAnalyticsService(mockRepo).GetCategoryTotalForPeriod(ctx, "user-1", tc.category, start, end)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, tc.category, sawCategory,
				"the token must reach the repository UNCHANGED — validation is a gate, not a rewrite")
			assert.Equal(t, tc.category, result.Category)
		})
	}
}

// TestGetCategoryTotalForPeriod_MalformedTokenIsNotMistakenForARepositoryFailure keeps the two
// error paths distinguishable: a real repository error must NOT be reported as a bad request, or
// the 400 stops meaning anything.
func TestGetCategoryTotalForPeriod_MalformedTokenIsNotMistakenForARepositoryFailure(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)

	mockRepo := &mockAnalyticsRepository{
		getCategoryTotalForPeriodFn: func(context.Context, string, string, time.Time, time.Time) (*domain.CategoryTotalRow, error) {
			return nil, errors.New("connection reset")
		},
	}
	_, err := NewAnalyticsService(mockRepo).GetCategoryTotalForPeriod(ctx, "user-1", "FOOD_AND_DRINK", start, end)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrInvalidCategoryToken,
		"a database failure is not a malformed request; conflating them would answer 400 to an outage")
}
