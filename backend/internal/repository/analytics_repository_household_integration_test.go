// analytics_repository_household_integration_test.go — DB-backed behavior for the
// household-aware (`scope=household`) analytics queries added by #1369 (Phase 5).
//
// Per project rule: pgxmock never catches type/aggregate errors or exercises the real
// CTE fan-out — only a real Postgres proves the household_accounts CTE joins, dedups,
// and clips correctly (see memory feedback_pgxmock_integration_coverage.md).
//
// Run:
//
//	DATABASE_URL="postgres://..." go test ./internal/repository/ -run TestAnalyticsRepository_.*Household -v
package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// insertHouseholdShare shares userAccountID into household householdID with the given
// visibility and physical fingerprint (nil is fine for non-joint accounts), then backdates
// shared_at to sharedAt for deterministic clip-boundary testing. Returns the share row ID.
func insertHouseholdShare(t *testing.T, pool *pgxpool.Pool, householdID, userAccountID string, physicalBlindIdx *string, visibility domain.ShareVisibility, sharedAt time.Time) string {
	t.Helper()
	ctx := context.Background()
	shareRepo := repository.NewHouseholdShareRepository(pool)
	require.NoError(t, shareRepo.InsertShare(ctx, householdID, userAccountID, physicalBlindIdx, visibility))

	var id string
	err := pool.QueryRow(ctx, `
		UPDATE household_account_shares SET shared_at = $1
		WHERE household_id = $2 AND user_account_id = $3 AND revoked_at IS NULL
		RETURNING id::text
	`, sharedAt, householdID, userAccountID).Scan(&id)
	require.NoError(t, err, "backdate shared_at")
	return id
}

// insertHouseholdTransaction inserts one expense transaction (amount > 0, PFCv2 convention)
// for accountID/userID on the given date, tagged with pfcPrimary as its category.
// effective_category_name is a plain (non-generated) column normally populated by the Go
// application layer at write time — raw test inserts must set it explicitly or
// GetSpendingByCategory's non-pointer string scan fails on NULL.
func insertHouseholdTransaction(t *testing.T, pool *pgxpool.Pool, userID, accountID, linkedItemID string, amount float64, date time.Time, pfcPrimary string) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO transactions (user_id, account_id, linked_item_id, source, name, merchant_name, amount, date, pending, pfc_primary, effective_category_name)
		VALUES ($1, $2, $3, 'plaid', 'Test Txn', 'Test Merchant', $4, $5, false, $6, $6)
	`, userID, accountID, linkedItemID, amount, date.Format("2006-01-02"), pfcPrimary)
	require.NoError(t, err, "seed household transaction")
}

// householdTestFixture is one member's full linkage chain: user -> user_account ->
// linked_item -> accounts row (status='active'). Returned so tests can share it into a
// household and post transactions against it.
type householdTestFixture struct {
	UserID        string
	UserAccountID string
	AccountID     string
	LinkedItemID  string
}

func setupHouseholdMemberAccount(t *testing.T, pool *pgxpool.Pool, userID string) householdTestFixture {
	t.Helper()
	ua := insertTestUserAccount(t, pool, userID)
	li := insertTestLinkedItem(t, pool, userID)
	acc := insertTestAccount(t, pool, li, userID, ua)
	return householdTestFixture{UserID: userID, UserAccountID: ua, AccountID: acc, LinkedItemID: li}
}

func TestAnalyticsRepository_GetSpendingByCategory_Household_IncludesSharedMemberAccount(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)

	ownerAcct := setupHouseholdMemberAccount(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)

	now := time.Now().UTC()
	insertHouseholdShare(t, pool, hh, ownerAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))

	insertHouseholdTransaction(t, pool, owner, ownerAcct.AccountID, ownerAcct.LinkedItemID, 100, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")
	insertHouseholdTransaction(t, pool, member, memberAcct.AccountID, memberAcct.LinkedItemID, 50, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")

	filters := domain.SpendingFilters{
		StartDate: now.AddDate(0, -1, 0),
		EndDate:   now,
		HouseholdScope: &domain.HouseholdScope{
			HouseholdID:   hh,
			MemberUserIDs: []string{owner, member},
		},
	}

	results, err := repo.GetSpendingByCategory(ctx, owner, filters)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "FOOD_AND_DRINK", results[0].Category)
	assert.InDelta(t, 150, results[0].Total, 0.01, "owner's $100 + member's $50 shared account must both be counted")
	assert.Equal(t, 2, results[0].TransactionCount)
}

func TestAnalyticsRepository_GetSpendingByCategory_Household_ExcludesHiddenVisibilityShare(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)

	ownerAcct := setupHouseholdMemberAccount(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)

	now := time.Now().UTC()
	insertHouseholdShare(t, pool, hh, ownerAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))
	// Member's account is explicitly HIDDEN from the household aggregate.
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityHidden, now.AddDate(0, -2, 0))

	insertHouseholdTransaction(t, pool, owner, ownerAcct.AccountID, ownerAcct.LinkedItemID, 100, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")
	insertHouseholdTransaction(t, pool, member, memberAcct.AccountID, memberAcct.LinkedItemID, 999, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")

	filters := domain.SpendingFilters{
		StartDate: now.AddDate(0, -1, 0),
		EndDate:   now,
		HouseholdScope: &domain.HouseholdScope{
			HouseholdID:   hh,
			MemberUserIDs: []string{owner, member},
		},
	}

	results, err := repo.GetSpendingByCategory(ctx, owner, filters)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.InDelta(t, 100, results[0].Total, 0.01, "hidden-visibility share must be excluded entirely")
}

// revokeHouseholdShareAt closes the member's open grant in this household at an explicit instant.
// RevokeSharesForUser is the real production path (member removal / self-leave) and sets
// revoked_at = now(); the follow-up UPDATE backdates it so a test can place transactions on BOTH
// sides of the revocation deterministically.
//
// The instant is supplied from Go rather than read back from the DB on purpose: the Docker Postgres
// clock runs about a millisecond behind the host, so a DB-generated revoked_at compared against a
// Go-generated transaction date is a flaky comparison waiting to happen.
func revokeHouseholdShareAt(t *testing.T, pool *pgxpool.Pool, householdID, memberUserID, userAccountID string, revokedAt time.Time) {
	t.Helper()
	ctx := context.Background()

	// Capture the OPEN grant first. Backdating by (household, user_account) alone would also rewrite
	// grants closed by an earlier call, which is exactly what the re-fingerprinted fixture builds --
	// the id is what keeps repeated revocations independent.
	var shareID string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT id::text FROM household_account_shares
		WHERE household_id = $1 AND user_account_id = $2 AND revoked_at IS NULL
	`, householdID, userAccountID).Scan(&shareID), "expected exactly one open grant to revoke")

	shareRepo := repository.NewHouseholdShareRepository(pool)
	require.NoError(t, shareRepo.RevokeSharesForUser(ctx, householdID, memberUserID))

	tag, err := pool.Exec(ctx, `
		UPDATE household_account_shares SET revoked_at = $1
		WHERE id = $2 AND revoked_at IS NOT NULL
	`, revokedAt, shareID)
	require.NoError(t, err, "backdate revoked_at")
	require.Equal(t, int64(1), tag.RowsAffected(), "expected the revoked grant to be backdated")
}

// TestAnalyticsRepository_GetSpendingByCategory_Household_DepartedMemberSharedPeriodRetained is the
// #1569 T23 behaviour change, and it REPLACES a test that asserted the opposite
// (..._RevokedShareExcludedImmediately, which required a revoked share to drop out of the aggregate
// entirely, past included).
//
// That old rule erased a departed member's whole history from household spending, income and
// savings-rate, while the balance/net-worth reads had already been fixed to keep the shared period.
// The two universes disagreed about the same days. The rule now matches the balance side: the
// household keeps exactly what was shared with it, for exactly as long as it was shared, and gains
// nothing after the member leaves.
//
// Both directions are asserted here, because a fix that only widened would be just as wrong as the
// bug: the pre-revocation transaction must be IN and the post-revocation one must be OUT.
func TestAnalyticsRepository_GetSpendingByCategory_Household_DepartedMemberSharedPeriodRetained(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner, member, hh, ownerAcct, memberAcct := setupTwoMemberSharedHousehold(t, pool)
	now := time.Now().UTC()

	insertHouseholdTransaction(t, pool, owner, ownerAcct.AccountID, ownerAcct.LinkedItemID, 100, now.AddDate(0, 0, -10), "FOOD_AND_DRINK")
	// Inside the shared period (shared 2 months ago, revoked 3 days ago) -- must be RETAINED.
	insertHouseholdTransaction(t, pool, member, memberAcct.AccountID, memberAcct.LinkedItemID, 999, now.AddDate(0, 0, -10), "FOOD_AND_DRINK")
	// After the member left -- must NOT appear; the household never had visibility of it.
	insertHouseholdTransaction(t, pool, member, memberAcct.AccountID, memberAcct.LinkedItemID, 777, now.AddDate(0, 0, -1), "FOOD_AND_DRINK")

	revokeHouseholdShareAt(t, pool, hh, member, memberAcct.UserAccountID, now.AddDate(0, 0, -3))

	filters := domain.SpendingFilters{
		StartDate: now.AddDate(0, -1, 0),
		EndDate:   now,
		HouseholdScope: &domain.HouseholdScope{
			HouseholdID: hh,
			// The member has DEPARTED: they are no longer an active member. Their closed grant is
			// what admits their shared period, which is the membership relaxation the balance CTEs
			// already carry.
			MemberUserIDs: []string{owner},
		},
	}

	results, err := repo.GetSpendingByCategory(ctx, owner, filters)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.InDelta(t, 1099, results[0].Total, 0.01,
		"owner's $100 + departed member's in-period $999; the post-revocation $777 must be excluded")
	assert.Equal(t, 2, results[0].TransactionCount)
}

// TestAnalyticsRepository_GetSpendingByCategory_Household_RefingerprintedClosedGrantsCountedOnce is
// the regression that retention would otherwise have shipped, and it is the reason this fix is a
// SEMI-JOIN rather than a widened JOIN.
//
// household_grants emits one row per GRANT. A single account that was shared, re-fingerprinted
// (a Plaid re-link can change physical_account_blind_idx) and shared again owns two closed grants
// whose intervals both cover the same day. Under the old `JOIN household_accounts ha ON
// ha.user_account_id = a.user_account_id`, that account would match twice and every one of its
// transactions would land twice in SUM() and COUNT() -- silent money inflation across nine
// endpoints, strictly worse than the under-reporting being fixed.
//
// EXISTS makes that unrepresentable: it answers once per transaction however many grants cover it.
// The assertion is the EXACT total, not merely a non-zero one -- the defect it guards against
// doubles the number, so an assertion that tolerated 500 would not see it.
func TestAnalyticsRepository_GetSpendingByCategory_Household_RefingerprintedClosedGrantsCountedOnce(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)

	now := time.Now().UTC()
	txnDate := now.AddDate(0, 0, -10)

	// Grant 1: shared 3 months ago under the pre-relink fingerprint, revoked 2 days ago.
	fpBefore := "fp-before-" + hh
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, &fpBefore, domain.ShareVisibilityShared, now.AddDate(0, -3, 0))
	revokeHouseholdShareAt(t, pool, hh, member, memberAcct.UserAccountID, now.AddDate(0, 0, -2))

	// Grant 2: the SAME user_account shared again under a NEW fingerprint, over an interval that
	// also covers txnDate, and likewise now closed. Two closed grants, one user_account.
	fpAfter := "fp-after-" + hh
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, &fpAfter, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))
	revokeHouseholdShareAt(t, pool, hh, member, memberAcct.UserAccountID, now.AddDate(0, 0, -1))

	var grants int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM household_account_shares
		WHERE household_id = $1 AND user_account_id = $2 AND revoked_at IS NOT NULL
	`, hh, memberAcct.UserAccountID).Scan(&grants))
	require.Equal(t, 2, grants, "fixture must produce TWO closed grants, or this proves nothing")

	insertHouseholdTransaction(t, pool, member, memberAcct.AccountID, memberAcct.LinkedItemID, 250, txnDate, "FOOD_AND_DRINK")

	filters := domain.SpendingFilters{
		StartDate: now.AddDate(0, -1, 0),
		EndDate:   now,
		HouseholdScope: &domain.HouseholdScope{
			HouseholdID:   hh,
			MemberUserIDs: []string{owner},
		},
	}

	results, err := repo.GetSpendingByCategory(ctx, owner, filters)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.InDelta(t, 250, results[0].Total, 0.01,
		"two closed grants covering the same day must not double the money: $250, never $500")
	assert.Equal(t, 1, results[0].TransactionCount,
		"two closed grants covering the same day must not double the transaction count")
}

// TestAnalyticsRepository_Household_BalanceAndSpendingUniversesAgreeOverSharedPeriod pins the
// property #1569 Decision 7 previously recorded as ABSENT: household spending and household
// net-worth answered about the same days out of different account universes, and no test asserted
// they agreed because they did not.
//
// They do now. One departed member, one closed grant, one transaction and one balance snapshot both
// inside the shared period: both reads must see the member's account. If either universe drifts
// from the other again, this fails.
func TestAnalyticsRepository_Household_BalanceAndSpendingUniversesAgreeOverSharedPeriod(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	analytics := repository.NewAnalyticsRepository(pool)
	balances := repository.NewBalanceSnapshotRepository(pool)

	owner, member, hh, ownerAcct, memberAcct := setupTwoMemberSharedHousehold(t, pool)
	now := time.Now().UTC()
	inPeriod := now.AddDate(0, 0, -10)

	insertHouseholdTransaction(t, pool, owner, ownerAcct.AccountID, ownerAcct.LinkedItemID, 100, inPeriod, "FOOD_AND_DRINK")
	insertHouseholdTransaction(t, pool, member, memberAcct.AccountID, memberAcct.LinkedItemID, 400, inPeriod, "FOOD_AND_DRINK")
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 1000, inPeriod)
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 500, inPeriod)

	// The member leaves AFTER both the transaction and the snapshot.
	revokeHouseholdShareAt(t, pool, hh, member, memberAcct.UserAccountID, now.AddDate(0, 0, -3))

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner}}

	results, err := analytics.GetSpendingByCategory(ctx, owner, domain.SpendingFilters{
		StartDate:      now.AddDate(0, -1, 0),
		EndDate:        now,
		HouseholdScope: scope,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.InDelta(t, 500, results[0].Total, 0.01,
		"SPENDING must include the departed member's in-period $400 alongside the owner's $100")

	snapshots, err := balances.GetSnapshotsAsOfForHousehold(ctx, owner, scope, inPeriod.AddDate(0, 0, 1))
	require.NoError(t, err)
	var balanceTotal float64
	for _, snap := range snapshots {
		if snap.CurrentBalance != nil {
			balanceTotal += *snap.CurrentBalance
		}
	}
	assert.InDelta(t, 1500, balanceTotal, 0.01,
		"BALANCE must include the same departed member's account over the same shared period")
}

func TestAnalyticsRepository_GetSpendingByCategory_Household_IsHiddenAccountStillIncluded(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)

	memberAcct := setupHouseholdMemberAccount(t, pool, member)
	// Mark the underlying accounts row is_hidden=true (the member's OWN personal-view
	// preference) — must NOT affect household inclusion (decision #3: visibility-only).
	_, err := pool.Exec(ctx, `UPDATE accounts SET is_hidden = true WHERE id = $1`, memberAcct.AccountID)
	require.NoError(t, err)

	now := time.Now().UTC()
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))
	insertHouseholdTransaction(t, pool, member, memberAcct.AccountID, memberAcct.LinkedItemID, 75, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")

	filters := domain.SpendingFilters{
		StartDate: now.AddDate(0, -1, 0),
		EndDate:   now,
		HouseholdScope: &domain.HouseholdScope{
			HouseholdID:   hh,
			MemberUserIDs: []string{owner, member},
		},
	}

	results, err := repo.GetSpendingByCategory(ctx, owner, filters)
	require.NoError(t, err)
	require.Len(t, results, 1, "is_hidden=true on a SHARED account must still be included")
	assert.InDelta(t, 75, results[0].Total, 0.01)
}

func TestAnalyticsRepository_GetSpendingByCategory_Household_ClipsToSharedAt(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)

	memberAcct := setupHouseholdMemberAccount(t, pool, member)

	now := time.Now().UTC()
	sharedAt := now.AddDate(0, 0, -10)
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityShared, sharedAt)

	// Before shared_at — must be excluded.
	insertHouseholdTransaction(t, pool, member, memberAcct.AccountID, memberAcct.LinkedItemID, 40, sharedAt.AddDate(0, 0, -1), "FOOD_AND_DRINK")
	// Same day as shared_at (shared_at has a non-midnight time-of-day) — must be INCLUDED
	// (Codex P1-4: TIMESTAMPTZ->date cast so same-day transactions are not dropped).
	insertHouseholdTransaction(t, pool, member, memberAcct.AccountID, memberAcct.LinkedItemID, 25, sharedAt, "FOOD_AND_DRINK")
	// After shared_at — must be included.
	insertHouseholdTransaction(t, pool, member, memberAcct.AccountID, memberAcct.LinkedItemID, 10, sharedAt.AddDate(0, 0, 1), "FOOD_AND_DRINK")

	filters := domain.SpendingFilters{
		StartDate: now.AddDate(0, -1, 0),
		EndDate:   now,
		HouseholdScope: &domain.HouseholdScope{
			HouseholdID:   hh,
			MemberUserIDs: []string{owner, member},
		},
	}

	results, err := repo.GetSpendingByCategory(ctx, owner, filters)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.InDelta(t, 35, results[0].Total, 0.01, "only the same-day and after-shared_at transactions ($25+$10) should count")
	assert.Equal(t, 2, results[0].TransactionCount)
}

func TestAnalyticsRepository_GetSpendingByCategory_Household_JointAccountCountedOnce(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)

	ownerAcct := setupHouseholdMemberAccount(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)

	now := time.Now().UTC()
	fingerprint := "joint-fp-" + hh
	// Both members' copies of the SAME joint account share the same physical fingerprint.
	// Owner shared first (earlier shared_at) — the CTE must pick the owner's copy.
	insertHouseholdShare(t, pool, hh, ownerAcct.UserAccountID, &fingerprint, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, &fingerprint, domain.ShareVisibilityShared, now.AddDate(0, -1, 0))

	insertHouseholdTransaction(t, pool, owner, ownerAcct.AccountID, ownerAcct.LinkedItemID, 60, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")
	insertHouseholdTransaction(t, pool, member, memberAcct.AccountID, memberAcct.LinkedItemID, 60, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")

	filters := domain.SpendingFilters{
		StartDate: now.AddDate(0, -1, 0).AddDate(0, -1, 0),
		EndDate:   now,
		HouseholdScope: &domain.HouseholdScope{
			HouseholdID:   hh,
			MemberUserIDs: []string{owner, member},
		},
	}

	results, err := repo.GetSpendingByCategory(ctx, owner, filters)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.InDelta(t, 60, results[0].Total, 0.01, "joint account must be counted once (owner's earliest-shared copy), not $120")
	assert.Equal(t, 1, results[0].TransactionCount)
}

func TestAnalyticsRepository_GetSpendingByCategory_Household_CustomCategoryNamespaceSafeToRequester(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)

	ownerAcct := setupHouseholdMemberAccount(t, pool, owner)
	now := time.Now().UTC()
	insertHouseholdShare(t, pool, hh, ownerAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))

	// Owner has one custom category of their own.
	var ownerCustomCatID string
	err := pool.QueryRow(ctx, `
		INSERT INTO custom_categories (user_id, name) VALUES ($1, 'Owner Custom')
		RETURNING id::text
	`, owner).Scan(&ownerCustomCatID)
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM custom_categories WHERE id = $1`, ownerCustomCatID) //nolint:errcheck
	})

	_, err = pool.Exec(ctx, `
		INSERT INTO transactions (user_id, account_id, linked_item_id, source, name, merchant_name, amount, date, pending,
			category_override_kind, custom_category_override_id, effective_category_name)
		VALUES ($1, $2, $3, 'plaid', 'Test Txn', 'Test Merchant', 30, $4, false, 'custom', $5, 'Owner Custom')
	`, owner, ownerAcct.AccountID, ownerAcct.LinkedItemID, now.AddDate(0, 0, -5).Format("2006-01-02"), ownerCustomCatID)
	require.NoError(t, err)

	filters := domain.SpendingFilters{
		StartDate:         now.AddDate(0, -1, 0),
		EndDate:           now,
		CustomCategoryIDs: []string{ownerCustomCatID},
		HouseholdScope: &domain.HouseholdScope{
			HouseholdID:   hh,
			MemberUserIDs: []string{owner, member},
		},
	}

	// Scoping to the requester's own custom category must still resolve correctly in
	// household mode — $1 stays the requesting user for the namespace subquery.
	results, err := repo.GetSpendingByCategory(ctx, owner, filters)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.InDelta(t, 30, results[0].Total, 0.01)
}

// TestAnalyticsRepository_GetSpendingByCategory_Household_CustomCategoryRollUpFalse_RejectsForeignCategory
// closes gap-closure G4: with roll_up=false, buildSpendingFilter's else-branch had no
// user_id ownership check on custom_category_override_id (unlike the RollUpChildren=true
// branch, which scopes via a `user_id = $1::uuid` subquery). Inert in personal mode (the
// outer query's `t.user_id = $1` already constrains to the requester's own transactions),
// but live in household mode: a requester supplying another member's custom category ID
// with roll_up=false must not see that member's spending.
func TestAnalyticsRepository_GetSpendingByCategory_Household_CustomCategoryRollUpFalse_RejectsForeignCategory(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner, member, hh, _, memberAcct := setupTwoMemberSharedHousehold(t, pool)
	now := time.Now().UTC()

	// Member has their OWN custom category and a transaction using it.
	var memberCustomCatID string
	err := pool.QueryRow(ctx, `
		INSERT INTO custom_categories (user_id, name) VALUES ($1, 'Member Custom')
		RETURNING id::text
	`, member).Scan(&memberCustomCatID)
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM custom_categories WHERE id = $1`, memberCustomCatID) //nolint:errcheck
	})
	_, err = pool.Exec(ctx, `
		INSERT INTO transactions (user_id, account_id, linked_item_id, source, name, merchant_name, amount, date, pending,
			category_override_kind, custom_category_override_id, effective_category_name)
		VALUES ($1, $2, $3, 'plaid', 'Test Txn', 'Test Merchant', 999, $4, false, 'custom', $5, 'Member Custom')
	`, member, memberAcct.AccountID, memberAcct.LinkedItemID, now.AddDate(0, 0, -5).Format("2006-01-02"), memberCustomCatID)
	require.NoError(t, err)

	// Owner (the requester) probes with the MEMBER's category ID, roll_up=false.
	filters := domain.SpendingFilters{
		StartDate:         now.AddDate(0, -1, 0),
		EndDate:           now,
		CustomCategoryIDs: []string{memberCustomCatID},
		RollUpChildren:    false,
		HouseholdScope: &domain.HouseholdScope{
			HouseholdID:   hh,
			MemberUserIDs: []string{owner, member},
		},
	}

	results, err := repo.GetSpendingByCategory(ctx, owner, filters)
	require.NoError(t, err)
	assert.Empty(t, results, "requester must not see another member's custom-category spending via roll_up=false")
}

// TestAnalyticsRepository_GetSpendingByCategory_Household_ThreeActiveMembers closes gap
// G3: every other household test uses exactly 2 members; this proves the CTE's
// `ua.user_id = ANY($3)` fan-out and aggregation scale correctly to 3+.
func TestAnalyticsRepository_GetSpendingByCategory_Household_ThreeActiveMembers(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner := insertTestUser(t, pool)
	member2 := insertTestUser(t, pool)
	member3 := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)

	ownerAcct := setupHouseholdMemberAccount(t, pool, owner)
	member2Acct := setupHouseholdMemberAccount(t, pool, member2)
	member3Acct := setupHouseholdMemberAccount(t, pool, member3)

	now := time.Now().UTC()
	insertHouseholdShare(t, pool, hh, ownerAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))
	insertHouseholdShare(t, pool, hh, member2Acct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))
	insertHouseholdShare(t, pool, hh, member3Acct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))

	insertHouseholdTransaction(t, pool, owner, ownerAcct.AccountID, ownerAcct.LinkedItemID, 100, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")
	insertHouseholdTransaction(t, pool, member2, member2Acct.AccountID, member2Acct.LinkedItemID, 50, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")
	insertHouseholdTransaction(t, pool, member3, member3Acct.AccountID, member3Acct.LinkedItemID, 25, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")

	filters := domain.SpendingFilters{
		StartDate: now.AddDate(0, -1, 0),
		EndDate:   now,
		HouseholdScope: &domain.HouseholdScope{
			HouseholdID:   hh,
			MemberUserIDs: []string{owner, member2, member3},
		},
	}

	results, err := repo.GetSpendingByCategory(ctx, owner, filters)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.InDelta(t, 175, results[0].Total, 0.01, "all 3 members' shared-account spending ($100+$50+$25) must be aggregated together")
	assert.Equal(t, 3, results[0].TransactionCount)
}

// ─── GetCashFlowAggregation / GetCashFlowGrouped — household scope ───────────
//
// The household_accounts CTE fan-out is already exhaustively covered above (visibility,
// is_hidden, shared_at clip, joint-account dedup, custom-category namespace safety) via
// GetSpendingByCategory. These cash-flow tests confirm the same CTE wires correctly into
// the income/expense aggregation shape — not re-litigate every fan-out edge case.

func TestAnalyticsRepository_GetCashFlowAggregation_Household_IncludesSharedMemberAccount(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)

	ownerAcct := setupHouseholdMemberAccount(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)

	now := time.Now().UTC()
	insertHouseholdShare(t, pool, hh, ownerAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))

	// Expense (amount > 0) on each account.
	insertHouseholdTransaction(t, pool, owner, ownerAcct.AccountID, ownerAcct.LinkedItemID, 80, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")
	insertHouseholdTransaction(t, pool, member, memberAcct.AccountID, memberAcct.LinkedItemID, 20, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")
	// Income (amount < 0) on the member's account only.
	insertHouseholdTransaction(t, pool, member, memberAcct.AccountID, memberAcct.LinkedItemID, -500, now.AddDate(0, 0, -5), "INCOME")

	filters := domain.CashFlowFilters{
		StartDate: now.AddDate(0, -1, 0),
		EndDate:   now,
		HouseholdScope: &domain.HouseholdScope{
			HouseholdID:   hh,
			MemberUserIDs: []string{owner, member},
		},
	}

	agg, err := repo.GetCashFlowAggregation(ctx, owner, filters)
	require.NoError(t, err)
	assert.InDelta(t, 500, agg.Income, 0.01, "member's income must be included in household aggregation")
	assert.InDelta(t, 100, agg.Expenses, 0.01, "owner's $80 + member's $20 expenses must both be counted")
}

func TestAnalyticsRepository_GetCashFlowAggregation_Household_ExcludesHiddenVisibilityShare(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)

	ownerAcct := setupHouseholdMemberAccount(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)

	now := time.Now().UTC()
	insertHouseholdShare(t, pool, hh, ownerAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityHidden, now.AddDate(0, -2, 0))

	insertHouseholdTransaction(t, pool, owner, ownerAcct.AccountID, ownerAcct.LinkedItemID, 80, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")
	insertHouseholdTransaction(t, pool, member, memberAcct.AccountID, memberAcct.LinkedItemID, 999, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")

	filters := domain.CashFlowFilters{
		StartDate: now.AddDate(0, -1, 0),
		EndDate:   now,
		HouseholdScope: &domain.HouseholdScope{
			HouseholdID:   hh,
			MemberUserIDs: []string{owner, member},
		},
	}

	agg, err := repo.GetCashFlowAggregation(ctx, owner, filters)
	require.NoError(t, err)
	assert.InDelta(t, 80, agg.Expenses, 0.01, "hidden-visibility share must be excluded")
}

func TestAnalyticsRepository_GetCashFlowGrouped_Household_IncludesSharedMemberAccount(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)

	ownerAcct := setupHouseholdMemberAccount(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)

	now := time.Now().UTC()
	insertHouseholdShare(t, pool, hh, ownerAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))

	sameDay := now.AddDate(0, 0, -5)
	insertHouseholdTransaction(t, pool, owner, ownerAcct.AccountID, ownerAcct.LinkedItemID, 80, sameDay, "FOOD_AND_DRINK")
	insertHouseholdTransaction(t, pool, member, memberAcct.AccountID, memberAcct.LinkedItemID, 20, sameDay, "FOOD_AND_DRINK")

	filters := domain.CashFlowFilters{
		StartDate: now.AddDate(0, -1, 0),
		EndDate:   now,
		Grouping:  "daily",
		HouseholdScope: &domain.HouseholdScope{
			HouseholdID:   hh,
			MemberUserIDs: []string{owner, member},
		},
	}

	results, err := repo.GetCashFlowGrouped(ctx, owner, filters)
	require.NoError(t, err)

	var total float64
	for _, r := range results {
		total += r.Expenses
	}
	assert.InDelta(t, 100, total, 0.01, "owner's $80 + member's $20 must both appear across the grouped periods")
}

// ─── GetSpendingByMerchant / GetSpendingTrends / GetIncomeSourceBreakdown /
// GetAverageMonthlySpend / GetMonthOverMonthSpending — household scope wiring ─────
//
// The household_accounts CTE fan-out itself is exhaustively covered above via
// GetSpendingByCategory. These confirm each remaining SpendingFilters-based method wires
// the same CTE correctly — one shared-account-inclusion check per method.

func setupTwoMemberSharedHousehold(t *testing.T, pool *pgxpool.Pool) (owner, member, hh string, ownerAcct, memberAcct householdTestFixture) {
	t.Helper()
	owner = insertTestUser(t, pool)
	member = insertTestUser(t, pool)
	hh = insertTestHousehold(t, pool, owner)
	ownerAcct = setupHouseholdMemberAccount(t, pool, owner)
	memberAcct = setupHouseholdMemberAccount(t, pool, member)
	now := time.Now().UTC()
	insertHouseholdShare(t, pool, hh, ownerAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))
	insertHouseholdShare(t, pool, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))
	return owner, member, hh, ownerAcct, memberAcct
}

func TestAnalyticsRepository_GetSpendingByMerchant_Household_IncludesSharedMemberAccount(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner, member, hh, ownerAcct, memberAcct := setupTwoMemberSharedHousehold(t, pool)
	now := time.Now().UTC()
	insertHouseholdTransaction(t, pool, owner, ownerAcct.AccountID, ownerAcct.LinkedItemID, 80, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")
	insertHouseholdTransaction(t, pool, member, memberAcct.AccountID, memberAcct.LinkedItemID, 20, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")

	filters := domain.SpendingFilters{
		StartDate:      now.AddDate(0, -1, 0),
		EndDate:        now,
		HouseholdScope: &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}},
	}

	results, err := repo.GetSpendingByMerchant(ctx, owner, filters, 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.InDelta(t, 100, results[0].Total, 0.01, "owner's $80 + member's $20 must both be counted")
}

func TestAnalyticsRepository_GetSpendingTrends_Household_IncludesSharedMemberAccount(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner, member, hh, ownerAcct, memberAcct := setupTwoMemberSharedHousehold(t, pool)
	now := time.Now().UTC()
	insertHouseholdTransaction(t, pool, owner, ownerAcct.AccountID, ownerAcct.LinkedItemID, 80, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")
	insertHouseholdTransaction(t, pool, member, memberAcct.AccountID, memberAcct.LinkedItemID, 20, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")

	filters := domain.SpendingFilters{
		StartDate:      now.AddDate(0, -1, 0),
		EndDate:        now,
		HouseholdScope: &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}},
	}

	results, err := repo.GetSpendingTrends(ctx, owner, filters)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.InDelta(t, 100, results[0].Total, 0.01, "owner's $80 + member's $20 must both be counted")
}

func TestAnalyticsRepository_GetIncomeSourceBreakdown_Household_IncludesSharedMemberAccount(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner, member, hh, ownerAcct, memberAcct := setupTwoMemberSharedHousehold(t, pool)
	now := time.Now().UTC()
	insertHouseholdTransaction(t, pool, owner, ownerAcct.AccountID, ownerAcct.LinkedItemID, -1000, now.AddDate(0, 0, -5), "INCOME")
	insertHouseholdTransaction(t, pool, member, memberAcct.AccountID, memberAcct.LinkedItemID, -500, now.AddDate(0, 0, -5), "INCOME")

	filters := domain.SpendingFilters{
		StartDate:      now.AddDate(0, -1, 0),
		EndDate:        now,
		HouseholdScope: &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}},
	}

	results, err := repo.GetIncomeSourceBreakdown(ctx, owner, filters)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.InDelta(t, 1500, results[0].Total, 0.01, "owner's $1000 + member's $500 income must both be counted")
}

func TestAnalyticsRepository_GetAverageMonthlySpend_Household_IncludesSharedMemberAccount(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner, member, hh, ownerAcct, memberAcct := setupTwoMemberSharedHousehold(t, pool)
	now := time.Now().UTC()
	insertHouseholdTransaction(t, pool, owner, ownerAcct.AccountID, ownerAcct.LinkedItemID, 80, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")
	insertHouseholdTransaction(t, pool, member, memberAcct.AccountID, memberAcct.LinkedItemID, 20, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")

	filters := domain.SpendingFilters{
		StartDate:      now.AddDate(0, -1, 0),
		EndDate:        now,
		HouseholdScope: &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}},
	}

	row, err := repo.GetAverageMonthlySpend(ctx, owner, filters)
	require.NoError(t, err)
	assert.InDelta(t, 100, row.Total, 0.01, "owner's $80 + member's $20 must both be counted")
}

func TestAnalyticsRepository_GetMonthOverMonthSpending_Household_IncludesSharedMemberAccount(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner, member, hh, ownerAcct, memberAcct := setupTwoMemberSharedHousehold(t, pool)
	now := time.Now().UTC()
	insertHouseholdTransaction(t, pool, owner, ownerAcct.AccountID, ownerAcct.LinkedItemID, 80, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")
	insertHouseholdTransaction(t, pool, member, memberAcct.AccountID, memberAcct.LinkedItemID, 20, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")

	filters := domain.SpendingFilters{
		HouseholdScope: &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}},
	}

	results, err := repo.GetMonthOverMonthSpending(ctx, owner, now.AddDate(0, -1, 0), "", filters)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.InDelta(t, 100, results[0].Total, 0.01, "owner's $80 + member's $20 must both be counted")
}

func TestAnalyticsRepository_GetSavingsRateForPeriod_Household_IncludesSharedMemberAccount(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner, member, hh, ownerAcct, memberAcct := setupTwoMemberSharedHousehold(t, pool)
	now := time.Now().UTC()
	insertHouseholdTransaction(t, pool, owner, ownerAcct.AccountID, ownerAcct.LinkedItemID, -1000, now.AddDate(0, 0, -5), "INCOME")
	insertHouseholdTransaction(t, pool, member, memberAcct.AccountID, memberAcct.LinkedItemID, -500, now.AddDate(0, 0, -5), "INCOME")

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}
	row, err := repo.GetSavingsRateForPeriod(ctx, owner, now.AddDate(0, -1, 0), now, scope)
	require.NoError(t, err)
	assert.InDelta(t, 1500, row.Income, 0.01, "owner's $1000 + member's $500 income must both be counted")
}

// TestAnalyticsRepository_GetSpendingByCategory_Household_IncludesReplacedAccountTransactions is the
// household counterpart of the personal-scope guarantee, and it closes a real scope divergence.
//
// #1109 deliberately REMOVED `AND a.status = 'active'` from the personal-scope analytics joins, with
// the stated reason "pre-relink transactions are immutable historical events; status filter
// incorrectly drops them", and renamed its tests to *_IncludesReplacedAccountTransactions to pin
// that (see analytics_spending_repository_test.go). Spending that happened, happened — re-linking an
// institution later does not un-spend it.
//
// #1369's household variants then reintroduced the filter on 9 join sites. The result was that the
// SAME user, over the SAME period, saw different totals depending on scope, and the household number
// was the one silently missing real history. Nothing pinned that behaviour: the household tests only
// ever seeded status='active' accounts, and #1369's commit message justifies "active" in terms of
// household MEMBERSHIP (ActiveMemberUserIDs / ResolveScope) — a different concept the SQL then
// applied to accounts.status.
//
// accounts_valid_status admits exactly ('active','replaced','expired'), so the filter dropped both
// re-linked and expired history.
func TestAnalyticsRepository_GetSpendingByCategory_Household_IncludesReplacedAccountTransactions(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)

	activeAcct := setupHouseholdMemberAccount(t, pool, owner)
	replacedAcct := setupHouseholdMemberAccount(t, pool, owner)

	now := time.Now().UTC()
	insertHouseholdShare(t, pool, hh, activeAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))
	insertHouseholdShare(t, pool, hh, replacedAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))

	// The institution was re-linked: the old accounts row is 'replaced', but the spending it recorded
	// is immutable history.
	// accounts_replaced_at_consistency requires replaced_at alongside status='replaced'.
	_, err := pool.Exec(ctx,
		`UPDATE accounts SET status = 'replaced', replaced_at = now() WHERE id = $1`, replacedAcct.AccountID)
	require.NoError(t, err, "mark the account replaced")

	insertHouseholdTransaction(t, pool, owner, activeAcct.AccountID, activeAcct.LinkedItemID, 100, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")
	insertHouseholdTransaction(t, pool, owner, replacedAcct.AccountID, replacedAcct.LinkedItemID, 40, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")

	filters := domain.SpendingFilters{
		StartDate: now.AddDate(0, -1, 0),
		EndDate:   now,
		HouseholdScope: &domain.HouseholdScope{
			HouseholdID:   hh,
			MemberUserIDs: []string{owner},
		},
	}

	results, err := repo.GetSpendingByCategory(ctx, owner, filters)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.InDelta(t, 140, results[0].Total, 0.01,
		"household scope must include the replaced account's $40 alongside the active account's $100. "+
			"A total of 100 means household scope is still filtering accounts.status='active' and is "+
			"dropping pre-relink history that personal scope deliberately keeps (#1109) — the same user "+
			"would see 140 in personal scope and 100 here.")
	assert.Equal(t, 2, results[0].TransactionCount,
		"both transactions are real spending regardless of the account's current link state")
}

// ─── Non-active account inclusion across ALL household join sites ────────────────
//
// The test above pins getSpendingByCategoryHousehold only. #1369 reintroduced
// `AND a.status = 'active'` on NINE household join sites; every other pre-existing
// household test seeds status='active' accounts exclusively, so re-adding the predicate
// to any of the other eight left the suite green. The tests below close that hole, one
// per remaining join site:
//
//	analytics_repository.go join site -> unexported variant -> exported method
//	~168   getCashFlowAggregationHousehold      -> GetCashFlowAggregation
//	~319   getCashFlowGroupedHousehold          -> GetCashFlowGrouped
//	~874   getSpendingByMerchantHousehold       -> GetSpendingByMerchant
//	~987   getSpendingTrendsHousehold           -> GetSpendingTrends
//	~1133  getAverageMonthlySpendHousehold      -> GetAverageMonthlySpend
//	~1269  getSavingsRateForPeriodHousehold     -> GetSavingsRateForPeriod
//	~1375  getMonthOverMonthSpendingHousehold   -> GetMonthOverMonthSpending
//	~1566  getIncomeSourceBreakdownHousehold    -> GetIncomeSourceBreakdown
//
// Both non-active statuses are covered across the set. 'expired' matters especially:
// AccountRepository.ExpireAccountsByFingerprint writes exactly that status in production
// (the cross-user fingerprint-collision path), so 'expired' — not 'replaced' — is the
// status real users' dropped history actually carries.

// markAccountNonActive flips an accounts row to a non-active status. accounts_valid_status
// admits exactly ('active','replaced','expired'); accounts_replaced_at_consistency
// additionally requires replaced_at alongside status='replaced', so set it for that case.
func markAccountNonActive(t *testing.T, pool *pgxpool.Pool, accountID, status string) {
	t.Helper()
	var err error
	switch status {
	case "replaced":
		_, err = pool.Exec(context.Background(),
			`UPDATE accounts SET status = 'replaced', replaced_at = now() WHERE id = $1`, accountID)
	case "expired":
		// accounts_expired_at_consistency (migration 000155, Issue #1569 Task 6) requires
		// expired_at whenever status = 'expired'.
		_, err = pool.Exec(context.Background(),
			`UPDATE accounts SET status = 'expired', expired_at = now() WHERE id = $1`, accountID)
	default:
		_, err = pool.Exec(context.Background(),
			`UPDATE accounts SET status = $2 WHERE id = $1`, accountID, status)
	}
	require.NoError(t, err, "mark account %s", status)

	// Assert the mutation actually landed — a silently-matched-nothing UPDATE would make
	// every assertion below vacuous (the account would still be 'active').
	var got string
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT status FROM accounts WHERE id = $1`, accountID).Scan(&got))
	require.Equal(t, status, got, "accounts.status must actually be %s for this test to mean anything", status)
}

// nonActiveHouseholdFixture builds a single-member household owning two shared accounts:
// one 'active', one flipped to the given non-active status. Both carry spend/income, so a
// reintroduced `a.status = 'active'` predicate shows up as a missing contribution.
func nonActiveHouseholdFixture(t *testing.T, pool *pgxpool.Pool, status string) (owner, hh string, activeAcct, staleAcct householdTestFixture) {
	t.Helper()
	owner = insertTestUser(t, pool)
	hh = insertTestHousehold(t, pool, owner)
	activeAcct = setupHouseholdMemberAccount(t, pool, owner)
	staleAcct = setupHouseholdMemberAccount(t, pool, owner)

	now := time.Now().UTC()
	insertHouseholdShare(t, pool, hh, activeAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))
	insertHouseholdShare(t, pool, hh, staleAcct.UserAccountID, nil, domain.ShareVisibilityShared, now.AddDate(0, -2, 0))

	markAccountNonActive(t, pool, staleAcct.AccountID, status)
	return owner, hh, activeAcct, staleAcct
}

func TestAnalyticsRepository_GetCashFlowAggregation_Household_IncludesExpiredAccountTransactions(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	// 'expired' is what ExpireAccountsByFingerprint writes in production.
	owner, hh, activeAcct, staleAcct := nonActiveHouseholdFixture(t, pool, "expired")
	now := time.Now().UTC()
	insertHouseholdTransaction(t, pool, owner, activeAcct.AccountID, activeAcct.LinkedItemID, 80, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")
	insertHouseholdTransaction(t, pool, owner, staleAcct.AccountID, staleAcct.LinkedItemID, 20, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")
	insertHouseholdTransaction(t, pool, owner, staleAcct.AccountID, staleAcct.LinkedItemID, -500, now.AddDate(0, 0, -5), "INCOME")

	filters := domain.CashFlowFilters{
		StartDate:      now.AddDate(0, -1, 0),
		EndDate:        now,
		HouseholdScope: &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner}},
	}

	agg, err := repo.GetCashFlowAggregation(ctx, owner, filters)
	require.NoError(t, err)
	assert.InDelta(t, 100, agg.Expenses, 0.01,
		"expired account's $20 must join the active account's $80; 80 means the household cash-flow "+
			"join is filtering accounts.status='active' and dropping post-expiry history")
	assert.InDelta(t, 500, agg.Income, 0.01,
		"income recorded on the now-expired account is immutable history and must still count")
}

func TestAnalyticsRepository_GetCashFlowGrouped_Household_IncludesReplacedAccountTransactions(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner, hh, activeAcct, staleAcct := nonActiveHouseholdFixture(t, pool, "replaced")
	now := time.Now().UTC()
	sameDay := now.AddDate(0, 0, -5)
	insertHouseholdTransaction(t, pool, owner, activeAcct.AccountID, activeAcct.LinkedItemID, 80, sameDay, "FOOD_AND_DRINK")
	insertHouseholdTransaction(t, pool, owner, staleAcct.AccountID, staleAcct.LinkedItemID, 20, sameDay, "FOOD_AND_DRINK")

	filters := domain.CashFlowFilters{
		StartDate:      now.AddDate(0, -1, 0),
		EndDate:        now,
		Grouping:       "daily",
		HouseholdScope: &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner}},
	}

	results, err := repo.GetCashFlowGrouped(ctx, owner, filters)
	require.NoError(t, err)

	var total float64
	for _, r := range results {
		total += r.Expenses
	}
	assert.InDelta(t, 100, total, 0.01,
		"replaced account's $20 must appear in the grouped household series alongside the active $80")
}

func TestAnalyticsRepository_GetSpendingByMerchant_Household_IncludesExpiredAccountTransactions(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner, hh, activeAcct, staleAcct := nonActiveHouseholdFixture(t, pool, "expired")
	now := time.Now().UTC()
	insertHouseholdTransaction(t, pool, owner, activeAcct.AccountID, activeAcct.LinkedItemID, 80, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")
	insertHouseholdTransaction(t, pool, owner, staleAcct.AccountID, staleAcct.LinkedItemID, 20, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")

	filters := domain.SpendingFilters{
		StartDate:      now.AddDate(0, -1, 0),
		EndDate:        now,
		HouseholdScope: &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner}},
	}

	results, err := repo.GetSpendingByMerchant(ctx, owner, filters, 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.InDelta(t, 100, results[0].Total, 0.01,
		"merchant spend on an expired account is real history and must stay in the household total")
	assert.Equal(t, 2, results[0].TransactionCount)
}

func TestAnalyticsRepository_GetSpendingTrends_Household_IncludesReplacedAccountTransactions(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner, hh, activeAcct, staleAcct := nonActiveHouseholdFixture(t, pool, "replaced")
	now := time.Now().UTC()
	insertHouseholdTransaction(t, pool, owner, activeAcct.AccountID, activeAcct.LinkedItemID, 80, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")
	insertHouseholdTransaction(t, pool, owner, staleAcct.AccountID, staleAcct.LinkedItemID, 20, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")

	filters := domain.SpendingFilters{
		StartDate:      now.AddDate(0, -1, 0),
		EndDate:        now,
		HouseholdScope: &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner}},
	}

	results, err := repo.GetSpendingTrends(ctx, owner, filters)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.InDelta(t, 100, results[0].Total, 0.01,
		"a re-link must not put a step-change in the household trend line — pre-relink spend still counts")
}

func TestAnalyticsRepository_GetAverageMonthlySpend_Household_IncludesExpiredAccountTransactions(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner, hh, activeAcct, staleAcct := nonActiveHouseholdFixture(t, pool, "expired")
	now := time.Now().UTC()
	insertHouseholdTransaction(t, pool, owner, activeAcct.AccountID, activeAcct.LinkedItemID, 80, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")
	insertHouseholdTransaction(t, pool, owner, staleAcct.AccountID, staleAcct.LinkedItemID, 20, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")

	filters := domain.SpendingFilters{
		StartDate:      now.AddDate(0, -1, 0),
		EndDate:        now,
		HouseholdScope: &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner}},
	}

	row, err := repo.GetAverageMonthlySpend(ctx, owner, filters)
	require.NoError(t, err)
	assert.InDelta(t, 100, row.Total, 0.01,
		"expired account's $20 must be part of the household average-monthly-spend base")
}

func TestAnalyticsRepository_GetSavingsRateForPeriod_Household_IncludesReplacedAccountTransactions(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner, hh, activeAcct, staleAcct := nonActiveHouseholdFixture(t, pool, "replaced")
	now := time.Now().UTC()
	insertHouseholdTransaction(t, pool, owner, activeAcct.AccountID, activeAcct.LinkedItemID, -1000, now.AddDate(0, 0, -5), "INCOME")
	insertHouseholdTransaction(t, pool, owner, staleAcct.AccountID, staleAcct.LinkedItemID, -500, now.AddDate(0, 0, -5), "INCOME")
	insertHouseholdTransaction(t, pool, owner, staleAcct.AccountID, staleAcct.LinkedItemID, 200, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner}}
	row, err := repo.GetSavingsRateForPeriod(ctx, owner, now.AddDate(0, -1, 0), now, scope)
	require.NoError(t, err)
	assert.InDelta(t, 1500, row.Income, 0.01,
		"paycheques deposited into an account that was later replaced still happened")
	assert.InDelta(t, 200, row.Expenses, 0.01,
		"and so did the spending on it — dropping it would inflate the household savings rate")
}

func TestAnalyticsRepository_GetMonthOverMonthSpending_Household_IncludesExpiredAccountTransactions(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner, hh, activeAcct, staleAcct := nonActiveHouseholdFixture(t, pool, "expired")
	now := time.Now().UTC()
	insertHouseholdTransaction(t, pool, owner, activeAcct.AccountID, activeAcct.LinkedItemID, 80, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")
	insertHouseholdTransaction(t, pool, owner, staleAcct.AccountID, staleAcct.LinkedItemID, 20, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")

	filters := domain.SpendingFilters{
		HouseholdScope: &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner}},
	}

	results, err := repo.GetMonthOverMonthSpending(ctx, owner, now.AddDate(0, -1, 0), "", filters)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.InDelta(t, 100, results[0].Total, 0.01,
		"an expiry mid-comparison must not fabricate a month-over-month drop in household spend")
}

func TestAnalyticsRepository_GetIncomeSourceBreakdown_Household_IncludesReplacedAccountTransactions(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner, hh, activeAcct, staleAcct := nonActiveHouseholdFixture(t, pool, "replaced")
	now := time.Now().UTC()
	insertHouseholdTransaction(t, pool, owner, activeAcct.AccountID, activeAcct.LinkedItemID, -1000, now.AddDate(0, 0, -5), "INCOME")
	insertHouseholdTransaction(t, pool, owner, staleAcct.AccountID, staleAcct.LinkedItemID, -500, now.AddDate(0, 0, -5), "INCOME")

	filters := domain.SpendingFilters{
		StartDate:      now.AddDate(0, -1, 0),
		EndDate:        now,
		HouseholdScope: &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner}},
	}

	results, err := repo.GetIncomeSourceBreakdown(ctx, owner, filters)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.InDelta(t, 1500, results[0].Total, 0.01,
		"income received on a since-replaced account must remain in the household income breakdown")
}
