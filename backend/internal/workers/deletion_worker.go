package workers

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// DeletionWorker handles periodic processing of scheduled account deletions
type DeletionWorker struct {
	logger          *slog.Logger
	deletionService services.DeletionService
	profileRepo     repository.ProfileRepository
	interval        time.Duration
}

// NewDeletionWorker creates a new DeletionWorker instance
func NewDeletionWorker(
	deletionService services.DeletionService,
	profileRepo repository.ProfileRepository,
	interval time.Duration,
	logger *slog.Logger,
) *DeletionWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &DeletionWorker{
		logger:          logger,
		deletionService: deletionService,
		profileRepo:     profileRepo,
		interval:        interval,
	}
}

// ProcessScheduledDeletions processes all users scheduled for deletion
// Errors are logged but don't stop processing of other users
func (w *DeletionWorker) ProcessScheduledDeletions(ctx context.Context) error {
	// Get users scheduled for deletion
	users, err := w.profileRepo.GetUsersScheduledForDeletion(ctx)
	if err != nil {
		w.logger.Error("error fetching users scheduled for deletion", "error", err)
		return fmt.Errorf("failed to fetch users scheduled for deletion: %w", err)
	}

	if len(users) == 0 {
		// No users to process - not an error
		return nil
	}

	w.logger.Info("found users scheduled for deletion", "count", len(users))

	// Process each user
	for _, user := range users {
		// Check context cancellation
		select {
		case <-ctx.Done():
			w.logger.Info("context cancelled, stopping deletion processing")
			return nil
		default:
			// Continue processing
		}

		// Execute deletion for this user
		if err := w.deletionService.ExecuteDeletion(ctx, user.ID); err != nil {
			// Log error but continue processing other users
			w.logger.Error("error deleting user", "user_id", user.ID, "error", err)
			// Don't return error - we want to continue processing
			continue
		}

		w.logger.Info("successfully deleted user", "user_id", user.ID)
	}

	return nil
}

// Start begins the worker's periodic deletion processing
// Runs until context is cancelled
func (w *DeletionWorker) Start(ctx context.Context) {
	w.logger.Info("deletion worker starting", "interval", w.interval)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	// Check if context is already cancelled before first run
	select {
	case <-ctx.Done():
		w.logger.Info("deletion worker shutting down gracefully (context cancelled before start)")
		return
	default:
		// Run once immediately on startup
		if err := w.ProcessScheduledDeletions(ctx); err != nil {
			w.logger.Error("error processing scheduled deletions on startup", "error", err)
		}
	}

	// Then run periodically
	for {
		select {
		case <-ctx.Done():
			w.logger.Info("deletion worker shutting down gracefully")
			return
		case <-ticker.C:
			if err := w.ProcessScheduledDeletions(ctx); err != nil {
				w.logger.Error("error processing scheduled deletions", "error", err)
			}
		}
	}
}
