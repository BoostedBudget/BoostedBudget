package operations

// Handler tests for GET /api/v1/accounts/{id}/balance-history (Issue #1569).

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
)

type mockAccountBalanceHistoryService struct {
	history *domain.AccountBalanceHistory
	err     error

	gotUserID    string
	gotAccountID string
	gotStart     *time.Time
	gotEnd       *time.Time
	calls        int
}

func (m *mockAccountBalanceHistoryService) GetAccountBalanceHistory(
	_ context.Context, userID, accountID string, start, end *time.Time,
) (*domain.AccountBalanceHistory, error) {
	m.calls++
	m.gotUserID, m.gotAccountID, m.gotStart, m.gotEnd = userID, accountID, start, end
	return m.history, m.err
}

func TestAccountBalanceHistoryHandler_Success_PassesTenancyAndBoundsThrough(t *testing.T) {
	svc := &mockAccountBalanceHistoryService{history: &domain.AccountBalanceHistory{
		AccountID: validAccountUUID,
		Points: []domain.AccountBalanceHistoryPoint{
			{Date: "2026-08-19", CurrencyCode: "USD"},
			{Date: "2026-08-20", CurrencyCode: "USD"},
		},
	}}
	handler := NewAccountBalanceHistoryHandler(svc)

	out, err := handler.GetAccountBalanceHistory(context.Background(), &GetAccountBalanceHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 validAccountUUID,
		StartDate:          "2026-08-19",
		EndDate:            "2026-08-20",
	})

	require.NoError(t, err)
	assert.Equal(t, "user-1", svc.gotUserID, "the caller's own id, never a value from the request")
	assert.Equal(t, validAccountUUID, svc.gotAccountID)
	require.NotNil(t, svc.gotStart)
	assert.Equal(t, time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC), *svc.gotStart)
	require.NotNil(t, svc.gotEnd)
	assert.Equal(t, time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC), *svc.gotEnd)
	require.Len(t, out.Body.Points, 2)
}

func TestAccountBalanceHistoryHandler_OmittedBoundsAreUnbounded(t *testing.T) {
	svc := &mockAccountBalanceHistoryService{history: &domain.AccountBalanceHistory{AccountID: validAccountUUID}}
	handler := NewAccountBalanceHistoryHandler(svc)

	_, err := handler.GetAccountBalanceHistory(context.Background(), &GetAccountBalanceHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 validAccountUUID,
	})

	require.NoError(t, err)
	assert.Nil(t, svc.gotStart, "an omitted bound must reach the repository as NULL, not as year zero")
	assert.Nil(t, svc.gotEnd)
}

func TestAccountBalanceHistoryHandler_InvalidAccountID_Is400(t *testing.T) {
	svc := &mockAccountBalanceHistoryService{}
	handler := NewAccountBalanceHistoryHandler(svc)

	_, err := handler.GetAccountBalanceHistory(context.Background(), &GetAccountBalanceHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "not-a-uuid",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid account id")
	assert.Zero(t, svc.calls, "a malformed id must not reach a ::uuid cast and surface as a 500")
}

// Both bounds go into a ::date cast. Validating them here is what keeps a typo a 400 instead of a
// Postgres error rendered as an internal server error — and time.Parse, not a shape regex, is what
// rejects an impossible calendar date.
func TestAccountBalanceHistoryHandler_MalformedBounds_Are400(t *testing.T) {
	bad := []string{"20-08-2026", "2026-8-1", "yesterday", "2026-08-20T00:00:00Z", "2026-08-20; DROP", "2026-02-30", "2026-13-01", "2026-00-15", "2026-04-31"}
	for _, v := range bad {
		for _, field := range []string{"start_date", "end_date"} {
			svc := &mockAccountBalanceHistoryService{}
			handler := NewAccountBalanceHistoryHandler(svc)
			in := &GetAccountBalanceHistoryInput{
				AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
				ID:                 validAccountUUID,
			}
			if field == "start_date" {
				in.StartDate = v
			} else {
				in.EndDate = v
			}

			_, err := handler.GetAccountBalanceHistory(context.Background(), in)
			require.Error(t, err, "%s=%q must be refused", field, v)
			assert.Contains(t, err.Error(), "invalid "+field, "%s=%q", field, v)
			assert.Zero(t, svc.calls, "%s=%q must not reach the service", field, v)
		}
	}
}

// An inverted window can only ever come back empty, and an empty series on this screen reads as
// "nothing was retained" — a false statement about the user's money.
func TestAccountBalanceHistoryHandler_InvertedRange_Is400(t *testing.T) {
	svc := &mockAccountBalanceHistoryService{}
	handler := NewAccountBalanceHistoryHandler(svc)

	_, err := handler.GetAccountBalanceHistory(context.Background(), &GetAccountBalanceHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 validAccountUUID,
		StartDate:          "2026-08-20",
		EndDate:            "2026-08-19",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "end_date must not be before start_date")
	assert.Zero(t, svc.calls)
}

// A single-day window is NOT inverted and must be served.
func TestAccountBalanceHistoryHandler_SingleDayRangeIsAllowed(t *testing.T) {
	svc := &mockAccountBalanceHistoryService{history: &domain.AccountBalanceHistory{AccountID: validAccountUUID}}
	handler := NewAccountBalanceHistoryHandler(svc)

	_, err := handler.GetAccountBalanceHistory(context.Background(), &GetAccountBalanceHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 validAccountUUID,
		StartDate:          "2026-08-20",
		EndDate:            "2026-08-20",
	})

	require.NoError(t, err)
	assert.Equal(t, 1, svc.calls)
}

// An account we kept nothing for is a 200 with an empty series, never a 404. The screen this feeds
// exists to say nothing was destroyed; a 404 would render there as a failure.
func TestAccountBalanceHistoryHandler_EmptyHistoryIs200(t *testing.T) {
	svc := &mockAccountBalanceHistoryService{history: &domain.AccountBalanceHistory{
		AccountID: validAccountUUID,
		Points:    []domain.AccountBalanceHistoryPoint{},
	}}
	handler := NewAccountBalanceHistoryHandler(svc)

	out, err := handler.GetAccountBalanceHistory(context.Background(), &GetAccountBalanceHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 validAccountUUID,
	})

	require.NoError(t, err)
	require.NotNil(t, out.Body)
	assert.Empty(t, out.Body.Points)
}

func TestAccountBalanceHistoryHandler_ServiceError_IsNot200(t *testing.T) {
	handler := NewAccountBalanceHistoryHandler(&mockAccountBalanceHistoryService{err: errors.New("db down")})

	_, err := handler.GetAccountBalanceHistory(context.Background(), &GetAccountBalanceHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 validAccountUUID,
	})

	require.Error(t, err, "a failed read must never fall through to an empty series")
}

func TestAccountBalanceHistoryHandler_Unauthorized(t *testing.T) {
	_, api := humatest.New(t)
	RegisterAccountBalanceHistoryRoutes(api, NewAccountBalanceHistoryHandler(&mockAccountBalanceHistoryService{}))

	resp := api.Get("/api/v1/accounts/" + validAccountUUID + "/balance-history")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)
}
