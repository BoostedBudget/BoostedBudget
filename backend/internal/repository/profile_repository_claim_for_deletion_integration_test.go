//go:build integration

package repository

// Integration tests for ClaimForDeletion (Issue #1413 Critical finding C2, Codex review round 2).
//
// ExecuteDeletion previously read a user, checked HasActiveManualOverride() &&
// DeletionRequestedAt == nil against that snapshot, then performed irreversible external
// cleanup (Paddle cancel, Plaid disconnect) and a cascade delete — nothing tied the final
// deletion to the state that was checked. ClaimForDeletion closes that race: the predicate that
// authorises the destructive work now lives inside a conditional UPDATE, evaluated against
// committed state at the moment of the claim, not an earlier, non-atomic read. pgxmock cannot
// validate this — it only matches query text, not the live row state the predicate depends on
// or RowsAffected semantics — so a real database is required.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestClaimForDeletion_Integration -tags integration -v

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

// claimFixtureRetentionMonths matches RetentionCleanupCheckWorker's default retention window
// (intOrDefault(deps.RetentionMonths, 6)) so the fixture row looks like a production one. Any
// positive value works — the fixture shifts the resulting stamps rather than depending on the size.
const claimFixtureRetentionMonths = 6

// createClaimForDeletionFixtureUser inserts a plain user row, then sets is_deleted,
// deletion_requested_at, manual_tier_override, and manual_tier_ends_at directly via UPDATE (the
// same pattern used throughout this package's manual-override fixtures), and registers cleanup.
//
// deletion_scheduled_at is set to the SAME value as deletionRequestedAt rather than being a
// separate parameter (Issue #1440, T0). Two reasons: every production writer of either column
// (CreateUser, CreateUserTx, MarkForDeletion, CancelDeletion — see the four-writer enumeration
// above deletionAuthorisationUnionPredicate in profile_repository.go) sets both columns together in
// a single statement, so a fixture with one set and the other NULL is a state the system cannot
// actually reach; and every caller here passes a PAST
// deletionRequestedAt, so the grace period reads as elapsed — which is the state in which a
// deletion job legitimately gets enqueued. A test that needs a schedule still in the FUTURE
// (cancel-then-re-request) overrides the column explicitly after calling this.
//
// Cross-package coupling: setting deletion_scheduled_at here makes every fixture user with a
// non-nil deletionRequestedAt also match GetUsersScheduledForDeletionPage's WHERE clause (past
// deletion_scheduled_at, is_deleted = FALSE). profile_repository_scheduled_deletion_page_integration_test.go
// pages through ALL matching rows with a small limit and a hard cap on page count; a leaked row
// from a failed cleanup in THIS file's tests (or asof_threading_integration_test.go, which shares
// this helper) is picked up by that page walk and can push it over its cap — a false failure
// that reads as a cursor bug rather than a fixture-cleanup leak. Keep t.Cleanup wired on every
// call site.
func createClaimForDeletionFixtureUser(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repo ProfileRepository,
	suffix string,
	isDeleted bool,
	deletionRequestedAt *time.Time,
	manualTierOverride *string,
	manualTierEndsAt *time.Time,
) *domain.User {
	t.Helper()

	user := &domain.User{
		ClerkUserID:        "clerk_claimdel_" + suffix,
		Email:              "claimdel-" + suffix + "@example.com",
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

	// An ALREADY-ELAPSED retention clock, on every fixture. #1440 T0-retention: the retention
	// claim now re-verifies `retention_expires_at IS NOT NULL AND retention_expires_at <= NOW()` and
	// `subscription_status IN ('inactive','cancelled')` (expiredRetentionClockPredicate), so a
	// DeletionKindRetention claim is only granted for a user GetUsersWithExpiredRetentionPage would
	// still select — which is what every retention-kind test here means by "an otherwise eligible
	// user". Armed through the production writer and then shifted backwards, i.e. simulating elapsed
	// time rather than hand-writing the target row: no production writer of retention_expires_at ever
	// computes a past instant (all use clock_timestamp() + make_interval), production only gets there
	// by waiting. Harmless for the user-requested fixtures, whose branch ignores these columns.
	//
	// Runs BEFORE the is_deleted write below, deliberately: SetRetentionDates (like every production
	// writer here) is scoped `WHERE is_deleted = FALSE`, so arming it after an isDeleted=true fixture
	// would fail with ErrUserNotFound.
	// RetentionClockUnguarded: this fixture arms the clock as SETUP for the deletion-claim tests
	// below, not as a subscription transition, so there is no authorising status to name (Issue
	// #1500 residual — see RetentionClockGuard's doc comment).
	require.NoError(t, repo.SetRetentionDates(ctx, user.ID, RetentionClockUnguarded, claimFixtureRetentionMonths))
	_, err = pool.Exec(ctx, `
		UPDATE users
		SET retention_started_at = retention_started_at - make_interval(months => $1::int),
		    retention_expires_at = retention_expires_at - make_interval(months => $1::int),
		    -- Issue #1569 T1: the sweep and the retention claim both now require BOTH promised
		    -- pre-deletion notices to have actually been recorded
		    -- (retentionNoticesDeliveredPredicate). A fixture with NULL markers is a WITHHELD
		    -- account, not a deletion candidate, so every test below would be asserting against a
		    -- user the gate refuses. Backdated relative to the (already backdated) expiry so the
		    -- row describes a user genuinely warned before their clock ran out.
		    retention_notice_30d_sent_at = retention_expires_at - make_interval(months => $1::int) - INTERVAL '30 days',
		    retention_notice_7d_sent_at = retention_expires_at - make_interval(months => $1::int) - INTERVAL '7 days'
		WHERE id = $2`,
		claimFixtureRetentionMonths+1, user.ID,
	)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		UPDATE users
		SET is_deleted = $1,
		    deletion_requested_at = $2,
		    deletion_scheduled_at = $2,
		    manual_tier_override = $3,
		    manual_tier_ends_at = $4
		WHERE id = $5`,
		isDeleted, deletionRequestedAt, manualTierOverride, manualTierEndsAt, user.ID,
	)
	require.NoError(t, err)

	user.IsDeleted = isDeleted
	user.DeletionRequestedAt = deletionRequestedAt
	user.DeletionScheduledAt = deletionRequestedAt
	user.ManualTierOverride = manualTierOverride
	user.ManualTierEndsAt = manualTierEndsAt
	return user
}

// TestClaimForDeletion_Integration_NonComped_Claims proves the happy path: a genuinely eligible
// user (no override at all, no deletion request) can claim deletion. Also verifies the claim
// itself is non-destructive — it must not flip is_deleted; that remains DeleteUserCascade's job.
func TestClaimForDeletion_Integration_NonComped_Claims(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, false, nil, nil, nil)

	claimed, err := repo.ClaimForDeletion(ctx, user.ID, DeletionKindRetention)
	require.NoError(t, err)
	assert.True(t, claimed, "an ordinary never-comped user with no deletion request must be claimable")

	var isDeleted bool
	require.NoError(t, pool.QueryRow(ctx, "SELECT is_deleted FROM users WHERE id = $1", user.ID).Scan(&isDeleted))
	assert.False(t, isDeleted, "ClaimForDeletion is a reservation, not the deletion itself — is_deleted must remain FALSE")
}

// TestClaimForDeletion_Integration_ActiveOverride_NotClaimed is the race-closing proof: this
// performs the actual interleaving the design note calls for — grant a comp on a candidate row
// (simulating an admin acting between the retention worker's candidate selection and this call),
// THEN attempt the claim — rather than merely testing the happy path.
func TestClaimForDeletion_Integration_ActiveOverride_NotClaimed(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, false, nil, nil, nil)

	// Simulates an admin granting a comp AFTER the retention worker selected this user as a
	// candidate but BEFORE ExecuteDeletion's claim runs — the exact race window C2 closes.
	overrideEndsAt := time.Now().Add(24 * time.Hour) // still active
	tier := "premium"
	_, err := pool.Exec(ctx, `UPDATE users SET manual_tier_override = $1, manual_tier_ends_at = $2 WHERE id = $3`,
		tier, overrideEndsAt, user.ID)
	require.NoError(t, err)

	claimed, err := repo.ClaimForDeletion(ctx, user.ID, DeletionKindRetention)
	require.NoError(t, err)
	assert.False(t, claimed, "a user with a live manual tier override and no deletion request must NOT be claimed")

	var isDeleted bool
	require.NoError(t, pool.QueryRow(ctx, "SELECT is_deleted FROM users WHERE id = $1", user.ID).Scan(&isDeleted))
	assert.False(t, isDeleted)
}

// TestClaimForDeletion_Integration_ActiveOverride_WithDeletionRequest_StillClaims is the
// ratified-rule proof the brief requires explicitly: a user's own GDPR deletion request beats
// an admin comp. A comped user who has ALSO asked to be deleted must still be claimed — a
// blanket comp check (rather than one conditional on DeletionRequestedAt) would illegally
// strand this user.
func TestClaimForDeletion_Integration_ActiveOverride_WithDeletionRequest_StillClaims(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tier := "premium"
	overrideEndsAt := time.Now().Add(24 * time.Hour) // still active
	requestedAt := time.Now().Add(-24 * time.Hour)
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, false, &requestedAt, &tier, &overrideEndsAt)

	// DeletionKindUserRequested: this user is here on the strength of their OWN request, which is
	// the authorisation that is allowed to override a comp. The retention kind would (correctly)
	// refuse them — it requires deletion_requested_at IS NULL.
	claimed, err := repo.ClaimForDeletion(ctx, user.ID, DeletionKindUserRequested)
	require.NoError(t, err)
	assert.True(t, claimed, "a user's own outstanding GDPR deletion request must beat a live admin comp")
}

// TestClaimForDeletion_Integration_UserRequested_Cancelled_NotClaimed is the Issue #1440 T0
// regression proof at the predicate level: a user whose deletion request has been CANCELLED must
// not be claimable under the user-requested authorisation, no matter how ordinary they otherwise
// look. This user carries NO manual tier override at all — which is exactly what made the
// pre-split predicate authorise them, via its "OR (no override at all)" disjunct.
func TestClaimForDeletion_Integration_UserRequested_Cancelled_NotClaimed(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	requestedAt := time.Now().Add(-24 * time.Hour)
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, false, &requestedAt, nil, nil)

	claimed, err := repo.ClaimForDeletion(ctx, user.ID, DeletionKindUserRequested)
	require.NoError(t, err)
	require.True(t, claimed, "sanity check: claimable while the request is outstanding and its window has elapsed")

	// The user cancels — the real CancelDeletion, not a hand-written UPDATE, so this test would
	// notice if CancelDeletion ever stopped clearing one of the two columns.
	require.NoError(t, repo.CancelDeletion(ctx, user.ID))

	claimed, err = repo.ClaimForDeletion(ctx, user.ID, DeletionKindUserRequested)
	require.NoError(t, err)
	assert.False(t, claimed, "a cancelled deletion request must not authorise a hard delete — "+
		"this is the Issue #1440 T0 data-loss scenario")
}

// TestClaimForDeletion_Integration_UserRequested_ReRequestedNotYetDue_NotClaimed is the second
// Issue #1440 T0 regression proof: cancel-then-re-request. A job queued for the FIRST episode must
// not be able to delete the user while the SECOND episode's grace period is still running.
//
// deletion_requested_at alone cannot express this — the stale job reads the NEW value and sees a
// perfectly valid outstanding request. Only deletion_scheduled_at <= NOW() distinguishes them.
func TestClaimForDeletion_Integration_UserRequested_ReRequestedNotYetDue_NotClaimed(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	requestedAt := time.Now().Add(-24 * time.Hour)
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, false, &requestedAt, nil, nil)

	claimed, err := repo.ClaimForDeletion(ctx, user.ID, DeletionKindUserRequested)
	require.NoError(t, err)
	require.True(t, claimed, "sanity check: episode 1's window has elapsed, so it is claimable")

	// Cancel, then re-request with a fresh 30-day window — both through the real writers.
	require.NoError(t, repo.CancelDeletion(ctx, user.ID))
	require.NoError(t, repo.MarkForDeletion(ctx, user.ID, time.Now().Add(30*24*time.Hour)))

	claimed, err = repo.ClaimForDeletion(ctx, user.ID, DeletionKindUserRequested)
	require.NoError(t, err)
	assert.False(t, claimed, "a re-requested deletion must not be claimable before its own "+
		"deletion_scheduled_at elapses, even though deletion_requested_at is non-NULL")
}

// TestClaimForDeletion_Integration_UserRequested_RequestClearedButScheduleRemains_NotClaimed pins
// the `deletion_requested_at IS NOT NULL` clause, which mutation testing showed is NOT what closes
// the cancellation case (deletion_scheduled_at <= NOW() does that, because CancelDeletion NULLs both
// columns). It is defence in depth, and this test is the only thing that covers it.
//
// The state it builds — deletion_requested_at NULL while deletion_scheduled_at is set and elapsed —
// is UNREACHABLE through any production writer today: all four writers of either column
// (CreateUser, CreateUserTx, MarkForDeletion, CancelDeletion — see the four-writer enumeration above
// deletionAuthorisationUnionPredicate in profile_repository.go) always write both together in a
// single statement, and the two INSERTs create the row, so there is no prior episode for a partial
// write to leave behind. It is constructed by direct UPDATE deliberately, to pin the behaviour of a
// future partial-clear writer before one exists rather than after it causes a wrongful hard delete.
func TestClaimForDeletion_Integration_UserRequested_RequestClearedButScheduleRemains_NotClaimed(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	requestedAt := time.Now().Add(-24 * time.Hour)
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, false, &requestedAt, nil, nil)

	claimed, err := repo.ClaimForDeletion(ctx, user.ID, DeletionKindUserRequested)
	require.NoError(t, err)
	require.True(t, claimed, "sanity check: both columns set and the window elapsed, so claimable")

	// Simulate a hypothetical writer that clears the request but leaves the schedule behind.
	_, err = pool.Exec(ctx, `UPDATE users SET deletion_requested_at = NULL WHERE id = $1`, user.ID)
	require.NoError(t, err)

	claimed, err = repo.ClaimForDeletion(ctx, user.ID, DeletionKindUserRequested)
	require.NoError(t, err)
	assert.False(t, claimed, "with no deletion request on the row, an orphaned elapsed schedule must "+
		"not authorise a user-requested hard delete")
}

// TestClaimForDeletion_Integration_Retention_OutstandingRequest_NotClaimed pins the tightening on
// the retention side: a user who requested deletion between the retention candidate query and this
// claim must NOT be swept up by the retention authorisation, which would delete them immediately
// and rob them of both their grace period and their cancellation window. The user-requested path
// will collect them once their own window elapses.
func TestClaimForDeletion_Integration_Retention_OutstandingRequest_NotClaimed(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, false, nil, nil, nil)

	claimed, err := repo.ClaimForDeletion(ctx, user.ID, DeletionKindRetention)
	require.NoError(t, err)
	require.True(t, claimed, "sanity check: an uncomped user with no request is a valid retention candidate")

	// The user requests deletion after the candidate query ran.
	require.NoError(t, repo.MarkForDeletion(ctx, user.ID, time.Now().Add(30*24*time.Hour)))

	claimed, err = repo.ClaimForDeletion(ctx, user.ID, DeletionKindRetention)
	require.NoError(t, err)
	assert.False(t, claimed, "the retention authorisation must not apply to a user with an "+
		"outstanding deletion request of their own — that request carries its own grace period")
}

// TestClaimForDeletion_Integration_UnknownKind_FailsClosed proves the kind switch fails CLOSED: an
// unrecognised authorisation — including DeletionClaimKind's zero value, which is what a caller
// that forgot to set it would pass — must be an error and must never claim.
func TestClaimForDeletion_Integration_UnknownKind_FailsClosed(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, false, nil, nil, nil)

	for _, kind := range []DeletionClaimKind{"", "retention_but_typoed"} {
		claimed, err := repo.ClaimForDeletion(ctx, user.ID, kind)
		require.Error(t, err, "an unrecognised claim kind %q must be an error", string(kind))
		assert.False(t, claimed, "an unrecognised claim kind %q must never claim", string(kind))
	}

	// The user is untouched and still claimable under a real kind — the failures above rejected the
	// call, they did not corrupt anything.
	claimed, err := repo.ClaimForDeletion(ctx, user.ID, DeletionKindRetention)
	require.NoError(t, err)
	assert.True(t, claimed)
}

// TestClaimForDeletion_Integration_ExpiredOverride_NoDeletionRequest_NotClaimed is the C1
// regression proof (Issue #1413, third round of remediation): a LAPSED-but-uncleared override
// must NOT be claimable for hard deletion, no differently than a live one.
//
// This test previously asserted the opposite — that an expired override still permits the
// claim — on the theory that the retention-kind claim predicate should use the "still live right
// now" rule (noActiveManualOverridePredicate), matching the deleted CancelIfPastDueOccurrenceWithRetention and ExpireTrialIfNoOverride (#1652). That
// was the C1 bug, not a documented rule: the retention *queries* that select this same user as a
// deletion candidate (GetUsersWithExpiredRetention) were correctly changed to exclude ANY
// override, live or lapsed (noManualOverrideAtAllPredicate) — see that constant's doc comment for
// why: a comp pauses the retention clock while live, and is only re-armed by the independently
// scheduled manual_tier_override_expiry_check job, which has no ordering guarantee relative to
// the retention worker. If the claim used the live-only rule while the query used the
// all-overrides rule, a user whose comp lapsed moments before the expiry job re-armed their clock
// would sail through: selected by the query (any override present, so actually excluded — wait,
// the real race is a comp granted AFTER a stale candidate was selected under the OLD "no override
// at all" query semantics from before this fix existed) and then hard-deleted by this claim
// before the expiry job ever ran. Requiring retentionDeletionClaimPredicate to agree with the
// retention queries' all-overrides rule closes that window: the worst case is a delay of up to one expiry
// job cycle, never a wrongful deletion — see
// TestClaimForDeletion_Integration_ClearedOverride_Claims immediately below for proof this is a
// delay, not a permanent exemption.
func TestClaimForDeletion_Integration_ExpiredOverride_NoDeletionRequest_NotClaimed(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tier := "premium"
	overrideEndsAt := time.Now().Add(-24 * time.Hour) // already lapsed, not yet cleared
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, false, nil, &tier, &overrideEndsAt)

	claimed, err := repo.ClaimForDeletion(ctx, user.ID, DeletionKindRetention)
	require.NoError(t, err)
	assert.False(t, claimed, "a lapsed-but-uncleared override must still block the deletion claim — "+
		"this is the C1 data-loss scenario: a lapsed comp must not slip through to a hard delete")

	var isDeleted bool
	require.NoError(t, pool.QueryRow(ctx, "SELECT is_deleted FROM users WHERE id = $1", user.ID).Scan(&isDeleted))
	assert.False(t, isDeleted)
}

// TestClaimForDeletion_Integration_ClearedOverride_Claims proves the exclusion above is a DELAY,
// not a permanent exemption: once manual_tier_override is actually cleared (simulating the daily
// manual_tier_override_expiry_check job doing its job), the same user becomes claimable again.
func TestClaimForDeletion_Integration_ClearedOverride_Claims(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tier := "premium"
	overrideEndsAt := time.Now().Add(-24 * time.Hour) // lapsed
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, false, nil, &tier, &overrideEndsAt)

	claimed, err := repo.ClaimForDeletion(ctx, user.ID, DeletionKindRetention)
	require.NoError(t, err)
	require.False(t, claimed, "sanity check: still blocked before the override is cleared")

	// Simulate the expiry job clearing the column.
	_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = NULL, manual_tier_ends_at = NULL WHERE id = $1`, user.ID)
	require.NoError(t, err)

	claimed, err = repo.ClaimForDeletion(ctx, user.ID, DeletionKindRetention)
	require.NoError(t, err)
	assert.True(t, claimed, "once the expiry job clears the override column, the user must become claimable again")
}

// TestClaimForDeletion_Integration_AlreadyDeleted_NotClaimed proves is_deleted = TRUE is
// excluded defense-in-depth (ExecuteDeletion already short-circuits on IsDeleted before ever
// calling ClaimForDeletion, but the claim itself must be safe to call standalone too).
func TestClaimForDeletion_Integration_AlreadyDeleted_NotClaimed(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, true, nil, nil, nil)

	claimed, err := repo.ClaimForDeletion(ctx, user.ID, DeletionKindRetention)
	require.NoError(t, err)
	assert.False(t, claimed, "an already-deleted user must not be claimable again")
}

// TestClaimForDeletion_Integration_MissingUser_NotClaimed proves a non-existent user ID simply
// fails the claim (0 rows affected) rather than erroring.
func TestClaimForDeletion_Integration_MissingUser_NotClaimed(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	claimed, err := repo.ClaimForDeletion(ctx, "00000000-0000-0000-0000-000000000000", DeletionKindRetention)
	require.NoError(t, err)
	assert.False(t, claimed)
}
