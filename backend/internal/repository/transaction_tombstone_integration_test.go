//go:build integration

package repository

// Issue #1569 Task 7 — the real-database half of the tombstone work.
//
// pgxmock asserts query STRINGS. Everything that actually decides whether #1569 is fixed is
// semantics: what `CASE WHEN pending` evaluates to, whether a row survives an UPDATE that used to
// be a DELETE, whether `deleted_at IS NULL` in ~70 separate queries genuinely hides the row, what
// the rule_execution_history foreign key does, and what `DELETE FROM users` still destroys. None of
// that is observable against a mock, so it is all here.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

type tombstoneFixture struct {
	tx           pgx.Tx
	repo         TransactionRepository
	userID       string
	linkedItemID string
	accountID    string

	pendingTxnID  string
	pendingExtID  string
	postedTxnID   string
	postedExtID   string
	liveTxnID     string
	liveExtID     string
	ruleID        string
	ruleExecID    string
	userAccountID string
}

// seedTombstoneFixture builds one user with three transactions on one live account:
//
//	pending — pending = true,  destined to become a SETTLED PENDING tombstone (never visible)
//	posted  — pending = false, destined to become a POSTED REVERSAL tombstone (surfacable),
//	          carrying user-authored work and a rule_execution_history row
//	live    — never tombstoned, the control that proves the filters exclude rather than empty
func seedTombstoneFixture(t *testing.T) *tombstoneFixture {
	t.Helper()
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	f := &tombstoneFixture{
		tx:            tx,
		userID:        uuid.New().String(),
		linkedItemID:  uuid.New().String(),
		accountID:     uuid.New().String(),
		userAccountID: uuid.New().String(),
		pendingTxnID:  uuid.New().String(),
		postedTxnID:   uuid.New().String(),
		liveTxnID:     uuid.New().String(),
		ruleID:        uuid.New().String(),
		ruleExecID:    uuid.New().String(),
	}
	f.pendingExtID = "plaid-tomb-pending-" + f.pendingTxnID
	f.postedExtID = "plaid-tomb-posted-" + f.postedTxnID
	f.liveExtID = "plaid-tomb-live-" + f.liveTxnID

	enc := newTestFieldEncryptor(t)
	f.repo = NewTransactionRepository(tx, enc)

	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, f.userID, "test|"+f.userID, "tombstone-1569-"+f.userID+"@example.com")
	require.NoError(t, err, "seed user")

	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1, $2, 'ins-tomb-1569', 'depository')
	`, f.userAccountID, f.userID)
	require.NoError(t, err, "seed user_account")

	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, f.linkedItemID, f.userID, "test-item-"+f.linkedItemID)
	require.NoError(t, err, "seed linked_item")

	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, user_account_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, $5, 'Tombstone Checking', 'depository', 'active')
	`, f.accountID, f.linkedItemID, f.userID, f.userAccountID, "plaid-acc-tomb-"+f.accountID)
	require.NoError(t, err, "seed account")

	date := time.Now().AddDate(0, 0, -3)
	notes := "reconciled this against the paper statement"
	overrideKind := "pfc"
	overridePrimary := "FOOD_AND_DRINK"
	reviewedAt := time.Now().Add(-time.Hour)

	txns := []domain.Transaction{
		{
			ID: f.pendingTxnID, UserID: f.userID, AccountID: f.accountID, LinkedItemID: f.linkedItemID,
			UserAccountID: &f.userAccountID,
			Source:        domain.SourcePlaid, ExternalTransactionID: &f.pendingExtID,
			Amount: 42.00, Date: date, Name: "Pending Coffee", Pending: true,
		},
		{
			ID: f.postedTxnID, UserID: f.userID, AccountID: f.accountID, LinkedItemID: f.linkedItemID,
			UserAccountID: &f.userAccountID,
			Source:        domain.SourcePlaid, ExternalTransactionID: &f.postedExtID,
			Amount: 99.00, Date: date, Name: "Posted Groceries", Pending: false,
			UserNotes:                &notes,
			CategoryOverrideKind:     &overrideKind,
			PFCPrimaryOverride:       &overridePrimary,
			CategorySourceField:      domain.CategorySourceManual,
			Reviewed:                 true,
			ReviewedAt:               &reviewedAt,
		},
		{
			ID: f.liveTxnID, UserID: f.userID, AccountID: f.accountID, LinkedItemID: f.linkedItemID,
			UserAccountID: &f.userAccountID,
			Source:        domain.SourcePlaid, ExternalTransactionID: &f.liveExtID,
			Amount: 7.00, Date: date, Name: "Live Sandwich", Pending: false,
		},
	}
	_, err = f.repo.CreateBatch(ctx, txns)
	require.NoError(t, err, "seed transactions")

	// tags/tags_source are deliberately absent from CreateBatch's INSERT column list — they are
	// user-authored and only ever preserved-on-conflict, never written by a sync — so the fixture
	// sets them the way the user's own tag endpoint does.
	_, err = tx.Exec(ctx, `
		UPDATE transactions
		SET tags = '["holiday","shared"]'::jsonb,
		    tags_source = '[{"tag":"holiday","source":"manual"},{"tag":"shared","source":"manual"}]'::jsonb
		WHERE id = $1
	`, f.postedTxnID)
	require.NoError(t, err, "seed user tags")

	_, err = tx.Exec(ctx, `
		INSERT INTO categorization_rules (id, user_id, name, conditions, target_kind, target_pfc_primary)
		VALUES ($1, $2, 'tombstone-test-rule', '[]'::jsonb, 'pfc', 'FOOD_AND_DRINK')
	`, f.ruleID, f.userID)
	require.NoError(t, err, "seed categorization_rule")

	_, err = tx.Exec(ctx, `
		INSERT INTO rule_execution_history (id, rule_id, transaction_id, action_type, result)
		VALUES ($1, $2, $3, 'category', 'applied')
	`, f.ruleExecID, f.ruleID, f.postedTxnID)
	require.NoError(t, err, "seed rule_execution_history")

	return f
}

func (f *tombstoneFixture) tombstoneBoth(t *testing.T, ctx context.Context) {
	t.Helper()
	pendingReason, err := f.repo.TombstoneByExternalID(
		ctx, domain.SourcePlaid, f.pendingExtID, f.userID, domain.TxnDeletedByPlaidSync)
	require.NoError(t, err)
	require.Equal(t, domain.TxnDeletedProviderRemovedPending, pendingReason,
		"a row removed while pending is the ordinary pending->posted lifecycle")

	postedReason, err := f.repo.TombstoneByExternalID(
		ctx, domain.SourcePlaid, f.postedExtID, f.userID, domain.TxnDeletedByPlaidSync)
	require.NoError(t, err)
	require.Equal(t, domain.TxnDeletedProviderRemovedPosted, postedReason,
		"a row removed after posting is a bank reversal")
}

// TestTombstone_ClassifiesFromPendingAndKeepsTheRow_Integration is the load-bearing claim: the
// removal no longer destroys anything, and the reason is derived from the row's own `pending`
// column by the UPDATE itself. Neither half is observable through pgxmock.
func TestTombstone_ClassifiesFromPendingAndKeepsTheRow_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedTombstoneFixture(t)
	f.tombstoneBoth(t, ctx)

	var rows int
	require.NoError(t, f.tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM transactions WHERE id = ANY($1::uuid[])`,
		[]string{f.pendingTxnID, f.postedTxnID}).Scan(&rows))
	assert.Equal(t, 2, rows, "both rows must SURVIVE the removal — this is the whole point of #1569")

	var deletedAt *time.Time
	var reason, by *string
	require.NoError(t, f.tx.QueryRow(ctx,
		`SELECT deleted_at, deleted_reason, deleted_by FROM transactions WHERE id = $1`,
		f.postedTxnID).Scan(&deletedAt, &reason, &by))
	require.NotNil(t, deletedAt)
	require.NotNil(t, reason)
	require.NotNil(t, by)
	assert.Equal(t, string(domain.TxnDeletedProviderRemovedPosted), *reason)
	assert.Equal(t, domain.TxnDeletedByPlaidSync, *by,
		"provenance: who tombstoned it, recorded for the same reason the purge path writes a "+
			"linked_item.history_purged audit row")
}

// TestTombstone_PreservesUserAuthoredWork_Integration. Categories, tags, notes and reviewed state
// have no other copy anywhere. The old DELETE destroyed all four, triggered by a provider event the
// user never caused.
func TestTombstone_PreservesUserAuthoredWork_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedTombstoneFixture(t)
	f.tombstoneBoth(t, ctx)

	var (
		notes, overrideKind, overridePrimary, categorySource *string
		tagsJSON                                             []byte
		reviewed                                             bool
	)
	require.NoError(t, f.tx.QueryRow(ctx, `
		SELECT user_notes, category_override_kind, pfc_primary_override, category_source, tags, reviewed
		FROM transactions WHERE id = $1
	`, f.postedTxnID).Scan(&notes, &overrideKind, &overridePrimary, &categorySource, &tagsJSON, &reviewed))

	require.NotNil(t, notes)
	assert.Equal(t, "reconciled this against the paper statement", *notes)
	require.NotNil(t, overrideKind)
	assert.Equal(t, "pfc", *overrideKind)
	require.NotNil(t, overridePrimary)
	assert.Equal(t, "FOOD_AND_DRINK", *overridePrimary)
	assert.JSONEq(t, `["holiday","shared"]`, string(tagsJSON))
	assert.True(t, reviewed, "the user's review decision must outlive the provider's removal")
}

// TestTombstone_RuleExecutionHistorySurvives_Integration covers migration 000157. Two separate
// things had to be true and only one of them is the FK flip: the tombstone must not reach the FK at
// all (it is an UPDATE), and a genuine hard delete must SET NULL rather than CASCADE.
func TestTombstone_RuleExecutionHistorySurvives_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedTombstoneFixture(t)
	f.tombstoneBoth(t, ctx)

	var linkedTxn *string
	require.NoError(t, f.tx.QueryRow(ctx,
		`SELECT transaction_id::text FROM rule_execution_history WHERE id = $1`, f.ruleExecID).Scan(&linkedTxn))
	require.NotNil(t, linkedTxn, "a tombstone is an UPDATE and must not disturb the FK at all")
	assert.Equal(t, f.postedTxnID, *linkedTxn)

	// Now the FK itself: a hard delete must leave the audit row behind, orphaned but intact.
	_, err := f.tx.Exec(ctx, `DELETE FROM transactions WHERE id = $1`, f.postedTxnID)
	require.NoError(t, err)

	var count int
	require.NoError(t, f.tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM rule_execution_history WHERE id = $1 AND transaction_id IS NULL`,
		f.ruleExecID).Scan(&count))
	assert.Equal(t, 1, count,
		"migration 000020's ON DELETE CASCADE destroyed the automation trail with the transaction; "+
			"000157 makes it ON DELETE SET NULL so the record of which rule fired survives")
}

// TestTombstone_HiddenFromEveryRead_Integration is the constraint the brief calls non-negotiable.
//
// The reads are named individually rather than counted, so that a future method is a compile-time
// addition to this list rather than a silent gap. TestNoTransactionReadForgetsTheTombstoneFilter is
// the mechanical half (no query FORGETS the predicate); this is the behavioural half (the predicate
// actually hides the row).
func TestTombstone_HiddenFromEveryRead_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedTombstoneFixture(t)
	f.tombstoneBoth(t, ctx)

	hidden := map[string]bool{f.pendingTxnID: true, f.postedTxnID: true}

	assertHidden := func(name string, got []domain.Transaction, err error) {
		t.Helper()
		require.NoError(t, err, name)
		sawLive := false
		for _, tr := range got {
			assert.False(t, hidden[tr.ID], "%s returned tombstoned transaction %s", name, tr.ID)
			if tr.ID == f.liveTxnID {
				sawLive = true
			}
		}
		assert.True(t, sawLive,
			"%s must still return the live control row — a read that returns NOTHING would pass "+
				"the exclusion assertion vacuously", name)
	}

	byUser, err := f.repo.GetByUserID(ctx, f.userID, 100, 0)
	assertHidden("GetByUserID", byUser, err)

	byAccount, err := f.repo.GetByAccountID(ctx, f.accountID)
	assertHidden("GetByAccountID", byAccount, err)

	bySource, err := f.repo.GetBySource(ctx, domain.SourcePlaid, f.userID)
	assertHidden("GetBySource", bySource, err)

	byFilters, err := f.repo.GetByUserIDWithFilters(ctx, f.userID, domain.TransactionFilters{}, 100, 0)
	assertHidden("GetByUserIDWithFilters", byFilters, err)

	byUserAccount, err := f.repo.GetByUserAccountID(ctx, f.userID, f.userAccountID)
	assertHidden("GetByUserAccountID", byUserAccount, err)

	keyset, _, err := f.repo.GetByUserIDKeyset(ctx, f.userID, domain.TransactionFilters{}, nil, 100)
	assertHidden("GetByUserIDKeyset", keyset, err)

	// The unified feed is the one the brief singles out: retainedAccountVisibilityLeg deliberately
	// WIDENS which accounts' rows are visible, so it is the query most able to resurrect a
	// tombstone, not the least.
	items, _, err := f.repo.GetByUserIDKeysetUnified(ctx, f.userID, domain.TransactionFilters{}, nil, 100)
	require.NoError(t, err)
	sawLiveInFeed := false
	for _, it := range items {
		assert.False(t, hidden[it.ID], "GetByUserIDKeysetUnified returned tombstoned transaction %s", it.ID)
		if it.ID == f.liveTxnID {
			sawLiveInFeed = true
		}
	}
	assert.True(t, sawLiveInFeed, "the unified feed must still return the live control row")

	count, err := f.repo.CountByUserID(ctx, f.userID, domain.TransactionFilters{})
	require.NoError(t, err)
	assert.Equal(t, 1, count, "CountByUserID must agree with the list it paginates, or the UI shows "+
		"a page count for rows it will never render")

	_, err = f.repo.GetByID(ctx, f.postedTxnID)
	assert.ErrorIs(t, err, ErrTransactionNotFound, "GetByID must not resolve a tombstoned row")

	_, err = f.repo.GetByExternalID(ctx, domain.SourcePlaid, f.postedExtID, f.userID)
	assert.ErrorIs(t, err, ErrTransactionNotFound, "GetByExternalID must not resolve a tombstoned row")

	inflows, err := f.repo.GetRecentInflowsByUserID(ctx, f.userID, time.Now().AddDate(0, 0, -30))
	require.NoError(t, err)
	for _, in := range inflows {
		assert.False(t, hidden[in.ID], "GetRecentInflowsByUserID returned tombstoned transaction %s", in.ID)
	}

	summary, err := f.repo.GetReviewSummary(ctx, f.userID, domain.ReviewSummaryFilters{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), summary.UnreviewedCount,
		"the review inbox counts rows the user can act on; the two tombstoned rows are not among "+
			"them, and the tombstoned POSTED row was reviewed=true anyway — only the live control "+
			"row is genuinely unreviewed")
}

// TestTombstone_SettledPendingIsNeverSurfaced_Integration. IncludeReversed is the ONLY door out of
// the tombstone filter, and it must not open for a settled pending: that row's posted twin is
// already in the table, so surfacing it double-counts real money.
func TestTombstone_SettledPendingIsNeverSurfaced_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedTombstoneFixture(t)
	f.tombstoneBoth(t, ctx)

	filters := domain.TransactionFilters{IncludeReversed: true}

	got, err := f.repo.GetByUserIDWithFilters(ctx, f.userID, filters, 100, 0)
	require.NoError(t, err)

	ids := map[string]domain.Transaction{}
	for _, tr := range got {
		ids[tr.ID] = tr
	}
	assert.NotContains(t, ids, f.pendingTxnID,
		"a settled pending must be visible NOWHERE, even to a caller that asked for reversals")
	require.Contains(t, ids, f.postedTxnID, "the posted reversal is exactly what IncludeReversed is for")
	assert.Contains(t, ids, f.liveTxnID, "opting into reversals must not drop live rows")

	reversal := ids[f.postedTxnID]
	live := ids[f.liveTxnID]
	assert.True(t, reversal.Reversed(), "the API has to be able to say THIS WAS REVERSED")
	require.NotNil(t, reversal.DeletedAt)
	assert.False(t, live.Reversed(), "a live row is not a reversal")

	items, _, err := f.repo.GetByUserIDKeysetUnified(ctx, f.userID, filters, nil, 100)
	require.NoError(t, err)
	feedIDs := map[string]bool{}
	for _, it := range items {
		feedIDs[it.ID] = true
	}
	assert.False(t, feedIDs[f.pendingTxnID], "the unified feed must not surface a settled pending either")
	assert.True(t, feedIDs[f.postedTxnID], "the unified feed must be able to surface a posted reversal")
}

// TestTombstone_IsIdempotent_Integration. #1570's storage-before-cursor contract makes re-delivery
// of an already-processed page the NORMAL consequence of a failed sync, so a repeat must be a
// recognisable no-op — and must not move the recorded removal time away from when it happened.
func TestTombstone_IsIdempotent_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedTombstoneFixture(t)
	f.tombstoneBoth(t, ctx)

	var first time.Time
	require.NoError(t, f.tx.QueryRow(ctx,
		`SELECT deleted_at FROM transactions WHERE id = $1`, f.postedTxnID).Scan(&first))

	_, err := f.repo.TombstoneByExternalID(
		ctx, domain.SourcePlaid, f.postedExtID, f.userID, domain.TxnDeletedByPlaidSync)
	assert.ErrorIs(t, err, ErrTransactionNotFound,
		"a re-delivered removal has no LIVE row to act on, and the caller relies on telling that "+
			"apart from a database failure")

	var second time.Time
	require.NoError(t, f.tx.QueryRow(ctx,
		`SELECT deleted_at FROM transactions WHERE id = $1`, f.postedTxnID).Scan(&second))
	assert.True(t, first.Equal(second), "a repeat must not restamp deleted_at")
}

// TestTombstone_UpsertDoesNotResurrect_Integration is the specific risk the residual-(3) decision
// had to clear before it could be taken: processModifiedTransactions now UPSERTS a modification
// whose row it cannot find, and GetByExternalID cannot find a TOMBSTONED row, so a modification
// arriving for a tombstoned transaction reaches CreateBatch.
//
// This asserts what CreateBatch actually does to it, rather than reasoning from its SET list: the
// conflict fires on the tombstoned row, the provider-owned fields update, and deleted_at survives.
func TestTombstone_UpsertDoesNotResurrect_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedTombstoneFixture(t)
	f.tombstoneBoth(t, ctx)

	newName := "Posted Groceries (corrected)"
	_, err := f.repo.CreateBatch(ctx, []domain.Transaction{{
		ID: uuid.New().String(), UserID: f.userID, AccountID: f.accountID, LinkedItemID: f.linkedItemID,
		Source: domain.SourcePlaid, ExternalTransactionID: &f.postedExtID,
		Amount: 88.00, Date: time.Now().AddDate(0, 0, -3), Name: newName, Pending: false,
	}})
	require.NoError(t, err)

	var rowCount int
	require.NoError(t, f.tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM transactions
		WHERE user_id = $1 AND source = 'plaid' AND name LIKE 'Posted Groceries%'
	`, f.userID).Scan(&rowCount))
	assert.Equal(t, 1, rowCount, "the upsert must conflict onto the tombstoned row, not insert beside it")

	var deletedAt *time.Time
	var name, notes *string
	require.NoError(t, f.tx.QueryRow(ctx,
		`SELECT deleted_at, name, user_notes FROM transactions WHERE id = $1`,
		f.postedTxnID).Scan(&deletedAt, &name, &notes))
	assert.NotNil(t, deletedAt, "the tombstone must survive the upsert — otherwise a modification "+
		"silently resurrects a removed transaction")
	require.NotNil(t, name)
	assert.Equal(t, newName, *name, "provider-owned fields still update")
	require.NotNil(t, notes)
	assert.Equal(t, "reconciled this against the paper statement", *notes, "user work still preserved")
}

// TestTombstone_GDPRErasureStillDestroysIt_Integration. Retention must never become an accidental
// exemption from erasure. Erasure is `DELETE FROM users` (profile_repository DeleteUserCascade) and
// reaches transactions through transactions.user_id -> users(id) ON DELETE CASCADE, which a
// tombstone does not touch — but "does not touch" is exactly the kind of claim this task's brief
// says must be executed rather than asserted.
//
// It also covers the 000157 flip's one plausible GDPR hazard: rule_execution_history now SET NULLs
// on transaction delete instead of cascading, so it must still be erased through its OTHER FK
// (rule_id -> categorization_rules -> users, both CASCADE).
func TestTombstone_GDPRErasureStillDestroysIt_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedTombstoneFixture(t)
	f.tombstoneBoth(t, ctx)

	// DeleteUserCascade re-points cross-user references first; none exist in this fixture, so the
	// `DELETE FROM users` it ends with is the whole of the erasure for these rows.
	_, err := f.tx.Exec(ctx, `UPDATE transactions SET user_account_id = NULL WHERE user_id = $1`, f.userID)
	require.NoError(t, err)
	_, err = f.tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, f.userID)
	require.NoError(t, err, "erasure must not be blocked by a tombstoned row")

	var txnRows int
	require.NoError(t, f.tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM transactions WHERE id = ANY($1::uuid[])`,
		[]string{f.pendingTxnID, f.postedTxnID, f.liveTxnID}).Scan(&txnRows))
	assert.Zero(t, txnRows, "a tombstoned transaction is still the user's personal data and must be erased")

	var execRows int
	require.NoError(t, f.tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM rule_execution_history WHERE id = $1`, f.ruleExecID).Scan(&execRows))
	assert.Zero(t, execRows,
		"SET NULL on transaction_id must not strand the audit row: it is erased through "+
			"rule_id -> categorization_rules -> users, both ON DELETE CASCADE")
}
