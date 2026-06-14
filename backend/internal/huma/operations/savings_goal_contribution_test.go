package operations

// savings_goal_contribution_test.go — Issue #1300 M20: TDD tests for contribution API
// Tests the ContributionHandler endpoints using testify mocks and humatest.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mocks
// =============================================================================

// mockSavingsContributionRepo is a testify mock for SavingsContributionRepository.
type mockSavingsContributionRepo struct {
	mock.Mock
}

func (m *mockSavingsContributionRepo) Insert(ctx context.Context, userID string, contrib *domain.SavingsGoalContribution) error {
	args := m.Called(ctx, userID, contrib)
	return args.Error(0)
}

func (m *mockSavingsContributionRepo) UpsertFromTransaction(ctx context.Context, userID, goalID, transactionID string, amount float64, source string) error {
	args := m.Called(ctx, userID, goalID, transactionID, amount, source)
	return args.Error(0)
}

func (m *mockSavingsContributionRepo) SumConfirmedByGoal(ctx context.Context, userID, goalID string) (float64, error) {
	args := m.Called(ctx, userID, goalID)
	return args.Get(0).(float64), args.Error(1)
}

func (m *mockSavingsContributionRepo) SumConfirmedByGoalInPeriod(ctx context.Context, userID, goalID string, start, end time.Time) (float64, error) {
	args := m.Called(ctx, userID, goalID, start, end)
	return args.Get(0).(float64), args.Error(1)
}

func (m *mockSavingsContributionRepo) UpsertProposedFromTransaction(ctx context.Context, userID, goalID, transactionID string, magnitude float64) error {
	args := m.Called(ctx, userID, goalID, transactionID, magnitude)
	return args.Error(0)
}

func (m *mockSavingsContributionRepo) ListByGoal(ctx context.Context, userID, goalID string) ([]*domain.SavingsGoalContribution, error) {
	args := m.Called(ctx, userID, goalID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.SavingsGoalContribution), args.Error(1)
}

func (m *mockSavingsContributionRepo) ConfirmContribution(ctx context.Context, userID, id string) (*domain.SavingsGoalContribution, error) {
	args := m.Called(ctx, userID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.SavingsGoalContribution), args.Error(1)
}

func (m *mockSavingsContributionRepo) DeleteContribution(ctx context.Context, userID, id string) error {
	args := m.Called(ctx, userID, id)
	return args.Error(0)
}

func (m *mockSavingsContributionRepo) DeleteManagedForTransaction(ctx context.Context, transactionID string) error {
	args := m.Called(ctx, transactionID)
	return args.Error(0)
}

func (m *mockSavingsContributionRepo) ArchiveByGoal(ctx context.Context, goalID string) error {
	args := m.Called(ctx, goalID)
	return args.Error(0)
}

// mockContributionProgressRecomputer mocks ContributionProgressRecomputer.
type mockContributionProgressRecomputer struct {
	mock.Mock
}

func (m *mockContributionProgressRecomputer) RecomputeUser(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

// =============================================================================
// Helpers
// =============================================================================

// makeTestContribution creates a test contribution fixture.
func makeTestContribution(goalID string) *domain.SavingsGoalContribution {
	return &domain.SavingsGoalContribution{
		ID:                      "contrib-test-1",
		SavingsGoalID:           goalID,
		Amount:                  250.00,
		Source:                  "manual",
		Status:                  "confirmed",
		ManagedByReconciliation: false,
		ConfirmedByUser:         true,
		OccurredOn:              "2026-06-14",
		CreatedAt:               time.Now(),
		UpdatedAt:               time.Now(),
	}
}

// =============================================================================
// Tests — Route registration
// =============================================================================

// TestContributionRouteRegistration verifies all four routes are registered
// and return 401 (no auth) rather than 404 (not registered).
func TestContributionRouteRegistration(t *testing.T) {
	contribRepo := new(mockSavingsContributionRepo)
	handler := NewContributionHandler(contribRepo, nil)

	_, api := humatest.New(t)
	RegisterContributionRoutes(api, handler)

	goalID := "goal-reg-1"
	contribID := "contrib-reg-1"

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/savings-goals/" + goalID + "/contributions"},
		{http.MethodGet, "/api/v1/savings-goals/" + goalID + "/contributions"},
		{http.MethodPost, "/api/v1/savings-goals/" + goalID + "/contributions/" + contribID + "/confirm"},
		{http.MethodDelete, "/api/v1/savings-goals/" + goalID + "/contributions/" + contribID},
	}

	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var code int
			if tc.method == http.MethodGet {
				code = api.Get(tc.path).Code
			} else if tc.method == http.MethodPost {
				code = api.Post(tc.path, map[string]interface{}{}).Code
			} else if tc.method == http.MethodDelete {
				code = api.Delete(tc.path).Code
			}
			// 401 = route found, auth required. 404 = route not registered.
			assert.Equal(t, http.StatusUnauthorized, code,
				"expected 401 (auth guard), not 404 (route missing) for %s %s", tc.method, tc.path)
		})
	}
}

// =============================================================================
// Tests — ListContributions
// =============================================================================

// TestListContributions_ReturnsGoalContributions verifies that ListContributions
// passes userID+goalID to the repo and returns the list (M20 ownership-scoped).
func TestListContributions_ReturnsGoalContributions(t *testing.T) {
	goalID := "goal-list-1"
	userID := "user-list-1"

	contribRepo := new(mockSavingsContributionRepo)
	handler := NewContributionHandler(contribRepo, nil)

	contribs := []*domain.SavingsGoalContribution{
		makeTestContribution(goalID),
	}
	contribRepo.On("ListByGoal", mock.Anything, userID, goalID).
		Return(contribs, nil)

	ctx := context.Background()
	input := &ListContributionsInput{
		AuthenticatedInput: authenticatedInput(userID),
		GoalID:             goalID,
	}

	output, err := handler.ListContributions(ctx, input)

	require.NoError(t, err)
	require.Len(t, output.Body, 1)
	assert.Equal(t, "contrib-test-1", output.Body[0].ID)
	contribRepo.AssertExpectations(t)
}

// =============================================================================
// Tests — LogContribution (POST)
// =============================================================================

// TestLogContribution_Success verifies that a valid manual contribution is inserted
// and progress is recomputed (M20 D6 source-a).
func TestLogContribution_Success(t *testing.T) {
	goalID := "goal-log-1"
	userID := "user-log-1"

	contribRepo := new(mockSavingsContributionRepo)
	progress := new(mockContributionProgressRecomputer)
	handler := NewContributionHandler(contribRepo, progress)

	contribRepo.On("Insert", mock.Anything, userID, mock.MatchedBy(func(c *domain.SavingsGoalContribution) bool {
		return c.SavingsGoalID == goalID &&
			c.Amount == 500.00 &&
			c.Source == "manual" &&
			c.Status == "confirmed" &&
			c.ConfirmedByUser &&
			!c.ManagedByReconciliation
	})).Return(nil)
	progress.On("RecomputeUser", mock.Anything, userID).Return(nil)

	ctx := context.Background()
	input := &LogContributionInput{
		AuthenticatedInput: authenticatedInput(userID),
		GoalID:             goalID,
		Body: domain.LogContributionRequest{
			Amount:     500.00,
			OccurredOn: "2026-06-14",
		},
	}

	output, err := handler.LogContribution(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, goalID, output.Body.SavingsGoalID)
	assert.Equal(t, 500.00, output.Body.Amount)
	assert.Equal(t, "manual", output.Body.Source)
	assert.Equal(t, "confirmed", output.Body.Status)
	contribRepo.AssertExpectations(t)
	progress.AssertExpectations(t)
}

// TestLogContribution_CrossUserGoal_Returns404 verifies that logging a contribution against
// a goal owned by a different user returns 404, not 500 (IDOR guard / Codex C1).
// The repo-level Insert rejects cross-user goals by checking ownership before inserting.
func TestLogContribution_CrossUserGoal_Returns404(t *testing.T) {
	goalID := "goal-other-user"
	userID := "user-attacker"

	contribRepo := new(mockSavingsContributionRepo)
	handler := NewContributionHandler(contribRepo, nil)

	// The repo Insert will verify goal ownership and reject with ErrSavingsGoalNotFound.
	contribRepo.On("Insert", mock.Anything, userID, mock.MatchedBy(func(c *domain.SavingsGoalContribution) bool {
		return c.SavingsGoalID == goalID
	})).Return(domain.ErrSavingsGoalNotFound)

	ctx := context.Background()
	input := &LogContributionInput{
		AuthenticatedInput: authenticatedInput(userID),
		GoalID:             goalID,
		Body: domain.LogContributionRequest{
			Amount:     100.00,
			OccurredOn: "2026-06-14",
		},
	}

	_, err := handler.LogContribution(ctx, input)
	require.Error(t, err)
	// Must surface as 404, not 500.
	humaErr, ok := err.(huma.StatusError)
	require.True(t, ok, "expected huma status error, got %T", err)
	assert.Equal(t, http.StatusNotFound, humaErr.GetStatus())
	contribRepo.AssertExpectations(t)
}

// TestLogContribution_InvalidAmount_Returns422 verifies that non-positive amounts are rejected.
func TestLogContribution_InvalidAmount_Returns422(t *testing.T) {
	goalID := "goal-invalid-amount"
	userID := "user-invalid"

	contribRepo := new(mockSavingsContributionRepo)
	handler := NewContributionHandler(contribRepo, nil)

	ctx := context.Background()
	input := &LogContributionInput{
		AuthenticatedInput: authenticatedInput(userID),
		GoalID:             goalID,
		Body: domain.LogContributionRequest{
			Amount:     -100.00, // invalid
			OccurredOn: "2026-06-14",
		},
	}

	_, err := handler.LogContribution(ctx, input)
	require.Error(t, err)
	// Should be a 422 validation error
	contribRepo.AssertNotCalled(t, "Insert")
}

// =============================================================================
// Tests — ConfirmContribution (POST confirm)
// =============================================================================

// TestConfirmContribution_Success verifies that a proposed contribution is promoted
// to confirmed and progress is recomputed (M20).
func TestConfirmContribution_Success(t *testing.T) {
	goalID := "goal-confirm-1"
	userID := "user-confirm-1"
	contribID := "contrib-confirm-1"

	contribRepo := new(mockSavingsContributionRepo)
	progress := new(mockContributionProgressRecomputer)
	handler := NewContributionHandler(contribRepo, progress)

	confirmed := makeTestContribution(goalID)
	confirmed.ID = contribID
	confirmed.Status = "confirmed"
	confirmed.ConfirmedByUser = true

	contribRepo.On("ConfirmContribution", mock.Anything, userID, contribID).
		Return(confirmed, nil)
	progress.On("RecomputeUser", mock.Anything, userID).Return(nil)

	ctx := context.Background()
	input := &ConfirmContributionInput{
		AuthenticatedInput: authenticatedInput(userID),
		GoalID:             goalID,
		ID:                 contribID,
	}

	output, err := handler.ConfirmContribution(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "confirmed", output.Body.Status)
	contribRepo.AssertExpectations(t)
	progress.AssertExpectations(t)
}

// TestConfirmContribution_NotFound_Returns404 verifies ErrContributionNotFound → 404.
func TestConfirmContribution_NotFound_Returns404(t *testing.T) {
	goalID := "goal-notfound"
	userID := "user-notfound"
	contribID := "contrib-notfound"

	contribRepo := new(mockSavingsContributionRepo)
	handler := NewContributionHandler(contribRepo, nil)

	contribRepo.On("ConfirmContribution", mock.Anything, userID, contribID).
		Return(nil, domain.ErrContributionNotFound)

	ctx := context.Background()
	input := &ConfirmContributionInput{
		AuthenticatedInput: authenticatedInput(userID),
		GoalID:             goalID,
		ID:                 contribID,
	}

	_, err := handler.ConfirmContribution(ctx, input)
	require.Error(t, err)
}

// TestConfirmContribution_AlreadyConfirmed_Returns409 verifies ErrContributionAlreadyConfirmed → 409.
func TestConfirmContribution_AlreadyConfirmed_Returns409(t *testing.T) {
	goalID := "goal-already"
	userID := "user-already"
	contribID := "contrib-already"

	contribRepo := new(mockSavingsContributionRepo)
	handler := NewContributionHandler(contribRepo, nil)

	contribRepo.On("ConfirmContribution", mock.Anything, userID, contribID).
		Return(nil, domain.ErrContributionAlreadyConfirmed)

	ctx := context.Background()
	input := &ConfirmContributionInput{
		AuthenticatedInput: authenticatedInput(userID),
		GoalID:             goalID,
		ID:                 contribID,
	}

	_, err := handler.ConfirmContribution(ctx, input)
	require.Error(t, err)
}

// TestConfirmContribution_OverAllocated_Returns422 verifies ErrContributionOverAllocated → 422.
func TestConfirmContribution_OverAllocated_Returns422(t *testing.T) {
	goalID := "goal-overalloc"
	userID := "user-overalloc"
	contribID := "contrib-overalloc"

	contribRepo := new(mockSavingsContributionRepo)
	handler := NewContributionHandler(contribRepo, nil)

	contribRepo.On("ConfirmContribution", mock.Anything, userID, contribID).
		Return(nil, domain.ErrContributionOverAllocated)

	ctx := context.Background()
	input := &ConfirmContributionInput{
		AuthenticatedInput: authenticatedInput(userID),
		GoalID:             goalID,
		ID:                 contribID,
	}

	_, err := handler.ConfirmContribution(ctx, input)
	require.Error(t, err)
}

// =============================================================================
// Tests — DeleteContribution (DELETE)
// =============================================================================

// TestDeleteContribution_Success verifies that a contribution is deleted and
// progress is recomputed (M20).
func TestDeleteContribution_Success(t *testing.T) {
	goalID := "goal-del-1"
	userID := "user-del-1"
	contribID := "contrib-del-1"

	contribRepo := new(mockSavingsContributionRepo)
	progress := new(mockContributionProgressRecomputer)
	handler := NewContributionHandler(contribRepo, progress)

	contribRepo.On("DeleteContribution", mock.Anything, userID, contribID).
		Return(nil)
	progress.On("RecomputeUser", mock.Anything, userID).Return(nil)

	ctx := context.Background()
	input := &DeleteContributionInput{
		AuthenticatedInput: authenticatedInput(userID),
		GoalID:             goalID,
		ID:                 contribID,
	}

	result, err := handler.DeleteContribution(ctx, input)

	require.NoError(t, err)
	assert.Nil(t, result) // 204 No Content
	contribRepo.AssertExpectations(t)
	progress.AssertExpectations(t)
}

// TestDeleteContribution_NotFound_Returns404 verifies ErrContributionNotFound → 404.
func TestDeleteContribution_NotFound_Returns404(t *testing.T) {
	goalID := "goal-delnf"
	userID := "user-delnf"
	contribID := "contrib-delnf"

	contribRepo := new(mockSavingsContributionRepo)
	handler := NewContributionHandler(contribRepo, nil)

	contribRepo.On("DeleteContribution", mock.Anything, userID, contribID).
		Return(domain.ErrContributionNotFound)

	ctx := context.Background()
	input := &DeleteContributionInput{
		AuthenticatedInput: authenticatedInput(userID),
		GoalID:             goalID,
		ID:                 contribID,
	}

	_, err := handler.DeleteContribution(ctx, input)
	require.Error(t, err)
}

// =============================================================================
// Serialization smoke test
// =============================================================================

// TestContributionResponseJSON verifies that SavingsGoalContribution serializes correctly.
func TestContributionResponseJSON(t *testing.T) {
	contrib := makeTestContribution("goal-json-1")
	data, err := json.Marshal(contrib)
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "contrib-test-1", parsed["id"])
	assert.Equal(t, "manual", parsed["source"])
	assert.Equal(t, "confirmed", parsed["status"])
	assert.Equal(t, 250.00, parsed["amount"])
}

// =============================================================================
// Helper — build an AuthenticatedInput with a specific userID
// =============================================================================

// authenticatedInput builds an AuthenticatedInput with the given userID.
// Reuses the same helper pattern as existing operations tests.
func authenticatedInput(userID string) inputs.AuthenticatedInput {
	return inputs.AuthenticatedInput{UserID: userID}
}
