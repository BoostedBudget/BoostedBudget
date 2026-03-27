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
}

// InvestmentTransactionsSyncWorkerDeps contains the dependencies for InvestmentTransactionsSyncWorker.
type InvestmentTransactionsSyncWorkerDeps struct {
	InvestmentSyncSvc services.InvestmentSyncService
	LinkedItemRepo    repository.LinkedItemRepository
	Logger            *slog.Logger
	DB                TxBeginner
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
			"duration_ms", time.Since(startTime).Milliseconds(),
		)

		if plaidErr.ErrorType == domain.PlaidErrorPermanent {
			w.logger.Warn("permanent plaid error, not retrying investment transactions sync",
				"job_id", job.ID,
				"error_code", plaidErr.ErrorCode,
			)
			return nil
		}

		return fmt.Errorf("investment transactions sync failed: %w", err)
	}

	if tx != nil {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return fmt.Errorf("failed to commit transaction: %w", commitErr)
		}
	}

	observability.InvestmentTransactionsSyncDuration.Observe(time.Since(startTime).Seconds())
	observability.InvestmentTransactionsSynced.WithLabelValues("inserted").Add(float64(result.TransactionsInserted))
	observability.InvestmentTransactionsSynced.WithLabelValues("skipped").Add(float64(result.TransactionsSkipped))

	w.logger.Info("investment transactions sync completed",
		"job_id", job.ID,
		"item_id", args.ItemID,
		"pages_processed", result.PagesProcessed,
		"transactions_inserted", result.TransactionsInserted,
		"transactions_skipped", result.TransactionsSkipped,
		"securities_upserted", result.SecuritiesUpserted,
		"duration_ms", time.Since(startTime).Milliseconds(),
	)

	return nil
}

// InvestmentTransactionsSyncUniqueOpts prevents duplicate transaction syncs for the same item
// within a 5-minute window.
func InvestmentTransactionsSyncUniqueOpts() *queue.UniqueOpts {
	return investmentSyncUniqueOpts()
}
