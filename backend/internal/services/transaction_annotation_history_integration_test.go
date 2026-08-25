//go:build integration

package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// Issue #1569 Task 6 (#1579) — the annotation trail, end to end against real Postgres.
//
// WHY INTEGRATION AND NOT pgxmock. Every claim here is about what Postgres does inside one
// statement: that `UPDATE transactions t ... FROM transactions prior WHERE prior.id = t.id`
// resolves `prior.*` against the pre-update snapshot, that the `chk_transaction_annotation_history_
// source` CHECK accepts what the writer sends, and that the row-wise `IS DISTINCT FROM` suppresses
// a genuine no-op. pgxmock asserts query TEXT and never sends one to a planner, so it can confirm
// none of that — the pgxmock-never-catches-constraints lesson this project has paid for twice.
//
// THE FIXTURES GO THROUGH THE PRODUCTION WRITE PATH. Transactions are inserted by
// transactionRepository.CreateBatch and every annotation change is made by calling
// transactionService (UpdateTransactionCategory / BulkUpdateTransactionCategory / SetReviewed) —
// the same three calls the HTTP handlers make. Nothing in this file INSERTs a
// transaction_annotation_history row; if the writer is absent, every assertion below fails, which
// is exactly how this test was first run.

type annotationHistoryFixture struct {
	tx           pgx.Tx
	svc          TransactionService
	histRepo     repository.TransactionAnnotationHistoryRepository
	txRepo       repository.TransactionRepository
	userID       string
	otherUserID  string
	accountID    string
	linkedItemID string
	customCatID  string

	otherAccountID    string
	otherLinkedItemID string

	// plainExtID keeps each seeded transaction's PLAINTEXT external id. CreateBatch encrypts the
	// field in place, so the value on the returned struct is ciphertext and re-encrypting it would
	// produce a different blind index — the lookup TombstoneByExternalID does would then miss.
	plainExtID map[string]string
}

// marchReview is the pre-existing reviewed_at both category statements silently clobber. A fixed
// date, not a relative one, so the assertion says what it means.
var marchReview = time.Date(2026, 3, 2, 9, 30, 0, 0, time.UTC)

func seedAnnotationHistoryFixture(t *testing.T) *annotationHistoryFixture {
	t.Helper()
	pool := connectIntegrationDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	f := &annotationHistoryFixture{
		tx:           tx,
		userID:       uuid.New().String(),
		otherUserID:  uuid.New().String(),
		accountID:    uuid.New().String(),
		linkedItemID: uuid.New().String(),
		customCatID:  uuid.New().String(),

		otherAccountID:    uuid.New().String(),
		otherLinkedItemID: uuid.New().String(),
		plainExtID:        map[string]string{},
	}

	mustExec := func(what, sql string, args ...any) {
		t.Helper()
		_, execErr := tx.Exec(ctx, sql, args...)
		require.NoError(t, execErr, "seed %s", what)
	}

	for _, u := range []string{f.userID, f.otherUserID} {
		mustExec("user", `
			INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
			VALUES ($1, $2, $3, 'premium', 'active', 'iad')
		`, u, "annot-hist|"+u, "annot-hist-"+u+"@example.com")
	}

	mustExec("linked_item", `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-annot-hist-token')
	`, f.linkedItemID, f.userID, "annot-hist-item-"+f.linkedItemID)

	mustExec("account", `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Checking', 'depository', 'active')
	`, f.accountID, f.linkedItemID, f.userID, "annot-hist-acct-"+f.accountID)

	mustExec("other_linked_item", `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-annot-hist-other-token')
	`, f.otherLinkedItemID, f.otherUserID, "annot-hist-other-item-"+f.otherLinkedItemID)

	mustExec("other_account", `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Their Checking', 'depository', 'active')
	`, f.otherAccountID, f.otherLinkedItemID, f.otherUserID, "annot-hist-other-acct-"+f.otherAccountID)

	mustExec("custom_category", `
		INSERT INTO custom_categories (id, user_id, name) VALUES ($1, $2, 'Weekend Coffee')
	`, f.customCatID, f.userID)

	enc, err := appcrypto.NewFieldEncryptor(plainPassthroughEncryptionServiceForRuleHistory{},
		[]byte("12345678901234567890123456789012"))
	require.NoError(t, err)

	f.txRepo = repository.NewTransactionRepository(tx, enc)
	f.svc = NewTransactionService(f.txRepo, repository.NewCustomCategoryRepository(tx))
	f.histRepo = repository.NewTransactionAnnotationHistoryRepository(tx)
	return f
}

// seedTransaction inserts one transaction through CreateBatch — the production write path — with a
// provider category, a pre-existing user override, and a March review timestamp.
func (f *annotationHistoryFixture) seedTransaction(t *testing.T, userID, merchant string, reviewed bool) domain.Transaction {
	t.Helper()
	kind := "pfc"
	priorOverride := "ENTERTAINMENT"
	pfcPrimary := "GENERAL_MERCHANDISE"
	extID := "plaid-" + uuid.New().String()

	txn := domain.Transaction{
		ID:                    uuid.New().String(),
		UserID:                userID,
		AccountID:             f.accountID,
		LinkedItemID:          f.linkedItemID,
		Source:                domain.SourcePlaid,
		ExternalTransactionID: strPtr(extID),
		Amount:                7.25,
		Date:                  time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		Name:                  merchant,
		MerchantName:          &merchant,
		PFCPrimary:            &pfcPrimary,
		CategoryOverrideKind:  &kind,
		PFCPrimaryOverride:    &priorOverride,
		CategorySourceField:   domain.CategorySourceManual,
		Reviewed:              reviewed,
	}
	if reviewed {
		at := marchReview
		txn.ReviewedAt = &at
	}
	// The cross-tenant fixture gets its OWN linked item and account: a transaction pointing at
	// another user's account_id would be a shape production cannot produce, and a fixture that
	// asserts against an impossible row proves nothing.
	if userID != f.userID {
		txn.AccountID = f.otherAccountID
		txn.LinkedItemID = f.otherLinkedItemID
	}

	batch := []domain.Transaction{txn}
	_, err := f.txRepo.CreateBatch(context.Background(), batch)
	require.NoError(t, err)
	f.plainExtID[batch[0].ID] = extID
	return batch[0]
}

func (f *annotationHistoryFixture) history(t *testing.T) ([]*domain.TransactionAnnotationChange, int) {
	t.Helper()
	rows, total, err := f.histRepo.GetByUserID(context.Background(), f.userID, 100, 0)
	require.NoError(t, err)
	return rows, total
}

// -------------------------------------------------------------------------------------------

// TestAnnotationHistory_SingleCategoryUpdateRecordsPriorOverrideAndReviewedAt_Integration is the
// headline. Before this change nothing on any of the three statements recorded a prior value, so
// this assertion could not have passed for any input at all.
//
// It also pins the fact #1579 MISSED: UpdateCategory writes `reviewed = true, reviewed_at = now`
// unconditionally, so the March timestamp is destroyed by a write the user made about a CATEGORY.
func TestAnnotationHistory_SingleCategoryUpdateRecordsPriorOverrideAndReviewedAt_Integration(t *testing.T) {
	f := seedAnnotationHistoryFixture(t)
	ctx := context.Background()

	txn := f.seedTransaction(t, f.userID, "Blue Bottle Coffee", true)

	_, err := f.svc.UpdateTransactionCategory(ctx, f.userID, txn.ID, "custom", nil, nil, &f.customCatID)
	require.NoError(t, err)

	rows, total := f.history(t)
	require.Equal(t, 1, total)
	require.Len(t, rows, 1)
	row := rows[0]

	assert.Equal(t, f.userID, row.UserID, "the row's own GDPR erasure path")
	assert.Equal(t, txn.ID, row.TransactionID)
	assert.Equal(t, domain.TransactionAnnotationChangeCategory, row.ChangeSource)

	require.NotNil(t, row.Prior.CategoryOverrideKind)
	assert.Equal(t, "pfc", *row.Prior.CategoryOverrideKind)
	require.NotNil(t, row.Prior.PFCPrimaryOverride)
	assert.Equal(t, "ENTERTAINMENT", *row.Prior.PFCPrimaryOverride)
	require.NotNil(t, row.Prior.EffectiveCategoryName)
	assert.Equal(t, "ENTERTAINMENT", *row.Prior.EffectiveCategoryName)
	assert.True(t, row.Prior.Reviewed)
	require.NotNil(t, row.Prior.ReviewedAt,
		"#1579 missed this: the pre-existing reviewed_at is destroyed by a CATEGORY write")
	assert.WithinDuration(t, marchReview, *row.Prior.ReviewedAt, time.Second)

	require.NotNil(t, row.New.CategoryOverrideKind)
	assert.Equal(t, "custom", *row.New.CategoryOverrideKind)
	require.NotNil(t, row.New.CustomCategoryOverrideID)
	assert.Equal(t, f.customCatID, *row.New.CustomCategoryOverrideID)
	require.NotNil(t, row.New.EffectiveCategoryName)
	assert.Equal(t, "Weekend Coffee", *row.New.EffectiveCategoryName)
	assert.True(t, row.New.Reviewed)
	require.NotNil(t, row.New.ReviewedAt)
	assert.True(t, row.New.ReviewedAt.After(marchReview), "the write moved reviewed_at forward")
	assert.False(t, row.ChangedAt.IsZero())
}

// TestAnnotationHistory_ChainedUpdatesRecordTheImmediatelyPriorValue_Integration is the atomic-
// capture property stated as behaviour: the second write's `prior` is the first write's `new`,
// not the value the transaction was created with. A SELECT-then-UPDATE writer can satisfy this
// serially and still be wrong under concurrency, which is why the capture is a CTE in the same
// statement rather than a second round-trip.
func TestAnnotationHistory_ChainedUpdatesRecordTheImmediatelyPriorValue_Integration(t *testing.T) {
	f := seedAnnotationHistoryFixture(t)
	ctx := context.Background()

	txn := f.seedTransaction(t, f.userID, "Blue Bottle Coffee", true)

	food := "FOOD_AND_DRINK"
	_, err := f.svc.UpdateTransactionCategory(ctx, f.userID, txn.ID, "pfc", &food, nil, nil)
	require.NoError(t, err)
	_, err = f.svc.UpdateTransactionCategory(ctx, f.userID, txn.ID, "custom", nil, nil, &f.customCatID)
	require.NoError(t, err)

	rows, total := f.history(t)
	require.Equal(t, 2, total)
	require.Len(t, rows, 2)

	newest, oldest := rows[0], rows[1] // ORDER BY changed_at DESC
	require.NotNil(t, oldest.New.PFCPrimaryOverride)
	require.NotNil(t, newest.Prior.PFCPrimaryOverride)
	assert.Equal(t, "FOOD_AND_DRINK", *oldest.New.PFCPrimaryOverride)
	assert.Equal(t, "FOOD_AND_DRINK", *newest.Prior.PFCPrimaryOverride,
		"the second write's prior must be the first write's new, not the seeded ENTERTAINMENT")
	require.NotNil(t, newest.Prior.ReviewedAt)
	require.NotNil(t, oldest.New.ReviewedAt)
	assert.WithinDuration(t, *oldest.New.ReviewedAt, *newest.Prior.ReviewedAt, time.Millisecond)
}

// TestAnnotationHistory_BulkCategoryRecordsOneRowPerTransaction_Integration: the bulk path is the
// one #1579 filed, and it must attribute each prior value to the RIGHT transaction rather than
// collapsing them.
func TestAnnotationHistory_BulkCategoryRecordsOneRowPerTransaction_Integration(t *testing.T) {
	f := seedAnnotationHistoryFixture(t)
	ctx := context.Background()

	a := f.seedTransaction(t, f.userID, "Blue Bottle Coffee", true)
	b := f.seedTransaction(t, f.userID, "Peets Coffee", false)

	n, err := f.svc.BulkUpdateTransactionCategory(ctx, f.userID, []string{a.ID, b.ID}, "custom", nil, nil, &f.customCatID)
	require.NoError(t, err)
	require.EqualValues(t, 2, n)

	rows, total := f.history(t)
	require.Equal(t, 2, total)

	byTxn := map[string]*domain.TransactionAnnotationChange{}
	for _, r := range rows {
		byTxn[r.TransactionID] = r
		assert.Equal(t, domain.TransactionAnnotationChangeCategory, r.ChangeSource)
	}
	require.Contains(t, byTxn, a.ID)
	require.Contains(t, byTxn, b.ID)

	require.NotNil(t, byTxn[a.ID].Prior.ReviewedAt, "a was reviewed in March and the bulk write clobbered it")
	assert.WithinDuration(t, marchReview, *byTxn[a.ID].Prior.ReviewedAt, time.Second)
	assert.True(t, byTxn[a.ID].Prior.Reviewed)

	assert.Nil(t, byTxn[b.ID].Prior.ReviewedAt, "b was never reviewed")
	assert.False(t, byTxn[b.ID].Prior.Reviewed)
	assert.True(t, byTxn[b.ID].New.Reviewed, "bulk recategorisation marks reviewed as a side effect")
}

// TestAnnotationHistory_ReviewChangeIsRecordedAndAttributedToTheReviewSource_Integration covers the
// two review endpoints. They share ONE repository method (SetReviewed) — #1579's "four methods" is
// wrong, and the single-row handler passes a one-element slice to the same statement.
func TestAnnotationHistory_ReviewChangeIsRecordedAndAttributedToTheReviewSource_Integration(t *testing.T) {
	f := seedAnnotationHistoryFixture(t)
	ctx := context.Background()

	txn := f.seedTransaction(t, f.userID, "Blue Bottle Coffee", true)

	// The single-transaction endpoint's exact call: one id in the slice.
	n, err := f.svc.SetReviewed(ctx, f.userID, []string{txn.ID}, false)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)

	rows, total := f.history(t)
	require.Equal(t, 1, total)
	row := rows[0]

	assert.Equal(t, domain.TransactionAnnotationChangeReview, row.ChangeSource)
	assert.True(t, row.Prior.Reviewed)
	require.NotNil(t, row.Prior.ReviewedAt)
	assert.WithinDuration(t, marchReview, *row.Prior.ReviewedAt, time.Second)
	assert.False(t, row.New.Reviewed)
	assert.Nil(t, row.New.ReviewedAt)

	// The category tuple is untouched by a review write, and both halves say so.
	require.NotNil(t, row.Prior.CategoryOverrideKind)
	require.NotNil(t, row.New.CategoryOverrideKind)
	assert.Equal(t, *row.Prior.CategoryOverrideKind, *row.New.CategoryOverrideKind)
}

// TestAnnotationHistory_ATrueNoOpRecordsNothing_Integration pins the no-op ruling. Un-reviewing an
// already-unreviewed transaction changes nothing at all — reviewed stays false, reviewed_at stays
// NULL — and an append-only trail that grows on writes that changed nothing is noise that makes the
// real overwrites harder to find.
//
// ⚠ The rule is column-wise, not endpoint-wise, and a category re-write of the SAME category is
// still recorded, because it moves reviewed_at. That is deliberate: the moved timestamp is itself
// an unrecoverable fact, which is the whole point of this task.
func TestAnnotationHistory_ATrueNoOpRecordsNothing_Integration(t *testing.T) {
	f := seedAnnotationHistoryFixture(t)
	ctx := context.Background()

	txn := f.seedTransaction(t, f.userID, "Peets Coffee", false)

	n, err := f.svc.SetReviewed(ctx, f.userID, []string{txn.ID}, false)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "rows-affected must be unchanged by the capture: the UPDATE still ran")

	_, total := f.history(t)
	assert.Equal(t, 0, total, "nothing was overwritten, so nothing is recorded")

	// And the same write that DOES change something is recorded, so the suppression is not blanket.
	_, err = f.svc.SetReviewed(ctx, f.userID, []string{txn.ID}, true)
	require.NoError(t, err)
	_, total = f.history(t)
	assert.Equal(t, 1, total)
}

// TestAnnotationHistory_NeverRecordsOrReturnsAnotherUsersRow_Integration. The bulk statements take
// a client-supplied id array, so a caller can name any uuid at all; the tenancy predicate is what
// stops it, and the capture must inherit that predicate rather than re-derive it.
func TestAnnotationHistory_NeverRecordsOrReturnsAnotherUsersRow_Integration(t *testing.T) {
	f := seedAnnotationHistoryFixture(t)
	ctx := context.Background()

	mine := f.seedTransaction(t, f.userID, "Blue Bottle Coffee", true)
	theirs := f.seedTransaction(t, f.otherUserID, "Not Mine", true)

	n, err := f.svc.BulkUpdateTransactionCategory(ctx, f.userID,
		[]string{mine.ID, theirs.ID}, "custom", nil, nil, &f.customCatID)
	require.NoError(t, err)
	require.EqualValues(t, 1, n, "the other user's transaction is not mine to update")

	rows, total := f.history(t)
	require.Equal(t, 1, total)
	assert.Equal(t, mine.ID, rows[0].TransactionID)

	// And the other user's own history read cannot see mine either.
	otherRows, otherTotal, err := f.histRepo.GetByUserID(ctx, f.otherUserID, 100, 0)
	require.NoError(t, err)
	assert.Equal(t, 0, otherTotal)
	assert.Empty(t, otherRows)
}

// TestAnnotationHistory_ATombstonedTransactionIsStillRecorded_Integration states the tombstone
// behaviour explicitly rather than leaving it to be discovered.
//
// None of the three UPDATE statements filters `deleted_at IS NULL` today (each carries a
// `tombstone-exempt:` note saying liveness is the service layer's gate), and this task does NOT
// change that — narrowing a write predicate is a behaviour change well outside "record the prior
// value". So the capture mirrors the write: whatever the UPDATE touched, the trail records. The
// direction is the safe one — a tombstoned transaction whose annotation is overwritten is exactly
// the case where the prior value is least recoverable from anywhere else.
func TestAnnotationHistory_ATombstonedTransactionIsStillRecorded_Integration(t *testing.T) {
	f := seedAnnotationHistoryFixture(t)
	ctx := context.Background()

	txn := f.seedTransaction(t, f.userID, "Blue Bottle Coffee", true)

	// Tombstoned through the production path, not by hand: TombstoneByExternalID is what the sync
	// calls, and it is the only writer that satisfies transactions_tombstone_consistency (deleted_at,
	// deleted_reason and deleted_by must all be set together) and computes the reason from `pending`.
	reason, err := f.txRepo.TombstoneByExternalID(ctx, domain.SourcePlaid,
		f.plainExtID[txn.ID], f.userID, "plaid_sync")
	require.NoError(t, err)
	require.Equal(t, domain.TxnDeletedProviderRemovedPosted, reason)

	n, err := f.svc.SetReviewed(ctx, f.userID, []string{txn.ID}, false)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)

	rows, total := f.history(t)
	require.Equal(t, 1, total)
	assert.Equal(t, txn.ID, rows[0].TransactionID)
}

// TestAnnotationHistory_SingleUpdateNeverTouchesAnotherUsersTransaction_Integration closes the gap
// review found in the first round's mutation set: M3 widened only the BULK tenancy predicate, so
// `t.user_id = $9` on the single-row statement had no test that would notice it going away.
//
// It calls the REPOSITORY, not the service, on purpose. UpdateTransactionCategory checks ownership
// with GetTransactionByID before it ever reaches the statement, so a service-level test would be
// satisfied by that check and would keep passing with the SQL predicate deleted. The claim under
// test is that the statement is single-tenant BY SQL CONSTRUCTION, which is exactly what survives
// if someone later adds a caller that skips the service.
func TestAnnotationHistory_SingleUpdateNeverTouchesAnotherUsersTransaction_Integration(t *testing.T) {
	f := seedAnnotationHistoryFixture(t)
	ctx := context.Background()

	theirs := f.seedTransaction(t, f.otherUserID, "Not Mine", true)

	err := f.txRepo.UpdateCategory(ctx, theirs.ID, f.userID,
		strPtr("custom"), nil, nil, &f.customCatID, domain.CategorySourceManual)
	require.ErrorIs(t, err, repository.ErrTransactionNotFound,
		"another user's transaction id must match zero rows, not one")

	_, total := f.history(t)
	assert.Equal(t, 0, total, "nothing was updated, so nothing may be recorded")

	otherRows, otherTotal, err := f.histRepo.GetByUserID(ctx, f.otherUserID, 100, 0)
	require.NoError(t, err)
	assert.Equal(t, 0, otherTotal, "and nothing may be recorded against the owner either")
	assert.Empty(t, otherRows)

	// The row itself is untouched: the override it was seeded with is still there.
	var kind *string
	require.NoError(t, f.tx.QueryRow(ctx,
		`SELECT pfc_primary_override FROM transactions WHERE id = $1`, theirs.ID).Scan(&kind))
	require.NotNil(t, kind)
	assert.Equal(t, "ENTERTAINMENT", *kind)
}
