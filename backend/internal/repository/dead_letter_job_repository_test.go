package repository

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupDLQMockDB creates a mock database and DLQ repository for testing.
func setupDLQMockDB(t *testing.T) (pgxmock.PgxPoolIface, *DeadLetterJobRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	repo := NewDeadLetterJobRepository(mock)
	return mock, repo
}

// dlqScanColumns are the columns returned by full DLQ SELECT queries.
var dlqScanColumns = []string{
	"id", "original_job_id", "kind", "queue", "args", "errors",
	"max_attempts", "created_at", "failed_at", "user_id", "notified",
	"retried_at", "retry_job_id", "notes",
}

func newDLQRow(id int64, kind, queue string, userID *string, retriedAt *time.Time) []interface{} {
	now := time.Now()
	uid := "user123"
	if userID != nil {
		uid = *userID
	}
	return []interface{}{
		id, int64(99), kind, queue,
		json.RawMessage(`{"user_id":"` + uid + `"}`),
		json.RawMessage(`[{"attempt":1,"message":"err","timestamp":"2024-01-01T00:00:00Z"}]`),
		5, now, now,
		userID, false, retriedAt, (*int64)(nil), (*string)(nil),
	}
}

// --- ListAll ---

func TestDeadLetterJobRepository_ListAll_NoFilters(t *testing.T) {
	mock, repo := setupDLQMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := "user123"

	mock.ExpectQuery(regexp.QuoteMeta(`FROM dead_letter_jobs`)).
		WillReturnRows(pgxmock.NewRows(dlqScanColumns).
			AddRow(newDLQRow(1, "plaid_sync", "default", &userID, nil)...))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*)`)).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))

	jobs, total, err := repo.ListAll(ctx, domain.DeadLetterJobFilters{}, 20, 0)

	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, jobs, 1)
	assert.Equal(t, int64(1), jobs[0].ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDeadLetterJobRepository_ListAll_WithKindFilter(t *testing.T) {
	mock, repo := setupDLQMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	kind := "plaid_sync"
	userID := "user123"

	mock.ExpectQuery(regexp.QuoteMeta(`FROM dead_letter_jobs`)).
		WithArgs("plaid_sync").
		WillReturnRows(pgxmock.NewRows(dlqScanColumns).
			AddRow(newDLQRow(1, kind, "default", &userID, nil)...))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*)`)).
		WithArgs("plaid_sync").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))

	jobs, total, err := repo.ListAll(ctx, domain.DeadLetterJobFilters{Kind: &kind}, 20, 0)

	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, jobs, 1)
	assert.Equal(t, kind, jobs[0].Kind)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDeadLetterJobRepository_ListAll_WithRetriedFilter(t *testing.T) {
	mock, repo := setupDLQMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	retried := false
	userID := "user123"

	// retried=false generates "retried_at IS NULL" condition with no args
	mock.ExpectQuery(regexp.QuoteMeta(`FROM dead_letter_jobs`)).
		WillReturnRows(pgxmock.NewRows(dlqScanColumns).
			AddRow(newDLQRow(1, "plaid_sync", "default", &userID, nil)...))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*)`)).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))

	jobs, total, err := repo.ListAll(ctx, domain.DeadLetterJobFilters{Retried: &retried}, 20, 0)

	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, jobs, 1)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDeadLetterJobRepository_ListAll_Empty(t *testing.T) {
	mock, repo := setupDLQMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	mock.ExpectQuery(regexp.QuoteMeta(`FROM dead_letter_jobs`)).
		WillReturnRows(pgxmock.NewRows(dlqScanColumns))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*)`)).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(0))

	jobs, total, err := repo.ListAll(ctx, domain.DeadLetterJobFilters{}, 20, 0)

	require.NoError(t, err)
	assert.Equal(t, 0, total)
	assert.Empty(t, jobs)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- ListAll sort ---

func TestListAllDLQSort(t *testing.T) {
	tests := []struct {
		name        string
		sortBy      string
		sortDir     string
		expectOrder string
	}{
		{
			name:        "default sort failed_at desc",
			sortBy:      "failed_at",
			sortDir:     "desc",
			expectOrder: "ORDER BY failed_at DESC",
		},
		{
			name:        "kind asc",
			sortBy:      "kind",
			sortDir:     "asc",
			expectOrder: "ORDER BY kind ASC",
		},
		{
			name:        "retried_at asc with NULLS LAST",
			sortBy:      "retried_at",
			sortDir:     "asc",
			expectOrder: "ORDER BY retried_at ASC NULLS LAST",
		},
		// Fallback cases — Huma enum prevents these in production, but repos are defense-in-depth
		{
			name:        "empty sort_by falls back to failed_at desc",
			sortBy:      "",
			sortDir:     "",
			expectOrder: "ORDER BY failed_at DESC",
		},
		{
			name:        "unknown sort_by falls back to failed_at desc",
			sortBy:      "nonexistent_column",
			sortDir:     "desc",
			expectOrder: "ORDER BY failed_at DESC",
		},
		{
			name:        "unknown sort_dir falls back to DESC",
			sortBy:      "failed_at",
			sortDir:     "invalid",
			expectOrder: "ORDER BY failed_at DESC",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock, repo := setupDLQMockDB(t)
			defer mock.Close()

			ctx := context.Background()
			userID := "user123"

			mock.ExpectQuery(regexp.MustCompile(regexp.QuoteMeta(tc.expectOrder)).String()).
				WillReturnRows(pgxmock.NewRows(dlqScanColumns).
					AddRow(newDLQRow(1, "plaid_sync", "default", &userID, nil)...))

			mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*)`)).
				WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))

			filters := domain.DeadLetterJobFilters{
				SortBy:  tc.sortBy,
				SortDir: tc.sortDir,
			}
			jobs, total, err := repo.ListAll(ctx, filters, 20, 0)

			require.NoError(t, err)
			assert.Equal(t, 1, total)
			assert.Len(t, jobs, 1)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// --- DeleteByID ---

func TestDeadLetterJobRepository_DeleteByID_Success(t *testing.T) {
	mock, repo := setupDLQMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM dead_letter_jobs WHERE id = $1`)).
		WithArgs(int64(42)).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	err := repo.DeleteByID(ctx, 42)

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- DeleteOlderThan ---

func TestDeadLetterJobRepository_DeleteOlderThan_SkipsNotes(t *testing.T) {
	mock, repo := setupDLQMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	before := time.Now().Add(-30 * 24 * time.Hour)

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM dead_letter_jobs WHERE failed_at < $1 AND notes IS NULL`)).
		WithArgs(pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("DELETE", 5))

	count, err := repo.DeleteOlderThan(ctx, before, true)

	require.NoError(t, err)
	assert.Equal(t, int64(5), count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDeadLetterJobRepository_DeleteOlderThan_IncludesNotes(t *testing.T) {
	mock, repo := setupDLQMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	before := time.Now().Add(-30 * 24 * time.Hour)

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM dead_letter_jobs WHERE failed_at < $1`)).
		WithArgs(pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("DELETE", 10))

	count, err := repo.DeleteOlderThan(ctx, before, false)

	require.NoError(t, err)
	assert.Equal(t, int64(10), count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- DeleteByFilters ---

func TestDeadLetterJobRepository_DeleteByFilters_NoFilters(t *testing.T) {
	mock, repo := setupDLQMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM dead_letter_jobs`)).
		WillReturnResult(pgxmock.NewResult("DELETE", 3))

	count, err := repo.DeleteByFilters(ctx, domain.DeadLetterJobFilters{})

	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- AtomicMarkRetried ---

func TestDeadLetterJobRepository_AtomicMarkRetried_Success(t *testing.T) {
	mock, repo := setupDLQMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	mock.ExpectQuery(regexp.QuoteMeta(`UPDATE dead_letter_jobs`)).
		WithArgs(pgxmock.AnyArg(), int64(999), int64(1)).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(int64(1)))

	claimed, err := repo.AtomicMarkRetried(ctx, 1, 999)

	require.NoError(t, err)
	assert.True(t, claimed)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDeadLetterJobRepository_AtomicMarkRetried_AlreadyRetried(t *testing.T) {
	mock, repo := setupDLQMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	// No rows returned = already retried or not found
	mock.ExpectQuery(regexp.QuoteMeta(`UPDATE dead_letter_jobs`)).
		WithArgs(pgxmock.AnyArg(), int64(999), int64(1)).
		WillReturnRows(pgxmock.NewRows([]string{"id"}))

	claimed, err := repo.AtomicMarkRetried(ctx, 1, 999)

	require.NoError(t, err)
	assert.False(t, claimed)
	assert.NoError(t, mock.ExpectationsWereMet())
}
