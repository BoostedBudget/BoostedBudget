//go:build integration

package repository

// system_health_repository_paddle_linked_count_integration_test.go — real-Postgres proof of
// Issue #1413 G5 item 3: GetActiveSubscriptionCount (system_health_repository.go) is
// deliberately a RAW-status query — it must not count a comped user whose real Paddle
// subscription has lapsed, because this metric is revenue-adjacent (Paddle-linked ACTIVE
// subscriptions) and a free admin comp inflating it would misrepresent real revenue.
//
// The two pre-existing pgxmock tests (system_health_repository_test.go:174, :188) only match
// the query STRING and return a canned row count ("99") — they exercise zero SQL semantics and
// would pass completely unchanged even if the predicate were switched to count comped users
// too. Only a real database, with a real manual_tier_override UPDATE, can prove the WHERE
// clause's actual filtering behavior — hence the build tag.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestGetActiveSubscriptionCount_Integration -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
)

// createPaddleLinkedCountUser creates a user row with the given Paddle subscription ID / status
// via ProfileRepository.CreateUser, so encryption/blind-indexing round-trips exactly as
// production does. Callers register their own cleanup.
func createPaddleLinkedCountUser(t *testing.T, profileRepo *profileRepository, suffix, paddleSubID, status string) *domain.User {
	t.Helper()
	var subIDPtr *string
	if paddleSubID != "" {
		subIDPtr = &paddleSubID
	}
	user := &domain.User{
		ClerkUserID:          "clerk_plc_" + suffix,
		Email:                "plc-" + suffix + "@integration.example",
		SubscriptionTier:     "premium",
		SubscriptionStatus:   status,
		PaddleSubscriptionID: subIDPtr,
		PrimaryRegion:        "iad",
		IsActive:             true,
	}
	require.NoError(t, profileRepo.CreateUser(context.Background(), user))
	return user
}

// TestGetActiveSubscriptionCount_Integration_CompedLapsedUser_NotCounted is the #1413 G5 pin: a
// user whose REAL Paddle subscription lapsed (raw status inactive/cancelled) but who carries a
// live admin comp (manual_tier_override) — and would therefore read as effectively "active" via
// domain.User.ComputeEffectiveStatus — must NOT be counted by this metric. Proves the query
// genuinely filters on raw subscription_status, not the comp-forced effective status, using a
// before/after delta so pre-existing rows in a shared dev database can't skew the assertion.
func TestGetActiveSubscriptionCount_Integration_CompedLapsedUser_NotCounted(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	profileRepo := NewProfileRepository(pool, enc)
	healthRepo := NewSystemHealthRepository(pool)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	baseline, err := healthRepo.GetActiveSubscriptionCount(ctx)
	require.NoError(t, err)

	// A genuinely active, real Paddle subscription — the metric's true-positive case, so the
	// "unchanged" assertion below is meaningful proof of exclusion, not just an always-zero query.
	realActive := createPaddleLinkedCountUser(t, profileRepo, suffix+"_real", "sub_real_"+suffix, "active")
	t.Cleanup(func() { pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", realActive.ID) }) //nolint:errcheck

	afterReal, err := healthRepo.GetActiveSubscriptionCount(ctx)
	require.NoError(t, err)
	require.Equal(t, baseline+1, afterReal, "a genuinely active Paddle-linked user must be counted")

	// THE PIN: a lapsed real subscription (raw "inactive") with a LIVE comp layered on top.
	// ComputeEffectiveStatus() for this user is "active", but the real Paddle relationship is
	// dead — this must not inflate the count.
	compedInactive := createPaddleLinkedCountUser(t, profileRepo, suffix+"_comped_inactive", "sub_lapsed_"+suffix, "inactive")
	t.Cleanup(func() { pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", compedInactive.ID) }) //nolint:errcheck
	overrideEndsAt := time.Now().Add(30 * 24 * time.Hour)
	_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = $1, manual_tier_ends_at = $2 WHERE id = $3`,
		"household_2", overrideEndsAt, compedInactive.ID)
	require.NoError(t, err)

	// The "cancelled" sibling — the other read-only raw status a comp can sit on top of (mirrors
	// the checkout-409 pin's inactive/cancelled pair).
	compedCancelled := createPaddleLinkedCountUser(t, profileRepo, suffix+"_comped_cancelled", "sub_cancelled_"+suffix, "cancelled")
	t.Cleanup(func() { pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", compedCancelled.ID) }) //nolint:errcheck
	_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = $1, manual_tier_ends_at = $2 WHERE id = $3`,
		"household_3", overrideEndsAt, compedCancelled.ID)
	require.NoError(t, err)

	afterComped, err := healthRepo.GetActiveSubscriptionCount(ctx)
	require.NoError(t, err)
	require.Equal(t, afterReal, afterComped,
		"THE PIN: comped users with a lapsed real Paddle subscription must NOT be counted, even "+
			"though their effective status is active — this metric tracks real Paddle billing "+
			"relationships, not comp-granted access")
}
