package operations

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
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
func (m *mockHouseholdService) ListMembers(ctx context.Context, userID string) ([]services.HouseholdMemberView, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]services.HouseholdMemberView), args.Error(1)
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
func (m *mockHouseholdService) SetAnalytics(a phclient.Analytics)                         {}
func (m *mockHouseholdService) SetProductAnalytics(a phclient.ProductAnalytics)           {}
func (m *mockHouseholdService) SetMemberProfileResolver(r services.MemberProfileResolver) {}
func (m *mockHouseholdService) SetCacheInvalidation(gens *cache.Generations)              {}

func (m *mockHouseholdService) ActiveMemberUserIDs(ctx context.Context, householdID string) ([]string, error) {
	args := m.Called(ctx, householdID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
}

func (m *mockHouseholdService) ResolveScope(ctx context.Context, requestingUserID, scopeParam string) (*domain.HouseholdScope, error) {
	args := m.Called(ctx, requestingUserID, scopeParam)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.HouseholdScope), args.Error(1)
}

// OffboardExcessMembers, PurgeRemovedMembership, and ForceDisband (#1371 — Phase 7
// foundation) are not exercised by these operation-layer tests (no HTTP endpoint wires
// to them yet); stubbed to satisfy the expanded HouseholdService interface.
func (m *mockHouseholdService) OffboardExcessMembers(ctx context.Context, householdID string, expectedOverCapacitySince time.Time) ([]services.QueuedRemovalEmail, error) {
	args := m.Called(ctx, householdID, expectedOverCapacitySince)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]services.QueuedRemovalEmail), args.Error(1)
}

func (m *mockHouseholdService) PurgeRemovedMembership(ctx context.Context, householdID, membershipID string) error {
	args := m.Called(ctx, householdID, membershipID)
	return args.Error(0)
}

func (m *mockHouseholdService) ForceDisband(ctx context.Context, ownerUserID string) ([]services.QueuedRemovalEmail, error) {
	args := m.Called(ctx, ownerUserID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]services.QueuedRemovalEmail), args.Error(1)
}

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
	summary := domain.HouseholdEntitlementSummary{
		Tier: "household_2", UsersIncluded: 2, IsOwner: true, SeatsActive: 1, SeatsPending: 1,
	}
	svc.On("GetMyHousehold", mock.Anything, "user-1").Return(h, summary, true, nil)

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	out, err := handler.GetMyHousehold(context.Background(), &GetMyHouseholdInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
	})
	require.NoError(t, err)
	assert.Equal(t, "hh-1", out.Body.Household.ID)
	assert.True(t, out.Body.Summary.IsOwner)
	// Gap Analysis Round 2 (GT-5): the handler must pass seat usage through untouched —
	// GET /households/me is the endpoint the issue names as returning seat usage.
	assert.Equal(t, 2, out.Body.Summary.UsersIncluded)
	assert.Equal(t, 1, out.Body.Summary.SeatsActive)
	assert.Equal(t, 1, out.Body.Summary.SeatsPending)
}

// TestGetMyHousehold_OverCapacitySince_RoundTrips proves GET /api/v1/households/me
// surfaces the #1371 Task 1 over_capacity_since grace-period anchor: omitted from the
// JSON response when the household is within its seat limit (nil), and present with the
// correct RFC3339 timestamp when a grace period is running. GetMyHousehold passes
// domain.Household straight through with no field-by-field DTO mapping, so this pins
// down the JSON wire contract the frontend (Task 10) will read, not just Go struct
// equality. #1371 — Phase 7 Task 9.
func TestGetMyHousehold_OverCapacitySince_RoundTrips(t *testing.T) {
	summary := domain.HouseholdEntitlementSummary{Tier: "household_2", UsersIncluded: 2, IsOwner: true, SeatsActive: 3, SeatsPending: 0}

	t.Run("nil when not over capacity", func(t *testing.T) {
		svc := &mockHouseholdService{}
		h := &domain.Household{ID: "hh-1", OwnerUserID: "user-1", Name: "The Smiths"}
		svc.On("GetMyHousehold", mock.Anything, "user-1").Return(h, summary, true, nil)

		handler := NewHouseholdHandler(svc, "https://app.example.com")
		out, err := handler.GetMyHousehold(context.Background(), &GetMyHouseholdInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		})
		require.NoError(t, err)
		assert.Nil(t, out.Body.Household.OverCapacitySince)

		raw, err := json.Marshal(out.Body)
		require.NoError(t, err)
		assert.NotContains(t, string(raw), "over_capacity_since", "omitempty must drop the field entirely when nil")
	})

	t.Run("set with correct timestamp when over capacity", func(t *testing.T) {
		since := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
		svc := &mockHouseholdService{}
		h := &domain.Household{ID: "hh-1", OwnerUserID: "user-1", Name: "The Smiths", OverCapacitySince: &since}
		svc.On("GetMyHousehold", mock.Anything, "user-1").Return(h, summary, true, nil)

		handler := NewHouseholdHandler(svc, "https://app.example.com")
		out, err := handler.GetMyHousehold(context.Background(), &GetMyHouseholdInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		})
		require.NoError(t, err)
		require.NotNil(t, out.Body.Household.OverCapacitySince)
		assert.True(t, since.Equal(*out.Body.Household.OverCapacitySince))

		raw, err := json.Marshal(out.Body)
		require.NoError(t, err)
		var decoded struct {
			Household struct {
				OverCapacitySince *time.Time `json:"over_capacity_since"`
			} `json:"household"`
		}
		require.NoError(t, json.Unmarshal(raw, &decoded))
		require.NotNil(t, decoded.Household.OverCapacitySince)
		assert.True(t, since.Equal(*decoded.Household.OverCapacitySince))
	})
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

// TestDeleteHousehold_NoForce_CallsDeleteHousehold pins down the unchanged (#1367) "soft"
// behavior when force is explicitly false: the handler must still call the old
// DeleteHousehold path, not ForceDisband.
func TestDeleteHousehold_NoForce_CallsDeleteHousehold(t *testing.T) {
	svc := &mockHouseholdService{}
	svc.On("DeleteHousehold", mock.Anything, "user-1").Return(nil).Once()

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	_, err := handler.DeleteHousehold(context.Background(), &DeleteHouseholdInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		Force:              false,
	})
	require.NoError(t, err)
	svc.AssertExpectations(t)
	svc.AssertNotCalled(t, "ForceDisband", mock.Anything, mock.Anything)
}

// TestDeleteHousehold_Force_Success covers #1371 Task 7: force=true must delegate to
// ForceDisband (not DeleteHousehold), even when active non-owner members exist —
// ForceDisband itself already force-removes them in one transaction (Task 0).
func TestDeleteHousehold_Force_Success(t *testing.T) {
	svc := &mockHouseholdService{}
	queued := []services.QueuedRemovalEmail{
		{ToEmail: "member@example.com", HouseholdName: "The Smiths", PurgeDate: time.Now().Add(30 * 24 * time.Hour)},
	}
	svc.On("ForceDisband", mock.Anything, "owner-1").Return(queued, nil).Once()

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	out, err := handler.DeleteHousehold(context.Background(), &DeleteHouseholdInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "owner-1"},
		Force:              true,
	})
	require.NoError(t, err)
	assert.NotNil(t, out)
	svc.AssertExpectations(t)
	svc.AssertNotCalled(t, "DeleteHousehold", mock.Anything, mock.Anything)
}

// TestDeleteHousehold_Force_NoOtherMembers_Success covers the owner-alone case: force=true
// on a household with no active non-owner members must still succeed (ForceDisband
// already handles this per Task 0 — an empty removal slice is not an error).
func TestDeleteHousehold_Force_NoOtherMembers_Success(t *testing.T) {
	svc := &mockHouseholdService{}
	svc.On("ForceDisband", mock.Anything, "owner-1").Return([]services.QueuedRemovalEmail{}, nil)

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	_, err := handler.DeleteHousehold(context.Background(), &DeleteHouseholdInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "owner-1"},
		Force:              true,
	})
	require.NoError(t, err)
}

// TestDeleteHousehold_Force_NonOwner_MapsError confirms force=true is rejected for a
// non-owner exactly like the existing non-force path — ForceDisband itself returns
// domain.ErrNotHouseholdOwner (same authorization pattern as DeleteHousehold), and the
// handler must propagate it, not silently succeed.
func TestDeleteHousehold_Force_NonOwner_MapsError(t *testing.T) {
	svc := &mockHouseholdService{}
	svc.On("ForceDisband", mock.Anything, "member-1").Return(nil, domain.ErrNotHouseholdOwner)

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	_, err := handler.DeleteHousehold(context.Background(), &DeleteHouseholdInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "member-1"},
		Force:              true,
	})
	require.Error(t, err)
}

// TestDeleteHousehold_Force_DoesNotResendRemovalEmails is the explicit regression test
// for the known Task 6 double-send bug shape (see #1371 history: OffboardExcessMembers's
// caller re-sent emails from the returned slice on top of the service's own internal
// send). ForceDisband already calls sendRemovalEmails internally right after its own
// tx.Commit — the handler must treat the returned []QueuedRemovalEmail as
// caller-bookkeeping ONLY and must never re-send from it. mockHouseholdService only
// stubs ForceDisband (Once()); HouseholdHandler holds no EmailService/notifier of its
// own, so there is no second send path available to it — if a future change added one
// and used it, this mock would either fail the Once() cardinality check (if it called
// ForceDisband again) or panic on an unexpected call to any other unstubbed method.
func TestDeleteHousehold_Force_DoesNotResendRemovalEmails(t *testing.T) {
	svc := &mockHouseholdService{}
	queued := []services.QueuedRemovalEmail{
		{ToEmail: "member1@example.com", HouseholdName: "The Smiths", PurgeDate: time.Now().Add(30 * 24 * time.Hour)},
		{ToEmail: "member2@example.com", HouseholdName: "The Smiths", PurgeDate: time.Now().Add(30 * 24 * time.Hour)},
	}
	svc.On("ForceDisband", mock.Anything, "owner-1").Return(queued, nil).Once()

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	_, err := handler.DeleteHousehold(context.Background(), &DeleteHouseholdInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "owner-1"},
		Force:              true,
	})
	require.NoError(t, err)

	// Once() above already enforces ForceDisband was called exactly one time; asserting
	// expectations here re-confirms no other stubbed/unstubbed interaction occurred.
	svc.AssertExpectations(t)
}

func TestListMembers_Success(t *testing.T) {
	svc := &mockHouseholdService{}
	members := []services.HouseholdMemberView{
		{UserID: "user-1", Role: domain.HouseholdRoleOwner, DisplayName: "Jane Doe", AvatarURL: "https://img.example/jane.png"},
	}
	svc.On("ListMembers", mock.Anything, "user-1").Return(members, nil)

	handler := NewHouseholdHandler(svc, "https://app.example.com")
	out, err := handler.ListMembers(context.Background(), &ListMembersInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
	})
	require.NoError(t, err)
	require.Len(t, out.Body, 1)
	assert.Equal(t, "Jane Doe", out.Body[0].DisplayName)
	assert.Equal(t, "https://img.example/jane.png", out.Body[0].AvatarURL)
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
