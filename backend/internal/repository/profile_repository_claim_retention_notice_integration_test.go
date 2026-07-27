//go:build integration

package repository

// Integration tests for ClaimRetentionNotice30dSent / ClaimRetentionNotice7dSent (Issue #1413
// Medium finding M9, Codex review round 2).
//
// retention_cleanup_check.go previously queried eligible users, then checked the comp state on
// the fetched object before sending a 30-day/7-day warning and marking the notice sent. A comp
// granted after the query wasn't observed, and nothing prevented two workers (or a retry) from
// both sending. These claim methods stamp the notice atomically — send only follows a
// successful claim — reusing R1's noManualOverrideAtAllPredicate (the same "any override, live
// or lapsed" rule the candidate queries themselves now enforce), NOT
// noActiveManualOverridePredicate. pgxmock cannot validate this — it only matches query text,
// not the live row state the predicate depends on — so a real database is required.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestClaimRetentionNotice -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// createClaimRetentionNoticeFixtureUser inserts a plain user row, then sets is_deleted,
// manual_tier_override, and manual_tier_ends_at directly via UPDATE, and registers cleanup.
func createClaimRetentionNoticeFixtureUser(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repo ProfileRepository,
	suffix string,
	isDeleted bool,
	manualTierOverride *string,
	manualTierEndsAt *time.Time,
) *domain.User {
	t.Helper()

	user := &domain.User{
		ClerkUserID:        "clerk_claimnotice_" + suffix,
		Email:              "claimnotice-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) }) //nolint:errcheck

	_, err = pool.Exec(ctx, `
		UPDATE users
		SET is_deleted = $1,
		    manual_tier_override = $2,
		    manual_tier_ends_at = $3
		WHERE id = $4`,
		isDeleted, manualTierOverride, manualTierEndsAt, user.ID,
	)
	require.NoError(t, err)

	user.IsDeleted = isDeleted
	user.ManualTierOverride = manualTierOverride
	user.ManualTierEndsAt = manualTierEndsAt
	return user
}

// claimNotice30d/claimNotice7d let the table-driven tests below exercise both methods with the
// exact production functions rather than a hand-copied restatement.
func claimNotice30d(repo ProfileRepository) func(context.Context, string) (bool, error) {
	return repo.ClaimRetentionNotice30dSent
}
func claimNotice7d(repo ProfileRepository) func(context.Context, string) (bool, error) {
	return repo.ClaimRetentionNotice7dSent
}

func sentAtColumn30d() string { return "retention_notice_30d_sent_at" }
func sentAtColumn7d() string  { return "retention_notice_7d_sent_at" }

// TestClaimRetentionNotice_Integration_Eligible_Claims proves the happy path for both notice
// windows: a genuinely eligible user (no override at all) can claim the notice, and the
// corresponding *_sent_at column is stamped.
func TestClaimRetentionNotice_Integration_Eligible_Claims(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	tests := []struct {
		name   string
		claim  func(context.Context, string) (bool, error)
		column string
	}{
		{"30d", claimNotice30d(repo), sentAtColumn30d()},
		{"7d", claimNotice7d(repo), sentAtColumn7d()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := fmt.Sprintf("%s_%d", tt.name, time.Now().UnixNano())
			user := createClaimRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, false, nil, nil)

			claimed, err := tt.claim(ctx, user.ID)
			require.NoError(t, err)
			assert.True(t, claimed, "an ordinary never-comped user must be able to claim the %s notice", tt.name)

			var sentAt *time.Time
			require.NoError(t, pool.QueryRow(ctx, "SELECT "+tt.column+" FROM users WHERE id = $1", user.ID).Scan(&sentAt))
			assert.NotNil(t, sentAt, "%s column must be stamped after a successful claim", tt.column)
		})
	}
}

// TestClaimRetentionNotice_Integration_AlreadySent_NotClaimed is the explicit double-send-
// prevention proof M9 requires: a second claim attempt for an already-stamped notice must
// return not-claimed — closing the window where two workers, or a retry, could both send.
func TestClaimRetentionNotice_Integration_AlreadySent_NotClaimed(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	tests := []struct {
		name  string
		claim func(context.Context, string) (bool, error)
	}{
		{"30d", claimNotice30d(repo)},
		{"7d", claimNotice7d(repo)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := fmt.Sprintf("%s_%d", tt.name, time.Now().UnixNano())
			user := createClaimRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, false, nil, nil)

			firstClaim, err := tt.claim(ctx, user.ID)
			require.NoError(t, err)
			require.True(t, firstClaim, "first claim must succeed")

			secondClaim, err := tt.claim(ctx, user.ID)
			require.NoError(t, err)
			assert.False(t, secondClaim, "a second claim attempt for an already-stamped %s notice must return not-claimed", tt.name)
		})
	}
}

// TestClaimRetentionNotice_Integration_ActiveOverride_NotClaimed is the race-closing proof:
// grant a comp on a candidate row (simulating an admin acting between the candidate query and
// the worker's send loop), THEN attempt the claim.
func TestClaimRetentionNotice_Integration_ActiveOverride_NotClaimed(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	tests := []struct {
		name  string
		claim func(context.Context, string) (bool, error)
	}{
		{"30d", claimNotice30d(repo)},
		{"7d", claimNotice7d(repo)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := fmt.Sprintf("%s_%d", tt.name, time.Now().UnixNano())
			user := createClaimRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, false, nil, nil)

			overrideEndsAt := time.Now().Add(24 * time.Hour) // still active
			tier := "premium"
			_, err := pool.Exec(ctx, `UPDATE users SET manual_tier_override = $1, manual_tier_ends_at = $2 WHERE id = $3`,
				tier, overrideEndsAt, user.ID)
			require.NoError(t, err)

			claimed, err := tt.claim(ctx, user.ID)
			require.NoError(t, err)
			assert.False(t, claimed, "a user with a live manual tier override must not receive the %s notice", tt.name)
		})
	}
}

// TestClaimRetentionNotice_Integration_LapsedOverride_NotClaimed is the test that distinguishes
// M9 from H5/H6: unlike CancelIfPastDue/ExpireTrialIfNoOverride/ClaimForDeletion (which use
// noActiveManualOverridePredicate — "still live right now"), the retention-notice claim reuses
// R1's noManualOverrideAtAllPredicate, matching the candidate queries' "any override, live or
// lapsed" rule. A LAPSED override must still block the notice — an over-eager predicate that
// only checked liveness would warn a user whose comp expired moments before the expiry job
// re-arms their clock, using stale retention data.
func TestClaimRetentionNotice_Integration_LapsedOverride_NotClaimed(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	tests := []struct {
		name  string
		claim func(context.Context, string) (bool, error)
	}{
		{"30d", claimNotice30d(repo)},
		{"7d", claimNotice7d(repo)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := fmt.Sprintf("%s_%d", tt.name, time.Now().UnixNano())
			tier := "premium"
			overrideEndsAt := time.Now().Add(-24 * time.Hour) // already lapsed
			user := createClaimRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, false, &tier, &overrideEndsAt)

			claimed, err := tt.claim(ctx, user.ID)
			require.NoError(t, err)
			assert.False(t, claimed, "a user with a LAPSED (uncleared) manual tier override must still be excluded from the %s notice", tt.name)
		})
	}
}

// TestClaimRetentionNotice_Integration_AlreadyDeleted_NotClaimed is a defense-in-depth check:
// an already-deleted user must never be claimed for a notice.
func TestClaimRetentionNotice_Integration_AlreadyDeleted_NotClaimed(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	tests := []struct {
		name  string
		claim func(context.Context, string) (bool, error)
	}{
		{"30d", claimNotice30d(repo)},
		{"7d", claimNotice7d(repo)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := fmt.Sprintf("%s_%d", tt.name, time.Now().UnixNano())
			user := createClaimRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, true, nil, nil)

			claimed, err := tt.claim(ctx, user.ID)
			require.NoError(t, err)
			assert.False(t, claimed)
		})
	}
}
