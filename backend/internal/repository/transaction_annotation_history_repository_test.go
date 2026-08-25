package repository

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// Issue #1569 Task 6 (#1579) — the annotation trail's read path and the writer's source values.
//
// The BEHAVIOURAL claims about the capture live in
// internal/services/transaction_annotation_history_integration_test.go, against real Postgres,
// because they are all claims about what one statement does inside the planner. What is left for
// this file is what a mock CAN prove: the read's tenancy predicate and column contract, and the
// agreement between the two Go constants the writer interpolates and the CHECK the database
// enforces.

func TestTransactionAnnotationHistoryRepository_GetByUserID_FiltersOnTheRowsOwnUserID(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionAnnotationHistoryRepository(mock)
	ctx := context.Background()
	userID := "user-1579"
	kind := "pfc"
	prev := "ENTERTAINMENT"
	reviewedAt := time.Date(2026, 3, 2, 9, 30, 0, 0, time.UTC)
	changedAt := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM transaction_annotation_history WHERE user_id = $1")).
		WithArgs(userID).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))

	mock.ExpectQuery("FROM transaction_annotation_history").
		WithArgs(userID, 20, 0).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "user_id", "transaction_id", "change_source",
			"prior_category_override_kind", "prior_pfc_primary_override", "prior_pfc_detailed_override",
			"prior_custom_category_override_id", "prior_effective_category_name", "prior_category_source",
			"prior_reviewed", "prior_reviewed_at",
			"new_category_override_kind", "new_pfc_primary_override", "new_pfc_detailed_override",
			"new_custom_category_override_id", "new_effective_category_name", "new_category_source",
			"new_reviewed", "new_reviewed_at", "changed_at",
		}).AddRow(
			"chg-1", userID, "tx-1", domain.TransactionAnnotationChangeCategory,
			&kind, &prev, (*string)(nil), (*string)(nil), &prev, ptrStr("manual"),
			true, &reviewedAt,
			ptrStr("custom"), (*string)(nil), (*string)(nil), ptrStr("cat-1"), ptrStr("Weekend Coffee"), ptrStr("manual"),
			true, &changedAt, changedAt,
		))

	rows, total, err := repo.GetByUserID(ctx, userID, 20, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	require.Len(t, rows, 1)

	// Both halves survive the scan. A read that dropped `prior` would return a perfectly shaped row
	// and lose the only thing #1579 is about.
	require.NotNil(t, rows[0].Prior.PFCPrimaryOverride)
	assert.Equal(t, "ENTERTAINMENT", *rows[0].Prior.PFCPrimaryOverride)
	require.NotNil(t, rows[0].Prior.ReviewedAt)
	require.NotNil(t, rows[0].New.EffectiveCategoryName)
	assert.Equal(t, "Weekend Coffee", *rows[0].New.EffectiveCategoryName)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestTransactionAnnotationHistoryRepository_GetByUserID_EmptyIsAnEmptySliceNotNil: the handler
// converts nil to [], but a repository that returns nil on a healthy read makes every caller
// responsible for that, which is how a `null` reaches a client.
func TestTransactionAnnotationHistoryRepository_GetByUserID_EmptyIsAnEmptySliceNotNil(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionAnnotationHistoryRepository(mock)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*)")).WithArgs("user-empty").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery("FROM transaction_annotation_history").WithArgs("user-empty", 20, 0).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "user_id", "transaction_id", "change_source",
			"prior_category_override_kind", "prior_pfc_primary_override", "prior_pfc_detailed_override",
			"prior_custom_category_override_id", "prior_effective_category_name", "prior_category_source",
			"prior_reviewed", "prior_reviewed_at",
			"new_category_override_kind", "new_pfc_primary_override", "new_pfc_detailed_override",
			"new_custom_category_override_id", "new_effective_category_name", "new_category_source",
			"new_reviewed", "new_reviewed_at", "changed_at",
		}))

	rows, total, err := repo.GetByUserID(context.Background(), "user-empty", 20, 0)
	require.NoError(t, err)
	assert.Equal(t, 0, total)
	assert.NotNil(t, rows)
	assert.Empty(t, rows)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTransactionAnnotationHistoryRepository_GetByUserID_CountErrorPropagates(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionAnnotationHistoryRepository(mock)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*)")).WithArgs("user-x").
		WillReturnError(errors.New("connection reset"))

	_, _, err = repo.GetByUserID(context.Background(), "user-x", 20, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to count transaction annotation changes")
}

// TestAnnotationCaptureSourcesMatchTheCheckConstraint is the guard the capture SQL cites. The two
// change_source values are INTERPOLATED into the statement rather than bound as parameters (a CTE
// body cannot take a placeholder in a position the caller's own $n numbering does not control), so
// a mismatch between the Go constants and the database's CHECK would fail at runtime, on a write
// path, in production — not here. This test makes it fail here instead.
//
// It reads the migration rather than the live database on purpose: it must run in the unit suite,
// with no DATABASE_URL, alongside the code it is pinning.
func TestAnnotationCaptureSourcesMatchTheCheckConstraint(t *testing.T) {
	migration, err := os.ReadFile("../../migrations/000169_record_the_prior_annotation.up.sql")
	require.NoError(t, err)

	check := regexp.MustCompile(`change_source IN \(([^)]*)\)`).FindStringSubmatch(string(migration))
	require.Len(t, check, 2, "the CHECK constraint is no longer where this test looks for it")

	allowed := map[string]bool{}
	for _, lit := range regexp.MustCompile(`'([a-z_]+)'`).FindAllStringSubmatch(check[1], -1) {
		allowed[lit[1]] = true
	}
	require.NotEmpty(t, allowed)

	for _, source := range []string{
		domain.TransactionAnnotationChangeCategory,
		domain.TransactionAnnotationChangeReview,
	} {
		assert.True(t, allowed[source],
			"the writer interpolates %q, which the live CHECK would reject", source)
		delete(allowed, source)
	}
	assert.Empty(t, allowed,
		"the CHECK allows a value no writer emits: either a source was dropped in Go without a "+
			"migration, or the constraint was widened speculatively")
}

// TestAnnotationCaptureIsUsedByEveryAnnotationWritingStatement. The capture is a CTE the three
// statements have to opt into, and the failure mode of that design is a FOURTH statement — or a
// rewrite of one of these three — that quietly drops it and starts losing prior values again with
// every test still green. This pins the count and the sources.
func TestAnnotationCaptureIsUsedByEveryAnnotationWritingStatement(t *testing.T) {
	src, err := os.ReadFile("transaction_repository.go")
	require.NoError(t, err)
	text := string(src)

	assert.Equal(t, 3, strings.Count(text, "annotationCaptureInsert("),
		"UpdateCategory, BulkUpdateCategory and SetReviewed each carry the capture; a change to "+
			"this count is either a new annotation-writing statement that must carry it too, or one "+
			"of the three losing it")
	assert.Equal(t, 3, strings.Count(text, "annotationCaptureReturning"),
		"a capture INSERT with no matching RETURNING list cannot compile the columns it selects")
	assert.Equal(t, 2, strings.Count(text, "domain.TransactionAnnotationChangeCategory"),
		"both category statements attribute their rows to the category source")
	assert.Equal(t, 1, strings.Count(text, "domain.TransactionAnnotationChangeReview"),
		"SetReviewed is the single statement behind BOTH review endpoints")
}
