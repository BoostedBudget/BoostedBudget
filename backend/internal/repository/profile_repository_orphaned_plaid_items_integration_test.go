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
	TerminalSince   *time.Time
	OrphanedAt      time.Time
}

// terminalVerdictAgeBeyondCoolingOff is how far into the past a test pushes
// linked_items.plaid_removal_failed_at when it wants a verdict that has already OUTLIVED C2's 7-day
// cooling-off window (uncleanedLinkedItemPredicate). Written as a raw UPDATE because no production code
// may backdate a verdict — the whole value of the window is that only elapsed time can satisfy it.
const terminalVerdictAgeBeyondCoolingOff = 8 * 24 * time.Hour

// ageTerminalVerdict backdates an item's plaid_removal_failed_at by d, simulating a verdict that has
// persisted for that long while daily retries kept answering the same way.
func ageTerminalVerdict(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID string, d time.Duration) {
	t.Helper()
	tag, err := pool.Exec(ctx,
		`UPDATE linked_items SET plaid_removal_failed_at = plaid_removal_failed_at - $2::interval WHERE id = $1`,
		itemID, fmt.Sprintf("%d seconds", int(d.Seconds())))
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected(), "the fixture row must exist and already carry a verdict")
}

// readOrphanRecords returns every plaid_orphaned_items row for the given linked_items ids.
func readOrphanRecords(t *testing.T, ctx context.Context, pool *pgxpool.Pool, enc decryptor, linkedItemIDs ...string) []orphanRecord {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT linked_item_id, plaid_item_id_encrypted, institution_id_encrypted,
		       institution_name_encrypted, linked_item_status, last_error_code,
		       terminal_since, orphaned_at
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
			&rec.Status, &rec.LastErrorCode, &rec.TerminalSince, &rec.OrphanedAt))
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
	// …and then aged past C2's cooling-off window, because a FRESH verdict deliberately does not unblock
	// (see TestDeleteUserCascade_Integration_OrphanFreshTerminalVerdictStillBlocks, which is the same
	// fixture minus this line).
	ageTerminalVerdict(t, ctx, pool, item.ID, terminalVerdictAgeBeyondCoolingOff)

	// Before the cascade there is nothing to act on yet — the fact lives on the linked_items row.
	require.Empty(t, readOrphanRecords(t, ctx, pool, enc, item.ID),
		"the orphan record is written by the cascade, not by the failure — while the row lives, the row IS the record")

	deleted, _, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
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
	require.NotNil(t, rec.TerminalSince, "when the verdict was first reached is what tells an operator it was tried at all")
	assert.True(t, rec.OrphanedAt.Sub(*rec.TerminalSince) >= 7*24*time.Hour,
		"terminal_since must predate the erasure by at least the cooling-off window — the pair is the audit "+
			"trail showing the verdict was OBSERVED rather than guessed")
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

	// And no foreign key of any kind. This is the structural half of the same property: an FK to users
	// would give the cascade a CASCADE path straight through this table, destroying the record it was
	// created to preserve — the failure would be invisible in this test's earlier assertions, because
	// the deletion order would still leave the record written and then remove it.
	var fkCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.table_constraints
		WHERE table_name = 'plaid_orphaned_items' AND constraint_type = 'FOREIGN KEY'
	`).Scan(&fkCount))
	assert.Equal(t, 0, fkCount,
		"plaid_orphaned_items must have no foreign key at all — the cascade must not be able to reach it")
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

	deleted, _, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err, "an aborted cascade is a normal outcome, not an error")
	assert.False(t, deleted, "neither fact recorded means work is still owed and still possible — keep blocking")
	assertUserRowExists(t, ctx, pool, user.ID)
	assert.Empty(t, readOrphanRecords(t, ctx, pool, enc, item.ID),
		"an aborted cascade must leave no orphan record: the linked_items row still exists and is still being retried")
}

// TestDeleteUserCascade_Integration_OrphanFreshTerminalVerdictStillBlocks is the cooling-off window,
// and it is the answer to the sharpest objection against this whole change: the terminal verdict is an
// INFERENCE, not something Plaid stated. Plaid returns INVALID_ACCESS_TOKEN both for a genuinely dead
// token and when a token is used against the wrong environment or client_id — a deployment-level
// misconfiguration, curable by an ops fix. Unblocking the moment the verdict is written would let ONE
// bad deploy mark every Item in the estate hopeless and then erase over all of them, destroying the
// tokens on the way out.
//
// So a verdict written just now must NOT unblock. Only elapsed time can satisfy the guard, and nothing
// stops retrying during it, which is what turns the inference into an observation. This test is the
// headline test's fixture minus the backdating.
func TestDeleteUserCascade_Integration_OrphanFreshTerminalVerdictStillBlocks(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, nil, nil, nil)

	item := &domain.LinkedItem{
		UserID:      user.ID,
		ItemID:      "plaid-item-fresh-verdict-" + suffix,
		AccessToken: "wrong-environment-token-" + suffix,
		Provider:    "plaid",
	}
	require.NoError(t, linkedItemRepo.Create(ctx, item))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM linked_items WHERE id = $1", item.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM plaid_orphaned_items WHERE linked_item_id = $1", item.ID)
	})
	require.NoError(t, linkedItemRepo.MarkPlaidRemovalFailed(ctx, []string{item.ID}, "INVALID_ACCESS_TOKEN"))

	deleted, _, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err, "waiting out the window is a normal outcome, not an error")
	assert.False(t, deleted,
		"a verdict reached moments ago has not been OBSERVED to persist — an environment misconfiguration "+
			"looks exactly like this and is fixed within hours")
	assertUserRowExists(t, ctx, pool, user.ID)
	assert.Empty(t, readOrphanRecords(t, ctx, pool, enc, item.ID),
		"no erasure means no orphan: the Item is still findable locally and still being retried")

	// One more day inside the window still blocks — the boundary is the window, not "any elapsed time".
	ageTerminalVerdict(t, ctx, pool, item.ID, 6*24*time.Hour)
	deleted, _, err = repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err)
	assert.False(t, deleted, "6 days is inside the 7-day window")

	// Past it, the same row goes through — proving the block was the window and nothing else.
	ageTerminalVerdict(t, ctx, pool, item.ID, 2*24*time.Hour)
	deleted, _, err = repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err)
	assert.True(t, deleted, "once the verdict has held for longer than the window, the erasure may complete")
	assert.Len(t, readOrphanRecords(t, ctx, pool, enc, item.ID), 1)
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

	deleted, _, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
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
	// Aged past the cooling-off window, so the cascade actually reaches the INSERT this test is about —
	// otherwise C2 blocks first and the test would pass for the wrong reason.
	ageTerminalVerdict(t, ctx, pool, item.ID, terminalVerdictAgeBeyondCoolingOff)

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

	deleted, _, cascadeErr := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
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

// TestMarkPlaidRemovalFailed_Integration_NeverLabelsAProvedRemovedRow pins MarkPlaidRemovalFailed's
// `plaid_removed_at IS NULL` guard, which is load-bearing in two places and was added by this change:
//
//   - It keeps the two columns from contradicting each other in the direction that would matter. A
//     retry racing a success (removal proved by one attempt, an error arriving from another) must not
//     be able to stamp "unremovable" onto a row we have PROVED is removed — the weaker fact must never
//     overwrite the stronger one.
//   - It is why the zero-row result is benign, which plaidItemCleanupService.
//     recordTerminalPlaidRemovalFailure depends on: zero rows means the row is gone or already proved
//     removed, and in neither case is a verdict needed. Drop the guard and that reasoning becomes
//     false while every service-level test still passes, because a mock cannot evaluate a WHERE clause.
//
// This test lives beside the #1494 cascade tests rather than with the other linked-item repository
// tests because the guard exists for the cascade's benefit.
func TestMarkPlaidRemovalFailed_Integration_NeverLabelsAProvedRemovedRow(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, nil, nil, nil)

	item := &domain.LinkedItem{
		UserID:      user.ID,
		ItemID:      "plaid-item-guard-" + suffix,
		AccessToken: "token-" + suffix,
		Provider:    "plaid",
	}
	require.NoError(t, linkedItemRepo.Create(ctx, item))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM linked_items WHERE id = $1", item.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID)
	})

	readVerdict := func() (*time.Time, *string) {
		var at *time.Time
		var code *string
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT plaid_removal_failed_at, plaid_removal_error_code FROM linked_items WHERE id = $1",
			item.ID).Scan(&at, &code))
		return at, code
	}

	// A first verdict lands. The TIMESTAMP is first-wins and the CODE is latest-wins, and the split is
	// load-bearing: C2's cooling-off window measures how long the verdict has held, so refreshing the
	// timestamp on each daily retry would push the deadline forward forever and restore the permanent
	// block #1494 is about. The code has no such role and an operator wants Plaid's latest answer.
	require.NoError(t, linkedItemRepo.MarkPlaidRemovalFailed(ctx, []string{item.ID}, "NO_USABLE_ACCESS_TOKEN"))
	firstAt, firstCode := readVerdict()
	require.NotNil(t, firstAt)
	require.NotNil(t, firstCode)
	assert.Equal(t, "NO_USABLE_ACCESS_TOKEN", *firstCode)

	require.NoError(t, linkedItemRepo.MarkPlaidRemovalFailed(ctx, []string{item.ID}, "INVALID_ACCESS_TOKEN"))
	secondAt, secondCode := readVerdict()
	require.NotNil(t, secondCode)
	assert.Equal(t, "INVALID_ACCESS_TOKEN", *secondCode,
		"the error code is overwritten: the row answers 'what did Plaid say most recently'")
	require.NotNil(t, secondAt)
	assert.Equal(t, firstAt.UTC(), secondAt.UTC(),
		"the timestamp must NOT move: it is when the verdict was FIRST reached, and the cooling-off window "+
			"depends on it never being refreshed by a retry")

	// Now the removal is PROVED. From here the verdict must be unwritable, and the attempt must report
	// the zero-row sentinel rather than silently succeeding.
	require.NoError(t, linkedItemRepo.MarkPlaidRemoved(ctx, []string{item.ID}))
	err := linkedItemRepo.MarkPlaidRemovalFailed(ctx, []string{item.ID}, "INVALID_ACCESS_TOKEN")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoLinkedItemsUpdated,
		"a proved-removed row matches nothing, and the caller must be able to tell that apart from a write")

	afterAt, afterCode := readVerdict()
	assert.Equal(t, secondAt.UTC(), afterAt.UTC(),
		"the earlier verdict is retained as evidence, but nothing new may be written over a proved removal")
	require.NotNil(t, afterCode)
	assert.Equal(t, *secondCode, *afterCode)

	// And the guard now lets the erasure through on the strength of plaid_removed_at alone.
	count, err := repo.CountUncleanedLinkedItems(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
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
	fresh := newItem("fresh")
	require.NoError(t, linkedItemRepo.MarkPlaidRemoved(ctx, []string{removed.ID}))
	require.NoError(t, linkedItemRepo.MarkPlaidRemovalFailed(ctx, []string{terminal.ID}, "NO_USABLE_ACCESS_TOKEN"))
	ageTerminalVerdict(t, ctx, pool, terminal.ID, terminalVerdictAgeBeyondCoolingOff)
	require.NoError(t, linkedItemRepo.MarkPlaidRemovalFailed(ctx, []string{fresh.ID}, "INVALID_ACCESS_TOKEN"))

	count, err := repo.CountUncleanedLinkedItems(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, count,
		"outstanding (%s) and a verdict still inside the cooling-off window (%s) both count; %s is proved "+
			"removed and %s has been proved unremovable for longer than the window",
		outstanding.ID, fresh.ID, removed.ID, terminal.ID)

	// And the guard agrees, on the same rows, in the same database — the two must never diverge.
	var blocked bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT `+uncleanedLinkedItemExistsClause, user.ID).Scan(&blocked))
	assert.True(t, blocked)

	require.NoError(t, linkedItemRepo.MarkPlaidRemovalFailed(ctx, []string{outstanding.ID}, "INVALID_ACCESS_TOKEN"))
	ageTerminalVerdict(t, ctx, pool, outstanding.ID, terminalVerdictAgeBeyondCoolingOff)
	ageTerminalVerdict(t, ctx, pool, fresh.ID, terminalVerdictAgeBeyondCoolingOff)
	count, err = repo.CountUncleanedLinkedItems(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	require.NoError(t, pool.QueryRow(ctx, `SELECT `+uncleanedLinkedItemExistsClause, user.ID).Scan(&blocked))
	assert.False(t, blocked, "once every row carries one of the two facts, the guard must let the erasure through")
}

// TestDeleteUserCascade_Integration_OrphanRetainsTheAccessTokenWithADeadline is Issue #1497's write
// side, and it asserts the two halves that only make sense together: the credential SURVIVES the
// erasure (so an automated retry has something to present to Plaid) and it survives WITH A DEADLINE
// (so the retention is bounded by the schema rather than by anyone's diligence).
//
// It reads access_token_encrypted back and DECRYPTS it, which is the load-bearing part: the cascade
// copies ciphertext verbatim, so a copy that double-encrypted, truncated or re-keyed the value would
// pass a NOT NULL assertion and still be useless to the retry sweep — the one and only reason the
// column exists.
//
// Falsifiability: dropping `access_token_encrypted` from the INSERT's column list (i.e. restoring
// migration 000134's position) leaves the column NULL and reddens this test; dropping the
// token_expires_at argument leaves the column's DEFAULT NOW() in place and reddens the deadline
// assertion, which is exactly the fail-safe that default was chosen for.
func TestDeleteUserCascade_Integration_OrphanRetainsTheAccessTokenWithADeadline(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, nil, nil, nil)

	// A 'replaced' re-link leftover: the leg whose /item/remove was NEVER attempted, and therefore the
	// leg that proves migration 000134's "the token is known unusable anyway" argument was wrong. Its
	// token has never been rejected by anything.
	plaintextToken := "access-sandbox-retained-" + suffix
	item := &domain.LinkedItem{
		UserID:          user.ID,
		ItemID:          "plaid-item-" + suffix,
		AccessToken:     plaintextToken,
		InstitutionID:   "ins_" + suffix,
		InstitutionName: "Still Billing Bank",
		Provider:        "plaid",
	}
	require.NoError(t, linkedItemRepo.Create(ctx, item))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM linked_items WHERE id = $1", item.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM plaid_orphaned_items WHERE linked_item_id = $1", item.ID)
	})

	require.NoError(t, linkedItemRepo.MarkPlaidRemovalFailed(ctx, []string{item.ID}, "INVALID_ACCESS_TOKEN"))
	ageTerminalVerdict(t, ctx, pool, item.ID, terminalVerdictAgeBeyondCoolingOff)

	before := time.Now().UTC()
	deleted, _, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err)
	require.True(t, deleted)

	var encToken *string
	var expiresAt time.Time
	var purgedAt *time.Time
	var attempts int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT access_token_encrypted, token_expires_at, token_purged_at, retry_attempt_count
		FROM plaid_orphaned_items WHERE linked_item_id = $1`, item.ID).
		Scan(&encToken, &expiresAt, &purgedAt, &attempts))

	require.NotNil(t, encToken, "without the credential the retry sweep has nothing to present to Plaid, "+
		"and #1497 does not exist")
	plain, err := enc.DecryptString(*encToken)
	require.NoError(t, err, "the copy must be decryptable through the same FieldEncryptor — a mangled "+
		"ciphertext would satisfy NOT NULL and still be worthless")
	assert.Equal(t, plaintextToken, plain,
		"the retained value must be the SAME credential, byte for byte; anything else cannot be presented to Plaid")

	assert.Nil(t, purgedAt, "nothing has been purged yet — the deadline is 30 days out")
	assert.Equal(t, 0, attempts)

	// The deadline: 30 days out, bounded on both sides so neither a missing argument (which would leave
	// DEFAULT NOW(), i.e. immediately expired) nor a wrong constant passes.
	wanted := before.Add(domain.OrphanedItemTokenRetention)
	assert.WithinDuration(t, wanted, expiresAt.UTC(), 2*time.Minute,
		"token_expires_at must be orphaned_at + domain.OrphanedItemTokenRetention (30 days) — the boundary the "+
			"GDPR argument for retaining this credential at all rests on")
	assert.True(t, expiresAt.UTC().After(before.Add(29*24*time.Hour)),
		"an already-expired or short deadline means the retry window never really existed")
}
