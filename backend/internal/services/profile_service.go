package services

import (
	"context"
	"errors"
	"log/slog"
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
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Update last login timestamp (non-critical, log errors but don't fail request)
	_ = s.repo.UpdateLastLogin(ctx, user.ClerkUserID)

	return user, nil
}

// UpdateProfile updates a user's profile with validation.
// When the email changes, the update runs inside a transaction that atomically
// updates both the users table and user_identities table, then enqueues an
// asynchronous job to propagate the change to external systems (Clerk, Paddle).
// NOTE: After PR #197, this method receives internal user UUID, not Clerk ID
func (s *profileService) UpdateProfile(ctx context.Context, userID string, req *domain.UpdateProfileRequest) (*domain.User, error) {
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	emailChanged := req.Email != nil && !strings.EqualFold(*req.Email, user.Email)

	// Validate inputs
	if req.Email != nil {
		if err := validateEmail(*req.Email); err != nil {
			return nil, err
		}
	}
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

		// Re-fetch before any further updates so user.Email reflects the committed email.
		// Without this, UpdateUser below would overwrite the new email with the stale value.
		user, err = s.repo.GetUserByID(ctx, userID)
		if err != nil {
			return nil, err
		}

		// Update region outside email transaction (non-critical)
		if req.PrimaryRegion != nil && *req.PrimaryRegion != user.PrimaryRegion {
			user.PrimaryRegion = *req.PrimaryRegion
			if err := s.repo.UpdateUser(ctx, user); err != nil {
				return nil, err
			}
		}

		// Enqueue async propagation to external systems (Clerk, Paddle).
		// Fire-and-forget: the DB is already committed; a failed enqueue is logged but
		// does not fail the request (River has its own persistence and retry logic).
		if s.jobQueue != nil {
			if _, err := s.jobQueue.Enqueue(ctx, emailChangePropagationArgs{
				UserID:      userID,
				ClerkUserID: user.ClerkUserID,
			}); err != nil {
				slog.Default().Error("failed to enqueue email propagation job", "user_id", userID, "error", err)
			}
		}
	} else {
		// No email change — simple non-transactional update
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
	if _, err := mail.ParseAddress(email); err != nil {
		return ErrInvalidEmail
	}
	return nil
}

// validateRegion validates a primary region value against the configured region registry
func validateRegion(region string) error {
	if !domain.IsValidRegion(region) {
		return ErrInvalidRegion
	}
	return nil
}
