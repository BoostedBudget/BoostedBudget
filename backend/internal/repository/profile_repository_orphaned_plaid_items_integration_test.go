//go:build integration

package repository

// Integration tests for Issue #1494 — the terminal-failure escape from DeleteUserCascade's C2 guard,
// and the record that makes taking it legitimate.
//
// Three facts are under test here and NONE of them can be tested with pgxmock, which matches query
// text and cannot evaluate a predicate against real rows or observe what a rolled-back transaction
// did or did not leave behind:
//
//  1. A row marked plaid_removal_failed_at stops blocking the cascade; a row marked neither still
//     blocks (fail-closed, unchanged from #1495).
//  2. Every row whose Plaid removal was not PROVED is copied into plaid_orphaned_items before the
//     delete, with values an operator can act on — and a row that WAS proved removed is not copied,
//     because nothing is owed for it.
//  3. If that copy cannot happen, the whole cascade aborts. This is the ordering pin: the record must
//     exist before the erasure completes, or #1494's escape is a silent leak of a live, billing Item.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestDeleteUserCascade_Integration_Orphan -tags integration -p 1 -v

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

// orphanRecord is one plaid_orphaned_items row, read back with its ciphertext columns already
// decrypted through the same FieldEncryptor the cascade's INSERT ... SELECT copied them under. Reading
// it this way is itself part of the test: it proves the copy preserved values that remain decryptable,
// rather than, say, double-encrypting them or copying a blind index.
type orphanRecord struct {
	LinkedItemID    string
	PlaidItemID     string
	InstitutionID   string
	InstitutionName string
	Status          *string
	LastErrorCode   *string
	LastAttemptedAt *time.Time
	OrphanedAt      time.Time
}

// readOrphanRecords returns every plaid_orphaned_items row for the given linked_items ids.
func readOrphanRecords(t *testing.T, ctx context.Context, pool *pgxpool.Pool, enc decryptor, linkedItemIDs ...string) []orphanRecord {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT linked_item_id, plaid_item_id_encrypted, institution_id_encrypted,
		       institution_name_encrypted, linked_item_status, last_error_code,
		       last_attempted_at, orphaned_at
		FROM plaid_orphaned_items
		WHERE linked_item_id = ANY($1)
		ORDER BY orphaned_at`, linkedItemIDs)
	require.NoError(t, err)
	defer rows.Close()

	var out []orphanRecord
	for rows.Next() {
		var rec orphanRecord
		var instID, instName *string
		require.NoError(t, rows.Scan(&rec.LinkedItemID, &rec.PlaidItemID, &instID, &instName,
			&rec.Status, &rec.LastErrorCode, &rec.LastAttemptedAt, &rec.OrphanedAt))
		plainItemID, err := enc.DecryptString(rec.PlaidItemID)
		require.NoError(t, err, "the copied plaid_item_id must still decrypt — the cascade copies ciphertext verbatim")
		rec.PlaidItemID = plainItemID
		if instID != nil {
			rec.InstitutionID, err = enc.DecryptString(*instID)
			require.NoError(t, err)
		}
		if instName != nil {
			rec.InstitutionName, err = enc.DecryptString(*instName)
			require.NoError(t, err)
		}
		out = append(out, rec)
	}
	require.NoError(t, rows.Err())
	return out
}

// decryptor is the one FieldEncryptor capability readOrphanRecords needs.
type decryptor interface {
	DecryptString(ciphertext string) (string, error)
}

// TestDeleteUserCascade_Integration_OrphanTerminallyFailedItemUnblocksAndIsPreserved is #1494's
// headline case, and both halves have to hold together: the erasure completes (it no longer waits
// forever on an Item we can never remove) AND the Item survives as a record we can still act on.
// Either half alone is a defect — the first without the second is the silent leak #1495 closed.
func TestDeleteUserCascade_Integration_OrphanTerminallyFailedItemUnblocksAndIsPreserved(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, nil, nil, nil)

	item := &domain.LinkedItem{
		UserID:          user.ID,
		ItemID:          "plaid-item-" + suffix,
		AccessToken:     "wrong-environment-token-" + suffix,
		InstitutionID:   "ins_" + suffix,
		InstitutionName: "First Unremovable Bank",
		Provider:        "plaid",
	}
	require.NoError(t, linkedItemRepo.Create(ctx, item))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM linked_items WHERE id = $1", item.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM plaid_orphaned_items WHERE linked_item_id = $1", item.ID)
	})

	// The state a real INVALID_ACCESS_TOKEN failure leaves behind — via the production writer, not a
	// hand-written UPDATE, so this test also covers MarkPlaidRemovalFailed against real Postgres.
	require.NoError(t, linkedItemRepo.MarkPlaidRemovalFailed(ctx, []string{item.ID}, "INVALID_ACCESS_TOKEN"))

	// Before the cascade there is nothing to act on yet — the fact lives on the linked_items row.
	require.Empty(t, readOrphanRecords(t, ctx, pool, enc, item.ID),
		"the orphan record is written by the cascade, not by the failure — while the row lives, the row IS the record")

	deleted, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err)
	assert.True(t, deleted,
		"#1494: an Item whose removal can never succeed must no longer block this user's GDPR erasure forever")
	assertUserRowGone(t, ctx, pool, user.ID)

	var itemsLeft int
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM linked_items WHERE id = $1", item.ID).Scan(&itemsLeft))
	require.Equal(t, 0, itemsLeft, "the cascade deletes the linked_items row — which is why the copy below must have happened first")

	records := readOrphanRecords(t, ctx, pool, enc, item.ID)
	require.Len(t, records, 1, "the surviving record is the whole reason unblocking the erasure is not a leak")
	rec := records[0]
	assert.Equal(t, item.ID, rec.LinkedItemID)
	assert.Equal(t, "plaid-item-"+suffix, rec.PlaidItemID,
		"the Plaid Item id is the one value an operator needs to remove this Item at Plaid")
	assert.Equal(t, "ins_"+suffix, rec.InstitutionID)
	assert.Equal(t, "First Unremovable Bank", rec.InstitutionName)
	require.NotNil(t, rec.LastErrorCode)
	assert.Equal(t, "INVALID_ACCESS_TOKEN", *rec.LastErrorCode,
		"the code that justified calling this hopeless must survive, so a too-wide classification stays auditable")
	assert.NotNil(t, rec.LastAttemptedAt, "when we last tried is what tells an operator this was tried at all")
	assert.WithinDuration(t, time.Now(), rec.OrphanedAt, time.Minute)
	require.NotNil(t, rec.Status)
	assert.Equal(t, string(domain.LinkedItemStatusActive), *rec.Status)

	// The record must NOT be a surviving pointer to the erased person. Asserted against the live
	// schema rather than against the migration text, so a later ALTER that adds one fails here.
	var userColumns int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_name = 'plaid_orphaned_items' AND column_name IN ('user_id', 'clerk_user_id', 'email')
	`).Scan(&userColumns))
	assert.Equal(t, 0, userColumns,
		"plaid_orphaned_items outlives an erased user and must carry no identifier for them (migration 000134)")
}

// TestDeleteUserCascade_Integration_OrphanUnresolvedItemStillBlocksAndWritesNothing is the falsifying
// counterpart: #1494's escape must be reachable ONLY through a recorded terminal verdict. An item that
// merely has not been dealt with yet — the overwhelmingly common case, and the one an outage produces —
// must still block, exactly as #1495 left it, and must NOT leave an orphan record behind (which would
// tell an operator to chase an Item that is still being retried automatically).
func TestDeleteUserCascade_Integration_OrphanUnresolvedItemStillBlocksAndWritesNothing(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, nil, nil, nil)

	item := &domain.LinkedItem{
		UserID:      user.ID,
		ItemID:      "plaid-item-unresolved-" + suffix,
		AccessToken: "perfectly-good-token-" + suffix,
		Provider:    "plaid",
	}
	require.NoError(t, linkedItemRepo.Create(ctx, item))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM linked_items WHERE id = $1", item.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM plaid_orphaned_items WHERE linked_item_id = $1", item.ID)
	})

	deleted, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err, "an aborted cascade is a normal outcome, not an error")
	assert.False(t, deleted, "neither fact recorded means work is still owed and still possible — keep blocking")
	assertUserRowExists(t, ctx, pool, user.ID)
	assert.Empty(t, readOrphanRecords(t, ctx, pool, enc, item.ID),
		"an aborted cascade must leave no orphan record: the linked_items row still exists and is still being retried")
}

// TestDeleteUserCascade_Integration_OrphanProvedRemovedItemWritesNothing pins the other boundary of
// the copy's scope. A row with plaid_removed_at is settled — Plaid billing has stopped and nothing is
// owed — so copying it would fill the operator's work list with items that need no work, which is the
// fastest way to make the list ignored.
func TestDeleteUserCascade_Integration_OrphanProvedRemovedItemWritesNothing(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, nil, nil, nil)

	item := &domain.LinkedItem{
		UserID:      user.ID,
		ItemID:      "plaid-item-removed-" + suffix,
		AccessToken: "token-" + suffix,
		Provider:    "plaid",
	}
	require.NoError(t, linkedItemRepo.Create(ctx, item))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM linked_items WHERE id = $1", item.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM plaid_orphaned_items WHERE linked_item_id = $1", item.ID)
	})
	require.NoError(t, linkedItemRepo.MarkPlaidRemoved(ctx, []string{item.ID}))

	deleted, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err)
	require.True(t, deleted, "a proved-removed item has never blocked the cascade and must not start now")
	assert.Empty(t, readOrphanRecords(t, ctx, pool, enc, item.ID),
		"nothing is owed for an Item Plaid confirmed removed — recording it as an orphan would be false")
}

// TestDeleteUserCascade_Integration_OrphanRecordInsertFailure_AbortsTheWholeCascade is this batch's
// ordering pin, and the one the C2 guard's comment names.
//
// The property: the erasure may complete ONLY if the record exists. Proving it needs the INSERT to
// fail for a reason the cascade cannot foresee, so the test installs a BEFORE INSERT trigger on
// plaid_orphaned_items that raises. Committed DDL (the cascade opens its own transaction from the
// pool and cannot see an uncommitted schema change), dropped again in t.Cleanup — which runs even if
// the assertions fail, so a failure here cannot leave the trigger behind for the rest of the suite.
//
// Without atomicity, the state after this test would be: users row gone, linked_items row gone, no
// orphan record, and a live Plaid Item nothing in the system knows about. That is the fifth instance
// of the failure shape this branch has closed four times, and the reason the INSERT lives inside the
// cascade's transaction rather than beside it.
func TestDeleteUserCascade_Integration_OrphanRecordInsertFailure_AbortsTheWholeCascade(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, nil, nil, nil)

	item := &domain.LinkedItem{
		UserID:      user.ID,
		ItemID:      "plaid-item-atomic-" + suffix,
		AccessToken: "bad-token-" + suffix,
		Provider:    "plaid",
	}
	require.NoError(t, linkedItemRepo.Create(ctx, item))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM linked_items WHERE id = $1", item.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM plaid_orphaned_items WHERE linked_item_id = $1", item.ID)
	})
	require.NoError(t, linkedItemRepo.MarkPlaidRemovalFailed(ctx, []string{item.ID}, "INVALID_ACCESS_TOKEN"))

	_, err := pool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION x2_block_orphan_insert() RETURNS trigger AS $$
		BEGIN
			RAISE EXCEPTION 'simulated failure writing the orphan record';
		END;
		$$ LANGUAGE plpgsql`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		CREATE TRIGGER x2_block_orphan_insert_trg BEFORE INSERT ON plaid_orphaned_items
		FOR EACH ROW EXECUTE FUNCTION x2_block_orphan_insert()`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DROP TRIGGER IF EXISTS x2_block_orphan_insert_trg ON plaid_orphaned_items")
		_, _ = pool.Exec(ctx, "DROP FUNCTION IF EXISTS x2_block_orphan_insert()")
	})

	deleted, cascadeErr := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.Error(t, cascadeErr,
		"a failure to preserve the obligation must be a real error, not a warning the cascade continues past")
	assert.Contains(t, cascadeErr.Error(), "preserve orphaned Plaid item records")
	assert.False(t, deleted)

	assertUserRowExists(t, ctx, pool, user.ID)
	var itemsLeft int
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM linked_items WHERE id = $1", item.ID).Scan(&itemsLeft))
	assert.Equal(t, 1, itemsLeft,
		"the linked_items row must survive: while it exists the Item is still findable and still being retried")
}

// TestCountUncleanedLinkedItems_Integration_CountsExactlyWhatTheGuardBlocksOn covers the count
// DeletionService's Plaid-less branch reports to an operator. It shares the guard's predicate in
// source, so the value of this test is proving that predicate means what both callers claim against
// real rows — in particular that a terminally-failed row is NOT counted, since counting it would
// reintroduce the permanent refusal #1494 removes.
func TestCountUncleanedLinkedItems_Integration_CountsExactlyWhatTheGuardBlocksOn(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, nil, nil, nil)

	newItem := func(label string) *domain.LinkedItem {
		item := &domain.LinkedItem{
			UserID:      user.ID,
			ItemID:      "plaid-item-" + label + "-" + suffix,
			AccessToken: "token-" + label + "-" + suffix,
			Provider:    "plaid",
		}
		require.NoError(t, linkedItemRepo.Create(ctx, item))
		t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM linked_items WHERE id = $1", item.ID) })
		return item
	}

	outstanding := newItem("outstanding")
	removed := newItem("removed")
	terminal := newItem("terminal")
	require.NoError(t, linkedItemRepo.MarkPlaidRemoved(ctx, []string{removed.ID}))
	require.NoError(t, linkedItemRepo.MarkPlaidRemovalFailed(ctx, []string{terminal.ID}, "NO_USABLE_ACCESS_TOKEN"))

	count, err := repo.CountUncleanedLinkedItems(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, count,
		"only the row with neither fact recorded is outstanding: %s counts, %s is proved removed, %s is proved unremovable",
		outstanding.ID, removed.ID, terminal.ID)

	// And the guard agrees, on the same rows, in the same database — the two must never diverge.
	var blocked bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT `+uncleanedLinkedItemExistsClause, user.ID).Scan(&blocked))
	assert.True(t, blocked)

	require.NoError(t, linkedItemRepo.MarkPlaidRemovalFailed(ctx, []string{outstanding.ID}, "INVALID_ACCESS_TOKEN"))
	count, err = repo.CountUncleanedLinkedItems(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	require.NoError(t, pool.QueryRow(ctx, `SELECT `+uncleanedLinkedItemExistsClause, user.ID).Scan(&blocked))
	assert.False(t, blocked, "once every row carries one of the two facts, the guard must let the erasure through")
}
