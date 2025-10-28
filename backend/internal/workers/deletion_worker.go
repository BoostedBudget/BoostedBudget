package workers

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// DeletionWorker handles periodic processing of scheduled account deletions
type DeletionWorker struct {
	deletionService services.DeletionService
	profileRepo     repository.ProfileRepository
	interval        time.Duration
}

// NewDeletionWorker creates a new DeletionWorker instance
func NewDeletionWorker(
	deletionService services.DeletionService,
	profileRepo repository.ProfileRepository,
	interval time.Duration,
) *DeletionWorker {
	return &DeletionWorker{
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
		log.Printf("Error fetching users scheduled for deletion: %v", err)
		return fmt.Errorf("failed to fetch users scheduled for deletion: %w", err)
	}

	if len(users) == 0 {
		// No users to process - not an error
		return nil
	}

	log.Printf("Found %d users scheduled for deletion", len(users))

	// Process each user
	for _, user := range users {
		// Check context cancellation
		select {
		case <-ctx.Done():
			log.Printf("Context cancelled, stopping deletion processing")
			return nil
		default:
			// Continue processing
		}

		// Execute deletion for this user
		if err := w.deletionService.ExecuteDeletion(ctx, user.ID); err != nil {
			// Log error but continue processing other users
			log.Printf("Error deleting user %s: %v", user.ID, err)
			// Don't return error - we want to continue processing
			continue
		}

		log.Printf("Successfully deleted user %s", user.ID)
	}

	return nil
}

// Start begins the worker's periodic deletion processing
// Runs until context is cancelled
func (w *DeletionWorker) Start(ctx context.Context) {
	log.Printf("Deletion worker starting with interval: %s", w.interval)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	// Check if context is already cancelled before first run
	select {
	case <-ctx.Done():
		log.Printf("Deletion worker shutting down gracefully (context cancelled before start)")
		return
	default:
		// Run once immediately on startup
		if err := w.ProcessScheduledDeletions(ctx); err != nil {
			log.Printf("Error processing scheduled deletions on startup: %v", err)
		}
	}

	// Then run periodically
	for {
		select {
		case <-ctx.Done():
			log.Printf("Deletion worker shutting down gracefully")
			return
		case <-ticker.C:
			if err := w.ProcessScheduledDeletions(ctx); err != nil {
				log.Printf("Error processing scheduled deletions: %v", err)
			}
		}
	}
}
