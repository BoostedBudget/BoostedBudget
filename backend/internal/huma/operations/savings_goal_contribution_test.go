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
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/mockassert"
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

func (m *mockSavingsContributionRepo) InsertManualFromTransaction(ctx context.Context, userID, goalID, transactionID string, amount float64, occurredOn string) (*domain.SavingsGoalContribution, error) {
	args := m.Called(ctx, userID, goalID, transactionID, amount, occurredOn)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.SavingsGoalContribution), args.Error(1)
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

func (m *mockSavingsContributionRepo) ListByDeletedGoal(ctx context.Context, userID, goalID string) ([]*domain.SavingsGoalContribution, error) {
	args := m.Called(ctx, userID, goalID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.SavingsGoalContribution), args.Error(1)
}

func (m *mockSavingsContributionRepo) SumConfirmedByDeletedGoal(ctx context.Context, userID, goalID string) (float64, error) {
	args := m.Called(ctx, userID, goalID)
	return args.Get(0).(float64), args.Error(1)
}

func (m *mockSavingsContributionRepo) ConfirmContribution(ctx context.Context, userID, goalID, id string) (*domain.SavingsGoalContribution, error) {
	args := m.Called(ctx, userID, goalID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.SavingsGoalContribution), args.Error(1)
}

func (m *mockSavingsContributionRepo) RejectContribution(ctx context.Context, userID, goalID, id string) (*domain.SavingsGoalContribution, error) {
	args := m.Called(ctx, userID, goalID, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.SavingsGoalContribution), args.Error(1)
}

func (m *mockSavingsContributionRepo) DeleteContribution(ctx context.Context, userID, goalID, id string, auditLog repository.AuditLogRepository) error {
	args := m.Called(ctx, userID, goalID, id, auditLog)
	return args.Error(0)
}

func (m *mockSavingsContributionRepo) DeleteManagedForTransaction(ctx context.Context, transactionID string) error {
	args := m.Called(ctx, transactionID)
	return args.Error(0)
}

func (m *mockSavingsContributionRepo) ArchiveByGoalInTx(ctx context.Context, tx repository.Transaction, goalID string) error {
	args := m.Called(ctx, tx, goalID)
	return args.Error(0)
}

func (m *mockSavingsContributionRepo) InsertInTx(ctx context.Context, tx repository.Transaction, userID string, contrib *domain.SavingsGoalContribution) error {
	args := m.Called(ctx, tx, userID, contrib)
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

func (m *mockContributionProgressRecomputer) RecomputeGoalByID(ctx context.Context, userID, goalID string) error {
	args := m.Called(ctx, userID, goalID)
	return args.Error(0)
}

// mockSavingsGoalReader mocks ContributionGoalReader.
type mockSavingsGoalReader struct {
	mock.Mock
}

func (m *mockSavingsGoalReader) GetByIDForUser(ctx context.Context, id, userID string) (*domain.SavingsGoal, error) {
	args := m.Called(ctx, id, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.SavingsGoal), args.Error(1)
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
	handler := NewContributionHandler(contribRepo, nil, nil)

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
			switch tc.method {
			case http.MethodGet:
				code = api.Get(tc.path).Code
			case http.MethodPost:
				code = api.Post(tc.path, map[string]interface{}{}).Code
			case http.MethodDelete:
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
	handler := NewContributionHandler(contribRepo, nil, nil)

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

// TestListContributions_ArchivedAtReachesTheAPIResponse (#1625 Task 13) proves ArchivedAt
// survives all the way to the JSON huma actually writes on the wire.
//
// Before this test, ArchivedAt was exercised only at the repository layer (its NULL/non-NULL
// scan) -- nothing verified it was still there once wrapped in ListContributionsOutput.Body and
// marshaled the way huma marshals every handler response. output.Body is exactly what huma's
// runtime writes back to the caller, so json.Marshal on it here is not a stand-in for the wire
// format -- it IS the wire format. Both directions are asserted: a closed-period row (created by
// #1625 migration 000190/191) must carry a present, correctly-valued "archived_at", and an
// ordinary in-progress row must have the key OMITTED (omitempty) rather than present as null --
// a caller that only checks for the KEY's presence would otherwise misread a null-but-present
// key on every row as "this row is archived".
func TestListContributions_ArchivedAtReachesTheAPIResponse(t *testing.T) {
	goalID := "goal-archived-1"
	userID := "user-archived-1"

	contribRepo := new(mockSavingsContributionRepo)
	handler := NewContributionHandler(contribRepo, nil, nil)

	archivedAt := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	archived := makeTestContribution(goalID)
	archived.ID = "contrib-archived"
	archived.ArchivedAt = &archivedAt

	live := makeTestContribution(goalID)
	live.ID = "contrib-live"
	live.ArchivedAt = nil

	contribRepo.On("ListByGoal", mock.Anything, userID, goalID).
		Return([]*domain.SavingsGoalContribution{archived, live}, nil)

	ctx := context.Background()
	input := &ListContributionsInput{
		AuthenticatedInput: authenticatedInput(userID),
		GoalID:             goalID,
	}

	output, err := handler.ListContributions(ctx, input)
	require.NoError(t, err)
	require.Len(t, output.Body, 2)

	wire, err := json.Marshal(output.Body)
	require.NoError(t, err)

	var parsed []map[string]interface{}
	require.NoError(t, json.Unmarshal(wire, &parsed))
	require.Len(t, parsed, 2)

	archivedJSON := parsed[0]
	liveJSON := parsed[1]

	rawArchivedAt, present := archivedJSON["archived_at"]
	require.True(t, present, "archived_at must reach the response for a closed-period row")
	parsedTime, err := time.Parse(time.RFC3339, rawArchivedAt.(string))
	require.NoError(t, err)
	assert.True(t, archivedAt.Equal(parsedTime), "archived_at value must round-trip through JSON")

	_, present = liveJSON["archived_at"]
	assert.False(t, present, "archived_at must be OMITTED (not null) for a row still in the live ledger")

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
	handler := NewContributionHandler(contribRepo, progress, nil)

	contribRepo.On("Insert", mock.Anything, userID, mock.MatchedBy(func(c *domain.SavingsGoalContribution) bool {
		return c.SavingsGoalID == goalID &&
			c.Amount == 500.00 &&
			c.Source == "manual" &&
			c.Status == "confirmed" &&
			c.ConfirmedByUser &&
			!c.ManagedByReconciliation
	})).Return(nil)
	progress.On("RecomputeGoalByID", mock.Anything, userID, goalID).Return(nil)

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
	handler := NewContributionHandler(contribRepo, nil, nil)

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
	handler := NewContributionHandler(contribRepo, nil, nil)

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
	handler := NewContributionHandler(contribRepo, progress, nil)

	confirmed := makeTestContribution(goalID)
	confirmed.ID = contribID
	confirmed.Status = "confirmed"
	confirmed.ConfirmedByUser = true

	contribRepo.On("ConfirmContribution", mock.Anything, userID, goalID, contribID).
		Return(confirmed, nil)
	progress.On("RecomputeGoalByID", mock.Anything, userID, goalID).Return(nil)

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
	handler := NewContributionHandler(contribRepo, nil, nil)

	contribRepo.On("ConfirmContribution", mock.Anything, userID, goalID, contribID).
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
	handler := NewContributionHandler(contribRepo, nil, nil)

	contribRepo.On("ConfirmContribution", mock.Anything, userID, goalID, contribID).
		Return(nil, domain.ErrContributionAlreadyConfirmed)

	ctx := context.Background()
	input := &ConfirmContributionInput{
		AuthenticatedInput: authenticatedInput(userID),
		GoalID:             goalID,
		ID:                 contribID,
	}

	_, err := handler.ConfirmContribution(ctx, input)
	require.Error(t, err)

	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr, "expected a huma.StatusError, got %T: %v", err, err)
	assert.Equal(t, http.StatusConflict, statusErr.GetStatus())
}

// TestConfirmContribution_AlreadyRejected_Returns409 verifies #1625 Task 9: confirming a
// contribution the user already rejected (a sticky tombstone, R3-7) must surface as a 409, not
// fall through to the default LoggingMapError branch (which would 500 — the exact defect class
// this ticket was opened to close).
func TestConfirmContribution_AlreadyRejected_Returns409(t *testing.T) {
	goalID := "goal-rejected"
	userID := "user-rejected"
	contribID := "contrib-rejected"

	contribRepo := new(mockSavingsContributionRepo)
	handler := NewContributionHandler(contribRepo, nil, nil)

	contribRepo.On("ConfirmContribution", mock.Anything, userID, goalID, contribID).
		Return(nil, domain.ErrContributionAlreadyRejected)

	ctx := context.Background()
	input := &ConfirmContributionInput{
		AuthenticatedInput: authenticatedInput(userID),
		GoalID:             goalID,
		ID:                 contribID,
	}

	_, err := handler.ConfirmContribution(ctx, input)
	require.Error(t, err)

	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr, "expected a huma.StatusError, got %T: %v", err, err)
	assert.Equal(t, http.StatusConflict, statusErr.GetStatus())
}

// TestConfirmContribution_OverAllocated_Returns422 verifies ErrContributionOverAllocated → 422.
func TestConfirmContribution_OverAllocated_Returns422(t *testing.T) {
	goalID := "goal-overalloc"
	userID := "user-overalloc"
	contribID := "contrib-overalloc"

	contribRepo := new(mockSavingsContributionRepo)
	handler := NewContributionHandler(contribRepo, nil, nil)

	contribRepo.On("ConfirmContribution", mock.Anything, userID, goalID, contribID).
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
	handler := NewContributionHandler(contribRepo, progress, nil)
	// #1569 r17 t11: DeleteContribution refuses outright when the audit sink is unwired, so the
	// success path must wire one. See TestDeleteContribution_RefusesWithoutAuditSink.
	handler.SetAuditLogRepository(&stubAuditLogRepository{})

	contribRepo.On("DeleteContribution", mock.Anything, userID, goalID, contribID, mock.Anything).
		Return(nil)
	progress.On("RecomputeGoalByID", mock.Anything, userID, goalID).Return(nil)

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
	handler := NewContributionHandler(contribRepo, nil, nil)
	handler.SetAuditLogRepository(&stubAuditLogRepository{})

	contribRepo.On("DeleteContribution", mock.Anything, userID, goalID, contribID, mock.Anything).
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
// Tests — R3-2: RecomputeGoal (not RecomputeUser) is called after writes
// =============================================================================

// TestLogContribution_RecomputesSpecificGoal_NotUser verifies that after a manual log
// the handler calls RecomputeGoal(userID, goalID) rather than RecomputeUser(userID).
// This ensures completed goals are also recomputed (R3-2).
func TestLogContribution_RecomputesSpecificGoal_NotUser(t *testing.T) {
	goalID := "goal-recompute-goal"
	userID := "user-recompute"

	contribRepo := new(mockSavingsContributionRepo)
	progress := new(mockContributionProgressRecomputer)
	goalReader := new(mockSavingsGoalReader)
	handler := NewContributionHandler(contribRepo, progress, goalReader)

	goal := &domain.SavingsGoal{
		ID:            goalID,
		UserID:        userID,
		ProgressBasis: domain.ProgressBasisContributions,
	}
	goalReader.On("GetByIDForUser", mock.Anything, goalID, userID).Return(goal, nil)
	contribRepo.On("Insert", mock.Anything, userID, mock.Anything).Return(nil)
	// Must be called with goalID, NOT just userID
	progress.On("RecomputeGoalByID", mock.Anything, userID, goalID).Return(nil)

	ctx := context.Background()
	input := &LogContributionInput{
		AuthenticatedInput: authenticatedInput(userID),
		GoalID:             goalID,
		Body: domain.LogContributionRequest{
			Amount:     200.00,
			OccurredOn: "2026-06-14",
		},
	}

	_, err := handler.LogContribution(ctx, input)
	require.NoError(t, err)
	progress.AssertExpectations(t)
}

// =============================================================================
// Tests — R3-3: Manual intake gated on contributions basis (C8)
// =============================================================================

// TestLogContribution_NonContributionsBasis_Returns422 verifies that a manual log
// for a goal with progress_basis != 'contributions' is rejected with 422 (C8 gate).
func TestLogContribution_NonContributionsBasis_Returns422(t *testing.T) {
	goalID := "goal-manual-basis"
	userID := "user-manual-basis"

	contribRepo := new(mockSavingsContributionRepo)
	goalReader := new(mockSavingsGoalReader)
	handler := NewContributionHandler(contribRepo, nil, goalReader)

	// Goal has manual basis — contributions not tracked here.
	goal := &domain.SavingsGoal{
		ID:            goalID,
		UserID:        userID,
		ProgressBasis: domain.ProgressBasisManual,
	}
	goalReader.On("GetByIDForUser", mock.Anything, goalID, userID).Return(goal, nil)

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
	humaErr, ok := err.(huma.StatusError)
	require.True(t, ok)
	assert.Equal(t, http.StatusUnprocessableEntity, humaErr.GetStatus(),
		"manual log on non-contributions goal must return 422")
	contribRepo.AssertNotCalled(t, "Insert")
}

// TestLogContribution_BalanceBasis_Returns422 verifies that balance-basis goals
// are also gated (completeness check for C8).
func TestLogContribution_BalanceBasis_Returns422(t *testing.T) {
	goalID := "goal-balance-basis"
	userID := "user-balance-basis"

	contribRepo := new(mockSavingsContributionRepo)
	goalReader := new(mockSavingsGoalReader)
	handler := NewContributionHandler(contribRepo, nil, goalReader)

	goal := &domain.SavingsGoal{
		ID:            goalID,
		UserID:        userID,
		ProgressBasis: domain.ProgressBasisBalance,
	}
	goalReader.On("GetByIDForUser", mock.Anything, goalID, userID).Return(goal, nil)

	ctx := context.Background()
	input := &LogContributionInput{
		AuthenticatedInput: authenticatedInput(userID),
		GoalID:             goalID,
		Body: domain.LogContributionRequest{
			Amount:     75.00,
			OccurredOn: "2026-06-14",
		},
	}

	_, err := handler.LogContribution(ctx, input)
	require.Error(t, err)
	humaErr, ok := err.(huma.StatusError)
	require.True(t, ok)
	assert.Equal(t, http.StatusUnprocessableEntity, humaErr.GetStatus())
	contribRepo.AssertNotCalled(t, "Insert")
}

// =============================================================================
// Tests — R3-1: Txn-linked manual log is capacity-safe (C2)
// =============================================================================

// TestLogContribution_TxnLinked_OverAllocated_Returns422 verifies that when a manual
// contribution includes a transaction_id, an over-allocation returns 422 (C2 / R3-1 / R4-1).
// R4-1: uses InsertManualFromTransaction (not UpsertFromTransaction).
func TestLogContribution_TxnLinked_OverAllocated_Returns422(t *testing.T) {
	goalID := "goal-txn-overalloc"
	userID := "user-txn-overalloc"
	txnID := "txn-overalloc"

	contribRepo := new(mockSavingsContributionRepo)
	goalReader := new(mockSavingsGoalReader)
	handler := NewContributionHandler(contribRepo, nil, goalReader)

	goal := &domain.SavingsGoal{
		ID:            goalID,
		UserID:        userID,
		ProgressBasis: domain.ProgressBasisContributions,
	}
	goalReader.On("GetByIDForUser", mock.Anything, goalID, userID).Return(goal, nil)

	// R4-1: InsertManualFromTransaction (not UpsertFromTransaction) handles txn-linked manual logs.
	contribRepo.On("InsertManualFromTransaction", mock.Anything, userID, goalID, txnID, 999.00, "2026-06-14").
		Return(nil, domain.ErrContributionOverAllocated)

	ctx := context.Background()
	txnIDStr := txnID
	input := &LogContributionInput{
		AuthenticatedInput: authenticatedInput(userID),
		GoalID:             goalID,
		Body: domain.LogContributionRequest{
			Amount:        999.00,
			OccurredOn:    "2026-06-14",
			TransactionID: &txnIDStr,
		},
	}

	_, err := handler.LogContribution(ctx, input)
	require.Error(t, err)
	humaErr, ok := err.(huma.StatusError)
	require.True(t, ok)
	assert.Equal(t, http.StatusUnprocessableEntity, humaErr.GetStatus(),
		"over-allocated txn-linked manual log must return 422")
	contribRepo.AssertNotCalled(t, "Insert")
	contribRepo.AssertNotCalled(t, "UpsertFromTransaction")
}

// TestLogContribution_TxnLinked_Success uses InsertManualFromTransaction (R4-1: capacity-safe,
// manual semantics). Verifies the response returns the REAL persisted row ID (not a fabricated uuid).
func TestLogContribution_TxnLinked_Success(t *testing.T) {
	goalID := "goal-txn-ok"
	userID := "user-txn-ok"
	txnID := "txn-ok"
	realRowID := "real-db-row-id"

	contribRepo := new(mockSavingsContributionRepo)
	progress := new(mockContributionProgressRecomputer)
	goalReader := new(mockSavingsGoalReader)
	handler := NewContributionHandler(contribRepo, progress, goalReader)

	goal := &domain.SavingsGoal{
		ID:            goalID,
		UserID:        userID,
		ProgressBasis: domain.ProgressBasisContributions,
	}
	// The repo returns the real persisted row (R4-1: not a fabricated uuid.New() struct).
	persistedRow := &domain.SavingsGoalContribution{
		ID:                      realRowID,
		SavingsGoalID:           goalID,
		Amount:                  100.00,
		Source:                  "manual",
		Status:                  "confirmed",
		ManagedByReconciliation: false,
		ConfirmedByUser:         true,
		OccurredOn:              "2025-12-01",
	}
	goalReader.On("GetByIDForUser", mock.Anything, goalID, userID).Return(goal, nil)
	// R4-1: InsertManualFromTransaction (not UpsertFromTransaction) for txn-linked manual logs.
	contribRepo.On("InsertManualFromTransaction", mock.Anything, userID, goalID, txnID, 100.00, "2025-12-01").
		Return(persistedRow, nil)
	progress.On("RecomputeGoalByID", mock.Anything, userID, goalID).Return(nil)

	ctx := context.Background()
	txnIDStr := txnID
	input := &LogContributionInput{
		AuthenticatedInput: authenticatedInput(userID),
		GoalID:             goalID,
		Body: domain.LogContributionRequest{
			Amount:        100.00,
			OccurredOn:    "2025-12-01",
			TransactionID: &txnIDStr,
		},
	}

	output, err := handler.LogContribution(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, output)
	// R4-1: ID must equal the real DB row ID, not a random uuid.
	assert.Equal(t, realRowID, output.Body.ID, "response ID must be the real persisted row ID")
	// R4-1: OccurredOn must equal the caller's date, not CURRENT_DATE.
	assert.Equal(t, "2025-12-01", output.Body.OccurredOn, "OccurredOn must reflect the request date")
	// R4-1 C3: managed_by_reconciliation must be false for manual rows.
	assert.False(t, output.Body.ManagedByReconciliation, "manual log must not be reconciler-owned")
	// R4-1: confirmed_by_user must be true.
	assert.True(t, output.Body.ConfirmedByUser, "manual log must have confirmed_by_user=true")
	contribRepo.AssertNotCalled(t, "Insert")
	contribRepo.AssertNotCalled(t, "UpsertFromTransaction")
	contribRepo.AssertExpectations(t)
	progress.AssertExpectations(t)
}

// TestLogContribution_TxnLinked_AlreadyConfirmed_Returns422 verifies that when
// InsertManualFromTransaction returns ErrContributionAlreadyConfirmed (the ON CONFLICT
// WHERE status='proposed' predicate was false — an existing confirmed/rejected row
// occupies the slot), the handler returns 422 rather than a 500. (Codex R4-1 follow-up)
func TestLogContribution_TxnLinked_AlreadyConfirmed_Returns422(t *testing.T) {
	goalID := "goal-already-confirmed"
	userID := "user-already-confirmed"
	txnID := "txn-already-confirmed"

	contribRepo := new(mockSavingsContributionRepo)
	goalReader := new(mockSavingsGoalReader)
	handler := NewContributionHandler(contribRepo, nil, goalReader)

	goal := &domain.SavingsGoal{
		ID:            goalID,
		UserID:        userID,
		ProgressBasis: domain.ProgressBasisContributions,
	}
	goalReader.On("GetByIDForUser", mock.Anything, goalID, userID).Return(goal, nil)
	contribRepo.On("InsertManualFromTransaction", mock.Anything, userID, goalID, txnID, 50.00, "2026-06-14").
		Return(nil, domain.ErrContributionAlreadyConfirmed)

	ctx := context.Background()
	txnIDStr := txnID
	input := &LogContributionInput{
		AuthenticatedInput: authenticatedInput(userID),
		GoalID:             goalID,
		Body: domain.LogContributionRequest{
			Amount:        50.00,
			OccurredOn:    "2026-06-14",
			TransactionID: &txnIDStr,
		},
	}

	_, err := handler.LogContribution(ctx, input)
	require.Error(t, err)
	humaErr, ok := err.(huma.StatusError)
	require.True(t, ok)
	assert.Equal(t, http.StatusUnprocessableEntity, humaErr.GetStatus(),
		"already-confirmed slot must return 422, not 500")
}

// TestLogContribution_NoTxn_Success uses Insert (unlinked manual log — no capacity to consume).
func TestLogContribution_NoTxn_Success(t *testing.T) {
	goalID := "goal-notxn"
	userID := "user-notxn"

	contribRepo := new(mockSavingsContributionRepo)
	goalReader := new(mockSavingsGoalReader)
	handler := NewContributionHandler(contribRepo, nil, goalReader)

	goal := &domain.SavingsGoal{
		ID:            goalID,
		UserID:        userID,
		ProgressBasis: domain.ProgressBasisContributions,
	}
	goalReader.On("GetByIDForUser", mock.Anything, goalID, userID).Return(goal, nil)
	contribRepo.On("Insert", mock.Anything, userID, mock.MatchedBy(func(c *domain.SavingsGoalContribution) bool {
		return c.SavingsGoalID == goalID && c.Amount == 150.00
	})).Return(nil)

	ctx := context.Background()
	input := &LogContributionInput{
		AuthenticatedInput: authenticatedInput(userID),
		GoalID:             goalID,
		Body: domain.LogContributionRequest{
			Amount:     150.00,
			OccurredOn: "2026-06-14",
		},
	}

	output, err := handler.LogContribution(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, output)
	contribRepo.AssertNotCalled(t, "UpsertFromTransaction")
	contribRepo.AssertExpectations(t)
}

// =============================================================================
// Tests — RejectContribution (POST /reject)
// =============================================================================

// TestRejectContribution_Success verifies that a proposed contribution can be rejected
// (status→'rejected') and progress is recomputed (R3-7).
func TestRejectContribution_Success(t *testing.T) {
	goalID := "goal-reject-1"
	userID := "user-reject-1"
	contribID := "contrib-reject-1"

	contribRepo := new(mockSavingsContributionRepo)
	progress := new(mockContributionProgressRecomputer)
	handler := NewContributionHandler(contribRepo, progress, nil)

	rejected := makeTestContribution(goalID)
	rejected.ID = contribID
	rejected.Status = "rejected"

	contribRepo.On("RejectContribution", mock.Anything, userID, goalID, contribID).
		Return(rejected, nil)
	progress.On("RecomputeGoalByID", mock.Anything, userID, goalID).Return(nil)

	ctx := context.Background()
	input := &RejectContributionInput{
		AuthenticatedInput: authenticatedInput(userID),
		GoalID:             goalID,
		ID:                 contribID,
	}

	output, err := handler.RejectContribution(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "rejected", output.Body.Status)
	contribRepo.AssertExpectations(t)
	progress.AssertExpectations(t)
}

// TestRejectContribution_NotFound_Returns404 verifies ErrContributionNotFound → 404.
func TestRejectContribution_NotFound_Returns404(t *testing.T) {
	goalID := "goal-rjnf"
	userID := "user-rjnf"
	contribID := "contrib-rjnf"

	contribRepo := new(mockSavingsContributionRepo)
	handler := NewContributionHandler(contribRepo, nil, nil)

	contribRepo.On("RejectContribution", mock.Anything, userID, goalID, contribID).
		Return(nil, domain.ErrContributionNotFound)

	ctx := context.Background()
	input := &RejectContributionInput{
		AuthenticatedInput: authenticatedInput(userID),
		GoalID:             goalID,
		ID:                 contribID,
	}

	_, err := handler.RejectContribution(ctx, input)
	require.Error(t, err)
	humaErr, ok := err.(huma.StatusError)
	require.True(t, ok)
	assert.Equal(t, http.StatusNotFound, humaErr.GetStatus())
}

// TestRejectContribution_NotProposed_Returns409 verifies ErrContributionNotProposed → 409.
func TestRejectContribution_NotProposed_Returns409(t *testing.T) {
	goalID := "goal-rjnp"
	userID := "user-rjnp"
	contribID := "contrib-rjnp"

	contribRepo := new(mockSavingsContributionRepo)
	handler := NewContributionHandler(contribRepo, nil, nil)

	contribRepo.On("RejectContribution", mock.Anything, userID, goalID, contribID).
		Return(nil, domain.ErrContributionNotProposed)

	ctx := context.Background()
	input := &RejectContributionInput{
		AuthenticatedInput: authenticatedInput(userID),
		GoalID:             goalID,
		ID:                 contribID,
	}

	_, err := handler.RejectContribution(ctx, input)
	require.Error(t, err)
	humaErr, ok := err.(huma.StatusError)
	require.True(t, ok)
	assert.Equal(t, http.StatusConflict, humaErr.GetStatus())
}

// =============================================================================
// Helper — build an AuthenticatedInput with a specific userID
// =============================================================================

// authenticatedInput builds an AuthenticatedInput with the given userID.
// Reuses the same helper pattern as existing operations tests.
func authenticatedInput(userID string) inputs.AuthenticatedInput {
	return inputs.AuthenticatedInput{UserID: userID}
}

// stubAuditLogRepository is a non-nil repository.AuditLogRepository for handler tests that are not
// about the audit trail itself. #1569 round 17 task 11 made a nil sink a hard refusal, so "no audit
// sink" is no longer a usable default in a test that expects the delete to reach the repository.
type stubAuditLogRepository struct{}

func (stubAuditLogRepository) Log(context.Context, string, string, string) error { return nil }
func (stubAuditLogRepository) LogInTx(context.Context, pgx.Tx, string, string, string) error {
	return nil
}

// TestDeleteContribution_RefusesWithoutAuditSink pins the fail-loud half of ADR control 2 at the
// handler: with no audit repository wired — the exact state a dropped SetAuditLogRepository line in
// cmd/server/main.go produces — the contribution is NOT destroyed un-audited. The repository is
// never reached.
func TestDeleteContribution_RefusesWithoutAuditSink(t *testing.T) {
	contribRepo := new(mockSavingsContributionRepo)
	handler := NewContributionHandler(contribRepo, nil, nil)

	_, err := handler.DeleteContribution(context.Background(), &DeleteContributionInput{
		AuthenticatedInput: authenticatedInput("user-noaudit"),
		GoalID:             "goal-noaudit",
		ID:                 "contrib-noaudit",
	})

	require.Error(t, err)
	mockassert.NoCallTo(t, &contribRepo.Mock, "DeleteContribution")
}

// Codex gap analysis C3: the existing test asserts the 422 status only, which would still pass
// with the old bare "basis mismatch" string. That message named neither the offending value nor
// the remedy, so an agent hitting it on a brand-new goal had no way to proceed — which is how
// #1625 was reported in the first place. This pins the CONTENT.
func TestLogContribution_BasisMismatchMessageNamesTheRemedy(t *testing.T) {
	goalID := "goal-remedy"
	userID := "user-remedy"

	contribRepo := new(mockSavingsContributionRepo)
	goalReader := new(mockSavingsGoalReader)
	handler := NewContributionHandler(contribRepo, nil, goalReader)

	goalReader.On("GetByIDForUser", mock.Anything, goalID, userID).Return(&domain.SavingsGoal{
		ID:            goalID,
		UserID:        userID,
		ProgressBasis: domain.ProgressBasisManual,
	}, nil)

	_, err := handler.LogContribution(context.Background(), &LogContributionInput{
		AuthenticatedInput: authenticatedInput(userID),
		GoalID:             goalID,
		Body:               domain.LogContributionRequest{Amount: 100, OccurredOn: "2026-06-14"},
	})

	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, domain.ProgressBasisManual, "the message must name the goal's CURRENT basis")
	assert.Contains(t, msg, domain.ProgressBasisContributions, "and the basis it needs")
	assert.Contains(t, msg, goalID, "and the goal to change, so the fix is a copyable call")
}

// #1625 Task 10: Confirm and Reject need the basis gate that LogContribution has.
//
// On a goal switched off the contributions basis they returned 200 while RecomputeGoalByID
// early-returned (savings_progress_service.go) — the row changed status, the goal's
// current_amount did not, and the caller was told it worked. A silent no-op reported as success
// is the exact defect shape #1625 opened with.
//
// ⚠ DeleteContribution deliberately does NOT get this guard. Not because the rows would be
// "permanently undeletable" — an adversarial review correctly pointed out that a user could switch
// the goal back to the contributions basis and delete them then. The real reason is narrower:
// gating Delete would make removing your own row require an unrelated, side-effecting change to
// the goal's configuration first. Confirm and Reject genuinely cannot take effect on a manual
// goal; a delete does not have that failure mode. (⚠ "always does exactly what it says" was
// FALSIFIED by the cross-goal DELETE bug #1625 fixed — see savings_goal_contribution.go's
// assertLedgerIsLive comment.)
// TestDeleteContribution_IsNotBlockedByBasis pins that.
func TestConfirmContribution_RefusesOnANonContributionsBasis(t *testing.T) {
	goalID, userID := "goal-manual", "user-1"

	contribRepo := new(mockSavingsContributionRepo)
	goalReader := new(mockSavingsGoalReader)
	handler := NewContributionHandler(contribRepo, nil, goalReader)

	goalReader.On("GetByIDForUser", mock.Anything, goalID, userID).Return(&domain.SavingsGoal{
		ID: goalID, UserID: userID, ProgressBasis: domain.ProgressBasisManual,
	}, nil)

	_, err := handler.ConfirmContribution(context.Background(), &ConfirmContributionInput{
		AuthenticatedInput: authenticatedInput(userID),
		GoalID:             goalID,
		ID:                 "contrib-1",
	})

	require.Error(t, err, "a confirm that cannot change the goal must not report success")
	assert.Contains(t, err.Error(), domain.ProgressBasisContributions,
		"the message must name the remedy, as LogContribution's does")
	contribRepo.AssertNotCalled(t, "ConfirmContribution", mock.Anything, mock.Anything, mock.Anything)
}

func TestRejectContribution_RefusesOnANonContributionsBasis(t *testing.T) {
	goalID, userID := "goal-manual", "user-1"

	contribRepo := new(mockSavingsContributionRepo)
	goalReader := new(mockSavingsGoalReader)
	handler := NewContributionHandler(contribRepo, nil, goalReader)

	goalReader.On("GetByIDForUser", mock.Anything, goalID, userID).Return(&domain.SavingsGoal{
		ID: goalID, UserID: userID, ProgressBasis: domain.ProgressBasisManual,
	}, nil)

	_, err := handler.RejectContribution(context.Background(), &RejectContributionInput{
		AuthenticatedInput: authenticatedInput(userID),
		GoalID:             goalID,
		ID:                 "contrib-1",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), domain.ProgressBasisContributions)
	contribRepo.AssertNotCalled(t, "RejectContribution", mock.Anything, mock.Anything, mock.Anything)
}

// The deliberate asymmetry. A contribution on a goal that has since moved to the manual basis must
// still be destroyable, or the basis switch becomes a one-way trap for the rows it leaves behind.
func TestDeleteContribution_IsNotBlockedByBasis(t *testing.T) {
	goalID, userID := "goal-manual", "user-1"

	contribRepo := new(mockSavingsContributionRepo)
	goalReader := new(mockSavingsGoalReader)
	handler := NewContributionHandler(contribRepo, nil, goalReader)
	handler.SetAuditLogRepository(&stubAuditLogRepository{})

	goalReader.On("GetByIDForUser", mock.Anything, goalID, userID).Return(&domain.SavingsGoal{
		ID: goalID, UserID: userID, ProgressBasis: domain.ProgressBasisManual,
	}, nil)
	contribRepo.On("DeleteContribution", mock.Anything, userID, goalID, "contrib-1", mock.Anything).Return(nil)

	_, err := handler.DeleteContribution(context.Background(), &DeleteContributionInput{
		AuthenticatedInput: authenticatedInput(userID),
		GoalID:             goalID,
		ID:                 "contrib-1",
	})

	require.NoError(t, err,
		"a row left behind by a basis switch must stay deletable, or the switch traps it forever")
	contribRepo.AssertCalled(t, "DeleteContribution", mock.Anything, userID, goalID, "contrib-1", mock.Anything)
}
