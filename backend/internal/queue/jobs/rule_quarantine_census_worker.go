package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/riverqueue/river"
)

// RuleQuarantineCensusArgs is the River job for #1625 Task 18's distinct-rule census
// (services.RunRuleQuarantineCensus). Fix round 1: the census existed but had no production
// caller, so categorization_rule_quarantine_population never left its pre-warmed zero — an
// unrun census and a genuinely empty one were indistinguishable, the exact ambiguity this
// branch already fixed once for the per-observation counter. This worker is that caller.
type RuleQuarantineCensusArgs struct {
	TriggerSource string `json:"trigger_source,omitempty"`
}

func (RuleQuarantineCensusArgs) Kind() string { return "rule_quarantine_census" }

// RuleQuarantineCensusWorker scans every categorization rule once (a full-table read via
// ListAllForQuarantineCensus) and sets the distinct-rule population gauge. It depends only on
// the repository, not the service package's other machinery, to keep this worker's blast radius
// to exactly the one function it calls.
type RuleQuarantineCensusWorker struct {
	river.WorkerDefaults[RuleQuarantineCensusArgs]

	ruleRepo repository.CategorizationRuleRepository
	logger   *slog.Logger
}

type RuleQuarantineCensusWorkerDeps struct {
	RuleRepo repository.CategorizationRuleRepository
	Logger   *slog.Logger
}

func NewRuleQuarantineCensusWorker(deps RuleQuarantineCensusWorkerDeps) *RuleQuarantineCensusWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &RuleQuarantineCensusWorker{
		ruleRepo: deps.RuleRepo,
		logger:   logger,
	}
}

// Work runs the census. Success/failure are both made visible the same way every other
// periodic job in this codebase is: observability.JobsCompleted counts this run by status,
// observability.JobDuration times it, and — ONLY on success — observability.RecordJobSuccess
// stamps job_last_success_timestamp_seconds for this job's Kind(). That heartbeat is the fix
// round 1 finding's third requirement: if this job starts silently failing, the gauge keeps
// serving its last value and LOOKS healthy, but the heartbeat stops advancing and is
// alertable — a stale census is detectable even though the gauge itself carries no timestamp.
func (w *RuleQuarantineCensusWorker) Work(ctx context.Context, job *river.Job[RuleQuarantineCensusArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting rule quarantine census",
		"job_id", job.ID,
		"trigger_source", args.TriggerSource,
		"attempt", job.Attempt,
	)

	start := time.Now()
	defer func() {
		status := "success"
		if err != nil {
			status = "failed"
		} else {
			observability.RecordJobSuccess(job.Kind)
		}
		observability.JobsCompleted.WithLabelValues(job.Kind, job.Queue, status).Inc()
		observability.JobDuration.WithLabelValues(job.Kind).Observe(time.Since(start).Seconds())
	}()

	if err = services.RunRuleQuarantineCensus(ctx, w.ruleRepo); err != nil {
		w.logger.Error("rule quarantine census failed",
			"job_id", job.ID,
			"error", err,
		)
		return fmt.Errorf("rule quarantine census failed: %w", err)
	}

	w.logger.Info("rule quarantine census complete",
		"job_id", job.ID,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	return nil
}

// RuleQuarantineCensusUniqueOpts dedupes by a 24h period, matching every other daily
// full-table-scan-into-an-aggregate job in this file's family (SavingsStatsWorker,
// PortfolioSnapshotWorker). Neither of those siblings paginates or batches its scan either —
// the mitigation this codebase uses for a bounded, once-a-day full-table read is cadence and
// queue priority, not chunking. categorization_rules is bounded by (unlimited-but-human) rules
// per user, the same shape as the tables those siblings scan; if that ever changes, batching
// belongs in ListAllForQuarantineCensus itself, not in this worker's scheduling.
func RuleQuarantineCensusUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByPeriod: 24 * time.Hour,
	}
}

// RuleQuarantineCensusInsertOpts uses queue.QueueLow — the same queue every other daily
// check/aggregate job in this file's family runs on, so this census competes with background
// maintenance work for worker capacity, never with user-facing traffic.
func RuleQuarantineCensusInsertOpts() *river.InsertOpts {
	return riverInsertOptsFromUnique(queue.QueueLow, RuleQuarantineCensusUniqueOpts())
}

func RuleQuarantineCensusPeriodicOpts() *river.PeriodicJobOpts {
	return &river.PeriodicJobOpts{RunOnStart: true, ID: RuleQuarantineCensusArgs{}.Kind()}
}
