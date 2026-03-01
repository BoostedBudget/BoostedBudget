package services

import (
	"context"
	"errors"
	"testing"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/signintoken"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSignInTokenCreator is a hand-written mock for testing.
type mockSignInTokenCreator struct {
	token *clerk.SignInToken
	err   error
}

func (m *mockSignInTokenCreator) Create(ctx context.Context, params *signintoken.CreateParams) (*clerk.SignInToken, error) {
	return m.token, m.err
}

func qaStrPtr(s string) *string { return &s }

func TestCreateSignInToken_AdminRole_Success(t *testing.T) {
	url := "https://clerk.example.com/sign-in-token?token=abc"
	mock := &mockSignInTokenCreator{
		token: &clerk.SignInToken{URL: qaStrPtr(url)},
	}
	svc := NewQAAuthBypassService("admin-clerk-id", "premium-clerk-id", mock)

	result, err := svc.CreateSignInToken(context.Background(), "admin")

	require.NoError(t, err)
	assert.Equal(t, url, result.URL)
	assert.Equal(t, "qa-admin@boostedbudget.com", result.Email)
	assert.Equal(t, "premium", result.Tier)
}

func TestCreateSignInToken_PremiumRole_Success(t *testing.T) {
	url := "https://clerk.example.com/sign-in-token?token=xyz"
	mock := &mockSignInTokenCreator{
		token: &clerk.SignInToken{URL: qaStrPtr(url)},
	}
	svc := NewQAAuthBypassService("admin-clerk-id", "premium-clerk-id", mock)

	result, err := svc.CreateSignInToken(context.Background(), "premium")

	require.NoError(t, err)
	assert.Equal(t, url, result.URL)
	assert.Equal(t, "qa-premium@boostedbudget.com", result.Email)
	assert.Equal(t, "premium", result.Tier)
}

func TestCreateSignInToken_InvalidRole_Error(t *testing.T) {
	mock := &mockSignInTokenCreator{}
	svc := NewQAAuthBypassService("admin-clerk-id", "premium-clerk-id", mock)

	_, err := svc.CreateSignInToken(context.Background(), "viewer")

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidQARole))
}

func TestCreateSignInToken_ClerkError(t *testing.T) {
	clerkErr := errors.New("clerk API unavailable")
	mock := &mockSignInTokenCreator{err: clerkErr}
	svc := NewQAAuthBypassService("admin-clerk-id", "premium-clerk-id", mock)

	_, err := svc.CreateSignInToken(context.Background(), "admin")

	require.Error(t, err)
	assert.ErrorIs(t, err, clerkErr)
}

func TestCreateSignInToken_NilURL(t *testing.T) {
	mock := &mockSignInTokenCreator{
		token: &clerk.SignInToken{URL: nil},
	}
	svc := NewQAAuthBypassService("admin-clerk-id", "premium-clerk-id", mock)

	_, err := svc.CreateSignInToken(context.Background(), "admin")

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrSignInTokenNoURL))
}
