//go:build integration

package repository_test

// holding_household_scope_integration_test.go — pins the HOUSEHOLD SCOPE fix in the
// investments read behind the dashboard's Portfolio Holdings widget (issue #1706).
//
// The read under test is HoldingRepository.GetByUserIDWithSecurities. Before #1706 it filtered
// ONLY `WHERE h.user_id = $1` (holding_repository.go) — it had NO household predicate of any
// kind. So when the owner of a household opened the dashboard with scope=household, every
// investment holding the owner personally owned was rendered, including holdings from accounts
// the owner never shared into the household. The household inclusion rule everywhere else is
// share-gated (household_account_shares, visibility='shared'); this read ignored it entirely.
//
// WHAT THIS TEST DOES NOW. It builds a real two-member household in Postgres, gives the OWNER
// two investment accounts, shares only ONE of them, calls the household-scoped read
// `GetByUserIDWithSecurities(ctx, ownerID, nil, scope)`, and asserts ONLY the shared account's
// holding comes back — the unshared account's holding is ABSENT. The personal read (scope nil)
// still returns both, which is correct: personal scope is the owner's own universe.
//
// Run:
//
//	DATABASE_URL="postgres://..." CACHE_REDIS_TEST_URL="redis://localhost:6380" \
//	REDIS_TEST_URL="redis://localhost:6379" \
//	go test -tags integration -p 1 -count=1 ./internal/repository/... -run 'TestHoldingsHouseholdScope' -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// insertHouseholdScopeMember inserts the active household_members row that makes userID a real
// member of householdID (role owner/member). Cleanup rides on the household delete: the FK from
// household_members to households is ON DELETE CASCADE (000107), and insertTestHousehold already
// registers that delete.
func insertHouseholdScopeMember(t *testing.T, pool *pgxpool.Pool, householdID, userID, role string) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO household_members (household_id, user_id, role, status)
		VALUES ($1, $2, $3, 'active')
	`, householdID, userID, role)
	require.NoError(t, err, "insert household member %s", userID)
}

// householdScopeAccount is one investment account's linkage chain: the accounts row plus the
// user_accounts row that a household share is actually keyed on (shares are keyed to
// user_account_id, which survives Plaid re-links). Distinct from insertTestAccount in the
// household-share test file, which hardcodes type='depository' and so is invisible to the
// type='investment'-gated holdings read.
type householdScopeAccount struct {
	UserAccountID string
	AccountID     string
	DisplayName   string
}

// insertHouseholdScopeInvestmentAccount inserts an ACTIVE type='investment' accounts row (the two
// predicates GetByUserIDWithSecurities joins on: currentStateAccountLeg => status='active', and
// a.type='investment') together with its user_accounts row.
func insertHouseholdScopeInvestmentAccount(t *testing.T, pool *pgxpool.Pool, linkedItemID, userID, displayName string) householdScopeAccount {
	t.Helper()
	ctx := context.Background()

	ua := insertTestUserAccount(t, pool, userID)

	var accID string
	err := pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, 'plaid_'||gen_random_uuid()::text, $3, 'investment', 'active', $4)
		RETURNING id::text
	`, linkedItemID, userID, displayName, ua).Scan(&accID)
	require.NoError(t, err, "insert investment account %q", displayName)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM accounts WHERE id = $1`, accID) //nolint:errcheck
	})

	return householdScopeAccount{UserAccountID: ua, AccountID: accID, DisplayName: displayName}
}

// insertHouseholdScopeHolding seeds a security and one holdings row for accountID. Ticker and
// institution_value are DISTINCT per call so the evidence output can name each account's position
// unambiguously.
func insertHouseholdScopeHolding(t *testing.T, pool *pgxpool.Pool, userID string, acct householdScopeAccount, ticker, securityName string, institutionValue float64) {
	t.Helper()
	ctx := context.Background()

	var secID string
	err := pool.QueryRow(ctx, `
		INSERT INTO securities (plaid_security_id, name, ticker_symbol, type)
		VALUES ($1, $2, $3, 'equity')
		RETURNING id::text
	`, "sec_"+uuid.New().String(), securityName, ticker).Scan(&secID)
	require.NoError(t, err, "insert security %s", ticker)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM securities WHERE id = $1`, secID) //nolint:errcheck
	})

	var holdingID string
	err = pool.QueryRow(ctx, `
		INSERT INTO holdings (user_id, account_id, user_account_id, security_id, quantity, cost_basis, institution_value)
		VALUES ($1, $2, $3, $4, 10, 1000.00, $5)
		RETURNING id::text
	`, userID, acct.AccountID, acct.UserAccountID, secID, institutionValue).Scan(&holdingID)
	require.NoError(t, err, "insert holding %s for %s", ticker, acct.DisplayName)
	// Cleanup order (LIFO): the holding is removed before the security, and the accounts row
	// (registered earlier) is removed last — every FK edge is respected.
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM holdings WHERE id = $1`, holdingID) //nolint:errcheck
	})
}

// TestHoldingsHouseholdScope_UnsharedAccountMustNotAppear pins the #1706 scope fix.
//
// Setup: a two-member household (owner + member). The OWNER owns TWO investment accounts. Only
// the first is shared into the household; the second is the owner's private brokerage and is
// never shared. The household-scoped read is called with the owner's user ID and a nil account
// filter.
//
// Asserted: ONLY the SHARED account's holding is returned. The unshared account's holding is
// ABSENT — it is never shared into the household, so it must not appear in a household total.
func TestHoldingsHouseholdScope_UnsharedAccountMustNotAppear(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	insertHouseholdScopeMember(t, pool, hh, owner, "owner")
	insertHouseholdScopeMember(t, pool, hh, member, "member")

	ownerLinkedItem := insertTestLinkedItem(t, pool, owner)

	// The owner's TWO investment accounts. Names are deliberately loud so the evidence output
	// identifies each one.
	sharedAcct := insertHouseholdScopeInvestmentAccount(t, pool, ownerLinkedItem, owner, "OWNER-SHARED-BROKERAGE")
	privateAcct := insertHouseholdScopeInvestmentAccount(t, pool, ownerLinkedItem, owner, "OWNER-PRIVATE-BROKERAGE")

	// Distinct, easily-identifiable positions: one per account.
	insertHouseholdScopeHolding(t, pool, owner, sharedAcct, "SHRD", "Owner Shared Fund", 1111.11)
	insertHouseholdScopeHolding(t, pool, owner, privateAcct, "PRVT", "Owner Private Fund", 9999.99)

	// Share ONLY the first account into the household. The second stays unshared and hidden.
	now := time.Now().UTC()
	insertHouseholdShare(t, pool, hh, sharedAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}

	// ── The read under test: household-scoped. ──
	holdings, err := repository.NewHoldingRepository(pool).GetByUserIDWithSecurities(ctx, owner, nil, scope)
	require.NoError(t, err, "GetByUserIDWithSecurities must not error")

	byAccount := make(map[string]domain.Holding, len(holdings))
	for _, h := range holdings {
		byAccount[h.AccountID] = h
	}

	sharedHolding, sharedPresent := byAccount[sharedAcct.AccountID]
	_, privatePresent := byAccount[privateAcct.AccountID]

	if sharedPresent {
		t.Logf("SHARED   owner account %q (%s) holding returned: ticker=%s value=%.2f",
			sharedAcct.DisplayName, sharedAcct.AccountID, tickerOf(sharedHolding), valueOf(sharedHolding))
	}
	t.Logf("household-scoped read returned %d holding(s); shared_acct=%s unshared_acct=%s",
		len(holdings), sharedAcct.AccountID, privateAcct.AccountID)

	require.Len(t, holdings, 1,
		"household scope must return ONLY the shared account's holding; the unshared account %q (%s, "+
			"ticker PRVT) is never shared into household %s and MUST BE ABSENT",
		privateAcct.DisplayName, privateAcct.AccountID, hh)

	assert.True(t, sharedPresent,
		"the SHARED account %q (%s) holding must be returned — it is shared into the household and is "+
			"the correct, in-scope position", sharedAcct.DisplayName, sharedAcct.AccountID)

	assert.False(t, privatePresent,
		"the UNSHARED account %q (%s) holding was returned under household scope. It is never shared "+
			"into household %s, so it MUST BE ABSENT (#1706).",
		privateAcct.DisplayName, privateAcct.AccountID, hh)

	assert.InDelta(t, 1111.11, valueOf(sharedHolding), 0.01, "shared holding value")
}

// TestHoldingsHouseholdScope_RevokedShareExcluded proves the household CTE's interval predicate
// excludes a share revoked BEFORE the read's asOf instant. The membership clause admits a closed
// grant (`OR has.revoked_at IS NOT NULL`), so exclusion can only come from
// household_grants_asof's `has.revoked_at > $4` — this test asserts that, rather than trusting it.
func TestHoldingsHouseholdScope_RevokedShareExcluded(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	insertHouseholdScopeMember(t, pool, hh, owner, "owner")
	insertHouseholdScopeMember(t, pool, hh, member, "member")

	ownerLinkedItem := insertTestLinkedItem(t, pool, owner)
	acct := insertHouseholdScopeInvestmentAccount(t, pool, ownerLinkedItem, owner, "OWNER-REVOKED-BROKERAGE")
	insertHouseholdScopeHolding(t, pool, owner, acct, "RVKD", "Owner Revoked Fund", 4242.42)

	now := time.Now().UTC()
	shareID := insertHouseholdShare(t, pool, hh, acct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -3, 0))
	// Revoke the share BEFORE now: the account was shared for a past window only.
	_, err := pool.Exec(ctx, `UPDATE household_account_shares SET revoked_at = $1 WHERE id = $2`, now.AddDate(0, -1, 0), shareID)
	require.NoError(t, err, "revoke share")

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}
	holdings, err := repository.NewHoldingRepository(pool).GetByUserIDWithSecurities(ctx, owner, nil, scope)
	require.NoError(t, err, "GetByUserIDWithSecurities must not error")

	assert.Empty(t, holdings,
		"a share revoked before now must be excluded from the household read: household_grants_asof "+
			"filters `revoked_at IS NULL OR revoked_at > $4`, so the account is not in household_accounts")
}

func tickerOf(h domain.Holding) string {
	if h.Security == nil || h.Security.TickerSymbol == nil {
		return ""
	}
	return *h.Security.TickerSymbol
}

func valueOf(h domain.Holding) float64 {
	if h.InstitutionValue == nil {
		return 0
	}
	return *h.InstitutionValue
}

// ─── #1706 shape guards: revoked, hidden, joint and re-link ─────────────────────────────────
//
// The five tests below are the shapes the reviews demanded for the NEW household holdings read
// (GetByUserIDWithSecurities with a non-nil *domain.HouseholdScope, committed in af5884b1b). Each
// is its own named function so it is individually -run-addressable, and each asserts CONCRETE
// tickers and values rather than merely "no error".

// insertHouseholdScopeInvestmentAccountOnChain inserts an accounts row on an EXISTING user_accounts
// chain with an explicit status. The re-link guard needs a 'replaced' predecessor and an 'active'
// successor sharing ONE chain row — insertHouseholdScopeInvestmentAccount mints a fresh chain each
// call and cannot express that.
func insertHouseholdScopeInvestmentAccountOnChain(t *testing.T, pool *pgxpool.Pool, linkedItemID, userID, userAccountID, displayName, status string) string {
	t.Helper()
	ctx := context.Background()
	var accID string
	// accounts_replaced_at_consistency (000033) requires replaced_at whenever status='replaced'.
	var replacedAt *time.Time
	if status == "replaced" {
		now := time.Now().UTC()
		replacedAt = &now
	}
	err := pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id, replaced_at)
		VALUES ($1, $2, 'plaid_'||gen_random_uuid()::text, $3, 'investment', $4, $5, $6)
		RETURNING id::text
	`, linkedItemID, userID, displayName, status, userAccountID, replacedAt).Scan(&accID)
	require.NoError(t, err, "insert investment account %q on chain %s", displayName, userAccountID)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM accounts WHERE id = $1`, accID) //nolint:errcheck
	})
	return accID
}

// setHouseholdScopeAccountBalance sets accounts.current_balance so the household net-worth read
// (GetForHouseholdScopeActiveNow) has a concrete investment leg to compare the holdings total
// against.
func setHouseholdScopeAccountBalance(t *testing.T, pool *pgxpool.Pool, accountID string, balance float64) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`UPDATE accounts SET current_balance = $1 WHERE id = $2`, balance, accountID)
	require.NoError(t, err, "set current_balance on account %s", accountID)
}

// setHouseholdScopeUserAccountHidden flips user_accounts.is_hidden — the OWNER'S personal-view
// control and the source of truth for the #1609 customization overlay. The household read must not
// consult it: inclusion is governed SOLELY by the share (account_repository.go's ruling).
func setHouseholdScopeUserAccountHidden(t *testing.T, pool *pgxpool.Pool, userAccountID string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`UPDATE user_accounts SET is_hidden = TRUE WHERE id = $1`, userAccountID)
	require.NoError(t, err, "hide user_account %s", userAccountID)
}

// holdingsByAccount indexes a read result by account id. Every fixture here has one holding per
// account, so the map is lossless.
func holdingsByAccount(holdings []domain.Holding) map[string]domain.Holding {
	byAccount := make(map[string]domain.Holding, len(holdings))
	for _, h := range holdings {
		byAccount[h.AccountID] = h
	}
	return byAccount
}

// TestHoldingsHouseholdScope_RevokedShareExcluded_ActiveSiblingStillPresent is sub-case (a), made
// NON-VACUOUS. The sibling test asserts an empty result for a lone revoked share; an empty result
// is also what a read that returned nothing at all would produce. This one puts a revoked share
// BESIDE an active one and demands the active holding survive, so the exclusion is provably
// specific to the revoked grant — i.e. it comes from household_grants_asof's
// `shared_at <= $4 AND (revoked_at IS NULL OR revoked_at > $4)`, not from the read returning
// nothing.
func TestHoldingsHouseholdScope_RevokedShareExcluded_ActiveSiblingStillPresent(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	insertHouseholdScopeMember(t, pool, hh, owner, "owner")
	insertHouseholdScopeMember(t, pool, hh, member, "member")

	ownerLinkedItem := insertTestLinkedItem(t, pool, owner)
	revokedAcct := insertHouseholdScopeInvestmentAccount(t, pool, ownerLinkedItem, owner, "OWNER-REVOKED-BROKERAGE")
	activeAcct := insertHouseholdScopeInvestmentAccount(t, pool, ownerLinkedItem, owner, "OWNER-ACTIVE-BROKERAGE")
	insertHouseholdScopeHolding(t, pool, owner, revokedAcct, "RVKD", "Owner Revoked Fund", 4242.42)
	insertHouseholdScopeHolding(t, pool, owner, activeAcct, "ACTV", "Owner Active Fund", 1111.11)

	now := time.Now().UTC()
	revokedShareID := insertHouseholdShare(t, pool, hh, revokedAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -3, 0))
	// Revoke the share BEFORE now: the account was shared for a past window only.
	_, err := pool.Exec(ctx, `UPDATE household_account_shares SET revoked_at = $1 WHERE id = $2`, now.AddDate(0, -1, 0), revokedShareID)
	require.NoError(t, err, "revoke share")
	insertHouseholdShare(t, pool, hh, activeAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}
	holdings, err := repository.NewHoldingRepository(pool).GetByUserIDWithSecurities(ctx, owner, nil, scope)
	require.NoError(t, err, "GetByUserIDWithSecurities must not error")

	byAccount := holdingsByAccount(holdings)
	_, revokedPresent := byAccount[revokedAcct.AccountID]
	activeHolding, activePresent := byAccount[activeAcct.AccountID]
	t.Logf("household read returned %d holding(s); revoked_acct=%s active_acct=%s",
		len(holdings), revokedAcct.AccountID, activeAcct.AccountID)

	require.Len(t, holdings, 1,
		"only the ACTIVE share may contribute; the revoked share's holding (ticker RVKD) must be ABSENT")
	assert.False(t, revokedPresent,
		"the revoked share's holding was returned: household_grants_asof must exclude it via "+
			"`revoked_at IS NULL OR revoked_at > $4` (revoked %s, read at %s)",
		now.AddDate(0, -1, 0).Format(time.RFC3339), now.Format(time.RFC3339))
	assert.True(t, activePresent,
		"the ACTIVE share's holding must survive — a read that returned nothing would also pass the "+
			"revoked assertion, which is why this sibling exists")
	assert.InDelta(t, 1111.11, valueOf(activeHolding), 0.01, "active holding value")
}

// TestHoldingsHouseholdScope_HiddenSharedStillVisible_HiddenUnsharedAbsent is sub-case (b): the
// shipped ruling is that household inclusion is governed SOLELY by the share, and is_hidden is the
// OWNER'S personal-view control (account_repository.go:1039-1046). So a member's own hidden toggle
// must NOT withdraw an account from the household they shared it with — while an account that is
// hidden AND never shared must not appear. Both directions are asserted, because either one alone
// is satisfiable by a read that ignores is_hidden for the wrong reason.
func TestHoldingsHouseholdScope_HiddenSharedStillVisible_HiddenUnsharedAbsent(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	insertHouseholdScopeMember(t, pool, hh, owner, "owner")
	insertHouseholdScopeMember(t, pool, hh, member, "member")

	ownerLinkedItem := insertTestLinkedItem(t, pool, owner)
	hiddenShared := insertHouseholdScopeInvestmentAccount(t, pool, ownerLinkedItem, owner, "OWNER-HIDDEN-SHARED")
	hiddenUnshared := insertHouseholdScopeInvestmentAccount(t, pool, ownerLinkedItem, owner, "OWNER-HIDDEN-UNSHARED")
	// is_hidden is the owner's own control, on the chain row (the #1609 source of truth).
	setHouseholdScopeUserAccountHidden(t, pool, hiddenShared.UserAccountID)
	setHouseholdScopeUserAccountHidden(t, pool, hiddenUnshared.UserAccountID)

	insertHouseholdScopeHolding(t, pool, owner, hiddenShared, "HSHR", "Hidden Shared Fund", 2222.22)
	insertHouseholdScopeHolding(t, pool, owner, hiddenUnshared, "HPRV", "Hidden Unshared Fund", 8888.88)

	now := time.Now().UTC()
	insertHouseholdShare(t, pool, hh, hiddenShared.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))
	// hiddenUnshared gets NO share row.

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}
	holdings, err := repository.NewHoldingRepository(pool).GetByUserIDWithSecurities(ctx, owner, nil, scope)
	require.NoError(t, err, "GetByUserIDWithSecurities must not error")

	byAccount := holdingsByAccount(holdings)
	sharedHolding, sharedPresent := byAccount[hiddenShared.AccountID]
	_, unsharedPresent := byAccount[hiddenUnshared.AccountID]
	t.Logf("household read returned %d holding(s); hidden_shared=%s hidden_unshared=%s",
		len(holdings), hiddenShared.AccountID, hiddenUnshared.AccountID)

	require.Len(t, holdings, 1, "exactly the hidden-but-SHARED account's holding")
	assert.True(t, sharedPresent,
		"a hidden account that IS shared must remain visible to the household: is_hidden is the "+
			"owner's personal-view control and must not withdraw it from the household")
	assert.False(t, unsharedPresent,
		"a hidden account that is NOT shared must not appear — inclusion is governed by the share, "+
			"not by is_hidden")
	assert.InDelta(t, 2222.22, valueOf(sharedHolding), 0.01, "hidden-shared holding value")
}

// TestHoldingsHouseholdScope_JointPairSharedCopyCountedOnceUnsharedCopyAbsent is sub-case (c): two
// user_accounts rows denoting ONE physical account (same physical_account_blind_idx fingerprint),
// with the sharing split — one member's copy is shared into the household, the other's is not. The
// shared copy must be counted exactly once and the unshared copy must never appear.
func TestHoldingsHouseholdScope_JointPairSharedCopyCountedOnceUnsharedCopyAbsent(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	insertHouseholdScopeMember(t, pool, hh, owner, "owner")
	insertHouseholdScopeMember(t, pool, hh, member, "member")

	ownerLinkedItem := insertTestLinkedItem(t, pool, owner)
	memberLinkedItem := insertTestLinkedItem(t, pool, member)
	ownerCopy := insertHouseholdScopeInvestmentAccount(t, pool, ownerLinkedItem, owner, "JOINT-OWNER-COPY")
	memberCopy := insertHouseholdScopeInvestmentAccount(t, pool, memberLinkedItem, member, "JOINT-MEMBER-COPY")
	insertHouseholdScopeHolding(t, pool, owner, ownerCopy, "JOWN", "Joint Owner Fund", 3333.33)
	insertHouseholdScopeHolding(t, pool, member, memberCopy, "JMEM", "Joint Member Fund", 7777.77)

	// One physical account: both copies carry the SAME fingerprint. Only the owner's copy is shared.
	fingerprint := "joint-fp-" + ownerCopy.UserAccountID
	now := time.Now().UTC()
	insertHouseholdShare(t, pool, hh, ownerCopy.UserAccountID, &fingerprint, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))
	// memberCopy gets NO share row.

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}
	holdings, err := repository.NewHoldingRepository(pool).GetByUserIDWithSecurities(ctx, owner, nil, scope)
	require.NoError(t, err, "GetByUserIDWithSecurities must not error")

	byAccount := holdingsByAccount(holdings)
	ownerHolding, ownerPresent := byAccount[ownerCopy.AccountID]
	_, memberPresent := byAccount[memberCopy.AccountID]
	t.Logf("household read returned %d holding(s); shared_copy=%s unshared_copy=%s",
		len(holdings), ownerCopy.AccountID, memberCopy.AccountID)

	require.Len(t, holdings, 1,
		"the shared copy of the joint account must be counted exactly once; the unshared copy must "+
			"never appear")
	assert.True(t, ownerPresent, "the SHARED copy's holding must be returned")
	assert.False(t, memberPresent,
		"the UNSHARED copy of the same physical account was returned — it is never shared into "+
			"household %s and MUST BE ABSENT", hh)
	assert.InDelta(t, 3333.33, valueOf(ownerHolding), 0.01, "shared copy holding value")
}

// TestHoldingsHouseholdScope_RelinkChainCountedOnce is sub-case (d): a 'replaced' predecessor and
// an 'active' successor under ONE user_accounts row (a Plaid re-link). Both carry a holding; the
// chain's money must appear exactly once — the predecessor is excluded by the read's
// currentStateAccountLeg (`a.status = 'active'`), so the successor's holding is the only one.
func TestHoldingsHouseholdScope_RelinkChainCountedOnce(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	insertHouseholdScopeMember(t, pool, hh, owner, "owner")
	insertHouseholdScopeMember(t, pool, hh, member, "member")

	ownerLinkedItem := insertTestLinkedItem(t, pool, owner)
	// ONE chain row, TWO accounts: the re-link shape.
	chain := insertTestUserAccount(t, pool, owner)
	predecessorID := insertHouseholdScopeInvestmentAccountOnChain(t, pool, ownerLinkedItem, owner, chain, "RELINK-OLD-BROKERAGE", "replaced")
	successorID := insertHouseholdScopeInvestmentAccountOnChain(t, pool, ownerLinkedItem, owner, chain, "RELINK-NEW-BROKERAGE", "active")

	insertHouseholdScopeHolding(t, pool, owner,
		householdScopeAccount{UserAccountID: chain, AccountID: predecessorID, DisplayName: "RELINK-OLD-BROKERAGE"},
		"OLDX", "Old Brokerage Fund", 5000.00)
	insertHouseholdScopeHolding(t, pool, owner,
		householdScopeAccount{UserAccountID: chain, AccountID: successorID, DisplayName: "RELINK-NEW-BROKERAGE"},
		"NEWX", "New Brokerage Fund", 5000.00)

	now := time.Now().UTC()
	insertHouseholdShare(t, pool, hh, chain, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}
	holdings, err := repository.NewHoldingRepository(pool).GetByUserIDWithSecurities(ctx, owner, nil, scope)
	require.NoError(t, err, "GetByUserIDWithSecurities must not error")

	byAccount := holdingsByAccount(holdings)
	_, predecessorPresent := byAccount[predecessorID]
	successorHolding, successorPresent := byAccount[successorID]
	t.Logf("household read returned %d holding(s); predecessor=%s successor=%s",
		len(holdings), predecessorID, successorID)

	require.Len(t, holdings, 1,
		"a re-link chain is ONE logical account: 2 holdings means the replaced predecessor was "+
			"double-counted")
	assert.False(t, predecessorPresent,
		"the 'replaced' predecessor's holding must be excluded by currentStateAccountLeg "+
			"(`a.status = 'active'`)")
	assert.True(t, successorPresent, "the 'active' successor's holding must be returned")
	assert.InDelta(t, 5000.00, valueOf(successorHolding), 0.01,
		"the chain's money must appear ONCE (5000), not twice (10000)")
}

// TestHoldingsHouseholdScope_SubsetOfHouseholdNetWorthInvestmentLeg is sub-case (e), the
// cross-check invariant: the household holdings set must be a SUBSET of the household net-worth's
// investment accounts, and the sum of household holdings must never exceed the household
// investment leg. Both reads are share-gated by the same CTE, so a leak in the holdings read (an
// unshared account's position) shows up here as a holding whose account is absent from the
// net-worth set, and as a holdings total above the leg.
func TestHoldingsHouseholdScope_SubsetOfHouseholdNetWorthInvestmentLeg(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	insertHouseholdScopeMember(t, pool, hh, owner, "owner")
	insertHouseholdScopeMember(t, pool, hh, member, "member")

	ownerLinkedItem := insertTestLinkedItem(t, pool, owner)
	sharedAcct := insertHouseholdScopeInvestmentAccount(t, pool, ownerLinkedItem, owner, "XCHECK-SHARED-BROKERAGE")
	privateAcct := insertHouseholdScopeInvestmentAccount(t, pool, ownerLinkedItem, owner, "XCHECK-PRIVATE-BROKERAGE")
	insertHouseholdScopeHolding(t, pool, owner, sharedAcct, "XSHR", "Cross-check Shared Fund", 1111.11)
	insertHouseholdScopeHolding(t, pool, owner, privateAcct, "XPRV", "Cross-check Private Fund", 9999.99)
	// The net-worth leg is the account BALANCE, not the holdings value: a brokerage holds cash too.
	setHouseholdScopeAccountBalance(t, pool, sharedAcct.AccountID, 5000.00)
	setHouseholdScopeAccountBalance(t, pool, privateAcct.AccountID, 9999.99)

	now := time.Now().UTC()
	insertHouseholdShare(t, pool, hh, sharedAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}

	holdings, err := repository.NewHoldingRepository(pool).GetByUserIDWithSecurities(ctx, owner, nil, scope)
	require.NoError(t, err, "GetByUserIDWithSecurities must not error")

	accounts, err := repository.NewAccountRepository(pool, newTestFieldEncryptor(t)).
		GetForHouseholdScopeActiveNow(ctx, owner, scope, now)
	require.NoError(t, err, "GetForHouseholdScopeActiveNow must not error")

	investmentIDs := make(map[string]bool)
	var investmentLeg float64
	for _, a := range accounts {
		if a.Type != "investment" {
			continue
		}
		investmentIDs[a.ID] = true
		if a.CurrentBalance != nil {
			investmentLeg += *a.CurrentBalance
		}
	}

	var holdingsTotal float64
	for _, h := range holdings {
		assert.True(t, investmentIDs[h.AccountID],
			"holding on account %s (ticker %s) is NOT in the household net-worth investment set — "+
				"the holdings read leaked an account the net-worth read excludes", h.AccountID, tickerOf(h))
		holdingsTotal += valueOf(h)
	}

	t.Logf("household holdings total=%.2f over %d holding(s); net-worth investment leg=%.2f over %d account(s)",
		holdingsTotal, len(holdings), investmentLeg, len(investmentIDs))

	assert.InDelta(t, 1111.11, holdingsTotal, 0.01,
		"only the shared account's holding may contribute; 11111.10 means the private account leaked")
	assert.InDelta(t, 5000.00, investmentLeg, 0.01,
		"only the shared account's balance is in the household investment leg")
	assert.LessOrEqual(t, holdingsTotal, investmentLeg,
		"the sum of household holdings must never exceed the household investment leg")
}

// TestHoldingsHouseholdScope_MemberAsRequesterSeesOwnerSharedNotOwnUnshared is the shape where the
// personal-predicate REPLACEMENT matters most, and the one the other guards leave uncovered: the
// CALLER is the household MEMBER, not the owner. The read is called with the member's user ID.
//
// Under the old personal predicate (`WHERE h.user_id = $1`) this call would have returned the
// member's OWN unshared holding and NOTHING of the owner's. The share-gated replacement must invert
// both: the owner's SHARED account's holding IS present, and the member's OWN UNSHARED account's
// holding is ABSENT.
func TestHoldingsHouseholdScope_MemberAsRequesterSeesOwnerSharedNotOwnUnshared(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	insertHouseholdScopeMember(t, pool, hh, owner, "owner")
	insertHouseholdScopeMember(t, pool, hh, member, "member")

	ownerLinkedItem := insertTestLinkedItem(t, pool, owner)
	memberLinkedItem := insertTestLinkedItem(t, pool, member)

	// The OWNER shares one account into the household; the MEMBER keeps their own account unshared.
	ownerShared := insertHouseholdScopeInvestmentAccount(t, pool, ownerLinkedItem, owner, "OWNER-SHARED-TO-MEMBER")
	memberPrivate := insertHouseholdScopeInvestmentAccount(t, pool, memberLinkedItem, member, "MEMBER-PRIVATE-BROKERAGE")

	insertHouseholdScopeHolding(t, pool, owner, ownerShared, "OSHR", "Owner Shared Fund", 1111.11)
	insertHouseholdScopeHolding(t, pool, member, memberPrivate, "MPRV", "Member Private Fund", 9999.99)

	now := time.Now().UTC()
	insertHouseholdShare(t, pool, hh, ownerShared.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))
	// memberPrivate gets NO share row.

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}

	// ── The read under test: the CALLER is the MEMBER. ──
	holdings, err := repository.NewHoldingRepository(pool).GetByUserIDWithSecurities(ctx, member, nil, scope)
	require.NoError(t, err, "GetByUserIDWithSecurities must not error")

	byAccount := holdingsByAccount(holdings)
	ownerHolding, ownerPresent := byAccount[ownerShared.AccountID]
	_, memberPresent := byAccount[memberPrivate.AccountID]
	t.Logf("member-requester household read returned %d holding(s); owner_shared=%s member_private=%s",
		len(holdings), ownerShared.AccountID, memberPrivate.AccountID)

	require.Len(t, holdings, 1,
		"a member-requester household read must return ONLY the owner's shared holding; the member's "+
			"own unshared account %q (%s, ticker MPRV) MUST BE ABSENT",
		memberPrivate.DisplayName, memberPrivate.AccountID)

	assert.True(t, ownerPresent,
		"the OWNER's shared account %q (%s) holding must be visible to the member — the personal "+
			"predicate `h.user_id = $1` would have excluded it, which is exactly what the share-gated "+
			"replacement fixes", ownerShared.DisplayName, ownerShared.AccountID)
	assert.False(t, memberPresent,
		"the MEMBER's own unshared account %q (%s) holding was returned under household scope. It is "+
			"never shared into household %s, so it MUST BE ABSENT (#1706).",
		memberPrivate.DisplayName, memberPrivate.AccountID, hh)
	assert.InDelta(t, 1111.11, valueOf(ownerHolding), 0.01, "owner shared holding value")
	assert.Equal(t, "OSHR", tickerOf(ownerHolding), "owner shared holding ticker")
}

// TestHoldingsHouseholdScope_HiddenVisibilityShareRowDoesNotContribute pins the CTE's
// `has.visibility = 'shared'` predicate directly: a share row whose visibility is 'hidden' is NOT a
// share, so its account's holding must not enter the household read. The hidden row is placed BESIDE
// an active 'shared' row so the exclusion is provably specific to the visibility value rather than
// the read returning nothing.
func TestHoldingsHouseholdScope_HiddenVisibilityShareRowDoesNotContribute(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	insertHouseholdScopeMember(t, pool, hh, owner, "owner")
	insertHouseholdScopeMember(t, pool, hh, member, "member")

	ownerLinkedItem := insertTestLinkedItem(t, pool, owner)
	hiddenAcct := insertHouseholdScopeInvestmentAccount(t, pool, ownerLinkedItem, owner, "OWNER-HIDDEN-VISIBILITY")
	sharedAcct := insertHouseholdScopeInvestmentAccount(t, pool, ownerLinkedItem, owner, "OWNER-SHARED-VISIBILITY")
	insertHouseholdScopeHolding(t, pool, owner, hiddenAcct, "HIDN", "Hidden Visibility Fund", 7777.77)
	insertHouseholdScopeHolding(t, pool, owner, sharedAcct, "SHWN", "Shared Visibility Fund", 1111.11)

	now := time.Now().UTC()
	// A share ROW exists for hiddenAcct, but its visibility is 'hidden' — not a share.
	insertHouseholdShare(t, pool, hh, hiddenAcct.UserAccountID, nil, domain.ShareVisibilityHidden, now.AddDate(0, -2, 0))
	insertHouseholdShare(t, pool, hh, sharedAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}
	holdings, err := repository.NewHoldingRepository(pool).GetByUserIDWithSecurities(ctx, owner, nil, scope)
	require.NoError(t, err, "GetByUserIDWithSecurities must not error")

	byAccount := holdingsByAccount(holdings)
	sharedHolding, sharedPresent := byAccount[sharedAcct.AccountID]
	_, hiddenPresent := byAccount[hiddenAcct.AccountID]
	t.Logf("household read returned %d holding(s); hidden_visibility=%s shared_visibility=%s",
		len(holdings), hiddenAcct.AccountID, sharedAcct.AccountID)

	require.Len(t, holdings, 1,
		"only the visibility='shared' row may contribute; the visibility='hidden' row's holding "+
			"(ticker HIDN) must be ABSENT")
	assert.False(t, hiddenPresent,
		"a share row with visibility='hidden' contributed a holding — household_grants_asof must "+
			"filter `has.visibility = 'shared'`")
	assert.True(t, sharedPresent,
		"the visibility='shared' row's holding must survive — a read that returned nothing would "+
			"also pass the hidden assertion, which is why this sibling exists")
	assert.InDelta(t, 1111.11, valueOf(sharedHolding), 0.01, "shared-visibility holding value")
	assert.Equal(t, "SHWN", tickerOf(sharedHolding), "shared-visibility holding ticker")
}
