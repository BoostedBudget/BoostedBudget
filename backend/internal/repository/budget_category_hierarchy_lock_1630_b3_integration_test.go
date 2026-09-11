//go:build integration

package repository

// Issue #1630 Task B3: AddCategory's (and UpdateCategory's) read-then-check-then-write was a
// genuine TOCTOU: two concurrent requests could each evaluate domain.EscapedNestedChild /
// domain.DuplicateParentIdentity against their own stale, pre-write snapshot, both pass, and both
// persist a combination that VIOLATES the guard when viewed together. unique_budget_category_
// discriminated (migration 000083) backstops the identity half at the DB level, but
// EscapedNestedChild has NO DB backstop — it is a cross-row compatibility rule a UNIQUE index
// cannot express — so the row lock in AddCategoryChecked / UpdateCategoryChecked is the only thing
// standing between two concurrent writers and a silently double-counted budget.
//
// These tests exercise the REAL race: two goroutines on separate pgxpool connections, hitting a
// real Postgres, not two sequential calls dressed up as "concurrent". Before AddCategoryChecked /
// UpdateCategoryChecked existed (i.e. against the old AddCategory/UpdateCategory which read outside
// any transaction and checked only in memory), both assertions below were FALSIFIED: both writers
// observed the other's category as absent, both guard checks passed, and both rows were persisted —
// confirmed by temporarily routing this test at AddCategory/UpdateCategory-equivalent unlocked
// calls during development of this fix.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... \
//	  -run TestBudgetCategoryHierarchyLock_1630_B3 -tags integration -p 1 -count=1 -v

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// seedHierarchyLockBudget inserts a committed (not rolled back) user + budget directly on pool, so
// two goroutines using pool's own connections can race against the SAME visible row — a
// transaction-wrapped seed (this package's usual pattern) would not be visible to a second,
// concurrently-open transaction on a different connection until commit, which would just serialize
// the seed itself, not exercise the race under test.
func seedHierarchyLockBudget(t *testing.T, pool *pgxpool.Pool) (budgetID string) {
	t.Helper()
	ctx := context.Background()

	userID := uuid.New()
	budget := uuid.New()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|1630b3|"+suffix, "1630-b3-"+suffix+"@example.com")
	require.NoError(t, err, "seed user")

	_, err = pool.Exec(ctx, `
		INSERT INTO budgets (id, user_id, name, period, start_date, amount, is_active, series_id)
		VALUES ($1, $2, '1630 B3 Lock Test Budget', 'monthly', NOW()::date, 1000.00, true, $1)
	`, budget, userID)
	require.NoError(t, err, "seed budget")

	t.Cleanup(func() {
		// Categories tombstone-cascade off the budget in production, but this is a hard test
		// fixture cleanup, not a user-facing delete — plain DELETE is correct here.
		_, _ = pool.Exec(context.Background(), `DELETE FROM budget_categories WHERE budget_id = $1`, budget)
		_, _ = pool.Exec(context.Background(), `DELETE FROM budgets WHERE id = $1`, budget)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})

	return budget.String()
}

// countLiveCategories is a cheap post-hoc check that exactly the winner's row persisted — the
// atomicity claim, not just the returned errors.
func countLiveCategories(t *testing.T, pool *pgxpool.Pool, budgetID string) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM budget_categories WHERE budget_id = $1 AND deleted_at IS NULL`, budgetID).Scan(&n))
	return n
}

// TestBudgetCategoryHierarchyLock_1630_B3_ConcurrentEscapingPair_OnlyOneAddSucceeds is the primary
// RED for Task B3: two goroutines call AddCategoryChecked concurrently on separate connections,
// each inserting ONE HALF of an escaping nested pair (a non-containing 'and' PARENT and a
// category_only CHILD nested under it). Neither row is individually invalid, and neither violates
// the guard against an EMPTY budget — the violation exists only once BOTH rows are present
// together. Without the lock, both goroutines would read "no siblings yet", both would pass
// domain.EscapedNestedChild, and both would persist.
func TestBudgetCategoryHierarchyLock_1630_B3_ConcurrentEscapingPair_OnlyOneAddSucceeds(t *testing.T) {
	pool := connectTestDB(t)
	budgetID := seedHierarchyLockBudget(t, pool)
	repo := NewBudgetRepository(pool)
	ctx := context.Background()

	parentPFC := "FOOD_AND_DRINK"
	childPFC := "FOOD_AND_DRINK"
	childDetailed := "FOOD_AND_DRINK_FAST_FOOD"

	parentCandidate := &domain.BudgetCategory{
		BudgetID:        budgetID,
		CategoryKind:    "pfc",
		PFCPrimary:      &parentPFC,
		MatchLogic:      domain.MatchLogicAnd, // NOT a containing shape
		Tags:            []string{"splurge"},
		AllocatedAmount: 300.00,
	}
	childCandidate := &domain.BudgetCategory{
		BudgetID:        budgetID,
		CategoryKind:    "pfc",
		PFCPrimary:      &childPFC,
		PFCDetailed:     &childDetailed,
		MatchLogic:      domain.MatchLogicCategoryOnly,
		AllocatedAmount: 50.00,
	}

	type result struct {
		category *domain.BudgetCategory
		err      error
	}
	results := make(chan result, 2)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		cat, err := repo.AddCategoryChecked(ctx, budgetID, parentCandidate)
		results <- result{cat, err}
	}()
	go func() {
		defer wg.Done()
		cat, err := repo.AddCategoryChecked(ctx, budgetID, childCandidate)
		results <- result{cat, err}
	}()
	wg.Wait()
	close(results)

	var successes, failures int
	for r := range results {
		if r.err == nil {
			successes++
			require.NotNil(t, r.category)
		} else {
			failures++
			assert.ErrorIs(t, r.err, domain.ErrEscapedNestedChild,
				"the losing goroutine of a genuine race must surface a mapped 4xx domain error, "+
					"not a raw serialization/constraint failure: got %v", r.err)
			assert.ErrorIs(t, r.err, domain.ErrInvalidBudget)
		}
	}

	assert.Equal(t, 1, successes, "exactly one of the two racing adds may persist an escaping pair")
	assert.Equal(t, 1, failures, "the other must be refused, not silently accepted")
	assert.Equal(t, 1, countLiveCategories(t, pool, budgetID),
		"only the winner's row may be visible in budget_categories — both persisting would silently double-count spend")
}

// TestBudgetCategoryHierarchyLock_1630_B3_UpdateWaitsForConcurrentAdd_ThenSeesIt proves the
// mechanism directly and deterministically (Issue #1630 B3: "every hierarchy-affecting operation
// needs the same lock protocol, or the guard is serialised on one door and open on the others").
// It is deliberately NOT a bare goroutine race like the test above — a race on this pairing turned
// out to resolve so fast that scheduling alone, not the lock, decided the outcome, which is not a
// reliable falsifier. Instead this test holds a REAL "SELECT ... FOR UPDATE" on the budget row via
// a raw connection (the exact statement AddCategoryChecked / UpdateCategoryChecked themselves take
// first), inserts the escaping child WHILE that lock is held (so it exists but is not yet visible
// to any other transaction), confirms — via waitUntilBlockedBy, not a sleep — that a concurrently
// launched UpdateCategoryChecked genuinely blocks on that same lock, then releases it and checks
// that UpdateCategoryChecked's re-read sees the now-committed child and refuses the reparent.
//
// Falsified against the same mutant as the test above (removing FOR UPDATE from
// lockBudgetRowInTx): without the lock, UpdateCategoryChecked's SELECT does not block at all, so
// waitUntilBlockedBy times out — proving this test depends on the real lock, not on timing luck.
func TestBudgetCategoryHierarchyLock_1630_B3_UpdateWaitsForConcurrentAdd_ThenSeesIt(t *testing.T) {
	pool := connectTestDB(t)
	budgetID := seedHierarchyLockBudget(t, pool)
	repo := NewBudgetRepository(pool)
	ctx := context.Background()

	parentPFC := "FOOD_AND_DRINK"
	childPFC := "FOOD_AND_DRINK"
	childDetailed := "FOOD_AND_DRINK_FAST_FOOD"

	parent := &domain.BudgetCategory{
		BudgetID:        budgetID,
		CategoryKind:    "pfc",
		PFCPrimary:      &parentPFC,
		MatchLogic:      domain.MatchLogicCategoryOnly, // containing, for now
		AllocatedAmount: 300.00,
	}
	created, err := repo.AddCategoryChecked(ctx, budgetID, parent)
	require.NoError(t, err, "seeding the parent row itself must not race anything")
	parentID := created.ID

	// tx1 is the "first mover": it takes the SAME lock AddCategoryChecked/UpdateCategoryChecked
	// take, inserts the escaping child under its own uncommitted transaction, and holds the lock
	// open — simulating being mid-way through a real AddCategoryChecked call.
	tx1, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx1.Rollback(ctx) //nolint:errcheck // no-op after the Commit below

	var lockedID string
	require.NoError(t, tx1.QueryRow(ctx,
		`SELECT id FROM budgets WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, budgetID).Scan(&lockedID))

	childID := uuid.New().String()
	_, err = tx1.Exec(ctx, `
		INSERT INTO budget_categories (id, budget_id, category_kind, pfc_primary, pfc_detailed, allocated_amount, spent_amount, match_logic)
		VALUES ($1, $2, 'pfc', $3, $4, 50.00, 0.00, 'category_only')
	`, childID, budgetID, childPFC, childDetailed)
	require.NoError(t, err, "tx1 inserts the escaping child while holding the budget lock, uncommitted")

	var tx1PID int32
	require.NoError(t, tx1.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&tx1PID))

	// Launch UpdateCategoryChecked concurrently — it must block trying to acquire the same lock.
	escapingLogic := domain.MatchLogicAnd
	updateDone := make(chan error, 1)
	go func() {
		_, err := repo.UpdateCategoryChecked(ctx, budgetID, parentID, func(c *domain.BudgetCategory) error {
			c.MatchLogic = escapingLogic
			c.Tags = []string{"splurge"}
			return c.Validate()
		})
		updateDone <- err
	}()

	waitUntilBlockedBy(t, pool, tx1PID, 5*time.Second)

	// Release the lock — tx1's child row is now committed and visible.
	require.NoError(t, tx1.Commit(ctx))

	var updateErr error
	select {
	case updateErr = <-updateDone:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for UpdateCategoryChecked to return after the lock was released")
	}

	// The whole point: UpdateCategoryChecked's re-read, taken AFTER acquiring the lock (which it
	// could only do once tx1 released it), sees the just-committed child — so the reparent must be
	// refused, not silently accepted against the stale (childless) view it would have had if it
	// had read before waiting.
	require.Error(t, updateErr, "UpdateCategoryChecked must refuse once it sees the live escaping child")
	assert.ErrorIs(t, updateErr, domain.ErrEscapedNestedChild)
	assert.ErrorIs(t, updateErr, domain.ErrInvalidBudget)

	final, err := repo.GetCategoriesByBudgetID(ctx, budgetID)
	require.NoError(t, err)
	for i := range final {
		if final[i].ID == parentID {
			assert.Equal(t, domain.MatchLogicCategoryOnly, final[i].MatchLogic,
				"the refused reparent must not have persisted")
		}
	}
}
