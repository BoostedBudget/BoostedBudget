package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockAdminUserService mocks the AdminUserService
type MockAdminUserService struct {
	mock.Mock
}

func (m *MockAdminUserService) ListUsers(ctx context.Context, filters domain.AdminUserFilters, page, pageSize int) (*domain.AdminUserListResponse, error) {
	args := m.Called(ctx, filters, page, pageSize)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.AdminUserListResponse), args.Error(1)
}

func (m *MockAdminUserService) GetUserDetail(ctx context.Context, userID string) (*domain.AdminUserDetail, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.AdminUserDetail), args.Error(1)
}

func (m *MockAdminUserService) UpdateUserTier(ctx context.Context, actorUserID string, targetUserID string, tier string, reason string, expiresAt *time.Time) (services.UpdateUserTierResult, error) {
	args := m.Called(ctx, actorUserID, targetUserID, tier, reason, expiresAt)
	if args.Get(0) == nil {
		return services.UpdateUserTierResult{}, args.Error(1)
	}
	return args.Get(0).(services.UpdateUserTierResult), args.Error(1)
}

func (m *MockAdminUserService) RemoveUserTierOverride(ctx context.Context, actorUserID, targetUserID, reason string) (services.RemoveTierOverrideResult, error) {
	args := m.Called(ctx, actorUserID, targetUserID, reason)
	if args.Get(0) == nil {
		return services.RemoveTierOverrideResult{}, args.Error(1)
	}
	return args.Get(0).(services.RemoveTierOverrideResult), args.Error(1)
}

// MockAdminService is a mock implementation of services.AdminService
type MockAdminService struct {
	mock.Mock
}

func (m *MockAdminService) HasRole(ctx context.Context, clerkUserID string, role domain.AdminRole) (bool, error) {
	args := m.Called(ctx, clerkUserID, role)
	return args.Bool(0), args.Error(1)
}

func (m *MockAdminService) GetRoles(ctx context.Context, clerkUserID string) ([]domain.AdminRole, error) {
	args := m.Called(ctx, clerkUserID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.AdminRole), args.Error(1)
}

func (m *MockAdminService) InvalidateCache(clerkUserID string) {
	m.Called(clerkUserID)
}

func (m *MockAdminService) IsHardcodedAdmin(email string) bool {
	args := m.Called(email)
	return args.Bool(0)
}

func setupAdminSubscriptionsAPI(t *testing.T) (humatest.TestAPI, *MockAdminUserService, *MockAdminService) {
	_, api := humatest.New(t)
	mockAdminUserService := new(MockAdminUserService)
	mockAdminService := new(MockAdminService)

	cfg := &config.Config{Environment: "qa"} // Set to QA so middleware allows the request
	handler := NewAdminHandler(nil, nil, nil, mockAdminService, mockAdminUserService, nil, nil, cfg)
	RegisterAdminSubscriptionsOperations(api, handler)
	return api, mockAdminUserService, mockAdminService
}

type updateTierResponseBody struct {
	Message string `json:"message"`
}

// TestUpdateAdminUserTier_Success exercises the full Huma request pipeline (path binding +
// AdminAuthenticatedInput.Resolve) to prove the PUT .../tier route works end to end and the
// service call receives the actor's internal UUID (not the target's, not the Clerk ID).
func TestUpdateAdminUserTier_Success(t *testing.T) {
	api, mockService, mockAdmin := setupAdminSubscriptionsAPI(t)

	clerkUserID := "user_" + uuid.New().String()
	actorUserID := uuid.New().String()
	targetUserID := uuid.New().String()
	tier := "premium"
	reason := "manual override for testing"

	mockAdmin.On("IsHardcodedAdmin", mock.Anything).Return(false)
	mockAdmin.On("HasRole", mock.Anything, clerkUserID, domain.RoleAdmin).Return(true, nil)
	mockService.On("UpdateUserTier", mock.Anything, actorUserID, targetUserID, tier, reason, (*time.Time)(nil)).
		Return(services.UpdateUserTierResult{}, nil)

	ctx := authorizedAdminCtx(clerkUserID, actorUserID)
	resp := api.PutCtx(ctx, "/api/v1/admin/users/"+targetUserID+"/subscription/tier", map[string]interface{}{
		"tier": tier, "reason": reason, "expires_at": nil,
	})

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var body updateTierResponseBody
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, "User subscription tier updated successfully", body.Message)

	mockService.AssertExpectations(t)
	mockAdmin.AssertExpectations(t)
}

// TestUpdateAdminUserTier_ActorAndTargetDiffer proves, through the real Huma request pipeline,
// that the acting admin's UUID and the target user's UUID land in the correct positional
// slots (mirrors TestRemoveUserTierOverride_ActorAndTargetDiffer — same shadowing hazard).
func TestUpdateAdminUserTier_ActorAndTargetDiffer(t *testing.T) {
	api, mockService, mockAdmin := setupAdminSubscriptionsAPI(t)

	clerkUserID := "user_" + uuid.New().String()
	actorUserID := uuid.New().String()
	targetUserID := uuid.New().String()
	require.NotEqual(t, actorUserID, targetUserID)
	reason := "actor/target must not swap"

	mockAdmin.On("IsHardcodedAdmin", mock.Anything).Return(false)
	mockAdmin.On("HasRole", mock.Anything, clerkUserID, domain.RoleAdmin).Return(true, nil)
	// Exact positional match: actorUserID in the 2nd slot, targetUserID in the 3rd. If the
	// handler ever reads input.UserID for the actor, this expectation will not match and the
	// mock will panic on an unexpected call.
	mockService.On("UpdateUserTier", mock.Anything, actorUserID, targetUserID, "premium", reason, (*time.Time)(nil)).
		Return(services.UpdateUserTierResult{}, nil)

	ctx := authorizedAdminCtx(clerkUserID, actorUserID)
	resp := api.PutCtx(ctx, "/api/v1/admin/users/"+targetUserID+"/subscription/tier", map[string]interface{}{
		"tier": "premium", "reason": reason, "expires_at": nil,
	})

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	mockService.AssertExpectations(t)
}

// TestUpdateAdminUserTier_WarningAppendedToMessage proves a non-empty
// UpdateUserTierResult.Warning (Issue #1413 Defect 5) is appended to the response's message
// field rather than dropped or surfaced via a new field.
func TestUpdateAdminUserTier_WarningAppendedToMessage(t *testing.T) {
	api, mockService, mockAdmin := setupAdminSubscriptionsAPI(t)

	clerkUserID := "user_" + uuid.New().String()
	actorUserID := uuid.New().String()
	targetUserID := uuid.New().String()
	reason := "comp down to premium"
	warning := "Warning: \"premium\" includes 1 seat(s), but this household has 4 active member(s)."

	mockAdmin.On("IsHardcodedAdmin", mock.Anything).Return(false)
	mockAdmin.On("HasRole", mock.Anything, clerkUserID, domain.RoleAdmin).Return(true, nil)
	mockService.On("UpdateUserTier", mock.Anything, actorUserID, targetUserID, "premium", reason, (*time.Time)(nil)).
		Return(services.UpdateUserTierResult{Warning: warning}, nil)

	ctx := authorizedAdminCtx(clerkUserID, actorUserID)
	resp := api.PutCtx(ctx, "/api/v1/admin/users/"+targetUserID+"/subscription/tier", map[string]interface{}{
		"tier": "premium", "reason": reason, "expires_at": nil,
	})

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var body updateTierResponseBody
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Contains(t, body.Message, "User subscription tier updated successfully")
	assert.Contains(t, body.Message, warning)
}

// TestUpdateAdminUserTier_InvalidTier_Returns400 proves services.ErrInvalidTierOverride
// (Issue #1413 Defect 4) is mapped to 400, not the generic 5xx LoggingMapError fallback.
//
// Calls the handler method directly rather than through the full Huma HTTP pipeline: the
// request body's `tier` field carries an `enum` tag that Huma's own schema validation would
// reject with 422 before the handler (or the mocked service) ever runs, which would defeat
// the point of this test — the handler's OWN error-mapping logic is what's under test here,
// as defense-in-depth for a caller that bypasses (or predates) that enum.
func TestUpdateAdminUserTier_InvalidTier_Returns400(t *testing.T) {
	mockService := new(MockAdminUserService)
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, mockService, nil, nil, cfg)

	actorUserID := uuid.New().String()
	targetUserID := uuid.New().String()
	reason := "bad tier"

	mockService.On("UpdateUserTier", mock.Anything, actorUserID, targetUserID, "not_a_real_tier", reason, (*time.Time)(nil)).
		Return(services.UpdateUserTierResult{}, fmt.Errorf("%w: %q", services.ErrInvalidTierOverride, "not_a_real_tier"))

	input := &AdminSubscriptionTierInput{UserID: targetUserID}
	input.AdminAuthenticatedInput.UserID = actorUserID
	input.Body.Tier = "not_a_real_tier"
	input.Body.Reason = reason

	_, err := handler.UpdateUserTier(context.Background(), input)
	require.Error(t, err)
	var statusErr huma.StatusError
	require.True(t, errors.As(err, &statusErr), "expected a huma.StatusError, got %T: %v", err, err)
	assert.Equal(t, http.StatusBadRequest, statusErr.GetStatus())

	mockService.AssertExpectations(t)
}

func TestGetUserSubscription_Success(t *testing.T) {
	api, mockService, mockAdmin := setupAdminSubscriptionsAPI(t)

	clerkUserID := "user_" + uuid.New().String()
	targetUserID := uuid.New().String()
	now := time.Now()
	tierOverride := "premium"

	mockAdmin.On("IsHardcodedAdmin", mock.Anything).Return(false)
	mockAdmin.On("HasRole", mock.Anything, clerkUserID, domain.RoleAdmin).Return(true, nil)

	mockService.On("GetUserDetail", mock.Anything, targetUserID).Return(&domain.AdminUserDetail{
		AdminUserListItem: domain.AdminUserListItem{
			ID:                 targetUserID,
			SubscriptionTier:   "free",
			SubscriptionStatus: "active",
			ManualTierOverride: &tierOverride,
			ManualTierEndsAt:   &now,
		},
	}, nil)

	ctx := context.WithValue(context.Background(), inputs.ClerkUserIDKey, clerkUserID)
	claims := &clerk.SessionClaims{
		Custom: map[string]interface{}{
			"email": "admin@example.com",
		},
	}
	ctx = context.WithValue(ctx, inputs.ClerkClaimsKey, claims)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/admin/users/"+targetUserID+"/subscription", nil)

	res := httptest.NewRecorder()
	api.Adapter().ServeHTTP(res, req)

	assert.NotNil(t, api)
}

// getUserSubscriptionResponseBody is the subset of AdminSubscriptionDetailOutput.Body relevant
// to the effective_status assertions below.
type getUserSubscriptionResponseBody struct {
	SubscriptionStatus string `json:"subscription_status"`
	EffectiveTier      string `json:"effective_tier"`
	EffectiveStatus    string `json:"effective_status"`
}

// TestGetUserSubscription_ReturnsEffectiveStatus proves the response body carries
// effective_status alongside the existing raw subscription_status and effective_tier (Issue
// #1413) — the modal previously had only raw status, so an admin could not see whether a comp
// actually unlocks access. The handler surfaces AdminUserService's already-computed
// EffectiveStatus (single source of truth) rather than recomputing it.
func TestGetUserSubscription_ReturnsEffectiveStatus(t *testing.T) {
	api, mockService, mockAdmin := setupAdminSubscriptionsAPI(t)

	clerkUserID := "user_" + uuid.New().String()
	actorUserID := uuid.New().String()
	targetUserID := uuid.New().String()
	tierOverride := "household_2"
	futureEnd := time.Now().Add(24 * time.Hour)

	mockAdmin.On("IsHardcodedAdmin", mock.Anything).Return(false)
	mockAdmin.On("HasRole", mock.Anything, clerkUserID, domain.RoleAdmin).Return(true, nil)

	mockService.On("GetUserDetail", mock.Anything, targetUserID).Return(&domain.AdminUserDetail{
		AdminUserListItem: domain.AdminUserListItem{
			ID:                 targetUserID,
			SubscriptionTier:   "premium",
			SubscriptionStatus: "inactive",
			EffectiveStatus:    "active", // as already computed by AdminUserService.enrichAdminUser
			ManualTierOverride: &tierOverride,
			ManualTierEndsAt:   &futureEnd,
		},
	}, nil)

	ctx := authorizedAdminCtx(clerkUserID, actorUserID)
	resp := api.GetCtx(ctx, "/api/v1/admin/users/"+targetUserID+"/subscription")

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var body getUserSubscriptionResponseBody
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, "inactive", body.SubscriptionStatus, "raw status must still be present")
	assert.Equal(t, "active", body.EffectiveStatus, "effective_status must reflect the live override")

	mockService.AssertExpectations(t)
	mockAdmin.AssertExpectations(t)
}

// TestGetUserSubscription_EffectiveStatus_NoOverride_MirrorsRawStatus proves that with no
// override, effective_status mirrors the raw subscription_status.
func TestGetUserSubscription_EffectiveStatus_NoOverride_MirrorsRawStatus(t *testing.T) {
	api, mockService, mockAdmin := setupAdminSubscriptionsAPI(t)

	clerkUserID := "user_" + uuid.New().String()
	actorUserID := uuid.New().String()
	targetUserID := uuid.New().String()

	mockAdmin.On("IsHardcodedAdmin", mock.Anything).Return(false)
	mockAdmin.On("HasRole", mock.Anything, clerkUserID, domain.RoleAdmin).Return(true, nil)

	mockService.On("GetUserDetail", mock.Anything, targetUserID).Return(&domain.AdminUserDetail{
		AdminUserListItem: domain.AdminUserListItem{
			ID:                 targetUserID,
			SubscriptionTier:   "premium",
			SubscriptionStatus: "past_due",
			EffectiveStatus:    "past_due",
		},
	}, nil)

	ctx := authorizedAdminCtx(clerkUserID, actorUserID)
	resp := api.GetCtx(ctx, "/api/v1/admin/users/"+targetUserID+"/subscription")

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var body getUserSubscriptionResponseBody
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, "past_due", body.EffectiveStatus)

	mockService.AssertExpectations(t)
	mockAdmin.AssertExpectations(t)
}

func TestCreateSubscriptionOverride_RouteRegistered(t *testing.T) {
	api, _, mockAdminSvc := setupAdminSubscriptionsAPI(t)

	targetUserID := uuid.New().String()
	clerkUserID := "user_" + uuid.New().String()

	mockAdminSvc.On("IsHardcodedAdmin", mock.Anything).Return(false)
	mockAdminSvc.On("HasRole", mock.Anything, clerkUserID, domain.RoleAdmin).Return(true, nil)

	ctx := context.WithValue(context.Background(), inputs.ClerkUserIDKey, clerkUserID)
	claims := &clerk.SessionClaims{
		Custom: map[string]interface{}{"email": "admin@example.com"},
	}
	ctx = context.WithValue(ctx, inputs.ClerkClaimsKey, claims)

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/admin/users/"+targetUserID+"/subscription/override", nil)
	res := httptest.NewRecorder()
	api.Adapter().ServeHTTP(res, req)

	// Route must exist (not 404/405 — any other status means the route is registered)
	assert.NotEqual(t, http.StatusNotFound, res.Code)
	assert.NotEqual(t, http.StatusMethodNotAllowed, res.Code)
}

// --- RemoveUserTierOverride tests (Issue #1414) ---

type removeTierOverrideResponseBody struct {
	Message        string `json:"message"`
	Removed        bool   `json:"removed"`
	RevertedTier   string `json:"reverted_tier"`
	RevertedStatus string `json:"reverted_status"`
}

// authorizedAdminCtx builds a context that passes NewAdminMiddleware's role gate (via the
// HasRole path, not the hardcoded-admin bypass) AND resolves AdminAuthenticatedInput's
// AuthenticatedInput.UserID to actorUserID — i.e. the full context the real Clerk middleware
// would set up for an authenticated admin whose internal UUID is actorUserID.
func authorizedAdminCtx(clerkUserID, actorUserID string) context.Context {
	ctx := context.WithValue(context.Background(), inputs.ClerkUserIDKey, clerkUserID)
	ctx = context.WithValue(ctx, inputs.UserIDKey, actorUserID)
	claims := &clerk.SessionClaims{
		Custom: map[string]interface{}{"email": "admin@example.com"},
	}
	ctx = context.WithValue(ctx, inputs.ClerkClaimsKey, claims)
	return ctx
}

func TestRemoveUserTierOverride_Success(t *testing.T) {
	api, mockService, mockAdmin := setupAdminSubscriptionsAPI(t)

	clerkUserID := "user_" + uuid.New().String()
	actorUserID := uuid.New().String()
	targetUserID := uuid.New().String()
	reason := "comp period ended"

	mockAdmin.On("IsHardcodedAdmin", mock.Anything).Return(false)
	mockAdmin.On("HasRole", mock.Anything, clerkUserID, domain.RoleAdmin).Return(true, nil)
	mockService.On("RemoveUserTierOverride", mock.Anything, actorUserID, targetUserID, reason).Return(services.RemoveTierOverrideResult{
		Removed:        true,
		RevertedTier:   "premium",
		RevertedStatus: "active",
	}, nil)

	ctx := authorizedAdminCtx(clerkUserID, actorUserID)
	resp := api.PostCtx(ctx, "/api/v1/admin/users/"+targetUserID+"/subscription/tier/clear", map[string]string{"reason": reason})

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var body removeTierOverrideResponseBody
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.True(t, body.Removed)
	assert.Equal(t, "premium", body.RevertedTier)
	assert.Equal(t, "active", body.RevertedStatus)
	assert.NotEmpty(t, body.Message)

	mockService.AssertExpectations(t)
	mockAdmin.AssertExpectations(t)
}

func TestRemoveUserTierOverride_NoExistingOverride(t *testing.T) {
	api, mockService, mockAdmin := setupAdminSubscriptionsAPI(t)

	clerkUserID := "user_" + uuid.New().String()
	actorUserID := uuid.New().String()
	targetUserID := uuid.New().String()
	reason := "repair stale metadata"

	mockAdmin.On("IsHardcodedAdmin", mock.Anything).Return(false)
	mockAdmin.On("HasRole", mock.Anything, clerkUserID, domain.RoleAdmin).Return(true, nil)
	mockService.On("RemoveUserTierOverride", mock.Anything, actorUserID, targetUserID, reason).Return(services.RemoveTierOverrideResult{
		Removed:        false,
		RevertedTier:   "premium",
		RevertedStatus: "active",
	}, nil)

	ctx := authorizedAdminCtx(clerkUserID, actorUserID)
	resp := api.PostCtx(ctx, "/api/v1/admin/users/"+targetUserID+"/subscription/tier/clear", map[string]string{"reason": reason})

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var body removeTierOverrideResponseBody
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.False(t, body.Removed, "no prior override must return removed=false, not an error")

	mockService.AssertExpectations(t)
}

func TestRemoveUserTierOverride_UserNotFound(t *testing.T) {
	api, mockService, mockAdmin := setupAdminSubscriptionsAPI(t)

	clerkUserID := "user_" + uuid.New().String()
	actorUserID := uuid.New().String()
	targetUserID := uuid.New().String()
	reason := "cleanup"

	mockAdmin.On("IsHardcodedAdmin", mock.Anything).Return(false)
	mockAdmin.On("HasRole", mock.Anything, clerkUserID, domain.RoleAdmin).Return(true, nil)
	mockService.On("RemoveUserTierOverride", mock.Anything, actorUserID, targetUserID, reason).Return(nil, services.ErrAdminUserNotFound)

	ctx := authorizedAdminCtx(clerkUserID, actorUserID)
	resp := api.PostCtx(ctx, "/api/v1/admin/users/"+targetUserID+"/subscription/tier/clear", map[string]string{"reason": reason})

	assert.Equal(t, http.StatusNotFound, resp.Code, resp.Body.String())
	mockService.AssertExpectations(t)
}

// TestRemoveUserTierOverride_EmptyReason_Rejected proves the Huma body schema
// (required + minLength:1) rejects an empty reason before the handler/service is ever
// invoked — mockService has no expectation set, so an unexpected call would panic the test.
func TestRemoveUserTierOverride_EmptyReason_Rejected(t *testing.T) {
	api, mockService, mockAdmin := setupAdminSubscriptionsAPI(t)

	clerkUserID := "user_" + uuid.New().String()
	actorUserID := uuid.New().String()
	targetUserID := uuid.New().String()

	mockAdmin.On("IsHardcodedAdmin", mock.Anything).Return(false)
	mockAdmin.On("HasRole", mock.Anything, clerkUserID, domain.RoleAdmin).Return(true, nil)

	ctx := authorizedAdminCtx(clerkUserID, actorUserID)
	resp := api.PostCtx(ctx, "/api/v1/admin/users/"+targetUserID+"/subscription/tier/clear", map[string]string{"reason": ""})

	assert.Equal(t, http.StatusUnprocessableEntity, resp.Code, resp.Body.String())
	mockService.AssertNotCalled(t, "RemoveUserTierOverride")
}

// TestRemoveUserTierOverride_ActorAndTargetDiffer proves, through the real Huma request
// pipeline (path binding + AdminAuthenticatedInput.Resolve), that the acting admin's UUID and
// the target user's UUID land in the correct positional slots and are never swapped. The
// struct's own path-bound UserID field shadows the promoted AuthenticatedInput.UserID field —
// invisible on a skim — so this test pins the wiring at the handler boundary.
func TestRemoveUserTierOverride_ActorAndTargetDiffer(t *testing.T) {
	api, mockService, mockAdmin := setupAdminSubscriptionsAPI(t)

	clerkUserID := "user_" + uuid.New().String()
	actorUserID := uuid.New().String()
	targetUserID := uuid.New().String()
	require.NotEqual(t, actorUserID, targetUserID)
	reason := "actor/target must not swap"

	mockAdmin.On("IsHardcodedAdmin", mock.Anything).Return(false)
	mockAdmin.On("HasRole", mock.Anything, clerkUserID, domain.RoleAdmin).Return(true, nil)
	// Exact positional match: actorUserID in the 2nd slot, targetUserID in the 3rd. If the
	// handler ever reads input.UserID for the actor (instead of
	// input.AdminAuthenticatedInput.UserID) this expectation will not match, and the mock
	// will panic on an unexpected call.
	mockService.On("RemoveUserTierOverride", mock.Anything, actorUserID, targetUserID, reason).Return(services.RemoveTierOverrideResult{
		Removed:        false,
		RevertedTier:   "premium",
		RevertedStatus: "active",
	}, nil)

	ctx := authorizedAdminCtx(clerkUserID, actorUserID)
	resp := api.PostCtx(ctx, "/api/v1/admin/users/"+targetUserID+"/subscription/tier/clear", map[string]string{"reason": reason})

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	mockService.AssertExpectations(t)
}
