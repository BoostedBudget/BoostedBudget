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
	ManualTierOverride *string
	ManualTierEndsAt   *time.Time
	RetentionStartedAt *time.Time
	RetentionExpiresAt *time.Time
	RetentionNotice30d *time.Time
	RetentionNotice7d  *time.Time
}

func fetchRetentionSnapshot(t *testing.T, pool *pgxpool.Pool, userID string) retentionSnapshot {
	t.Helper()
	var s retentionSnapshot
	err := pool.QueryRow(context.Background(), `
		SELECT manual_tier_override, manual_tier_ends_at,
		       retention_started_at, retention_expires_at,
		       retention_notice_30d_sent_at, retention_notice_7d_sent_at
		FROM users WHERE id = $1
	`, userID).Scan(
		&s.ManualTierOverride, &s.ManualTierEndsAt,
		&s.RetentionStartedAt, &s.RetentionExpiresAt,
		&s.RetentionNotice30d, &s.RetentionNotice7d,
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

			override := "household_2"
			setManualOverride(t, pool, target.ID, &override, nil)

			// Simulate an 8-month-old comp against a stale retention clock that has already
			// expired — the exact hazard scenario from the issue.
			staleStart := time.Now().Add(-8 * 30 * 24 * time.Hour)
			staleExpires := time.Now().Add(-2 * 24 * time.Hour) // already in the past
			staleNotice30 := time.Now().Add(-40 * 24 * time.Hour)
			staleNotice7 := time.Now().Add(-10 * 24 * time.Hour)
			setRetentionState(t, pool, target.ID, &staleStart, &staleExpires, &staleNotice30, &staleNotice7)

			beforeCall := time.Now()
			row, err := repo.RemoveUserTierOverride(ctx, actor.ID, target.ID, "comp ended", testRetentionMonths)
			require.NoError(t, err)
			assert.Equal(t, status, row.RevertedStatus)

			snap := fetchRetentionSnapshot(t, pool, target.ID)
			require.NotNil(t, snap.RetentionStartedAt)
			require.NotNil(t, snap.RetentionExpiresAt)
			assert.WithinDuration(t, beforeCall, *snap.RetentionStartedAt, 10*time.Second,
				"retention_started_at must restart to ~now")
			expectedExpiry := beforeCall.AddDate(0, testRetentionMonths, 0)
			assert.WithinDuration(t, expectedExpiry, *snap.RetentionExpiresAt, 10*time.Second,
				"retention_expires_at must be ~now+retentionMonths, honoring the parameter (not hardcoded 6)")
			assert.Nil(t, snap.RetentionNotice30d, "30d notice flag must be nulled so the warning fires again")
			assert.Nil(t, snap.RetentionNotice7d, "7d notice flag must be nulled so the warning fires again")
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

	beforeCall := time.Now()
	row, err := repo.RemoveUserTierOverride(ctx, actor.ID, target.ID, "comp ended, never had a clock", testRetentionMonths)
	require.NoError(t, err)
	assert.Equal(t, "cancelled", row.RevertedStatus)

	snap := fetchRetentionSnapshot(t, pool, target.ID)
	require.NotNil(t, snap.RetentionStartedAt, "retention_started_at must be INITIALISED (H3 fix) even though the clock never ran before")
	require.NotNil(t, snap.RetentionExpiresAt, "retention_expires_at must be INITIALISED (H3 fix) even though the clock never ran before")
	assert.WithinDuration(t, beforeCall, *snap.RetentionStartedAt, 10*time.Second)
	assert.WithinDuration(t, beforeCall.AddDate(0, testRetentionMonths, 0), *snap.RetentionExpiresAt, 10*time.Second)
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

	override := "household_2"
	endsAt := time.Now().Add(-1 * time.Hour) // expired — exactly what the worker would have selected
	setManualOverride(t, pool, target.ID, &override, &endsAt)

	row, claimed, err := repo.RemoveUserTierOverrideIfMatches(
		ctx, actor.ID, target.ID, "expiry worker: lapsed comp", testRetentionMonths, &override, &endsAt)
	require.NoError(t, err)
	assert.True(t, claimed, "the clear must apply when the CAS predicate matches")
	assert.True(t, row.Removed)
	require.NotNil(t, row.PriorManualTierOverride)
	assert.Equal(t, "household_2", *row.PriorManualTierOverride)

	snap := fetchRetentionSnapshot(t, pool, target.ID)
	assert.Nil(t, snap.ManualTierOverride, "manual_tier_override must be cleared on a CAS match")
	assert.Nil(t, snap.ManualTierEndsAt, "manual_tier_ends_at must be cleared on a CAS match")

	audit := fetchLatestAuditRow(t, pool, target.ID, "remove_manual_tier")
	assert.Equal(t, actor.ID, *audit.UserID, "the CAS variant must still write an audit row")
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
