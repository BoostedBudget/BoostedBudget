package services

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// Issue #1569 Task 6 (#1579) — the read service.

type stubAnnotationHistoryRepo struct {
	rows      []*domain.TransactionAnnotationChange
	total     int
	err       error
	gotUserID string
	gotLimit  int
	gotOffset int
	calls     int
}

func (s *stubAnnotationHistoryRepo) GetByUserID(
	_ context.Context, userID string, limit, offset int,
) ([]*domain.TransactionAnnotationChange, int, error) {
	s.calls++
	s.gotUserID, s.gotLimit, s.gotOffset = userID, limit, offset
	return s.rows, s.total, s.err
}

// TestTransactionAnnotationHistoryService_PassesTheAuthenticatedUserStraightThrough. There is no
// second identifier in this call, and there must not be: adding a transaction id here is what would
// hide changes whose transaction has been purged.
func TestTransactionAnnotationHistoryService_PassesTheAuthenticatedUserStraightThrough(t *testing.T) {
	repo := &stubAnnotationHistoryRepo{
		rows:  []*domain.TransactionAnnotationChange{{ID: "chg-1", UserID: "user-1579"}},
		total: 1,
	}
	svc := NewTransactionAnnotationHistoryService(repo)

	rows, total, err := svc.GetUserAnnotationHistory(context.Background(), "user-1579", 20, 40)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	require.Len(t, rows, 1)
	assert.Equal(t, "user-1579", repo.gotUserID)
	assert.Equal(t, 20, repo.gotLimit)
	assert.Equal(t, 40, repo.gotOffset)
	assert.Equal(t, 1, repo.calls)
}

func TestTransactionAnnotationHistoryService_RepositoryErrorIsNotSwallowed(t *testing.T) {
	svc := NewTransactionAnnotationHistoryService(&stubAnnotationHistoryRepo{err: errors.New("boom")})
	_, _, err := svc.GetUserAnnotationHistory(context.Background(), "user-1579", 20, 0)
	require.Error(t, err)
}
