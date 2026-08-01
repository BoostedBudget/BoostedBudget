//go:build integration

package repository

// Integration tests for IsRetentionNotice30dEligibleForOccurrence / IsRetentionNotice7dEligibleForOccurrence
// (Issue #1413 Medium finding M9, Codex review round 3; occurrence-scoped per round-4 finding M1).
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
// Issue #1442: the non-occurrence predecessors, IsRetentionNotice30dEligible/IsRetentionNotice7dEligible,
// were deleted for having zero production callers (only the *ForOccurrence variants are reachable
// from retention_cleanup_check.go's RetentionProfileRepository consumer interface). Every test
// below is scoped to the *ForOccurrence variants; each fixes the occurrence discriminator
// (retention_expires_at) to the row's own value so the scenario under test — window, sent-state,
// status, override — is isolated from the occurrence-matching axis, which the MatchingOccurrence/
// StaleOccurrence tests below cover on their own.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestRetentionNoticeEligibleForOccurrence_Integration -tags integration -v

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

// windowOffset30d/windowOffset7d return a retention_expires_at comfortably inside each variant's
// own notice window (i.e. what GetUsersNeedingRetention30dNotice/7dNotice would themselves select).
func windowOffset30d() time.Duration { return 25 * 24 * time.Hour }
func windowOffset7d() time.Duration  { return 5 * 24 * time.Hour }

// --- IsRetentionNotice30dEligibleForOccurrence / IsRetentionNotice7dEligibleForOccurrence
// (Issue #1413 round-4 finding M1) ---
//
// The non-occurrence predecessors, IsRetentionNotice30d/7dEligible, used to re-verify
// window/sent-state/status/override but NOT that the row's CURRENT retention_expires_at still
// equals the value the caller claimed. Round-4 review sequence: retention_cleanup_check.go claims
// using the candidate's retention_expires_at (E1), but the row has since been RE-ARMED to a
// different value (E2) — resubscribe (ClearRetentionDates), then cancel again, restarting the
// clock from a later point. With a one-month retention configuration, two workers can hold E1 and
// E2 and BOTH see the current clock as eligible (E2 is ALSO genuinely in-window, unsent, uncomped
// right now) — the user gets a DUPLICATE notice. The *ForOccurrence siblings close this by
// requiring retention_expires_at = expected in the SQL predicate itself.
//
// A real DB is required because pgxmock cannot prove that two DIFFERENT retention_expires_at
// values behave differently against the same live row.

// TestRetentionNoticeEligibleForOccurrence_Integration_MatchingOccurrence_ReturnsTrue proves the
// happy path: the claimed value matches the row's current retention_expires_at exactly.
func TestRetentionNoticeEligibleForOccurrence_Integration_MatchingOccurrence_ReturnsTrue(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	tests := []struct {
		name     string
		eligible func(context.Context, string, time.Time) (bool, error)
		offset   time.Duration
	}{
		{"30d", repo.IsRetentionNotice30dEligibleForOccurrence, windowOffset30d()},
		{"7d", repo.IsRetentionNotice7dEligibleForOccurrence, windowOffset7d()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := fmt.Sprintf("occmatch_%s_%d", tt.name, time.Now().UnixNano())
			expiresAt := time.Now().Add(tt.offset).Truncate(time.Microsecond)
			user := createRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, retentionNoticeFixtureOpts{
				retentionExpiresAt: &expiresAt,
			})

			eligible, err := tt.eligible(ctx, user.ID, expiresAt)
			require.NoError(t, err)
			assert.True(t, eligible, "the claimed occurrence matches the row's current retention_expires_at exactly")
		})
	}
}

// TestRetentionNoticeEligibleForOccurrence_Integration_StaleOccurrence_ReturnsFalse is THE M1
// regression proof: a stale candidate claimed E1, but the row has since been RE-ARMED to a
// DIFFERENT retention_expires_at (E2) — even though E2 is itself currently in-window, unsent, and
// uncomped (i.e. the row genuinely IS eligible right now, just not for E1's claim), the
// occurrence-scoped check must report ineligible for the stale E1 claim.
func TestRetentionNoticeEligibleForOccurrence_Integration_StaleOccurrence_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	tests := []struct {
		name     string
		eligible func(context.Context, string, time.Time) (bool, error)
		offset   time.Duration
	}{
		{"30d", repo.IsRetentionNotice30dEligibleForOccurrence, windowOffset30d()},
		{"7d", repo.IsRetentionNotice7dEligibleForOccurrence, windowOffset7d()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := fmt.Sprintf("occstale_%s_%d", tt.name, time.Now().UnixNano())
			staleExpiresAt := time.Now().Add(tt.offset).Truncate(time.Microsecond) // E1 — what the stale candidate claimed
			currentExpiresAt := staleExpiresAt.Add(2 * time.Hour)                  // E2 — the row's CURRENT (re-armed) value, still in-window

			user := createRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, retentionNoticeFixtureOpts{
				retentionExpiresAt: &currentExpiresAt,
			})

			eligible, err := tt.eligible(ctx, user.ID, staleExpiresAt)
			require.NoError(t, err)
			assert.False(t, eligible, "a stale claimed occurrence must not be reported eligible once the row has been re-armed to a different retention_expires_at")

			// Sanity: the CURRENT occurrence (E2) genuinely IS eligible right now — proving the
			// false result above isn't accidentally caused by some OTHER predicate
			// (window/sent/status/override).
			currentEligible, err := tt.eligible(ctx, user.ID, currentExpiresAt)
			require.NoError(t, err)
			assert.True(t, currentEligible, "test setup sanity: the row's current (re-armed) occurrence must itself be genuinely eligible")
		})
	}
}

// TestRetentionNoticeEligibleForOccurrence_Integration_ActiveOverride_ReturnsFalse closes a gap
// left by the *ForOccurrence tests above (Issue #1413/#1443 gap analysis, PR #1443 AC9 pattern-2
// finding): the non-occurrence predecessors' own ActiveOverride proof exercised that override
// predicate for the non-occurrence-scoped methods, but the *ForOccurrence siblings splice their
// OWN copy of noManualOverrideAtAllPredicate at profile_repository.go:1848/:1872 — nothing before
// this test proved THAT copy actually excludes a comped user. Every *ForOccurrence test up to this
// point only varies the occurrence (retention_expires_at) axis, on an always-uncomped fixture.
// Uses a MATCHING occurrence (so the occurrence discriminator itself passes) plus a live override,
// isolating the override predicate as the only thing that can produce `false`.
func TestRetentionNoticeEligibleForOccurrence_Integration_ActiveOverride_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	tests := []struct {
		name     string
		eligible func(context.Context, string, time.Time) (bool, error)
		offset   time.Duration
	}{
		{"30d", repo.IsRetentionNotice30dEligibleForOccurrence, windowOffset30d()},
		{"7d", repo.IsRetentionNotice7dEligibleForOccurrence, windowOffset7d()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := fmt.Sprintf("occcomp_%s_%d", tt.name, time.Now().UnixNano())
			expiresAt := time.Now().Add(tt.offset).Truncate(time.Microsecond)
			overrideEndsAt := time.Now().Add(24 * time.Hour) // still active
			tier := "premium"
			user := createRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, retentionNoticeFixtureOpts{
				retentionExpiresAt: &expiresAt,
				manualTierOverride: &tier,
				manualTierEndsAt:   &overrideEndsAt,
			})

			eligible, err := tt.eligible(ctx, user.ID, expiresAt)
			require.NoError(t, err)
			assert.False(t, eligible, "a user with a live manual tier override, even with the exact matching occurrence, must not be %s-notice eligible", tt.name)
		})
	}
}

// --- Issue #1442 ports: scenarios the deleted non-occurrence predecessors used to pin, with no
// occurrence-side equivalent before this file. Each fixes the occurrence discriminator to the
// row's own retention_expires_at so the scenario under test is isolated from the occurrence axis.

// TestRetentionNoticeEligibleForOccurrence_Integration_NoWrite is the M9 regression proof, ported
// for the *ForOccurrence variants (Issue #1442): unlike the old ClaimRetentionNotice30dSent/7dSent,
// eligibility re-verification must be a pure read. Calling it must never stamp
// retention_notice_30d_sent_at/7d_sent_at itself — only SetRetentionNotice30dSent/7dSent (called by
// the worker AFTER a successful send) may do that. The deleted non-occurrence predecessors' own
// NoWrite test proved this for IsRetentionNotice30d/7dEligible; nothing proved it for the
// *ForOccurrence copies, which are a textually distinct SQL string.
func TestRetentionNoticeEligibleForOccurrence_Integration_NoWrite(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	tests := []struct {
		name     string
		eligible func(context.Context, string, time.Time) (bool, error)
		offset   time.Duration
		column   string
	}{
		{"30d", repo.IsRetentionNotice30dEligibleForOccurrence, windowOffset30d(), "retention_notice_30d_sent_at"},
		{"7d", repo.IsRetentionNotice7dEligibleForOccurrence, windowOffset7d(), "retention_notice_7d_sent_at"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := fmt.Sprintf("occnowrite_%s_%d", tt.name, time.Now().UnixNano())
			expiresAt := time.Now().Add(tt.offset).Truncate(time.Microsecond)
			user := createRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, retentionNoticeFixtureOpts{
				retentionExpiresAt: &expiresAt,
			})

			// Call it twice — if it were still a claim-and-stamp UPDATE (the M9 bug), the second
			// call would see the marker it just set and report ineligible.
			first, err := tt.eligible(ctx, user.ID, expiresAt)
			require.NoError(t, err)
			require.True(t, first)

			second, err := tt.eligible(ctx, user.ID, expiresAt)
			require.NoError(t, err)
			assert.True(t, second, "%s occurrence eligibility must be a pure read — a second call must see the SAME state, not a marker the first call stamped", tt.name)

			var sentAt *time.Time
			require.NoError(t, pool.QueryRow(ctx, "SELECT "+tt.column+" FROM users WHERE id = $1", user.ID).Scan(&sentAt))
			assert.Nil(t, sentAt, "%s must never stamp %s itself", tt.name, tt.column)
		})
	}
}

// TestRetentionNoticeEligibleForOccurrence_Integration_AlreadySent_ReturnsFalse ports the deleted
// non-occurrence predecessors' AlreadySent proof (Issue #1442): retention_notice_*_sent_at IS NULL
// is spliced identically into both queries, but nothing proved the *ForOccurrence copy enforces it
// — once SetRetentionNotice30dSent/7dSent has run (simulating a prior successful send), a later
// eligibility check for the SAME matching occurrence must still report ineligible.
func TestRetentionNoticeEligibleForOccurrence_Integration_AlreadySent_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	now := time.Now()
	tests := []struct {
		name     string
		eligible func(context.Context, string, time.Time) (bool, error)
		offset   time.Duration
	}{
		{"30d", repo.IsRetentionNotice30dEligibleForOccurrence, windowOffset30d()},
		{"7d", repo.IsRetentionNotice7dEligibleForOccurrence, windowOffset7d()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := fmt.Sprintf("occsent_%s_%d", tt.name, time.Now().UnixNano())
			expiresAt := time.Now().Add(tt.offset).Truncate(time.Microsecond)
			opts := retentionNoticeFixtureOpts{retentionExpiresAt: &expiresAt}
			if tt.name == "30d" {
				opts.notice30dSentAt = &now
			} else {
				opts.notice7dSentAt = &now
			}
			user := createRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, opts)

			eligible, err := tt.eligible(ctx, user.ID, expiresAt)
			require.NoError(t, err)
			assert.False(t, eligible, "an already-sent %s notice must not be re-sent, even for the exact matching occurrence", tt.name)
		})
	}
}

// TestRetentionNoticeEligibleForOccurrence_Integration_LapsedOverride_ReturnsFalse ports the
// deleted non-occurrence predecessors' LapsedOverride proof (Issue #1442): both variants splice
// noManualOverrideAtAllPredicate, but nothing proved the *ForOccurrence copy blocks a LAPSED
// (uncleared) override the same way ActiveOverride above already proved for a LIVE one. Distinct
// from H5/H6's noActiveManualOverridePredicate ("still live right now") — the retention-notice
// re-check must still exclude a lapsed-but-uncleared override.
func TestRetentionNoticeEligibleForOccurrence_Integration_LapsedOverride_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	tests := []struct {
		name     string
		eligible func(context.Context, string, time.Time) (bool, error)
		offset   time.Duration
	}{
		{"30d", repo.IsRetentionNotice30dEligibleForOccurrence, windowOffset30d()},
		{"7d", repo.IsRetentionNotice7dEligibleForOccurrence, windowOffset7d()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := fmt.Sprintf("occlapsed_%s_%d", tt.name, time.Now().UnixNano())
			expiresAt := time.Now().Add(tt.offset).Truncate(time.Microsecond)
			tier := "premium"
			overrideEndsAt := time.Now().Add(-24 * time.Hour) // already lapsed
			user := createRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, retentionNoticeFixtureOpts{
				retentionExpiresAt: &expiresAt,
				manualTierOverride: &tier,
				manualTierEndsAt:   &overrideEndsAt,
			})

			eligible, err := tt.eligible(ctx, user.ID, expiresAt)
			require.NoError(t, err)
			assert.False(t, eligible, "a user with a LAPSED (uncleared) manual tier override must still be excluded from the %s notice, even for the exact matching occurrence", tt.name)
		})
	}
}

// TestRetentionNoticeEligibleForOccurrence_Integration_AlreadyDeleted_ReturnsFalse ports the
// deleted non-occurrence predecessors' AlreadyDeleted proof (Issue #1442): a defense-in-depth
// check that an already-deleted user must never be re-verified as eligible, even for the exact
// matching occurrence.
func TestRetentionNoticeEligibleForOccurrence_Integration_AlreadyDeleted_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	tests := []struct {
		name     string
		eligible func(context.Context, string, time.Time) (bool, error)
		offset   time.Duration
	}{
		{"30d", repo.IsRetentionNotice30dEligibleForOccurrence, windowOffset30d()},
		{"7d", repo.IsRetentionNotice7dEligibleForOccurrence, windowOffset7d()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := fmt.Sprintf("occdeleted_%s_%d", tt.name, time.Now().UnixNano())
			expiresAt := time.Now().Add(tt.offset).Truncate(time.Microsecond)
			user := createRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, retentionNoticeFixtureOpts{
				isDeleted:          true,
				retentionExpiresAt: &expiresAt,
			})

			eligible, err := tt.eligible(ctx, user.ID, expiresAt)
			require.NoError(t, err)
			assert.False(t, eligible, "an already-deleted user must never be %s-notice eligible, even for the exact matching occurrence", tt.name)
		})
	}
}

// TestRetentionNoticeEligibleForOccurrence_Integration_DeletionRequested_ReturnsFalse ports the
// deleted non-occurrence predecessors' DeletionRequested proof (Issue #1442): a user with an
// outstanding GDPR deletion request of their own must not receive a "resubscribe or lose your
// data" retention notice — mirrors GetUsersNeedingRetention30dNotice/7dNotice's
// "deletion_requested_at IS NULL" clause.
func TestRetentionNoticeEligibleForOccurrence_Integration_DeletionRequested_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	tests := []struct {
		name     string
		eligible func(context.Context, string, time.Time) (bool, error)
		offset   time.Duration
	}{
		{"30d", repo.IsRetentionNotice30dEligibleForOccurrence, windowOffset30d()},
		{"7d", repo.IsRetentionNotice7dEligibleForOccurrence, windowOffset7d()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := fmt.Sprintf("occdelreq_%s_%d", tt.name, time.Now().UnixNano())
			expiresAt := time.Now().Add(tt.offset).Truncate(time.Microsecond)
			requestedAt := time.Now()
			user := createRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, retentionNoticeFixtureOpts{
				retentionExpiresAt:  &expiresAt,
				deletionRequestedAt: &requestedAt,
			})

			eligible, err := tt.eligible(ctx, user.ID, expiresAt)
			require.NoError(t, err)
			assert.False(t, eligible, "a user with an outstanding GDPR deletion request must not receive the %s notice, even for the exact matching occurrence", tt.name)
		})
	}
}

// TestRetentionNoticeEligibleForOccurrence_Integration_WrongSubscriptionStatus_ReturnsFalse ports
// the deleted non-occurrence predecessors' WrongSubscriptionStatus proof (Issue #1442): only
// inactive/cancelled subscriptions are eligible — an active/trial/past_due user should never be
// warned about retention deletion, since they aren't on that clock.
func TestRetentionNoticeEligibleForOccurrence_Integration_WrongSubscriptionStatus_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	tests := []struct {
		name     string
		eligible func(context.Context, string, time.Time) (bool, error)
		offset   time.Duration
	}{
		{"30d", repo.IsRetentionNotice30dEligibleForOccurrence, windowOffset30d()},
		{"7d", repo.IsRetentionNotice7dEligibleForOccurrence, windowOffset7d()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := fmt.Sprintf("occstatus_%s_%d", tt.name, time.Now().UnixNano())
			expiresAt := time.Now().Add(tt.offset).Truncate(time.Microsecond)
			user := createRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, retentionNoticeFixtureOpts{
				subscriptionStatus: "active",
				retentionExpiresAt: &expiresAt,
			})

			eligible, err := tt.eligible(ctx, user.ID, expiresAt)
			require.NoError(t, err)
			assert.False(t, eligible, "an active-subscription user must not be %s-notice eligible, even for the exact matching occurrence", tt.name)
		})
	}
}

// TestRetentionNoticeEligibleForOccurrence_Integration_AlreadyExpired_ReturnsFalse ports the
// deleted non-occurrence predecessors' AlreadyExpired proof (Issue #1442): once
// retention_expires_at has already passed, the user belongs to GetUsersWithExpiredRetention's
// hard-delete path, not the notice path — the notice window requires retention_expires_at > NOW().
func TestRetentionNoticeEligibleForOccurrence_Integration_AlreadyExpired_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	tests := []struct {
		name     string
		eligible func(context.Context, string, time.Time) (bool, error)
	}{
		{"30d", repo.IsRetentionNotice30dEligibleForOccurrence},
		{"7d", repo.IsRetentionNotice7dEligibleForOccurrence},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := fmt.Sprintf("occexpired_%s_%d", tt.name, time.Now().UnixNano())
			expiresAt := time.Now().Add(-24 * time.Hour).Truncate(time.Microsecond) // already expired
			user := createRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, retentionNoticeFixtureOpts{
				retentionExpiresAt: &expiresAt,
			})

			eligible, err := tt.eligible(ctx, user.ID, expiresAt)
			require.NoError(t, err)
			assert.False(t, eligible, "a user whose retention has ALREADY expired must not be %s-notice eligible, even for the exact matching occurrence", tt.name)
		})
	}
}

// TestRetentionNoticeEligibleForOccurrence_Integration_OutsideOwnWindow_ReturnsFalse ports the
// deleted non-occurrence predecessors' OutsideOwnWindow proof (Issue #1442): retention_expires_at
// 10 days out is inside the 30-day window but OUTSIDE the 7-day window, for the exact matching
// occurrence in both cases — the test that would catch a shared-predicate bug crossing the two
// variants (e.g. IsRetentionNotice7dEligibleForOccurrence accidentally reusing the 30-day window).
func TestRetentionNoticeEligibleForOccurrence_Integration_OutsideOwnWindow_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("occwindowcross_%d", time.Now().UnixNano())
	expiresAt := time.Now().Add(10 * 24 * time.Hour).Truncate(time.Microsecond) // inside 30d window, outside 7d window
	user := createRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, retentionNoticeFixtureOpts{
		retentionExpiresAt: &expiresAt,
	})

	eligible30, err := repo.IsRetentionNotice30dEligibleForOccurrence(ctx, user.ID, expiresAt)
	require.NoError(t, err)
	assert.True(t, eligible30, "10 days out is inside the 30-day window and must be 30d-eligible for the matching occurrence")

	eligible7, err := repo.IsRetentionNotice7dEligibleForOccurrence(ctx, user.ID, expiresAt)
	require.NoError(t, err)
	assert.False(t, eligible7, "10 days out is OUTSIDE the 7-day window and must not be 7d-eligible even for the matching occurrence")
}

// TestRetentionNoticeEligibleForOccurrence_Integration_FarOutsideWindow_ReturnsFalse ports the
// deleted non-occurrence predecessors' FarOutsideWindow proof (Issue #1442): retention_expires_at
// 45 days out is outside BOTH windows, for the exact matching occurrence.
func TestRetentionNoticeEligibleForOccurrence_Integration_FarOutsideWindow_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("occfarfuture_%d", time.Now().UnixNano())
	expiresAt := time.Now().Add(45 * 24 * time.Hour).Truncate(time.Microsecond)
	user := createRetentionNoticeFixtureUser(t, ctx, pool, repo, suffix, retentionNoticeFixtureOpts{
		retentionExpiresAt: &expiresAt,
	})

	eligible30, err := repo.IsRetentionNotice30dEligibleForOccurrence(ctx, user.ID, expiresAt)
	require.NoError(t, err)
	assert.False(t, eligible30, "45 days out is outside the 30-day window, even for the matching occurrence")

	eligible7, err := repo.IsRetentionNotice7dEligibleForOccurrence(ctx, user.ID, expiresAt)
	require.NoError(t, err)
	assert.False(t, eligible7, "45 days out is outside the 7-day window, even for the matching occurrence")
}
