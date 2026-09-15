package main

// Issue #1448: periodic River job registration, extracted out of main()'s ~300-line inline
// block so it can be unit-tested directly.
//
// river.PeriodicJob's own fields (constructorFunc, opts, scheduleFunc) are unexported —
// nothing outside package river can inspect what a *river.PeriodicJob actually carries. Every
// job here is therefore built as a periodicJobSpec first (a plain, fully-inspectable
// intermediate value) and only converted to river's opaque type at the very end, in
// buildPeriodicJobs. This is what lets periodic_jobs_test.go assert the Issue #1448 invariant
// — every registration has RunOnStart: true, a non-empty PeriodicJobOpts.ID, and a real dedup
// guarantee — against real values instead of scanning main.go as text.
import (
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue/jobs"
	"github.com/riverqueue/river"
)

// periodicJobsDeps holds the (nil-guarded) workers whose periodic registration
// buildPeriodicJobSpecs configures. A nil field omits that job's spec entirely, matching this
// package's existing nil-guard convention for optional subsystems (e.g. Plaid-specific
// workers that are nil when Plaid isn't configured).
type periodicJobsDeps struct {
	revenueCatRepairWorker            *jobs.RevenueCatRepairWorker
	scheduledDeletionCheckWorker      *jobs.ScheduledDeletionCheckWorker
	itemCleanupCheckWorker            *jobs.ItemCleanupCheckWorker
	retentionCleanupCheckWorker       *jobs.RetentionCleanupCheckWorker
	householdMemberCleanupCheckWorker *jobs.HouseholdMemberCleanupCheckWorker
	householdOverCapacityCheckWorker  *jobs.HouseholdOverCapacityCheckWorker
	dlqCleanupCheckWorker             *jobs.DLQCleanupCheckWorker
	budgetPeriodCloseCheckWorker      *jobs.BudgetPeriodCloseCheckWorker
	savingsStatsWorker                *jobs.SavingsStatsWorker
	portfolioSnapshotWorker           *jobs.PortfolioSnapshotWorker
	ruleQuarantineCensusWorker        *jobs.RuleQuarantineCensusWorker
	// budgetHierarchyAuditWorker is the Issue #1630 Task B7 periodic sweep over budgets the
	// write-time guards (domain.DuplicateParentIdentity, domain.EscapedNestedChild) would refuse
	// if created today, but which predate them. No Plaid dependency.
	budgetHierarchyAuditWorker *jobs.BudgetHierarchyAuditWorker
	// balanceSnapshotDispatchWorker is Plaid-specific and nil when Plaid is not configured: its
	// capture jobs call /accounts/get, so with no Plaid there is nothing for it to dispatch
	// (#1626 Phase 3, Defect 3).
	balanceSnapshotDispatchWorker *jobs.BalanceSnapshotDispatchWorker
	// plaidOrphanRetryWorker is Plaid-specific and nil when Plaid is not configured (Issue #1497).
	// ⚠ There is deliberately NO field for the orphan-token PURGE worker: its registration is
	// unconditional, so making it a nil-guardable dependency would be the first step towards it
	// silently not running. See the spec below.
	plaidOrphanRetryWorker *jobs.PlaidOrphanRetryWorker
}

// periodicJobSpec is an intermediate representation of a periodic job registration, captured
// BEFORE conversion to river's opaque *river.PeriodicJob. See this file's package doc comment
// for why the extraction exists.
type periodicJobSpec struct {
	// name identifies the job in test failure messages (human-readable, not River's Kind()).
	name string
	// logMessage is the exact startup log line main.go historically emitted for this job —
	// preserved so operators see the same "<job> periodic job configured (<cadence>)" line.
	logMessage string
	interval   time.Duration
	build      func() (river.JobArgs, *river.InsertOpts)
	opts       *river.PeriodicJobOpts
	// schedule OVERRIDES interval when set, for a job whose cadence is a wall-clock time rather
	// than a gap since the last run (#1626 Phase 3, Defect 3).
	//
	// ⚠ WHY THIS FIELD EXISTS AT ALL, since 13 of the 14 specs do not use it. River ships only
	// PeriodicInterval, which schedules "N after the last run" — and with RunOnStart: true (which
	// every spec here carries, by Issue #1448's invariant) that anchors a daily job to whenever the
	// process last started. For a cleanup sweep that is irrelevant. For the balance snapshot
	// dispatch it is not: its 21:00 UTC hour is chosen so the row lands near a closing balance AND
	// so River's retry budget fits before the UTC day rolls over, and an interval would silently
	// move that hour to whenever the last deploy happened.
	//
	// `interval` is still set alongside it, as the documented cadence the introspection tests read;
	// periodic_jobs_test.go asserts the two AGREE, so the pair cannot drift into stating different
	// frequencies.
	schedule river.PeriodicSchedule
}

// toPeriodicJob converts this spec to the *river.PeriodicJob river.Config.PeriodicJobs
// actually requires.
func (s periodicJobSpec) toPeriodicJob() *river.PeriodicJob {
	schedule := s.schedule
	if schedule == nil {
		schedule = river.PeriodicInterval(s.interval)
	}
	return river.NewPeriodicJob(schedule, s.build, s.opts)
}

// buildPeriodicJobSpecs returns the periodicJobSpec for every periodic job that should be
// registered given the (nil-guarded) workers in deps, plus the always-on idempotency cleanup
// job (constructed unconditionally, matching main.go's pre-existing behavior — its worker is
// built after periodic jobs are configured, but the periodic registration only needs its Args
// type and Insert/PeriodicOpts, not the worker instance itself).
//
// Issue #1448: every spec here MUST carry RunOnStart: true, a non-empty PeriodicJobOpts.ID,
// and a real dedup guarantee (UniqueOpts, or — for BudgetPeriodCloseCheck — its own
// documented DB-level exactly-once marker, which BudgetPeriodCloseCheckInsertOpts also now
// backs with UniqueOpts as defense in depth). TestBuildPeriodicJobSpecs_AllHaveRunOnStartDedupAndID
// (periodic_jobs_test.go) enforces this for every entry this function returns.
func buildPeriodicJobSpecs(deps periodicJobsDeps) []periodicJobSpec {
	var specs []periodicJobSpec
	if deps.revenueCatRepairWorker != nil {
		specs = append(specs, periodicJobSpec{
			name: "RevenueCat repair", logMessage: "RevenueCat repair configured (every minute)", interval: time.Minute,
			build: func() (river.JobArgs, *river.InsertOpts) {
				return jobs.RevenueCatRepairArgs{}, jobs.RevenueCatRepairInsertOpts()
			},
			opts: jobs.RevenueCatRepairPeriodicOpts(),
		})
	}

	// Issue #930 / #1450: Scheduled deletion check (hourly).
	if deps.scheduledDeletionCheckWorker != nil {
		specs = append(specs, periodicJobSpec{
			name:       "scheduled deletion check",
			logMessage: "Scheduled deletion check periodic job configured (hourly)",
			interval:   1 * time.Hour,
			build: func() (river.JobArgs, *river.InsertOpts) {
				return jobs.ScheduledDeletionCheckArgs{TriggerSource: "periodic"}, jobs.ScheduledDeletionCheckInsertOpts()
			},
			opts: jobs.ScheduledDeletionCheckPeriodicOpts(),
		})
	}

	// Issue #668: Item cleanup check (daily) — Plaid-specific, nil-guarded.
	if deps.itemCleanupCheckWorker != nil {
		specs = append(specs, periodicJobSpec{
			name:       "item cleanup check",
			logMessage: "Item cleanup check periodic job configured (daily)",
			interval:   24 * time.Hour,
			build: func() (river.JobArgs, *river.InsertOpts) {
				return jobs.ItemCleanupCheckArgs{TriggerSource: "periodic"}, jobs.ItemCleanupCheckInsertOpts()
			},
			opts: jobs.ItemCleanupCheckPeriodicOpts(),
		})
	}

	// Issue #930 / #1413: Retention cleanup check (daily) — the only place that runs
	// retention-clock repair, the 30-day/7-day GDPR pre-deletion notices,
	// deleteExpiredAccounts, and the Plaid-disconnect repair pass.
	if deps.retentionCleanupCheckWorker != nil {
		specs = append(specs, periodicJobSpec{
			name:       "retention cleanup check",
			logMessage: "Retention cleanup check periodic job configured (daily)",
			interval:   24 * time.Hour,
			build: func() (river.JobArgs, *river.InsertOpts) {
				return jobs.RetentionCleanupCheckArgs{TriggerSource: "periodic"}, jobs.RetentionCleanupCheckInsertOpts()
			},
			opts: jobs.RetentionCleanupCheckPeriodicOpts(),
		})
	}

	// Issue #1371 — Phase 7 (Task 2): Household member cleanup check (daily).
	if deps.householdMemberCleanupCheckWorker != nil {
		specs = append(specs, periodicJobSpec{
			name:       "household member cleanup check",
			logMessage: "Household member cleanup check periodic job configured (daily)",
			interval:   24 * time.Hour,
			build: func() (river.JobArgs, *river.InsertOpts) {
				return jobs.HouseholdMemberCleanupCheckArgs{TriggerSource: "periodic"}, jobs.HouseholdMemberCleanupCheckInsertOpts()
			},
			opts: jobs.HouseholdMemberCleanupCheckPeriodicOpts(),
		})
	}

	// Issue #1371 — Phase 7 (Task 6): Household over-capacity check (daily).
	if deps.householdOverCapacityCheckWorker != nil {
		specs = append(specs, periodicJobSpec{
			name:       "household over-capacity check",
			logMessage: "Household over-capacity check periodic job configured (daily)",
			interval:   24 * time.Hour,
			build: func() (river.JobArgs, *river.InsertOpts) {
				return jobs.HouseholdOverCapacityCheckArgs{TriggerSource: "periodic"}, jobs.HouseholdOverCapacityCheckInsertOpts()
			},
			opts: jobs.HouseholdOverCapacityCheckPeriodicOpts(),
		})
	}

	// Issue #633: DLQ cleanup check (daily).
	if deps.dlqCleanupCheckWorker != nil {
		specs = append(specs, periodicJobSpec{
			name:       "DLQ cleanup check",
			logMessage: "DLQ cleanup check periodic job configured (daily)",
			interval:   24 * time.Hour,
			build: func() (river.JobArgs, *river.InsertOpts) {
				return jobs.DLQCleanupCheckArgs{TriggerSource: "periodic"}, jobs.DLQCleanupInsertOpts()
			},
			opts: jobs.DLQCleanupPeriodicOpts(),
		})
	}

	// Issue #972: Budget period close check (daily) — detects closed budget periods and fires events.
	if deps.budgetPeriodCloseCheckWorker != nil {
		specs = append(specs, periodicJobSpec{
			name:       "budget period close check",
			logMessage: "Budget period close check periodic job configured (daily)",
			interval:   24 * time.Hour,
			build: func() (river.JobArgs, *river.InsertOpts) {
				return jobs.BudgetPeriodCloseCheckArgs{TriggerSource: "periodic"}, jobs.BudgetPeriodCloseCheckInsertOpts()
			},
			opts: jobs.BudgetPeriodCloseCheckPeriodicOpts(),
		})
	}

	// Issue #972: Savings stats snapshot (daily) — aggregate completed transfer amounts.
	if deps.savingsStatsWorker != nil {
		specs = append(specs, periodicJobSpec{
			name:       "savings stats snapshot",
			logMessage: "Savings stats snapshot periodic job configured (daily)",
			interval:   24 * time.Hour,
			build: func() (river.JobArgs, *river.InsertOpts) {
				return jobs.SavingsStatsArgs{TriggerSource: "periodic"}, jobs.SavingsStatsInsertOpts()
			},
			opts: jobs.SavingsStatsPeriodicOpts(),
		})
	}

	// Issue #138: Portfolio snapshot (daily) — per-user holdings aggregate upsert.
	if deps.portfolioSnapshotWorker != nil {
		specs = append(specs, periodicJobSpec{
			name:       "portfolio snapshot",
			logMessage: "Portfolio snapshot periodic job configured (daily)",
			interval:   24 * time.Hour,
			build: func() (river.JobArgs, *river.InsertOpts) {
				return jobs.PortfolioSnapshotArgs{TriggerSource: "periodic"}, jobs.PortfolioSnapshotInsertOpts()
			},
			opts: jobs.PortfolioSnapshotPeriodicOpts(),
		})
	}

	// Issue #1630 Task B7: Budget hierarchy audit (daily) — sweeps budgets
	// BudgetIDsWithUnexaminedHierarchy reports, most-behind-first. Daily + QueueLow matches this
	// file's other full-table-scan-into-a-ledger jobs (portfolio snapshot's own #1569 Task 11
	// repair pass runs at the SAME cadence for the same reason: a bounded per-run cap plus a
	// resumable enumeration means daily is frequent enough to drain any backlog without a
	// dedicated schedule).
	if deps.budgetHierarchyAuditWorker != nil {
		specs = append(specs, periodicJobSpec{
			name:       "budget hierarchy audit",
			logMessage: "Budget hierarchy audit periodic job configured (daily)",
			interval:   24 * time.Hour,
			build: func() (river.JobArgs, *river.InsertOpts) {
				return jobs.BudgetHierarchyAuditArgs{TriggerSource: "periodic"}, jobs.BudgetHierarchyAuditInsertOpts()
			},
			opts: jobs.BudgetHierarchyAuditPeriodicOpts(),
		})
	}

	// #1625 Task 18 fix round 1: rule quarantine census (daily) — distinct-rule population gauge,
	// previously computed by services.RunRuleQuarantineCensus but never invoked in production.
	// Daily + QueueLow matches this file's other full-table-scan-into-an-aggregate jobs
	// (savings stats, portfolio snapshot) — see RuleQuarantineCensusUniqueOpts/InsertOpts for why
	// that cadence and queue are the right bound on cost rather than pagination.
	if deps.ruleQuarantineCensusWorker != nil {
		specs = append(specs, periodicJobSpec{
			name:       "rule quarantine census",
			logMessage: "Rule quarantine census periodic job configured (daily)",
			interval:   24 * time.Hour,
			build: func() (river.JobArgs, *river.InsertOpts) {
				return jobs.RuleQuarantineCensusArgs{TriggerSource: "periodic"}, jobs.RuleQuarantineCensusInsertOpts()
			},
			opts: jobs.RuleQuarantineCensusPeriodicOpts(),
		})
	}

	// Issue #1626 Phase 3, Defect 3: Balance snapshot dispatch (daily at a fixed UTC hour) — fans
	// out one per-item capture job each, so River's retry applies per item.
	//
	// ⚠ THE ONLY SPEC HERE WITH AN EXPLICIT SCHEDULE. See periodicJobSpec.schedule for why an
	// interval cannot express this job's cadence, and jobs.BalanceSnapshotDispatchHourUTC for why
	// the hour is 21:00 rather than later.
	if deps.balanceSnapshotDispatchWorker != nil {
		specs = append(specs, periodicJobSpec{
			name:       "balance snapshot dispatch",
			logMessage: "Balance snapshot dispatch periodic job configured (daily at 21:00 UTC)",
			interval:   24 * time.Hour,
			schedule:   jobs.BalanceSnapshotDispatchSchedule(),
			build: func() (river.JobArgs, *river.InsertOpts) {
				return jobs.BalanceSnapshotDispatchArgs{TriggerSource: "periodic"}, jobs.BalanceSnapshotDispatchInsertOpts()
			},
			opts: jobs.BalanceSnapshotDispatchPeriodicOpts(),
		})
	}

	// Issue #1087 R4-7: Idempotency keys cleanup (daily — deletes expired rows). Unconditional
	// — its worker is constructed after periodic jobs are configured (see main()), but the
	// periodic registration itself needs only the Args type and Insert/PeriodicOpts.
	specs = append(specs, periodicJobSpec{
		name:       "idempotency cleanup",
		logMessage: "Idempotency cleanup periodic job configured (daily)",
		interval:   24 * time.Hour,
		build: func() (river.JobArgs, *river.InsertOpts) {
			return jobs.IdempotencyCleanupArgs{TriggerSource: "periodic"}, jobs.IdempotencyCleanupInsertOpts()
		},
		opts: jobs.IdempotencyCleanupPeriodicOpts(),
	})

	// Issue #1497: purge retained Plaid credentials at their retention deadline (hourly).
	//
	// ⚠ UNCONDITIONAL, AND THAT IS THE POINT. Unlike every other Plaid-adjacent job here this one is NOT
	// nil-guarded, because it must not be: DeleteUserCascade writes plaid_orphaned_items rows — with a
	// retained credential — whether or not Plaid is configured at the time, and migration 000135's
	// promise is that the credential is purged at its deadline regardless of anything else — at the
	// first sweep at or after it, the interval below being the bound. Guarding this on
	// Plaid config would make a GDPR retention limit conditional on a feature flag. Its worker is
	// constructed unconditionally in main() for the same reason; the registration needs only the Args
	// type and Insert/PeriodicOpts, exactly like idempotency cleanup above.
	//
	// ⚠ THE INTERVAL IS PART OF THE PUBLISHED GUARANTEE, so it is domain.OrphanedItemTokenPurgeInterval
	// and not a literal here. Unconditional is not instantaneous: a credential ceases to EXIST at the
	// first sweep at or after its deadline, so this interval IS the stated upper bound on how long one
	// outlives its deadline (its unusability is exact and independent of this job — see
	// domain.TokenIsLive). It was 24 hours while the schema comment claimed the credential could not
	// exist past the deadline at all; one hour, plus honest prose, is that fix. PlaidOrphanTokenPurge-
	// UniqueOpts().ByPeriod reads the same constant — with a longer ByPeriod, River would deduplicate
	// every extra insert against the already-Completed run and this interval would be inert.
	specs = append(specs, periodicJobSpec{
		name:       "plaid orphan token purge",
		logMessage: "Plaid orphaned-item credential purge periodic job configured (hourly)",
		interval:   domain.OrphanedItemTokenPurgeInterval,
		build: func() (river.JobArgs, *river.InsertOpts) {
			return jobs.PlaidOrphanTokenPurgeArgs{TriggerSource: "periodic"}, jobs.PlaidOrphanTokenPurgeInsertOpts()
		},
		opts: jobs.PlaidOrphanTokenPurgePeriodicOpts(),
	})

	// Issue #1497: re-attempt /item/remove for Items stranded by an erasure (daily) — Plaid-specific,
	// so nil-guarded, which is precisely why the purge above is not.
	if deps.plaidOrphanRetryWorker != nil {
		specs = append(specs, periodicJobSpec{
			name:       "plaid orphan retry",
			logMessage: "Plaid orphaned-item removal retry periodic job configured (daily)",
			interval:   24 * time.Hour,
			build: func() (river.JobArgs, *river.InsertOpts) {
				return jobs.PlaidOrphanRetryArgs{TriggerSource: "periodic"}, jobs.PlaidOrphanRetryInsertOpts()
			},
			opts: jobs.PlaidOrphanRetryPeriodicOpts(),
		})
	}

	return specs
}

// buildPeriodicJobs converts every spec buildPeriodicJobSpecs returns to river's
// *river.PeriodicJob, logging each one's configuration the same way main.go historically did
// inline, and returns the slice for riverConfig.PeriodicJobs.
func buildPeriodicJobs(deps periodicJobsDeps, logger *slog.Logger) []*river.PeriodicJob {
	specs := buildPeriodicJobSpecs(deps)
	periodicJobs := make([]*river.PeriodicJob, 0, len(specs))
	for _, s := range specs {
		periodicJobs = append(periodicJobs, s.toPeriodicJob())
		if logger != nil {
			logger.Info(s.logMessage)
		}
	}
	return periodicJobs
}
