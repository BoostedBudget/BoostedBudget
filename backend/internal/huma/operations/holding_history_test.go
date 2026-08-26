package operations

// Handler tests for GET /api/v1/investments/holdings/history (Issue #1569).

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
)

type mockHoldingHistoryService struct {
	history *domain.HoldingSnapshotHistory
	err     error

	gotUserID    string
	gotAccountID string
	gotAsOf      string
	calls        int
}

func (m *mockHoldingHistoryService) GetAccountHoldingsHistory(_ context.Context, userID, accountID, asOf string) (*domain.HoldingSnapshotHistory, error) {
	m.calls++
	m.gotUserID, m.gotAccountID, m.gotAsOf = userID, accountID, asOf
	return m.history, m.err
}

const validAccountUUID = "0f6a5a4e-3a1c-4c3a-9d2f-1b7c8e5a2d10"

func TestHoldingHistoryHandler_Success_PassesTenancyAndDayThrough(t *testing.T) {
	day := "2026-08-20"
	svc := &mockHoldingHistoryService{history: &domain.HoldingSnapshotHistory{
		AccountID:      validAccountUUID,
		AsOfDate:       &day,
		AvailableDates: []string{day},
		Positions:      []domain.HoldingSnapshotView{{SecurityID: "sec-1", AsOfDate: day, Quantity: 4}},
	}}
	handler := NewHoldingHistoryHandler(svc)

	out, err := handler.GetAccountHoldingsHistory(context.Background(), &GetAccountHoldingsHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		AccountID:          validAccountUUID,
		AsOf:               day,
	})

	require.NoError(t, err)
	assert.Equal(t, "user-1", svc.gotUserID, "the caller's own id, never a value from the query string")
	assert.Equal(t, validAccountUUID, svc.gotAccountID)
	assert.Equal(t, day, svc.gotAsOf)
	require.Len(t, out.Body.Positions, 1)
}

func TestHoldingHistoryHandler_InvalidAccountID_Is400(t *testing.T) {
	svc := &mockHoldingHistoryService{}
	handler := NewHoldingHistoryHandler(svc)

	_, err := handler.GetAccountHoldingsHistory(context.Background(), &GetAccountHoldingsHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		AccountID:          "not-a-uuid",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid account_id")
	assert.Zero(t, svc.calls, "a malformed id must not reach a ::uuid cast and surface as a 500")
}

// as_of goes into a ::date cast. Validating its shape here is what keeps a typo a 400 instead of
// a Postgres error rendered as an internal server error.
func TestHoldingHistoryHandler_MalformedAsOf_Is400(t *testing.T) {
	for _, bad := range []string{"20-08-2026", "2026-8-1", "yesterday", "2026-08-20T00:00:00Z", "2026-08-20; DROP"} {
		svc := &mockHoldingHistoryService{}
		handler := NewHoldingHistoryHandler(svc)

		_, err := handler.GetAccountHoldingsHistory(context.Background(), &GetAccountHoldingsHistoryInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
			AccountID:          validAccountUUID,
			AsOf:               bad,
		})

		require.Error(t, err, "as_of=%q must be refused", bad)
		assert.Contains(t, err.Error(), "invalid as_of", "as_of=%q", bad)
		assert.Zero(t, svc.calls, "as_of=%q must not reach the service", bad)
	}
}

func TestHoldingHistoryHandler_UnretainedDay_Is404(t *testing.T) {
	handler := NewHoldingHistoryHandler(&mockHoldingHistoryService{err: domain.ErrHoldingHistoryDateNotRetained})

	_, err := handler.GetAccountHoldingsHistory(context.Background(), &GetAccountHoldingsHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		AccountID:          validAccountUUID,
		AsOf:               "2020-01-01",
	})

	require.Error(t, err)
	var se interface{ GetStatus() int }
	require.ErrorAs(t, err, &se, "the refusal must carry an HTTP status, not fall through to a 500")
	assert.Equal(t, http.StatusNotFound, se.GetStatus())
}

func TestHoldingHistoryHandler_ServiceError_IsNotA404(t *testing.T) {
	handler := NewHoldingHistoryHandler(&mockHoldingHistoryService{err: errors.New("db down")})

	_, err := handler.GetAccountHoldingsHistory(context.Background(), &GetAccountHoldingsHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		AccountID:          validAccountUUID,
	})

	require.Error(t, err)
	var se interface{ GetStatus() int }
	if errors.As(err, &se) {
		assert.NotEqual(t, http.StatusNotFound, se.GetStatus(), "a dead database must not read as 'that day was never kept'")
	}
}

func TestHoldingHistoryHandler_Unauthorized(t *testing.T) {
	_, api := humatest.New(t)
	RegisterHoldingHistoryRoutes(api, NewHoldingHistoryHandler(&mockHoldingHistoryService{}))

	resp := api.Get("/api/v1/investments/holdings/history?account_id=" + validAccountUUID)
	assert.Equal(t, http.StatusUnauthorized, resp.Code)
}

// account_id is required:true, so the route must refuse a call that omits it rather than reaching
// a handler with an empty account id.
func TestHoldingHistoryHandler_MissingAccountID_IsRejectedByTheRoute(t *testing.T) {
	_, api := humatest.New(t)
	RegisterHoldingHistoryRoutes(api, NewHoldingHistoryHandler(&mockHoldingHistoryService{}))

	resp := api.Get("/api/v1/investments/holdings/history")
	assert.NotEqual(t, http.StatusOK, resp.Code)
}

// The route this endpoint MUST NOT become. GET /investments/holdings is the current-state read and
// GET /investments/holdings/history is the historical one; if a future edit collapsed them the
// registration below would clash or the literal would be shadowed.
func TestHoldingHistoryHandler_RegistersItsOwnPathDistinctFromTheLiveHoldingsRead(t *testing.T) {
	_, api := humatest.New(t)
	RegisterHoldingHistoryRoutes(api, NewHoldingHistoryHandler(&mockHoldingHistoryService{}))
	RegisterInvestmentRoutes(api, NewInvestmentHandler(&mockInvestmentService{}, &mockInvestmentAnalyticsService{}))

	// Both respond (401 without auth) — proving two distinct registered paths, not one shadowing
	// the other into a 404.
	assert.Equal(t, http.StatusUnauthorized, api.Get("/api/v1/investments/holdings").Code)
	assert.Equal(t, http.StatusUnauthorized, api.Get("/api/v1/investments/holdings/history?account_id="+validAccountUUID).Code)
}
