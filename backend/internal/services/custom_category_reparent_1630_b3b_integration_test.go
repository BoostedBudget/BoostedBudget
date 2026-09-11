//go:build integration

package services

// Issue #1630 Task B3b: the reparent-containment guard (customCategoryService.
// reparentCategoryChecked, née checkReparentDoesNotEscape) used to run its budget-row lock and
// re-read via GetCategoriesByBudgetIDLocked -- a helper that opens and COMMITS its own
// transaction, releasing the row lock before returning. The reparent's custom_categories write
// then persisted afterward, in a SEPARATE transaction on a DIFFERENT repository
// (CustomCategoryRepository.Update). That gap is exactly the shape B3 closed for
// AddCategoryChecked/UpdateCategoryChecked: a caller can lock, read, decide, and release, all
// before the caller's own write lands, so a genuinely concurrent second writer can slip into the
// released window and invalidate what the first writer's decision assumed.
//
// This test reproduces the concrete failure: a reparent (customCategoryService.UpdateCategory,
// moving catA under catP) races a concurrent budget-category match_logic change (
// BudgetRepository.UpdateCategoryChecked, flipping catP's OWN budgeted row to a non-containing
// match_logic) on the SAME budget. Neither operation is individually wrong in isolation -- each
// is correct against whatever state it actually observes -- but the OLD implementation let the
// reparent's check-then-release-then-write structure hand the match_logic change a window in
// which it could not yet see the pending reparent, only for the reparent's unconditional write to
// land anyway afterward. The result: an ESCAPING pair (catA nested under catP, whose match_logic
// no longer contains it) that neither guard ever evaluated together.
//
// The test makes the interleaving deterministic (not a hope-it-races coin flip) the same way
// budget_category_hierarchy_lock_1630_b3_integration_test.go's second test does: hold the
// budget row's FOR UPDATE lock on a raw connection, launch BOTH racers in order, and confirm --
// via polling pg_stat_activity, never a sleep -- that BOTH are genuinely queued behind it before
// releasing. (Postgres's row-lock wait queue is a CHAIN: the second waiter for an
// already-contended tuple waits on the FIRST waiter's transaction, not directly on the original
// holder, so pg_blocking_pids only chases the head of the chain -- see
// b3bCountWaitingOnBudgetRowLock for how this test observes both waiters anyway.) Postgres wakes
// queued waiters in the order they started waiting, so launching the reparent first and
// confirming its own block before launching the match_logic change second pins the reparent to
// win the row lock first once released -- deterministic, not timing-luck.
//
//   - Reparent (checked+written atomically, Issue #1630 B3b): wins the lock first, checks catP
//     fresh (still safe), persists, commits -- releasing the lock only now.
//   - UpdateCategoryChecked (match_logic change): blocked until the reparent's FULL transaction
//     (check AND write) has committed. Once unblocked, its own fresh re-read sees catA ALREADY
//     nested under catP, so ITS OWN guard (domain.EscapedNestedChild, already re-run under lock
//     since Issue #1630 B3) correctly refuses turning catP non-containing out from under a child
//     it does not yet know about at request time, but definitely knows about by the time it is
//     allowed to check.
//
// Mutating reparentCategoryChecked back to check-via-a-self-committing-per-budget-transaction-
// then-write-via-plain-Update (the pre-B3b shape) reopens the window: the match_logic change's
// lock is released as soon as the CHECK's own transaction commits, so it can proceed and commit
// BEFORE the reparent's separate write lands -- from its perspective catA is not yet catP's
// child, so nothing looks wrong, and it succeeds. The reparent's write then lands regardless
// (nothing re-checks it), leaving an escaping pair neither guard evaluated together. Verified by
// hand while writing this test: on an in-process Go call the released-lock-to-next-write gap is
// microseconds, so the natural race needs a small artificial delay between the mutated check's
// commit and its write to reproduce reliably -- with that delay inserted, the "match_logic change
// must be refused" assertion below goes RED (it returns nil instead), exactly because the
// concurrent write already landed by the time the check ran. The fix removes the gap itself
// (one transaction, lock held through the write), which is what makes the delay irrelevant on the
// code as shipped: nothing can run in a window that no longer exists.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/services/... -tags integration -p 1 \
//	  -run TestCustomCategoryReparent_1630_B3b -v

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// b3bAlwaysSucceedsJobQueue is a minimal queue.JobQueue stub: UpdateCategory's resync enqueue is
// now fail-closed (Issue #1630 review fix), so the reparent under test here needs a job queue
// wired even though this test's own point is the row-lock race, not the enqueue itself. Safe for
// concurrent use — Enqueue does no shared-state mutation.
type b3bAlwaysSucceedsJobQueue struct{}

func (b3bAlwaysSucceedsJobQueue) Enqueue(ctx context.Context, job queue.Job, opts ...queue.EnqueueOption) (*queue.JobResult, error) {
	return &queue.JobResult{ID: 1, Kind: job.Kind()}, nil
}

func (b3bAlwaysSucceedsJobQueue) EnqueueTx(ctx context.Context, tx pgx.Tx, job queue.Job, opts ...queue.EnqueueOption) (*queue.JobResult, error) {
	panic("b3bAlwaysSucceedsJobQueue.EnqueueTx unused by this test")
}

func (b3bAlwaysSucceedsJobQueue) EnqueueMany(ctx context.Context, jobs []queue.Job, opts ...queue.EnqueueOption) ([]*queue.JobResult, error) {
	panic("b3bAlwaysSucceedsJobQueue.EnqueueMany unused by this test")
}

func (b3bAlwaysSucceedsJobQueue) EnqueueManyTx(ctx context.Context, tx pgx.Tx, jobs []queue.Job, opts ...queue.EnqueueOption) ([]*queue.JobResult, error) {
	panic("b3bAlwaysSucceedsJobQueue.EnqueueManyTx unused by this test")
}

// b3bWaitUntilBlockedBy polls pg_blocking_pids(pid) from a separate connection until some backend
// is genuinely, currently blocked waiting specifically on blockerPID -- mirroring
// repository.waitUntilBlockedBy (unexported there, so duplicated narrowly here rather than
// exporting a repository-package test helper into production code).
func b3bWaitUntilBlockedBy(t *testing.T, pool *pgxpool.Pool, blockerPID int32, timeout time.Duration) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(timeout)
	for {
		var blocked bool
		err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity
				WHERE $1 = ANY(pg_blocking_pids(pid))
			)
		`, blockerPID).Scan(&blocked)
		require.NoError(t, err)
		if blocked {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for a backend to be genuinely blocked on PID %d", timeout, blockerPID)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// b3bCountWaitingOnBudgetRowLock counts backends currently waiting to acquire the SAME
// "SELECT ... FOR UPDATE" row lock lockBudgetRowInTx takes. Postgres's row-lock wait queue is a
// CHAIN, not a star: a SECOND waiter for an already-contended tuple waits on the FIRST waiter's
// transaction id (wait_event=transactionid), not directly on the original lock holder
// (wait_event=tuple) -- so pg_blocking_pids(secondWaiter) returns the first waiter's PID, not the
// holder's. Counting by query text + wait_event_type='Lock' sidesteps that chain and just asks
// "how many backends are queued for this exact row lock right now", regardless of who each one is
// directly waiting on.
func b3bCountWaitingOnBudgetRowLock(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM pg_stat_activity
		WHERE wait_event_type = 'Lock'
		  AND query ILIKE 'SELECT id FROM budgets WHERE id = $1 AND deleted_at IS NULL FOR UPDATE%'
	`).Scan(&n))
	return n
}

func TestCustomCategoryReparent_1630_B3b_RacesConcurrentMatchLogicChange_OnlyOneMaySucceed(t *testing.T) {
	pool := connectIntegrationDB(t)
	ctx := context.Background()

	userID := uuid.New().String()
	budgetID := uuid.New().String()

	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|1630b3b|"+userID, "1630-b3b-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM budget_categories WHERE budget_id = $1`, budgetID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM budgets WHERE id = $1`, budgetID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM custom_categories WHERE user_id = $1`, userID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = $1`, userID)
	})

	budgetRepo := repository.NewBudgetRepository(pool)
	customCategoryRepo := repository.NewCustomCategoryRepository(pool)

	budget := &domain.Budget{
		ID:       budgetID,
		UserID:   userID,
		Name:     "1630 B3b Reparent Race Budget",
		Period:   domain.BudgetPeriodMonthly,
		Amount:   1000.00,
		IsActive: true,
	}
	budget.StartDate = time.Now()
	require.NoError(t, budgetRepo.Create(ctx, budget), "seed budget")

	// catP: the reparent's TARGET PARENT. Currently 'category_only' (safe/containing) -- the
	// reparent's own check must see this as safe.
	catP := &domain.CustomCategory{UserID: userID, Name: "Parent P"}
	require.NoError(t, customCategoryRepo.Create(ctx, catP), "seed catP")
	bcP := &domain.BudgetCategory{
		BudgetID:         budgetID,
		CategoryKind:     "custom",
		CustomCategoryID: &catP.ID,
		MatchLogic:       domain.MatchLogicCategoryOnly,
		AllocatedAmount:  200.00,
	}
	require.NoError(t, budgetRepo.CreateCategory(ctx, bcP), "seed bcP")

	// catA: currently top-level, will be reparented under catP.
	catA := &domain.CustomCategory{UserID: userID, Name: "Child A"}
	require.NoError(t, customCategoryRepo.Create(ctx, catA), "seed catA")
	bcA := &domain.BudgetCategory{
		BudgetID:         budgetID,
		CategoryKind:     "custom",
		CustomCategoryID: &catA.ID,
		MatchLogic:       domain.MatchLogicCategoryOnly,
		AllocatedAmount:  50.00,
	}
	require.NoError(t, budgetRepo.CreateCategory(ctx, bcA), "seed bcA")

	svc := NewCustomCategoryService(customCategoryRepo)
	svc.SetBudgetCategoryRepository(budgetRepo)
	svc.SetJobQueue(b3bAlwaysSucceedsJobQueue{})

	// Step 1: hold the budget row's FOR UPDATE lock on a raw connection so BOTH racers below are
	// forced to queue behind it in launch order (Postgres grants a released row lock to waiters
	// FIFO), making the interleaving deterministic.
	holder, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer holder.Rollback(ctx) //nolint:errcheck // no-op after the Commit below

	var holderPID int32
	require.NoError(t, holder.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&holderPID))
	var locked string
	require.NoError(t, holder.QueryRow(ctx,
		`SELECT id FROM budgets WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, budgetID).Scan(&locked))

	// Step 2: launch the reparent FIRST and confirm it is blocked on the holder before launching
	// anything else -- this is what pins it to win the lock first once released.
	reparentDone := make(chan error, 1)
	go func() {
		req := &domain.UpdateCustomCategoryRequest{ParentID: &catP.ID}
		_, err := svc.UpdateCategory(ctx, userID, catA.ID, req)
		reparentDone <- err
	}()
	b3bWaitUntilBlockedBy(t, pool, holderPID, 5*time.Second)

	// Step 3: launch the match_logic change SECOND and confirm it is ALSO now queued behind the
	// holder (two backends blocked on the same PID) before releasing anything.
	matchLogicDone := make(chan error, 1)
	go func() {
		_, err := budgetRepo.UpdateCategoryChecked(ctx, budgetID, bcP.ID, func(c *domain.BudgetCategory) error {
			c.MatchLogic = domain.MatchLogicTagOnly // non-containing AND non-contained
			c.Tags = []string{"splurge"}            // 'tag_only' requires non-empty tags
			return c.Validate()
		})
		matchLogicDone <- err
	}()
	select {
	case earlyErr := <-matchLogicDone:
		t.Fatalf("the match_logic change finished before ever queuing behind the held lock (err=%v) -- it should be blocked at this point", earlyErr)
	case <-time.After(2 * time.Second):
	}
	require.Eventually(t, func() bool {
		return b3bCountWaitingOnBudgetRowLock(t, pool) == 2
	}, 5*time.Second, 10*time.Millisecond, "expected BOTH the reparent and the match_logic change to be queued behind the held lock")

	// Step 4: release. FIFO grants the reparent the lock first.
	require.NoError(t, holder.Commit(ctx))

	var reparentErr, matchLogicErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); reparentErr = <-reparentDone }()
	go func() { defer wg.Done(); matchLogicErr = <-matchLogicDone }()
	waitDone := make(chan struct{})
	go func() { wg.Wait(); close(waitDone) }()
	select {
	case <-waitDone:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for both racers to finish")
	}

	// The reparent must succeed: catP was genuinely safe at the moment it (correctly) acquired
	// the lock first.
	assert.NoError(t, reparentErr, "the reparent must succeed -- catP was safe when it won the lock")

	// The match_logic change MUST be refused: by the time it is allowed to check (after the
	// reparent's FULL transaction, check-and-write, has committed), catA is already catP's child,
	// and flipping catP to 'tag_only' would leave that child escaping. This is the assertion that
	// goes RED if reparentCategoryChecked is mutated back to check-then-release-then-write: in
	// that shape, the match_logic change's own lock is released as soon as ITS check-holding
	// transaction (GetCategoriesByBudgetIDLocked) commits -- BEFORE the reparent's separate write
	// has landed -- so it observes catA as NOT YET catP's child, sees nothing wrong, and succeeds.
	require.Error(t, matchLogicErr, "the concurrent match_logic change must be refused once it sees the already-reparented child")
	assert.ErrorIs(t, matchLogicErr, domain.ErrEscapedNestedChild)

	// Belt-and-suspenders: confirm the actual persisted state agrees with the errors returned --
	// an inconsistent state (escaping pair actually committed) is the real-world consequence of
	// the bug this test targets, independent of what any error value claims.
	final, err := budgetRepo.GetCategoriesByBudgetID(ctx, budgetID)
	require.NoError(t, err)
	var gotA, gotP *domain.BudgetCategory
	for i := range final {
		switch final[i].ID {
		case bcA.ID:
			gotA = &final[i]
		case bcP.ID:
			gotP = &final[i]
		}
	}
	require.NotNil(t, gotA)
	require.NotNil(t, gotP)
	assert.Equal(t, domain.MatchLogicCategoryOnly, gotP.MatchLogic,
		"catP's match_logic must remain 'category_only' -- the refused change must not have persisted")
	require.NotNil(t, gotA.CustomParentID)
	assert.Equal(t, catP.ID, *gotA.CustomParentID, "the reparent itself must have persisted")
}
