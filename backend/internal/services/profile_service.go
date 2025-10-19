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

// Valid regions for data residency
var validRegions = map[string]bool{
	"us-east-1":      true,
	"eu-west-1":      true,
	"ap-southeast-1": true,
}

// ProfileService defines the interface for user profile business logic
type ProfileService interface {
	// GetProfile retrieves a user's profile by their Auth0 user ID
	// If the user doesn't exist, creates a new user (first login scenario)
	GetProfile(ctx context.Context, auth0UserID string, email string) (*domain.User, error)

	// UpdateProfile updates a user's profile with validated data
	// Only allows updating email and primary_region fields
	UpdateProfile(ctx context.Context, auth0UserID string, req *domain.UpdateProfileRequest) (*domain.User, error)
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
func (s *profileService) GetProfile(ctx context.Context, auth0UserID string, email string) (*domain.User, error) {
	// Try to get existing user
	user, err := s.repo.GetUserByAuth0ID(ctx, auth0UserID)

	if err == repository.ErrUserNotFound {
		// First login - create new user with defaults
		newUser := domain.NewUser(auth0UserID, email)
		if err := s.repo.CreateUser(ctx, newUser); err != nil {
			return nil, err
		}
		return newUser, nil
	}

	if err != nil {
		return nil, err
	}

	// Update last login timestamp
	if err := s.repo.UpdateLastLogin(ctx, auth0UserID); err != nil {
		// Log error but don't fail the request
		// In production, you'd use a proper logger here
	}

	return user, nil
}

// UpdateProfile updates a user's profile with validation
func (s *profileService) UpdateProfile(ctx context.Context, auth0UserID string, req *domain.UpdateProfileRequest) (*domain.User, error) {
	// Get existing user
	user, err := s.repo.GetUserByAuth0ID(ctx, auth0UserID)
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
