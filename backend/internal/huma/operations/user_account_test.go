// Package operations provides HTTP handler tests for user account operations.
// Issue #171: Stable user account references that survive Plaid re-links.
package operations

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockUserAccountService is a mock implementation of services.UserAccountService.
type mockUserAccountService struct {
	mock.Mock
}

func (m *mockUserAccountService) MatchOrCreate(
	ctx context.Context,
	userID, institutionID string,
	mask *string,
	accountName, accountType, accountSubtype string,
) (*domain.UserAccount, error) {
	args := m.Called(ctx, userID, institutionID, mask, accountName, accountType, accountSubtype)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.UserAccount), args.Error(1)
}

func (m *mockUserAccountService) GetByUserID(ctx context.Context, userID string, includeHidden bool) ([]domain.UserAccount, error) {
	args := m.Called(ctx, userID, includeHidden)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.UserAccount), args.Error(1)
}

func (m *mockUserAccountService) GetByID(ctx context.Context, id, userID string) (*domain.UserAccount, error) {
	args := m.Called(ctx, id, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.UserAccount), args.Error(1)
}

func (m *mockUserAccountService) UpdateCustomization(
	ctx context.Context,
	id, userID string,
	req *domain.UpdateUserAccountRequest,
) (*domain.UserAccount, error) {
	args := m.Called(ctx, id, userID, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.UserAccount), args.Error(1)
}

func (m *mockUserAccountService) SetHidden(ctx context.Context, userAccountID, userID string, hidden bool) error {
	args := m.Called(ctx, userAccountID, userID, hidden)
	return args.Error(0)
}

// SetInvestmentBackfillEnqueuer is a no-op in this mock: the #1631 M4 nudge it wires is optional
// and nil-safe by design, so no test here needs to observe it. The durable half of the backfill
// obligation is a SQL write, not this setter.
func (m *mockUserAccountService) SetInvestmentBackfillEnqueuer(jobQueue queue.JobQueue, accountRepo repository.AccountRepository, logger *slog.Logger) {
}

// ===========================================================================
// NewUserAccountHandler
// ===========================================================================

// TestNewUserAccountHandler_ReturnsNonNil verifies the constructor returns a valid handler.
func TestNewUserAccountHandler_ReturnsNonNil(t *testing.T) {
	svc := new(mockUserAccountService)
	handler := NewUserAccountHandler(svc)
	require.NotNil(t, handler, "NewUserAccountHandler must return non-nil handler")
	assert.Equal(t, svc, handler.service)
}

// ===========================================================================
// GetUserAccounts handler
// ===========================================================================

// TestUserAccountHandler_GetUserAccounts_ReturnsAccounts verifies the list handler.
func TestUserAccountHandler_GetUserAccounts_ReturnsAccounts(t *testing.T) {
	svc := new(mockUserAccountService)
	userID := "user-abc"
	accounts := []domain.UserAccount{
		{ID: "ua-1", UserID: userID, InstitutionID: "ins-1"},
		{ID: "ua-2", UserID: userID, InstitutionID: "ins-1"},
	}
	svc.On("GetByUserID", mock.Anything, userID, false).Return(accounts, nil)

	handler := NewUserAccountHandler(svc)
	input := &GetUserAccountsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		IncludeHidden:      false,
	}

	result, err := handler.GetUserAccounts(context.Background(), input)

	require.NoError(t, err)
	assert.Len(t, result.Body, 2)
	assert.Equal(t, "ua-1", result.Body[0].ID)
	svc.AssertExpectations(t)
}

// TestUserAccountHandler_GetUserAccounts_EmptyList verifies nil returns empty array.
func TestUserAccountHandler_GetUserAccounts_EmptyList(t *testing.T) {
	svc := new(mockUserAccountService)
	userID := "user-abc"
	svc.On("GetByUserID", mock.Anything, userID, false).Return(nil, nil)

	handler := NewUserAccountHandler(svc)
	input := &GetUserAccountsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		IncludeHidden:      false,
	}

	result, err := handler.GetUserAccounts(context.Background(), input)

	require.NoError(t, err)
	assert.NotNil(t, result.Body, "empty list must not be nil")
	assert.Empty(t, result.Body)
}

// TestUserAccountHandler_GetUserAccounts_ServiceError verifies error propagation.
func TestUserAccountHandler_GetUserAccounts_ServiceError(t *testing.T) {
	svc := new(mockUserAccountService)
	userID := "user-abc"
	svc.On("GetByUserID", mock.Anything, userID, false).Return(nil, errors.New("db error"))

	handler := NewUserAccountHandler(svc)
	input := &GetUserAccountsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
	}

	_, err := handler.GetUserAccounts(context.Background(), input)
	require.Error(t, err)
}

// ===========================================================================
// GetUserAccountByID handler
// ===========================================================================

// TestUserAccountHandler_GetUserAccountByID_ReturnsAccount verifies single account lookup.
func TestUserAccountHandler_GetUserAccountByID_ReturnsAccount(t *testing.T) {
	svc := new(mockUserAccountService)
	userID := "user-abc"
	accountID := "ua-1"
	account := &domain.UserAccount{ID: accountID, UserID: userID, InstitutionID: "ins-1"}
	svc.On("GetByID", mock.Anything, accountID, userID).Return(account, nil)

	handler := NewUserAccountHandler(svc)
	input := &GetUserAccountByIDInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 accountID,
	}

	result, err := handler.GetUserAccountByID(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, result.Body)
	assert.Equal(t, accountID, result.Body.ID)
	svc.AssertExpectations(t)
}

// TestUserAccountHandler_GetUserAccountByID_NotFound verifies 404 propagation.
func TestUserAccountHandler_GetUserAccountByID_NotFound(t *testing.T) {
	svc := new(mockUserAccountService)
	userID := "user-abc"
	accountID := "nonexistent"
	svc.On("GetByID", mock.Anything, accountID, userID).Return(nil, errors.New("not found"))

	handler := NewUserAccountHandler(svc)
	input := &GetUserAccountByIDInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 accountID,
	}

	_, err := handler.GetUserAccountByID(context.Background(), input)
	require.Error(t, err)
}

// ===========================================================================
// UpdateUserAccount handler
// ===========================================================================

// TestUserAccountHandler_UpdateUserAccount_UpdatesAccount verifies the update handler.
func TestUserAccountHandler_UpdateUserAccount_UpdatesAccount(t *testing.T) {
	svc := new(mockUserAccountService)
	userID := "user-abc"
	accountID := "ua-1"
	nickname := "My Checking"
	req := domain.UpdateUserAccountRequest{Nickname: &nickname}
	updated := &domain.UserAccount{ID: accountID, UserID: userID, InstitutionID: "ins-1", Nickname: &nickname}

	svc.On("UpdateCustomization", mock.Anything, accountID, userID, &req).Return(updated, nil)

	handler := NewUserAccountHandler(svc)
	input := &UpdateUserAccountInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 accountID,
		Body:               req,
	}

	result, err := handler.UpdateUserAccount(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, result.Body)
	assert.Equal(t, accountID, result.Body.ID)
	assert.Equal(t, &nickname, result.Body.Nickname)
	svc.AssertExpectations(t)
}

// TestUserAccountHandler_UpdateUserAccount_ServiceError verifies error propagation on update.
func TestUserAccountHandler_UpdateUserAccount_ServiceError(t *testing.T) {
	svc := new(mockUserAccountService)
	userID := "user-abc"
	accountID := "ua-1"
	req := domain.UpdateUserAccountRequest{}
	svc.On("UpdateCustomization", mock.Anything, accountID, userID, &req).Return(nil, errors.New("update failed"))

	handler := NewUserAccountHandler(svc)
	input := &UpdateUserAccountInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 accountID,
		Body:               req,
	}

	_, err := handler.UpdateUserAccount(context.Background(), input)
	require.Error(t, err)
}

// ===========================================================================
// Priority 4: Register*Routes functions — zero coverage
// ===========================================================================

// TestRegisterUserAccountRoutes_DoesNotPanic verifies that registering user account
// routes with a valid handler does not panic.
func TestRegisterUserAccountRoutes_DoesNotPanic(t *testing.T) {
	_, api := humatest.New(t)
	svc := new(mockUserAccountService)
	handler := NewUserAccountHandler(svc)
	require.NotPanics(t, func() {
		RegisterUserAccountRoutes(api, handler)
	}, "RegisterUserAccountRoutes must not panic")
}
