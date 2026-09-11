//go:build integration

package repository

// Issue #1630 review fix (HIGH): AuditBudgetHierarchy must not clobber a concurrent, correct
// spent_amount write that lands while the audit is running. Before this fix, the audit read
// categories and recomputed spend BEFORE opening its own write transaction — no lock or version
// check tied the later UPDATE to that snapshot, so a concurrent BudgetRepository.
// UpdateCategorySpentAmount call (the same primitive both Task B1's resync job and
// BudgetSyncService.RecalculateBudget use) landing in that window could commit a NEWER correct
// value that the audit would then silently overwrite with its own stale one — and mark the budget
// permanently examined, so the clobber would never self-heal.
//
// This test exercises the REAL race on a real Postgres connection, not two sequential calls
// dressed up as concurrent: a stub BudgetHierarchySpentCalculator, invoked from INSIDE
// AuditBudgetHierarchy's own transaction (i.e. only after it has locked the category row —
// lockLiveCategoriesForAuditInTx), launches a concurrent UpdateCategorySpentAmount call on a
// SEPARATE connection and proves — via a genuine lock-wait check against that connection's own
// backend pid, not a sleep — that the concurrent write is blocked before letting the audit's
// recompute return. Only after the audit commits (and releases the lock) does the concurrent write
// proceed. The final persisted spent_amount must be the CONCURRENT writer's value, not the audit's
// stale one — proving the audit did not clobber it. Even if the lock-wait check somehow raced past
// its own poll window, the final-value assertion below independently falsifies the old (unlocked)
// behavior: without the fix, the concurrent write lands first (unblocked), then the audit's own
// later UPDATE — using its already-computed, now-stale value — overwrites it.
//
// Run with:
//
//	DATABASE_URL=postgres://... CACHE_REDIS_TEST_URL=... REDIS_TEST_URL=... \
//	  go test ./internal/repository/... \
//	  -run TestAuditBudgetHierarchy_1630Review_DoesNotClobberConcurrentSpentAmountWrite \
//	  -tags integration -p 1 -count=1 -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// waitUntilPIDBlockedOnLock polls pg_stat_activity for pid's OWN wait_event_type, from a SEPARATE
// pool connection, until it reports 'Lock' — i.e. until pid is genuinely, currently blocked
// acquiring a row lock some other backend holds. Unlike waitUntilBlockedBy (admin_user_repository_
// remove_tier_override_integration_test.go), which asks "is anyone blocked BY this known blocker",
// this asks "is this SPECIFIC backend blocked" — what's needed here, since the blocking backend is
// AuditBudgetHierarchy's own transaction, whose pid this test never learns (it never touches the
// connection AuditBudgetHierarchy checked out of the pool), but the WAITER's pid is one this test
// controls directly.
func waitUntilPIDBlockedOnLock(t *testing.T, pool *pgxpool.Pool, pid int32, timeout time.Duration) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(timeout)
	for {
		var blocked bool
		err := pool.QueryRow(ctx, `
			SELECT wait_event_type = 'Lock' FROM pg_stat_activity WHERE pid = $1
		`, pid).Scan(&blocked)
		require.NoError(t, err)
		if blocked {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for pid %d to be blocked on a lock — either the "+
				"concurrent write raced through unlocked (the defect this test exists to catch) or "+
				"it already finished before this could observe it waiting; the final-value assertion "+
				"below still falsifies the defect independently of this timing check", timeout, pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// raceInjectingSpentCalculator is a BudgetHierarchySpentCalculator stub that, on its call for the
// targeted category, injects the concurrent write mid-audit. AuditBudgetHierarchy only reaches
// this call AFTER it has locked the budget_categories row (Issue #1630 review fix), so triggering
// the race from inside it exercises exactly the window the fix closes.
type raceInjectingSpentCalculator struct {
	t                   *testing.T
	pool                *pgxpool.Pool
	categoryID          string
	concurrentValue     float64
	auditValue          float64
	concurrentWriteDone chan error
	injected            bool
}

func (r *raceInjectingSpentCalculator) CalculateCategorySpent(ctx context.Context, userID string, category *domain.BudgetCategory, startDate, endDate time.Time) (float64, error) {
	if !r.injected && category.ID == r.categoryID {
		r.injected = true

		conn, err := r.pool.Acquire(ctx)
		require.NoError(r.t, err)

		var pid int32
		require.NoError(r.t, conn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid))

		concurrentRepo := NewBudgetRepository(conn)
		go func() {
			defer conn.Release()
			r.concurrentWriteDone <- concurrentRepo.UpdateCategorySpentAmount(context.Background(), r.categoryID, r.concurrentValue)
		}()

		waitUntilPIDBlockedOnLock(r.t, r.pool, pid, 5*time.Second)
	}
	return r.auditValue, nil
}

func TestAuditBudgetHierarchy_1630Review_DoesNotClobberConcurrentSpentAmountWrite(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	userID := uuid.New()
	budgetID := uuid.New()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|1630review|"+suffix, "1630-review-"+suffix+"@example.com")
	require.NoError(t, err, "seed user")

	_, err = pool.Exec(ctx, `
		INSERT INTO budgets (id, user_id, name, period, start_date, amount, is_active, series_id)
		VALUES ($1, $2, '1630 Review Concurrency Test Budget', 'monthly', NOW()::date, 1000.00, true, $1)
	`, budgetID, userID)
	require.NoError(t, err, "seed budget")

	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM budget_hierarchy_audits WHERE budget_id = $1`, budgetID)
		_, _ = pool.Exec(bg, `DELETE FROM budget_categories WHERE budget_id = $1`, budgetID)
		_, _ = pool.Exec(bg, `DELETE FROM budgets WHERE id = $1`, budgetID)
		_, _ = pool.Exec(bg, `DELETE FROM users WHERE id = $1`, userID)
	})

	repo := NewBudgetRepository(pool)
	pfcPrimary := "FOOD_AND_DRINK"
	category := &domain.BudgetCategory{
		BudgetID:        budgetID.String(),
		CategoryKind:    "pfc",
		PFCPrimary:      &pfcPrimary,
		MatchLogic:      domain.MatchLogicCategoryOnly,
		AllocatedAmount: 500,
		SpentAmount:     1.00, // deliberately wrong, so the audit's own recompute would "repair" it
	}
	require.NoError(t, repo.CreateCategory(ctx, category), "seed category")

	const concurrentValue = 275.50 // the value a concurrent, real transaction-sync would compute
	const auditValue = 999.99      // the audit's own recompute — stale the instant the concurrent write lands

	calc := &raceInjectingSpentCalculator{
		t:                   t,
		pool:                pool,
		categoryID:          category.ID,
		concurrentValue:     concurrentValue,
		auditValue:          auditValue,
		concurrentWriteDone: make(chan error, 1),
	}

	// Categories is populated the way BudgetRepository.GetByID's caller (the periodic worker) would
	// hand it in — the OLD (pre-fix) implementation read straight from this field; the fixed
	// implementation ignores it and re-reads inside its own locked transaction instead. Populating
	// it keeps this test a fair comparison against both.
	budget := &domain.Budget{ID: budgetID.String(), UserID: userID.String(), Categories: []domain.BudgetCategory{*category}}
	periodStart := time.Now().AddDate(0, 0, -1)
	periodEnd := time.Now().AddDate(0, 0, 1)

	verdict, err := repo.AuditBudgetHierarchy(ctx, budget, periodStart, periodEnd, calc)
	require.NoError(t, err)
	assert.True(t, calc.injected, "the race must actually have been injected — otherwise this test proves nothing")

	select {
	case concurrentErr := <-calc.concurrentWriteDone:
		require.NoError(t, concurrentErr, "the concurrent UpdateCategorySpentAmount call itself must succeed")
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the concurrent write to complete after the audit released its lock")
	}

	final, err := repo.GetCategoryByIDAndUser(ctx, category.ID, userID.String())
	require.NoError(t, err)
	assert.InDelta(t, concurrentValue, final.SpentAmount, 0.001,
		fmt.Sprintf("the concurrent writer's value must win — the audit must not have clobbered it "+
			"with its own stale recomputation. verdict was: %+v", verdict))
}
