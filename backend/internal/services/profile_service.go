package services

import (
	"context"
	"errors"
	"net/mail"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

var (
	// ErrInvalidEmail is returned when an email address is invalid
	ErrInvalidEmail = errors.New("invalid email address")
	// ErrInvalidRegion is returned when a primary region is invalid
	ErrInvalidRegion = errors.New("invalid primary region")
	// ErrForbiddenUpdate is returned when attempting to update forbidden fields
	ErrForbiddenUpdate = errors.New("cannot update subscription fields")
)

// Valid regions for data residency (Fly.io IATA codes)
// Uses the region validation from domain.Regions
var validRegions = map[string]bool{
	domain.RegionIAD: true, // iad - US East (Virginia)
	domain.RegionLHR: true, // lhr - UK (London)
	domain.RegionSYD: true, // syd - Australia (Sydney)
}

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
}

// profileService implements ProfileService
type profileService struct {
	repo repository.ProfileRepository
}

// NewProfileService creates a new ProfileService instance
func NewProfileService(repo repository.ProfileRepository) ProfileService {
	return &profileService{repo: repo}
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

// UpdateProfile updates a user's profile with validation
// NOTE: After PR #197, this method receives internal user UUID, not Clerk ID
func (s *profileService) UpdateProfile(ctx context.Context, userID string, req *domain.UpdateProfileRequest) (*domain.User, error) {
	// Get existing user by internal UUID
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Validate and apply email update
	if req.Email != nil {
		if err := validateEmail(*req.Email); err != nil {
			return nil, err
		}
		user.Email = *req.Email
	}

	// Validate and apply region update
	if req.PrimaryRegion != nil {
		if err := validateRegion(*req.PrimaryRegion); err != nil {
			return nil, err
		}
		user.PrimaryRegion = *req.PrimaryRegion
	}

	// Update user in database
	if err := s.repo.UpdateUser(ctx, user); err != nil {
		return nil, err
	}

	// TODO: Add audit logging here
	// s.auditLog.Log(ctx, "profile.update", user.ID, req)

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

// validateRegion validates a primary region value
func validateRegion(region string) error {
	if !validRegions[region] {
		return ErrInvalidRegion
	}
	return nil
}
