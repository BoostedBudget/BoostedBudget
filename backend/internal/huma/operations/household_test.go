package operations

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockHouseholdService implements services.HouseholdService for operation-layer tests.
type mockHouseholdService struct{ mock.Mock }

func (m *mockHouseholdService) CreateHousehold(ctx context.Context, ownerUserID, name string) (*domain.Household, error) {
	args := m.Called(ctx, ownerUserID, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Household), args.Error(1)
}
func (m *mockHouseholdService) GetMyHousehold(ctx context.Context, userID string) (*domain.Household, domain.HouseholdEntitlementSummary, bool, error) {
	args := m.Called(ctx, userID)
	var h *domain.Household
	if args.Get(0) != nil {
		h = args.Get(0).(*domain.Household)
	}
	return h, args.Get(1).(domain.HouseholdEntitlementSummary), args.Bool(2), args.Error(3)
}
func (m *mockHouseholdService) UpdateHouseholdName(ctx context.Context, userID, name string) (*domain.Household, error) {
	args := m.Called(ctx, userID, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Household), args.Error(1)
}
func (m *mockHouseholdService) DeleteHousehold(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}
func (m *mockHouseholdService) ListMembers(ctx context.Context, userID string) ([]domain.HouseholdMember, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.HouseholdMember), args.Error(1)
}
func (m *mockHouseholdService) RemoveMember(ctx context.Context, actorUserID, targetUserID string) error {
	args := m.Called(ctx, actorUserID, targetUserID)
	return args.Error(0)
}
func (m *mockHouseholdService) CreateInvite(ctx context.Context, ownerUserID, inviteeEmail, frontendURL string) (*services.HouseholdCreateInviteResult, error) {
	args := m.Called(ctx, ownerUserID, inviteeEmail, frontendURL)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*services.HouseholdCreateInviteResult), args.Error(1)
}
func (m *mockHouseholdService) ListInvites(ctx context.Context, ownerUserID string) ([]domain.HouseholdInvite, error) {
	args := m.Called(ctx, ownerUserID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.HouseholdInvite), args.Error(1)
}
func (m *mockHouseholdService) RevokeInvite(ctx context.Context, ownerUserID, inviteID string) error {
	args := m.Called(ctx, ownerUserID, inviteID)
	return args.Error(0)
}
func (m *mockHouseholdService) GetInvitePreview(ctx context.Context, rawToken string) (*repository.HouseholdInvitePreview, error) {
	args := m.Called(ctx, rawToken)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*repository.HouseholdInvitePreview), args.Error(1)
}
func (m *mockHouseholdService) AcceptInvite(ctx context.Context, callerUserID, rawToken string, consent bool, hiddenAccountIDs []string) (*services.HouseholdAcceptResult, error) {
	args := m.Called(ctx, callerUserID, rawToken, consent, hiddenAccountIDs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*services.HouseholdAcceptResult), args.Error(1)
}
func (m *mockHouseholdService) DeclineInvite(ctx context.Context, callerUserID, rawToken string) error {
	args := m.Called(ctx, callerUserID, rawToken)
	return args.Error(0)
}
func (m *mockHouseholdService) SetAnalytics(a phclient.Analytics) {}

func TestCreateHousehold_Success(t *testing.T) {
	svc := &mockHouseholdService{}
	h := &domain.Household{ID: "hh-1", OwnerUserID: "user-1", Name: "The Smiths"}
	svc.On("CreateHousehold", mock.Anything, "user-1", "The Smiths").Return(h, nil)

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	out, err := handler.CreateHousehold(context.Background(), &CreateHouseholdInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		Body:               CreateHouseholdRequestBody{Name: "The Smiths"},
	})
	require.NoError(t, err)
	assert.Equal(t, "hh-1", out.Body.ID)
}

func TestCreateHousehold_MapsError(t *testing.T) {
	svc := &mockHouseholdService{}
	svc.On("CreateHousehold", mock.Anything, "user-1", "The Smiths").Return(nil, domain.ErrAlreadyOwnsHousehold)

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	_, err := handler.CreateHousehold(context.Background(), &CreateHouseholdInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		Body:               CreateHouseholdRequestBody{Name: "The Smiths"},
	})
	require.Error(t, err)
}

func TestGetMyHousehold_Found(t *testing.T) {
	svc := &mockHouseholdService{}
	h := &domain.Household{ID: "hh-1", OwnerUserID: "user-1", Name: "The Smiths"}
	summary := domain.HouseholdEntitlementSummary{Tier: "household_2", UsersIncluded: 2, IsOwner: true}
	svc.On("GetMyHousehold", mock.Anything, "user-1").Return(h, summary, true, nil)

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	out, err := handler.GetMyHousehold(context.Background(), &GetMyHouseholdInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
	})
	require.NoError(t, err)
	assert.Equal(t, "hh-1", out.Body.Household.ID)
	assert.True(t, out.Body.Summary.IsOwner)
}

func TestGetMyHousehold_NotFound(t *testing.T) {
	svc := &mockHouseholdService{}
	svc.On("GetMyHousehold", mock.Anything, "user-1").Return(nil, domain.HouseholdEntitlementSummary{}, false, nil)

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	_, err := handler.GetMyHousehold(context.Background(), &GetMyHouseholdInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
	})
	require.Error(t, err)
}

func TestUpdateHouseholdName_Success(t *testing.T) {
	svc := &mockHouseholdService{}
	h := &domain.Household{ID: "hh-1", OwnerUserID: "user-1", Name: "New Name"}
	svc.On("UpdateHouseholdName", mock.Anything, "user-1", "New Name").Return(h, nil)

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	out, err := handler.UpdateHouseholdName(context.Background(), &UpdateHouseholdNameInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		Body:               UpdateHouseholdNameRequestBody{Name: "New Name"},
	})
	require.NoError(t, err)
	assert.Equal(t, "New Name", out.Body.Name)
}

func TestDeleteHousehold_Success(t *testing.T) {
	svc := &mockHouseholdService{}
	svc.On("DeleteHousehold", mock.Anything, "user-1").Return(nil)

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	_, err := handler.DeleteHousehold(context.Background(), &DeleteHouseholdInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
	})
	require.NoError(t, err)
}

func TestDeleteHousehold_MapsError(t *testing.T) {
	svc := &mockHouseholdService{}
	svc.On("DeleteHousehold", mock.Anything, "user-1").Return(domain.ErrHouseholdHasActiveMembers)

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	_, err := handler.DeleteHousehold(context.Background(), &DeleteHouseholdInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
	})
	require.Error(t, err)
}

func TestListMembers_Success(t *testing.T) {
	svc := &mockHouseholdService{}
	members := []domain.HouseholdMember{{ID: "mem-1", UserID: "user-1", Role: domain.HouseholdRoleOwner}}
	svc.On("ListMembers", mock.Anything, "user-1").Return(members, nil)

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	out, err := handler.ListMembers(context.Background(), &ListMembersInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
	})
	require.NoError(t, err)
	require.Len(t, out.Body, 1)
}

func TestRemoveMember_Success(t *testing.T) {
	svc := &mockHouseholdService{}
	svc.On("RemoveMember", mock.Anything, "owner-1", "member-1").Return(nil)

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	_, err := handler.RemoveMember(context.Background(), &RemoveMemberInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "owner-1"},
		TargetUserID:       "member-1",
	})
	require.NoError(t, err)
}

func TestRemoveMember_MapsError(t *testing.T) {
	svc := &mockHouseholdService{}
	svc.On("RemoveMember", mock.Anything, "owner-1", "owner-1").Return(domain.ErrCannotRemoveOwner)

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	_, err := handler.RemoveMember(context.Background(), &RemoveMemberInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "owner-1"},
		TargetUserID:       "owner-1",
	})
	require.Error(t, err)
}

func TestCreateInvite_Success(t *testing.T) {
	svc := &mockHouseholdService{}
	result := &services.HouseholdCreateInviteResult{
		Invite:     domain.HouseholdInvite{ID: "inv-1", InviteeEmail: "friend@example.com"},
		InviteLink: "https://app.example.com/households/invites/rawtoken",
	}
	svc.On("CreateInvite", mock.Anything, "owner-1", "friend@example.com", "https://app.example.com").Return(result, nil)

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	out, err := handler.CreateInvite(context.Background(), &CreateInviteInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "owner-1"},
		Body:               CreateInviteRequestBody{Email: "friend@example.com"},
	})
	require.NoError(t, err)
	assert.Equal(t, "inv-1", out.Body.Invite.ID)
	assert.Equal(t, "https://app.example.com/households/invites/rawtoken", out.Body.InviteLink)
}

func TestCreateInvite_MapsError(t *testing.T) {
	svc := &mockHouseholdService{}
	svc.On("CreateInvite", mock.Anything, "owner-1", "friend@example.com", "https://app.example.com").Return(nil, domain.ErrHouseholdSeatsFull)

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	_, err := handler.CreateInvite(context.Background(), &CreateInviteInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "owner-1"},
		Body:               CreateInviteRequestBody{Email: "friend@example.com"},
	})
	require.Error(t, err)
}

func TestListInvites_Success(t *testing.T) {
	svc := &mockHouseholdService{}
	invites := []domain.HouseholdInvite{{ID: "inv-1"}}
	svc.On("ListInvites", mock.Anything, "owner-1").Return(invites, nil)

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	out, err := handler.ListInvites(context.Background(), &ListInvitesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "owner-1"},
	})
	require.NoError(t, err)
	require.Len(t, out.Body, 1)
}

func TestRevokeInvite_Success(t *testing.T) {
	svc := &mockHouseholdService{}
	svc.On("RevokeInvite", mock.Anything, "owner-1", "inv-1").Return(nil)

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	_, err := handler.RevokeInvite(context.Background(), &RevokeInviteInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "owner-1"},
		ID:                 "inv-1",
	})
	require.NoError(t, err)
}

func TestGetInvitePreview_Success(t *testing.T) {
	svc := &mockHouseholdService{}
	preview := &repository.HouseholdInvitePreview{HouseholdName: "The Smiths", InviteeEmail: "friend@example.com", ExpiresAt: time.Now().Add(48 * time.Hour)}
	svc.On("GetInvitePreview", mock.Anything, "rawtoken").Return(preview, nil)

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	out, err := handler.GetInvitePreview(context.Background(), &GetInvitePreviewInput{Token: "rawtoken"})
	require.NoError(t, err)
	assert.Equal(t, "The Smiths", out.Body.HouseholdName)
	assert.Equal(t, "no-store", out.CacheControl)
}

func TestGetInvitePreview_NotFound(t *testing.T) {
	svc := &mockHouseholdService{}
	svc.On("GetInvitePreview", mock.Anything, "badtoken").Return(nil, domain.ErrInviteNotFound)

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	_, err := handler.GetInvitePreview(context.Background(), &GetInvitePreviewInput{Token: "badtoken"})
	require.Error(t, err)
}

func TestAcceptInvite_Success(t *testing.T) {
	svc := &mockHouseholdService{}
	result := &services.HouseholdAcceptResult{
		Member:    domain.HouseholdMember{ID: "mem-1", UserID: "caller-1"},
		Household: domain.Household{ID: "hh-1"},
	}
	svc.On("AcceptInvite", mock.Anything, "caller-1", "rawtoken", true, []string{"acct-2"}).Return(result, nil)

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	out, err := handler.AcceptInvite(context.Background(), &AcceptInviteInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "caller-1"},
		Token:              "rawtoken",
		Body:               AcceptInviteRequestBody{Consent: true, HiddenAccountIDs: []string{"acct-2"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "hh-1", out.Body.HouseholdID)
}

func TestAcceptInvite_MapsError(t *testing.T) {
	svc := &mockHouseholdService{}
	svc.On("AcceptInvite", mock.Anything, "caller-1", "rawtoken", true, []string(nil)).Return(nil, domain.ErrInviteeHasActivePaidSubscription)

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	_, err := handler.AcceptInvite(context.Background(), &AcceptInviteInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "caller-1"},
		Token:              "rawtoken",
		Body:               AcceptInviteRequestBody{Consent: true},
	})
	require.Error(t, err)
}

func TestAcceptInvite_NoConsent_ReturnsError(t *testing.T) {
	svc := &mockHouseholdService{}
	svc.On("AcceptInvite", mock.Anything, "caller-1", "rawtoken", false, []string(nil)).Return(nil, domain.ErrInvalidConsent)

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	_, err := handler.AcceptInvite(context.Background(), &AcceptInviteInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "caller-1"},
		Token:              "rawtoken",
		Body:               AcceptInviteRequestBody{Consent: false},
	})
	require.Error(t, err)
}

func TestDeclineInvite_Success(t *testing.T) {
	svc := &mockHouseholdService{}
	svc.On("DeclineInvite", mock.Anything, "caller-1", "rawtoken").Return(nil)

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	_, err := handler.DeclineInvite(context.Background(), &DeclineInviteInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "caller-1"},
		Token:              "rawtoken",
	})
	require.NoError(t, err)
}

func TestDeclineInvite_MapsError(t *testing.T) {
	svc := &mockHouseholdService{}
	svc.On("DeclineInvite", mock.Anything, "caller-1", "rawtoken").Return(domain.ErrInviteEmailMismatch)

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	_, err := handler.DeclineInvite(context.Background(), &DeclineInviteInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "caller-1"},
		Token:              "rawtoken",
	})
	require.Error(t, err)
}
