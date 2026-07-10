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

// TestAnalyticsRepository_GetSpendingByCategory_Household_RevokedShareExcludedImmediately
// covers the member-removal path: RemoveMember/self-leave revokes the member's shares via
// HouseholdShareRepository.RevokeSharesForUser (revoked_at = now()); the household_accounts
// CTE's `revoked_at IS NULL` filter must drop that account from the aggregate immediately,
// with no separate cleanup step required.
func TestAnalyticsRepository_GetSpendingByCategory_Household_RevokedShareExcludedImmediately(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAnalyticsRepository(pool)

	owner, member, hh, ownerAcct, memberAcct := setupTwoMemberSharedHousehold(t, pool)
	now := time.Now().UTC()
	insertHouseholdTransaction(t, pool, owner, ownerAcct.AccountID, ownerAcct.LinkedItemID, 100, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")
	insertHouseholdTransaction(t, pool, member, memberAcct.AccountID, memberAcct.LinkedItemID, 999, now.AddDate(0, 0, -5), "FOOD_AND_DRINK")

	// Simulate member removal / self-leave: RevokeSharesForUser is the real production path.
	shareRepo := repository.NewHouseholdShareRepository(pool)
	require.NoError(t, shareRepo.RevokeSharesForUser(ctx, hh, member))

	filters := domain.SpendingFilters{
		StartDate: now.AddDate(0, -1, 0),
		EndDate:   now,
		HouseholdScope: &domain.HouseholdScope{
			HouseholdID:   hh,
			MemberUserIDs: []string{owner, member}, // still "active" per caller's stale view —
			// the revoked SHARE (not membership) is what must gate inclusion here.
		},
	}

	results, err := repo.GetSpendingByCategory(ctx, owner, filters)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.InDelta(t, 100, results[0].Total, 0.01, "removed member's revoked share must drop out of the aggregate immediately")
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
