package jobs

// sweep_cursor_name_binding_test.go — Issue #1477 follow-up: the ONE thing keeping the five
// persisted sweep cursors apart is a string, and until this file nothing tested it.
//
// THE HAZARD, stated exactly. All five sweeps share one position type (repository.SweepPosition),
// one repository, and one table (sweep_cursors). What makes scheduled_deletion's watermark
// scheduled_deletion's is the repository.SweepCursorName handed to newSweepResume — and every one
// of the five constants has the same TYPE, so writing
//
//	resume := newSweepResume(ctx, w.sweepCursors, repository.SweepCursorItemCleanup, ...)
//
// at scheduled_deletion_check.go's call site COMPILES CLEANLY. It would feed the item-cleanup
// watermark — a (last_error_at, last_synced_at) pair from a different table — into the deletion
// sweep's keyset predicate, and start the sweep that erases users at an arbitrary point in its
// candidate set. The call sites were correct by convention only.
//
// WHAT THIS FILE ASSERTS, and why it is not a "the constants exist" test. Each worker is RUN,
// through the same entry point production uses, with a store that records every name it is asked
// about. The assertion is that the run touched EXACTLY ONE name and that it was that sweep's own.
// Swap any two of the call sites and two cases here fail; point two sweeps at one name and two
// fail; add a new sweep without a case and TestSweepCursorNames_EveryDeclaredNameHasBoundWorker
// fails (the name is spelled exactly as the func below declares it, so it works as a `-run` pattern;
// it previously read "…HasABoundWorker", which matched nothing).
// The name never appears as a literal in the arrange half of a case — it is read back from
// the store the worker used.
//
// Deliberately a UNIT test with no database: the binding is a property of the wiring, and the page
// repositories are stubbed to return an empty first page so each Work() reaches Finish immediately
// and never touches a row.

import (
	"context"
	"io"
	"log/slog"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// recordingSweepCursorStore is a repository.SweepCursorRepository that records the name of every
// call, on every method. Recording Load AND Clear AND Save (rather than only Load) is what makes a
// half-swap detectable: a worker that loads under one name and clears under another is just as
// broken as one that uses the wrong name throughout.
type recordingSweepCursorStore struct {
	mu    sync.Mutex
	names map[repository.SweepCursorName]bool
}

func newRecordingSweepCursorStore() *recordingSweepCursorStore {
	return &recordingSweepCursorStore{names: map[repository.SweepCursorName]bool{}}
}

func (s *recordingSweepCursorStore) record(name repository.SweepCursorName) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.names[name] = true
}

func (s *recordingSweepCursorStore) Load(_ context.Context, name repository.SweepCursorName) (*repository.SweepPosition, error) {
	s.record(name)
	// nil means "no stored position": the run starts from the beginning, sees an empty first page,
	// and ends by CLEARING — which is the second call this store gets to check the name of.
	return nil, nil
}

func (s *recordingSweepCursorStore) Save(_ context.Context, name repository.SweepCursorName, _ repository.SweepPosition) error {
	s.record(name)
	return nil
}

func (s *recordingSweepCursorStore) Clear(_ context.Context, name repository.SweepCursorName) error {
	s.record(name)
	return nil
}

// observed is the sorted set of distinct names this store was asked about.
func (s *recordingSweepCursorStore) observed() []repository.SweepCursorName {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]repository.SweepCursorName, 0, len(s.names))
	for name := range s.names {
		out = append(out, name)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// --- the stub data sources: an empty first page for each sweep, and nothing else ---------------

type emptyPageDeletionProfileRepo struct{ ProfileRepository }

func (emptyPageDeletionProfileRepo) GetUsersScheduledForDeletionPage(context.Context, *repository.DeletionSweepCursor, int) ([]*domain.User, error) {
	return nil, nil
}

type emptyPageRetentionProfileRepo struct{ RetentionProfileRepository }

func (emptyPageRetentionProfileRepo) GetUsersWithExpiredRetentionPage(context.Context, *repository.RetentionSweepCursor, int) ([]*domain.User, error) {
	return nil, nil
}

// neverCalledDeletionService satisfies deleteExpiredAccounts's nil guard — an unset deletion
// service makes that step return BEFORE it builds its resume, so without this stub the retention
// case would observe no name at all and pass vacuously for the wrong reason. It is never invoked:
// the page above is empty.
type neverCalledDeletionService struct{ RetentionDeletionService }

type emptyPageCleanupItemRepo struct{ LinkedItemCleanupRepository }

func (emptyPageCleanupItemRepo) GetItemsForCleanupPage(context.Context, int, int, *repository.ItemCleanupSweepCursor, int) ([]domain.LinkedItem, error) {
	return nil, nil
}

// GetItemsPendingCleanup backs the stranded-cleanup re-enqueue pass ItemCleanupCheckWorker.Work runs
// alongside the swept page. Empty, for the same reason as the page above: this test is about which
// cursor row the run touches, not about what it would do with a row.
func (emptyPageCleanupItemRepo) GetItemsPendingCleanup(context.Context) ([]domain.LinkedItem, error) {
	return nil, nil
}

// emptyPageOrphanRepo backs the REAL services.PlaidOrphanRetryService, so the orphan-retry case
// exercises the genuine orphanRetryResume adapter (the one sweep whose paging loop lives in the
// service layer) rather than a hand-rolled stand-in for it.
type emptyPageOrphanRepo struct{}

func (emptyPageOrphanRepo) GetRetryablePage(context.Context, time.Time, *repository.OrphanRetryCursor, int) ([]domain.PlaidOrphanedItem, error) {
	return nil, nil
}
func (emptyPageOrphanRepo) MarkResolved(context.Context, string, time.Time) (bool, error) { return false, nil }
func (emptyPageOrphanRepo) RecordRetryFailure(context.Context, string, string, time.Time) error {
	return nil
}

type neverCalledOrphanRemover struct{}

func (neverCalledOrphanRemover) RemoveItem(context.Context, string) error { return nil }

// emptyPageBalanceCaptureItemRepo backs the balance-snapshot dispatcher. Empty first page, for the
// same reason as its siblings: this test is about which cursor row the run touches, not about what
// it would dispatch.
type emptyPageBalanceCaptureItemRepo struct{}

func (emptyPageBalanceCaptureItemRepo) GetItemsForBalanceCapturePage(context.Context, *repository.BalanceCaptureSweepCursor, int) ([]domain.BalanceCaptureTarget, error) {
	return nil, nil
}

func bindingTestLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// --- the bindings ----------------------------------------------------------------------------

// sweepCursorNameBindings is the mapping under test: one entry per sweep, each running that sweep's
// real entry point against a recording store.
func sweepCursorNameBindings() []struct {
	sweep string
	want  repository.SweepCursorName
	run   func(t *testing.T, store repository.SweepCursorRepository)
} {
	return []struct {
		sweep string
		want  repository.SweepCursorName
		run   func(t *testing.T, store repository.SweepCursorRepository)
	}{
		{
			sweep: "ScheduledDeletionCheckWorker",
			want:  repository.SweepCursorScheduledDeletion,
			run: func(t *testing.T, store repository.SweepCursorRepository) {
				t.Helper()
				w := NewScheduledDeletionCheckWorker(ScheduledDeletionCheckWorkerDeps{
					ProfileRepo:  emptyPageDeletionProfileRepo{},
					Logger:       bindingTestLogger(),
					SweepCursors: store,
				})
				require.NoError(t, w.Work(context.Background(),
					newTestScheduledDeletionCheckJob(ScheduledDeletionCheckArgs{TriggerSource: "cursor-name-binding"})))
			},
		},
		{
			sweep: "RetentionCleanupCheckWorker.deleteExpiredAccounts",
			want:  repository.SweepCursorExpiredRetention,
			run: func(t *testing.T, store repository.SweepCursorRepository) {
				t.Helper()
				w := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
					ProfileRepo:     emptyPageRetentionProfileRepo{},
					DeletionService: neverCalledDeletionService{},
					Logger:          bindingTestLogger(),
					SweepCursors:    store,
				})
				// deleteExpiredAccounts, not Work: this sweep is one step of a multi-step job, and
				// this is the step that owns the cursor (retention_cleanup_check.go's resume).
				require.NoError(t, w.deleteExpiredAccounts(context.Background(), 1477))
			},
		},
		{
			sweep: "ItemCleanupCheckWorker",
			want:  repository.SweepCursorItemCleanup,
			run: func(t *testing.T, store repository.SweepCursorRepository) {
				t.Helper()
				w := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
					LinkedItemRepo: emptyPageCleanupItemRepo{},
					Logger:         bindingTestLogger(),
					SweepCursors:   store,
				})
				require.NoError(t, w.Work(context.Background(),
					newTestItemCleanupCheckJob(ItemCleanupCheckArgs{TriggerSource: "cursor-name-binding"})))
			},
		},
		{
			sweep: "PlaidOrphanRetryWorker",
			want:  repository.SweepCursorOrphanRetry,
			run: func(t *testing.T, store repository.SweepCursorRepository) {
				t.Helper()
				svc := services.NewPlaidOrphanRetryService(emptyPageOrphanRepo{}, neverCalledOrphanRemover{})
				svc.SetLogger(bindingTestLogger())
				w := NewPlaidOrphanRetryWorker(PlaidOrphanRetryWorkerDeps{
					Service:      svc,
					Logger:       bindingTestLogger(),
					SweepCursors: store,
				})
				require.NoError(t, w.Work(context.Background(), &river.Job[PlaidOrphanRetryArgs]{
					JobRow: &rivertype.JobRow{ID: 1477, Attempt: 1, Kind: PlaidOrphanRetryArgs{}.Kind(), Queue: "low"},
					Args:   PlaidOrphanRetryArgs{TriggerSource: "cursor-name-binding"},
				}))
			},
		},
		{
			sweep: "BalanceSnapshotDispatchWorker",
			want:  repository.SweepCursorBalanceCapture,
			run: func(t *testing.T, store repository.SweepCursorRepository) {
				t.Helper()
				w := NewBalanceSnapshotDispatchWorker(BalanceSnapshotDispatchWorkerDeps{
					ItemRepo:     emptyPageBalanceCaptureItemRepo{},
					JobQueue:     &captureRecordingQueue{},
					Logger:       bindingTestLogger(),
					SweepCursors: store,
				})
				require.NoError(t, w.Work(context.Background(), captureDispatchJob()))
			},
		},
	}
}

// TestSweepCursorName_EachWorkerTouchesOnlyItsOwnCursorRow is the swap detector.
func TestSweepCursorName_EachWorkerTouchesOnlyItsOwnCursorRow(t *testing.T) {
	for _, tc := range sweepCursorNameBindings() {
		t.Run(tc.sweep, func(t *testing.T) {
			store := newRecordingSweepCursorStore()
			tc.run(t, store)

			// EXACTLY ONE name, and it is this sweep's. Not "contains" — a worker that touched two
			// rows would be reading one sweep's watermark into another's predicate, which is the
			// whole failure this test exists for.
			require.Equal(t, []repository.SweepCursorName{tc.want}, store.observed(),
				"%s must persist its resume position under %q and under NO other name. All five sweeps "+
					"share one SweepPosition type and one sweep_cursors table, so passing a sibling's "+
					"SweepCursorName at this worker's newSweepResume call site compiles cleanly and would "+
					"feed a foreign watermark into this sweep's keyset predicate — starting a sweep that "+
					"hard-deletes users, or removes Plaid Items, at an arbitrary point in its candidate set",
				tc.sweep, tc.want)
		})
	}
}

// TestSweepCursorNames_EveryDeclaredNameHasBoundWorker closes the enumeration in both directions:
// no declared cursor name is left without a worker bound to it, and no two workers share one.
//
// The list below is the complete set of repository.SweepCursorName constants. A new sweep that
// adds a constant without adding a binding above fails here rather than shipping unbound.
func TestSweepCursorNames_EveryDeclaredNameHasBoundWorker(t *testing.T) {
	declared := []repository.SweepCursorName{
		repository.SweepCursorScheduledDeletion,
		repository.SweepCursorExpiredRetention,
		repository.SweepCursorItemCleanup,
		repository.SweepCursorOrphanRetry,
		repository.SweepCursorBalanceCapture,
	}

	bound := map[repository.SweepCursorName]string{}
	for _, tc := range sweepCursorNameBindings() {
		if other, dup := bound[tc.want]; dup {
			t.Fatalf("cursor name %q is claimed by both %s and %s; two sweeps sharing one "+
				"sweep_cursors row would overwrite each other's watermark on every run",
				tc.want, other, tc.sweep)
		}
		bound[tc.want] = tc.sweep
	}

	for _, name := range declared {
		require.Contains(t, bound, name,
			"cursor name %q is declared in repository.SweepCursorName but no worker in "+
				"sweepCursorNameBindings is bound to it; either the sweep is unwired (it silently "+
				"restarts from the beginning every run, the pre-#1477 starvation) or this test was "+
				"not updated with it", name)
	}
	require.Len(t, bound, len(declared),
		"a worker is bound to a cursor name that repository.SweepCursorName does not declare")
}
