package jobs

// sweep_cursor_byperiod_floor_test.go — Issue #1477 follow-up (AC-3): the persisted sweep cursor's
// concurrency safety rests ENTIRELY on one thing that was written down and never tested.
//
// THE DEPENDENCY, stated exactly. repository.sweepCursorRepository.Save is an unconditional
// `INSERT ... ON CONFLICT (sweep_name) DO UPDATE` — no row lock, no compare-and-set, no version
// column. Two runs of the SAME sweep overlapping in time both Load, both walk, and both Save: the
// last writer wins and the other run's advance is lost silently, which puts the sweep back to
// re-reading rows the lost run already reached. (Lost-advance is the benign direction; it is not
// the direction that skips rows. See the sibling proof of the semantics themselves in
// repository/sweep_cursor_repository_concurrency_integration_test.go.)
//
// The ONLY thing preventing that overlap is River's UniqueOpts.ByPeriod on each sweep's periodic
// job. So the safety property is:
//
//	ByPeriod > the sweep's worst-case wall-clock runtime
//
// and the worst case is bounded by the sweep's own per-run cap: at most `cap` rows, each costing at
// most one per-row budget. This file turns that inequality into a test.
//
// ⚠ WHY A TEST AND NOT A COMMENT. This branch has repeatedly found that a documented dependency is
// an untested one. Both sides of the inequality are read from the REAL production values — the
// ByPeriod out of each sweep's own UniqueOpts(), the cap out of its own constant — so shortening any
// ByPeriod, or raising any cap, recomputes the check and fails here rather than silently narrowing
// the only defence the cursor has.
//
// ⚠ AND IF IT FAILS, THE FIX IS NOT TO RELAX THE BUDGET. The budgets below are per-row time
// allowances, chosen conservatively against what each sweep's loop body actually does. A red result
// means either the ByPeriod must grow or the cap must shrink. Editing perRowBudget down to restore
// green re-creates exactly the untested assumption this file exists to remove.
//
// ⚠ WHAT THIS DOES NOT CLAIM. ByPeriod buckets are FIXED, not rolling — this repo says so itself at
// retention_cleanup_check.go's derivation of maxUsersPerRetentionSweep: two runs landing either side
// of a bucket boundary can execute close together. So this inequality bounds the overlap of runs
// deduplicated INTO THE SAME bucket, and the boundary case remains real and remains documented
// there. It is a floor on the sole existing defence, not a proof of mutual exclusion.

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// sweepConcurrencyBudget is one sweep's side of the inequality. Nothing here is a literal copy of a
// production value: byPeriod and cap are read from production, and only the per-row allowance —
// which production does not express anywhere — is stated here, with its reasoning.
type sweepConcurrencyBudget struct {
	sweep  string
	cursor repository.SweepCursorName
	// unique is the sweep's REAL periodic-job uniqueness options.
	unique *queue.UniqueOpts
	// cap is the sweep's REAL per-run cap constant.
	cap int
	// perRowBudget is the wall-clock time one row of this sweep's loop body is allowed to take
	// before the ByPeriod stops being a safe bound. Deliberately generous.
	perRowBudget time.Duration
	// why records what the loop body does, because the budget is only meaningful with it.
	why string
}

func sweepConcurrencyBudgets() []sweepConcurrencyBudget {
	return []sweepConcurrencyBudget{
		{
			sweep:        "scheduled_deletion_check",
			cursor:       repository.SweepCursorScheduledDeletion,
			unique:       ScheduledDeletionCheckUniqueOpts(),
			cap:          maxUsersPerDeletionSweep,
			perRowBudget: 50 * time.Millisecond,
			why: "the loop body is ONE River enqueue (the deletion itself happens later, in " +
				"AccountDeletionWorker), amortised over pages of " +
				"scheduledDeletionCheckPageSize; 50ms is roughly an order of magnitude above a " +
				"single-row insert on a loaded database",
		},
		{
			sweep:        "retention_cleanup_check.deleteExpiredAccounts",
			cursor:       repository.SweepCursorExpiredRetention,
			unique:       RetentionCleanupCheckUniqueOpts(),
			cap:          maxUsersPerRetentionSweep,
			perRowBudget: 30 * time.Second,
			why: "the loop body IS the deletion: a synchronous ExecuteDeletion per row, which " +
				"acquires a work claim, disconnects Plaid, cancels Paddle and runs " +
				"DeleteUserCascade's DELETE FROM users across ~37 cascading foreign keys — the " +
				"only one of the four whose per-row cost includes external network calls AND a " +
				"large cascade",
		},
		{
			sweep:        "item_cleanup_check",
			cursor:       repository.SweepCursorItemCleanup,
			unique:       ItemCleanupCheckUniqueOpts(),
			cap:          maxItemsPerCleanupSweep,
			perRowBudget: 5 * time.Second,
			why: "the loop body is one MarkForCleanup UPDATE plus one River enqueue; the actual " +
				"Plaid /item/remove happens later, in ItemCleanupWorker",
		},
		{
			sweep:        "plaid_orphan_retry",
			cursor:       repository.SweepCursorOrphanRetry,
			unique:       PlaidOrphanRetryUniqueOpts(),
			cap:          maxOrphanRetriesPerSweep,
			perRowBudget: 30 * time.Second,
			why: "the loop body is a synchronous Plaid /item/remove round trip plus one write-back; " +
				"30s covers a slow call and the client's own retries",
		},
		{
			sweep:        "balance_snapshot_dispatch",
			cursor:       repository.SweepCursorBalanceCapture,
			unique:       BalanceSnapshotDispatchUniqueOpts(),
			cap:          balanceCaptureItemsPerRun,
			perRowBudget: 50 * time.Millisecond,
			why: "the loop body is ONE River enqueue — the Plaid /accounts/get and the snapshot " +
				"writes happen later, in BalanceSnapshotCaptureWorker — amortised over pages of " +
				"balanceCaptureDispatchPageSize; 50ms matches scheduled_deletion_check, whose loop " +
				"body is the same single insert. ⚠ Note the WithScheduledAt stagger does NOT enter " +
				"this budget: it delays when a capture job becomes AVAILABLE, not how long the " +
				"enqueue itself takes, so the dispatch run does not wait for it",
		},
	}
}

// TestSweepByPeriod_ExceedsTheSweepsWorstCaseRuntime is AC-3's falsifiable form of the dependency.
func TestSweepByPeriod_ExceedsTheSweepsWorstCaseRuntime(t *testing.T) {
	for _, b := range sweepConcurrencyBudgets() {
		t.Run(b.sweep, func(t *testing.T) {
			require.NotNil(t, b.unique, "%s must declare UniqueOpts at all", b.sweep)
			require.Positive(t, b.cap, "%s must declare a per-run cap; without one its worst-case "+
				"runtime is unbounded and no ByPeriod can bound it", b.sweep)
			require.Positive(t, b.unique.ByPeriod,
				"%s has NO ByPeriod. That is not a shorter window, it is the ABSENCE of the only "+
					"mechanism preventing two runs of this sweep from overlapping — and "+
					"repository.SweepCursorRepository.Save has no lock, no CAS and no version column "+
					"to fall back on. Either restore a ByPeriod that satisfies the floor below, or "+
					"add a compare-and-set to Save and rewrite this test around it", b.sweep)

			worstCase := time.Duration(b.cap) * b.perRowBudget
			require.Greater(t, b.unique.ByPeriod, worstCase,
				"%s's ByPeriod (%s) does not exceed its worst-case run (%d rows x %s = %s). River's "+
					"ByPeriod uniqueness is the SOLE defence against two runs of this sweep "+
					"overlapping, because %q's row in sweep_cursors is written by an unconditional "+
					"last-writer-wins upsert. Below this floor, two runs interleave their Saves and "+
					"one run's advance is silently lost. Per-row budget rationale: %s. ⚠ The fix is "+
					"to raise ByPeriod or lower the cap — NOT to shrink perRowBudget until this "+
					"passes",
				b.sweep, b.unique.ByPeriod, b.cap, b.perRowBudget, worstCase, b.cursor, b.why)
		})
	}
}

// TestSweepByPeriodFloor_CoversEveryPersistedCursor stops the floor above from being quietly
// narrowed by omission: a sweep dropped from the table would leave its ByPeriod unchecked while
// every remaining case still passed.
func TestSweepByPeriodFloor_CoversEveryPersistedCursor(t *testing.T) {
	covered := map[repository.SweepCursorName]bool{}
	for _, b := range sweepConcurrencyBudgets() {
		covered[b.cursor] = true
	}
	for _, tc := range sweepCursorNameBindings() {
		require.Contains(t, covered, tc.want,
			"sweep %s persists a cursor under %q but has no entry in sweepConcurrencyBudgets, so "+
				"nothing checks that its ByPeriod still exceeds its worst-case runtime",
			tc.sweep, tc.want)
	}
	require.Len(t, covered, len(sweepCursorNameBindings()),
		"sweepConcurrencyBudgets covers a cursor that no worker is bound to")
}
