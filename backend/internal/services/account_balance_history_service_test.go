package services

// Unit tests for AccountBalanceHistoryService (Issue #1569).
//
// These pin the composition rules the service owns and the repository cannot: that tenancy
// reaches the one statement, that the bounds come from the ends of the series rather than being
// re-derived, that an empty history is an ANSWER rather than an error, and that the slice is
// never nil. The SQL itself is proved against real Postgres in internal/repository.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

type stubBalanceHistoryRepo struct {
	repository.BalanceSnapshotRepository // every other method panics if reached: nil embedded iface

	points []domain.AccountBalanceHistoryPoint
	err    error

	gotUserID    string
	gotAccountID string
	gotStart     *time.Time
	gotEnd       *time.Time
	calls        int
}

func (s *stubBalanceHistoryRepo) GetAccountBalanceHistory(
	_ context.Context, userID, accountID string, start, end *time.Time,
) ([]domain.AccountBalanceHistoryPoint, error) {
	s.calls++
	s.gotUserID, s.gotAccountID, s.gotStart, s.gotEnd = userID, accountID, start, end
	return s.points, s.err
}

func fptr(v float64) *float64 { return &v }

func TestAccountBalanceHistoryService_PassesTenancyAndBoundsToTheRepository(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	repo := &stubBalanceHistoryRepo{}
	svc := NewAccountBalanceHistoryService(repo)

	_, err := svc.GetAccountBalanceHistory(context.Background(), "user-1", "acct-1", &start, &end)
	require.NoError(t, err)

	assert.Equal(t, 1, repo.calls, "exactly one round trip: the AI tool-calling contract is one query per answer")
	assert.Equal(t, "user-1", repo.gotUserID, "the caller's user id must reach the statement that filters on it")
	assert.Equal(t, "acct-1", repo.gotAccountID)
	require.NotNil(t, repo.gotStart)
	assert.Equal(t, start, *repo.gotStart)
	require.NotNil(t, repo.gotEnd)
	assert.Equal(t, end, *repo.gotEnd)
}

func TestAccountBalanceHistoryService_UnboundedRangePassesNilThrough(t *testing.T) {
	repo := &stubBalanceHistoryRepo{}
	svc := NewAccountBalanceHistoryService(repo)

	_, err := svc.GetAccountBalanceHistory(context.Background(), "user-1", "acct-1", nil, nil)
	require.NoError(t, err)

	assert.Nil(t, repo.gotStart, "an absent bound must stay absent rather than become a zero time")
	assert.Nil(t, repo.gotEnd)
}

// The whole point of the endpoint: a closed account that we kept nothing for is a real answer, not
// a failure. Rendering an error here would say the retention failed when it did not.
func TestAccountBalanceHistoryService_NoRetainedHistoryIsAnAnswerNotAnError(t *testing.T) {
	repo := &stubBalanceHistoryRepo{points: nil}
	svc := NewAccountBalanceHistoryService(repo)

	got, err := svc.GetAccountBalanceHistory(context.Background(), "u", "a", nil, nil)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "a", got.AccountID)
	assert.NotNil(t, got.Points, "points must be an empty slice, never nil — the JSON contract is []")
	assert.Empty(t, got.Points)
	assert.Nil(t, got.FirstDate, "an empty series bounds nothing")
	assert.Nil(t, got.LastDate)
}

func TestAccountBalanceHistoryService_BoundsComeFromTheEndsOfTheSeries(t *testing.T) {
	repo := &stubBalanceHistoryRepo{points: []domain.AccountBalanceHistoryPoint{
		{Date: "2026-01-01", CurrentBalance: fptr(100), CurrencyCode: "USD"},
		{Date: "2026-01-02", CurrentBalance: fptr(150), CurrencyCode: "USD"},
		{Date: "2026-01-05", CurrentBalance: fptr(90), CurrencyCode: "USD"},
	}}
	svc := NewAccountBalanceHistoryService(repo)

	got, err := svc.GetAccountBalanceHistory(context.Background(), "u", "a", nil, nil)
	require.NoError(t, err)
	require.Len(t, got.Points, 3, "every retained day must survive the service unchanged")
	require.NotNil(t, got.FirstDate)
	require.NotNil(t, got.LastDate)
	assert.Equal(t, "2026-01-01", *got.FirstDate)
	assert.Equal(t, "2026-01-05", *got.LastDate)
	// The series must reach the caller in the repository's order — a chart drawn from a reordered
	// series is a different picture of the same rows.
	assert.Equal(t, "2026-01-02", got.Points[1].Date)
	require.NotNil(t, got.Points[2].CurrentBalance)
	assert.Equal(t, float64(90), *got.Points[2].CurrentBalance)
}

// A missing balance stays missing. Coercing it to zero would report that the account held nothing
// on a day the provider simply did not tell us about.
func TestAccountBalanceHistoryService_AbsentBalanceStaysAbsent(t *testing.T) {
	repo := &stubBalanceHistoryRepo{points: []domain.AccountBalanceHistoryPoint{
		{Date: "2026-01-01", CurrentBalance: nil, AvailableBalance: nil, CurrencyCode: "USD"},
	}}
	svc := NewAccountBalanceHistoryService(repo)

	got, err := svc.GetAccountBalanceHistory(context.Background(), "u", "a", nil, nil)
	require.NoError(t, err)
	require.Len(t, got.Points, 1)
	assert.Nil(t, got.Points[0].CurrentBalance)
	assert.Nil(t, got.Points[0].AvailableBalance)
}

func TestAccountBalanceHistoryService_RepositoryErrorPropagates(t *testing.T) {
	boom := errors.New("boom")
	repo := &stubBalanceHistoryRepo{err: boom}
	svc := NewAccountBalanceHistoryService(repo)

	got, err := svc.GetAccountBalanceHistory(context.Background(), "u", "a", nil, nil)
	assert.Nil(t, got, "a failed read must never fall through to an empty series: that would claim we kept nothing")
	assert.ErrorIs(t, err, boom)
}
