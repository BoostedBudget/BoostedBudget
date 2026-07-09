package operations

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockHouseholdShareService implements services.HouseholdAccountShareService (#1368 —
// Phase 4) for operation-layer tests. Kept separate from mockHouseholdService (which
// implements the broad services.HouseholdService interface) — the narrow interface is
// the whole point of not adding these methods to HouseholdService.
type mockHouseholdShareService struct{ mock.Mock }

func (m *mockHouseholdShareService) SetAccountShareVisibility(ctx context.Context, callerUserID, userAccountID string, visibility domain.ShareVisibility) (*domain.HouseholdAccountShare, error) {
	args := m.Called(ctx, callerUserID, userAccountID, visibility)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.HouseholdAccountShare), args.Error(1)
}
func (m *mockHouseholdShareService) ListMyAccountShares(ctx context.Context, callerUserID string) ([]domain.UserAccountShareView, error) {
	args := m.Called(ctx, callerUserID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.UserAccountShareView), args.Error(1)
}

func TestSetHouseholdShareVisibility_Success(t *testing.T) {
	svc := &mockHouseholdShareService{}
	now := time.Now()
	share := &domain.HouseholdAccountShare{
		ID: "share-1", HouseholdID: "hh-1", UserAccountID: "acct-1",
		Visibility: domain.ShareVisibilityHidden, SharedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	svc.On("SetAccountShareVisibility", mock.Anything, "user-1", "acct-1", domain.ShareVisibilityHidden).Return(share, nil)

	handler := NewHouseholdShareHandler(svc)
	out, err := handler.SetHouseholdShareVisibility(context.Background(), &SetHouseholdShareVisibilityInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "acct-1",
		Body:               SetHouseholdShareVisibilityRequestBody{Visibility: "hidden"},
	})
	require.NoError(t, err)
	assert.Equal(t, "share-1", out.Body.ID)
	assert.Equal(t, domain.ShareVisibilityHidden, out.Body.Visibility)
}

func TestSetHouseholdShareVisibility_MapsNotInHousehold_404(t *testing.T) {
	svc := &mockHouseholdShareService{}
	svc.On("SetAccountShareVisibility", mock.Anything, "user-1", "acct-1", domain.ShareVisibilityShared).
		Return(nil, domain.ErrHouseholdNotFound)

	handler := NewHouseholdShareHandler(svc)
	_, err := handler.SetHouseholdShareVisibility(context.Background(), &SetHouseholdShareVisibilityInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "acct-1",
		Body:               SetHouseholdShareVisibilityRequestBody{Visibility: "shared"},
	})
	require.Error(t, err)
}

func TestSetHouseholdShareVisibility_MapsAccountNotOwned_403(t *testing.T) {
	svc := &mockHouseholdShareService{}
	svc.On("SetAccountShareVisibility", mock.Anything, "user-1", "acct-not-mine", domain.ShareVisibilityShared).
		Return(nil, domain.ErrAccountNotOwned)

	handler := NewHouseholdShareHandler(svc)
	_, err := handler.SetHouseholdShareVisibility(context.Background(), &SetHouseholdShareVisibilityInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "acct-not-mine",
		Body:               SetHouseholdShareVisibilityRequestBody{Visibility: "shared"},
	})
	require.Error(t, err)
}

func TestListMyAccountShares_Success(t *testing.T) {
	svc := &mockHouseholdShareService{}
	hidden := domain.ShareVisibilityHidden
	views := []domain.UserAccountShareView{
		{UserAccountID: "acct-1", Visibility: &hidden},
		{UserAccountID: "acct-2"},
	}
	svc.On("ListMyAccountShares", mock.Anything, "user-1").Return(views, nil)

	handler := NewHouseholdShareHandler(svc)
	out, err := handler.ListMyAccountShares(context.Background(), &ListMyAccountSharesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
	})
	require.NoError(t, err)
	require.Len(t, out.Body, 2)
	assert.Equal(t, "acct-1", out.Body[0].UserAccountID)
	assert.Nil(t, out.Body[1].Visibility)
}

func TestListMyAccountShares_MapsNotInHousehold_404(t *testing.T) {
	svc := &mockHouseholdShareService{}
	svc.On("ListMyAccountShares", mock.Anything, "user-1").Return(nil, domain.ErrHouseholdNotFound)

	handler := NewHouseholdShareHandler(svc)
	_, err := handler.ListMyAccountShares(context.Background(), &ListMyAccountSharesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
	})
	require.Error(t, err)
}
