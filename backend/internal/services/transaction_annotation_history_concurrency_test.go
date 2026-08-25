//go:build integration

package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// Issue #1569 Task 6 (#1579), review fix round 1 (F1 / ruling R79) — the annotation capture under
// CONCURRENCY.
//
// The first cut of this task documented an "EvalPlanQual residual" and called it a slightly stale
// prior value. Review measured it and it is worse than that in two distinct ways, both reproduced
// here BEFORE the fix and both of which the fix has to close:
//
//  1. A FABRICATED TRANSITION. prior.* comes from the statement's original snapshot; RETURNING t.*
//     comes from the POST-EvalPlanQual tuple. When a concurrent writer commits between the two, the
//     halves of one history row come from DIFFERENT snapshots, and the row describes a before->after
//     step that no single write ever performed. That is not a stale record, it is a false one, and
//     a false record is worse than none: this table exists to be believed.
//
//  2. THE RECORD IS DROPPED ENTIRELY. The no-op suppression compares the stale prior against the
//     post-EPQ new. If our write happens to restore exactly the value the stale prior holds, the two
//     are equal, the INSERT is suppressed, and the concurrent writer's committed annotation is
//     destroyed with NOTHING recorded — the precise defect #1579 exists to eliminate, surviving its
//     own fix.
//
// THESE TESTS NEED COMMITTED ROWS, so they cannot use the rollback fixture the rest of the suite
// uses: one session has to SEE what the other committed. Each test creates its own user and deletes
// it at the end, which cascades everything away.
//
// HOW THE RACE IS FORCED, since a concurrency test that does not actually interleave is the most
// convincing kind of vacuous test:
//
//	B: BEGIN; <production annotation write on row X>;        -- holds the row lock, uncommitted
//	A:        <capturing statement over row X>               -- takes its snapshot (pre-B), then
//	                                                            BLOCKS on B's row lock
//	B: COMMIT;                                               -- A unblocks into EvalPlanQual
//
// A's block is not assumed from a sleep: waitForBlockedOn polls pg_locks for A's ungranted tuple
// lock and the test fails if it never appears. Without that, a test that simply ran A after B would
// pass for the wrong reason.

// annotationRaceFixture is a committed-data fixture: a real user, account and transaction that both
// sessions can see, removed by a cascading delete at the end of the test.
type annotationRaceFixture struct {
	pool         *pgxpool.Pool
	enc          *appcrypto.FieldEncryptor
	userID       string
	accountID    string
	linkedItemID string
	txnID        string
}

func seedAnnotationRaceFixture(t *testing.T) *annotationRaceFixture {
	t.Helper()
	pool := connectIntegrationDB(t)
	ctx := context.Background()

	enc, err := appcrypto.NewFieldEncryptor(plainPassthroughEncryptionServiceForRuleHistory{},
		[]byte("12345678901234567890123456789012"))
	require.NoError(t, err)

	f := &annotationRaceFixture{
		pool:         pool,
		enc:          enc,
		userID:       uuid.New().String(),
		accountID:    uuid.New().String(),
		linkedItemID: uuid.New().String(),
	}

	mustExec := func(what, sql string, args ...any) {
		t.Helper()
		_, execErr := pool.Exec(ctx, sql, args...)
		require.NoError(t, execErr, "seed %s", what)
	}

	mustExec("user", `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, f.userID, "annot-race|"+f.userID, "annot-race-"+f.userID+"@example.com")

	// One cascading delete removes the user, the item, the account, the transaction and every
	// history row written during the test. context.Background(), not t.Context(): a cancelled
	// cleanup context leaks fixture rows onto the shared database and breaks OTHER packages' tests.
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, f.userID)
	})

	mustExec("linked_item", `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-annot-race-token')
	`, f.linkedItemID, f.userID, "annot-race-item-"+f.linkedItemID)

	mustExec("account", `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Checking', 'depository', 'active')
	`, f.accountID, f.linkedItemID, f.userID, "annot-race-acct-"+f.accountID)

	// Through CreateBatch, the production write path, exactly as the rest of the suite does.
	kind := "pfc"
	priorOverride := "ENTERTAINMENT"
	pfcPrimary := "GENERAL_MERCHANDISE"
	merchant := "Blue Bottle Coffee"
	reviewedAt := marchReview
	batch := []domain.Transaction{{
		ID:                    uuid.New().String(),
		UserID:                f.userID,
		AccountID:             f.accountID,
		LinkedItemID:          f.linkedItemID,
		Source:                domain.SourcePlaid,
		ExternalTransactionID: strPtr("plaid-" + uuid.New().String()),
		Amount:                7.25,
		Date:                  time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		Name:                  merchant,
		MerchantName:          &merchant,
		PFCPrimary:            &pfcPrimary,
		CategoryOverrideKind:  &kind,
		PFCPrimaryOverride:    &priorOverride,
		CategorySourceField:   domain.CategorySourceManual,
		Reviewed:              true,
		ReviewedAt:            &reviewedAt,
	}}
	_, err = repository.NewTransactionRepository(pool, enc).CreateBatch(ctx, batch)
	require.NoError(t, err)
	f.txnID = batch[0].ID
	return f
}

func (f *annotationRaceFixture) repoOn(db repository.DB) repository.TransactionRepository {
	return repository.NewTransactionRepository(db, f.enc)
}

func (f *annotationRaceFixture) history(t *testing.T) []*domain.TransactionAnnotationChange {
	t.Helper()
	rows, _, err := repository.NewTransactionAnnotationHistoryRepository(f.pool).
		GetByUserID(context.Background(), f.userID, 100, 0)
	require.NoError(t, err)
	return rows
}

// liveAnnotation reads the transaction's current annotation straight from the table, so an
// assertion about "what the row actually says now" does not depend on any code under test.
func (f *annotationRaceFixture) liveAnnotation(t *testing.T) (override *string, reviewed bool, reviewedAt *time.Time) {
	t.Helper()
	require.NoError(t, f.pool.QueryRow(context.Background(),
		`SELECT pfc_primary_override, reviewed, reviewed_at FROM transactions WHERE id = $1`, f.txnID,
	).Scan(&override, &reviewed, &reviewedAt))
	return
}

// waitForBlockedOn fails the test unless some backend other than the one holding the lock is
// waiting on a row lock for our transaction. This is what makes the interleaving a MEASUREMENT
// rather than an assumption — without it, a test whose "concurrent" statement quietly ran to
// completion first would pass while proving nothing.
func waitForBlockedOn(t *testing.T, pool *pgxpool.Pool, blockerPID int32) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		require.NoError(t, pool.QueryRow(context.Background(), `
			SELECT COUNT(*) FROM pg_stat_activity
			WHERE pid <> $1
			  AND wait_event_type = 'Lock'
			  AND state = 'active'
			  AND query ILIKE '%transactions%'
		`, blockerPID).Scan(&waiting))
		if waiting > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the second session never blocked on the first session's row lock; the race did not " +
		"interleave, so this test would prove nothing about concurrency")
}

// backendPID returns the server-side PID of a connection, so waitForBlockedOn can exclude it.
func backendPID(t *testing.T, tx pgx.Tx) int32 {
	t.Helper()
	var pid int32
	require.NoError(t, tx.QueryRow(context.Background(), `SELECT pg_backend_pid()`).Scan(&pid))
	return pid
}

// runRace performs the interleave described in the file header and returns once BOTH sessions have
// finished. writeB runs inside B's open transaction; writeA is the capturing write under test.
func runRace(t *testing.T, f *annotationRaceFixture, writeB func(repository.TransactionRepository) error, writeA func(repository.TransactionRepository) error) {
	t.Helper()
	ctx := context.Background()

	txB, err := f.pool.Begin(ctx)
	require.NoError(t, err)
	committed := false
	defer func() {
		if !committed {
			_ = txB.Rollback(context.Background())
		}
	}()

	require.NoError(t, writeB(f.repoOn(txB)), "session B's write")
	pidB := backendPID(t, txB)

	done := make(chan error, 1)
	go func() { done <- writeA(f.repoOn(f.pool)) }()

	waitForBlockedOn(t, f.pool, pidB)
	require.NoError(t, txB.Commit(ctx))
	committed = true

	select {
	case err := <-done:
		require.NoError(t, err, "session A's write")
	case <-time.After(15 * time.Second):
		t.Fatal("session A never completed after B committed")
	}
}

// ---------------------------------------------------------------------------------------------

// TestAnnotationHistory_ConcurrentWriteDoesNotFabricateATransition_Integration is review scenario 1.
//
// B unreviews the transaction and moves the override to FOOD_AND_DRINK. A then recategorises to
// ENTERTAINMENT-with-no-override... whatever A does, the row A records must describe A's OWN write:
// its `prior` must be the state B committed, because that is what A overwrote.
//
// Before the lock-first fix this recorded prior = the pre-B state, so a single row claimed a
// transition (ENTERTAINMENT / reviewed-in-March -> A's result) that no write ever made.
func TestAnnotationHistory_ConcurrentWriteDoesNotFabricateATransition_Integration(t *testing.T) {
	f := seedAnnotationRaceFixture(t)
	ctx := context.Background()

	food := "FOOD_AND_DRINK"
	runRace(t, f,
		func(repo repository.TransactionRepository) error {
			// B: a real recategorisation, through the production statement.
			return repo.UpdateCategory(ctx, f.txnID, f.userID, strPtr("pfc"), &food, nil, nil, domain.CategorySourceManual)
		},
		func(repo repository.TransactionRepository) error {
			// A: a review flip, whose capture must describe what B left behind.
			_, err := repo.SetReviewed(ctx, f.userID, []string{f.txnID}, false)
			return err
		},
	)

	rows := f.history(t)
	require.Len(t, rows, 2, "B's recategorisation and A's review flip are two separate overwrites")

	// rows[0] is newest: A's review write.
	a := rows[0]
	assert.Equal(t, domain.TransactionAnnotationChangeReview, a.ChangeSource)
	require.NotNil(t, a.Prior.PFCPrimaryOverride,
		"A's prior must be the annotation B committed, not the one B replaced")
	assert.Equal(t, "FOOD_AND_DRINK", *a.Prior.PFCPrimaryOverride,
		"the recorded transition is fabricated: prior comes from a snapshot older than the write it "+
			"claims to describe")

	// And the two halves must come from the same snapshot: A only touched the review columns, so
	// the category tuple must be identical on both sides of its row.
	require.NotNil(t, a.New.PFCPrimaryOverride)
	assert.Equal(t, *a.Prior.PFCPrimaryOverride, *a.New.PFCPrimaryOverride,
		"prior and new disagree about a column this write never touched, which can only mean they "+
			"were read from different snapshots")
}

// TestAnnotationHistory_ConcurrentRevertIsStillRecorded_Integration is review scenario 2, and it is
// the more serious of the two: the API reports success, the concurrent writer's committed
// annotation is destroyed, and the trail is EMPTY.
//
// ⚠ IT HAS TO BE TWO *REVIEW* WRITES, and finding that out corrected the review's framing. A
// category write can never be suppressed, because both category statements move reviewed_at to a
// fresh clock value on every call, so the tracked tuple always differs no matter how stale the
// prior is. I first wrote this scenario with a category revert and it did NOT drop the row — it
// produced a stale-prior row instead (that failure mode is scenario 1's). The drop needs a write
// whose entire tracked tuple can be identical to the stale prior, and SetReviewed(false) on a
// transaction that was unreviewed before B touched it is exactly that: reviewed false -> false,
// reviewed_at NULL -> NULL.
//
// The setup therefore un-reviews first, and the assertion is on the GROWTH of the trail rather than
// its absolute length, so the setup's own (legitimate) row cannot mask the missing one.
func TestAnnotationHistory_ConcurrentRevertIsStillRecorded_Integration(t *testing.T) {
	f := seedAnnotationRaceFixture(t)
	ctx := context.Background()

	// Committed starting point: unreviewed, reviewed_at NULL. This is a real overwrite of the
	// March timestamp and legitimately records one row.
	_, err := f.repoOn(f.pool).SetReviewed(ctx, f.userID, []string{f.txnID}, false)
	require.NoError(t, err)
	before := len(f.history(t))
	require.Equal(t, 1, before)

	runRace(t, f,
		func(repo repository.TransactionRepository) error {
			// B reviews it, stamping a reviewed_at that exists nowhere else.
			_, e := repo.SetReviewed(ctx, f.userID, []string{f.txnID}, true)
			return e
		},
		func(repo repository.TransactionRepository) error {
			// A un-reviews. Against the truth this destroys B's timestamp; against the stale
			// pre-B snapshot it looks like false -> false and is suppressed as a no-op.
			_, e := repo.SetReviewed(ctx, f.userID, []string{f.txnID}, false)
			return e
		},
	)

	// B's committed reviewed_at really is gone, so something really was destroyed.
	_, reviewed, reviewedAt := f.liveAnnotation(t)
	require.False(t, reviewed)
	require.Nil(t, reviewedAt, "A's write must actually have taken effect")

	rows := f.history(t)
	assert.Len(t, rows, before+2,
		"B's review and A's un-review are two overwrites; suppressing A's record destroys B's "+
			"committed reviewed_at with no trail at all, which is the exact defect #1579 exists to "+
			"eliminate, surviving its own fix")

	require.NotEmpty(t, rows)
	a := rows[0]
	assert.True(t, a.Prior.Reviewed, "A overwrote the reviewed state B committed")
	require.NotNil(t, a.Prior.ReviewedAt,
		"B's reviewed_at survives in this row or it survives nowhere")
	assert.False(t, a.New.Reviewed)
	assert.Nil(t, a.New.ReviewedAt)
}
