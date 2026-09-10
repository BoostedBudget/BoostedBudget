package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/riverqueue/river"
)

// InvestmentTransactionsSyncArgs contains the arguments for an investment transactions sync job.
// Triggered by the INVESTMENTS_TRANSACTIONS webhook; uses offset-based pagination with
// an incremental start date tracked via investments_last_synced_date.
// Issue #659: Plaid Investments — Holdings, Securities, Investment Transactions
type InvestmentTransactionsSyncArgs struct {
	// UserID is the ID of the user who owns the linked item.
	UserID string `json:"user_id"`

	// ItemID is the internal UUID of the LinkedItem to sync (not the Plaid item_id).
	ItemID string `json:"item_id"`

	// WebhookID optionally tracks which webhook triggered this sync.
	WebhookID string `json:"webhook_id,omitempty"`
}

// Kind returns the unique identifier for this job type.
func (InvestmentTransactionsSyncArgs) Kind() string { return "investment_transactions_sync" }

// InvestmentTransactionsSyncWorker processes investment transactions sync jobs by fetching
// investment transactions from Plaid using offset pagination and upserting them.
type InvestmentTransactionsSyncWorker struct {
	river.WorkerDefaults[InvestmentTransactionsSyncArgs]

	investmentSyncSvc services.InvestmentSyncService
	linkedItemRepo    repository.LinkedItemRepository
	logger            *slog.Logger
	db                TxBeginner
	itemErrorSvc      ItemErrorNotifier // Issue #1609 P3-T2: nil-safe, notifies on permanent errors
}

// InvestmentTransactionsSyncWorkerDeps contains the dependencies for InvestmentTransactionsSyncWorker.
type InvestmentTransactionsSyncWorkerDeps struct {
	InvestmentSyncSvc services.InvestmentSyncService
	LinkedItemRepo    repository.LinkedItemRepository
	Logger            *slog.Logger
	DB                TxBeginner
	// ItemErrorSvc routes a permanent Plaid error into the same in-app notification the ITEM
	// webhook path produces (Issue #1609 P3-T2). Optional — see plaid_permanent_error_notify.go.
	ItemErrorSvc ItemErrorNotifier
}

// NewInvestmentTransactionsSyncWorker creates a new worker with the given dependencies.
func NewInvestmentTransactionsSyncWorker(deps InvestmentTransactionsSyncWorkerDeps) *InvestmentTransactionsSyncWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &InvestmentTransactionsSyncWorker{
		investmentSyncSvc: deps.InvestmentSyncSvc,
		linkedItemRepo:    deps.LinkedItemRepo,
		logger:            logger,
		db:                deps.DB,
		itemErrorSvc:      deps.ItemErrorSvc,
	}
}

// Work processes an investment transactions sync job.
//
// The worker acquires a row-level lock on the LinkedItem, calls SyncInvestmentTransactions
// which fetches all pages and upserts transactions, then releases the lock.
// Permanent Plaid errors are not retried; temporary errors return an error for retry.
func (w *InvestmentTransactionsSyncWorker) Work(ctx context.Context, job *river.Job[InvestmentTransactionsSyncArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting investment transactions sync job",
		"job_id", job.ID,
		"user_id", args.UserID,
		"item_id", args.ItemID,
		"attempt", job.Attempt,
	)

	startTime := time.Now()
	defer func() {
		status := "success"
		if err != nil {
			status = "failed"
		}
		observability.JobsCompleted.WithLabelValues(job.Kind, job.Queue, status).Inc()
		observability.JobDuration.WithLabelValues(job.Kind).Observe(time.Since(startTime).Seconds())
	}()

	linkedItem, tx, lockErr := acquireLinkedItemLock(ctx, w.db, w.linkedItemRepo, args.ItemID, args.UserID)
	if lockErr != nil {
		if errors.Is(lockErr, repository.ErrLinkedItemNotFound) {
			w.logger.Warn("linked item not found, skipping investment transactions sync",
				"item_id", args.ItemID,
				"user_id", args.UserID,
			)
			return nil
		}
		if errors.Is(lockErr, errOwnershipMismatch) {
			w.logger.Warn("linked item ownership mismatch, skipping investment transactions sync",
				"item_id", args.ItemID,
				"user_id", args.UserID,
			)
			return nil
		}
		return fmt.Errorf("failed to acquire lock on linked item: %w", lockErr)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx) // No-op if already committed
		}
	}()

	// Issue #1609 Item 7: release the lock BEFORE the sync, not after. This tx takes
	// only a `SELECT ... FOR UPDATE` row lock and does no writes, so committing it after
	// the multi-page Plaid sync left the connection idle-in-transaction until Postgres
	// terminated it (SQLSTATE 25P03 — a production DLQ entry burned all 5 attempts).
	// Safe because the webhook path takes no lock at all and worker-vs-worker
	// serialization already comes from River job uniqueness. See plaid_sync.go for the
	// full rationale; pinned by TestInvestmentTransactionsSyncWorker_LockTxCommitsBeforeSync
	// in issue1609_lock_tx_lifetime_test.go.
	if tx != nil {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return fmt.Errorf("failed to commit transaction: %w", commitErr)
		}
		tx = nil // the deferred Rollback above must not fire for a committed tx
	}

	result, err := w.investmentSyncSvc.SyncInvestmentTransactions(ctx, linkedItem)
	if err != nil {
		var plaidErr *domain.PlaidSyncError
		if !errors.As(err, &plaidErr) {
			plaidErr = domain.ClassifyPlaidError(err)
		}

		errorCode := "unknown"
		if plaidErr.ErrorCode != "" {
			errorCode = plaidErr.ErrorCode
		}
		observability.PlaidAPIErrors.WithLabelValues(plaidErrorTypeLabel(plaidErr.ErrorType), errorCode).Inc()

		w.logger.Error("investment transactions sync failed",
			"job_id", job.ID,
			"item_id", args.ItemID,
			"error", err,
			"error_type", plaidErr.ErrorType,
			"error_code", plaidErr.ErrorCode,
			"duration_ms", time.Since(startTime).Milliseconds(),
		)

		if plaidErr.ErrorType == domain.PlaidErrorPermanent {
			w.logger.Warn("permanent plaid error, not retrying investment transactions sync",
				"job_id", job.ID,
				"error_code", plaidErr.ErrorCode,
			)
			// Issue #1609 P3-T2: see notifyPermanentPlaidError — same handlers the ITEM webhook
			// path uses, non-blocking, excludes PRODUCTS_NOT_SUPPORTED.
			notifyPermanentPlaidError(ctx, w.itemErrorSvc, w.logger, linkedItem, plaidErr)
			return nil
		}

		return fmt.Errorf("investment transactions sync failed: %w", err)
	}

	// Issue #1609 Item 7: lock tx already committed above, before the sync ran.

	observability.InvestmentTransactionsSyncDuration.Observe(time.Since(startTime).Seconds())
	observability.InvestmentTransactionsSynced.WithLabelValues("inserted").Add(float64(result.TransactionsInserted))
	observability.InvestmentTransactionsSynced.WithLabelValues("skipped").Add(float64(result.TransactionsSkipped))

	// #1631 M4: the drop counters have to reach the metrics, not just the service's own log line.
	//
	// ⚠ THIS IS THE ONLY SIGNAL FOR A PINNED CURSOR. A dropped row makes the sync decline to
	// advance investments_last_synced_date, deliberately, so the next run re-fetches the same
	// window and can store what it missed. If the cause is permanent (a date Plaid will always send
	// malformed, a security it will never return) the item re-fetches its whole window forever — and
	// from this worker's perspective that is a SUCCESS: err is nil, we return nil, and the job is
	// green. Nothing else in the system would ever report it. Alert on a sustained non-zero rate.
	//
	// "excluded" is emitted too but means the opposite thing: the owner asked us not to store those
	// rows, the window was fully processed, and the watermark advanced normally. It is here to be
	// countable, not to be alerted on.
	observability.InvestmentTransactionsSynced.WithLabelValues("excluded").Add(float64(result.TransactionsExcluded))
	observability.InvestmentTransactionsSynced.WithLabelValues("dropped_unmapped_account").Add(float64(result.TransactionsDroppedUnmappedAccount))
	observability.InvestmentTransactionsSynced.WithLabelValues("dropped_unparsable_date").Add(float64(result.TransactionsDroppedUnparsableDate))
	observability.InvestmentTransactionsSynced.WithLabelValues("dropped_unresolved_security").Add(float64(result.TransactionsDroppedUnresolvedSecurity))

	w.logger.Info("investment transactions sync completed",
		"job_id", job.ID,
		"item_id", args.ItemID,
		"pages_processed", result.PagesProcessed,
		"transactions_inserted", result.TransactionsInserted,
		"transactions_skipped", result.TransactionsSkipped,
		"transactions_excluded", result.TransactionsExcluded,
		"backfilled_chains", result.BackfilledChains,
		"transactions_dropped_unmapped_account", result.TransactionsDroppedUnmappedAccount,
		"transactions_dropped_unparsable_date", result.TransactionsDroppedUnparsableDate,
		"transactions_dropped_unresolved_security", result.TransactionsDroppedUnresolvedSecurity,
		// watermark_advanced makes the consequence legible without the reader having to know that
		// any drop pins the cursor. A "completed" line with watermark_advanced=false is the shape
		// to grep for when an item's history stops moving.
		"watermark_advanced", !result.AnyTransactionDropped(),
		"securities_upserted", result.SecuritiesUpserted,
		"duration_ms", time.Since(startTime).Milliseconds(),
	)

	if result.AnyTransactionDropped() {
		// A second line at WARN, because the Info above is the one nobody reads. The sync service
		// logs the same fact at its own layer; this one is what a job-level alert or a dashboard
		// filtered to this worker will actually see.
		w.logger.Warn("investment transactions sync completed with DROPPED rows; "+
			"investments_last_synced_date was NOT advanced, so this item will re-fetch the same "+
			"window until the cause is fixed",
			"job_id", job.ID,
			"item_id", args.ItemID,
			"dropped_unmapped_account", result.TransactionsDroppedUnmappedAccount,
			"dropped_unparsable_date", result.TransactionsDroppedUnparsableDate,
			"dropped_unresolved_security", result.TransactionsDroppedUnresolvedSecurity,
		)
	}

	return nil
}

// InvestmentTransactionsSyncUniqueOpts prevents duplicate transaction syncs for the same item
// within a 5-minute window.
func InvestmentTransactionsSyncUniqueOpts() *queue.UniqueOpts {
	return investmentSyncUniqueOpts()
}
