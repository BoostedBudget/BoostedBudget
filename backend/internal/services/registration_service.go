package services

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// Email validation regex (basic RFC 5322 compliant)
var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// RegistrationService handles user registration and onboarding
type RegistrationService interface {
	// RegisterUser creates a new user account or returns existing user (idempotent)
	// Creates user profile, identity, and privacy settings in a single operation
	RegisterUser(ctx context.Context, auth0UserID, email string) (*domain.User, error)
}

type registrationService struct {
	profileRepo  repository.ProfileRepository
	identityRepo repository.UserIdentityRepository
	privacyRepo  repository.PrivacyRepository
}

// NewRegistrationService creates a new RegistrationService
func NewRegistrationService(
	profileRepo repository.ProfileRepository,
	identityRepo repository.UserIdentityRepository,
	privacyRepo repository.PrivacyRepository,
) RegistrationService {
	return &registrationService{
		profileRepo:  profileRepo,
		identityRepo: identityRepo,
		privacyRepo:  privacyRepo,
	}
}

// RegisterUser implements idempotent user registration
// If user already exists (identity found), returns existing user
// If user doesn't exist, creates user + identity + privacy settings
func (s *registrationService) RegisterUser(ctx context.Context, auth0UserID, email string) (*domain.User, error) {
	// Validate required fields
	if auth0UserID == "" {
		return nil, errors.New("auth0_user_id is required")
	}

	// Validate email format
	if !emailRegex.MatchString(email) {
		return nil, fmt.Errorf("invalid email format: %s", email)
	}

	// Check if user already exists (idempotent)
	existingIdentity, err := s.identityRepo.GetByAuth0ID(ctx, auth0UserID)
	if err != nil && !errors.Is(err, repository.ErrIdentityNotFound) {
		// Unexpected error (not "not found")
		return nil, fmt.Errorf("failed to check existing identity: %w", err)
	}

	if existingIdentity != nil {
		// User already exists - return existing user (idempotent behavior)
		user, err := s.profileRepo.GetUserByID(ctx, existingIdentity.UserID)
		if err != nil {
			return nil, fmt.Errorf("failed to get existing user: %w", err)
		}
		return user, nil
	}

	// New user - create user, identity, and privacy settings
	// Step 1: Create user profile
	user := domain.NewUser(auth0UserID, email)
	err = s.profileRepo.CreateUser(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	// Step 2: Create user identity
	// First identity is always primary; assume email verified from Auth0
	identity := domain.NewUserIdentity(user.ID, auth0UserID, email, true, true)
	err = s.identityRepo.Create(ctx, identity)
	if err != nil {
		return nil, fmt.Errorf("failed to create identity: %w", err)
	}

	// Step 3: Create privacy settings with GDPR defaults
	privacy := domain.NewPrivacySettings(user.ID)
	err = s.privacyRepo.Create(ctx, privacy)
	if err != nil {
		return nil, fmt.Errorf("failed to create privacy settings: %w", err)
	}

	return user, nil
}
