package jobs

// Issue #1531 Task 10b — Layer 4 item 4 (durable retry). The GDPR deletion path's cache purge
// (internal/cache's Purger, Generations.Bump) is best-effort Redis work run synchronously inside
// deletion_service.go's ExecuteDeletion, AFTER the DB cascade has committed — the erasure has
// already happened durably; only its cache footprint might not have. A Redis failure at that point
// must not silently complete the deletion with an un-purged cache (a household's stale aggregate
// or, worse, the deleted user's own u:/k: cache entries surviving past their TTL). This job is
// enqueued on that failure so River's own retry/backoff keeps trying until it succeeds or is
// dead-lettered (visible, not silent).
//
// No PII beyond the internal user ID / household ID / Clerk user ID travels in these args — same
// posture as EmailChangePropagationArgs.

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/riverqueue/river"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
)

// CachePurgeRetryArgs is enqueued when the deletion path's synchronous cache purge/bump failed.
// HouseholdID is empty when the deleted user did not belong to a household at deletion time.
type CachePurgeRetryArgs struct {
	UserID      string `json:"user_id"`
	ClerkUserID string `json:"clerk_user_id"`
	HouseholdID string `json:"household_id,omitempty"`
}

func (CachePurgeRetryArgs) Kind() string { return "cache_purge_retry" }

// CachePurgeRetryWorker retries a failed GDPR-deletion cache purge/household bump.
type CachePurgeRetryWorker struct {
	river.WorkerDefaults[CachePurgeRetryArgs]

	purger cache.Purger
	gens   *cache.Generations
	logger *slog.Logger
}

// NewCachePurgeRetryWorker builds a worker. purger/gens may be nil (mirrors a disabled/noop
// cache) — Work then does nothing and reports success, since there is nothing to purge.
func NewCachePurgeRetryWorker(purger cache.Purger, gens *cache.Generations, logger *slog.Logger) *CachePurgeRetryWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &CachePurgeRetryWorker{purger: purger, gens: gens, logger: logger}
}

// Work retries the purge/bump. Returning an error here lets River's own retry/backoff schedule
// another attempt (eventually dead-lettering per the standard queue configuration) rather than
// this worker swallowing the failure and completing silently.
func (w *CachePurgeRetryWorker) Work(ctx context.Context, job *river.Job[CachePurgeRetryArgs]) error {
	args := job.Args

	if w.purger != nil {
		if err := w.purger.PurgeUser(ctx, args.UserID, args.ClerkUserID); err != nil {
			return fmt.Errorf("cache purge retry: PurgeUser: %w", err)
		}
	}

	// The household bump is void by design (cache.Generations.Bump never returns an error) —
	// consistent with every other generation bump in this codebase. A Redis failure there is
	// logged by Bump itself and counted on cache_bump_failures_total; it does not fail this job,
	// since the user-scope purge above (the part that CAN fail loudly) already succeeded.
	//
	// Issue #1531 Final fixes: runs on context.WithTimeout(context.WithoutCancel(ctx), bumpCacheTimeout)
	// rather than the raw job ctx. This job is itself the durable retry for a bump that already
	// failed once — if River cancels ITS context (shutdown, retry, timeout) in the narrow window
	// after PurgeUser above has already succeeded but before this bump runs, the raw ctx would
	// swallow that cancellation silently and this job would report success with the household bump
	// never having happened — a second, terminal loss with nothing left to retry it. WithoutCancel
	// alone would leave the bump unbounded (it strips the parent's deadline too), hence the explicit
	// WithTimeout — matching every other post-commit bump helper in this codebase.
	if args.HouseholdID != "" && w.gens != nil {
		bumpCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bumpCacheTimeout)
		defer cancel()
		w.gens.Bump(bumpCtx, cache.ScopeHousehold, args.HouseholdID, "gdpr_deletion_retry")
	}

	return nil
}

var _ river.Worker[CachePurgeRetryArgs] = (*CachePurgeRetryWorker)(nil)
