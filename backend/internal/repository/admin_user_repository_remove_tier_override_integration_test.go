//go:build integration

package repository

// Integration tests for AdminUserRepository.RemoveUserTierOverride (Issue #1414).
//
// pgxmock matches query strings and cannot validate a CTE, FOR UPDATE, CASE-driven writes, or
// an interval cast (make_interval) — a real database is required to prove these branches.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestRemoveUserTierOverride_Integration -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// testRetentionMonths is deliberately NOT the config default (6) so an implementation that
// hardcodes 6 in the SQL (instead of honoring the retentionMonths parameter) would produce a
// detectably wrong retention_expires_at.
const testRetentionMonths = 9

// createRemoveTierOverrideUser creates a user row with the given tier/status via
// ProfileRepository.CreateUserTx, registers cleanup, and returns the created user (ID populated).
func createRemoveTierOverrideUser(t *testing.T, pool *pgxpool.Pool, enc *appcrypto.FieldEncryptor, suffix, tier, status string) *domain.User {
	t.Helper()
	profileRepo := NewProfileRepository(pool, enc)
	user := &domain.User{
		ClerkUserID:        "clerk_rto_" + suffix,
		Email:              "rto-" + suffix + "@integration.example",
		SubscriptionTier:   tier,
		SubscriptionStatus: status,
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, profileRepo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) }) //nolint:errcheck
	return user
}

// setManualOverride sets manual_tier_override/manual_tier_ends_at directly via SQL (bypassing
// the repository under test, since we're constructing the "prior" state, not exercising it).
func setManualOverride(t *testing.T, pool *pgxpool.Pool, userID string, override *string, endsAt *time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		"UPDATE users SET manual_tier_override = $1, manual_tier_ends_at = $2 WHERE id = $3",
		override, endsAt, userID,
	)
	require.NoError(t, err)
}

// setRetentionState sets the four retention-tracking columns directly via SQL.
func setRetentionState(t *testing.T, pool *pgxpool.Pool, userID string, startedAt, expiresAt, notice30, notice7 *time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`UPDATE users
		 SET retention_started_at = $1,
		     retention_expires_at = $2,
		     retention_notice_30d_sent_at = $3,
		     retention_notice_7d_sent_at = $4
		 WHERE id = $5`,
		startedAt, expiresAt, notice30, notice7, userID,
	)
	require.NoError(t, err)
}

// retentionSnapshot is the full retention + override state of a user row, read directly via SQL
// (bypassing the repository under test).
type retentionSnapshot struct {
	ManualTierOverride    *string
	ManualTierEndsAt      *time.Time
	RetentionStartedAt    *time.Time
	RetentionExpiresAt    *time.Time
	RetentionNotice30d    *time.Time
	RetentionNotice7d     *time.Time
	PlaidCleanupPendingAt *time.Time
}

func fetchRetentionSnapshot(t *testing.T, pool *pgxpool.Pool, userID string) retentionSnapshot {
	t.Helper()
	var s retentionSnapshot
	err := pool.QueryRow(context.Background(), `
		SELECT manual_tier_override, manual_tier_ends_at,
		       retention_started_at, retention_expires_at,
		       retention_notice_30d_sent_at, retention_notice_7d_sent_at,
		       plaid_cleanup_pending_at
		FROM users WHERE id = $1
	`, userID).Scan(
		&s.ManualTierOverride, &s.ManualTierEndsAt,
		&s.RetentionStartedAt, &s.RetentionExpiresAt,
		&s.RetentionNotice30d, &s.RetentionNotice7d,
		&s.PlaidCleanupPendingAt,
	)
	require.NoError(t, err)
	return s
}

// auditRow is the subset of an audit_logs row relevant to these tests.
type auditRow struct {
	UserID     *string
	ResourceID *string
	Action     string
}

func fetchLatestAuditRow(t *testing.T, pool *pgxpool.Pool, resourceID, action string) auditRow {
	t.Helper()
	var row auditRow
	err := pool.QueryRow(context.Background(), `
		SELECT user_id::text, resource_id::text, action
		FROM audit_logs
		WHERE resource_id = $1 AND action = $2
		ORDER BY created_at DESC
		LIMIT 1
	`, resourceID, action).Scan(&row.UserID, &row.ResourceID, &row.Action)
	require.NoError(t, err)
	return row
}

// dbNow returns the database server's own current time — fencing a Go-side timestamp assertion
// against Postgres's clock, never against a Go-side time.Now(), which a containerized Postgres
// server's clock can disagree with by more than any fixed tolerance would safely assume (this
// exact class of flake has already failed CI twice on this branch's exact-boundary tests).
func dbNow(t *testing.T, pool *pgxpool.Pool) time.Time {
	t.Helper()
	var now time.Time
	require.NoError(t, pool.QueryRow(context.Background(), "SELECT NOW()").Scan(&now))
	return now
}

// assertBetweenFence asserts ts falls within [before, after] inclusive — the DB-clock-fenced
// replacement for assert.WithinDuration(..., beforeCall, 10*time.Second): before/after must both be
// dbNow(t, pool) calls bracketing the write under test, so this never depends on how far the Go
// runtime's clock and the Postgres server's clock have drifted apart, and needs no arbitrary
// tolerance window at all.
func assertBetweenFence(t *testing.T, before, after, ts time.Time, msg string) {
	t.Helper()
	assert.False(t, ts.Before(before), "%s: got %v, expected not before fence-before %v", msg, ts, before)
	assert.False(t, ts.After(after), "%s: got %v, expected not after fence-after %v", msg, ts, after)
}

// TestRemoveUserTierOverride_Integration_ClearsBothColumns verifies manual_tier_override and
// manual_tier_ends_at are both NULLed after removal.
func TestRemoveUserTierOverride_Integration_ClearsBothColumns(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	actor := createRemoveTierOverrideUser(t, pool, enc, suffix+"_actor", "premium", "active")
	target := createRemoveTierOverrideUser(t, pool, enc, suffix+"_target", "premium", "active")

	override := "household_3"
	endsAt := time.Now().Add(30 * 24 * time.Hour)
	setManualOverride(t, pool, target.ID, &override, &endsAt)

	_, err := repo.RemoveUserTierOverride(ctx, actor.ID, target.ID, "clearing override", testRetentionMonths)
	require.NoError(t, err)

	snap := fetchRetentionSnapshot(t, pool, target.ID)
	assert.Nil(t, snap.ManualTierOverride, "manual_tier_override must be cleared")
	assert.Nil(t, snap.ManualTierEndsAt, "manual_tier_ends_at must be cleared")
}

// TestRemoveUserTierOverride_Integration_NoOverride_ReturnsRemovedFalse verifies the idempotent
// no-op case: a user with no existing override returns Removed=false and no error.
func TestRemoveUserTierOverride_Integration_NoOverride_ReturnsRemovedFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	actor := createRemoveTierOverrideUser(t, pool, enc, suffix+"_actor", "premium", "active")
	target := createRemoveTierOverrideUser(t, pool, enc, suffix+"_target", "premium", "active")
	// No setManualOverride call — manual_tier_override stays NULL from creation.

	row, err := repo.RemoveUserTierOverride(ctx, actor.ID, target.ID, "repair stale metadata", testRetentionMonths)
	require.NoError(t, err)
	assert.False(t, row.Removed)
	assert.Nil(t, row.PriorManualTierOverride)
}

// TestRemoveUserTierOverride_Integration_ReturnsPriorAndRevertedState proves the CTE reads the
// pre-UPDATE snapshot: PriorManualTierOverride must be the value from BEFORE this call cleared
// it, and RevertedTier/RevertedStatus must reflect the user's raw (base) subscription state.
func TestRemoveUserTierOverride_Integration_ReturnsPriorAndRevertedState(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	actor := createRemoveTierOverrideUser(t, pool, enc, suffix+"_actor", "premium", "active")
	target := createRemoveTierOverrideUser(t, pool, enc, suffix+"_target", "premium", "active")

	override := "household_3"
	setManualOverride(t, pool, target.ID, &override, nil)

	row, err := repo.RemoveUserTierOverride(ctx, actor.ID, target.ID, "comp ended", testRetentionMonths)
	require.NoError(t, err)
	assert.True(t, row.Removed)
	require.NotNil(t, row.PriorManualTierOverride)
	assert.Equal(t, "household_3", *row.PriorManualTierOverride)
	assert.Equal(t, "premium", row.RevertedTier)
	assert.Equal(t, "active", row.RevertedStatus)
}

// TestRemoveUserTierOverride_Integration_RearmsRetention_WhenReadOnly verifies that when a
// retention clock was running (retention_expires_at IS NOT NULL) and the reverted raw status is
// read-only, the clock restarts: retention_started_at ~ now, retention_expires_at ~
// now+testRetentionMonths, and both notice-sent flags are nulled.
func TestRemoveUserTierOverride_Integration_RearmsRetention_WhenReadOnly(t *testing.T) {
	for _, status := range []string{"inactive", "cancelled", "past_due"} {
		t.Run(status, func(t *testing.T) {
			pool := connectTestDB(t)
			enc := newTestFieldEncryptor(t)
			repo := NewAdminUserRepository(pool, enc)
			ctx := context.Background()

			suffix := fmt.Sprintf("%d_%s", time.Now().UnixNano(), status)
			actor := createRemoveTierOverrideUser(t, pool, enc, suffix+"_actor", "premium", "active")
			target := createRemoveTierOverrideUser(t, pool, enc, suffix+"_target", "premium", status)
			// A live linked item is seeded here — this models the ordinary "cleanup genuinely
			// outstanding" case, and IS load-bearing for the plaid_cleanup_pending_at assertions
			// below: should_mark_plaid_cleanup_pending requires a live item at CTE time again
			// (Issue #1413 round 16 restored round 14's liveLinkedItemExistsClause guard — see
			// removeUserTierOverrideSelectPrior's doc comment for the round 14 -> 15 -> 16 history).
			seedLinkedItem(t, pool, target.ID, "item-"+suffix, "active")

			override := "household_2"
			setManualOverride(t, pool, target.ID, &override, nil)

			// Simulate an 8-month-old comp against a stale retention clock that has already
			// expired — the exact hazard scenario from the issue.
			staleStart := time.Now().Add(-8 * 30 * 24 * time.Hour)
			staleExpires := time.Now().Add(-2 * 24 * time.Hour) // already in the past
			staleNotice30 := time.Now().Add(-40 * 24 * time.Hour)
			staleNotice7 := time.Now().Add(-10 * 24 * time.Hour)
			setRetentionState(t, pool, target.ID, &staleStart, &staleExpires, &staleNotice30, &staleNotice7)

			before := dbNow(t, pool)
			row, err := repo.RemoveUserTierOverride(ctx, actor.ID, target.ID, "comp ended", testRetentionMonths)
			require.NoError(t, err)
			after := dbNow(t, pool)
			assert.Equal(t, status, row.RevertedStatus)

			snap := fetchRetentionSnapshot(t, pool, target.ID)
			require.NotNil(t, snap.RetentionStartedAt)
			require.NotNil(t, snap.RetentionExpiresAt)
			assertBetweenFence(t, before, after, *snap.RetentionStartedAt, "retention_started_at must restart to ~now")
			expectedExpiryFloor := before.AddDate(0, testRetentionMonths, 0)
			expectedExpiryCeil := after.AddDate(0, testRetentionMonths, 0)
			assertBetweenFence(t, expectedExpiryFloor, expectedExpiryCeil, *snap.RetentionExpiresAt,
				"retention_expires_at must be ~now+retentionMonths, honoring the parameter (not hardcoded 6)")
			assert.Nil(t, snap.RetentionNotice30d, "30d notice flag must be nulled so the warning fires again")
			assert.Nil(t, snap.RetentionNotice7d, "7d notice flag must be nulled so the warning fires again")

			// Issue #1413 round-10: plaid_cleanup_pending_at must (re-)start on comp removal ONLY
			// for the narrower set the Plaid-disconnect repair pass actually targets
			// (inactive/cancelled) — NOT past_due, which has its own dunning timeline
			// (past_due_service.go) and must never be immediately Plaid-disconnected by this pass.
			if status == "past_due" {
				assert.Nil(t, snap.PlaidCleanupPendingAt,
					"plaid_cleanup_pending_at must NOT be set when the reverted status is past_due — that has its own dunning timeline")
			} else {
				require.NotNil(t, snap.PlaidCleanupPendingAt,
					"plaid_cleanup_pending_at must be set when the reverted status is inactive/cancelled")
				assertBetweenFence(t, before, after, *snap.PlaidCleanupPendingAt, "plaid_cleanup_pending_at must be set to ~now")
			}
		})
	}
}

// TestRemoveUserTierOverride_Integration_DoesNotTouchRetention_WhenActive verifies that when the
// reverted raw status is NOT read-only (the user resubscribed while comped), retention fields are
// left completely untouched — the normal webhook path already clears them.
func TestRemoveUserTierOverride_Integration_DoesNotTouchRetention_WhenActive(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	actor := createRemoveTierOverrideUser(t, pool, enc, suffix+"_actor", "premium", "active")
	target := createRemoveTierOverrideUser(t, pool, enc, suffix+"_target", "premium", "active")

	override := "household_2"
	setManualOverride(t, pool, target.ID, &override, nil)

	fixedStart := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	fixedExpires := time.Date(2020, 7, 1, 0, 0, 0, 0, time.UTC)
	fixedNotice30 := time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC)
	fixedNotice7 := time.Date(2020, 6, 20, 0, 0, 0, 0, time.UTC)
	setRetentionState(t, pool, target.ID, &fixedStart, &fixedExpires, &fixedNotice30, &fixedNotice7)

	row, err := repo.RemoveUserTierOverride(ctx, actor.ID, target.ID, "resubscribed while comped", testRetentionMonths)
	require.NoError(t, err)
	assert.Equal(t, "active", row.RevertedStatus)

	snap := fetchRetentionSnapshot(t, pool, target.ID)
	require.NotNil(t, snap.RetentionStartedAt)
	require.NotNil(t, snap.RetentionExpiresAt)
	require.NotNil(t, snap.RetentionNotice30d)
	require.NotNil(t, snap.RetentionNotice7d)
	assert.True(t, fixedStart.Equal(*snap.RetentionStartedAt), "retention_started_at must be untouched for an active reverted status")
	assert.True(t, fixedExpires.Equal(*snap.RetentionExpiresAt), "retention_expires_at must be untouched for an active reverted status")
	assert.True(t, fixedNotice30.Equal(*snap.RetentionNotice30d), "30d notice flag must be untouched for an active reverted status")
	assert.True(t, fixedNotice7.Equal(*snap.RetentionNotice7d), "7d notice flag must be untouched for an active reverted status")
	assert.Nil(t, snap.PlaidCleanupPendingAt, "plaid_cleanup_pending_at must never be set when the reverted status is active — Issue #1413 round-10")
}

// TestRemoveUserTierOverride_Integration_InitialisesRetention_WhenClockNeverRan proves the H3
// fix (Issue #1413 High finding, Codex review): when a prior override existed, the reverted raw
// status is read-only, AND retention_expires_at was never set (NULL — the clock never ran, e.g.
// the cancellation webhook skipped SetRetentionDates because the user was comped at the time),
// the clock must now be INITIALISED on removal, not left NULL forever. Before this fix, the old
// CASE required `prior.retention_expires_at IS NOT NULL` to re-arm, which meant a user who was
// comped before ever going read-only would have no retention clock, get comped, have their
// subscription cancelled while comped (clock skipped), and then have the comp removed — landing
// permanently in read-only with no clock ever starting and no deletion warning ever sent. This
// test is the direct regression test for that scenario: it previously asserted the clock stayed
// NULL (encoding the bug); it now asserts the clock starts.
func TestRemoveUserTierOverride_Integration_InitialisesRetention_WhenClockNeverRan(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	actor := createRemoveTierOverrideUser(t, pool, enc, suffix+"_actor", "premium", "active")
	target := createRemoveTierOverrideUser(t, pool, enc, suffix+"_target", "premium", "cancelled")

	override := "household_2"
	setManualOverride(t, pool, target.ID, &override, nil)
	// retention_started_at/expires_at/notice flags all remain NULL from creation — clock never ran.

	before := dbNow(t, pool)
	row, err := repo.RemoveUserTierOverride(ctx, actor.ID, target.ID, "comp ended, never had a clock", testRetentionMonths)
	require.NoError(t, err)
	after := dbNow(t, pool)
	assert.Equal(t, "cancelled", row.RevertedStatus)

	snap := fetchRetentionSnapshot(t, pool, target.ID)
	require.NotNil(t, snap.RetentionStartedAt, "retention_started_at must be INITIALISED (H3 fix) even though the clock never ran before")
	require.NotNil(t, snap.RetentionExpiresAt, "retention_expires_at must be INITIALISED (H3 fix) even though the clock never ran before")
	assertBetweenFence(t, before, after, *snap.RetentionStartedAt, "retention_started_at must be ~now")
	assertBetweenFence(t, before.AddDate(0, testRetentionMonths, 0), after.AddDate(0, testRetentionMonths, 0), *snap.RetentionExpiresAt, "retention_expires_at must be ~now+retentionMonths")
	assert.Nil(t, snap.RetentionNotice30d)
	assert.Nil(t, snap.RetentionNotice7d)
}

// TestRemoveUserTierOverride_Integration_NoOpClear_DoesNotResetRunningRetention proves the H8
// fix (Issue #1413 High finding, Codex review): a "clear" call with NO prior override must be
// genuinely side-effect-free on retention, even when the reverted status is read-only and a
// retention clock IS already running. Before this fix, the CASE condition only checked
// `retention_expires_at IS NOT NULL AND status read-only` — it never checked whether an override
// had actually existed — so a repeated/stale/double-click "remove override" call on an already
// cancelled-with-no-override user would silently RESET retention_expires_at to
// now+retentionMonths and null both notice flags, indefinitely postponing deletion and
// suppressing warnings, every time it was called. This test fixes retention to a known past
// value and asserts it is untouched by a no-op call.
func TestRemoveUserTierOverride_Integration_NoOpClear_DoesNotResetRunningRetention(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	actor := createRemoveTierOverrideUser(t, pool, enc, suffix+"_actor", "premium", "active")
	target := createRemoveTierOverrideUser(t, pool, enc, suffix+"_target", "premium", "cancelled")
	// No setManualOverride call — manual_tier_override stays NULL (no prior override).

	fixedStart := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	fixedExpires := time.Date(2021, 7, 1, 0, 0, 0, 0, time.UTC)
	fixedNotice30 := time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC)
	fixedNotice7 := time.Date(2021, 6, 20, 0, 0, 0, 0, time.UTC)
	setRetentionState(t, pool, target.ID, &fixedStart, &fixedExpires, &fixedNotice30, &fixedNotice7)

	row, err := repo.RemoveUserTierOverride(ctx, actor.ID, target.ID, "stale double-click, no override present", testRetentionMonths)
	require.NoError(t, err)
	assert.False(t, row.Removed, "no prior override existed — this must be a no-op")

	snap := fetchRetentionSnapshot(t, pool, target.ID)
	require.NotNil(t, snap.RetentionStartedAt)
	require.NotNil(t, snap.RetentionExpiresAt)
	require.NotNil(t, snap.RetentionNotice30d)
	require.NotNil(t, snap.RetentionNotice7d)
	assert.True(t, fixedStart.Equal(*snap.RetentionStartedAt), "retention_started_at must be UNCHANGED by a no-op clear (H8 fix)")
	assert.True(t, fixedExpires.Equal(*snap.RetentionExpiresAt), "retention_expires_at must be UNCHANGED by a no-op clear (H8 fix) — a stale/repeated clear must not indefinitely postpone deletion")
	assert.True(t, fixedNotice30.Equal(*snap.RetentionNotice30d), "30d notice flag must be UNCHANGED by a no-op clear (H8 fix)")
	assert.True(t, fixedNotice7.Equal(*snap.RetentionNotice7d), "7d notice flag must be UNCHANGED by a no-op clear (H8 fix)")
}

// TestRemoveUserTierOverride_Integration_AuditRow_ActorAndTarget verifies the audit row lands
// with user_id=actor, resource_id=target, action='remove_manual_tier'.
func TestRemoveUserTierOverride_Integration_AuditRow_ActorAndTarget(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	actor := createRemoveTierOverrideUser(t, pool, enc, suffix+"_actor", "premium", "active")
	target := createRemoveTierOverrideUser(t, pool, enc, suffix+"_target", "premium", "active")
	require.NotEqual(t, actor.ID, target.ID)

	_, err := repo.RemoveUserTierOverride(ctx, actor.ID, target.ID, "audit wiring check", testRetentionMonths)
	require.NoError(t, err)

	audit := fetchLatestAuditRow(t, pool, target.ID, "remove_manual_tier")
	require.NotNil(t, audit.UserID)
	require.NotNil(t, audit.ResourceID)
	assert.Equal(t, actor.ID, *audit.UserID, "audit_logs.user_id must be the ACTING ADMIN")
	assert.Equal(t, target.ID, *audit.ResourceID, "audit_logs.resource_id must be the TARGET user")
	assert.Equal(t, "remove_manual_tier", audit.Action)
}

// TestRemoveUserTierOverride_Integration_EmptyActor_NoFKViolation verifies that an empty
// actorUserID is coerced to SQL NULL (not an invalid-UUID / FK violation), since audit_logs.user_id
// is nullable with ON DELETE SET NULL and a missing actor must never abort a legitimate admin action.
func TestRemoveUserTierOverride_Integration_EmptyActor_NoFKViolation(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	target := createRemoveTierOverrideUser(t, pool, enc, suffix+"_target", "premium", "active")

	_, err := repo.RemoveUserTierOverride(ctx, "", target.ID, "system-initiated cleanup", testRetentionMonths)
	require.NoError(t, err, "empty actor must not cause an FK violation or any other error")

	audit := fetchLatestAuditRow(t, pool, target.ID, "remove_manual_tier")
	assert.Nil(t, audit.UserID, "audit_logs.user_id must be NULL when actorUserID is empty")
}

// TestRemoveUserTierOverride_Integration_UserNotFound verifies a missing/soft-deleted target
// surfaces as ErrUserNotFound (the same sentinel UpdateUserTier uses, so the handler's existing
// errors.Is(err, services.ErrAdminUserNotFound) -> 404 mapping works unchanged).
func TestRemoveUserTierOverride_Integration_UserNotFound(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	actor := createRemoveTierOverrideUser(t, pool, enc, suffix+"_actor", "premium", "active")

	_, err := repo.RemoveUserTierOverride(ctx, actor.ID, "00000000-0000-0000-0000-000000000000", "no such user", testRetentionMonths)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUserNotFound)
}

// TestRemoveUserTierOverride_Integration_SoftDeletedTarget verifies a soft-deleted
// (is_deleted=TRUE) target also surfaces as ErrUserNotFound, matching the PUT sibling's
// is_deleted=FALSE guard.
func TestRemoveUserTierOverride_Integration_SoftDeletedTarget(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	actor := createRemoveTierOverrideUser(t, pool, enc, suffix+"_actor", "premium", "active")
	target := createRemoveTierOverrideUser(t, pool, enc, suffix+"_target", "premium", "active")

	_, err := pool.Exec(ctx, "UPDATE users SET is_deleted = TRUE WHERE id = $1", target.ID)
	require.NoError(t, err)

	_, err = repo.RemoveUserTierOverride(ctx, actor.ID, target.ID, "target is soft-deleted", testRetentionMonths)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUserNotFound)
}

// ─── Issue #1413 High finding H4 (Codex review): RemoveUserTierOverrideIfMatches ───────────
//
// The expiry worker selects expired-override candidates, then removes them. Without a CAS
// guard, an admin who extends or replaces the override in the window between the select and the
// removal has their fresh comp destroyed by the worker's stale decision. These tests prove the
// CAS clause closes that window while leaving the unconditional admin path (tested above)
// untouched.

// TestRemoveUserTierOverrideIfMatches_Integration_CASMismatch_Override_NoClear verifies that
// when manual_tier_override no longer matches what the caller expected (an admin changed it
// concurrently), the clear does NOT happen: claimed=false, no error, and the override column is
// left exactly as it now is in the database.
func TestRemoveUserTierOverrideIfMatches_Integration_CASMismatch_Override_NoClear(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	actor := createRemoveTierOverrideUser(t, pool, enc, suffix+"_actor", "premium", "active")
	target := createRemoveTierOverrideUser(t, pool, enc, suffix+"_target", "premium", "cancelled")

	endsAt := time.Now().Add(1 * time.Hour)
	currentOverride := "household_3" // an admin has since replaced "household_2" with this
	setManualOverride(t, pool, target.ID, &currentOverride, &endsAt)

	staleExpected := "household_2" // what the worker observed at selection time
	row, claimed, err := repo.RemoveUserTierOverrideIfMatches(
		ctx, actor.ID, target.ID, "expiry worker: stale decision", testRetentionMonths, &staleExpected, &endsAt)
	require.NoError(t, err, "a CAS mismatch must not be an error")
	assert.False(t, claimed, "the clear must not apply when the override no longer matches")
	assert.Equal(t, RemoveTierOverrideRow{}, row, "a zero-value row must be returned on CAS mismatch")

	snap := fetchRetentionSnapshot(t, pool, target.ID)
	require.NotNil(t, snap.ManualTierOverride)
	assert.Equal(t, "household_3", *snap.ManualTierOverride, "the override must be left INTACT — a stale worker decision must not clobber the admin's fresh comp")
	require.NotNil(t, snap.ManualTierEndsAt)
	assert.WithinDuration(t, endsAt, *snap.ManualTierEndsAt, time.Second)
}

// TestRemoveUserTierOverrideIfMatches_Integration_CASMismatch_EndsAt_NoClear verifies the same
// non-clearing outcome when only manual_tier_ends_at (not the override name) has changed under
// the caller — e.g. an admin extended the comp's expiry.
func TestRemoveUserTierOverrideIfMatches_Integration_CASMismatch_EndsAt_NoClear(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	actor := createRemoveTierOverrideUser(t, pool, enc, suffix+"_actor", "premium", "active")
	target := createRemoveTierOverrideUser(t, pool, enc, suffix+"_target", "premium", "cancelled")

	override := "household_2"
	extendedEndsAt := time.Now().Add(48 * time.Hour) // admin extended it
	setManualOverride(t, pool, target.ID, &override, &extendedEndsAt)

	staleEndsAt := time.Now().Add(-1 * time.Hour) // what the worker observed (already past)
	row, claimed, err := repo.RemoveUserTierOverrideIfMatches(
		ctx, actor.ID, target.ID, "expiry worker: stale decision", testRetentionMonths, &override, &staleEndsAt)
	require.NoError(t, err)
	assert.False(t, claimed, "the clear must not apply when manual_tier_ends_at no longer matches")
	assert.Equal(t, RemoveTierOverrideRow{}, row)

	snap := fetchRetentionSnapshot(t, pool, target.ID)
	require.NotNil(t, snap.ManualTierOverride)
	assert.Equal(t, "household_2", *snap.ManualTierOverride)
	require.NotNil(t, snap.ManualTierEndsAt)
	assert.WithinDuration(t, extendedEndsAt, *snap.ManualTierEndsAt, time.Second, "the extended expiry must be left INTACT")
}

// TestRemoveUserTierOverrideIfMatches_Integration_CASMatch_Clears verifies the positive case:
// when the current override still matches exactly what the caller expected, the clear applies
// normally — claimed=true, columns cleared, Removed=true, retention/audit behavior identical to
// the unconditional path (both variants share removeTierOverride).
func TestRemoveUserTierOverrideIfMatches_Integration_CASMatch_Clears(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	actor := createRemoveTierOverrideUser(t, pool, enc, suffix+"_actor", "premium", "active")
	target := createRemoveTierOverrideUser(t, pool, enc, suffix+"_target", "premium", "cancelled")
	// A live linked item is seeded here — this models the ordinary "cleanup genuinely outstanding"
	// case, and IS load-bearing for the plaid_cleanup_pending_at assertion below:
	// should_mark_plaid_cleanup_pending requires a live item at CTE time again (Issue #1413 round 16
	// restored round 14's liveLinkedItemExistsClause guard — see removeUserTierOverrideSelectPrior's
	// doc comment for the round 14 -> 15 -> 16 history). See
	// TestRemoveUserTierOverrideIfMatches_Integration_CASMatch_NoLiveItems_MarkerNotArmed below for
	// the negative control when no live item exists.
	seedLinkedItem(t, pool, target.ID, "item-"+suffix, "active")

	override := "household_2"
	endsAt := time.Now().Add(-1 * time.Hour) // expired — exactly what the worker would have selected
	setManualOverride(t, pool, target.ID, &override, &endsAt)
	// retention_started_at/expires_at/notice flags and plaid_cleanup_pending_at all remain NULL
	// from creation — same starting state as
	// TestRemoveUserTierOverride_Integration_InitialisesRetention_WhenClockNeverRan.

	before := dbNow(t, pool)
	row, claimed, err := repo.RemoveUserTierOverrideIfMatches(
		ctx, actor.ID, target.ID, "expiry worker: lapsed comp", testRetentionMonths, &override, &endsAt)
	require.NoError(t, err)
	after := dbNow(t, pool)
	assert.True(t, claimed, "the clear must apply when the CAS predicate matches")
	assert.True(t, row.Removed)
	require.NotNil(t, row.PriorManualTierOverride)
	assert.Equal(t, "household_2", *row.PriorManualTierOverride)

	snap := fetchRetentionSnapshot(t, pool, target.ID)
	assert.Nil(t, snap.ManualTierOverride, "manual_tier_override must be cleared on a CAS match")
	assert.Nil(t, snap.ManualTierEndsAt, "manual_tier_ends_at must be cleared on a CAS match")

	// The comment above promises retention behavior "identical to the unconditional path" — assert
	// it, don't just claim it. Reverted status is 'cancelled' (read-only) with a prior override,
	// so should_rearm_retention fires the same as the unconditional RemoveUserTierOverride path.
	require.NotNil(t, snap.RetentionStartedAt, "retention_started_at must be (re-)armed on a CAS-matched clear, same as the unconditional path")
	require.NotNil(t, snap.RetentionExpiresAt, "retention_expires_at must be (re-)armed on a CAS-matched clear, same as the unconditional path")
	assertBetweenFence(t, before, after, *snap.RetentionStartedAt, "retention_started_at must be ~now")
	assertBetweenFence(t, before.AddDate(0, testRetentionMonths, 0), after.AddDate(0, testRetentionMonths, 0), *snap.RetentionExpiresAt,
		"retention_expires_at must be ~now+retentionMonths, honoring the parameter (not hardcoded 6)")
	assert.Nil(t, snap.RetentionNotice30d, "30d notice flag must be nulled on a CAS-matched clear")
	assert.Nil(t, snap.RetentionNotice7d, "7d notice flag must be nulled on a CAS-matched clear")
	require.NotNil(t, snap.PlaidCleanupPendingAt, "plaid_cleanup_pending_at must be (re-)armed too — reverted status 'cancelled' is Plaid-repair-eligible, a prior override existed, and a live linked item genuinely remains (should_mark_plaid_cleanup_pending's restored liveLinkedItemExistsClause guard)")
	assertBetweenFence(t, before, after, *snap.PlaidCleanupPendingAt, "plaid_cleanup_pending_at must be ~now")

	audit := fetchLatestAuditRow(t, pool, target.ID, "remove_manual_tier")
	assert.Equal(t, actor.ID, *audit.UserID, "the CAS variant must still write an audit row")
}

// ─── Issue #1413 round 14 -> 15 -> 16 (Codex review, multiple rounds on PR #1443): the marker must
// NOT be re-armed for an episode whose cleanup already finished ──
//
// M1's original finding (round 14): should_mark_plaid_cleanup_pending was true for ANY prior
// override on an inactive/cancelled user, with no check on whether cleanup was actually still
// outstanding. Sequence: (1) cancellation sets the marker; (2) the repair pass completes cleanup and
// clears it; (3) an admin grants a comp over the now-cleaned-up user (round-13: the grant itself
// never touches the marker); (4) the admin removes that comp while raw status is STILL
// inactive/cancelled. Before M1's fix, step (4)'s COALESCE(NULL, NOW()) stamped a FRESH marker for
// an episode that had already fully ended — with every item already 'disconnected', the repair
// query's live-item EXISTS clause would never select that row again, so the phantom marker survived
// forever.
//
// Round 14's fix (gating should_mark_plaid_cleanup_pending on liveLinkedItemExistsClause) closed
// that, but round 15 (Finding 3, High) found the gate itself raced a concurrent LinkAccounts insert
// into what was, at the time, a PERMANENT leak (missed marker, never revisited, bank connection
// never disconnected) — round 15 judged that strictly worse than M1's phantom and removed the gate
// again, accepting the phantom back as the lesser evil.
//
// Round 16 (this comment) restores round 14's gate because the calculus changed:
// profileRepository.StampMissingPlaidCleanupMarkers (profile_repository.go) now self-heals round
// 15's exact race on the very next repair pass instead of leaking forever (see
// removeUserTierOverrideSelectPrior's doc comment, admin_user_repository.go, for the full
// round-14 -> 15 -> 16 history), so the gate no longer has to be traded away to avoid a permanent
// leak. The two tests below are back to their original (round 14) names and Nil assertions, and
// TestRemoveUserTierOverride_Integration_LiveItemInsertedDuringRemoval_MarkerNotLost further down —
// round 15's genuine two-connection concurrency proof that the race is real — is KEPT as the
// backstop's own regression test, with its assertion updated: the race still occurs, but the user is
// now healed on the next pass instead of leaking.

// TestRemoveUserTierOverrideIfMatches_Integration_CASMatch_NoLiveItems_MarkerNotArmed is the M1
// negative control at the CAS-guarded call site: the SAME fixture shape as
// TestRemoveUserTierOverrideIfMatches_Integration_CASMatch_Clears above, EXCEPT no linked item is
// ever seeded for the target — cleanup has nothing outstanding. The marker must stay NULL even
// though the reverted status ('cancelled') and prior override are otherwise identical to the
// positive-control test.
//
// Falsifiability: dropping the liveLinkedItemExistsClause AND clause from
// should_mark_plaid_cleanup_pending makes this test fail — plaid_cleanup_pending_at would be
// stamped to ~now despite no live item existing.
func TestRemoveUserTierOverrideIfMatches_Integration_CASMatch_NoLiveItems_MarkerNotArmed(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	actor := createRemoveTierOverrideUser(t, pool, enc, suffix+"_actor", "premium", "active")
	target := createRemoveTierOverrideUser(t, pool, enc, suffix+"_target", "premium", "cancelled")
	// Deliberately NO seedLinkedItem call — this target has never linked (or has already fully
	// cleaned up) any Plaid item; there is nothing outstanding to repair.

	override := "household_2"
	endsAt := time.Now().Add(-1 * time.Hour)
	setManualOverride(t, pool, target.ID, &override, &endsAt)

	row, claimed, err := repo.RemoveUserTierOverrideIfMatches(
		ctx, actor.ID, target.ID, "expiry worker: lapsed comp, no live items", testRetentionMonths, &override, &endsAt)
	require.NoError(t, err)
	assert.True(t, claimed, "the clear must still apply — should_mark_plaid_cleanup_pending gates the MARKER, not the clear itself")
	assert.True(t, row.Removed)

	snap := fetchRetentionSnapshot(t, pool, target.ID)
	assert.Nil(t, snap.ManualTierOverride, "manual_tier_override must still be cleared")
	assert.Nil(t, snap.PlaidCleanupPendingAt,
		"M1: plaid_cleanup_pending_at must NOT be armed when the user has no live linked item — there is nothing outstanding for the repair pass to do, so re-arming it would create a phantom; round 16 restores this guard because StampMissingPlaidCleanupMarkers now heals the concurrent-insert race the guard can reopen, instead of that race being a permanent leak")
}

// TestRemoveUserTierOverride_Integration_M1_NoPhantomMarker_WhenCleanupAlreadyComplete is the full
// regression test, built through PRODUCTION WRITERS ONLY (profileRepo.UpdateSubscriptionFromPaddle,
// profileRepo.ClearPlaidCleanupPending, adminRepo.UpdateUserTier, adminRepo.RemoveUserTierOverride,
// linkedItemRepo.MarkItemsDisconnected — no raw `UPDATE users SET manual_tier_override ...` or
// `SET plaid_cleanup_pending_at ...`, and (as of Finding 5, Codex review round on PR #1443) no raw
// `SET status = 'disconnected'` either — the item is created via a raw insert as a live fixture,
// then actually transitioned to 'disconnected' via linkedItemRepo.MarkItemsDisconnected, the same
// writer repairPlaidDisconnects calls, so this test proves the real cleanup path can produce the
// state it asserts on, not just that a hand-seeded row does — and, closing Finding 5's observation
// gap, reads the item back afterward to confirm the writer actually changed its status rather than
// merely returning nil), reproducing the EXACT sequence from the M1 finding:
//
//  1. A real Paddle cancellation (UpdateSubscriptionFromPaddle) sets plaid_cleanup_pending_at —
//     the marker records that an ineligibility episode began.
//  2. The repair pass completes: the user's one linked item is marked 'disconnected' via the real
//     production writer (linkedItemRepo.MarkItemsDisconnected), and the marker is cleared
//     (ClearPlaidCleanupPending — the same production writer repairPlaidDisconnects calls via
//     ClearPlaidCleanupPendingIfMatches once cleanup genuinely completes).
//  3. An admin grants a comp over this already-cleaned-up, already-churned user
//     (adminRepo.UpdateUserTier) — round-13: the grant itself must not touch the marker.
//  4. The admin removes that comp (adminRepo.RemoveUserTierOverride) while raw subscription_status
//     is STILL 'cancelled' (nothing in this flow ever changes it back to 'active').
//
// The marker must stay NULL throughout — there is nothing outstanding to repair, so removing the
// comp must not conjure a phantom episode. Round 16 restores this as the correct outcome (see the
// header comment above): StampMissingPlaidCleanupMarkers now heals the one race that once forced
// this guard to be traded away (round 15, Finding 3), so the guard no longer has to choose between
// this phantom and that leak.
//
// Falsifiability: reverting should_mark_plaid_cleanup_pending to drop the liveLinkedItemExistsClause
// AND clause makes this test fail at step 4 — plaid_cleanup_pending_at would be stamped to ~now
// despite the user's only linked item already being fully disconnected.
func TestRemoveUserTierOverride_Integration_M1_NoPhantomMarker_WhenCleanupAlreadyComplete(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	profileRepo := NewProfileRepository(pool, enc)
	adminRepo := NewAdminUserRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	actor := createRemoveTierOverrideUser(t, pool, enc, suffix+"_actor", "premium", "active")
	target := createRemoveTierOverrideUser(t, pool, enc, suffix+"_target", "premium", "active")

	// Step 1: real churn via the real webhook-facing writer — this is what starts the episode.
	require.NoError(t, profileRepo.UpdateSubscriptionFromPaddle(ctx, target.ID, "premium", "cancelled", nil, nil, nil))
	snapAfterCancel := fetchRetentionSnapshot(t, pool, target.ID)
	require.NotNil(t, snapAfterCancel.PlaidCleanupPendingAt, "precondition: cancellation must start the marker")

	// The user's only linked item starts live, then is actually transitioned to 'disconnected' via
	// the real production writer below (Finding 5 fix) — not seeded directly as 'disconnected'.
	var itemID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, status)
		VALUES (gen_random_uuid(), $1, $2, 'enc-test-token', 'active')
		RETURNING id
	`, target.ID, "item-"+suffix).Scan(&itemID))

	// Step 2: the repair pass completing — the same production writers repairPlaidDisconnects calls
	// once DisconnectAllUserItemsIfEligible actually succeeds: MarkItemsDisconnected transitions the
	// item's real status, then ClearPlaidCleanupPending clears the marker.
	require.NoError(t, linkedItemRepo.MarkItemsDisconnected(ctx, []string{itemID}))
	// Finding 5's observation gap: MarkItemsDisconnected's own error return proves nothing about
	// whether the row's status actually changed (an UPDATE matching zero rows is not an error) —
	// read the item back and assert the status the repair pass actually depends on.
	var itemStatusAfterDisconnect *string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM linked_items WHERE id = $1`, itemID).Scan(&itemStatusAfterDisconnect))
	require.NotNil(t, itemStatusAfterDisconnect)
	require.Equal(t, "disconnected", *itemStatusAfterDisconnect, "precondition: MarkItemsDisconnected must actually have transitioned the item's status, not just returned nil")
	require.NoError(t, profileRepo.ClearPlaidCleanupPending(ctx, target.ID))
	snapAfterClear := fetchRetentionSnapshot(t, pool, target.ID)
	require.Nil(t, snapAfterClear.PlaidCleanupPendingAt, "precondition: the repair pass must have cleared the marker — cleanup is done")

	// Step 3: an admin comps this already-cancelled, already-cleaned-up account.
	future := time.Now().Add(24 * time.Hour)
	_, err := adminRepo.UpdateUserTier(ctx, actor.ID, target.ID, "household_2", "comp an already-cleaned-up churned account", &future)
	require.NoError(t, err)
	snapAfterGrant := fetchRetentionSnapshot(t, pool, target.ID)
	require.Nil(t, snapAfterGrant.PlaidCleanupPendingAt, "precondition: granting a comp must not touch the marker (round-13)")

	// Step 4: the admin removes the comp — raw subscription_status is still 'cancelled'
	// (UpdateUserTier never touches subscription_status).
	row, err := adminRepo.RemoveUserTierOverride(ctx, actor.ID, target.ID, "comp ended", testRetentionMonths)
	require.NoError(t, err)
	assert.True(t, row.Removed)
	require.Equal(t, "cancelled", row.RevertedStatus, "precondition: reverted status must still be cancelled for should_mark_plaid_cleanup_pending's CASE to even be considered")

	snapAfterRemove := fetchRetentionSnapshot(t, pool, target.ID)
	assert.Nil(t, snapAfterRemove.PlaidCleanupPendingAt,
		"M1: removing a comp must NOT re-arm a fresh marker for an episode whose cleanup already fully completed — the user's only linked item is already 'disconnected', so there is nothing outstanding to repair")
}

// ─── Issue #1413 Codex review round on PR #1443, Finding 3 (High), and its round-16 backstop ──
//
// This test proves TWO things in sequence, deliberately: (1) the race is real — restoring
// liveLinkedItemExistsClause (round 16) genuinely reopens the window round 14's gate always had,
// and the marker IS left NULL immediately after the concurrent insert lands; (2) that is no longer
// a permanent leak — StampMissingPlaidCleanupMarkers (profile_repository.go), run once per
// repairPlaidDisconnects pass, heals it on the very next call. This is the backstop's own
// regression test: it is what makes restoring round 14's gate (admin_user_repository.go) safe
// again.

// TestRemoveUserTierOverride_Integration_LiveItemInsertedDuringRemoval_MarkerNotLost reproduces the
// Finding 3 race with genuine two-connection concurrency (real goroutines, ordering enforced by a
// real Postgres lock — not a sleep): a user has a LIVE comp masking an already-'cancelled' raw
// status (not read-only, so LinkAccounts is reachable — the read-only allow-list contains only
// /api/v1/plaid/webhook). Connection 1 takes the exact FOR UPDATE lock
// removeUserTierOverrideQuery's CTE takes — via removeUserTierOverrideSelectPrior (the real
// production constant) plus FOR UPDATE, run as ITS OWN statement, because the production query is a
// single opaque round trip with no room to inject a pause — and confirms no live item is visible
// yet. Only THEN, strictly after that snapshot, connection 2 (a goroutine) issues an INSERT into
// linked_items for the same user, modeling LinkAccounts' insert (AccountService.LinkAccounts ->
// LinkedItemRepository.Create — a single independent autocommit statement with no lock of its own).
// Because linked_items.user_id has an FK to users(id), that INSERT's implicit key-lock acquisition
// genuinely blocks behind connection 1's held FOR UPDATE until connection 1 commits — so the insert
// is DETERMINISTICALLY guaranteed to land no earlier than the removal's own commit, reproducing the
// race's worst case without relying on scheduling luck. Connection 1 then runs the real, unmodified
// removeUserTierOverrideQuery (verbatim production text) and commits before the test waits on the
// insert goroutine.
//
// Honesty note on what kind of test this is: this IS true concurrent interleaving — two real
// connections, a real lock enforcing the order, no sleeps — but with one deliberate deviation from
// production worth stating plainly: connection 1 runs the equivalent read-then-write as two SQL
// round trips (a standalone FOR UPDATE snapshot, then the real production UPDATE statement) instead
// of production's single combined statement, because decomposing it is the only way to open an
// injection point for connection 2's insert. The lock semantics and the query text used for each
// half are otherwise identical to production.
//
// Falsifiability: dropping the liveLinkedItemExistsClause AND clause from
// should_mark_plaid_cleanup_pending makes the FIRST half of this test fail — the marker would be
// stamped unconditionally at removal time regardless of the race, so it would never be observed
// NULL here. Removing the StampMissingPlaidCleanupMarkers call below (or reverting that method to a
// no-op) makes the SECOND half fail — the marker would stay NULL forever instead of healing.
func TestRemoveUserTierOverride_Integration_LiveItemInsertedDuringRemoval_MarkerNotLost(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	// Raw status already 'cancelled' with a LIVE override masking it (not read-only, so
	// LinkAccounts is reachable) — the comp is exactly what makes the race's window possible. No
	// actor user is needed: this test drives the CTE and UPDATE directly (to open an injection
	// point for the concurrent insert), not the full removeTierOverride wrapper that writes the
	// audit log using an actor ID.
	target := createRemoveTierOverrideUser(t, pool, enc, suffix+"_target", "premium", "cancelled")
	override := "household_2"
	endsAt := time.Now().Add(24 * time.Hour) // live — not yet expired
	setManualOverride(t, pool, target.ID, &override, &endsAt)
	// Deliberately no seedLinkedItem call yet — LinkAccounts' insert has not landed when the
	// removal's snapshot below is taken; it lands only after connection 1 has captured its
	// decision.

	// Connection 1: take the same lock + snapshot the production CTE would, as a standalone
	// statement so the test can inject a synchronization point.
	tx1, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx1.Rollback(ctx) //nolint:errcheck // no-op after Commit below

	var priorOverride *string
	var subStatus string
	var retentionExpiresAt *time.Time
	var shouldRearm, shouldMarkSnapshot bool
	err = tx1.QueryRow(ctx, removeUserTierOverrideSelectPrior+` FOR UPDATE`, target.ID).Scan(
		&priorOverride, &subStatus, &retentionExpiresAt, &shouldRearm, &shouldMarkSnapshot,
	)
	require.NoError(t, err)
	// With round 14's liveLinkedItemExistsClause gate restored (round 16), shouldMarkSnapshot must
	// be FALSE here — this is the race's mechanism made concrete: no live item exists yet at this
	// snapshot, so the CTE genuinely (and, in isolation, correctly) sees nothing to re-arm.
	assert.False(t, shouldMarkSnapshot, "precondition: at snapshot time, with no live item yet, should_mark_plaid_cleanup_pending must be false — this is exactly what makes the race possible")
	var liveItemExistsBeforeInsert bool
	require.NoError(t, tx1.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM linked_items WHERE user_id = $1)`, target.ID,
	).Scan(&liveItemExistsBeforeInsert))
	require.False(t, liveItemExistsBeforeInsert, "precondition: no linked item exists yet at snapshot time")

	// Connection 2 (goroutine): LinkAccounts' insert, launched only now — strictly after
	// connection 1's snapshot has already been taken and its FOR UPDATE lock acquired. Uses a plain
	// pool.Exec (a fresh connection) rather than the shared seedLinkedItem helper, because t.Helper
	// assertions are unsafe to call from a non-test goroutine — the error is sent back over a
	// channel and checked on the main goroutine instead.
	insertDone := make(chan error, 1)
	go func() {
		_, insertErr := pool.Exec(context.Background(), `
			INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, status)
			VALUES (gen_random_uuid(), $1, $2, 'enc-test-token', 'active')
		`, target.ID, "item-"+suffix)
		insertDone <- insertErr
	}()

	// Run the REAL, unmodified production query (verbatim removeUserTierOverrideQuery) as
	// connection 1's second statement, then commit — releasing the FOR UPDATE lock the concurrent
	// insert's FK check has been waiting on. Re-acquiring FOR UPDATE on a row this same transaction
	// already holds is a self-lock no-op, not a deadlock.
	var revertedTier, revertedStatus, clerkEnc string
	var returnedPriorOverride *string
	err = tx1.QueryRow(ctx, removeUserTierOverrideQuery, target.ID, testRetentionMonths).Scan(
		&revertedTier, &revertedStatus, &clerkEnc, &returnedPriorOverride,
	)
	require.NoError(t, err)
	require.NoError(t, tx1.Commit(ctx))

	select {
	case insertErr := <-insertDone:
		require.NoError(t, insertErr, "the concurrent linked_item insert must eventually succeed once the removal's lock is released")
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the concurrent linked_item insert — the removal's FOR UPDATE lock should release on commit")
	}

	snap := fetchRetentionSnapshot(t, pool, target.ID)
	assert.Nil(t, snap.ManualTierOverride, "override must still be cleared")
	// PART 1: the race is real. Immediately after the removal transaction commits, the marker is
	// left NULL despite a live linked item now genuinely existing for this cancelled user — exactly
	// the scenario that was, before round 16, a PERMANENT leak.
	require.Nil(t, snap.PlaidCleanupPendingAt,
		"precondition: the race genuinely occurs — the marker is left NULL immediately after the removal commits, even though a live linked item now exists, because the concurrent insert landed strictly after the CTE's own snapshot (Issue #1413 Finding 3)")

	// PART 2: it self-heals. Round 16's backstop, run exactly as repairPlaidDisconnects runs it at
	// the start of every pass, must stamp this user on its very next invocation.
	profileRepo := NewProfileRepository(pool, enc)
	healedCount, err := profileRepo.StampMissingPlaidCleanupMarkers(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, healedCount, 1, "the backstop must report at least one row healed (this user, at minimum)")

	healedSnap := fetchRetentionSnapshot(t, pool, target.ID)
	assert.NotNil(t, healedSnap.PlaidCleanupPendingAt,
		"the marker must be healed by StampMissingPlaidCleanupMarkers on the next repair pass — this cancelled user's live bank connection must not be permanently invisible to GetUsersNeedingPlaidDisconnectRepair just because one pass's writer lost this race")
}

// TestRemoveUserTierOverrideIfMatches_Integration_CASMatch_NilEndsAt_Clears verifies the CAS
// clause's NULL-safety: when the expected manual_tier_ends_at is nil and the actual value in the
// database is also NULL, IS NOT DISTINCT FROM must treat that as a match (plain `=` would not).
func TestRemoveUserTierOverrideIfMatches_Integration_CASMatch_NilEndsAt_Clears(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	actor := createRemoveTierOverrideUser(t, pool, enc, suffix+"_actor", "premium", "active")
	target := createRemoveTierOverrideUser(t, pool, enc, suffix+"_target", "premium", "cancelled")

	override := "household_2"
	setManualOverride(t, pool, target.ID, &override, nil) // permanent override, no expiry

	row, claimed, err := repo.RemoveUserTierOverrideIfMatches(
		ctx, actor.ID, target.ID, "admin-equivalent removal via CAS path", testRetentionMonths, &override, nil)
	require.NoError(t, err)
	assert.True(t, claimed, "NULL expectedEndsAt must match a NULL manual_tier_ends_at (IS NOT DISTINCT FROM)")
	assert.True(t, row.Removed)

	snap := fetchRetentionSnapshot(t, pool, target.ID)
	assert.Nil(t, snap.ManualTierOverride)
	assert.Nil(t, snap.ManualTierEndsAt)
}

// TestRemoveUserTierOverrideIfMatches_Integration_MissingTarget_NotClaimed verifies a
// missing/soft-deleted target is reported the same way as a CAS mismatch: claimed=false, nil
// error — never ErrUserNotFound. Unlike the unconditional path, this CAS-guarded method has no
// way to distinguish "target gone" from "override changed", and the claim-then-act pattern
// treats both identically: a legitimate skip, not a failure.
func TestRemoveUserTierOverrideIfMatches_Integration_MissingTarget_NotClaimed(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	actor := createRemoveTierOverrideUser(t, pool, enc, suffix+"_actor", "premium", "active")

	override := "household_2"
	endsAt := time.Now().Add(-1 * time.Hour)
	row, claimed, err := repo.RemoveUserTierOverrideIfMatches(
		ctx, actor.ID, "00000000-0000-0000-0000-000000000000", "no such user", testRetentionMonths, &override, &endsAt)
	require.NoError(t, err)
	assert.False(t, claimed)
	assert.Equal(t, RemoveTierOverrideRow{}, row)
}

// TestRemoveUserTierOverrideIfMatches_Integration_AdminPathUnaffected is a direct A/B proof that
// adding the CAS variant did not change the unconditional admin path's behavior: given the exact
// same fixture as the CAS-match test above, RemoveUserTierOverride (no CAS args at all) still
// clears regardless, confirming its signature and semantics are untouched by the H4 fix.
func TestRemoveUserTierOverrideIfMatches_Integration_AdminPathUnaffected(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	actor := createRemoveTierOverrideUser(t, pool, enc, suffix+"_actor", "premium", "active")
	target := createRemoveTierOverrideUser(t, pool, enc, suffix+"_target", "premium", "cancelled")

	override := "household_5" // whatever is currently there — the admin path does not care
	endsAt := time.Now().Add(30 * 24 * time.Hour)
	setManualOverride(t, pool, target.ID, &override, &endsAt)

	row, err := repo.RemoveUserTierOverride(ctx, actor.ID, target.ID, "admin clicked Remove Override", testRetentionMonths)
	require.NoError(t, err)
	assert.True(t, row.Removed)

	snap := fetchRetentionSnapshot(t, pool, target.ID)
	assert.Nil(t, snap.ManualTierOverride, "admin path clears regardless of current value — no CAS involved")
	assert.Nil(t, snap.ManualTierEndsAt)
}
