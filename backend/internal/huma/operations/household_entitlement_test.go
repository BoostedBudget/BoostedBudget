package operations

import (
	"context"
	"errors"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockProfileRepoForEntitlement struct{ mock.Mock }

func (m *mockProfileRepoForEntitlement) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

type mockEntitlementService struct{ mock.Mock }

func (m *mockEntitlementService) GetEntitlementSummary(ctx context.Context, user *domain.User) (domain.HouseholdEntitlementSummary, error) {
	args := m.Called(ctx, user)
	return args.Get(0).(domain.HouseholdEntitlementSummary), args.Error(1)
}

func TestGetHouseholdEntitlement_Success(t *testing.T) {
	profileRepo := &mockProfileRepoForEntitlement{}
	user := &domain.User{ID: "user-1", SubscriptionTier: "premium"}
	profileRepo.On("GetUserByID", mock.Anything, "user-1").Return(user, nil)

	entService := &mockEntitlementService{}
	entService.On("GetEntitlementSummary", mock.Anything, user).Return(domain.HouseholdEntitlementSummary{
		Tier: "premium", UsersIncluded: 1, Source: domain.EntitlementSourceOwn,
	}, nil)

	handler := NewHouseholdEntitlementHandler(entService, profileRepo)
	out, err := handler.GetHouseholdEntitlement(context.Background(), &GetHouseholdEntitlementInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
	})
	require.NoError(t, err)
	assert.Equal(t, "premium", out.Body.Tier)
	assert.Equal(t, domain.EntitlementSourceOwn, out.Body.Source)
}

func TestGetHouseholdEntitlement_ProfileLookupError(t *testing.T) {
	profileRepo := &mockProfileRepoForEntitlement{}
	profileRepo.On("GetUserByID", mock.Anything, "user-1").Return(nil, errors.New("db error"))

	handler := NewHouseholdEntitlementHandler(&mockEntitlementService{}, profileRepo)
	_, err := handler.GetHouseholdEntitlement(context.Background(), &GetHouseholdEntitlementInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
	})
	require.Error(t, err)
}

func TestGetHouseholdEntitlement_ServiceError(t *testing.T) {
	profileRepo := &mockProfileRepoForEntitlement{}
	user := &domain.User{ID: "user-1", SubscriptionTier: "premium"}
	profileRepo.On("GetUserByID", mock.Anything, "user-1").Return(user, nil)

	entService := &mockEntitlementService{}
	entService.On("GetEntitlementSummary", mock.Anything, user).Return(domain.HouseholdEntitlementSummary{}, errors.New("service error"))

	handler := NewHouseholdEntitlementHandler(entService, profileRepo)
	_, err := handler.GetHouseholdEntitlement(context.Background(), &GetHouseholdEntitlementInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
	})
	require.Error(t, err)
}
