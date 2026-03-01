package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/signintoken"
)

// Sentinel errors for QAAuthBypassService.
var (
	ErrInvalidQARole   = errors.New("invalid QA role: must be 'admin' or 'premium'")
	ErrSignInTokenNoURL = errors.New("clerk sign-in token has no URL")
)

// SignInTokenCreator abstracts signintoken.Create for testability.
type SignInTokenCreator interface {
	Create(ctx context.Context, params *signintoken.CreateParams) (*clerk.SignInToken, error)
}

// QAAuthBypassResult holds the result of a successful sign-in token creation.
type QAAuthBypassResult struct {
	URL   string
	Email string
	Tier  string
}

// QAAuthBypassService creates Clerk sign-in tokens for pre-created QA test users.
type QAAuthBypassService interface {
	CreateSignInToken(ctx context.Context, role string) (*QAAuthBypassResult, error)
}

type qaAuthBypassService struct {
	adminClerkUserID   string
	premiumClerkUserID string
	tokenCreator       SignInTokenCreator
}

// defaultSignInTokenCreator wraps signintoken.Create for production use.
type defaultSignInTokenCreator struct{}

func (d *defaultSignInTokenCreator) Create(ctx context.Context, params *signintoken.CreateParams) (*clerk.SignInToken, error) {
	return signintoken.Create(ctx, params)
}

// NewQAAuthBypassService creates a QAAuthBypassService.
// If tokenCreator is nil, the default Clerk signintoken package is used.
func NewQAAuthBypassService(adminClerkUserID, premiumClerkUserID string, tokenCreator SignInTokenCreator) QAAuthBypassService {
	if tokenCreator == nil {
		tokenCreator = &defaultSignInTokenCreator{}
	}
	return &qaAuthBypassService{
		adminClerkUserID:   adminClerkUserID,
		premiumClerkUserID: premiumClerkUserID,
		tokenCreator:       tokenCreator,
	}
}

// CreateSignInToken creates a Clerk sign-in token for the given QA role.
func (s *qaAuthBypassService) CreateSignInToken(ctx context.Context, role string) (*QAAuthBypassResult, error) {
	var clerkUserID, email string
	switch role {
	case "admin":
		clerkUserID = s.adminClerkUserID
		email = "qa-admin@boostedbudget.test"
	case "premium":
		clerkUserID = s.premiumClerkUserID
		email = "qa-premium@boostedbudget.test"
	default:
		return nil, fmt.Errorf("%w: got %q", ErrInvalidQARole, role)
	}

	expiresIn := int64(300) // 5 minutes
	token, err := s.tokenCreator.Create(ctx, &signintoken.CreateParams{
		UserID:           &clerkUserID,
		ExpiresInSeconds: &expiresIn,
	})
	if err != nil {
		return nil, err
	}

	if token.URL == nil {
		return nil, ErrSignInTokenNoURL
	}

	return &QAAuthBypassResult{
		URL:   *token.URL,
		Email: email,
		Tier:  "premium",
	}, nil
}
