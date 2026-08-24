//go:build integration

package services

// Issue #1569 (Task 13) — live-Postgres proof that a re-linked savings goal resumes
// recomputation, and that a chain with no live member still freezes.
//
// WHY THIS CANNOT BE A pgxmock TEST. The fix's correctness rests entirely on a SQL predicate
// this package does not own: AccountChainResolver.ResolveActiveID delegates to
// accountRepository.GetByUserAccountAndStatus, whose WHERE clause must pick exactly the
// status='active' row out of a chain that also contains a 'replaced' row sharing the same
// user_account_id. pgxmock asserts the query TEXT and replays canned rows — it would happily
// pass a query that returns the replaced row, or both rows, or none. Only real Postgres, with
// two real accounts rows in one real chain, can show which row comes back.
//
// Run:
//
//	DATABASE_URL="postgres://..." go test -tags integration -p 1 ./internal/services/ \
//	    -run TestSavingsProgressChain_Integration -v

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// savingsChainFixture is one user with a two-generation account chain and a balance-basis
// savings goal still pointing at the OLD (replaced) generation — the exact post-re-link row
// shape the defect produced.
type savingsChainFixture struct {
	pool          *pgxpool.Pool
	userID        string
	userAccountID string
	oldAccountID  string
	newAccountID  string
	goalID        string
	svc           SavingsProgressService
	goalRepo      repository.SavingsGoalRepository
}

func setupSavingsChainFixture(t *testing.T, relinked bool) *savingsChainFixture {
	t.Helper()
	return setupSavingsChainFixtureWithBasis(t, relinked, domain.ProgressBasisBalance, nil)
}

// setupSavingsChainFixtureWithBasis builds the same chain for either balance-derived basis.
// accountPercentage must be non-nil exactly when basis is account_percentage.
func setupSavingsChainFixtureWithBasis(t *testing.T, relinked bool, basis string, accountPercentage *float64) *savingsChainFixture {
	t.Helper()
	pool := setupHouseholdServiceIntegrationDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	profileRepo := repository.NewProfileRepository(pool, enc)
	user := insertIntegrationTestUser(t, profileRepo, "savings-chain-"+suffix+"@example.com", "premium", "active")

	// ⚠ Registered HERE, immediately after the user exists and BEFORE any child row is
	// inserted. Registering it at the end of the fixture (where it used to live) means a
	// require.* failure part-way through — a new NOT NULL column, a check constraint — calls
	// t.FailNow() before Cleanup is ever registered, and every row created so far survives.
	// That is not hypothetical: three user_accounts rows leaked that way during this task and
	// broke migration 000100's #1334 preflight over in internal/database, a package this test
	// does not touch. Deleting rows that do not exist yet is free; not deleting rows that do is
	// a cross-package failure.
	t.Cleanup(func() {
		// context.Background(), not t.Context(): a cancelled context here leaves fixture rows
		// behind that break other packages sharing this database.
		cleanupCtx := context.Background()

		// ⚠ Every statement is checked rather than swallowed with `_, _ =`. This is a safety
		// net, not the fix for the leak described above (that was the registration point) —
		// but it is the reason a future leak surfaces here, in the test that caused it, rather
		// than as an unrelated package's failure two runs later. A silent cleanup in a shared
		// database is not a tidy-up, it is a delayed failure somewhere else.
		type step struct {
			what string
			sql  string
			arg  any
		}
		for _, st := range []step{
			{"savings_goals", `DELETE FROM savings_goals WHERE user_id = $1`, user.ID},
			{"accounts.replaced_by_id", `UPDATE accounts SET replaced_by_id = NULL WHERE user_id = $1`, user.ID},
			{"accounts", `DELETE FROM accounts WHERE user_id = $1`, user.ID},
			{"linked_items", `DELETE FROM linked_items WHERE user_id = $1`, user.ID},
			{"user_accounts", `DELETE FROM user_accounts WHERE user_id = $1`, user.ID},
			{"users", `DELETE FROM users WHERE id = $1`, user.ID},
		} {
			if _, err := pool.Exec(cleanupCtx, st.sql, st.arg); err != nil {
				t.Errorf("fixture cleanup failed at %s (leaks rows into the shared database): %v", st.what, err)
			}
		}
	})

	var userAccountID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO user_accounts (user_id, institution_id) VALUES ($1, $2) RETURNING id`, user.ID, "ins_test_"+suffix).Scan(&userAccountID))

	// Each generation of the chain has its own linked_item, as a real re-link produces.
	insertLinkedItem := func(itemSuffix, status string) string {
		var id string
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO linked_items (user_id, item_id, access_token_encrypted, institution_id, status)
			VALUES ($1, $2, 'enc-not-a-real-token', $3, $4)
			RETURNING id`,
			user.ID, "item-"+itemSuffix, "ins_test_"+suffix, status).Scan(&id))
		return id
	}

	insertAccount := func(linkedItemID, name, plaidID, status string, balance float64) string {
		// accounts_replaced_at_consistency requires replaced_at NOT NULL exactly when
		// status='replaced'.
		var replacedAt *time.Time
		if status == string(domain.AccountStatusReplaced) {
			now := time.Now()
			replacedAt = &now
		}
		var id string
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO accounts (user_id, linked_item_id, user_account_id, plaid_account_id, name,
			                      type, subtype, current_balance, currency_code, status, replaced_at)
			VALUES ($1, $2, $3, $4, $5, 'depository', 'savings', $6, 'USD', $7, $8)
			RETURNING id`,
			user.ID, linkedItemID, userAccountID, plaidID, name, balance, status, replacedAt).Scan(&id))
		return id
	}

	// Generation 1: the account the goal was created against, frozen at disconnect.
	oldItemID := insertLinkedItem("old-"+suffix, "disconnected")
	oldAccountID := insertAccount(oldItemID, "Old Savings", "plaid-old-"+suffix, "replaced", 3500.00)

	newAccountID := ""
	if relinked {
		// Generation 2: the live account created by an ordinary re-link, same chain.
		newItemID := insertLinkedItem("new-"+suffix, "active")
		newAccountID = insertAccount(newItemID, "New Savings", "plaid-new-"+suffix, "active", 4200.00)
		_, err := pool.Exec(ctx, `UPDATE accounts SET replaced_by_id = $1 WHERE id = $2`, newAccountID, oldAccountID)
		require.NoError(t, err)
	}

	var goalID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO savings_goals (user_id, name, target_amount, current_amount, currency_code,
		                           account_id, user_account_id, progress_basis, account_percentage)
		VALUES ($1, 'Emergency Fund', 10000, 1000, 'USD', $2, $3, $4, $5)
		RETURNING id`, user.ID, oldAccountID, userAccountID, basis, accountPercentage).Scan(&goalID))

	accountRepo := repository.NewAccountRepository(pool, enc)
	goalRepo := repository.NewSavingsGoalRepository(pool)
	contribRepo := repository.NewSavingsContributionRepository(pool)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	return &savingsChainFixture{
		pool:          pool,
		userID:        user.ID,
		userAccountID: userAccountID,
		oldAccountID:  oldAccountID,
		newAccountID:  newAccountID,
		goalID:        goalID,
		goalRepo:      goalRepo,
		svc: NewSavingsProgressService(goalRepo, contribRepo, accountRepo,
			NewAccountChainResolver(accountRepo), logger),
	}
}

func (f *savingsChainFixture) storedAmount(t *testing.T) float64 {
	t.Helper()
	var amount float64
	require.NoError(t, f.pool.QueryRow(context.Background(),
		`SELECT current_amount FROM savings_goals WHERE id = $1`, f.goalID).Scan(&amount))
	return amount
}

// TestSavingsProgressChain_Integration_RelinkResumesRecompute is the defect's acceptance test:
// the goal row still names the replaced account, and recompute must nonetheless store the LIVE
// account's 4200 rather than staying frozen at 3500.
func TestSavingsProgressChain_Integration_RelinkResumesRecompute(t *testing.T) {
	f := setupSavingsChainFixture(t, true)
	ctx := context.Background()

	// The stored amount (1000) is deliberately DIFFERENT from both the frozen balance (3500)
	// and the live balance (4200), so "did not move" and "recomputed from the frozen row" are
	// distinguishable outcomes. With all three equal these assertions would prove nothing.
	require.Equal(t, 1000.00, f.storedAmount(t), "precondition: goal starts frozen at its last computed amount")

	require.NoError(t, f.svc.RecomputeGoalByID(ctx, f.userID, f.goalID))

	assert.Equal(t, 4200.00, f.storedAmount(t),
		"a re-linked goal must recompute from the live account in its chain")

	summary, err := f.svc.GetProgressSummary(ctx, f.userID)
	require.NoError(t, err)
	require.Len(t, summary.Goals, 1)
	assert.NotEqual(t, domain.FreshnessBasisAccountDisconnected, summary.Goals[0].Freshness,
		"a re-linked goal is not disconnected")
	require.NotNil(t, summary.Goals[0].LinkedBalance)
	assert.Equal(t, 4200.00, *summary.Goals[0].LinkedBalance)
}

// TestSavingsProgressChain_Integration_NoLiveMemberStillFreezes is the other half: the user
// disconnected and never re-linked, so the chain contains only the retained row. The stored
// amount must not move — neither recomputed from the frozen balance nor dropped to zero — and
// the API must say why.
func TestSavingsProgressChain_Integration_NoLiveMemberStillFreezes(t *testing.T) {
	f := setupSavingsChainFixture(t, false)
	ctx := context.Background()

	// Prove the chain really has no active member at the SQL level, so the assertion below
	// is testing the guard and not a mis-seeded fixture.
	var activeCount int
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM accounts WHERE user_account_id = $1 AND status = 'active'`,
		f.userAccountID).Scan(&activeCount))
	require.Equal(t, 0, activeCount)

	require.NoError(t, f.svc.RecomputeGoalByID(ctx, f.userID, f.goalID))

	assert.Equal(t, 1000.00, f.storedAmount(t),
		"a goal whose chain has no live member stays frozen at its last computed amount — "+
			"not recomputed from the retained row's frozen 3500, and not dropped to zero")

	summary, err := f.svc.GetProgressSummary(ctx, f.userID)
	require.NoError(t, err)
	require.Len(t, summary.Goals, 1)
	assert.Equal(t, domain.FreshnessBasisAccountDisconnected, summary.Goals[0].Freshness)
}

// TestSavingsProgressChain_Integration_NullUserAccountID_DegradesHonestly covers the rows this
// branch's main.go wiring defect left behind: user_account_id IS NULL on both the goal and the
// account. There is no chain to follow, so the goal reads its physical row and freezes — it
// must not crash, and it must not fall back to some other account's balance.
func TestSavingsProgressChain_Integration_NullUserAccountID_DegradesHonestly(t *testing.T) {
	f := setupSavingsChainFixture(t, true)
	ctx := context.Background()

	_, err := f.pool.Exec(ctx, `UPDATE savings_goals SET user_account_id = NULL WHERE id = $1`, f.goalID)
	require.NoError(t, err)

	require.NoError(t, f.svc.RecomputeGoalByID(ctx, f.userID, f.goalID))

	assert.Equal(t, 1000.00, f.storedAmount(t),
		"with no chain reference the goal reads its own replaced row and freezes there — "+
			"it must not adopt that row's frozen 3500, nor the live sibling's 4200")
}

// TestSavingsProgressChain_Integration_AccountPercentage_NoLiveMemberStillFreezes is the
// account_percentage twin of the balance freeze test. Round 1 covered only `balance`, and a
// mutation run proved the account_percentage freeze guard could be deleted with the entire
// unit AND integration suite still green.
func TestSavingsProgressChain_Integration_AccountPercentage_NoLiveMemberStillFreezes(t *testing.T) {
	pct := 0.25
	f := setupSavingsChainFixtureWithBasis(t, false, domain.ProgressBasisAccountPercentage, &pct)
	ctx := context.Background()

	require.NoError(t, f.svc.RecomputeGoalByID(ctx, f.userID, f.goalID))

	// The retained row holds 3500; recomputing from it would store 3500 * 0.25 = 875.
	assert.Equal(t, 1000.00, f.storedAmount(t),
		"a percent-of-balance goal with no live member stays frozen at 1000, not 875")

	summary, err := f.svc.GetProgressSummary(ctx, f.userID)
	require.NoError(t, err)
	require.Len(t, summary.Goals, 1)
	assert.Equal(t, domain.FreshnessBasisAccountDisconnected, summary.Goals[0].Freshness)
}

// TestSavingsProgressChain_Integration_AccountPercentage_RelinkResumesRecompute is the other
// half for account_percentage: the live row holds 4200, so 4200 * 0.25 = 1050.
func TestSavingsProgressChain_Integration_AccountPercentage_RelinkResumesRecompute(t *testing.T) {
	pct := 0.25
	f := setupSavingsChainFixtureWithBasis(t, true, domain.ProgressBasisAccountPercentage, &pct)
	ctx := context.Background()

	require.NoError(t, f.svc.RecomputeGoalByID(ctx, f.userID, f.goalID))

	assert.Equal(t, 1050.00, f.storedAmount(t),
		"a re-linked percent-of-balance goal recomputes from the live 4200, not the frozen 3500")
}
