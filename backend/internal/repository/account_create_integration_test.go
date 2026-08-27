//go:build integration

package repository

// Integration tests for account CreateBatch correctness (#1306 P1 fix).
// Verifies that accounts.user_account_id is persisted at creation time.
//
// Fixture note: institution_id and access_token_encrypted use plain-text sentinel
// values in all integration fixtures (consistent with seedInvestmentFixture). The
// user_accounts seed row only needs to satisfy the FK constraint — its columns are
// not read back by these tests.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestAccountRepository_CreateBatch_PersistsUserAccountID_Integration verifies that
// CreateBatch persists user_account_id to the database.
func TestAccountRepository_CreateBatch_PersistsUserAccountID_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Seed user
	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|p1306-"+userID.String(), "p1306-"+userID.String()+"@example.com")
	require.NoError(t, err)

	// Seed linked_item
	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, "p1306-item-"+linkedItemID.String())
	require.NoError(t, err)

	// Seed user_account (FK target must exist before the account row is created)
	uaID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id)
		VALUES ($1, $2, 'ins-p1306')
	`, uaID, userID)
	require.NoError(t, err)
	uaStr := uaID.String()

	repo := NewAccountRepository(tx, newTestFieldEncryptor(t))
	acct := domain.Account{
		ID:             uuid.New().String(),
		LinkedItemID:   linkedItemID.String(),
		UserID:         userID.String(),
		UserAccountID:  &uaStr,
		PlaidAccountID: "p1306-acc-" + uuid.New().String(),
		Name:           "Checking",
		Type:           "depository",
		CurrencyCode:   "USD",
	}
	require.NoError(t, repo.CreateBatch(ctx, acct.UserID, []domain.Account{acct}))

	var got *string
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT user_account_id::text FROM accounts WHERE id = $1`, acct.ID).Scan(&got))
	require.NotNil(t, got, "user_account_id must be persisted by CreateBatch, not silently dropped")
	assert.Equal(t, uaStr, *got)
}

// ── #1569 T9.1 item B: at most ONE active account per re-link chain, enforced where it is written ─

// seedRelinkChainFixture seeds a user, a linked item, a user_account and one ACTIVE account
// already attached to that user_account — the predecessor a re-link is about to supersede.
func seedRelinkChainFixture(ctx context.Context, t *testing.T, db DB, prefix string) (
	userID, linkedItemID, userAccountID, oldAccountID uuid.UUID,
) {
	t.Helper()
	userID, linkedItemID, userAccountID, oldAccountID = uuid.New(), uuid.New(), uuid.New(), uuid.New()

	_, err := db.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')`,
		userID, "test|"+prefix+"-"+userID.String(), prefix+"-"+userID.String()+"@example.com")
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')`, linkedItemID, userID, prefix+"-item-"+linkedItemID.String())
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id) VALUES ($1, $2, $3)`,
		userAccountID, userID, prefix+"-ins")
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'Old Checking', 'depository', 'active', $5)`,
		oldAccountID, linkedItemID, userID, prefix+"-old-"+oldAccountID.String(), userAccountID)
	require.NoError(t, err)
	return
}

func accountStatusRow(ctx context.Context, t *testing.T, db DB, id uuid.UUID) (status string, replacedBy *string, replacedAt *time.Time) {
	t.Helper()
	require.NoError(t, db.QueryRow(ctx,
		`SELECT status, replaced_by_id::text, replaced_at FROM accounts WHERE id = $1`, id).
		Scan(&status, &replacedBy, &replacedAt))
	return
}

// TestAccountRepository_CreateBatch_SupersedesAStrandedActiveSibling_Integration is the item-B
// regression, and it is a regression against a state the product can already be in.
//
// account_service's re-link marks the predecessor 'replaced' with a LOG-AND-CONTINUE on failure
// (see the "failed to soft-delete old account" branch), and it does so BEFORE the successor rows
// exist. A transient error there therefore leaves two 'active' accounts sharing one
// user_account_id, silently — the exact state balance_snapshot_repository.go's dedup comment says
// "the Plaid re-link handler guarantees never happens (no DB constraint enforces it)". While it
// lasts, every current-state read that scopes on status='active' — live holdings, the four
// investment-analytics SUMs — counts one logical $100 position twice.
//
// The enforcement therefore lives in the write that CREATES the second active row, inside the
// transaction that creates it: either the successor exists and the predecessor is superseded, or
// neither happened. Nothing is destroyed — 'replaced' is the retention tombstone the re-link
// already used — and no new mechanism is introduced, because there is none to reuse: 'replaced'
// has exactly one writer in the whole backend and no sweep reconciles a stranded sibling.
func TestAccountRepository_CreateBatch_SupersedesAStrandedActiveSibling_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, userAccountID, oldAccountID := seedRelinkChainFixture(ctx, t, tx, "relink-supersede")
	uaStr := userAccountID.String()

	newID := uuid.New().String()
	repo := NewAccountRepository(tx, newTestFieldEncryptor(t))
	require.NoError(t, repo.CreateBatch(ctx, userID.String(), []domain.Account{{
		ID:             newID,
		LinkedItemID:   linkedItemID.String(),
		UserID:         userID.String(),
		UserAccountID:  &uaStr,
		PlaidAccountID: "relink-supersede-new-" + newID,
		Name:           "New Checking",
		Type:           "depository",
		CurrencyCode:   "USD",
	}}))

	status, replacedBy, replacedAt := accountStatusRow(ctx, t, tx, oldAccountID)
	assert.Equal(t, string(domain.AccountStatusReplaced), status,
		"the predecessor must not still be 'active' once its successor exists — that window is "+
			"what doubles live holdings and every investment-analytics SUM")
	require.NotNil(t, replacedBy, "replaced_by_id is how a reader gets from the tombstone to the live row")
	assert.Equal(t, newID, *replacedBy)
	require.NotNil(t, replacedAt, "migration 000149 pairs status='replaced' with replaced_at")

	var active int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT count(*) FROM accounts WHERE user_account_id = $1 AND status = 'active'`, userAccountID).
		Scan(&active))
	assert.Equal(t, 1, active, "exactly one active row per re-link chain, by construction")

	// The transition must be RECORDED, not merely applied: every liveness and as-of read in this
	// package resolves against account_status_history, not accounts.status.
	var closed int
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT count(*) FROM account_status_history
		WHERE account_id = $1 AND status = 'replaced' AND valid_to IS NULL`, oldAccountID).Scan(&closed))
	assert.Equal(t, 1, closed,
		"the supersede must go through the same status-history trigger the re-link path does, or "+
			"the account stays live forever as far as accountLiveOnDateClause is concerned")
}

// TestAccountRepository_CreateBatch_SupersedeTouchesOnlyItsOwnChain pins the blast radius. Two ways
// to get this wrong are both silent and both destroy the user's account list: matching on user_id
// alone would supersede every account the user has, and letting a NULL user_account_id match a NULL
// would supersede every legacy account at once.
func TestAccountRepository_CreateBatch_SupersedeTouchesOnlyItsOwnChain(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, userAccountID, oldAccountID := seedRelinkChainFixture(ctx, t, tx, "relink-blast")
	uaStr := userAccountID.String()

	// A second chain of the user's, entirely unrelated.
	otherUA, otherAccount := uuid.New(), uuid.New()
	_, err = tx.Exec(ctx, `INSERT INTO user_accounts (id, user_id, institution_id) VALUES ($1, $2, 'relink-blast-ins2')`,
		otherUA, userID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'Other Savings', 'depository', 'active', $5)`,
		otherAccount, linkedItemID, userID, "relink-blast-other-"+otherAccount.String(), otherUA)
	require.NoError(t, err)

	// A legacy account with NO stable identity at all.
	legacyAccount := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Legacy', 'depository', 'active')`,
		legacyAccount, linkedItemID, userID, "relink-blast-legacy-"+legacyAccount.String())
	require.NoError(t, err)

	// The batch deliberately also carries an identity-less row. That is what makes the nil-skip in
	// supersedeStrandedChainSiblings load-bearing: relax it to an IS NOT DISTINCT FROM match and
	// this row supersedes every legacy account the user owns.
	newID, identitylessID := uuid.New().String(), uuid.New().String()
	repo := NewAccountRepository(tx, newTestFieldEncryptor(t))
	require.NoError(t, repo.CreateBatch(ctx, userID.String(), []domain.Account{
		{
			ID:             newID,
			LinkedItemID:   linkedItemID.String(),
			UserID:         userID.String(),
			UserAccountID:  &uaStr,
			PlaidAccountID: "relink-blast-new-" + newID,
			Name:           "New Checking",
			Type:           "depository",
			CurrencyCode:   "USD",
		},
		{
			ID:             identitylessID,
			LinkedItemID:   linkedItemID.String(),
			UserID:         userID.String(),
			PlaidAccountID: "relink-blast-noid-" + identitylessID,
			Name:           "No Identity",
			Type:           "depository",
			CurrencyCode:   "USD",
		},
	}))

	superseded, _, _ := accountStatusRow(ctx, t, tx, oldAccountID)
	require.Equal(t, string(domain.AccountStatusReplaced), superseded, "sanity: the target chain WAS superseded")

	other, _, _ := accountStatusRow(ctx, t, tx, otherAccount)
	assert.Equal(t, string(domain.AccountStatusActive), other, "a different chain must be untouched")
	legacy, _, _ := accountStatusRow(ctx, t, tx, legacyAccount)
	assert.Equal(t, string(domain.AccountStatusActive), legacy,
		"a batch row with NO user_account_id must supersede nothing. SQL equality already makes "+
			"NULL match nothing; the nil-skip is what stops an IS NOT DISTINCT FROM 'fix' from "+
			"retiring every legacy account the user owns in one statement")
}
