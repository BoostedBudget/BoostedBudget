//go:build integration

package repository

// Integration tests for DeleteUserCascade's eligibility guard (Issue #1413 round-4 Critical
// findings C1 and C2, Codex adversarial review round 4).
//
// ClaimForDeletion genuinely re-verifies eligibility with a conditional UPDATE — but the flow
// then performs external cleanup (Paddle cancel, Plaid disconnect — potentially many seconds of
// network calls) and only THEN calls DeleteUserCascade, which used to be keyed on nothing but the
// user ID. During that external-cleanup window, an admin could grant a comp, or — the serious
// case — the user could cancel their own deletion request, and the cascade would still run,
// irreversibly destroying an account whose owner explicitly changed their mind.
//
// pgxmock cannot validate any of this — it only matches query text, not the live row state the
// FOR UPDATE lock and predicates depend on, or whether a concurrent write actually lands before
// or after this transaction's guard runs — so a real database is required.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestDeleteUserCascade_Integration -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// deleteCascadeRowQuerier is the minimal interface both *pgxpool.Pool and pgx.Tx satisfy — lets the
// assertUserRow* helpers below run against either a committed pool connection or an in-progress
// test transaction (TestDeleteUserCascade_Integration_NullStatusLinkedItemRemains_NotDeleted runs
// entirely inside its own tx, which is rolled back at the end instead of committed).
type deleteCascadeRowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row
}

// assertUserRowGone asserts the users row no longer exists at all. Issue #1440 replaced
// DeleteUserCascade's soft-delete tombstone (SET is_deleted = TRUE, row survives forever) with a
// real `DELETE FROM users` — a successful cascade must leave NO row behind, not a flagged one.
func assertUserRowGone(t *testing.T, ctx context.Context, q deleteCascadeRowQuerier, userID string) {
	t.Helper()
	var exists bool
	err := q.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)", userID).Scan(&exists)
	require.NoError(t, err)
	assert.False(t, exists, "Issue #1440: the users row must be hard-deleted, not soft-deleted or left behind")
}

// assertUserRowExists is assertUserRowGone's counterpart, used by every guard-abort test (comp
// granted, deletion cancelled, un-cleaned Plaid item remains): an aborted cascade must leave the
// row completely untouched — no hard delete, no tombstone, nothing.
func assertUserRowExists(t *testing.T, ctx context.Context, q deleteCascadeRowQuerier, userID string) {
	t.Helper()
	var exists bool
	err := q.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)", userID).Scan(&exists)
	require.NoError(t, err)
	assert.True(t, exists, "Issue #1440: an aborted cascade must not have touched the users row")
}

// createDeleteCascadeFixtureUser inserts a plain user row, then sets deletion_requested_at /
// manual_tier_override / manual_tier_ends_at directly via UPDATE, mirroring
// createClaimForDeletionFixtureUser's pattern. Returns the user re-read from the database
// (rather than the pre-write struct) so callers get the EXACT, DB-truncated
// deletion_requested_at value to use as "expected" — precisely what ExecuteDeletion's
// GetUserByID would have observed in production, avoiding any Go-vs-Postgres timestamp
// precision mismatch in the IS NOT DISTINCT FROM comparison under test.
func createDeleteCascadeFixtureUser(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repo ProfileRepository,
	suffix string,
	deletionRequestedAt *time.Time,
	manualTierOverride *string,
	manualTierEndsAt *time.Time,
) *domain.User {
	t.Helper()

	user := &domain.User{
		ClerkUserID:        "clerk_delcasc_" + suffix,
		Email:              "delcasc-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) })

	_, err = pool.Exec(ctx, `
		UPDATE users
		SET deletion_requested_at = $1,
		    manual_tier_override = $2,
		    manual_tier_ends_at = $3
		WHERE id = $4`,
		deletionRequestedAt, manualTierOverride, manualTierEndsAt, user.ID,
	)
	require.NoError(t, err)

	fresh, err := repo.GetUserByID(ctx, user.ID)
	require.NoError(t, err)
	return fresh
}

// TestDeleteUserCascade_Integration_OrdinaryEligibleUser_Deletes is the control/regression case:
// an ordinary eligible user (no override, no outstanding deletion request — the retention-
// triggered path) with no linked items must still be deleted exactly as before this fix.
func TestDeleteUserCascade_Integration_OrdinaryEligibleUser_Deletes(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, nil, nil, nil)

	deleted, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err)
	assert.True(t, deleted, "an ordinary eligible user must still be deleted")

	// Issue #1440: DeleteUserCascade now HARD-deletes the users row (soft-delete's
	// is_deleted = TRUE tombstone is gone) — the row must not exist at all afterward.
	assertUserRowGone(t, ctx, pool, user.ID)
}

// TestDeleteUserCascade_Integration_CompGrantedAfterClaim_NotDeleted proves C1's first race: an
// admin grants a comp AFTER ClaimForDeletion succeeded (simulated here directly against the row)
// but BEFORE DeleteUserCascade runs — the external-cleanup window. The user must NOT be deleted.
func TestDeleteUserCascade_Integration_CompGrantedAfterClaim_NotDeleted(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	// Retention-triggered path: no outstanding deletion request at claim time.
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, nil, nil, nil)

	claimed, err := repo.ClaimForDeletion(ctx, user.ID)
	require.NoError(t, err)
	require.True(t, claimed, "sanity check: the user must be claimable before the comp lands")

	// Simulates an admin granting a comp during the external-cleanup window (Paddle cancel,
	// Plaid disconnect) between ClaimForDeletion succeeding and DeleteUserCascade running.
	tier := "premium"
	overrideEndsAt := time.Now().Add(24 * time.Hour)
	_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = $1, manual_tier_ends_at = $2 WHERE id = $3`,
		tier, overrideEndsAt, user.ID)
	require.NoError(t, err)

	deleted, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err, "an aborted cascade is a normal outcome, not an error")
	assert.False(t, deleted, "a comp granted after the claim but before the cascade must abort the deletion")

	// Issue #1440: an aborted cascade must leave the row fully intact — not hard-deleted, and
	// not tombstoned either (there is no tombstone state anymore; the row is simply untouched).
	assertUserRowExists(t, ctx, pool, user.ID)
}

// TestDeleteUserCascade_Integration_DeletionCancelledAfterClaim_NotDeleted is the highest-value
// test in this task (Issue #1413 round-4 C1): a user with their OWN outstanding GDPR deletion
// request cancels it during the external-cleanup window, after ClaimForDeletion already
// succeeded but before DeleteUserCascade runs. The user carries NO manual tier override at all,
// so reusing claimForDeletionPredicate alone would NOT catch this (that predicate treats "no
// override" as sufficient authorization regardless of deletion_requested_at) — only the
// additional exact-value re-check this fix adds closes it. The user must NOT be deleted.
func TestDeleteUserCascade_Integration_DeletionCancelledAfterClaim_NotDeleted(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	requestedAt := time.Now().Add(-24 * time.Hour)
	// GDPR self-requested deletion path: outstanding request at claim time, NO override at all.
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, &requestedAt, nil, nil)
	require.NotNil(t, user.DeletionRequestedAt, "fixture sanity check")

	claimed, err := repo.ClaimForDeletion(ctx, user.ID)
	require.NoError(t, err)
	require.True(t, claimed, "sanity check: the user must be claimable before cancelling")

	// The user cancels their own deletion request during the external-cleanup window.
	require.NoError(t, repo.CancelDeletion(ctx, user.ID))

	// DeleteUserCascade is called with the STALE expected value ClaimForDeletion observed
	// (user.DeletionRequestedAt, captured before the cancellation) — exactly what ExecuteDeletion
	// would pass, since it never re-reads deletion_requested_at between the claim and the cascade.
	deleted, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err, "an aborted cascade is a normal outcome, not an error")
	assert.False(t, deleted, "a deletion request cancelled after the claim but before the cascade must abort the deletion — this is the C1 data-loss scenario")

	// Issue #1440: the row must survive an aborted cascade intact (hard delete never ran).
	var deletionRequestedAt *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT deletion_requested_at FROM users WHERE id = $1", user.ID).Scan(&deletionRequestedAt))
	assert.Nil(t, deletionRequestedAt, "the cancellation must stick — the cascade must not have touched this row at all")
}

// TestDeleteUserCascade_Integration_OwnGDPRRequestStillOutstanding_DeletedDespiteComp proves the
// deliberate precedence must survive the new guard: a user's OWN outstanding GDPR deletion
// request beats an admin comp. An over-tight guard that blocks legitimate GDPR deletions whenever
// ANY override is present must fail this test.
func TestDeleteUserCascade_Integration_OwnGDPRRequestStillOutstanding_DeletedDespiteComp(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	requestedAt := time.Now().Add(-24 * time.Hour)
	tier := "premium"
	overrideEndsAt := time.Now().Add(24 * time.Hour) // still active
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, &requestedAt, &tier, &overrideEndsAt)

	claimed, err := repo.ClaimForDeletion(ctx, user.ID)
	require.NoError(t, err)
	require.True(t, claimed, "a user's own outstanding GDPR deletion request must beat a live admin comp at claim time")

	// Nothing changes deletion_requested_at between the claim and the cascade in this scenario.
	deleted, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err)
	assert.True(t, deleted, "a user's own outstanding GDPR deletion request must still beat a live comp inside the cascade guard too")

	assertUserRowGone(t, ctx, pool, user.ID)
}

// TestDeleteUserCascade_Integration_UncleanedPlaidItemRemains_NotDeleted proves C1's companion
// finding C2: DeleteUserCascade must refuse to run while any linked_items row still needs
// Plaid-side cleanup (uncleanedLinkedItemExistsClause — status IS NULL OR status NOT IN
// ('disconnected', 'replaced'), Issue #1444) — deleting the local row would orphan a live,
// still-billing Plaid access token with no local record it ever existed.
func TestDeleteUserCascade_Integration_UncleanedPlaidItemRemains_NotDeleted(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, nil, nil, nil)

	// Default status from Create is 'active' — i.e. NOT yet cleaned up at Plaid.
	item := &domain.LinkedItem{
		UserID:      user.ID,
		ItemID:      "plaid-item-" + suffix,
		AccessToken: "access-token-" + suffix,
		Provider:    "plaid",
	}
	require.NoError(t, linkedItemRepo.Create(ctx, item))
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM linked_items WHERE id = $1", item.ID) })

	deleted, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err, "an aborted cascade is a normal outcome, not an error")
	assert.False(t, deleted, "the cascade must not run while an un-cleaned Plaid item remains")

	assertUserRowExists(t, ctx, pool, user.ID)

	var itemCount int
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM linked_items WHERE id = $1", item.ID).Scan(&itemCount))
	assert.Equal(t, 1, itemCount, "the linked_items row — the only pointer to the live Plaid token — must survive the aborted cascade")
}

// TestDeleteUserCascade_Integration_NullStatusLinkedItemRemains_NotDeleted is Issue #1444 Site
// C's RED test: the un-cleaned-Plaid-item guard's original predicate (`status <> 'disconnected'`)
// is NULL-unsafe — SQL's three-valued logic makes `NULL <> 'disconnected'` evaluate to UNKNOWN,
// not TRUE, so a linked_items row with a genuinely NULL status was silently excluded from the
// EXISTS check and uncleanedItemsExist came back false, letting the cascade proceed past its own
// guard and hard-delete the local pointer to what may still be a live, billing Plaid access
// token. This is NOT a scan panic (uncleanedItemsExist is a bool, not the status column itself)
// — it is a pure logic bug, silently WRONG rather than erroring. A NULL status must count as
// "active"/un-cleaned, per the same rule normalizeLinkedItemStatus and liveLinkedItemExistsClause
// already establish elsewhere in this file.
//
// Since migration 000126 (Issue #1444) added a NOT NULL constraint to linked_items.status, a
// genuinely NULL status can no longer be constructed against a fully migrated database at all.
// This test therefore runs entirely inside its own transaction, which transactionally DROPs that
// constraint first (rolled back with everything else — the real schema is never permanently
// altered) — it cannot reuse createDeleteCascadeFixtureUser (which creates its user via a separate,
// committed transaction against pool directly, invisible to this test's own transaction).
func TestDeleteUserCascade_Integration_NullStatusLinkedItemRemains_NotDeleted(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck
	_, err = tx.Exec(ctx, "ALTER TABLE linked_items ALTER COLUMN status DROP NOT NULL")
	require.NoError(t, err)

	repo := NewProfileRepository(tx, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_delcasc_" + suffix,
		Email:              "delcasc-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))

	// Genuinely NULL status — bypasses linkedItemRepo.Create (which omits `status` from its
	// column list and always gets the 'active' column default) via raw SQL, exactly as the
	// coordinator's brief requires for an honest RED: a NULL status is unreachable through any
	// production write path today, but was schema-legal and known to occur on rows created before
	// migration 000013's 'active' default existed (and before 000126's NOT NULL, dropped above for
	// the duration of this transaction only).
	itemID := "plaid-item-null-status-" + suffix
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, status)
		VALUES (gen_random_uuid(), $1, $2, 'enc-test-token', NULL)
	`, user.ID, itemID)
	require.NoError(t, err)

	deleted, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err, "an aborted cascade is a normal outcome, not an error")
	assert.False(t, deleted, "a NULL-status linked item must count as un-cleaned and block the cascade")

	assertUserRowExists(t, ctx, tx, user.ID)

	var itemCount int
	require.NoError(t, tx.QueryRow(ctx, "SELECT COUNT(*) FROM linked_items WHERE user_id = $1", user.ID).Scan(&itemCount))
	assert.Equal(t, 1, itemCount, "the NULL-status linked_items row must survive the aborted cascade")
}

// TestDeleteUserCascade_Integration_ReplacedStatusLinkedItemDoesNotBlock_Deletes is the
// companion control case for the #1444 Site C fix: a 'replaced' linked item (superseded by a
// re-link — Issue #171) does not need further Plaid-side cleanup — see
// isLiveLinkedItemStatus/liveLinkedItemExistsClause's established "NOT 'disconnected' or
// 'replaced'" definition of "still live" — so it must not block the cascade either, aligning
// this guard's predicate with the rest of the file (liveLinkedItemExistsClause,
// anyLinkedItemExistsClause) instead of the prior narrower spelling that only excluded
// 'disconnected'.
func TestDeleteUserCascade_Integration_ReplacedStatusLinkedItemDoesNotBlock_Deletes(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, nil, nil, nil)

	item := &domain.LinkedItem{
		UserID:      user.ID,
		ItemID:      "plaid-item-replaced-" + suffix,
		AccessToken: "access-token-" + suffix,
		Provider:    "plaid",
	}
	require.NoError(t, linkedItemRepo.Create(ctx, item))
	now := time.Now()
	_, err := pool.Exec(ctx, `UPDATE linked_items SET status = 'replaced', replaced_at = $1 WHERE id = $2`, now, item.ID)
	require.NoError(t, err)

	deleted, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err)
	assert.True(t, deleted, "a 'replaced' linked item does not need Plaid cleanup and must not block the cascade")

	assertUserRowGone(t, ctx, pool, user.ID)
}

// TestDeleteUserCascade_Integration_AllItemsDisconnected_Deletes is the C2 control case: once
// every linked_items row is 'disconnected' (Plaid cleanup already ran), the cascade proceeds
// normally and the linked_items rows are actually removed.
func TestDeleteUserCascade_Integration_AllItemsDisconnected_Deletes(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, nil, nil, nil)

	item := &domain.LinkedItem{
		UserID:      user.ID,
		ItemID:      "plaid-item-" + suffix,
		AccessToken: "access-token-" + suffix,
		Provider:    "plaid",
	}
	require.NoError(t, linkedItemRepo.Create(ctx, item))
	_, err := pool.Exec(ctx, `UPDATE linked_items SET status = 'disconnected' WHERE id = $1`, item.ID)
	require.NoError(t, err)

	deleted, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err)
	assert.True(t, deleted, "the cascade must proceed once every linked item is already disconnected")

	assertUserRowGone(t, ctx, pool, user.ID)

	var itemCount int
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM linked_items WHERE id = $1", item.ID).Scan(&itemCount))
	assert.Equal(t, 0, itemCount, "Step 3 of the cascade must have actually deleted the linked_items row")
}

// TestDeleteUserCascade_Integration_RedactsAuditLogPII proves Issue #1440's audit-log redaction
// requirement. audit_logs.user_id (the ACTING admin — see admin_user_repository.go:390-393's
// doc comment: "audit_logs.user_id -> the ACTING ADMIN; resource_id -> the TARGET") already gets
// nulled for free by its own ON DELETE SET NULL FK once the row is hard-deleted. But
// audit_logs.resource_id — the column that actually names the SUBJECT of an admin action, e.g. a
// tier-override target — is a bare UUID column with NO foreign key at all (migration 000003):
// it would otherwise keep pointing at the deleted user's id, WITH its PII-bearing `changes`
// payload intact, forever. This redacts the payload (`changes = NULL`) for both columns before
// the DELETE, while keeping the row itself — preserving the tamper-evident audit trail GDPR
// itself expects, while removing the personal data it carries.
func TestDeleteUserCascade_Integration_RedactsAuditLogPII(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	target := createDeleteCascadeFixtureUser(t, ctx, pool, repo, "target-"+suffix, nil, nil, nil)
	admin := createDeleteCascadeFixtureUser(t, ctx, pool, repo, "admin-"+suffix, nil, nil, nil)

	// Row 1: target is the ACTING admin (audit_logs.user_id) on some unrelated resource.
	var actorRowID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO audit_logs (user_id, action, resource_type, resource_id, changes)
		VALUES ($1, 'user.tier_override', 'user', gen_random_uuid(), 'actor-side-sensitive-payload')
		RETURNING id
	`, target.ID).Scan(&actorRowID))

	// Row 2: target is the SUBJECT of an admin action taken by someone else (audit_logs.resource_id)
	// — this is the column with NO foreign key, and the one Issue #1440 exists to close.
	var subjectRowID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO audit_logs (user_id, action, resource_type, resource_id, changes)
		VALUES ($1, 'user.tier_override', 'user', $2, 'subject-side-sensitive-payload')
		RETURNING id
	`, admin.ID, target.ID).Scan(&subjectRowID))

	deleted, err := repo.DeleteUserCascade(ctx, target.ID, target.DeletionRequestedAt)
	require.NoError(t, err)
	require.True(t, deleted)

	assertUserRowGone(t, ctx, pool, target.ID)
	assertUserRowExists(t, ctx, pool, admin.ID) // the admin was never a deletion target

	var actorUserID, actorChanges *string
	require.NoError(t, pool.QueryRow(ctx, "SELECT user_id, changes FROM audit_logs WHERE id = $1", actorRowID).Scan(&actorUserID, &actorChanges))
	assert.Nil(t, actorUserID, "audit_logs.user_id's own ON DELETE SET NULL FK must have fired")
	assert.Nil(t, actorChanges, "the actor-side row's PII payload must be redacted")

	var subjectResourceID, subjectUserID string
	var subjectChanges *string
	require.NoError(t, pool.QueryRow(ctx, "SELECT resource_id, changes, user_id FROM audit_logs WHERE id = $1", subjectRowID).Scan(&subjectResourceID, &subjectChanges, &subjectUserID))
	assert.Equal(t, target.ID, subjectResourceID, "resource_id has no FK — it must survive untouched, unlike user_id")
	assert.Nil(t, subjectChanges, "the subject-side row's PII payload must be redacted even though resource_id itself is not FK'd")
	assert.Equal(t, admin.ID, subjectUserID, "the unrelated acting admin's own id must be untouched")
}

// TestDeleteUserCascade_Integration_HouseholdOwnerWithActiveNonOwnerMember_NotDeleted is the
// Critical finding's RED test (Codex review, coordinator's own #1440 follow-up brief): the #1367
// household-owner guard (domain.ErrHouseholdHasActiveMembers) lives ONLY in
// DeletionService.RequestDeletion — the USER-INITIATED request path. ExecuteDeletion's other two
// callers (the retention-triggered deleteExpiredAccounts, and the legacy DeletionWorker /
// AccountDeletionWorker paths) call DeleteUserCascade directly with NO owner guard at all. A
// household owner whose retention clock expires while they still have an ACTIVE non-owner member
// must not be hard-deleted here: households.owner_user_id's CASCADE would take the household row
// and every other member's household_members row with it. This test calls DeleteUserCascade
// exactly as the retention path does — no RequestDeletion involved, deletion_requested_at stays
// nil — so it is a direct proof the guard must live in DeleteUserCascade itself, not upstream.
func TestDeleteUserCascade_Integration_HouseholdOwnerWithActiveNonOwnerMember_NotDeleted(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	owner := createDeleteCascadeFixtureUser(t, ctx, pool, repo, "owner-"+suffix, nil, nil, nil)
	member := createDeleteCascadeFixtureUser(t, ctx, pool, repo, "member-"+suffix, nil, nil, nil)

	var householdID string
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO households (owner_user_id, name) VALUES ($1, 'Guard H1') RETURNING id`, owner.ID,
	).Scan(&householdID))
	execNoErr(t, ctx, pool,
		`INSERT INTO household_members (household_id, user_id, role, status) VALUES ($1, $2, 'owner', 'active')`,
		householdID, owner.ID)
	execNoErr(t, ctx, pool,
		`INSERT INTO household_members (household_id, user_id, role, status) VALUES ($1, $2, 'member', 'active')`,
		householdID, member.ID)

	// Retention-triggered path: no ClaimForDeletion, no deletion_requested_at — DeleteUserCascade
	// called directly, exactly as deleteExpiredAccounts (retention_cleanup_check.go) does.
	deleted, err := repo.DeleteUserCascade(ctx, owner.ID, owner.DeletionRequestedAt)
	require.Error(t, err, "an owner with an active non-owner member must NOT be silently skipped (false, nil) — it must be a genuine, loud error so it is retried and eventually visible (DLQ / logs), not silently dropped forever")
	assert.ErrorIs(t, err, domain.ErrHouseholdHasActiveMembers)
	assert.False(t, deleted)

	assertUserRowExists(t, ctx, pool, owner.ID)
	assertUserRowExists(t, ctx, pool, member.ID)

	var householdCount int
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM households WHERE id = $1", householdID).Scan(&householdCount))
	assert.Equal(t, 1, householdCount, "the household must survive an aborted cascade")

	var memberRowCount int
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM household_members WHERE household_id = $1 AND user_id = $2", householdID, member.ID).Scan(&memberRowCount))
	assert.Equal(t, 1, memberRowCount, "the other member's household_members row must survive — this is the exact data the Critical finding says CASCADE would otherwise destroy")
}

// TestDeleteUserCascade_Integration_HouseholdOwnerWithOrphanedMemberEntitlement_NotDeleted proves
// the guard is broader than the #1367 active-member-count rule alone: RemoveMember
// (household_repository.go) only flips household_members.status to 'removed' — its own doc
// comment states it "Never touches Plaid Items", and it likewise never touches
// subscription_entitlements — so a just-removed member's entitlement, funded by this owner
// (payer_user_id), can outlive their membership row. An active-member-count-only guard would miss
// this: CountActiveMembers reports 1 (only the owner), yet subscription_entitlements.payer_user_id
// would still CASCADE away this OTHER real user's entitlement row. The owner must not be deletable
// while any such entitlement exists, regardless of the beneficiary's current membership status.
func TestDeleteUserCascade_Integration_HouseholdOwnerWithOrphanedMemberEntitlement_NotDeleted(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	owner := createDeleteCascadeFixtureUser(t, ctx, pool, repo, "owner2-"+suffix, nil, nil, nil)
	beneficiary := createDeleteCascadeFixtureUser(t, ctx, pool, repo, "beneficiary-"+suffix, nil, nil, nil)

	var householdID string
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO households (owner_user_id, name) VALUES ($1, 'Guard H2') RETURNING id`, owner.ID,
	).Scan(&householdID))
	execNoErr(t, ctx, pool,
		`INSERT INTO household_members (household_id, user_id, role, status) VALUES ($1, $2, 'owner', 'active')`,
		householdID, owner.ID)
	// beneficiary is deliberately NOT an active (or even present) household_members row — simulating
	// a removed member whose entitlement was never cleaned up, or any other provisioning gap.

	var entitlementID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO subscription_entitlements (user_id, source, tier, status, household_id, payer_user_id)
		VALUES ($1, 'household', 'premium', 'active', $2, $3) RETURNING id`,
		beneficiary.ID, householdID, owner.ID,
	).Scan(&entitlementID))

	deleted, err := repo.DeleteUserCascade(ctx, owner.ID, owner.DeletionRequestedAt)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrHouseholdHasActiveMembers)
	assert.False(t, deleted)

	assertUserRowExists(t, ctx, pool, owner.ID)
	assertUserRowExists(t, ctx, pool, beneficiary.ID)

	var entitlementCount int
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM subscription_entitlements WHERE id = $1", entitlementID).Scan(&entitlementCount))
	assert.Equal(t, 1, entitlementCount, "the beneficiary's entitlement (funded by the owner being deleted) must survive")
}

// TestDeleteUserCascade_Integration_HouseholdOwnerSoleActiveMemberSelfPayer_Deletes is the guard's
// control/negative case: a genuinely sole household owner — no other active members, and any
// household-funded entitlement belongs to the owner themselves (payer_user_id = user_id) — must
// still be deletable. An over-broad guard that blocks every household owner, not just ones with
// another real user's data at stake, would fail this test.
func TestDeleteUserCascade_Integration_HouseholdOwnerSoleActiveMemberSelfPayer_Deletes(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	owner := createDeleteCascadeFixtureUser(t, ctx, pool, repo, "owner3-"+suffix, nil, nil, nil)

	var householdID string
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO households (owner_user_id, name) VALUES ($1, 'Guard H3') RETURNING id`, owner.ID,
	).Scan(&householdID))
	execNoErr(t, ctx, pool,
		`INSERT INTO household_members (household_id, user_id, role, status) VALUES ($1, $2, 'owner', 'active')`,
		householdID, owner.ID)
	execNoErr(t, ctx, pool, `
		INSERT INTO subscription_entitlements (user_id, source, tier, status, household_id, payer_user_id)
		VALUES ($1, 'household', 'premium', 'active', $2, $3)`,
		owner.ID, householdID, owner.ID)

	deleted, err := repo.DeleteUserCascade(ctx, owner.ID, owner.DeletionRequestedAt)
	require.NoError(t, err)
	assert.True(t, deleted, "a genuinely sole household owner (no other member, no other beneficiary) must still be deletable")

	assertUserRowGone(t, ctx, pool, owner.ID)

	var householdCount int
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM households WHERE id = $1", householdID).Scan(&householdCount))
	assert.Equal(t, 0, householdCount, "the household must cascade away along with its sole owner")
}

// TestDeleteUserCascade_Integration_NestedCustomCategoriesAndOwnTransactionOverride_Deletes is
// Issue #1440 Finding 2's explicitly-requested test (Codex review): seed nested custom categories
// (a RESTRICT self-FK, custom_categories.parent_id) AND a transaction carrying
// custom_category_override_id (RESTRICT to custom_categories) set to the CHILD category, all owned
// by the SAME user being deleted, then delete. Empirically verified (see the coordinator's report):
// when every referencing and referenced row is cascaded away together within the SAME
// `DELETE FROM users` statement, Postgres's RESTRICT check is satisfied — it evaluates once every
// cascaded delete from this statement has completed, not per-row as each cascade step runs. This is
// the ordinary, expected shape (a user's own nested categories and own transaction overrides) and
// is a control/regression pin, not a bug fix: it must keep passing on its own, with no special
// pre-clean step required for the intra-user case.
func TestDeleteUserCascade_Integration_NestedCustomCategoriesAndOwnTransactionOverride_Deletes(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, "restrict-own-"+suffix, nil, nil, nil)

	var parentCategoryID string
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO custom_categories (user_id, name) VALUES ($1, 'Parent Cat') RETURNING id`, user.ID,
	).Scan(&parentCategoryID))

	var childCategoryID string
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO custom_categories (user_id, name, parent_id) VALUES ($1, 'Child Cat', $2) RETURNING id`,
		user.ID, parentCategoryID,
	).Scan(&childCategoryID))

	execNoErr(t, ctx, pool, `
		INSERT INTO transactions (user_id, source, amount, date, name, category_override_kind, custom_category_override_id)
		VALUES ($1, 'manual', 12.34, CURRENT_DATE, 'Test Txn', 'custom', $2)`,
		user.ID, childCategoryID)

	deleted, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err, "a user's own nested categories and own transaction override must not block their own deletion")
	assert.True(t, deleted)

	assertUserRowGone(t, ctx, pool, user.ID)

	var categoryCount int
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM custom_categories WHERE user_id = $1", user.ID).Scan(&categoryCount))
	assert.Equal(t, 0, categoryCount, "both nested categories must be gone")
}

// TestDeleteUserCascade_Integration_OtherUsersTransactionOverridesTargetsCategory_StillDeletes is
// Issue #1440 Finding 2's real, narrower residual: transactions.custom_category_override_id has NO
// trigger enforcing same-user ownership (unlike custom_categories.parent_id's own
// enforce_custom_category_two_levels trigger) — so a DIFFERENT, non-deleted user's transaction can
// reference the deletion target's custom_categories row as its override. That row is NOT part of
// the target's own DELETE FROM users cascade (the referencing transaction belongs to someone
// else), so the RESTRICT fires for real and the whole deletion fails — verified empirically before
// this fix existed. The fix clears the dangling override (both custom_category_override_id AND
// category_override_kind, to keep the valid_category_override_discriminator CHECK satisfied) on
// ANY transaction referencing a category the target owns, regardless of who owns that transaction,
// before the hard delete.
func TestDeleteUserCascade_Integration_OtherUsersTransactionOverridesTargetsCategory_StillDeletes(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	target := createDeleteCascadeFixtureUser(t, ctx, pool, repo, "restrict-target-"+suffix, nil, nil, nil)
	other := createDeleteCascadeFixtureUser(t, ctx, pool, repo, "restrict-other-"+suffix, nil, nil, nil)

	var categoryID string
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO custom_categories (user_id, name) VALUES ($1, 'Target Cat') RETURNING id`, target.ID,
	).Scan(&categoryID))

	var otherTxnID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO transactions (user_id, source, amount, date, name, category_override_kind, custom_category_override_id)
		VALUES ($1, 'manual', 20.00, CURRENT_DATE, 'Other user txn', 'custom', $2) RETURNING id`,
		other.ID, categoryID,
	).Scan(&otherTxnID))

	deleted, err := repo.DeleteUserCascade(ctx, target.ID, target.DeletionRequestedAt)
	require.NoError(t, err, "a stray cross-user override on target's category must not abort the deletion")
	assert.True(t, deleted)

	assertUserRowGone(t, ctx, pool, target.ID)
	assertUserRowExists(t, ctx, pool, other.ID)

	var otherKind, otherOverrideID *string
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT category_override_kind, custom_category_override_id FROM transactions WHERE id = $1", otherTxnID,
	).Scan(&otherKind, &otherOverrideID))
	assert.Nil(t, otherOverrideID, "the dangling override must be cleared once its target category is gone")
	assert.Nil(t, otherKind, "category_override_kind must be cleared too — leaving 'custom' with a NULL override_id would violate valid_category_override_discriminator")

	var otherTxnCount int
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM transactions WHERE id = $1", otherTxnID).Scan(&otherTxnCount))
	assert.Equal(t, 1, otherTxnCount, "the OTHER user's transaction row itself must survive — only the dangling override is cleared")
}

// TestDeleteUserCascade_Integration_NonFKUserScopedTablesAreCleaned is Issue #1440 Finding 3
// (Codex review): three tables carry a user_id (or actor_user_id) column with NO foreign key to
// users at all — dead_letter_jobs, idempotency_keys, mcp_audit_log — so the schema's own CASCADE
// graph (the mechanism this whole method exists to rely on) cannot clean them up automatically.
// dead_letter_jobs.args/errors and idempotency_keys.response_body can embed arbitrary PII from the
// original failed/cached request; mcp_audit_log.actor_user_id directly identifies the deleted
// user. Disposition (see the coordinator's report for the full per-table reasoning): all three are
// ERASED outright — unlike audit_logs (kept + redacted, since an ADMIN's audit_logs.user_id row
// documents an action against a DIFFERENT user, whose own security-audit interest survives the
// admin's deletion), none of these three has an "another user's own record depends on this row"
// property: dead_letter_jobs/idempotency_keys are the deleted user's own operational/ephemeral
// records, and mcp_audit_log.actor_user_id is always the deleted user's OWN MCP tool-call history,
// not a record ABOUT someone else.
func TestDeleteUserCascade_Integration_NonFKUserScopedTablesAreCleaned(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	target := createDeleteCascadeFixtureUser(t, ctx, pool, repo, "nonfk-target-"+suffix, nil, nil, nil)
	other := createDeleteCascadeFixtureUser(t, ctx, pool, repo, "nonfk-other-"+suffix, nil, nil, nil)

	now := time.Now()

	var targetDLQID, otherDLQID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO dead_letter_jobs (original_job_id, kind, queue, args, errors, max_attempts, created_at, user_id)
		VALUES (1001, 'account_deletion', 'critical', $1::jsonb, '[]'::jsonb, 5, $2, $3)
		RETURNING id`, `{"user_id":"`+target.ID+`"}`, now, target.ID,
	).Scan(&targetDLQID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO dead_letter_jobs (original_job_id, kind, queue, args, errors, max_attempts, created_at, user_id)
		VALUES (1002, 'account_deletion', 'critical', '{}'::jsonb, '[]'::jsonb, 5, $1, $2)
		RETURNING id`, now, other.ID,
	).Scan(&otherDLQID))
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM dead_letter_jobs WHERE id IN ($1, $2)", targetDLQID, otherDLQID) })

	execNoErr(t, ctx, pool, `
		INSERT INTO idempotency_keys (key, user_id, operation_id, response_body)
		VALUES ($1, $2, 'op-target', '{}'::jsonb)`, "idem-target-"+suffix, target.ID)
	execNoErr(t, ctx, pool, `
		INSERT INTO idempotency_keys (key, user_id, operation_id, response_body)
		VALUES ($1, $2, 'op-other', '{}'::jsonb)`, "idem-other-"+suffix, other.ID)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM idempotency_keys WHERE user_id IN ($1, $2)", target.ID, other.ID) })

	var targetAuditID, otherAuditID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO mcp_audit_log (actor_user_id, grant_id, operation_class, operation_id, result)
		VALUES ($1, 'grant-target', 'read', 'op-1', 'success') RETURNING id`, target.ID,
	).Scan(&targetAuditID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO mcp_audit_log (actor_user_id, grant_id, operation_class, operation_id, result)
		VALUES ($1, 'grant-other', 'read', 'op-2', 'success') RETURNING id`, other.ID,
	).Scan(&otherAuditID))
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM mcp_audit_log WHERE actor_user_id IN ($1, $2)", target.ID, other.ID) })

	deleted, err := repo.DeleteUserCascade(ctx, target.ID, target.DeletionRequestedAt)
	require.NoError(t, err)
	assert.True(t, deleted)

	assertUserRowGone(t, ctx, pool, target.ID)
	assertUserRowExists(t, ctx, pool, other.ID)

	var dlqCount int
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM dead_letter_jobs WHERE id = $1", targetDLQID).Scan(&dlqCount))
	assert.Equal(t, 0, dlqCount, "dead_letter_jobs.user_id has no FK to users — DeleteUserCascade must erase it explicitly")
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM dead_letter_jobs WHERE id = $1", otherDLQID).Scan(&dlqCount))
	assert.Equal(t, 1, dlqCount, "the OTHER user's dead-lettered job must survive")

	var idemCount int
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM idempotency_keys WHERE user_id = $1", target.ID).Scan(&idemCount))
	assert.Equal(t, 0, idemCount, "idempotency_keys.user_id has no FK to users — DeleteUserCascade must erase it explicitly")
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM idempotency_keys WHERE user_id = $1", other.ID).Scan(&idemCount))
	assert.Equal(t, 1, idemCount, "the OTHER user's idempotency key must survive")

	var auditCount int
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM mcp_audit_log WHERE id = $1", targetAuditID).Scan(&auditCount))
	assert.Equal(t, 0, auditCount, "mcp_audit_log.actor_user_id has no FK to users — DeleteUserCascade must erase it explicitly")
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM mcp_audit_log WHERE id = $1", otherAuditID).Scan(&auditCount))
	assert.Equal(t, 1, auditCount, "the OTHER user's mcp_audit_log row must survive")
}

// userScopedFK describes one live foreign key discovered by queryUserScopedFKsToUsers, pointing
// at users(id).
type userScopedFK struct {
	Table      string
	Column     string
	DeleteRule string
}

// queryUserScopedFKsToUsers dynamically enumerates EVERY foreign key in the live schema that
// references users(id), grouped by (table, column, delete_rule). Deliberately NOT hard-coded:
// Issue #1440 exists because DeleteUserCascade's five explicit DELETEs were silently treated as
// "the whole cascade" while 31 other user-scoped tables leaked personal data for years — a test
// that hard-codes "these N tables" would rot the exact same way. Querying pg_constraint/
// pg_attribute at TEST RUN TIME means the set of tables this test must cover is always the
// actual, current FK graph, not a snapshot of it from whenever this test was written.
//
// Issue #1440 Finding 6 (Codex review, hardening): this used to query information_schema's
// table_constraints/key_column_usage/constraint_column_usage/referential_constraints views,
// joined only by (constraint_name, schema) — never the child table. Postgres constraint names are
// unique only PER TABLE (pg_constraint's real key is (conrelid, conname)), not globally, so a
// same-named constraint on a completely unrelated table corrupted the discovered graph: verified
// empirically, a 2-real-FK scenario with one colliding name returned 8 phantom/duplicate rows,
// including the unrelated table appearing to have a fabricated FK to users and the genuine FK's
// delete_rule appearing twice with contradictory values. information_schema's views cannot be
// fully disambiguated by adding more join keys once two constraints share a (schema, name) pair —
// constraint_column_usage/referential_constraints don't expose the defining (child) table at all.
// Querying pg_constraint/pg_attribute directly sidesteps the whole class of bug: every row is
// already keyed by conrelid/confrelid (real object OIDs), never by name-matching, so a collision
// on the NAME cannot contaminate the result regardless of how many constraints share it.
func queryUserScopedFKsToUsers(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []userScopedFK {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT
			con.conrelid::regclass::text AS table_name,
			att.attname AS column_name,
			CASE con.confdeltype
				WHEN 'c' THEN 'CASCADE'
				WHEN 'n' THEN 'SET NULL'
				WHEN 'd' THEN 'SET DEFAULT'
				WHEN 'r' THEN 'RESTRICT'
				WHEN 'a' THEN 'NO ACTION'
				ELSE 'UNKNOWN'
			END AS delete_rule
		FROM pg_constraint con
		JOIN pg_attribute att
			ON att.attrelid = con.conrelid AND att.attnum = con.conkey[1]
		WHERE con.contype = 'f'
		  AND con.confrelid = 'users'::regclass
		  AND array_length(con.conkey, 1) = 1
		  AND con.confkey[1] = (SELECT attnum FROM pg_attribute WHERE attrelid = con.confrelid AND attname = 'id')
		ORDER BY table_name, column_name
	`)
	require.NoError(t, err)
	defer rows.Close()

	var out []userScopedFK
	for rows.Next() {
		var fk userScopedFK
		require.NoError(t, rows.Scan(&fk.Table, &fk.Column, &fk.DeleteRule))
		out = append(out, fk)
	}
	require.NoError(t, rows.Err())
	return out
}

// TestQueryUserScopedFKsToUsers_ImmuneToCollidingConstraintNameOnAnotherTable is Issue #1440
// Finding 6 (Codex review, hardening): Postgres constraint names are unique only PER TABLE
// (pg_constraint's real key is (conrelid, conname)), not globally — so a constraint on a
// completely unrelated table can share the exact same name as a genuine users-referencing FK.
// queryUserScopedFKsToUsers's ORIGINAL joins keyed information_schema.key_column_usage/
// constraint_column_usage to information_schema.table_constraints by constraint_name + schema
// only, never the child table — so a name collision made every one of those views' rows for BOTH
// constraints join against BOTH tables, corrupting the discovered graph with phantom rows,
// wrong delete_rules, and a completely unrelated table appearing to reference users. Verified
// empirically before this fix (see the coordinator's report): the original query returned 8 rows
// for a 2-real-FK scenario, including the colliding table with a fabricated "user_id" column and
// two different (contradictory) delete_rules for the SAME real column. This test creates that
// exact collision — a throwaway table whose FK constraint reuses the literal name
// "fk_merchant_name_overrides_user_id" but points at securities(id), not users — and asserts the
// discovered graph is immune to it.
func TestQueryUserScopedFKsToUsers_ImmuneToCollidingConstraintNameOnAnotherTable(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	execNoErr(t, ctx, pool, `CREATE TABLE IF NOT EXISTS _fk_collision_test_table (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), sec_id UUID)`)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DROP TABLE IF EXISTS _fk_collision_test_table") })
	execNoErr(t, ctx, pool, `
		ALTER TABLE _fk_collision_test_table
		ADD CONSTRAINT fk_merchant_name_overrides_user_id
		FOREIGN KEY (sec_id) REFERENCES securities(id) ON DELETE SET NULL
	`)

	liveFKs := queryUserScopedFKsToUsers(t, ctx, pool)

	for _, fk := range liveFKs {
		assert.NotEqual(t, "_fk_collision_test_table", fk.Table,
			"a same-named constraint on an unrelated table (pointing at securities, not users) must not appear in the users-scoped FK list")
	}

	var found *userScopedFK
	for i := range liveFKs {
		if liveFKs[i].Table == "merchant_name_overrides" && liveFKs[i].Column == "user_id" {
			found = &liveFKs[i]
		}
	}
	require.NotNil(t, found, "the genuine merchant_name_overrides -> users FK must still be discovered")
	assert.Equal(t, "CASCADE", found.DeleteRule, "the genuine FK's delete_rule must not be corrupted by the colliding constraint name")

	// No duplicate entries for the genuine FK either — the collision must not fan it out.
	count := 0
	for _, fk := range liveFKs {
		if fk.Table == "merchant_name_overrides" && fk.Column == "user_id" {
			count++
		}
	}
	assert.Equal(t, 1, count, "the genuine FK must be reported exactly once, not once per colliding constraint")
}

// quoteIdent wraps a Postgres identifier in double quotes. Safe here because every value passed
// through it originates from information_schema.table_name/column_name — the live schema itself,
// discovered by queryUserScopedFKsToUsers, never external input.
func quoteIdent(ident string) string {
	return `"` + ident + `"`
}

// execNoErr is a test helper that executes a query and requires no error. Reduces boilerplate
// in seeding blocks where many sequential Exec calls must all succeed.
func execNoErr(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...interface{}) {
	t.Helper()
	_, err := pool.Exec(ctx, sql, args...)
	require.NoError(t, err)
}

// TestDeleteUserCascade_Integration_EveryUserScopedTableIsCleaned is Issue #1440's central
// regression test. The bug this closes: DeleteUserCascade explicitly DELETEd 5 tables (boosts,
// transactions, linked_items, accounts, notifications) and then soft-deleted the users row — so
// NONE of the (36, at the time) ON DELETE CASCADE foreign keys into users ever fired, and every
// OTHER user-scoped table silently retained personal data after a "completed" GDPR deletion.
//
// A test asserting "these 5 tables are empty" would have passed for years while 31 others leaked
// — so this test does NOT hard-code a table list for its ASSERTIONS. It queries
// information_schema at RUN TIME (queryUserScopedFKsToUsers) for every foreign key that
// references users(id), asserts that live list matches expectedUserScopedFKs EXACTLY, and then
// iterates the LIVE list — not the hard-coded one — to make its per-table assertions. Add a new
// users-referencing FK to the schema without updating expectedUserScopedFKs (and seeding a row
// for it below), and the ElementsMatch assertion fails loudly, naming the exact gap — instead of
// silently passing with a table nobody seeded. That failure mode is the whole point: it is what
// should have caught this bug the first time.
func TestDeleteUserCascade_Integration_EveryUserScopedTableIsCleaned(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	target := createDeleteCascadeFixtureUser(t, ctx, pool, repo, "target-"+suffix, nil, nil, nil)
	otherA := createDeleteCascadeFixtureUser(t, ctx, pool, repo, "otherA-"+suffix, nil, nil, nil)
	otherB := createDeleteCascadeFixtureUser(t, ctx, pool, repo, "otherB-"+suffix, nil, nil, nil)

	// expectedUserScopedFKs is the tripwire: the exact set of (table, column, delete_rule) this
	// test knows how to seed and verify today. A live schema entry missing from this list (or a
	// listed entry no longer in the live schema) fails the ElementsMatch check below with a diff
	// — telling a future developer exactly which table needs seed+assertion coverage added here.
	expectedUserScopedFKs := []userScopedFK{
		{Table: "accounts", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "audit_logs", Column: "user_id", DeleteRule: "SET NULL"},
		{Table: "balance_snapshots", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "boosts", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "budgets", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "categorization_rules", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "credit_liabilities", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "custom_categories", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "holdings", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "household_invites", Column: "accepted_user_id", DeleteRule: "SET NULL"},
		{Table: "household_invites", Column: "inviter_user_id", DeleteRule: "CASCADE"},
		{Table: "household_members", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "household_share_consents", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "households", Column: "owner_user_id", DeleteRule: "CASCADE"},
		{Table: "investment_transactions", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "linked_items", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "mcp_recipes", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "merchant_name_overrides", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "mortgage_liabilities", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "notifications", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "paddle_transactions", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "portfolio_snapshots", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "privacy_settings", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "referral_codes", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "referrals", Column: "referred_id", DeleteRule: "CASCADE"},
		{Table: "referrals", Column: "referrer_id", DeleteRule: "CASCADE"},
		{Table: "rollover_history", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "savings_goals", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "student_loan_liabilities", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "subscription_entitlements", Column: "payer_user_id", DeleteRule: "CASCADE"},
		{Table: "subscription_entitlements", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "transactions", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "transfer_suggestions", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "user_accounts", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "user_dashboard_preferences", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "user_email_prefix_indices", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "user_identities", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "user_subscription_overrides", Column: "user_id", DeleteRule: "CASCADE"},
		{Table: "user_tags", Column: "user_id", DeleteRule: "CASCADE"},
	}

	liveFKs := queryUserScopedFKsToUsers(t, ctx, pool)
	require.ElementsMatch(t, expectedUserScopedFKs, liveFKs,
		"the schema's users-referencing FK graph changed since this test was written — add seed "+
			"and assertion coverage for the new/removed entry, then update expectedUserScopedFKs. "+
			"This mismatch IS the regression test for Issue #1440: a table with a user-scoped "+
			"column but no matching entry here is exactly how #1440 shipped.")

	// --- Seed one row in every table above, plus the supporting rows they need. ---

	// households: target is the SOLE owner of H1, with no other active household_members row and
	// no subscription_entitlements row funded for anyone other than target themselves. This is
	// deliberate: DeleteUserCascade DOES now enforce the household-owner guard
	// (householdOwnerBlocksDeletionClause, Issue #1440 Critical finding) INSIDE this same
	// transaction — a household with another active member, or an entitlement funded for someone
	// else, would abort this deletion entirely, defeating this test's actual purpose (proving every
	// OTHER user-scoped table's cascade is complete for a genuinely eligible deletion). The guard
	// itself — including the case this test intentionally avoids — is covered by its own dedicated
	// tests above (TestDeleteUserCascade_Integration_HouseholdOwnerWithActiveNonOwnerMember_NotDeleted
	// and TestDeleteUserCascade_Integration_HouseholdOwnerWithOrphanedMemberEntitlement_NotDeleted).
	var householdH1 string
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO households (owner_user_id, name) VALUES ($1, 'H1') RETURNING id`, target.ID,
	).Scan(&householdH1))

	var householdH2 string
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO households (owner_user_id, name) VALUES ($1, 'H2') RETURNING id`, otherA.ID,
	).Scan(&householdH2))

	execNoErr(t, ctx, pool,
		`INSERT INTO household_members (household_id, user_id, role, status) VALUES ($1, $2, 'owner', 'active')`,
		householdH1, target.ID)

	execNoErr(t, ctx, pool,
		`INSERT INTO household_share_consents (household_id, user_id, consent_version) VALUES ($1, $2, 'v1')`,
		householdH1, target.ID)

	// household_invites: one row tests inviter_user_id (target invites into their own household
	// H1, still pending) and one tests accepted_user_id (target accepted an invitation into
	// someone ELSE's household H2 — SET NULL, row survives).
	execNoErr(t, ctx, pool, `
		INSERT INTO household_invites (household_id, inviter_user_id, invitee_email, invitee_email_blind_idx, token_hash, status, expires_at)
		VALUES ($1, $2, 'invitee1@example.com', $3, $4, 'pending', NOW() + INTERVAL '1 day')`,
		householdH1, target.ID, "blind-"+suffix+"-1", "tok-"+suffix+"-1")

	var acceptedInviteID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO household_invites (household_id, inviter_user_id, invitee_email, invitee_email_blind_idx, token_hash, status, expires_at, accepted_at, accepted_user_id)
		VALUES ($1, $2, 'invitee2@example.com', $3, $4, 'accepted', NOW() + INTERVAL '1 day', NOW(), $5)
		RETURNING id`,
		householdH2, otherA.ID, "blind-"+suffix+"-2", "tok-"+suffix+"-2", target.ID,
	).Scan(&acceptedInviteID))

	// subscription_entitlements: a single household-sourced row where target is BOTH the
	// beneficiary (user_id) and the PAYER (payer_user_id) — exercises both CASCADE FKs
	// (user_id and payer_user_id) with one row, since uq_subscription_entitlements_active_user
	// forbids two ACTIVE rows for the same user_id. payer_user_id MUST equal the household's
	// owner_user_id (composite FK fk_subscription_entitlements_payer_is_owner) — but user_id is
	// independently free to name ANYONE, including someone other than the owner (Issue #1440
	// Critical finding, Codex review: this is exactly how an owner's deletion can CASCADE away a
	// DIFFERENT real user's entitlement, which is now blocked by
	// householdOwnerBlocksDeletionClause). Deliberately self-payer here so this row exercises the
	// payer_user_id CASCADE FK without tripping that guard; the cross-user shape (payer_user_id
	// names the owner, user_id names someone else) has its own dedicated guard test above
	// (TestDeleteUserCascade_Integration_HouseholdOwnerWithOrphanedMemberEntitlement_NotDeleted).
	execNoErr(t, ctx, pool, `
		INSERT INTO subscription_entitlements (user_id, source, tier, status, household_id, payer_user_id)
		VALUES ($1, 'household', 'premium', 'active', $2, $3)`, target.ID, householdH1, target.ID)

	// referrals: one row tests referred_id (target was referred by otherA), one tests
	// referrer_id (target referred otherB) — uq_referrals_referred forces distinct referred_id
	// values, hence two different counterpart users.
	execNoErr(t, ctx, pool, `INSERT INTO referrals (referrer_id, referred_id) VALUES ($1, $2)`, otherA.ID, target.ID)
	execNoErr(t, ctx, pool, `INSERT INTO referrals (referrer_id, referred_id) VALUES ($1, $2)`, target.ID, otherB.ID)

	execNoErr(t, ctx, pool, `INSERT INTO referral_codes (user_id, code) VALUES ($1, $2)`, target.ID, "R"+suffix) // VARCHAR(20): "R" + 19-digit nanosecond suffix fits exactly

	// linked_items -> accounts -> {balance_snapshots, credit/mortgage/student_loan_liabilities,
	// holdings, investment_transactions, savings_goals}.
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	item := &domain.LinkedItem{
		UserID:      target.ID,
		ItemID:      "plaid-item-" + suffix,
		AccessToken: "access-token-" + suffix,
		Provider:    "plaid",
	}
	require.NoError(t, linkedItemRepo.Create(ctx, item))
	// This test is proving ordinary successful-deletion cascade coverage, not the C2 un-cleaned-
	// Plaid-item guard (that's UncleanedPlaidItemRemains_NotDeleted/AllItemsDisconnected_Deletes
	// above) — mark the item already-cleaned-up so the guard does not abort this cascade.
	execNoErr(t, ctx, pool, `UPDATE linked_items SET status = 'disconnected' WHERE id = $1`, item.ID)

	var accountID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'Checking', 'depository') RETURNING id`,
		item.ID, target.ID, "plaid-acct-"+suffix,
	).Scan(&accountID))

	execNoErr(t, ctx, pool, `
		INSERT INTO balance_snapshots (account_id, user_id, snapshot_at, source)
		VALUES ($1, $2, NOW(), 'plaid')`, accountID, target.ID)

	execNoErr(t, ctx, pool, `INSERT INTO credit_liabilities (user_id, account_id) VALUES ($1, $2)`, target.ID, accountID)
	execNoErr(t, ctx, pool, `INSERT INTO mortgage_liabilities (user_id, account_id) VALUES ($1, $2)`, target.ID, accountID)
	execNoErr(t, ctx, pool, `INSERT INTO student_loan_liabilities (user_id, account_id) VALUES ($1, $2)`, target.ID, accountID)

	var securityID string
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO securities (plaid_security_id, name) VALUES ($1, 'Test Security') RETURNING id`,
		"sec-"+suffix,
	).Scan(&securityID))

	execNoErr(t, ctx, pool, `
		INSERT INTO holdings (user_id, account_id, security_id, quantity)
		VALUES ($1, $2, $3, 1)`, target.ID, accountID, securityID)

	execNoErr(t, ctx, pool, `
		INSERT INTO investment_transactions (user_id, account_id, plaid_investment_transaction_id, type, name, amount, date)
		VALUES ($1, $2, $3, 'buy', 'Test Buy', 100, CURRENT_DATE)`,
		target.ID, accountID, "plaid-inv-txn-"+suffix)

	execNoErr(t, ctx, pool, `
		INSERT INTO savings_goals (user_id, name, target_amount, account_id, progress_basis)
		VALUES ($1, 'Test Goal', 1000, $2, 'manual')`, target.ID, accountID)

	execNoErr(t, ctx, pool, `INSERT INTO portfolio_snapshots (user_id, snapshot_date) VALUES ($1, CURRENT_DATE)`, target.ID)

	// merchants -> merchant_name_overrides (Issue #1440: this FK did not exist before migration
	// 000128 — the one gap the coordinator's own schema audit missed; see this task's report).
	var merchantID string
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO merchants (name) VALUES ($1) RETURNING id`, "Merchant "+suffix,
	).Scan(&merchantID))
	execNoErr(t, ctx, pool, `
		INSERT INTO merchant_name_overrides (user_id, merchant_id, display_name)
		VALUES ($1, $2, 'My Nickname')`, target.ID, merchantID)

	// transactions: account_id is nullable — omitted, standalone row.
	execNoErr(t, ctx, pool, `
		INSERT INTO transactions (user_id, source, amount, date, name)
		VALUES ($1, 'manual', 12.34, CURRENT_DATE, 'Test Transaction')`, target.ID)

	// budgets -> budget_categories -> rollover_history, and budget_period_events. Issue #1440
	// bullet 2's due diligence: budget_period_events.user_id is TEXT with NO FK to users at all —
	// this table is covered only TRANSITIVELY, via its own budget_id CASCADE to budgets, not
	// directly. Seeded and asserted here to prove that transitive chain actually holds, not just
	// assert it in a comment.
	var budgetID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO budgets (user_id, name, period, start_date, amount, series_id)
		VALUES ($1, 'Test Budget', 'monthly', CURRENT_DATE, 100, gen_random_uuid()) RETURNING id`,
		target.ID,
	).Scan(&budgetID))

	execNoErr(t, ctx, pool, `
		INSERT INTO budget_period_events (user_id, budget_id, period_start, period_end, event_type)
		VALUES ($1, $2, CURRENT_DATE, CURRENT_DATE + 30, 'created')`, target.ID, budgetID)

	var budgetCategoryID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO budget_categories (budget_id, allocated_amount, category_kind, pfc_primary)
		VALUES ($1, 50, 'pfc', 'FOOD_AND_DRINK') RETURNING id`, budgetID,
	).Scan(&budgetCategoryID))

	execNoErr(t, ctx, pool, `
		INSERT INTO rollover_history (user_id, budget_id, source_budget_category_id, amount, rollover_type, budget_period_start, budget_period_end)
		VALUES ($1, $2, $3, 10, 'full', NOW(), NOW() + INTERVAL '1 day')`,
		target.ID, budgetID, budgetCategoryID)

	// Standalone tables (no dependencies beyond user_id).
	execNoErr(t, ctx, pool, `INSERT INTO boosts (user_id, name, boost_type) VALUES ($1, 'Test Boost', 'budget_alert')`, target.ID)
	execNoErr(t, ctx, pool, `INSERT INTO categorization_rules (user_id, name) VALUES ($1, 'Test Rule')`, target.ID)
	execNoErr(t, ctx, pool, `INSERT INTO custom_categories (user_id, name) VALUES ($1, 'Test Category')`, target.ID)
	execNoErr(t, ctx, pool, `INSERT INTO mcp_recipes (user_id, name) VALUES ($1, 'Test Recipe')`, target.ID)
	execNoErr(t, ctx, pool, `
		INSERT INTO notifications (user_id, type, title, message)
		VALUES ($1, 'budget_alert', 'Test', 'Test message')`, target.ID)
	execNoErr(t, ctx, pool, `
		INSERT INTO paddle_transactions (user_id, transaction_id, amount, status, payload)
		VALUES ($1, $2, 9.99, 'completed', '{}')`, target.ID, "txn-"+suffix)
	execNoErr(t, ctx, pool, `INSERT INTO privacy_settings (user_id) VALUES ($1)`, target.ID)
	execNoErr(t, ctx, pool, `INSERT INTO transfer_suggestions (user_id, amount, source) VALUES ($1, 25, 'manual')`, target.ID)
	execNoErr(t, ctx, pool, `INSERT INTO user_accounts (user_id, institution_id) VALUES ($1, 'inst-test')`, target.ID)
	execNoErr(t, ctx, pool, `INSERT INTO user_dashboard_preferences (user_id) VALUES ($1)`, target.ID)
	execNoErr(t, ctx, pool, `INSERT INTO user_email_prefix_indices (user_id, prefix_blind_idx) VALUES ($1, $2)`, target.ID, "prefix-"+suffix)
	execNoErr(t, ctx, pool, `
		INSERT INTO user_identities (user_id, clerk_user_id, provider, provider_user_id)
		VALUES ($1, $2, 'clerk', $3)`, target.ID, "clerk-identity-"+suffix, "provider-uid-"+suffix)
	execNoErr(t, ctx, pool, `
		INSERT INTO user_subscription_overrides (user_id, resource_type, limit_value)
		VALUES ($1, 'linked_items', 5)`, target.ID)
	execNoErr(t, ctx, pool, `INSERT INTO user_tags (user_id, name) VALUES ($1, 'Test Tag')`, target.ID)

	// audit_logs: one SET NULL row (target as actor) — resource_id deliberately points elsewhere
	// so this row is unambiguously the "actor" case, not the "subject" case (that's this suite's
	// dedicated TestDeleteUserCascade_Integration_RedactsAuditLogPII).
	var auditLogRowID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO audit_logs (user_id, action, resource_type, resource_id, changes)
		VALUES ($1, 'user.login', 'user', gen_random_uuid(), 'sensitive-payload')
		RETURNING id`, target.ID).Scan(&auditLogRowID))

	// --- Sanity-check: every live CASCADE FK actually has a seeded row before the cascade runs, so
	// the post-delete count==0 assertions below are not vacuously true. Issue #1440 Finding 5
	// (Codex review): an FK added to expectedUserScopedFKs (and the dynamic loop below) without a
	// matching seed above would previously pass silently — 0 rows before the cascade, 0 rows after,
	// no signal either way that coverage was actually missing.
	for _, fk := range liveFKs {
		if fk.DeleteRule != "CASCADE" {
			continue
		}
		var preCount int
		query := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE %s = $1`, quoteIdent(fk.Table), quoteIdent(fk.Column))
		require.NoError(t, pool.QueryRow(ctx, query, target.ID).Scan(&preCount))
		require.Greater(t, preCount, 0, "no seed row exists yet for %s.%s — the post-delete count==0 assertion below would be vacuous; add seed data above", fk.Table, fk.Column)
	}

	// --- Run the cascade. ---
	deleted, err := repo.DeleteUserCascade(ctx, target.ID, target.DeletionRequestedAt)
	require.NoError(t, err)
	require.True(t, deleted, "sanity check: an ordinary eligible target with no blockers must be deleted")

	assertUserRowGone(t, ctx, pool, target.ID)
	assertUserRowExists(t, ctx, pool, otherA.ID)
	assertUserRowExists(t, ctx, pool, otherB.ID)

	// Iterate the LIVE (dynamically queried) FK list, not the hard-coded one above, so this loop
	// itself never needs editing when a table is added; only the seed data above and
	// expectedUserScopedFKs need to grow, and the ElementsMatch check above forces that to happen.
	for _, fk := range liveFKs {
		switch fk.DeleteRule {
		case "CASCADE":
			var count int
			query := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE %s = $1`, quoteIdent(fk.Table), quoteIdent(fk.Column))
			require.NoError(t, pool.QueryRow(ctx, query, target.ID).Scan(&count))
			assert.Equal(t, 0, count, "CASCADE FK %s.%s must have removed every row referencing the deleted user", fk.Table, fk.Column)
		case "SET NULL":
			// Handled individually below — once nulled, the column no longer equals target.ID,
			// so each SET NULL row needs its own stable (non-FK) identifier to look it up by.
		default:
			t.Fatalf("unexpected delete rule %q for %s.%s — this test only knows how to verify CASCADE and SET NULL", fk.DeleteRule, fk.Table, fk.Column)
		}
	}

	var auditLogUserID *string
	require.NoError(t, pool.QueryRow(ctx, "SELECT user_id FROM audit_logs WHERE id = $1", auditLogRowID).Scan(&auditLogUserID))
	assert.Nil(t, auditLogUserID, "audit_logs.user_id SET NULL FK must have fired")

	var acceptedUserID *string
	require.NoError(t, pool.QueryRow(ctx, "SELECT accepted_user_id FROM household_invites WHERE id = $1", acceptedInviteID).Scan(&acceptedUserID))
	assert.Nil(t, acceptedUserID, "household_invites.accepted_user_id SET NULL FK must have fired")

	var acceptedInviteCount int
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM household_invites WHERE id = $1", acceptedInviteID).Scan(&acceptedInviteCount))
	assert.Equal(t, 1, acceptedInviteCount, "the SET NULL row itself must survive — only the column is nulled")

	// budget_period_events has NO direct FK to users (user_id is TEXT) — it is covered only
	// transitively, via its own budget_id CASCADE to budgets. Prove that chain actually holds.
	var budgetPeriodEventCount int
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM budget_period_events WHERE budget_id = $1", budgetID).Scan(&budgetPeriodEventCount))
	assert.Equal(t, 0, budgetPeriodEventCount, "budget_period_events must be cleaned transitively via budgets' own CASCADE, even without its own FK to users")

	var budgetCategoryCount int
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM budget_categories WHERE id = $1", budgetCategoryID).Scan(&budgetCategoryCount))
	assert.Equal(t, 0, budgetCategoryCount, "budget_categories must cascade away transitively via budgets")
}
