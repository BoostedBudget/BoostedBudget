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
