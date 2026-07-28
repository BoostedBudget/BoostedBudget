//go:build integration

package repository

// Integration tests for IsRetentionNotice30dEligible / IsRetentionNotice7dEligible (Issue #1413
// Medium finding M9, Codex review round 3).
//
// These replace ClaimRetentionNotice30dSent / ClaimRetentionNotice7dSent (round 2), which
// conflated two different concerns into one atomic UPDATE: EXCLUSIVITY (stamping
// retention_notice_*_sent_at as a claim marker) and CORRECTNESS (re-verifying the user is still
// eligible). Because the marker was stamped BEFORE the email send was attempted, a failed send
// left the marker set — the candidate query then silently excluded the user forever, and they
// were never warned before their data was deleted. The fix separates the two: work_claims now
// owns exclusivity (Claim/Complete/Release), and these methods are a pure, side-effect-free
// re-check of eligibility. The caller sends the email FIRST, and stamps the *_sent_at marker
// (via SetRetentionNotice30dSent/7dSent) only after a successful send.
//
// Field-for-field, these mirror GetUsersNeedingRetention30dNotice / GetUsersNeedingRetention7dNotice's
// WHERE clause (window, sent-state, subscription status, deletion request, override), scoped to a
// single user by ID — so the candidate query and this per-user re-check can never silently drift
// apart. pgxmock cannot validate this: it only matches query text, not the live row state these
// predicates depend on, so a real database is required.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestRetentionNoticeEligible -tags integration -v

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

// retentionNoticeFixtureOpts configures createRetentionNoticeFixtureUser's row state. Zero values
// produce a genuinely eligible user for BOTH the 30d and 7d windows (retentionExpiresAt must
// still be set explicitly per-test — there is no sane default for it).
type retentionNoticeFixtureOpts struct {
	isDeleted           bool
	subscriptionStatus  string // defaults to "cancelled" when empty
	retentionExpiresAt  *time.Time
	deletionRequestedAt *time.Time
	manualTierOverride  *string
	manualTierEndsAt    *time.Time
	notice30dSentAt     *time.Time
	notice7dSentAt      *time.Time
}

// createRetentionNoticeFixtureUser inserts a plain user row via the real CreateUserTx path, then
// overwrites the retention/override/notice columns directly so each test can construct exactly
// the row state it needs.
func createRetentionNoticeFixtureUser(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repo ProfileRepository,
	suffix string,
	opts retentionNoticeFixtureOpts,
) *domain.User {
	t.Helper()

	status := opts.subscriptionStatus
	if status == "" {
		status = "cancelled"
	}

	user := &domain.User{
		ClerkUserID:        "clerk_retnotice_" + suffix,
		Email:              "retnotice-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: status,
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
		    manual_tier_ends_at = $3,
		    retention_expires_at = $4,
		    deletion_requested_at = $5,
		    retention_notice_30d_sent_at = $6,
		    retention_notice_7d_sent_at = $7
		WHERE id = $8`,
		opts.isDeleted, opts.manualTierOverride, opts.manualTierEndsAt,
		opts.retentionExpiresAt, opts.deletionRequestedAt,
		opts.notice30dSentAt, opts.notice7dSentAt,
		user.ID,
	)
	require.NoError(t, err)

	user.IsDeleted = opts.isDeleted
	user.ManualTierOverride = opts.manualTierOverride
	user.ManualTierEndsAt = opts.manualTierEndsAt
	user.SubscriptionStatus = status
	return user
}

// eligible30d/eligible7d let the table-driven tests below exercise both production methods
// directly rather than a hand-copied restatement.
func eligible30d(repo ProfileRepository) func(context.Context, string) (bool, error) {
	return repo.IsRetentionNotice30dEligible
}
func eligible7d(repo ProfileRepository) func(context.Context, string) (bool, error) {
	return repo.IsRetentionNotice7dEligible
}

// windowOffset30d/windowOffset7d return a retention_expires_at comfortably inside each variant's
// own notice window (i.e. what GetUsersNeedingRetention30dNotice/7dNotice would themselves select).
func windowOffset30d() time.Duration { return 25 * 24 * time.Hour }
func windowOffset7d() time.Duration  { return 5 * 24 * time.Hour }

// TestRetentionNoticeEligible_Integration_Eligible_ReturnsTrue proves the happy path for both
// notice windows: a genuinely eligible user (in-window, unsent, no override, correct status, no
// deletion request) is reported eligible — the positive-direction proof required alongside every
// exclusion test below. Without this, an over-tight predicate that silently excluded everyone
// would pass every "nothing bad happened" test.
func TestRetentionNoticeEligible_Integration_Eligible_ReturnsTrue(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	tests := []struct {
		name     string
		eligible func(context.Context, string) (bool, error)
		offset   time.Duration
	}{
		{"30d", eligible30d(repo), windowOffset30d()},
		{"7d", eligible7d(repo), windowOffset7d()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := fmt.Sprintf("%s_%d", tt.name, time.Now().UnixNano())
			expiresAt := time.Now().Add(tt.offset)
			user := createRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, retentionNoticeFixtureOpts{
				retentionExpiresAt: &expiresAt,
			})

			eligible, err := tt.eligible(ctx, user.ID)
			require.NoError(t, err)
			assert.True(t, eligible, "an ordinary in-window, never-comped, unsent user must be eligible for the %s notice", tt.name)
		})
	}
}

// TestRetentionNoticeEligible_Integration_NoWrite is the M9 regression proof at the repository
// layer: unlike the old ClaimRetentionNotice30dSent/7dSent, eligibility re-verification must be a
// pure read. Calling it must never stamp retention_notice_30d_sent_at/7d_sent_at itself — only
// SetRetentionNotice30dSent/7dSent (called by the worker AFTER a successful send) may do that.
func TestRetentionNoticeEligible_Integration_NoWrite(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	tests := []struct {
		name     string
		eligible func(context.Context, string) (bool, error)
		offset   time.Duration
		column   string
	}{
		{"30d", eligible30d(repo), windowOffset30d(), "retention_notice_30d_sent_at"},
		{"7d", eligible7d(repo), windowOffset7d(), "retention_notice_7d_sent_at"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := fmt.Sprintf("%s_%d", tt.name, time.Now().UnixNano())
			expiresAt := time.Now().Add(tt.offset)
			user := createRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, retentionNoticeFixtureOpts{
				retentionExpiresAt: &expiresAt,
			})

			// Call it twice — if it were still a claim-and-stamp UPDATE (the M9 bug), the second
			// call would see the marker it just set and report ineligible.
			first, err := tt.eligible(ctx, user.ID)
			require.NoError(t, err)
			require.True(t, first)

			second, err := tt.eligible(ctx, user.ID)
			require.NoError(t, err)
			assert.True(t, second, "%s eligibility must be a pure read — a second call must see the SAME state, not a marker the first call stamped", tt.name)

			var sentAt *time.Time
			require.NoError(t, pool.QueryRow(ctx, "SELECT "+tt.column+" FROM users WHERE id = $1", user.ID).Scan(&sentAt))
			assert.Nil(t, sentAt, "%s must never stamp %s itself", tt.name, tt.column)
		})
	}
}

// TestRetentionNoticeEligible_Integration_AlreadySent_ReturnsFalse proves double-send prevention
// still holds even though the marker is no longer stamped by the eligibility check itself: once
// SetRetentionNotice30dSent/7dSent has run (simulating a prior successful send), a later
// eligibility check for the same window must report ineligible.
func TestRetentionNoticeEligible_Integration_AlreadySent_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	now := time.Now()
	tests := []struct {
		name     string
		eligible func(context.Context, string) (bool, error)
		offset   time.Duration
		sentAt   *time.Time
	}{
		{"30d", eligible30d(repo), windowOffset30d(), &now},
		{"7d", eligible7d(repo), windowOffset7d(), &now},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := fmt.Sprintf("%s_%d", tt.name, time.Now().UnixNano())
			expiresAt := time.Now().Add(tt.offset)
			opts := retentionNoticeFixtureOpts{retentionExpiresAt: &expiresAt}
			if tt.name == "30d" {
				opts.notice30dSentAt = tt.sentAt
			} else {
				opts.notice7dSentAt = tt.sentAt
			}
			user := createRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, opts)

			eligible, err := tt.eligible(ctx, user.ID)
			require.NoError(t, err)
			assert.False(t, eligible, "an already-sent %s notice must not be re-sent", tt.name)
		})
	}
}

// TestRetentionNoticeEligible_Integration_ActiveOverride_ReturnsFalse is the race-closing proof:
// grant a comp on a candidate row (simulating an admin acting between the candidate query and the
// worker's send loop), THEN re-verify eligibility.
func TestRetentionNoticeEligible_Integration_ActiveOverride_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	tests := []struct {
		name     string
		eligible func(context.Context, string) (bool, error)
		offset   time.Duration
	}{
		{"30d", eligible30d(repo), windowOffset30d()},
		{"7d", eligible7d(repo), windowOffset7d()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := fmt.Sprintf("%s_%d", tt.name, time.Now().UnixNano())
			expiresAt := time.Now().Add(tt.offset)
			overrideEndsAt := time.Now().Add(24 * time.Hour) // still active
			tier := "premium"
			user := createRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, retentionNoticeFixtureOpts{
				retentionExpiresAt: &expiresAt,
				manualTierOverride: &tier,
				manualTierEndsAt:   &overrideEndsAt,
			})

			eligible, err := tt.eligible(ctx, user.ID)
			require.NoError(t, err)
			assert.False(t, eligible, "a user with a live manual tier override must not receive the %s notice", tt.name)
		})
	}
}

// TestRetentionNoticeEligible_Integration_LapsedOverride_ReturnsFalse distinguishes M9 from
// H5/H6: unlike CancelIfPastDueWithRetention/ExpireTrialIfNoOverride/ClaimForDeletion (which use
// noActiveManualOverridePredicate — "still live right now"), the retention-notice re-check reuses
// noManualOverrideAtAllPredicate, matching the candidate queries' "any override, live or lapsed"
// rule. A LAPSED override must still block the notice.
func TestRetentionNoticeEligible_Integration_LapsedOverride_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	tests := []struct {
		name     string
		eligible func(context.Context, string) (bool, error)
		offset   time.Duration
	}{
		{"30d", eligible30d(repo), windowOffset30d()},
		{"7d", eligible7d(repo), windowOffset7d()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := fmt.Sprintf("%s_%d", tt.name, time.Now().UnixNano())
			expiresAt := time.Now().Add(tt.offset)
			tier := "premium"
			overrideEndsAt := time.Now().Add(-24 * time.Hour) // already lapsed
			user := createRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, retentionNoticeFixtureOpts{
				retentionExpiresAt: &expiresAt,
				manualTierOverride: &tier,
				manualTierEndsAt:   &overrideEndsAt,
			})

			eligible, err := tt.eligible(ctx, user.ID)
			require.NoError(t, err)
			assert.False(t, eligible, "a user with a LAPSED (uncleared) manual tier override must still be excluded from the %s notice", tt.name)
		})
	}
}

// TestRetentionNoticeEligible_Integration_AlreadyDeleted_ReturnsFalse is a defense-in-depth
// check: an already-deleted user must never be re-verified as eligible.
func TestRetentionNoticeEligible_Integration_AlreadyDeleted_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	tests := []struct {
		name     string
		eligible func(context.Context, string) (bool, error)
		offset   time.Duration
	}{
		{"30d", eligible30d(repo), windowOffset30d()},
		{"7d", eligible7d(repo), windowOffset7d()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := fmt.Sprintf("%s_%d", tt.name, time.Now().UnixNano())
			expiresAt := time.Now().Add(tt.offset)
			user := createRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, retentionNoticeFixtureOpts{
				isDeleted:          true,
				retentionExpiresAt: &expiresAt,
			})

			eligible, err := tt.eligible(ctx, user.ID)
			require.NoError(t, err)
			assert.False(t, eligible, "an already-deleted user must never be %s-notice eligible", tt.name)
		})
	}
}

// TestRetentionNoticeEligible_Integration_DeletionRequested_ReturnsFalse: a user with an
// outstanding GDPR deletion request of their own must not receive a "resubscribe or lose your
// data" retention notice — mirrors GetUsersNeedingRetention30dNotice/7dNotice's
// "deletion_requested_at IS NULL" clause.
func TestRetentionNoticeEligible_Integration_DeletionRequested_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	tests := []struct {
		name     string
		eligible func(context.Context, string) (bool, error)
		offset   time.Duration
	}{
		{"30d", eligible30d(repo), windowOffset30d()},
		{"7d", eligible7d(repo), windowOffset7d()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := fmt.Sprintf("%s_%d", tt.name, time.Now().UnixNano())
			expiresAt := time.Now().Add(tt.offset)
			requestedAt := time.Now()
			user := createRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, retentionNoticeFixtureOpts{
				retentionExpiresAt:  &expiresAt,
				deletionRequestedAt: &requestedAt,
			})

			eligible, err := tt.eligible(ctx, user.ID)
			require.NoError(t, err)
			assert.False(t, eligible, "a user with an outstanding GDPR deletion request must not receive the %s notice", tt.name)
		})
	}
}

// TestRetentionNoticeEligible_Integration_WrongSubscriptionStatus_ReturnsFalse: only
// inactive/cancelled subscriptions are eligible — an active/trial/past_due user should never be
// warned about retention deletion, since they aren't on that clock.
func TestRetentionNoticeEligible_Integration_WrongSubscriptionStatus_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	tests := []struct {
		name     string
		eligible func(context.Context, string) (bool, error)
		offset   time.Duration
	}{
		{"30d", eligible30d(repo), windowOffset30d()},
		{"7d", eligible7d(repo), windowOffset7d()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := fmt.Sprintf("%s_%d", tt.name, time.Now().UnixNano())
			expiresAt := time.Now().Add(tt.offset)
			user := createRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, retentionNoticeFixtureOpts{
				subscriptionStatus: "active",
				retentionExpiresAt: &expiresAt,
			})

			eligible, err := tt.eligible(ctx, user.ID)
			require.NoError(t, err)
			assert.False(t, eligible, "an active-subscription user must not be %s-notice eligible", tt.name)
		})
	}
}

// TestRetentionNoticeEligible_Integration_AlreadyExpired_ReturnsFalse: once retention_expires_at
// has already passed, the user belongs to GetUsersWithExpiredRetention's hard-delete path, not the
// notice path — the notice window requires retention_expires_at > NOW().
func TestRetentionNoticeEligible_Integration_AlreadyExpired_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	tests := []struct {
		name     string
		eligible func(context.Context, string) (bool, error)
	}{
		{"30d", eligible30d(repo)},
		{"7d", eligible7d(repo)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := fmt.Sprintf("%s_%d", tt.name, time.Now().UnixNano())
			expiresAt := time.Now().Add(-24 * time.Hour) // already expired
			user := createRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, retentionNoticeFixtureOpts{
				retentionExpiresAt: &expiresAt,
			})

			eligible, err := tt.eligible(ctx, user.ID)
			require.NoError(t, err)
			assert.False(t, eligible, "a user whose retention has ALREADY expired must not be %s-notice eligible", tt.name)
		})
	}
}

// TestRetentionNoticeEligible_Integration_OutsideOwnWindow_ReturnsFalse is the test that would
// catch a shared-predicate bug crossing the two variants (e.g. IsRetentionNotice7dEligible
// accidentally reusing the 30-day window): retention_expires_at 10 days out is within the 30-day
// window but OUTSIDE the 7-day window, so 30d must report eligible and 7d must not. Per the
// brief's falsifiability requirement, a bug in one variant's window arithmetic must fail only
// that variant's test.
func TestRetentionNoticeEligible_Integration_OutsideOwnWindow_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("windowcross_%d", time.Now().UnixNano())
	expiresAt := time.Now().Add(10 * 24 * time.Hour) // inside 30d window, outside 7d window
	user := createRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, retentionNoticeFixtureOpts{
		retentionExpiresAt: &expiresAt,
	})

	eligible30, err := repo.IsRetentionNotice30dEligible(ctx, user.ID)
	require.NoError(t, err)
	assert.True(t, eligible30, "10 days out is inside the 30-day window and must be 30d-eligible")

	eligible7, err := repo.IsRetentionNotice7dEligible(ctx, user.ID)
	require.NoError(t, err)
	assert.False(t, eligible7, "10 days out is OUTSIDE the 7-day window and must not be 7d-eligible")
}

// TestRetentionNoticeEligible_Integration_FarOutsideWindow_ReturnsFalse: retention_expires_at 45
// days out is outside BOTH windows.
func TestRetentionNoticeEligible_Integration_FarOutsideWindow_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("farfuture_%d", time.Now().UnixNano())
	expiresAt := time.Now().Add(45 * 24 * time.Hour)
	user := createRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, retentionNoticeFixtureOpts{
		retentionExpiresAt: &expiresAt,
	})

	eligible30, err := repo.IsRetentionNotice30dEligible(ctx, user.ID)
	require.NoError(t, err)
	assert.False(t, eligible30, "45 days out is outside the 30-day window")

	eligible7, err := repo.IsRetentionNotice7dEligible(ctx, user.ID)
	require.NoError(t, err)
	assert.False(t, eligible7, "45 days out is outside the 7-day window")
}
