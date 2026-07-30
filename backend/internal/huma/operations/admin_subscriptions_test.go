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
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
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

// SetAnalytics (Issue #1413 Task MB) is not exercised by these handler-level tests — analytics
// wiring is tested at the service layer (admin_user_service_test.go); stubbed here only to
// satisfy the expanded services.AdminUserService interface.
func (m *MockAdminUserService) SetAnalytics(a phclient.Analytics) {}

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

// TestGetUserSubscription_IncludesDeletionFields_WhenPending proves deletion_requested_at and
// deletion_scheduled_at (Issue #1413 AC 10 — the admin override modal must warn when comping a
// deletion-scheduled user) are surfaced on the subscription-detail response, sourced from the
// same AdminUserService.GetUserDetail() call already made for base_tier/status — no second
// query. Values are read via a raw map (not a partial struct) so the omitempty behaviour proven
// by the companion "omits" test below is meaningfully exercised by the same decoding path.
func TestGetUserSubscription_IncludesDeletionFields_WhenPending(t *testing.T) {
	api, mockService, mockAdmin := setupAdminSubscriptionsAPI(t)

	clerkUserID := "user_" + uuid.New().String()
	actorUserID := uuid.New().String()
	targetUserID := uuid.New().String()
	requestedAt := time.Now().Add(-48 * time.Hour)
	scheduledAt := time.Now().Add(30 * 24 * time.Hour)

	mockAdmin.On("IsHardcodedAdmin", mock.Anything).Return(false)
	mockAdmin.On("HasRole", mock.Anything, clerkUserID, domain.RoleAdmin).Return(true, nil)

	mockService.On("GetUserDetail", mock.Anything, targetUserID).Return(&domain.AdminUserDetail{
		AdminUserListItem: domain.AdminUserListItem{
			ID:                  targetUserID,
			SubscriptionTier:    "premium",
			SubscriptionStatus:  "active",
			EffectiveStatus:     "active",
			DeletionRequestedAt: &requestedAt,
			DeletionScheduledAt: &scheduledAt,
		},
	}, nil)

	ctx := authorizedAdminCtx(clerkUserID, actorUserID)
	resp := api.GetCtx(ctx, "/api/v1/admin/users/"+targetUserID+"/subscription")

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var asMap map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &asMap))
	requestedRaw, hasRequested := asMap["deletion_requested_at"]
	scheduledRaw, hasScheduled := asMap["deletion_scheduled_at"]
	require.True(t, hasRequested, "deletion_requested_at must be present when a deletion is pending")
	require.True(t, hasScheduled, "deletion_scheduled_at must be present when a deletion is pending")

	gotRequested, err := time.Parse(time.RFC3339, requestedRaw.(string))
	require.NoError(t, err)
	assert.WithinDuration(t, requestedAt, gotRequested, time.Second)

	gotScheduled, err := time.Parse(time.RFC3339, scheduledRaw.(string))
	require.NoError(t, err)
	assert.WithinDuration(t, scheduledAt, gotScheduled, time.Second)

	mockService.AssertExpectations(t)
	mockAdmin.AssertExpectations(t)
}

// TestGetUserSubscription_OmitsDeletionFields_WhenNoPendingDeletion proves the two deletion
// fields are absent (not merely null) from the JSON body for a user with no pending GDPR
// deletion — the modal's warning must not accidentally fire for every user.
func TestGetUserSubscription_OmitsDeletionFields_WhenNoPendingDeletion(t *testing.T) {
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
			SubscriptionStatus: "active",
			EffectiveStatus:    "active",
		},
	}, nil)

	ctx := authorizedAdminCtx(clerkUserID, actorUserID)
	resp := api.GetCtx(ctx, "/api/v1/admin/users/"+targetUserID+"/subscription")

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var asMap map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &asMap))
	_, hasRequested := asMap["deletion_requested_at"]
	_, hasScheduled := asMap["deletion_scheduled_at"]
	assert.False(t, hasRequested, "deletion_requested_at must be omitted when no deletion is pending")
	assert.False(t, hasScheduled, "deletion_scheduled_at must be omitted when no deletion is pending")

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

// literalAdminSubscriptionsTierConfig returns a *config.TierConfig literal covering all five
// real tiers (config/subscription_tiers.yaml), for tests that need a real
// *services.SubscriptionService wired to h.subService. Per this branch's standing rule, tests
// must build a literal rather than call config.LoadTierConfig()/config.NewTierConfig("") (a
// process-wide sync.Once singleton) — mirrors services.fakeTierConfig's identical rationale.
func literalAdminSubscriptionsTierConfig() *config.TierConfig {
	return &config.TierConfig{
		Tiers: map[domain.TierName]domain.SubscriptionTier{
			domain.TierPremium:    {Name: domain.TierPremium, Pricing: domain.SubscriptionPricing{UsersIncluded: 1}},
			domain.TierHousehold2: {Name: domain.TierHousehold2, Pricing: domain.SubscriptionPricing{UsersIncluded: 2}},
			domain.TierHousehold3: {Name: domain.TierHousehold3, Pricing: domain.SubscriptionPricing{UsersIncluded: 3}},
			domain.TierHousehold4: {Name: domain.TierHousehold4, Pricing: domain.SubscriptionPricing{UsersIncluded: 4}},
			domain.TierHousehold5: {Name: domain.TierHousehold5, Pricing: domain.SubscriptionPricing{UsersIncluded: 5}},
		},
	}
}

// setupAdminSubscriptionsAPIWithSubService mirrors setupAdminSubscriptionsAPI but wires a real
// *services.SubscriptionService (never nil) so GetUserSubscription actually exercises
// SubscriptionService.GetEffectiveSubscription — needed to reproduce (and then prove the fix
// for) the tier-axis malformed-override bug (Issue #1413, this task). With subService nil (the
// existing helper's behavior), GetUserSubscription takes its own hardcoded raw-tier fallback
// branch and never reaches SubscriptionService at all.
func setupAdminSubscriptionsAPIWithSubService(t *testing.T, subService *services.SubscriptionService) (humatest.TestAPI, *MockAdminUserService, *MockAdminService) {
	_, api := humatest.New(t)
	mockAdminUserService := new(MockAdminUserService)
	mockAdminService := new(MockAdminService)

	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, mockAdminService, mockAdminUserService, nil, subService, cfg)
	RegisterAdminSubscriptionsOperations(api, handler)
	return api, mockAdminUserService, mockAdminService
}

// TestGetUserSubscription_MalformedOverride_FallsBackToRawTier_NotServerError is the RED test for
// Issue #1413 (this task — the tier axis of the malformed-override rule). manual_tier_override is
// an unconstrained VARCHAR(50) with no CHECK constraint, so it can hold a value naming no
// configured tier. Before this fix, SubscriptionService.GetEffectiveSubscription called
// domain.User.ComputeEffectiveTier() unconditionally, so a malformed override reached
// tierConfig.GetTier() as an unrecognized name and propagated as a 500 — bricking the only UI
// that can repair the row: the override modal renders from this exact response, so when it 500s,
// "Remove Override" never appears and "Save Override" stays disabled.
//
// SubscriptionStatus is deliberately "active" (not lapsed) — the worst-affected user is an
// ACTIVELY PAYING one: raw status active means the read-only middleware waves every request
// through, and only then does tier resolution 500.
func TestGetUserSubscription_MalformedOverride_FallsBackToRawTier_NotServerError(t *testing.T) {
	tierConfig := literalAdminSubscriptionsTierConfig()
	subService := services.NewSubscriptionService(tierConfig, nil)
	// Mirrors main.go's production wiring: the validator is a closure over the same tierConfig
	// this service already holds (see SubscriptionService.SetOverrideTierValidator's doc
	// comment) — always wired for the process lifetime in production, never nil.
	subService.SetOverrideTierValidator(func(tier string) bool {
		_, err := tierConfig.GetTier(domain.TierName(tier))
		return err == nil
	})
	api, mockService, mockAdmin := setupAdminSubscriptionsAPIWithSubService(t, subService)

	clerkUserID := "user_" + uuid.New().String()
	actorUserID := uuid.New().String()
	targetUserID := uuid.New().String()
	override := "not_a_tier"
	futureEnd := time.Now().Add(24 * time.Hour)

	mockAdmin.On("IsHardcodedAdmin", mock.Anything).Return(false)
	mockAdmin.On("HasRole", mock.Anything, clerkUserID, domain.RoleAdmin).Return(true, nil)

	mockService.On("GetUserDetail", mock.Anything, targetUserID).Return(&domain.AdminUserDetail{
		AdminUserListItem: domain.AdminUserListItem{
			ID:                 targetUserID,
			SubscriptionTier:   "premium",
			SubscriptionStatus: "active",
			EffectiveStatus:    "active",
			ManualTierOverride: &override,
			ManualTierEndsAt:   &futureEnd,
		},
	}, nil)

	ctx := authorizedAdminCtx(clerkUserID, actorUserID)
	resp := api.GetCtx(ctx, "/api/v1/admin/users/"+targetUserID+"/subscription")

	require.Equal(t, http.StatusOK, resp.Code, "a malformed manual_tier_override must not 500 the admin subscription-detail endpoint — that response is what the override modal needs to render the Remove Override button: "+resp.Body.String())
	var body getUserSubscriptionResponseBody
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, "premium", body.EffectiveTier, "a malformed override must fall back to the raw tier, not surface the garbage string")

	mockService.AssertExpectations(t)
	mockAdmin.AssertExpectations(t)
}

// TestGetUserSubscription_ValidOverride_ResolvesToOverrideTier is the POSITIVE CONTROL for the
// fix above: a VALID, currently-configured override must still resolve to the override tier via
// the real SubscriptionService, not fall back to the raw tier. An over-tight fix that makes every
// comp fall back to the raw tier would silently un-comp every legitimately comped user, which is
// worse than the bug being fixed.
func TestGetUserSubscription_ValidOverride_ResolvesToOverrideTier(t *testing.T) {
	tierConfig := literalAdminSubscriptionsTierConfig()
	subService := services.NewSubscriptionService(tierConfig, nil)
	subService.SetOverrideTierValidator(func(tier string) bool {
		_, err := tierConfig.GetTier(domain.TierName(tier))
		return err == nil
	})
	api, mockService, mockAdmin := setupAdminSubscriptionsAPIWithSubService(t, subService)

	clerkUserID := "user_" + uuid.New().String()
	actorUserID := uuid.New().String()
	targetUserID := uuid.New().String()
	override := "household_3"
	futureEnd := time.Now().Add(24 * time.Hour)

	mockAdmin.On("IsHardcodedAdmin", mock.Anything).Return(false)
	mockAdmin.On("HasRole", mock.Anything, clerkUserID, domain.RoleAdmin).Return(true, nil)

	mockService.On("GetUserDetail", mock.Anything, targetUserID).Return(&domain.AdminUserDetail{
		AdminUserListItem: domain.AdminUserListItem{
			ID:                 targetUserID,
			SubscriptionTier:   "premium",
			SubscriptionStatus: "inactive",
			EffectiveStatus:    "active",
			ManualTierOverride: &override,
			ManualTierEndsAt:   &futureEnd,
		},
	}, nil)

	ctx := authorizedAdminCtx(clerkUserID, actorUserID)
	resp := api.GetCtx(ctx, "/api/v1/admin/users/"+targetUserID+"/subscription")

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var body getUserSubscriptionResponseBody
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, "household_3", body.EffectiveTier, "a valid override must still resolve to the override tier — over-tightening would silently un-comp legitimately comped users")

	mockService.AssertExpectations(t)
	mockAdmin.AssertExpectations(t)
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
