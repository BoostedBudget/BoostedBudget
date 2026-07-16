// backend/internal/repository/household_repository_integration_test.go
package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// insertTestHouseholdMember inserts a household_members row and returns its UUID.
func insertTestHouseholdMember(t *testing.T, pool *pgxpool.Pool, householdID, userID, role string) string {
	t.Helper()
	ctx := context.Background()
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO household_members (household_id, user_id, role, status)
		VALUES ($1, $2, $3, 'active')
		RETURNING id::text
	`, householdID, userID, role).Scan(&id)
	if err != nil {
		t.Fatalf("insertTestHouseholdMember: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM household_members WHERE id = $1`, id) //nolint:errcheck
	})
	return id
}

// insertTestInvite inserts a household_invites row with the given status/expiry and returns its UUID.
func insertTestInvite(t *testing.T, pool *pgxpool.Pool, householdID, inviterID string, expiresAt time.Time) string {
	t.Helper()
	ctx := context.Background()
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO household_invites (household_id, inviter_user_id, invitee_email, invitee_email_blind_idx, token_hash, status, expires_at)
		VALUES ($1, $2, 'enc_test', 'blind_test_'||gen_random_uuid()::text, 'token_'||gen_random_uuid()::text, 'pending', $3)
		RETURNING id::text
	`, householdID, inviterID, expiresAt).Scan(&id)
	if err != nil {
		t.Fatalf("insertTestInvite: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM household_invites WHERE id = $1`, id) //nolint:errcheck
	})
	return id
}

func TestHouseholdRepository_GetHouseholdForUser(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdRepository(pool)
	ownerID := insertTestUser(t, pool)
	memberID := insertTestUser(t, pool)
	strangerID := insertTestUser(t, pool)
	householdID := insertTestHousehold(t, pool, ownerID)
	insertTestHouseholdMember(t, pool, householdID, ownerID, "owner")
	insertTestHouseholdMember(t, pool, householdID, memberID, "member")

	h, isOwner, found, err := repo.GetHouseholdForUser(ctx, ownerID)
	if err != nil || !found || !isOwner || h.ID != householdID {
		t.Fatalf("owner lookup: h=%+v isOwner=%v found=%v err=%v", h, isOwner, found, err)
	}

	h, isOwner, found, err = repo.GetHouseholdForUser(ctx, memberID)
	if err != nil || !found || isOwner || h.ID != householdID {
		t.Fatalf("member lookup: h=%+v isOwner=%v found=%v err=%v", h, isOwner, found, err)
	}

	_, _, found, err = repo.GetHouseholdForUser(ctx, strangerID)
	if err != nil || found {
		t.Fatalf("stranger lookup should be not-found: found=%v err=%v", found, err)
	}
}

// TestHouseholdRepository_GetHouseholdForUser_OverCapacityFields proves the new
// over-capacity columns (added by migration 000113) are actually projected and
// scanned onto the returned struct, not just present in the schema. #1371 Task 1.
func TestHouseholdRepository_GetHouseholdForUser_OverCapacityFields(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdRepository(pool)
	ownerID := insertTestUser(t, pool)
	householdID := insertTestHousehold(t, pool, ownerID)
	insertTestHouseholdMember(t, pool, householdID, ownerID, "owner")

	h, _, found, err := repo.GetHouseholdForUser(ctx, ownerID)
	if err != nil || !found {
		t.Fatalf("initial lookup: found=%v err=%v", found, err)
	}
	if h.OverCapacitySince != nil || h.OverCapacityStartNoticeSentAt != nil || h.OverCapacityWarn3dSentAt != nil {
		t.Fatalf("expected all over-capacity fields nil by default, got: %+v", h)
	}

	since := time.Now().Add(-2 * 24 * time.Hour).Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx, `UPDATE households SET over_capacity_since = $1 WHERE id = $2`, since, householdID); err != nil {
		t.Fatalf("set over_capacity_since: %v", err)
	}

	h, _, found, err = repo.GetHouseholdForUser(ctx, ownerID)
	if err != nil || !found {
		t.Fatalf("second lookup: found=%v err=%v", found, err)
	}
	if h.OverCapacitySince == nil || !h.OverCapacitySince.Equal(since) {
		t.Fatalf("expected over_capacity_since=%v, got %v", since, h.OverCapacitySince)
	}
	if h.OverCapacityStartNoticeSentAt != nil || h.OverCapacityWarn3dSentAt != nil {
		t.Fatalf("expected notice-sent fields to remain nil, got: %+v", h)
	}
}

func TestHouseholdRepository_CountActiveMembers(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdRepository(pool)
	ownerID := insertTestUser(t, pool)
	memberID := insertTestUser(t, pool)
	householdID := insertTestHousehold(t, pool, ownerID)
	insertTestHouseholdMember(t, pool, householdID, ownerID, "owner")
	insertTestHouseholdMember(t, pool, householdID, memberID, "member")

	count, err := repo.CountActiveMembers(ctx, householdID)
	if err != nil || count != 2 {
		t.Fatalf("expected 2 active members, got %d (err=%v)", count, err)
	}
}

func TestHouseholdRepository_CountPendingInvites_ExcludesExpired(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdRepository(pool)
	ownerID := insertTestUser(t, pool)
	householdID := insertTestHousehold(t, pool, ownerID)
	insertTestInvite(t, pool, householdID, ownerID, time.Now().Add(24*time.Hour))  // valid
	insertTestInvite(t, pool, householdID, ownerID, time.Now().Add(-24*time.Hour)) // expired, unswept

	count, err := repo.CountPendingInvites(ctx, householdID)
	if err != nil || count != 1 {
		t.Fatalf("expected 1 non-expired pending invite, got %d (err=%v)", count, err)
	}
}

// TestHouseholdRepository_DeleteRemovedMember_Integration proves the safety-critical
// guard on DeleteRemovedMember (`WHERE id = $1 AND status = 'removed'`) actually holds
// against a real schema: it must delete a genuinely-removed row, but must NEVER delete
// (or otherwise affect) an active-status row for the same household — pgxmock's regex
// match on the query text alone cannot prove the WHERE clause's status predicate is
// doing real work (#1371 Task 0 follow-up, coordinator review).
func TestHouseholdRepository_DeleteRemovedMember_Integration(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdRepository(pool)

	ownerID := insertTestUser(t, pool)
	activeMemberID := insertTestUser(t, pool)
	removedMemberID := insertTestUser(t, pool)
	householdID := insertTestHousehold(t, pool, ownerID)
	insertTestHouseholdMember(t, pool, householdID, ownerID, "owner")
	activeRowID := insertTestHouseholdMember(t, pool, householdID, activeMemberID, "member")
	removedRowID := insertTestHouseholdMember(t, pool, householdID, removedMemberID, "member")

	// Transition the second row to 'removed', mirroring RemoveMember's production
	// active -> removed transition.
	if _, err := pool.Exec(ctx, `UPDATE household_members SET status = 'removed', removed_at = now() WHERE id = $1`, removedRowID); err != nil {
		t.Fatalf("transition to removed: %v", err)
	}

	// The guard must refuse to delete an ACTIVE row.
	if _, _, err := repo.DeleteRemovedMember(ctx, activeRowID); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Fatalf("expected ErrMemberNotFound when targeting an active row, got: %v", err)
	}
	var activeStillPresent int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM household_members WHERE id = $1 AND status = 'active'`, activeRowID).Scan(&activeStillPresent); err != nil {
		t.Fatalf("count active row: %v", err)
	}
	if activeStillPresent != 1 {
		t.Fatalf("guard failed: active row was affected (count=%d)", activeStillPresent)
	}

	// The guard must actually delete a genuinely-removed row, returning its household/user.
	gotHouseholdID, gotUserID, err := repo.DeleteRemovedMember(ctx, removedRowID)
	if err != nil {
		t.Fatalf("DeleteRemovedMember on a removed row: %v", err)
	}
	if gotHouseholdID != householdID || gotUserID != removedMemberID {
		t.Fatalf("unexpected (household_id, user_id): got (%s, %s), want (%s, %s)", gotHouseholdID, gotUserID, householdID, removedMemberID)
	}
	var removedRowCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM household_members WHERE id = $1`, removedRowID).Scan(&removedRowCount); err != nil {
		t.Fatalf("count removed row: %v", err)
	}
	if removedRowCount != 0 {
		t.Fatalf("removed row must be gone, got count=%d", removedRowCount)
	}

	// The unrelated active member's row must remain completely untouched.
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM household_members WHERE id = $1 AND status = 'active'`, activeRowID).Scan(&activeStillPresent); err != nil {
		t.Fatalf("count active row after: %v", err)
	}
	if activeStillPresent != 1 {
		t.Fatalf("unrelated active row was affected, count=%d", activeStillPresent)
	}
}

// ─── SetOverCapacity / ClearOverCapacity (#1371 — Task 1) ────────────────────

// TestHouseholdRepository_SetOverCapacity_Idempotent_Integration proves the
// idempotency guard (`COALESCE(over_capacity_since, $2)`) actually holds against a
// real schema: a second call with a later timestamp must NOT reset the original
// grace deadline — and must return that original, FIRST timestamp directly via
// RETURNING, so a caller can detect a lost race without a follow-up read. pgxmock
// can prove the query shape, but not that the predicate really protects the
// first-set value across two live calls.
func TestHouseholdRepository_SetOverCapacity_Idempotent_Integration(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdRepository(pool)
	ownerID := insertTestUser(t, pool)
	householdID := insertTestHousehold(t, pool, ownerID)
	insertTestHouseholdMember(t, pool, householdID, ownerID, "owner")

	first := time.Now().Add(-3 * 24 * time.Hour).Truncate(time.Microsecond)
	firstEffective, err := repo.SetOverCapacity(ctx, householdID, first)
	if err != nil {
		t.Fatalf("first SetOverCapacity: %v", err)
	}
	if !firstEffective.Equal(first) {
		t.Fatalf("first SetOverCapacity should return the value it just set: expected %v, got %v", first, firstEffective)
	}

	second := time.Now().Truncate(time.Microsecond)
	secondEffective, err := repo.SetOverCapacity(ctx, householdID, second)
	if err != nil {
		t.Fatalf("second SetOverCapacity (should be a no-op, not an error): %v", err)
	}
	// The core assertion: the SECOND call's return value must equal the FIRST
	// timestamp (not the second) — proving COALESCE correctly preserves and returns
	// the earliest-set value, directly, with no follow-up read required.
	if !secondEffective.Equal(first) {
		t.Fatalf("expected second SetOverCapacity to return the FIRST timestamp %v, got %v", first, secondEffective)
	}

	// Secondary check: confirm the persisted row itself agrees.
	h, _, found, err := repo.GetHouseholdForUser(ctx, ownerID)
	if err != nil || !found {
		t.Fatalf("lookup after two SetOverCapacity calls: found=%v err=%v", found, err)
	}
	if h.OverCapacitySince == nil || !h.OverCapacitySince.Equal(first) {
		t.Fatalf("expected over_capacity_since to remain the FIRST timestamp %v, got %v", first, h.OverCapacitySince)
	}
}

// TestHouseholdRepository_ClearOverCapacity_Integration proves ClearOverCapacity
// nulls all three over-capacity columns against a real schema.
func TestHouseholdRepository_ClearOverCapacity_Integration(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdRepository(pool)
	ownerID := insertTestUser(t, pool)
	householdID := insertTestHousehold(t, pool, ownerID)
	insertTestHouseholdMember(t, pool, householdID, ownerID, "owner")

	since := time.Now().Truncate(time.Microsecond)
	if _, err := repo.SetOverCapacity(ctx, householdID, since); err != nil {
		t.Fatalf("SetOverCapacity: %v", err)
	}
	if err := repo.MarkOverCapacityStartNoticeSent(ctx, householdID, since); err != nil {
		t.Fatalf("MarkOverCapacityStartNoticeSent: %v", err)
	}
	if err := repo.MarkOverCapacityWarn3dSent(ctx, householdID, since); err != nil {
		t.Fatalf("MarkOverCapacityWarn3dSent: %v", err)
	}

	// Sanity: all three columns are actually set before clearing.
	h, _, found, err := repo.GetHouseholdForUser(ctx, ownerID)
	if err != nil || !found {
		t.Fatalf("pre-clear lookup: found=%v err=%v", found, err)
	}
	if h.OverCapacitySince == nil || h.OverCapacityStartNoticeSentAt == nil || h.OverCapacityWarn3dSentAt == nil {
		t.Fatalf("expected all three over-capacity columns set before clearing, got: %+v", h)
	}

	if err := repo.ClearOverCapacity(ctx, householdID); err != nil {
		t.Fatalf("ClearOverCapacity: %v", err)
	}

	h, _, found, err = repo.GetHouseholdForUser(ctx, ownerID)
	if err != nil || !found {
		t.Fatalf("post-clear lookup: found=%v err=%v", found, err)
	}
	if h.OverCapacitySince != nil || h.OverCapacityStartNoticeSentAt != nil || h.OverCapacityWarn3dSentAt != nil {
		t.Fatalf("expected all three over-capacity columns nulled after ClearOverCapacity, got: %+v", h)
	}

	// Idempotent: calling it again (household not in grace) must not error.
	if err := repo.ClearOverCapacity(ctx, householdID); err != nil {
		t.Fatalf("second ClearOverCapacity (should be a no-op, not an error): %v", err)
	}
}

// TestHouseholdRepository_MarkOverCapacityNotices_NoOpWhenNotInGrace_Integration proves
// MarkOverCapacityStartNoticeSent/MarkOverCapacityWarn3dSent are guarded by
// `over_capacity_since IS NOT NULL`: once a household's grace has been cleared (e.g. by
// a concurrent read-only-owner clear racing the daily backstop worker), a stale notice
// stamp must NOT be written. Round 5 Gap Analysis fix (#1371) — pgxmock can prove the
// query shape, but not that the predicate really protects a live, already-cleared row.
func TestHouseholdRepository_MarkOverCapacityNotices_NoOpWhenNotInGrace_Integration(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdRepository(pool)
	ownerID := insertTestUser(t, pool)
	householdID := insertTestHousehold(t, pool, ownerID)
	insertTestHouseholdMember(t, pool, householdID, ownerID, "owner")

	since := time.Now().Truncate(time.Microsecond)

	// Sanity: while genuinely in grace, marking DOES stamp the notice.
	if _, err := repo.SetOverCapacity(ctx, householdID, since); err != nil {
		t.Fatalf("SetOverCapacity: %v", err)
	}
	if err := repo.MarkOverCapacityStartNoticeSent(ctx, householdID, since); err != nil {
		t.Fatalf("MarkOverCapacityStartNoticeSent (in grace): %v", err)
	}
	h, _, found, err := repo.GetHouseholdForUser(ctx, ownerID)
	if err != nil || !found {
		t.Fatalf("lookup after in-grace mark: found=%v err=%v", found, err)
	}
	if h.OverCapacityStartNoticeSentAt == nil {
		t.Fatalf("expected start-notice stamp to be set while in grace, got nil")
	}

	// Clear grace (simulating a concurrent read-only-owner clear), then attempt both
	// marks again — neither must write a stamp onto a household that is no longer flagged.
	if err := repo.ClearOverCapacity(ctx, householdID); err != nil {
		t.Fatalf("ClearOverCapacity: %v", err)
	}
	if err := repo.MarkOverCapacityStartNoticeSent(ctx, householdID, time.Now()); err != nil {
		t.Fatalf("MarkOverCapacityStartNoticeSent (not in grace, should be a no-op, not an error): %v", err)
	}
	if err := repo.MarkOverCapacityWarn3dSent(ctx, householdID, time.Now()); err != nil {
		t.Fatalf("MarkOverCapacityWarn3dSent (not in grace, should be a no-op, not an error): %v", err)
	}

	h, _, found, err = repo.GetHouseholdForUser(ctx, ownerID)
	if err != nil || !found {
		t.Fatalf("post-clear lookup: found=%v err=%v", found, err)
	}
	if h.OverCapacitySince != nil || h.OverCapacityStartNoticeSentAt != nil || h.OverCapacityWarn3dSentAt != nil {
		t.Fatalf("expected all three over-capacity columns to remain nil (marks must no-op when not in grace), got: %+v", h)
	}
}

// ─── ListHouseholdsWithMembers (#1371 — Task 1) ──────────────────────────────

// TestHouseholdRepository_ListHouseholdsWithMembers_Integration proves the EXISTS
// predicate against a real schema: a household with an active member is included,
// a household with only a removed member (or no members at all) is excluded —
// regardless of over-capacity state.
func TestHouseholdRepository_ListHouseholdsWithMembers_Integration(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdRepository(pool)

	// Household A: has an active (owner) member — must be included.
	ownerA := insertTestUser(t, pool)
	householdA := insertTestHousehold(t, pool, ownerA)
	insertTestHouseholdMember(t, pool, householdA, ownerA, "owner")

	// Household B: over-capacity, but its only member row has been removed —
	// must be EXCLUDED (no active member left).
	ownerB := insertTestUser(t, pool)
	householdB := insertTestHousehold(t, pool, ownerB)
	removedRowB := insertTestHouseholdMember(t, pool, householdB, ownerB, "owner")
	if _, err := pool.Exec(ctx, `UPDATE household_members SET status = 'removed', removed_at = now() WHERE id = $1`, removedRowB); err != nil {
		t.Fatalf("transition household B owner to removed: %v", err)
	}
	if _, err := repo.SetOverCapacity(ctx, householdB, time.Now()); err != nil {
		t.Fatalf("SetOverCapacity on household B: %v", err)
	}

	households, err := repo.ListHouseholdsWithMembers(ctx)
	if err != nil {
		t.Fatalf("ListHouseholdsWithMembers: %v", err)
	}

	found := map[string]bool{}
	for _, h := range households {
		found[h.ID] = true
	}
	if !found[householdA] {
		t.Fatalf("expected household A (has active member) to be included, got: %v", found)
	}
	if found[householdB] {
		t.Fatalf("expected household B (no active members, only removed) to be EXCLUDED, got: %v", found)
	}
}

// ─── RestoreMember guard (#1371 — Task 1) ────────────────────────────────────

// TestHouseholdRepository_RestoreMember_GuardHasTeeth_Integration proves the
// `WHERE status = 'removed'` guard on RestoreMember has real teeth against a live
// schema. Per the Task 0 review lesson (never weaken production code to force a
// RED run), this test does NOT touch RestoreMember's SQL. Instead it executes a
// raw UPDATE that simulates what a *broken* guard (one missing the status
// predicate) would do to an active row, and shows that outcome is NOT what
// RestoreMember itself produces when correctly guarded — i.e. RestoreMember only
// ever affects genuinely-removed rows, matching the DeleteRemovedMember pattern.
func TestHouseholdRepository_RestoreMember_GuardHasTeeth_Integration(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdRepository(pool)

	ownerID := insertTestUser(t, pool)
	activeMemberID := insertTestUser(t, pool)
	removedMemberID := insertTestUser(t, pool)
	householdID := insertTestHousehold(t, pool, ownerID)
	insertTestHouseholdMember(t, pool, householdID, ownerID, "owner")
	activeRowID := insertTestHouseholdMember(t, pool, householdID, activeMemberID, "member")
	removedRowID := insertTestHouseholdMember(t, pool, householdID, removedMemberID, "member")

	if _, err := pool.Exec(ctx, `UPDATE household_members SET status = 'removed', removed_at = now() WHERE id = $1`, removedRowID); err != nil {
		t.Fatalf("transition to removed: %v", err)
	}

	// The guard must refuse to affect an ACTIVE row: RestoreMember is a no-op
	// (returns ErrMemberNotFound) targeting the active row, and must not touch it.
	if _, err := repo.RestoreMember(ctx, activeRowID); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Fatalf("expected ErrMemberNotFound when targeting an active row, got: %v", err)
	}
	var activeStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM household_members WHERE id = $1`, activeRowID).Scan(&activeStatus); err != nil {
		t.Fatalf("query active row status: %v", err)
	}
	if activeStatus != "active" {
		t.Fatalf("guard failed: active row's status was changed to %q", activeStatus)
	}

	// Sanity check that a naive UPDATE lacking the guard WOULD have silently
	// "succeeded" against an active row — proving the guard is doing real work,
	// not merely reflecting a query that could never match anyway. This does not
	// touch production code; it directly simulates the broken variant.
	tag, err := pool.Exec(ctx, `UPDATE household_members SET removed_at = NULL WHERE id = $1 AND status = 'active'`, activeRowID)
	if err != nil {
		t.Fatalf("simulated broken-guard update: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("expected the naive predicate to match the active row (proving RestoreMember's stricter guard is necessary), got %d rows", tag.RowsAffected())
	}

	// The guard must actually restore a genuinely-removed row, and RETURN the
	// freshly-written updated_at (Codex review, #1371 Task 4) — verified against
	// real Postgres here since pgxmock can't catch a missing RETURNING clause.
	//
	// Freshness is checked with a generous tolerance window rather than a strict
	// beforeRestore.Before(restoredUpdatedAt) comparison: beforeRestore is captured on
	// the Go test process's wall clock, while restoredUpdatedAt comes from Postgres's
	// own now() — a different clock domain/process/container. A zero-tolerance
	// cross-process clock comparison is inherently racy under clock skew (this
	// failed in CI with a ~441µs mismatch despite passing locally — gap-analysis
	// round, #1371). The real freshness/correctness proof is the DB-re-read
	// comparison below (restoredUpdatedAt vs. dbUpdatedAt), which stays entirely
	// within Postgres's clock domain and has no such race; this tolerance window
	// only needs to catch a grossly wrong value (e.g. a stale/zero timestamp), not
	// prove microsecond-level freshness.
	const clockSkewTolerance = 5 * time.Second
	beforeRestore := time.Now()
	restoredUpdatedAt, err := repo.RestoreMember(ctx, removedRowID)
	if err != nil {
		t.Fatalf("RestoreMember on a removed row: %v", err)
	}
	if restoredUpdatedAt.Before(beforeRestore.Add(-clockSkewTolerance)) {
		t.Fatalf("expected RestoreMember to return a freshly-written updated_at (within %v of %v), got %v", clockSkewTolerance, beforeRestore, restoredUpdatedAt)
	}
	var restoredStatus string
	var restoredRemovedAt *time.Time
	var dbUpdatedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT status, removed_at, updated_at FROM household_members WHERE id = $1`, removedRowID).Scan(&restoredStatus, &restoredRemovedAt, &dbUpdatedAt); err != nil {
		t.Fatalf("query restored row: %v", err)
	}
	if restoredStatus != "active" {
		t.Fatalf("expected restored row status='active', got %q", restoredStatus)
	}
	if restoredRemovedAt != nil {
		t.Fatalf("expected restored row removed_at=NULL, got %v", restoredRemovedAt)
	}
	if !restoredUpdatedAt.Equal(dbUpdatedAt) {
		t.Fatalf("RestoreMember's returned updated_at (%v) must match the row actually persisted (%v)", restoredUpdatedAt, dbUpdatedAt)
	}

	// Calling RestoreMember again on the now-active row must be a no-op error
	// (ErrMemberNotFound), never a silent double-restore.
	if _, err := repo.RestoreMember(ctx, removedRowID); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Fatalf("expected ErrMemberNotFound restoring an already-active row, got: %v", err)
	}
}

// TestHouseholdRepository_GetRemovedMemberForUpdate_PicksMostRecent_Integration proves
// the ORDER BY removed_at DESC, created_at DESC really disambiguates against a real
// schema when a (household_id, user_id) pair has multiple historical removed rows —
// a scenario the partial unique active-user index does not prevent (Codex review,
// #1371 Task 1).
func TestHouseholdRepository_GetRemovedMemberForUpdate_PicksMostRecent_Integration(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdRepository(pool)

	ownerID := insertTestUser(t, pool)
	memberID := insertTestUser(t, pool)
	householdID := insertTestHousehold(t, pool, ownerID)
	insertTestHouseholdMember(t, pool, householdID, ownerID, "owner")

	// First membership epoch: joins, then removed.
	oldRowID := insertTestHouseholdMember(t, pool, householdID, memberID, "member")
	if _, err := pool.Exec(ctx, `UPDATE household_members SET status = 'removed', removed_at = now() - interval '10 days' WHERE id = $1`, oldRowID); err != nil {
		t.Fatalf("transition old row to removed: %v", err)
	}

	// Second membership epoch for the SAME (household, user): rejoins under a new
	// row, then removed again more recently. The partial unique active-user index
	// only constrains 'active' rows, so both removed rows coexist.
	newRowID := insertTestHouseholdMember(t, pool, householdID, memberID, "member")
	if _, err := pool.Exec(ctx, `UPDATE household_members SET status = 'removed', removed_at = now() WHERE id = $1`, newRowID); err != nil {
		t.Fatalf("transition new row to removed: %v", err)
	}

	m, err := repo.GetRemovedMemberForUpdate(ctx, householdID, memberID, 30)
	if err != nil {
		t.Fatalf("GetRemovedMemberForUpdate: %v", err)
	}
	if m.ID != newRowID {
		t.Fatalf("expected the MOST RECENTLY removed row %q, got %q (old row was %q)", newRowID, m.ID, oldRowID)
	}
}

// TestHouseholdRepository_GetRemovedMemberForUpdate_WithinWindow_Integration proves the
// withinDays window enforcement (Gap Analysis Round 3, GT-3) does NOT reject a row
// removed well within the window — a row removed 29 days ago must still be found and
// restorable, exactly as before this fix.
func TestHouseholdRepository_GetRemovedMemberForUpdate_WithinWindow_Integration(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdRepository(pool)

	ownerID := insertTestUser(t, pool)
	memberID := insertTestUser(t, pool)
	householdID := insertTestHousehold(t, pool, ownerID)
	insertTestHouseholdMember(t, pool, householdID, ownerID, "owner")

	rowID := insertTestHouseholdMember(t, pool, householdID, memberID, "member")
	if _, err := pool.Exec(ctx, `UPDATE household_members SET status = 'removed', removed_at = now() - interval '29 days' WHERE id = $1`, rowID); err != nil {
		t.Fatalf("transition row to removed 29 days ago: %v", err)
	}

	m, err := repo.GetRemovedMemberForUpdate(ctx, householdID, memberID, 30)
	if err != nil {
		t.Fatalf("expected a row removed 29 days ago to be found within a 30-day window, got: %v", err)
	}
	if m.ID != rowID {
		t.Fatalf("expected row %q, got %q", rowID, m.ID)
	}
}

// TestHouseholdRepository_GetRemovedMemberForUpdate_OutsideWindow_Integration proves the
// Gap Analysis Round 3, GT-3 fix against a real schema: GetRemovedMemberForUpdate's own
// doc comment promises a 30-day restore window, but before this fix the query never
// filtered on removed_at age at all. A row removed 31 days ago must now be treated
// exactly like "no removed row exists" (ErrMemberNotFound), not silently returned as
// restorable — closing the gap that would otherwise let a re-invite restore an
// arbitrarily stale removal if the Task 2 purge worker were ever delayed or paused.
func TestHouseholdRepository_GetRemovedMemberForUpdate_OutsideWindow_Integration(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdRepository(pool)

	ownerID := insertTestUser(t, pool)
	memberID := insertTestUser(t, pool)
	householdID := insertTestHousehold(t, pool, ownerID)
	insertTestHouseholdMember(t, pool, householdID, ownerID, "owner")

	rowID := insertTestHouseholdMember(t, pool, householdID, memberID, "member")
	if _, err := pool.Exec(ctx, `UPDATE household_members SET status = 'removed', removed_at = now() - interval '31 days' WHERE id = $1`, rowID); err != nil {
		t.Fatalf("transition row to removed 31 days ago: %v", err)
	}

	_, err := repo.GetRemovedMemberForUpdate(ctx, householdID, memberID, 30)
	if !errors.Is(err, domain.ErrMemberNotFound) {
		t.Fatalf("expected ErrMemberNotFound for a row removed outside the 30-day window, got: %v", err)
	}
}

// TestHouseholdRepository_RestoreMember_ConflictWithActiveElsewhere_Integration proves
// RestoreMember maps the real uq_household_members_active_user constraint violation to
// domain.ErrAlreadyInHousehold against a live schema, rather than leaking a raw
// pgconn.PgError, when the user has since become active in a different household
// (Codex review, #1371 Task 1).
func TestHouseholdRepository_RestoreMember_ConflictWithActiveElsewhere_Integration(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdRepository(pool)

	ownerA := insertTestUser(t, pool)
	memberID := insertTestUser(t, pool)
	householdA := insertTestHousehold(t, pool, ownerA)
	insertTestHouseholdMember(t, pool, householdA, ownerA, "owner")

	removedRowID := insertTestHouseholdMember(t, pool, householdA, memberID, "member")
	if _, err := pool.Exec(ctx, `UPDATE household_members SET status = 'removed', removed_at = now() WHERE id = $1`, removedRowID); err != nil {
		t.Fatalf("transition to removed: %v", err)
	}

	// The same user joins a SECOND household and is currently active there.
	ownerB := insertTestUser(t, pool)
	householdB := insertTestHousehold(t, pool, ownerB)
	insertTestHouseholdMember(t, pool, householdB, ownerB, "owner")
	insertTestHouseholdMember(t, pool, householdB, memberID, "member")

	if _, err := repo.RestoreMember(ctx, removedRowID); !errors.Is(err, domain.ErrAlreadyInHousehold) {
		t.Fatalf("expected ErrAlreadyInHousehold restoring a member active elsewhere, got: %v", err)
	}

	// The old row must remain untouched (still removed) — the failed restore
	// must not have partially applied.
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM household_members WHERE id = $1`, removedRowID).Scan(&status); err != nil {
		t.Fatalf("query old row status: %v", err)
	}
	if status != "removed" {
		t.Fatalf("expected old row to remain 'removed' after failed restore, got %q", status)
	}
}

// ─── ListMembershipsPastPurgeWindow (#1371 — Task 2) ─────────────────────────

// TestHouseholdRepository_ListMembershipsPastPurgeWindow_Integration proves the
// `removed_at < now() - $1::interval` predicate actually discriminates against a real
// schema: a row removed well past the 30-day window must be returned, but a row removed
// just inside the window must NOT — pgxmock's regex match on the query text alone
// cannot prove the interval comparison is doing real work (#1371 Task 2, mirroring the
// Task 0/1 review lesson for DeleteRemovedMember/SetOverCapacity).
func TestHouseholdRepository_ListMembershipsPastPurgeWindow_Integration(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdRepository(pool)

	ownerID := insertTestUser(t, pool)
	pastWindowMemberID := insertTestUser(t, pool)
	withinWindowMemberID := insertTestUser(t, pool)
	activeMemberID := insertTestUser(t, pool)
	householdID := insertTestHousehold(t, pool, ownerID)
	insertTestHouseholdMember(t, pool, householdID, ownerID, "owner")

	pastWindowRowID := insertTestHouseholdMember(t, pool, householdID, pastWindowMemberID, "member")
	if _, err := pool.Exec(ctx, `UPDATE household_members SET status = 'removed', removed_at = now() - interval '31 days' WHERE id = $1`, pastWindowRowID); err != nil {
		t.Fatalf("transition past-window row to removed: %v", err)
	}

	withinWindowRowID := insertTestHouseholdMember(t, pool, householdID, withinWindowMemberID, "member")
	if _, err := pool.Exec(ctx, `UPDATE household_members SET status = 'removed', removed_at = now() - interval '29 days' WHERE id = $1`, withinWindowRowID); err != nil {
		t.Fatalf("transition within-window row to removed: %v", err)
	}

	// An ACTIVE row must never be returned regardless of age.
	activeRowID := insertTestHouseholdMember(t, pool, householdID, activeMemberID, "member")

	candidates, err := repo.ListMembershipsPastPurgeWindow(ctx, 30)
	if err != nil {
		t.Fatalf("ListMembershipsPastPurgeWindow: %v", err)
	}

	var gotIDs []string
	for _, c := range candidates {
		gotIDs = append(gotIDs, c.ID)
	}
	if !containsID(gotIDs, pastWindowRowID) {
		t.Fatalf("expected past-window row %q in results, got %v", pastWindowRowID, gotIDs)
	}
	if containsID(gotIDs, withinWindowRowID) {
		t.Fatalf("within-window row %q must NOT be returned, got %v", withinWindowRowID, gotIDs)
	}
	if containsID(gotIDs, activeRowID) {
		t.Fatalf("active row %q must NOT be returned, got %v", activeRowID, gotIDs)
	}
}

func containsID(ids []string, target string) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}
