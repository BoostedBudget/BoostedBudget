package main

// Issue #1448: every one of the 14 periodic River jobs registered by main.go must carry
// (1) a real dedup guarantee (queue-level UniqueOpts, or — for BudgetPeriodCloseCheck — its
// own documented DB-level exactly-once marker), (2) RunOnStart: true, and (3) a non-empty,
// stable PeriodicJobOpts.ID so River restores nextRunAt across restarts/leader elections
// instead of recomputing now+interval every time (river@v0.38.0
// internal/maintenance/periodic_job_enqueuer.go:409-426).
//
// Enforcing this against a real *river.PeriodicJob is not possible from this package: its
// constructorFunc/opts/scheduleFunc fields are unexported (package river only), so nothing
// outside river can inspect what a *river.PeriodicJob actually carries. buildPeriodicJobSpecs
// (periodic_jobs.go) is the extraction that makes this test possible: it returns
// []periodicJobSpec — a plain, fully-inspectable intermediate representation — BEFORE the
// final conversion to river's opaque type. This is what turns the invariant into something
// this test can actually check, rather than a text-scan of main.go (see
// issue1371_wiring_drift_test.go's history for the failure mode a scan produces: a hardcoded
// end-marker string that silently breaks the moment the very thing being fixed here changes).
import (
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/queue/jobs"
)

// allNonNilPeriodicJobsDeps returns a periodicJobsDeps with every worker field populated by a
// real (zero-configured) worker instance, so buildPeriodicJobSpecs produces the full set of
// 14 specs — one per periodic job main.go registers when every dependency is present.
func allNonNilPeriodicJobsDeps() periodicJobsDeps {
	return periodicJobsDeps{
		scheduledDeletionCheckWorker:        jobs.NewScheduledDeletionCheckWorker(jobs.ScheduledDeletionCheckWorkerDeps{}),
		itemCleanupCheckWorker:              jobs.NewItemCleanupCheckWorker(jobs.ItemCleanupCheckWorkerDeps{}),
		trialExpirationCheckWorker:          jobs.NewTrialExpirationCheckWorker(jobs.TrialExpirationCheckWorkerDeps{}),
		pastDueCheckWorker:                  jobs.NewPastDueCheckWorker(jobs.PastDueCheckWorkerDeps{}),
		paddleReconciliationWorker:          jobs.NewPaddleReconciliationWorker(jobs.PaddleReconciliationWorkerDeps{}),
		retentionCleanupCheckWorker:         jobs.NewRetentionCleanupCheckWorker(jobs.RetentionCleanupCheckWorkerDeps{}),
		manualTierOverrideExpiryCheckWorker: jobs.NewManualTierOverrideExpiryCheckWorker(jobs.ManualTierOverrideExpiryCheckWorkerDeps{}),
		householdMemberCleanupCheckWorker:   jobs.NewHouseholdMemberCleanupCheckWorker(jobs.HouseholdMemberCleanupCheckWorkerDeps{}),
		householdOverCapacityCheckWorker:    jobs.NewHouseholdOverCapacityCheckWorker(jobs.HouseholdOverCapacityCheckWorkerDeps{}),
		dlqCleanupCheckWorker:               jobs.NewDLQCleanupCheckWorker(jobs.DLQCleanupCheckWorkerDeps{}),
		budgetPeriodCloseCheckWorker:        jobs.NewBudgetPeriodCloseCheckWorker(jobs.BudgetPeriodCloseCheckWorkerDeps{}),
		savingsStatsWorker:                  jobs.NewSavingsStatsWorker(jobs.SavingsStatsWorkerDeps{}),
		portfolioSnapshotWorker:             jobs.NewPortfolioSnapshotWorker(jobs.PortfolioSnapshotWorkerDeps{}),
	}
}

// TestBuildPeriodicJobSpecs_Count proves every nil-guarded worker (plus the always-on
// idempotency cleanup job) produces exactly one spec when non-nil — 13 nil-guarded workers +
// 1 unconditional job = 14, matching the ticket's verified count.
func TestBuildPeriodicJobSpecs_Count(t *testing.T) {
	specs := buildPeriodicJobSpecs(allNonNilPeriodicJobsDeps())

	if len(specs) != 14 {
		t.Fatalf("expected 14 periodic job specs when every worker is non-nil, got %d", len(specs))
	}
}

// TestBuildPeriodicJobSpecs_NilWorkerOmitsItsSpec proves the nil-guard still works after the
// extraction — a nil worker must not produce a spec (matches main.go's pre-extraction
// behavior of skipping registration entirely for an unconfigured job).
func TestBuildPeriodicJobSpecs_NilWorkerOmitsItsSpec(t *testing.T) {
	deps := allNonNilPeriodicJobsDeps()
	deps.itemCleanupCheckWorker = nil

	specs := buildPeriodicJobSpecs(deps)

	if len(specs) != 13 {
		t.Fatalf("expected 13 specs with itemCleanupCheckWorker nil, got %d", len(specs))
	}
	for _, s := range specs {
		if s.name == "item cleanup check" {
			t.Fatalf("item cleanup check spec must be omitted when its worker is nil")
		}
	}
}

// TestBuildPeriodicJobSpecs_AllHaveRunOnStartDedupAndID is the central Issue #1448 invariant
// test: every spec this function returns — for every job, not a hardcoded subset — must have
// RunOnStart: true, a non-empty PeriodicJobOpts.ID, and a real dedup guarantee (non-zero
// UniqueOpts on the InsertOpts the spec's build() constructor returns). Unlike
// issue972_wiring_drift_test.go's "river.UniqueOpts{" substring guard (which passes as long
// as ANY single job in the whole file carries UniqueOpts, and would keep passing even if one
// specific job silently lost its own), this iterates every returned spec individually — a
// regression in any ONE job's wiring fails this test on that job specifically.
func TestBuildPeriodicJobSpecs_AllHaveRunOnStartDedupAndID(t *testing.T) {
	specs := buildPeriodicJobSpecs(allNonNilPeriodicJobsDeps())

	if len(specs) == 0 {
		t.Fatal("buildPeriodicJobSpecs returned no specs — test setup bug, not a real result")
	}

	for _, s := range specs {
		s := s
		t.Run(s.name, func(t *testing.T) {
			if s.opts == nil {
				t.Fatalf("%s: PeriodicJobOpts must not be nil", s.name)
			}
			if !s.opts.RunOnStart {
				t.Errorf("%s: RunOnStart must be true — otherwise this job can be enqueued zero "+
					"times ever on a deploy cadence faster than its interval (Issue #1448)", s.name)
			}
			if s.opts.ID == "" {
				t.Errorf("%s: PeriodicJobOpts.ID must be non-empty — without it, River recomputes "+
					"nextRunAt as now+interval on every restart/leader election instead of "+
					"restoring persisted state (Issue #1448)", s.name)
			}

			if s.build == nil {
				t.Fatalf("%s: build constructor must not be nil", s.name)
			}
			args, insertOpts := s.build()
			if args == nil {
				t.Fatalf("%s: build() returned nil JobArgs", s.name)
			}
			if insertOpts == nil {
				t.Fatalf("%s: build() returned nil InsertOpts", s.name)
			}

			hasDedup := insertOpts.UniqueOpts.ByPeriod > 0 || len(insertOpts.UniqueOpts.ByState) > 0
			if !hasDedup {
				t.Errorf("%s: InsertOpts must carry a real dedup guarantee (non-zero ByPeriod or "+
					"non-empty ByState) — RunOnStart: true without one lets a start-of-process "+
					"catch-up insert double-fire alongside an already-scheduled run (Issue #1448)",
					s.name)
			}
		})
	}
}

// TestBuildPeriodicJobSpecs_IDsAreUnique proves no two specs share an ID — river.PeriodicJob's
// own doc comment requires periodic job IDs to be unique across all periodic jobs registered
// with a single client; river.AddSafely errors on a duplicate.
func TestBuildPeriodicJobSpecs_IDsAreUnique(t *testing.T) {
	specs := buildPeriodicJobSpecs(allNonNilPeriodicJobsDeps())

	seen := make(map[string]string, len(specs))
	for _, s := range specs {
		if s.opts == nil || s.opts.ID == "" {
			continue // covered by TestBuildPeriodicJobSpecs_AllHaveRunOnStartDedupAndID
		}
		if other, ok := seen[s.opts.ID]; ok {
			t.Errorf("periodic job ID %q used by both %q and %q — river.PeriodicJobOpts.ID must be unique",
				s.opts.ID, other, s.name)
			continue
		}
		seen[s.opts.ID] = s.name
	}
}
