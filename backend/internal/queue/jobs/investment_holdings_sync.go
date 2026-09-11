package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/riverqueue/river"
)

// InvestmentHoldingsSyncArgs contains the arguments for an investment holdings sync job.
// Triggered by the HOLDINGS webhook; fetches a full snapshot and replaces holdings per account.
// Issue #659: Plaid Investments — Holdings, Securities, Investment Transactions
type InvestmentHoldingsSyncArgs struct {
	// UserID is the ID of the user who owns the linked item.
	UserID string `json:"user_id"`

	// ItemID is the internal UUID of the LinkedItem to sync (not the Plaid item_id).
	ItemID string `json:"item_id"`

	// WebhookID optionally tracks which webhook triggered this sync.
	WebhookID string `json:"webhook_id,omitempty"`
}

// Kind returns the unique identifier for this job type.
func (InvestmentHoldingsSyncArgs) Kind() string { return "investment_holdings_sync" }

// InvestmentHoldingsSyncWorker processes investment holdings sync jobs by fetching
// the complete holdings snapshot from Plaid and atomically replacing holdings per account.
type InvestmentHoldingsSyncWorker struct {
	river.WorkerDefaults[InvestmentHoldingsSyncArgs]

	investmentSyncSvc services.InvestmentSyncService
	linkedItemRepo    repository.LinkedItemRepository
	logger            *slog.Logger
	db                TxBeginner
	itemErrorSvc      ItemErrorNotifier // Issue #1609 P3-T2: nil-safe, notifies on permanent errors
}

// InvestmentHoldingsSyncWorkerDeps contains the dependencies for InvestmentHoldingsSyncWorker.
type InvestmentHoldingsSyncWorkerDeps struct {
	InvestmentSyncSvc services.InvestmentSyncService
	LinkedItemRepo    repository.LinkedItemRepository
	Logger            *slog.Logger
	DB                TxBeginner
	// ItemErrorSvc routes a permanent Plaid error into the same in-app notification the ITEM
	// webhook path produces (Issue #1609 P3-T2). Optional — see plaid_permanent_error_notify.go.
	ItemErrorSvc ItemErrorNotifier
}

// NewInvestmentHoldingsSyncWorker creates a new worker with the given dependencies.
func NewInvestmentHoldingsSyncWorker(deps InvestmentHoldingsSyncWorkerDeps) *InvestmentHoldingsSyncWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &InvestmentHoldingsSyncWorker{
		investmentSyncSvc: deps.InvestmentSyncSvc,
		linkedItemRepo:    deps.LinkedItemRepo,
		logger:            logger,
		db:                deps.DB,
		itemErrorSvc:      deps.ItemErrorSvc,
	}
}

// Work processes an investment holdings sync job.
//
// The worker acquires a row-level lock on the LinkedItem, calls SyncHoldings which
// atomically replaces all holdings per account, then releases the lock.
// Permanent Plaid errors are not retried; temporary errors return an error for retry.
func (w *InvestmentHoldingsSyncWorker) Work(ctx context.Context, job *river.Job[InvestmentHoldingsSyncArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting investment holdings sync job",
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
			w.logger.Warn("linked item not found, skipping investment holdings sync",
				"item_id", args.ItemID,
				"user_id", args.UserID,
			)
			return nil
		}
		if errors.Is(lockErr, errOwnershipMismatch) {
			w.logger.Warn("linked item ownership mismatch, skipping investment holdings sync",
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
	// Safe because the webhook path takes no lock at all. See plaid_sync.go for the
	// full rationale; pinned by TestInvestmentHoldingsSyncWorker_LockTxCommitsBeforeSync
	// in issue1609_lock_tx_lifetime_test.go.
	//
	// ⚠ This comment used to add "and worker-vs-worker serialization already comes from River job
	// uniqueness". That was FALSE for the path that matters and it cost a production DLQ entry
	// (2026-09-11: five identical `unique_account_security` violations). webhook_processing's
	// uniqueness is ByArgs and its args carry webhook_id, so two HOLDINGS webhooks for one item
	// are two distinct jobs River will happily run at once — and that path holds no lock. Nothing
	// here serializes two concurrent holdings replaces of one account. That is now enforced where
	// the statements are, by the per-account pg_advisory_xact_lock in
	// holdingRepository.ReplaceForAccount, which is the only place every caller passes through.
	if tx != nil {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return fmt.Errorf("failed to commit transaction: %w", commitErr)
		}
		tx = nil // the deferred Rollback above must not fire for a committed tx
	}

	result, err := w.investmentSyncSvc.SyncHoldings(ctx, linkedItem)
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

		w.logger.Error("investment holdings sync failed",
			"job_id", job.ID,
			"item_id", args.ItemID,
			"error", err,
			"error_type", plaidErr.ErrorType,
			"error_code", plaidErr.ErrorCode,
			"duration_ms", time.Since(startTime).Milliseconds(),
		)

		if plaidErr.ErrorType == domain.PlaidErrorPermanent {
			w.logger.Warn("permanent plaid error, not retrying investment holdings sync",
				"job_id", job.ID,
				"error_code", plaidErr.ErrorCode,
			)
			// Issue #1609 P3-T2: see notifyPermanentPlaidError — same handlers the ITEM webhook
			// path uses, non-blocking, excludes PRODUCTS_NOT_SUPPORTED.
			notifyPermanentPlaidError(ctx, w.itemErrorSvc, w.logger, linkedItem, plaidErr)
			return nil
		}

		return fmt.Errorf("investment holdings sync failed: %w", err)
	}

	// Issue #1609 Item 7: lock tx already committed above, before the sync ran.

	observability.InvestmentHoldingsSyncDuration.Observe(time.Since(startTime).Seconds())
	observability.InvestmentHoldingsSynced.WithLabelValues("replaced").Add(float64(result.HoldingsReplaced))

	w.logger.Info("investment holdings sync completed",
		"job_id", job.ID,
		"item_id", args.ItemID,
		"securities_upserted", result.SecuritiesUpserted,
		"holdings_replaced", result.HoldingsReplaced,
		"accounts_processed", result.AccountsProcessed,
		"duration_ms", time.Since(startTime).Milliseconds(),
	)

	return nil
}

// investmentSyncUniqueOpts returns the standard uniqueness options for investment sync jobs.
// Prevents duplicate syncs for the same item within a 5-minute window.
func investmentSyncUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs:   true,
		ByPeriod: 5 * time.Minute,
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
		},
	}
}

// InvestmentHoldingsSyncUniqueOpts prevents duplicate holdings syncs for the same item
// within a 5-minute window.
func InvestmentHoldingsSyncUniqueOpts() *queue.UniqueOpts {
	return investmentSyncUniqueOpts()
}

// acquireLinkedItemLock starts a transaction (if db is non-nil), acquires an exclusive
// row-level lock on the linked item, and validates ownership.
// The returned transaction MUST be committed or rolled back by the caller.
func acquireLinkedItemLock(ctx context.Context, db TxBeginner, linkedItemRepo repository.LinkedItemRepository, itemID, userID string) (*domain.LinkedItem, pgx.Tx, error) {
	if db == nil {
		item, err := linkedItemRepo.GetByIDAndUser(ctx, itemID, userID)
		return item, nil, err
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	item, err := linkedItemRepo.GetByIDForUpdate(ctx, tx, itemID)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, nil, err
	}

	if item.UserID != userID {
		_ = tx.Rollback(ctx)
		return nil, nil, errOwnershipMismatch
	}

	return item, tx, nil
}
