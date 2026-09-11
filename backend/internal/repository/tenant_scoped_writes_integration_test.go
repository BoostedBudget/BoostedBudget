//go:build integration

package repository

// Issue #1566 — tenant scoping must be enforced by the repository, not by caller discipline.
//
// Three writes were classified CoveredByHTTPMiddleware in #1537 because the cache-invalidation
// middleware bumps the acting user and "the write only touches that user's data". The second
// half held only because every live caller happened to build actor-scoped rows; the methods
// asserted nothing. The middleware bumps ONLY the actor, so a write that reaches another user's
// rows leaves that user's cached analytics stale — the exact failure mode of
// ExpireAccountsByFingerprint (#1531) and GetOrCreateMerchantsWithNormalize (PR #1564).
//
// These tests assert the REJECTION against a real database: the other tenant's row must be
// untouched. A happy-path test would prove nothing here — the happy path already worked.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// seedTenantFixture creates a user + linked_item + user_account, all scoped to db (a pgx.Tx in
// these tests) so everything rolls back with it.
func seedTenantFixture(ctx context.Context, t *testing.T, db DB, prefix string) (
	userID, linkedItemID, userAccountID uuid.UUID,
) {
	t.Helper()
	userID = uuid.New()
	linkedItemID = uuid.New()
	userAccountID = uuid.New()

	_, err := db.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+prefix+"-"+userID.String(), prefix+"-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	_, err = db.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, "item-"+prefix+"-"+linkedItemID.String())
	require.NoError(t, err, "seed linked_item")

	_, err = db.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type, primary_subtype, is_hidden)
		VALUES ($1, $2, $3, 'depository', 'checking', false)
	`, userAccountID, userID, "ins-"+prefix)
	require.NoError(t, err, "seed user_account")

	return userID, linkedItemID, userAccountID
}

// TestBulkApply_CrossTenantDisplayOnlyRow_Rejected_Integration proves the display-only leg of
// BulkApply refuses an element whose UserID is not the actor. bulkUpsertTx derives each written
// user_id from the individual element, so without the assertion an actor-authenticated request
// rewrites a stranger's override row while the middleware bumps only the actor.
func TestBulkApply_CrossTenantDisplayOnlyRow_Rejected_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	actorID, actorMerchant, _ := seedTwoMerchants(ctx, t, tx, "ts1566-actor")
	victimID, victimMerchant, _ := seedTwoMerchants(ctx, t, tx, "ts1566-victim")

	// The victim already has an override the actor must not be able to rewrite.
	original := "Victim Original"
	_, err = tx.Exec(ctx, `
		INSERT INTO merchant_name_overrides
			(id, user_id, merchant_id, display_name, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, $2, $3, NOW(), NOW())
	`, victimID, victimMerchant, original)
	require.NoError(t, err, "seed victim override")

	hijack := "HIJACKED"
	actorName := "Actor Rename"
	repo := NewMerchantOverrideRepository(tx)

	_, err = repo.BulkApply(ctx, actorID.String(), []domain.MerchantNameOverride{
		{UserID: actorID.String(), MerchantID: actorMerchant.String(), DisplayName: &actorName},
		{UserID: victimID.String(), MerchantID: victimMerchant.String(), DisplayName: &hijack},
	}, nil)
	require.ErrorIs(t, err, ErrOverrideTenantMismatch,
		"a displayOnly element belonging to another user must be refused, not written")

	// The victim's row must be byte-for-byte untouched.
	var got string
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT display_name FROM merchant_name_overrides
		WHERE user_id = $1 AND merchant_id = $2
	`, victimID, victimMerchant).Scan(&got))
	assert.Equal(t, original, got, "the other tenant's override must not be rewritten")

	// The actor's own element must not have been written either — the batch is atomic.
	var actorCount int
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM merchant_name_overrides WHERE user_id = $1 AND merchant_id = $2
	`, actorID, actorMerchant).Scan(&actorCount))
	assert.Equal(t, 0, actorCount, "a rejected batch must write nothing at all")
}

// TestAccountRepository_CreateBatch_CrossTenantRow_Rejected_Integration proves CreateBatch
// refuses a slice containing another user's account row rather than inserting it. `accounts` is
// joined by every analytics query, so a row written for a non-actor user is cached-read visible
// to a user the middleware never bumps.
func TestAccountRepository_CreateBatch_CrossTenantRow_Rejected_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	actorID, actorItemID, _ := seedTenantFixture(ctx, t, tx, "ts1566-cb-actor")
	victimID, victimItemID, _ := seedTenantFixture(ctx, t, tx, "ts1566-cb-victim")

	repo := NewAccountRepository(tx, newTestFieldEncryptor(t))

	actorAcct := domain.Account{
		ID:             uuid.New().String(),
		LinkedItemID:   actorItemID.String(),
		UserID:         actorID.String(),
		PlaidAccountID: "ts1566-actor-" + uuid.New().String(),
		Name:           "Checking",
		Type:           "depository",
		CurrencyCode:   "USD",
	}
	victimAcct := domain.Account{
		ID:             uuid.New().String(),
		LinkedItemID:   victimItemID.String(),
		UserID:         victimID.String(),
		PlaidAccountID: "ts1566-victim-" + uuid.New().String(),
		Name:           "Savings",
		Type:           "depository",
		CurrencyCode:   "USD",
	}

	err = repo.CreateBatch(ctx, actorID.String(), []domain.Account{actorAcct, victimAcct})
	require.ErrorIs(t, err, ErrAccountBatchTenantMismatch,
		"a batch element belonging to another user must be refused, not inserted")

	var victimCount int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM accounts WHERE user_id = $1`, victimID).Scan(&victimCount))
	assert.Equal(t, 0, victimCount, "the other tenant must have no account row created for them")

	var actorCount int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM accounts WHERE user_id = $1`, actorID).Scan(&actorCount))
	assert.Equal(t, 0, actorCount, "a rejected batch must insert nothing at all")
}

// TestUserAccountRepository_SetHiddenBySystem_CrossTenantRow_Untouched_Integration proves the
// statement is scoped by user_id in SQL: passing another tenant's user_accounts id with the
// actor's userID must match zero rows and leave that row unchanged.
//
// #1609 renamed the subject (UpdateHidden(bool) became SetHiddenBySystem/ClearHidden); the
// assertion is unchanged, and the hide direction is the one worth pinning here because it is the
// one a hostile caller would want.
func TestUserAccountRepository_SetHiddenBySystem_CrossTenantRow_Untouched_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	actorID, _, _ := seedTenantFixture(ctx, t, tx, "ts1566-uh-actor")
	_, _, victimUAID := seedTenantFixture(ctx, t, tx, "ts1566-uh-victim")

	repo := NewUserAccountRepository(tx, newTestFieldEncryptor(t))

	err = repo.SetHiddenBySystem(ctx, victimUAID.String(), actorID.String())
	require.ErrorIs(t, err, ErrUserAccountNotFound,
		"an id the actor does not own must match zero rows")

	var isHidden bool
	var hiddenSource *string
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT is_hidden, hidden_source FROM user_accounts WHERE id = $1`, victimUAID).
		Scan(&isHidden, &hiddenSource))
	assert.False(t, isHidden, "the other tenant's user_accounts row must not be flipped")
	assert.Nil(t, hiddenSource, "nor may it acquire a cause for a hide that never happened")
}

// TestUserAccountRepository_UpdatePrimaryTypeAndSubtype_CrossTenantRow_Untouched_Integration is
// the same assertion for the sibling id-only UPDATE.
func TestUserAccountRepository_UpdatePrimaryTypeAndSubtype_CrossTenantRow_Untouched_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	actorID, _, _ := seedTenantFixture(ctx, t, tx, "ts1566-upts-actor")
	_, _, victimUAID := seedTenantFixture(ctx, t, tx, "ts1566-upts-victim")

	repo := NewUserAccountRepository(tx, newTestFieldEncryptor(t))

	err = repo.UpdatePrimaryTypeAndSubtype(ctx, victimUAID.String(), actorID.String(), "credit", "credit card")
	require.ErrorIs(t, err, ErrUserAccountNotFound,
		"an id the actor does not own must match zero rows")

	var primaryType, primarySubtype string
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT primary_type, primary_subtype FROM user_accounts WHERE id = $1`,
		victimUAID).Scan(&primaryType, &primarySubtype))
	assert.Equal(t, "depository", primaryType, "the other tenant's primary_type must not change")
	assert.Equal(t, "checking", primarySubtype, "the other tenant's primary_subtype must not change")
}

// TestTenantScopedWrites_UserAccountRepository_ClearInvestmentBackfillRequests is the #1631 M4
// addition, and it is the one case in this file whose victim would never find out.
//
// The other two cross-tenant writes here flip a visible flag or a type label: the victim, or a
// support engineer, can see the wrong value and ask why. Discharging an investment backfill
// obligation removes the ONLY record that history is owed, and nothing anywhere reports "a backfill
// that should have happened did not" — the sync simply never widens its window again, and the
// user's excluded period stays missing past Plaid's 2-year serving limit. Silent and unrecoverable.
//
// The concrete hole this closes: investmentSyncService.SyncInvestmentTransactions checks
// a.UserID != linkedItem.UserID for each ACCOUNT, but the chain ids it collects come from
// a.UserAccountID, whose owner it never verifies. One mis-assigned accounts.user_account_id was
// enough. The first version of these two methods took no userID at all and rested on that caller
// check, which is exactly the "caller discipline" this file's header rejects.
func TestTenantScopedWrites_UserAccountRepository_ClearInvestmentBackfillRequests(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	actorID, _, _ := seedTenantFixture(ctx, t, tx, "ts1631-bf-actor")
	_, _, victimUAID := seedTenantFixture(ctx, t, tx, "ts1631-bf-victim")

	// The victim is owed a backfill.
	owedAt := time.Now().UTC().Add(-time.Hour)
	_, err = tx.Exec(ctx,
		`UPDATE user_accounts SET investment_backfill_requested_at = $2 WHERE id = $1`,
		victimUAID, owedAt)
	require.NoError(t, err)

	repo := NewUserAccountRepository(tx, newTestFieldEncryptor(t))

	// The actor's sync tries to discharge it. Note this returns NO error: unlike the two writes
	// above, zero rows affected is a legitimate outcome here (a marker superseded mid-run), so the
	// row count cannot distinguish "refused a stranger" from "nothing left to clear". That is
	// precisely why the predicate must be in the SQL — there is no error channel to fall back on.
	require.NoError(t, repo.ClearInvestmentBackfillRequests(ctx,
		[]string{victimUAID.String()}, actorID.String(), time.Now().UTC()))

	var stillOwed *time.Time
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT investment_backfill_requested_at FROM user_accounts WHERE id = $1`, victimUAID).
		Scan(&stillOwed))
	require.NotNil(t, stillOwed,
		"another tenant's sync discharged this user's backfill obligation. Nothing reads this "+
			"column but the sync, so the victim's excluded history would simply never be "+
			"re-fetched, with no error and no way to notice")
	assert.WithinDuration(t, owedAt, *stillOwed, time.Millisecond)

	// And the read half must not leak it either — a sync that can SEE a foreign obligation would
	// widen its own Plaid window on the strength of another tenant's row.
	pending, err := repo.PendingInvestmentBackfills(ctx, []string{victimUAID.String()}, actorID.String())
	require.NoError(t, err)
	assert.Empty(t, pending,
		"PendingInvestmentBackfills must not return a chain the caller does not own")
}

// seedTenantAccount inserts an accounts row for an existing user/linked_item fixture and returns
// its id. Scoped to db (a pgx.Tx here), so it rolls back with the test.
func seedTenantAccount(ctx context.Context, t *testing.T, db DB, userID, linkedItemID, userAccountID uuid.UUID, prefix string) uuid.UUID {
	t.Helper()
	accountID := uuid.New()
	_, err := db.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, user_account_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, $5, 'Test Account', 'investment', 'active')
	`, accountID, linkedItemID, userID, userAccountID, "plaid-"+prefix+"-"+accountID.String())
	require.NoError(t, err, "seed account")
	return accountID
}

// seedTestSecurity inserts a securities row (holdings.security_id is NOT NULL with an FK).
func seedTestSecurity(ctx context.Context, t *testing.T, db DB, prefix string) uuid.UUID {
	t.Helper()
	securityID := uuid.New()
	_, err := db.Exec(ctx, `
		INSERT INTO securities (id, plaid_security_id, name, type)
		VALUES ($1, $2, 'Test Security', 'equity')
	`, securityID, "sec-"+prefix+"-"+securityID.String())
	require.NoError(t, err, "seed security")
	return securityID
}

// TestHoldingRepository_ReplaceForAccount_CrossTenantHolding_Rejected_Integration proves the
// DELETE/INSERT tenant split in ReplaceForAccount is closed by assertion, against a real DB.
// The DELETE was scoped to the acting userID while each INSERT wrote holdings[i].UserID, so a
// foreign element would wipe the actor's holdings and write rows owned by another user — whose
// cached analytics the invalidation middleware never bumps.
func TestHoldingRepository_ReplaceForAccount_CrossTenantHolding_Rejected_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	actorID, actorItemID, actorUAID := seedTenantFixture(ctx, t, tx, "ts1566-h-actor")
	victimID, victimItemID, victimUAID := seedTenantFixture(ctx, t, tx, "ts1566-h-victim")
	actorAcct := seedTenantAccount(ctx, t, tx, actorID, actorItemID, actorUAID, "ts1566-h-actor")
	victimAcct := seedTenantAccount(ctx, t, tx, victimID, victimItemID, victimUAID, "ts1566-h-victim")
	securityID := seedTestSecurity(ctx, t, tx, "ts1566-h")

	// The actor has an existing holding the rejected call must NOT delete.
	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity)
		VALUES (gen_random_uuid(), $1, $2, $3, 5)
	`, actorID, actorAcct, securityID)
	require.NoError(t, err, "seed actor holding")

	repo := NewHoldingRepository(tx)
	_, err = repo.ReplaceForAccount(ctx, actorAcct.String(), actorID.String(), []domain.Holding{{
		UserID:     victimID.String(),
		AccountID:  victimAcct.String(),
		SecurityID: securityID.String(),
		Quantity:   99,
	}})
	require.ErrorIs(t, err, ErrReplaceTenantMismatch,
		"a holding belonging to another user must be refused, not inserted")

	var victimCount int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM holdings WHERE user_id = $1`, victimID).Scan(&victimCount))
	assert.Equal(t, 0, victimCount, "the other tenant must have no holding written for them")

	// And the DELETE must not have run: a rejected call writes and destroys nothing.
	var actorCount int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM holdings WHERE user_id = $1`, actorID).Scan(&actorCount))
	assert.Equal(t, 1, actorCount, "the actor's existing holdings must survive a rejected replace")
}

// TestCreditLiabilityRepository_ReplaceForAccount_CrossTenantLiability_Rejected_Integration is the
// same assertion for the credit liability replace, whose DELETE was scoped to userID while the
// INSERT wrote liability.UserID.
func TestCreditLiabilityRepository_ReplaceForAccount_CrossTenantLiability_Rejected_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	actorID, actorItemID, actorUAID := seedTenantFixture(ctx, t, tx, "ts1566-cl-actor")
	victimID, victimItemID, victimUAID := seedTenantFixture(ctx, t, tx, "ts1566-cl-victim")
	actorAcct := seedTenantAccount(ctx, t, tx, actorID, actorItemID, actorUAID, "ts1566-cl-actor")
	victimAcct := seedTenantAccount(ctx, t, tx, victimID, victimItemID, victimUAID, "ts1566-cl-victim")

	// The actor has an existing credit liability the rejected call must NOT delete.
	_, err = tx.Exec(ctx, `
		INSERT INTO credit_liabilities (id, user_id, account_id, synced_at)
		VALUES (gen_random_uuid(), $1, $2, NOW())
	`, actorID, actorAcct)
	require.NoError(t, err, "seed actor credit liability")

	repo := NewCreditLiabilityRepository(tx)
	err = repo.ReplaceForAccount(ctx, actorAcct.String(), actorID.String(), &domain.CreditLiability{
		UserID:    victimID.String(),
		AccountID: victimAcct.String(),
	})
	require.ErrorIs(t, err, ErrReplaceTenantMismatch,
		"a credit liability belonging to another user must be refused, not inserted")

	var victimCount int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM credit_liabilities WHERE user_id = $1`, victimID).Scan(&victimCount))
	assert.Equal(t, 0, victimCount, "the other tenant must have no credit liability written for them")

	var actorCount int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM credit_liabilities WHERE user_id = $1`, actorID).Scan(&actorCount))
	assert.Equal(t, 1, actorCount, "the actor's existing liability must survive a rejected replace")
}

// TestMortgageAndStudentLoanReplaceForAccount_CrossTenantLiability_Rejected_Integration covers the
// remaining two ReplaceForAccount methods, which share the identical DELETE/INSERT tenant split.
func TestMortgageAndStudentLoanReplaceForAccount_CrossTenantLiability_Rejected_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	actorID, actorItemID, actorUAID := seedTenantFixture(ctx, t, tx, "ts1566-ml-actor")
	victimID, victimItemID, victimUAID := seedTenantFixture(ctx, t, tx, "ts1566-ml-victim")
	actorAcct := seedTenantAccount(ctx, t, tx, actorID, actorItemID, actorUAID, "ts1566-ml-actor")
	victimAcct := seedTenantAccount(ctx, t, tx, victimID, victimItemID, victimUAID, "ts1566-ml-victim")
	enc := newTestFieldEncryptor(t)

	t.Run("mortgage", func(t *testing.T) {
		_, err := tx.Exec(ctx, `
			INSERT INTO mortgage_liabilities (id, user_id, account_id, synced_at)
			VALUES (gen_random_uuid(), $1, $2, NOW())
		`, actorID, actorAcct)
		require.NoError(t, err, "seed actor mortgage liability")

		err = NewMortgageLiabilityRepository(tx, enc).ReplaceForAccount(
			ctx, actorAcct.String(), actorID.String(),
			&domain.MortgageLiability{UserID: victimID.String(), AccountID: victimAcct.String()},
		)
		require.ErrorIs(t, err, ErrReplaceTenantMismatch)

		var victimCount, actorCount int
		require.NoError(t, tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM mortgage_liabilities WHERE user_id = $1`, victimID).Scan(&victimCount))
		assert.Equal(t, 0, victimCount, "no mortgage row may be written for the other tenant")
		require.NoError(t, tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM mortgage_liabilities WHERE user_id = $1`, actorID).Scan(&actorCount))
		assert.Equal(t, 1, actorCount, "the actor's mortgage row must survive a rejected replace")
	})

	t.Run("student loan", func(t *testing.T) {
		_, err := tx.Exec(ctx, `
			INSERT INTO student_loan_liabilities (id, user_id, account_id, synced_at)
			VALUES (gen_random_uuid(), $1, $2, NOW())
		`, actorID, actorAcct)
		require.NoError(t, err, "seed actor student loan liability")

		err = NewStudentLoanLiabilityRepository(tx, enc).ReplaceForAccount(
			ctx, actorAcct.String(), actorID.String(),
			&domain.StudentLoanLiability{UserID: victimID.String(), AccountID: victimAcct.String()},
		)
		require.ErrorIs(t, err, ErrReplaceTenantMismatch)

		var victimCount, actorCount int
		require.NoError(t, tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM student_loan_liabilities WHERE user_id = $1`, victimID).Scan(&victimCount))
		assert.Equal(t, 0, victimCount, "no student loan row may be written for the other tenant")
		require.NoError(t, tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM student_loan_liabilities WHERE user_id = $1`, actorID).Scan(&actorCount))
		assert.Equal(t, 1, actorCount, "the actor's student loan row must survive a rejected replace")
	})
}

// TestReplaceForAccount_SameTenantForeignAccount_Rejected_Integration covers the account axis of
// the DELETE/INSERT split (#1568). Every ReplaceForAccount scoped its DELETE by the scalar
// accountID but wrote each element's own AccountID, so replacing account A with rows carrying
// account B cleared A and re-filed the data under B. Same user, so the tenant assertion of #1566
// never fired and the cache middleware bump was correct — the data simply moved between the user's
// own accounts, silently. Assert the rejection against a real DB: account A keeps its row and
// account B gains nothing.
func TestReplaceForAccount_SameTenantForeignAccount_Rejected_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, itemID, uaID := seedTenantFixture(ctx, t, tx, "ts1568-acct")
	acctA := seedTenantAccount(ctx, t, tx, userID, itemID, uaID, "ts1568-a")
	acctB := seedTenantAccount(ctx, t, tx, userID, itemID, uaID, "ts1568-b")
	enc := newTestFieldEncryptor(t)

	// countsUnmoved asserts account A kept its single seeded row and account B never gained one.
	countsUnmoved := func(t *testing.T, table string) {
		t.Helper()
		var inA, inB int
		require.NoError(t, tx.QueryRow(ctx,
			"SELECT COUNT(*) FROM "+table+" WHERE account_id = $1", acctA).Scan(&inA))
		require.NoError(t, tx.QueryRow(ctx,
			"SELECT COUNT(*) FROM "+table+" WHERE account_id = $1", acctB).Scan(&inB))
		assert.Equal(t, 1, inA, table+": the replaced account must keep its row after a rejected call")
		assert.Equal(t, 0, inB, table+": no row may be re-filed under the other account")
	}

	t.Run("holdings", func(t *testing.T) {
		securityID := seedTestSecurity(ctx, t, tx, "ts1568")
		_, err := tx.Exec(ctx, `
			INSERT INTO holdings (id, user_id, account_id, security_id, quantity)
			VALUES (gen_random_uuid(), $1, $2, $3, 5)
		`, userID, acctA, securityID)
		require.NoError(t, err, "seed holding on account A")

		_, err = NewHoldingRepository(tx).ReplaceForAccount(ctx, acctA.String(), userID.String(),
			[]domain.Holding{{
				UserID:     userID.String(),
				AccountID:  acctB.String(),
				SecurityID: securityID.String(),
				Quantity:   99,
			}})
		require.ErrorIs(t, err, ErrReplaceAccountMismatch,
			"a holding attributed to another account must be refused, not re-filed")
		countsUnmoved(t, "holdings")
	})

	t.Run("credit liability", func(t *testing.T) {
		_, err := tx.Exec(ctx, `
			INSERT INTO credit_liabilities (id, user_id, account_id, synced_at)
			VALUES (gen_random_uuid(), $1, $2, NOW())
		`, userID, acctA)
		require.NoError(t, err, "seed credit liability on account A")

		err = NewCreditLiabilityRepository(tx).ReplaceForAccount(ctx, acctA.String(), userID.String(),
			&domain.CreditLiability{UserID: userID.String(), AccountID: acctB.String()})
		require.ErrorIs(t, err, ErrReplaceAccountMismatch)
		countsUnmoved(t, "credit_liabilities")
	})

	t.Run("mortgage liability", func(t *testing.T) {
		_, err := tx.Exec(ctx, `
			INSERT INTO mortgage_liabilities (id, user_id, account_id, synced_at)
			VALUES (gen_random_uuid(), $1, $2, NOW())
		`, userID, acctA)
		require.NoError(t, err, "seed mortgage liability on account A")

		err = NewMortgageLiabilityRepository(tx, enc).ReplaceForAccount(ctx, acctA.String(), userID.String(),
			&domain.MortgageLiability{UserID: userID.String(), AccountID: acctB.String()})
		require.ErrorIs(t, err, ErrReplaceAccountMismatch)
		countsUnmoved(t, "mortgage_liabilities")
	})

	t.Run("student loan liability", func(t *testing.T) {
		_, err := tx.Exec(ctx, `
			INSERT INTO student_loan_liabilities (id, user_id, account_id, synced_at)
			VALUES (gen_random_uuid(), $1, $2, NOW())
		`, userID, acctA)
		require.NoError(t, err, "seed student loan liability on account A")

		err = NewStudentLoanLiabilityRepository(tx, enc).ReplaceForAccount(ctx, acctA.String(), userID.String(),
			&domain.StudentLoanLiability{UserID: userID.String(), AccountID: acctB.String()})
		require.ErrorIs(t, err, ErrReplaceAccountMismatch)
		countsUnmoved(t, "student_loan_liabilities")
	})
}
