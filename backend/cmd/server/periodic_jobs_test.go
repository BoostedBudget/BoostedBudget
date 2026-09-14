package main

// Issue #1448: every one of the 18 periodic River jobs registered by main.go must carry
// (1) a real dedup guarantee (queue-level UniqueOpts, or — for BudgetPeriodCloseCheck — its
// own documented DB-level exactly-once marker), (2) RunOnStart: true, and (3) a non-empty,
// stable PeriodicJobOpts.ID so River restores nextRunAt across restarts/leader elections
// instead of recomputing now+interval every time (river@v0.43.0
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
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue/jobs"
)

// allNonNilPeriodicJobsDeps returns a periodicJobsDeps with every worker field populated by a
// real (zero-configured) worker instance, so buildPeriodicJobSpecs produces the full set of
// 18 specs — one per periodic job main.go registers when every dependency is present.
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
		ruleQuarantineCensusWorker:          jobs.NewRuleQuarantineCensusWorker(jobs.RuleQuarantineCensusWorkerDeps{}),
		balanceSnapshotDispatchWorker:       jobs.NewBalanceSnapshotDispatchWorker(jobs.BalanceSnapshotDispatchWorkerDeps{}),
		// Issue #1497. Only the RETRY worker appears here, because only it is nil-guardable: the
		// orphan-token purge has no periodicJobsDeps field at all — its registration is unconditional
		// on purpose, so there is nothing to set and nothing that could omit it. That asymmetry is
		// what TestBuildPeriodicJobSpecs_PurgeSpecSurvivesEveryNilWorker below asserts.
		plaidOrphanRetryWorker: jobs.NewPlaidOrphanRetryWorker(jobs.PlaidOrphanRetryWorkerDeps{}),
		postHogErasureWorker:   jobs.NewPostHogErasureWorker(jobs.PostHogErasureWorkerDeps{}),
	}
}

// TestBuildPeriodicJobSpecs_Count proves every nil-guarded worker (plus the always-on jobs)
// produces exactly one spec when non-nil — 16 nil-guarded workers + 2 unconditional jobs
// (idempotency cleanup, and Issue #1497's orphan-token purge) = 18.
//
// ⚠ THIS COUNT MOVED TWICE INDEPENDENTLY AND THE TWO INCREMENTS ARE BOTH REAL. #1625 Task 18 fix
// round 1 added the rule quarantine census (nil-guarded), 16 -> 17 on staging; #1626 Phase 3
// Defect 3 added the balance snapshot dispatch (nil-guarded), 16 -> 17 on this branch. Merging the
// two branches means BOTH specs exist, so the count is 18 — a resolution that kept only one side's
// number would have left an unwired worker, and `periodicJobsDeps` fields are nil-safe by design
// so nothing else in the suite would have said so.
func TestBuildPeriodicJobSpecs_Count(t *testing.T) {
	specs := buildPeriodicJobSpecs(allNonNilPeriodicJobsDeps())

	if len(specs) != 19 {
		t.Fatalf("expected 19 periodic job specs when every worker is non-nil, got %d", len(specs))
	}
}

// TestBuildPeriodicJobSpecs_PurgeSpecSurvivesEveryNilWorker is Issue #1497's registration guarantee,
// and it is deliberately the most aggressive form of the question: with EVERY nil-guardable worker
// absent — no Plaid, no retry sweep, nothing — the credential purge is still registered.
//
// That is the whole reason the purge is a separate job with no periodicJobsDeps field. Migration
// 000135 promises a retained Plaid credential is purged at its 30-day deadline — at the first sweep at
// or after it — "whether or not the retry job ever ran"; a registration that could be omitted by a nil
// worker would make that promise conditional on configuration, which is not a promise.
func TestBuildPeriodicJobSpecs_PurgeSpecSurvivesEveryNilWorker(t *testing.T) {
	specs := buildPeriodicJobSpecs(periodicJobsDeps{})

	var found bool
	for _, s := range specs {
		if s.name == "plaid orphan token purge" {
			found = true
		}
		if s.name == "plaid orphan retry" {
			t.Fatal("the Plaid-dependent retry sweep must NOT be registered with no worker to run it")
		}
	}
	if !found {
		t.Fatal("the orphaned-credential purge must be registered even with every optional worker nil — " +
			"its retention deadline is not conditional on Plaid being configured")
	}
}

// TestBuildPeriodicJobSpecs_NilWorkerOmitsItsSpec proves the nil-guard still works after the
// extraction — a nil worker must not produce a spec (matches main.go's pre-extraction
// behavior of skipping registration entirely for an unconfigured job).
func TestBuildPeriodicJobSpecs_NilWorkerOmitsItsSpec(t *testing.T) {
	deps := allNonNilPeriodicJobsDeps()
	deps.itemCleanupCheckWorker = nil

	specs := buildPeriodicJobSpecs(deps)

	if len(specs) != 18 {
		t.Fatalf("expected 18 specs with itemCleanupCheckWorker nil, got %d", len(specs))
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

// TestBuildPeriodicJobSpecs_PurgeRunsHourlyAndItsDedupDoesNotSuppressThat makes the sweep interval —
// the thing that BOUNDS how long a retained Plaid credential can outlive its deadline — observable.
//
// The interval was 24 hours, while migration 000135 and domain.OrphanedItemTokenRetention both claimed
// the credential "must not exist" after token_expires_at. A daily capped sweep cannot deliver that, so
// the bound is now stated numerically everywhere it appears AND shortened to an hour. This test is the
// only place that shortening is falsifiable.
//
// ⚠ FOUR ASSERTIONS, NONE OF THEM REDUNDANT:
//
//  1. The value comes from the SPEC BUILDER, and is compared to a literal 1*time.Hour as well as to
//     domain.OrphanedItemTokenPurgeInterval. Comparing only to the constant would be circular — the
//     spec is built FROM that constant, so the pair would agree by construction at any value.
//  2. UniqueOpts.ByPeriod must EQUAL the interval — both directions are real bugs. The dedup key
//     includes JobStateCompleted (required by RunOnStart, Issue #1448), so a 24-hour ByPeriod would
//     silently deduplicate all 23 extra hourly inserts against the run that already completed, leaving
//     the effective cadence at 24 hours while the interval field read "1h" — the interval change would
//     have been inert and no other test would have noticed. A ZERO ByPeriod is worse: uniqueness
//     degrades to kind-only while Completed stays in ByState, so the first completed run suppresses
//     every future insert and the sweep runs once, ever.
//  3. ByPeriod must be at least a second, which River itself requires at insert time.
//  4. SIBLING: the Plaid-dependent RETRY sweep is still daily. Without it this test passes just as
//     well if someone set every periodic job on the branch to an hour, and domain.
//     OrphanedItemRetryStaleAfter's "two cadences of the daily retry sweep" derivation would be
//     quietly wrong.
func TestBuildPeriodicJobSpecs_PurgeRunsHourlyAndItsDedupDoesNotSuppressThat(t *testing.T) {
	// Empty deps on purpose: the purge registration is unconditional, so this also re-proves it is
	// present at the interval being asserted rather than at some other code path's.
	var purge *periodicJobSpec
	unconditional := buildPeriodicJobSpecs(periodicJobsDeps{})
	for i, s := range unconditional {
		if s.name == "plaid orphan token purge" {
			purge = &unconditional[i]
		}
	}
	if purge == nil {
		t.Fatal("no 'plaid orphan token purge' spec — see TestBuildPeriodicJobSpecs_PurgeSpecSurvivesEveryNilWorker")
	}

	if purge.interval != 1*time.Hour {
		t.Errorf("purge interval = %v, want 1h: it is the stated per-sweep bound on how long a retained "+
			"Plaid credential can still exist past its deadline, and the schema comment, the domain "+
			"constant and the ADR all quote it", purge.interval)
	}
	if purge.interval != domain.OrphanedItemTokenPurgeInterval {
		t.Errorf("purge interval = %v but domain.OrphanedItemTokenPurgeInterval = %v — the registration "+
			"must be derived from the constant the prose quotes, or the two can drift",
			purge.interval, domain.OrphanedItemTokenPurgeInterval)
	}

	_, insertOpts := purge.build()
	if insertOpts == nil {
		t.Fatal("purge build() returned nil InsertOpts")
	}
	// EQUALITY, not `<=`: see (2) above. Neither direction is caught by any other test —
	// TestBuildPeriodicJobSpecs_AllHaveRunOnStartDedupAndID accepts non-zero ByPeriod OR a non-empty
	// ByState, and ByState stays non-empty when ByPeriod is zeroed.
	if insertOpts.UniqueOpts.ByPeriod != purge.interval {
		t.Errorf("purge UniqueOpts.ByPeriod = %v, want exactly its %v interval: longer discards inserts "+
			"against the already-completed run (the real cadence becomes ByPeriod); zero degrades dedup "+
			"to kind-only, and with JobStateCompleted in ByState that means the sweep runs once, ever",
			insertOpts.UniqueOpts.ByPeriod, purge.interval)
	}
	if insertOpts.UniqueOpts.ByPeriod < time.Second {
		t.Errorf("purge UniqueOpts.ByPeriod = %v: River rejects anything below one second at insert "+
			"time (river@v0.43.0 insert_opts.go:256), so this would fail in production, not here",
			insertOpts.UniqueOpts.ByPeriod)
	}

	// ⚠ WHAT THIS TEST DOES NOT PROVE, recorded so nobody mistakes it for a proof of exact hourly
	// execution: the dedup bucket is Truncate(ByPeriod) of an UNALIGNED wall-clock instant, computed
	// BEFORE ScheduledAt is assigned (river@v0.43.0 client.go:1778 vs :1787-1795, and
	// internal/dbunique/db_unique.go:117). Two runs one interval apart normally land in adjacent
	// buckets, but a run enqueued marginally early can collide with the previous bucket and be
	// suppressed. That — along with downtime, leader election and worker starvation — is why the
	// documented bound is PER SWEEP rather than absolute. See domain.OrphanedItemTokenPurgeInterval.

	var retry *periodicJobSpec
	full := buildPeriodicJobSpecs(allNonNilPeriodicJobsDeps())
	for i, s := range full {
		if s.name == "plaid orphan retry" {
			retry = &full[i]
		}
	}
	if retry == nil {
		t.Fatal("no 'plaid orphan retry' spec with every worker non-nil — test setup bug")
	}
	if retry.interval != 24*time.Hour {
		t.Errorf("SIBLING ASSERTION: the Plaid-dependent retry sweep interval = %v, want 24h. The purge "+
			"was shortened, the retry deliberately was not — domain.OrphanedItemRetryStaleAfter (48h) is "+
			"documented as two cadences of THIS sweep", retry.interval)
	}
}

// TestBuildPeriodicJobSpecs_BalanceSnapshotDispatchRunsDailyAtTheChosenUTCHour is #1626 Phase 3
// Task G's T1, and it pins the three things about this registration that could each go wrong
// silently.
//
// WHY IT NEEDS ITS OWN TEST rather than riding on the count and the RunOnStart/dedup invariant:
//
//  1. THE SCHEDULE, NOT THE INTERVAL. Every other periodic job in this file expresses "daily" as a
//     24-hour PeriodicInterval, which River anchors to the last run — so with RunOnStart, a deploy
//     at 04:00 moves every subsequent run to 04:00. That is harmless for a cleanup sweep and NOT
//     harmless here: the 21:00 UTC hour exists so that a net-worth series records something close
//     to a closing balance AND so River's retry budget fits before the UTC day rolls over (a failed
//     run at the boundary loses the day permanently — there is no backfill). This job therefore
//     carries an explicit schedule, and this asserts it is actually attached.
//  2. THE INTERVAL FIELD MUST STILL AGREE. The spec keeps `interval` as its documented cadence and
//     the introspection tests read it, so a schedule that fired at some other frequency than the
//     interval claims would make this file's own prose false.
//  3. ByPeriod MUST BE SHORTER THAN A DAY, which is the opposite of what every other daily job here
//     does — see BalanceSnapshotDispatchUniqueOpts for why a 24h window would let a deploy suppress
//     that day's 21:00 run against the already-completed RunOnStart run.
func TestBuildPeriodicJobSpecs_BalanceSnapshotDispatchRunsDailyAtTheChosenUTCHour(t *testing.T) {
	specs := buildPeriodicJobSpecs(allNonNilPeriodicJobsDeps())

	var spec *periodicJobSpec
	for i := range specs {
		if specs[i].name == "balance snapshot dispatch" {
			spec = &specs[i]
		}
	}
	if spec == nil {
		t.Fatal("no 'balance snapshot dispatch' spec — balance_snapshots would keep being written " +
			"ONLY as a side effect of a Plaid webhook sync, so a day with no webhook stays a day " +
			"with no row (#1626 Phase 3, Defect 3)")
	}

	if spec.interval != 24*time.Hour {
		t.Errorf("balance snapshot dispatch interval = %v, want 24h", spec.interval)
	}

	if spec.schedule == nil {
		t.Fatal("balance snapshot dispatch must carry an explicit schedule: a bare 24h " +
			"PeriodicInterval anchors the run to process start, so a deploy would move the daily " +
			"capture off its chosen UTC hour")
	}
	// Asked from an instant BEFORE the hour on a fixed date, so the answer is a value and not a
	// restatement of the constant's arithmetic.
	from := time.Date(2026, 9, 9, 6, 15, 0, 0, time.UTC)
	next := spec.schedule.Next(from)
	if got, want := next.UTC(), time.Date(2026, 9, 9, jobs.BalanceSnapshotDispatchHourUTC, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("schedule.Next(%v) = %v, want %v", from, got, want)
	}
	// And the schedule's own period must equal the interval the spec advertises.
	if got := spec.schedule.Next(next).Sub(next); got != spec.interval {
		t.Errorf("consecutive scheduled runs are %v apart but the spec's interval says %v", got, spec.interval)
	}

	_, insertOpts := spec.build()
	if insertOpts == nil {
		t.Fatal("balance snapshot dispatch build() returned nil InsertOpts")
	}
	if insertOpts.UniqueOpts.ByPeriod >= 24*time.Hour {
		t.Errorf("balance snapshot dispatch UniqueOpts.ByPeriod = %v: a window of a day or longer "+
			"buckets by UTC calendar day, so a deploy at any hour would silently suppress that day's "+
			"%02d:00 run against the already-completed RunOnStart run",
			insertOpts.UniqueOpts.ByPeriod, jobs.BalanceSnapshotDispatchHourUTC)
	}
}

// TestBuildPeriodicJobSpecs_EveryOtherSpecStillUsesItsInterval is the sibling assertion, and it is
// not decoration: without it, the test above passes just as well if someone attached a schedule to
// EVERY periodic job, which would change 17 unrelated jobs' cadence semantics as a side effect of
// this one addition.
func TestBuildPeriodicJobSpecs_EveryOtherSpecStillUsesItsInterval(t *testing.T) {
	for _, s := range buildPeriodicJobSpecs(allNonNilPeriodicJobsDeps()) {
		if s.name == "balance snapshot dispatch" {
			continue
		}
		if s.schedule != nil {
			t.Errorf("%s carries an explicit schedule: only the balance snapshot dispatch needs one, "+
				"and attaching them elsewhere silently changes those jobs' cadence anchoring", s.name)
		}
	}
}
