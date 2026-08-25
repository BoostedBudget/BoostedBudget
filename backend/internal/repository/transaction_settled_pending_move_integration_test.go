//go:build integration

package repository

// Issue #1569 Task 4 (#1578) — the user's work must follow a pending transaction to its posted twin.
//
// THE DEFECT THIS FILE PINS. When a pending charge settles, Plaid removes the pending transaction
// and delivers the posted one as a brand-new row with a different transaction_id. Task 7 made the
// removal a tombstone instead of a DELETE, so the row survives — but a settled pending renders
// NOWHERE (deleted_reason = provider_removed_pending; see isSettledPending in
// apps/web/src/features/transactions/types.ts). So before this task, every category override, note,
// tag and `reviewed` flag the user put on a pending charge ended up on a row nothing will ever show,
// and the posted twin arrived blank. That happened on every pending transaction anybody annotated.
//
// WHY THESE ARE INTEGRATION TESTS AND NOT pgxmock ONES. Everything that decides whether the fix
// works is semantics a mock cannot evaluate: whether a data-modifying CTE's second UPDATE sees the
// first statement's pre-update snapshot, what COALESCE does to a re-delivered NULL pointer, whether
// a JSONB tags array is empty, and whether the tenancy predicate actually excludes the other user.

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

type settlementFixture struct {
	tx   pgx.Tx
	repo TransactionRepository
	enc  interface{ BlindIndex(string) string }

	userID       string
	linkedItemID string
	accountID    string

	pendingID    string
	pendingExtID string
	postedID     string
	postedExtID  string
}

// seedSettlementFixture builds one user, one account and ONE pending transaction carrying the full
// annotation set the user can author. The posted twin is NOT created here: each test adds it the
// way the sync path that test is about would add it.
func seedSettlementFixture(t *testing.T) *settlementFixture {
	t.Helper()
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	enc := newTestFieldEncryptor(t)
	f := &settlementFixture{
		tx:           tx,
		enc:          enc,
		repo:         NewTransactionRepository(tx, enc),
		userID:       uuid.New().String(),
		linkedItemID: uuid.New().String(),
		accountID:    uuid.New().String(),
		pendingID:    uuid.New().String(),
		postedID:     uuid.New().String(),
	}
	f.pendingExtID = "plaid-settle-pending-" + f.pendingID
	f.postedExtID = "plaid-settle-posted-" + f.postedID

	f.seedUser(t, ctx, f.userID, f.linkedItemID, f.accountID)

	date := time.Now().AddDate(0, 0, -2)
	notes := "split with Sam, they owe me half"

	// The row arrives from Plaid the way a real one does: no override, category_source 'plaid',
	// unreviewed. user_notes is the one user-authored column CreateBatch genuinely writes.
	_, err = f.repo.CreateBatch(ctx, []domain.Transaction{{
		ID: f.pendingID, UserID: f.userID, AccountID: f.accountID, LinkedItemID: f.linkedItemID,
		Source: domain.SourcePlaid, ExternalTransactionID: &f.pendingExtID,
		Amount: 42.00, Date: date, Name: "Pending Coffee", Pending: true,
		PFCPrimary: strPtr("GENERAL_SERVICES"),
		UserNotes:  &notes,
	}})
	require.NoError(t, err, "seed pending transaction")

	// ⚠ THE USER'S CATEGORY IS SET THROUGH THE PRODUCTION WRITE PATH, NOT HAND-BUILT.
	//
	// An earlier version of this fixture manufactured the manual category by passing the override
	// columns straight to CreateBatch. That produced a row state PRODUCTION CANNOT PRODUCE -- a
	// manual category with a NULL category_override_date -- because every user-facing category
	// write goes through UpdateCategory, which sets category_override_date, reviewed and
	// reviewed_at in the same statement. The consequence was not a wrong assertion but a MISSING
	// one: mutating category_override_date out of the move survived, because the fixture's value
	// was NULL either way. Same vacuity class as writing a bare literal into an encrypted column.
	//
	// UpdateCategory also fills effective_category_name by resolving the category, so the fixture
	// no longer asserts a denormalised name it wrote itself.
	require.NoError(t, f.repo.UpdateCategory(ctx, f.pendingID, f.userID,
		strPtr("pfc"), strPtr("FOOD_AND_DRINK"), nil, nil, domain.CategorySourceManual),
		"seed the user's own category on the pending row")

	// tags/tags_source are NOT in CreateBatch's INSERT column list (a pre-existing gap this task
	// deliberately does not inherit — see the note on TombstoneByExternalID), so the fixture writes
	// them the way the user's own tag endpoint does, via UpdateTags.
	require.NoError(t, f.repo.UpdateTags(ctx, f.pendingID, f.userID,
		[]string{"holiday", "shared"},
		[]domain.TagSource{{Tag: "holiday", Source: "manual"}, {Tag: "shared", Source: "manual"}}),
		"seed user tags on the pending row")

	return f
}

func (f *settlementFixture) seedUser(t *testing.T, ctx context.Context, userID, linkedItemID, accountID string) {
	t.Helper()
	_, err := f.tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID, "settle-1578-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	_, err = f.tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, "test-item-"+linkedItemID)
	require.NoError(t, err, "seed linked_item")

	_, err = f.tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Settlement Checking', 'depository', 'active')
	`, accountID, linkedItemID, userID, "plaid-acc-settle-"+accountID)
	require.NoError(t, err, "seed account")
}

// addPostedTwin inserts the posted transaction exactly as processAddedTransactions would: through
// CreateBatch, with PendingTransactionID carrying the PLAINTEXT id of the pending row it replaces.
// The repository is what turns that into a blind index — the fixture never writes the derived
// column itself, so a broken derivation cannot be papered over here.
func (f *settlementFixture) addPostedTwin(t *testing.T, ctx context.Context, twin domain.Transaction) {
	t.Helper()
	if twin.ID == "" {
		twin.ID = f.postedID
	}
	if twin.UserID == "" {
		twin.UserID = f.userID
	}
	twin.AccountID = f.accountID
	twin.LinkedItemID = f.linkedItemID
	twin.Source = domain.SourcePlaid
	if twin.ExternalTransactionID == nil {
		twin.ExternalTransactionID = &f.postedExtID
	}
	if twin.Date.IsZero() {
		twin.Date = time.Now().AddDate(0, 0, -1)
	}
	if twin.Name == "" {
		twin.Name = "Posted Coffee"
	}
	if twin.Amount == 0 {
		twin.Amount = 42.00
	}
	_, err := f.repo.CreateBatch(ctx, []domain.Transaction{twin})
	require.NoError(t, err, "insert posted twin")
}

func (f *settlementFixture) settle(t *testing.T, ctx context.Context) {
	t.Helper()
	reason, err := f.repo.TombstoneByExternalID(
		ctx, domain.SourcePlaid, f.pendingExtID, f.userID, domain.TxnDeletedByPlaidSync)
	require.NoError(t, err)
	require.Equal(t, domain.TxnDeletedProviderRemovedPending, reason,
		"a row Plaid removes while it is still pending is a settlement, not a reversal")
}

func (f *settlementFixture) overrideDate(t *testing.T, ctx context.Context, txnID string) *time.Time {
	t.Helper()
	var d *time.Time
	require.NoError(t, f.tx.QueryRow(ctx,
		`SELECT category_override_date FROM transactions WHERE id = $1`, txnID).Scan(&d))
	return d
}

func (f *settlementFixture) storedPointer(t *testing.T, ctx context.Context, txnID string) *string {
	t.Helper()
	var ptr *string
	require.NoError(t, f.tx.QueryRow(ctx,
		`SELECT pending_transaction_id_blind_idx FROM transactions WHERE id = $1`, txnID).Scan(&ptr))
	return ptr
}

// TestSettledPending_AnnotationsMoveToPostedTwin_Integration is the headline claim of #1578: all
// nine-plus preserved columns land on the row the user can actually see.
func TestSettledPending_AnnotationsMoveToPostedTwin_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedSettlementFixture(t)

	f.addPostedTwin(t, ctx, domain.Transaction{PendingTransactionID: &f.pendingExtID})
	f.settle(t, ctx)

	posted, err := f.repo.GetByID(ctx, f.postedID)
	require.NoError(t, err, "the posted twin must still be live and readable")

	require.NotNil(t, posted.UserNotes, "the note the user wrote on the pending charge")
	assert.Equal(t, "split with Sam, they owe me half", *posted.UserNotes)

	require.NotNil(t, posted.CategoryOverrideKind)
	assert.Equal(t, "pfc", *posted.CategoryOverrideKind)
	require.NotNil(t, posted.PFCPrimaryOverride)
	assert.Equal(t, "FOOD_AND_DRINK", *posted.PFCPrimaryOverride)
	assert.Equal(t, domain.CategorySourceManual, posted.CategorySourceField,
		"the category is still the USER's, not Plaid's — losing this is what makes the bug visible")
	require.NotNil(t, posted.EffectiveCategoryName)
	assert.Equal(t, "FOOD_AND_DRINK", *posted.EffectiveCategoryName)

	assert.ElementsMatch(t, []string{"holiday", "shared"}, posted.Tags)
	require.Len(t, posted.TagsSource, 2, "tags_source must travel with tags or the tags lose provenance")
	for _, ts := range posted.TagsSource {
		assert.Equal(t, "manual", ts.Source)
	}

	assert.True(t, posted.Reviewed, "the user already reviewed this charge; settling it is not a reason to ask again")
	require.NotNil(t, posted.ReviewedAt)

	// category_override_date is the twelfth moved column and the one a hand-built fixture cannot
	// pin: UpdateCategory sets it, CreateBatch never does. Losing it would leave the twin claiming
	// a manual category the user apparently never dated.
	require.NotNil(t, posted.CategoryOverrideDate,
		"the date the user filed this charge under their own category")
	pendingDate := f.overrideDate(t, ctx, f.pendingID)
	require.NotNil(t, pendingDate)
	assert.WithinDuration(t, *pendingDate, *posted.CategoryOverrideDate, time.Millisecond,
		"the moved date must be the one the user's own edit recorded, not a fresh NOW()")

	pending := f.readAnyRow(t, ctx, f.pendingID)
	require.NotNil(t, pending.DeletedAt, "the pending row is still tombstoned")
	require.NotNil(t, pending.DeletedReason)
	assert.Equal(t, domain.TxnDeletedProviderRemovedPending, *pending.DeletedReason)
}

// readAnyRow reads a transaction WITHOUT the tombstone filter every production read carries. It
// exists only so the tests can inspect a tombstoned row's own state; no production path may do this.
func (f *settlementFixture) readAnyRow(t *testing.T, ctx context.Context, id string) domain.Transaction {
	t.Helper()
	var tx domain.Transaction
	var deletedReason *string
	require.NoError(t, f.tx.QueryRow(ctx, `
		SELECT user_notes, category_source, reviewed, deleted_at, deleted_reason
		FROM transactions WHERE id = $1
	`, id).Scan(&tx.UserNotes, &tx.CategorySourceField, &tx.Reviewed, &tx.DeletedAt, &deletedReason))
	if deletedReason != nil {
		r := domain.TransactionDeletedReason(*deletedReason)
		tx.DeletedReason = &r
	}
	return tx
}

// TestSettledPending_TombstonedRowStillRendersNowhere_Integration guards the invariant the move
// must not break. Surfacing the settled pending after moving its annotations would double-count the
// same spend against its twin — which is worse than the bug being fixed.
func TestSettledPending_TombstonedRowStillRendersNowhere_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedSettlementFixture(t)

	f.addPostedTwin(t, ctx, domain.Transaction{PendingTransactionID: &f.pendingExtID})
	f.settle(t, ctx)

	byUser, err := f.repo.GetByUserID(ctx, f.userID, 100, 0)
	require.NoError(t, err)
	assertAbsent(t, byUser, f.pendingID, "GetByUserID")
	assertPresent(t, byUser, f.postedID, "GetByUserID")

	// IncludeReversed is the ONE opt-in that widens a read to tombstones. It admits posted
	// reversals only; a settled pending must stay invisible even here.
	reversed, err := f.repo.GetByUserIDWithFilters(ctx, f.userID,
		domain.TransactionFilters{IncludeReversed: true}, 100, 0)
	require.NoError(t, err)
	assertAbsent(t, reversed, f.pendingID, "GetByUserIDWithFilters(IncludeReversed)")

	_, err = f.repo.GetByID(ctx, f.pendingID)
	assert.ErrorIs(t, err, ErrTransactionNotFound, "GetByID must not resurrect the settled pending")
}

func assertAbsent(t *testing.T, txns []domain.Transaction, id, where string) {
	t.Helper()
	for _, tx := range txns {
		if tx.ID == id {
			t.Fatalf("%s returned the settled pending row %s; it must render nowhere", where, id)
		}
	}
}

func assertPresent(t *testing.T, txns []domain.Transaction, id, where string) {
	t.Helper()
	for _, tx := range txns {
		if tx.ID == id {
			return
		}
	}
	t.Fatalf("%s did not return %s", where, id)
}

// TestSettledPending_PointerArrivingOnALaterDeliveryIsAccepted_Integration is Codex's finding. If
// the pointer column is left out of the ON CONFLICT ... DO UPDATE SET clause, a posted row that was
// already stored can NEVER learn a pointer Plaid sends later, and the move can never find it. The
// competing hazard — a re-delivery clobbering a stored pointer with NULL — is closed by COALESCE
// rather than by omitting the column, and both halves are asserted here.
func TestSettledPending_PointerArrivingOnALaterDeliveryIsAccepted_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedSettlementFixture(t)

	// Delivery 1: the posted row arrives with NO pointer.
	f.addPostedTwin(t, ctx, domain.Transaction{})
	require.Nil(t, f.storedPointer(t, ctx, f.postedID), "delivery 1 carried no pointer")

	// Delivery 2: the same posted row arrives again, now carrying the pointer.
	f.addPostedTwin(t, ctx, domain.Transaction{ID: uuid.New().String(), PendingTransactionID: &f.pendingExtID})
	require.NotNil(t, f.storedPointer(t, ctx, f.postedID),
		"an existing posted row must be able to LEARN a pointer that arrives later")
	assert.Equal(t, f.enc.BlindIndex(f.pendingExtID), *f.storedPointer(t, ctx, f.postedID))

	// Delivery 3: a re-delivery with no pointer must not erase what delivery 2 taught it.
	f.addPostedTwin(t, ctx, domain.Transaction{ID: uuid.New().String()})
	require.NotNil(t, f.storedPointer(t, ctx, f.postedID),
		"a re-delivery carrying no pointer must not clobber the stored one")

	f.settle(t, ctx)

	posted, err := f.repo.GetByID(ctx, f.postedID)
	require.NoError(t, err)
	require.NotNil(t, posted.UserNotes, "the late-learned pointer must still find its target")
	assert.Equal(t, domain.CategorySourceManual, posted.CategorySourceField)
}

// TestSettledPending_UpdatePathAlsoLearnsThePointer_Integration covers the `modified` array, which
// does NOT go through CreateBatch: processModifiedTransactions calls Update. A posted twin whose
// pointer only ever arrives on a `modified` delivery would otherwise be unreachable by the move.
func TestSettledPending_UpdatePathAlsoLearnsThePointer_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedSettlementFixture(t)

	f.addPostedTwin(t, ctx, domain.Transaction{})
	require.Nil(t, f.storedPointer(t, ctx, f.postedID))

	stored, err := f.repo.GetByID(ctx, f.postedID)
	require.NoError(t, err)
	stored.PendingTransactionID = &f.pendingExtID
	require.NoError(t, f.repo.Update(ctx, stored))

	require.NotNil(t, f.storedPointer(t, ctx, f.postedID),
		"Update must be able to teach a stored row its pending pointer")

	// And Update must not erase it either, for the same reason CreateBatch must not.
	stored.PendingTransactionID = nil
	require.NoError(t, f.repo.Update(ctx, stored))
	require.NotNil(t, f.storedPointer(t, ctx, f.postedID),
		"a later Update carrying no pointer must not clobber the stored one")

	f.settle(t, ctx)
	posted, err := f.repo.GetByID(ctx, f.postedID)
	require.NoError(t, err)
	require.NotNil(t, posted.UserNotes)
}

// TestSettledPending_MoveIsTenancyScoped_Integration: another user's row carrying the SAME pointer
// value must receive nothing. The pointer is a provider id, not an ownership claim.
func TestSettledPending_MoveIsTenancyScoped_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedSettlementFixture(t)

	otherUser := uuid.New().String()
	otherItem := uuid.New().String()
	otherAccount := uuid.New().String()
	f.seedUser(t, ctx, otherUser, otherItem, otherAccount)

	otherID := uuid.New().String()
	otherExtID := "plaid-settle-other-" + otherID
	_, err := f.repo.CreateBatch(ctx, []domain.Transaction{{
		ID: otherID, UserID: otherUser, AccountID: otherAccount, LinkedItemID: otherItem,
		Source: domain.SourcePlaid, ExternalTransactionID: &otherExtID,
		Amount: 42.00, Date: time.Now().AddDate(0, 0, -1), Name: "Someone Else's Coffee",
		PendingTransactionID: &f.pendingExtID,
	}})
	require.NoError(t, err, "seed the other tenant's row")

	f.addPostedTwin(t, ctx, domain.Transaction{PendingTransactionID: &f.pendingExtID})
	f.settle(t, ctx)

	other, err := f.repo.GetByID(ctx, otherID)
	require.NoError(t, err)
	assert.Nil(t, other.UserNotes, "another tenant's note must never arrive from this user's pending row")
	assert.Empty(t, other.Tags)
	assert.False(t, other.Reviewed)
	assert.Equal(t, domain.CategorySourcePlaid, other.CategorySourceField)
}

// TestSettledPending_PostedTwinsOwnManualWorkWins_Integration pins the refusal-to-overwrite rule
// from the design note: the move FILLS a blank row, it does not overwrite the user's newer work.
func TestSettledPending_PostedTwinsOwnManualWorkWins_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedSettlementFixture(t)

	twinNotes := "already sorted this one on the posted row"
	twinKind := "pfc"
	twinPrimary := "TRANSPORTATION"
	f.addPostedTwin(t, ctx, domain.Transaction{
		PendingTransactionID: &f.pendingExtID,
		UserNotes:            &twinNotes,
		CategoryOverrideKind: &twinKind,
		PFCPrimaryOverride:   &twinPrimary,
		CategorySourceField:  domain.CategorySourceManual,
	})
	require.NoError(t, f.repo.UpdateTags(ctx, f.postedID, f.userID,
		[]string{"commute"}, []domain.TagSource{{Tag: "commute", Source: "manual"}}))

	f.settle(t, ctx)

	posted, err := f.repo.GetByID(ctx, f.postedID)
	require.NoError(t, err)
	require.NotNil(t, posted.UserNotes)
	assert.Equal(t, twinNotes, *posted.UserNotes, "the twin's own note must win")
	require.NotNil(t, posted.PFCPrimaryOverride)
	assert.Equal(t, "TRANSPORTATION", *posted.PFCPrimaryOverride, "the twin's own category must win")
	assert.ElementsMatch(t, []string{"commute"}, posted.Tags, "the twin's own tags must win")
}

// TestSettledPending_RuleAppliedTwinYieldsToTheUsersOwnCategory_Integration. Rules run in-place
// BEFORE CreateBatch (processAddedTransactions), so the posted twin routinely arrives already
// categorised by a rule with category_source = 'rule'. A blank-row-only gate would refuse the move
// in exactly the common case, so the gate is "no MANUAL work on the twin", not "no work at all":
// the user outranks a rule, which is the precedence category_source exists to express.
func TestSettledPending_RuleAppliedTwinYieldsToTheUsersOwnCategory_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedSettlementFixture(t)

	ruleKind := "pfc"
	rulePrimary := "GENERAL_MERCHANDISE"
	f.addPostedTwin(t, ctx, domain.Transaction{
		PendingTransactionID: &f.pendingExtID,
		CategoryOverrideKind: &ruleKind,
		PFCPrimaryOverride:   &rulePrimary,
		CategorySourceField:  domain.CategorySourceRule,
		Reviewed:             true,
	})

	f.settle(t, ctx)

	posted, err := f.repo.GetByID(ctx, f.postedID)
	require.NoError(t, err)
	require.NotNil(t, posted.PFCPrimaryOverride)
	assert.Equal(t, "FOOD_AND_DRINK", *posted.PFCPrimaryOverride,
		"the user's own category on the pending row outranks a rule's category on the twin")
	assert.Equal(t, domain.CategorySourceManual, posted.CategorySourceField)
	require.NotNil(t, posted.UserNotes)
}

// TestSettledPending_NoTwinLeavesTheTombstoneUntouched_Integration. A removal with no posted twin
// stored is a plain tombstone; nothing may be written anywhere, and the call must still succeed.
func TestSettledPending_NoTwinLeavesTheTombstoneUntouched_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedSettlementFixture(t)

	f.settle(t, ctx)

	pending := f.readAnyRow(t, ctx, f.pendingID)
	require.NotNil(t, pending.DeletedReason)
	assert.Equal(t, domain.TxnDeletedProviderRemovedPending, *pending.DeletedReason)
	require.NotNil(t, pending.UserNotes, "the tombstoned row keeps its own copy; the move copies, it does not cut")
}

// TestPostedReversal_DoesNotMoveAnything_Integration. Only a SETTLED PENDING has a twin. A posted
// reversal is a distinct event and must not donate its annotations to anything.
func TestPostedReversal_DoesNotMoveAnything_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedSettlementFixture(t)

	// Make the "pending" row posted, so its removal classifies as a reversal.
	_, err := f.tx.Exec(ctx, `UPDATE transactions SET pending = false WHERE id = $1`, f.pendingID)
	require.NoError(t, err)

	f.addPostedTwin(t, ctx, domain.Transaction{PendingTransactionID: &f.pendingExtID})

	reason, err := f.repo.TombstoneByExternalID(
		ctx, domain.SourcePlaid, f.pendingExtID, f.userID, domain.TxnDeletedByPlaidSync)
	require.NoError(t, err)
	require.Equal(t, domain.TxnDeletedProviderRemovedPosted, reason)

	posted, err := f.repo.GetByID(ctx, f.postedID)
	require.NoError(t, err)
	assert.Nil(t, posted.UserNotes, "a posted reversal has no settled-pending twin to donate to")
	assert.Empty(t, posted.Tags)
}

// TestSettledPending_AlreadyTombstonedTwinReceivesNothing_Integration closes F1. The move's
// `twin.deleted_at IS NULL` predicate had no coverage of ANY kind before this test: the static
// audit (TestNoTransactionReadForgetsTheTombstoneFilter) never inspects an UPDATE's target table,
// and no behavioural test built an already-tombstoned twin -- so deleting the predicate left the
// whole package green while the code comment above it said "do not remove".
//
// What it protects: donating the user's work onto a row that is ITSELF tombstoned writes it to a
// second invisible row. For a settled-pending twin that is the original bug reproduced one hop
// along; for a posted-reversal twin it silently re-files a reversal under a category the user
// chose for a different charge.
func TestSettledPending_AlreadyTombstonedTwinReceivesNothing_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedSettlementFixture(t)

	f.addPostedTwin(t, ctx, domain.Transaction{PendingTransactionID: &f.pendingExtID})

	// The twin is removed by the provider first -- a reversal, so it is a real tombstone with a
	// real reason, not a hand-written deleted_at.
	reason, err := f.repo.TombstoneByExternalID(
		ctx, domain.SourcePlaid, f.postedExtID, f.userID, domain.TxnDeletedByPlaidSync)
	require.NoError(t, err)
	require.Equal(t, domain.TxnDeletedProviderRemovedPosted, reason,
		"the twin must be tombstoned as a posted reversal before the pending settles")

	f.settle(t, ctx)

	twin := f.readAnyRow(t, ctx, f.postedID)
	require.NotNil(t, twin.DeletedAt, "the twin is still tombstoned")
	assert.Nil(t, twin.UserNotes,
		"the user's note must not be written onto an already-tombstoned row -- it would be just as invisible there")
	assert.Equal(t, domain.CategorySourcePlaid, twin.CategorySourceField,
		"the user's category must not be written onto an already-tombstoned row")
	assert.False(t, twin.Reviewed)
}

// TestSettledPending_RuleDerivedReviewDoesNotPropagate_Integration closes F4. `reviewed` was the one
// moved column with no refusal gate, and it is not always the user's: a rule applying a category
// sets category_source = 'rule' AND reviewed = true in the same block
// (rule_application_service.go). Without the gate, a rule's review on the pending row would mark
// the twin reviewed even where the category gate refused -- including a twin the user had
// deliberately un-reviewed.
func TestSettledPending_RuleDerivedReviewDoesNotPropagate_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedSettlementFixture(t)

	// Reshape the pending row into what ApplyRulesToBatch produces: a rule category, reviewed.
	// Its notes and tags are cleared so `reviewed` is the ONLY thing that could move -- otherwise
	// the move would run for another reason and the assertion would not isolate this gate.
	_, err := f.tx.Exec(ctx, `
		UPDATE transactions
		SET category_source = 'rule', reviewed = true, reviewed_at = NOW(),
		    user_notes = NULL, tags = '[]'::jsonb, tags_source = '[]'::jsonb
		WHERE id = $1
	`, f.pendingID)
	require.NoError(t, err)

	// The user has deliberately un-reviewed the twin.
	f.addPostedTwin(t, ctx, domain.Transaction{PendingTransactionID: &f.pendingExtID})
	_, err = f.tx.Exec(ctx,
		`UPDATE transactions SET reviewed = false, reviewed_at = NULL WHERE id = $1`, f.postedID)
	require.NoError(t, err)

	f.settle(t, ctx)

	posted, err := f.repo.GetByID(ctx, f.postedID)
	require.NoError(t, err)
	assert.False(t, posted.Reviewed,
		"a RULE's review on the pending row must not re-review a twin the user un-reviewed; the same rules run on the twin")
	assert.Nil(t, posted.ReviewedAt)
}

// TestSettledPending_UserReviewStillPropagates_Integration is the other half of F4's gate: excluding
// rule-derived reviews must not throw away a review the USER performed. Without this, the gate
// could be "never move reviewed" and still pass the test above.
func TestSettledPending_UserReviewStillPropagates_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedSettlementFixture(t)

	f.addPostedTwin(t, ctx, domain.Transaction{PendingTransactionID: &f.pendingExtID})
	_, err := f.tx.Exec(ctx,
		`UPDATE transactions SET reviewed = false, reviewed_at = NULL WHERE id = $1`, f.postedID)
	require.NoError(t, err)

	f.settle(t, ctx)

	posted, err := f.repo.GetByID(ctx, f.postedID)
	require.NoError(t, err)
	assert.True(t, posted.Reviewed,
		"the fixture's review came from UpdateCategory -- the user's own act -- and must survive settlement")
	require.NotNil(t, posted.ReviewedAt)
}

// TestSettledPending_CustomCategoryOverrideMoves_Integration closes the other half of F2. The
// discriminated union has a second branch the pfc fixture never exercises, and
// custom_category_override_id was unpinned because of it: mutating it out of the move survived.
// A custom category is also the branch where losing the id is worst -- the twin would keep an
// effective_category_name naming a category it no longer points at.
func TestSettledPending_CustomCategoryOverrideMoves_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedSettlementFixture(t)

	customID := uuid.New().String()
	_, err := f.tx.Exec(ctx, `
		INSERT INTO custom_categories (id, user_id, name) VALUES ($1, $2, 'Weekend Coffee')
	`, customID, f.userID)
	require.NoError(t, err, "seed custom category")

	// Re-file the pending charge under the custom category, through the production write path.
	require.NoError(t, f.repo.UpdateCategory(ctx, f.pendingID, f.userID,
		strPtr("custom"), nil, nil, &customID, domain.CategorySourceManual))

	f.addPostedTwin(t, ctx, domain.Transaction{PendingTransactionID: &f.pendingExtID})
	f.settle(t, ctx)

	posted, err := f.repo.GetByID(ctx, f.postedID)
	require.NoError(t, err)
	require.NotNil(t, posted.CategoryOverrideKind)
	assert.Equal(t, "custom", *posted.CategoryOverrideKind)
	require.NotNil(t, posted.CustomCategoryOverrideID,
		"the custom category id must move, or the twin points at nothing")
	assert.Equal(t, customID, *posted.CustomCategoryOverrideID)
	require.NotNil(t, posted.EffectiveCategoryName)
	assert.Equal(t, "Weekend Coffee", *posted.EffectiveCategoryName,
		"the resolved name must travel with the id it was resolved from")
}
