package services

// Unit tests for HoldingHistoryService (Issue #1569).
//
// These pin the composition rules the service owns and the repository cannot: which day gets
// picked, that an unretained day is REFUSED rather than answered with an empty set, that the total
// is nil rather than zero when nothing carries a value, and that the collections are never nil.
// The SQL itself is proved in internal/repository's integration tests against real Postgres.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

type stubHoldingSnapshotRepo struct {
	dates    []string
	datesErr error

	positions    []domain.HoldingSnapshotView
	positionsErr error

	// Recorded PER METHOD, not shared. A single gotUserID field let a mutant that passed "" to the
	// date listing survive, because the later position call overwrote it with the right value —
	// tenancy has to be proved on every statement separately, which is also why the SQL carries it
	// on every statement.
	datesUserID    string
	datesAccountID string

	gotUserID    string
	gotAccountID string
	gotDay       string
	calls        int
}

func (s *stubHoldingSnapshotRepo) ListHistoryDatesForAccount(_ context.Context, userID, accountID string) ([]string, error) {
	s.datesUserID, s.datesAccountID = userID, accountID
	return s.dates, s.datesErr
}

func (s *stubHoldingSnapshotRepo) GetHistoryForAccountOnDate(_ context.Context, userID, accountID, asOf string) ([]domain.HoldingSnapshotView, error) {
	s.calls++
	s.gotUserID, s.gotAccountID, s.gotDay = userID, accountID, asOf
	return s.positions, s.positionsErr
}

// The interface's other three methods belong to the WRITE side (the sync capture and the daily
// job). They are unreachable from this service and stubbed to fail loudly rather than silently:
// if a future edit routes a read through one of them, the test says so instead of passing.
func (s *stubHoldingSnapshotRepo) CaptureForUser(context.Context, string) (time.Time, error) {
	panic("HoldingHistoryService must never write")
}

func (s *stubHoldingSnapshotRepo) GetForUserOnDate(context.Context, string, time.Time) ([]domain.HoldingSnapshot, error) {
	panic("HoldingHistoryService must not use the whole-user reconstruction primitive")
}

func (s *stubHoldingSnapshotRepo) GetByAccountDateRange(context.Context, string, time.Time, time.Time) ([]domain.HoldingSnapshot, error) {
	panic("HoldingHistoryService must not use the UNSCOPED account read")
}

func f64p(v float64) *float64 { return &v }

func TestHoldingHistoryService_NoAsOf_PicksTheMostRecentRetainedDay(t *testing.T) {
	repo := &stubHoldingSnapshotRepo{
		// Newest first, as the repository contract promises.
		dates:     []string{"2026-08-20", "2026-08-19", "2026-07-01"},
		positions: []domain.HoldingSnapshotView{{SecurityID: "sec-1", AsOfDate: "2026-08-20", Quantity: 3, InstitutionValue: f64p(300)}},
	}
	svc := NewHoldingHistoryService(repo)

	got, err := svc.GetAccountHoldingsHistory(context.Background(), "user-1", "acct-1", "")

	require.NoError(t, err)
	assert.Equal(t, "2026-08-20", repo.gotDay, "must query the newest retained day, not the oldest")
	require.NotNil(t, got.AsOfDate)
	assert.Equal(t, "2026-08-20", *got.AsOfDate, "the resolved day must be echoed back so the client knows what it is looking at")
	assert.Equal(t, []string{"2026-08-20", "2026-08-19", "2026-07-01"}, got.AvailableDates)
	assert.Equal(t, "user-1", repo.datesUserID, "tenancy must reach the date listing")
	assert.Equal(t, "acct-1", repo.datesAccountID)
	assert.Equal(t, "user-1", repo.gotUserID, "tenancy must reach the position read too")
	assert.Equal(t, "acct-1", repo.gotAccountID)
	require.NotNil(t, got.TotalValue)
	assert.InDelta(t, 300.0, *got.TotalValue, 0.0001)
}

func TestHoldingHistoryService_ExplicitAsOf_ReadsThatDay(t *testing.T) {
	repo := &stubHoldingSnapshotRepo{
		dates:     []string{"2026-08-20", "2026-08-19"},
		positions: []domain.HoldingSnapshotView{{SecurityID: "sec-1", AsOfDate: "2026-08-19"}},
	}
	svc := NewHoldingHistoryService(repo)

	got, err := svc.GetAccountHoldingsHistory(context.Background(), "user-1", "acct-1", "2026-08-19")

	require.NoError(t, err)
	assert.Equal(t, "2026-08-19", repo.gotDay)
	require.NotNil(t, got.AsOfDate)
	assert.Equal(t, "2026-08-19", *got.AsOfDate)
}

// The mechanical refusal. Answering an unretained day with an empty list would say "this account
// held nothing on 2026-01-01", which is a claim about the user's money the service has no basis
// for — on the one surface that exists to stop making such claims.
func TestHoldingHistoryService_UnretainedDay_IsRefusedNotAnsweredEmpty(t *testing.T) {
	repo := &stubHoldingSnapshotRepo{dates: []string{"2026-08-20"}}
	svc := NewHoldingHistoryService(repo)

	got, err := svc.GetAccountHoldingsHistory(context.Background(), "user-1", "acct-1", "2026-01-01")

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrHoldingHistoryDateNotRetained))
	assert.Nil(t, got)
	assert.Zero(t, repo.calls, "an unretained day must be refused BEFORE a position query is issued")
}

// An account removed before this branch started capturing has no history, and that is not an
// error — it is a fact the caller renders in words. Both collections must still be non-nil so the
// client cannot confuse them with a failed read.
func TestHoldingHistoryService_NoRetainedHistory_IsEmptyNotAnError(t *testing.T) {
	repo := &stubHoldingSnapshotRepo{dates: []string{}}
	svc := NewHoldingHistoryService(repo)

	got, err := svc.GetAccountHoldingsHistory(context.Background(), "user-1", "acct-1", "")

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Nil(t, got.AsOfDate, "there is no day to name")
	assert.NotNil(t, got.AvailableDates, "a nil slice marshals as null, which reads as a failed request")
	assert.Empty(t, got.AvailableDates)
	assert.NotNil(t, got.Positions)
	assert.Empty(t, got.Positions)
	assert.Nil(t, got.TotalValue)
	assert.Zero(t, repo.calls, "with no retained days there is nothing to query for")
}

// Nil, not zero: a portfolio whose values the provider never sent is not a portfolio worth
// nothing, and rendering "$0.00" beside a closed brokerage is exactly the false-figure failure
// #1569 T7 deleted a caption for.
func TestHoldingHistoryService_TotalValue_IsNilWhenNoPositionCarriesAValue(t *testing.T) {
	repo := &stubHoldingSnapshotRepo{
		dates: []string{"2026-08-20"},
		positions: []domain.HoldingSnapshotView{
			{SecurityID: "sec-1", Quantity: 5},
			{SecurityID: "sec-2", Quantity: 2},
		},
	}
	svc := NewHoldingHistoryService(repo)

	got, err := svc.GetAccountHoldingsHistory(context.Background(), "user-1", "acct-1", "")

	require.NoError(t, err)
	assert.Nil(t, got.TotalValue)
	assert.Len(t, got.Positions, 2, "the positions are still real; only the total is unknowable")
}

func TestHoldingHistoryService_TotalValue_SumsTheValuesItDoesHave(t *testing.T) {
	repo := &stubHoldingSnapshotRepo{
		dates: []string{"2026-08-20"},
		positions: []domain.HoldingSnapshotView{
			{SecurityID: "sec-1", InstitutionValue: f64p(1200.50)},
			{SecurityID: "sec-2"},
			{SecurityID: "sec-3", InstitutionValue: f64p(99.50)},
		},
	}
	svc := NewHoldingHistoryService(repo)

	got, err := svc.GetAccountHoldingsHistory(context.Background(), "user-1", "acct-1", "")

	require.NoError(t, err)
	require.NotNil(t, got.TotalValue)
	assert.InDelta(t, 1300.0, *got.TotalValue, 0.0001)
}

func TestHoldingHistoryService_RepositoryErrorsPropagate(t *testing.T) {
	boom := errors.New("db down")

	svc := NewHoldingHistoryService(&stubHoldingSnapshotRepo{datesErr: boom})
	_, err := svc.GetAccountHoldingsHistory(context.Background(), "u", "a", "")
	assert.ErrorIs(t, err, boom)

	svc = NewHoldingHistoryService(&stubHoldingSnapshotRepo{dates: []string{"2026-08-20"}, positionsErr: boom})
	_, err = svc.GetAccountHoldingsHistory(context.Background(), "u", "a", "")
	assert.ErrorIs(t, err, boom)
}
