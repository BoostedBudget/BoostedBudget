package services

import (
	"context"
	"errors"
	"net/mail"
	"strings"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// emailChangePropagationArgs is a local struct for enqueueing email change propagation jobs.
// This mirrors jobs.EmailChangePropagationArgs to avoid import cycle (services → jobs → services).
// The JSON structure must match jobs.EmailChangePropagationArgs exactly for River to deserialize correctly.
type emailChangePropagationArgs struct {
	UserID      string `json:"user_id"`
	ClerkUserID string `json:"clerk_user_id"`
}

// Kind returns the job type identifier. Must match jobs.EmailChangePropagationArgs.Kind()
func (emailChangePropagationArgs) Kind() string { return "email_change_propagation" }

var (
	// ErrInvalidEmail is returned when an email address is invalid
	ErrInvalidEmail = errors.New("invalid email address")
	// ErrInvalidRegion is returned when a primary region is invalid
	ErrInvalidRegion = errors.New("invalid primary region")
	// ErrForbiddenUpdate is returned when attempting to update forbidden fields
	ErrForbiddenUpdate = errors.New("cannot update subscription fields")
)


// ProfileService defines the interface for user profile business logic
type ProfileService interface {
	// GetProfile retrieves a user's profile by their internal user UUID
	// If the user doesn't exist, creates a new user (first login scenario)
	// NOTE: After PR #197, this accepts internal UUID, not Clerk ID
	GetProfile(ctx context.Context, userID string, email string) (*domain.User, error)

	// UpdateProfile updates a user's profile with validated data
	// Only allows updating email and primary_region fields
	// NOTE: After PR #197, this accepts internal UUID, not Clerk ID
	UpdateProfile(ctx context.Context, userID string, req *domain.UpdateProfileRequest) (*domain.User, error)

	// SetJobQueue sets the job queue for email change propagation.
	// This is needed because of circular dependency: service must be created before
	// River client, but needs client to enqueue jobs.
	SetJobQueue(jq queue.JobQueue)
}

// profileService implements ProfileService
type profileService struct {
	repo         repository.ProfileRepository
	identityRepo repository.UserIdentityRepository
	db           TxBeginner
	jobQueue     queue.JobQueue
}

// NewProfileServiceDeps contains dependencies for NewProfileService.
type NewProfileServiceDeps struct {
	Repo         repository.ProfileRepository
	IdentityRepo repository.UserIdentityRepository
	DB           TxBeginner
	JobQueue     queue.JobQueue
}

// NewProfileService creates a new ProfileService instance
func NewProfileService(deps NewProfileServiceDeps) ProfileService {
	return &profileService{
		repo:         deps.Repo,
		identityRepo: deps.IdentityRepo,
		db:           deps.DB,
		jobQueue:     deps.JobQueue,
	}
}

// SetJobQueue sets the job queue for email change propagation.
func (s *profileService) SetJobQueue(jq queue.JobQueue) {
	s.jobQueue = jq
}

// GetProfile retrieves a user's profile or creates one if it doesn't exist
// NOTE: After PR #197, this method receives internal user UUID, not Clerk ID
func (s *profileService) GetProfile(ctx context.Context, userID string, email string) (*domain.User, error) {
	// Try to get existing user by internal UUID
	user, err := s.repo.GetUserByID(ctx, userID)

	if err == repository.ErrUserNotFound {
		// User doesn't exist - this shouldn't happen since Clerk middleware
		// already validated the user exists in user_identities table
		// Return error instead of creating (creation handled during first Clerk login)
		return nil, repository.ErrUserNotFound
	}

	if err != nil {
		return nil, err
	}

	// Update last login timestamp (use Clerk ID from user record)
	if err := s.repo.UpdateLastLogin(ctx, user.ClerkUserID); err != nil {
		// Log error but don't fail the request
		// In production, you'd use a proper logger here
	}

	return user, nil
}

// UpdateProfile updates a user's profile with validation.
// When the email changes, the update runs inside a transaction that atomically
// updates both the users table and user_identities table, then enqueues an
// asynchronous job to propagate the change to external systems (Clerk, Paddle).
// NOTE: After PR #197, this method receives internal user UUID, not Clerk ID
func (s *profileService) UpdateProfile(ctx context.Context, userID string, req *domain.UpdateProfileRequest) (*domain.User, error) {
	// Get existing user by internal UUID
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	emailChanged := req.Email != nil && !strings.EqualFold(*req.Email, user.Email)

	// Validate email if provided
	if req.Email != nil {
		if err := validateEmail(*req.Email); err != nil {
			return nil, err
		}
	}

	// Validate region if provided
	if req.PrimaryRegion != nil {
		if err := validateRegion(*req.PrimaryRegion); err != nil {
			return nil, err
		}
	}

	if emailChanged {
		tx, err := s.db.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer func() { _ = tx.Rollback(ctx) }()

		if err := s.repo.UpdateUserEmailTx(ctx, tx, userID, *req.Email); err != nil {
			return nil, err
		}
		if err := s.identityRepo.UpdatePrimaryIdentityEmailTx(ctx, tx, userID, *req.Email); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}

		// Handle region update outside the email transaction (non-critical path)
		if req.PrimaryRegion != nil && *req.PrimaryRegion != user.PrimaryRegion {
			user.PrimaryRegion = *req.PrimaryRegion
			if err := s.repo.UpdateUser(ctx, user); err != nil {
				return nil, err
			}
		}

		// Enqueue async propagation to external systems (Clerk, Paddle)
		_, _ = s.jobQueue.Enqueue(ctx, emailChangePropagationArgs{
			UserID:      userID,
			ClerkUserID: user.ClerkUserID,
		})

		// Re-fetch user to get the committed state (email_verified reset to false, etc.)
		user, err = s.repo.GetUserByID(ctx, userID)
		if err != nil {
			return nil, err
		}
	} else {
		// No email change — apply updates via the simple UpdateUser path
		if req.Email != nil {
			user.Email = *req.Email
		}
		if req.PrimaryRegion != nil {
			user.PrimaryRegion = *req.PrimaryRegion
		}
		if err := s.repo.UpdateUser(ctx, user); err != nil {
			return nil, err
		}
	}

	return user, nil
}

// validateEmail validates an email address format
func validateEmail(email string) error {
	_, err := mail.ParseAddress(email)
	if err != nil {
		return ErrInvalidEmail
	}
	return nil
}

// validateRegion validates a primary region value against the configured region registry.
func validateRegion(region string) error {
	if !domain.IsValidRegion(region) {
		return ErrInvalidRegion
	}
	return nil
}
