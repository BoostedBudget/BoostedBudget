package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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

// TestAnnotationCaptureSourcesMatchTheCheckConstraint is the guard the capture SQL cites. The
// change_source values are INTERPOLATED into the statement rather than bound as parameters (a CTE
// body cannot take a placeholder in a position the caller's own $n numbering does not control), so
// a mismatch between the Go constants and the database's CHECK would fail at runtime, on a write
// path, in production — not here. This test makes it fail here instead.
//
// It reads the migrations rather than the live database on purpose: it must run in the unit suite,
// with no DATABASE_URL, alongside the code it is pinning. It takes the LAST migration that defines
// the constraint rather than naming one file, so widening it again (as 000170 widened 000169's two
// values to three) does not silently leave this test pinning a superseded definition.
func TestAnnotationCaptureSourcesMatchTheCheckConstraint(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.up.sql"))
	require.NoError(t, err)
	sort.Strings(files)

	checkRe := regexp.MustCompile(`change_source IN \(([^)]*)\)`)
	var latest string
	var latestFile string
	for _, f := range files {
		body, readErr := os.ReadFile(f)
		require.NoError(t, readErr)
		if m := checkRe.FindStringSubmatch(string(body)); m != nil {
			latest, latestFile = m[1], filepath.Base(f)
		}
	}
	require.NotEmpty(t, latest,
		"no migration defines the change_source CHECK any more; this test is pinning nothing")
	t.Logf("live change_source CHECK comes from %s", latestFile)

	allowed := map[string]bool{}
	for _, lit := range regexp.MustCompile(`'([a-z_]+)'`).FindAllStringSubmatch(latest, -1) {
		allowed[lit[1]] = true
	}
	require.NotEmpty(t, allowed)

	for _, source := range []string{
		domain.TransactionAnnotationChangeCategory,
		domain.TransactionAnnotationChangeReview,
		domain.TransactionAnnotationChangeSettlement,
	} {
		assert.True(t, allowed[source],
			"the writer interpolates %q, which the live CHECK would reject", source)
		delete(allowed, source)
	}
	assert.Empty(t, allowed,
		"the CHECK allows a value no writer emits: either a source was dropped in Go without a "+
			"migration, or the constraint was widened speculatively")
}

// TestAnnotationCaptureFragmentsCarryNoFormatVerb is cheap insurance against a nasty failure mode.
// Two of the four capturing statements splice the generated fragments INTO a fmt.Sprintf format
// string (TombstoneByExternalID's whole query is a Sprintf), so a `%` appearing in a column name or
// in the generated SQL would be eaten as a verb and produce either a corrupted statement or a
// %!s(MISSING) in production SQL. Nothing else would catch it: the fragments are only exercised
// end to end by the integration suite.
func TestAnnotationCaptureFragmentsCarryNoFormatVerb(t *testing.T) {
	fragments := map[string]string{
		"returning(t)":       annotationCaptureReturning("t"),
		"returning(twin)":    annotationCaptureReturning("twin"),
		"insert(category)":   annotationCaptureInsert(domain.TransactionAnnotationChangeCategory, "updated"),
		"insert(review)":     annotationCaptureInsert(domain.TransactionAnnotationChangeReview, "updated"),
		"insert(settlement)": annotationCaptureInsert(domain.TransactionAnnotationChangeSettlement, "moved"),
		"columnList(prior_)": annotationCaptureColumnList("prior_", ", "),
		"columnList(new_)":   annotationCaptureColumnList("new_", ", "),
	}
	for name, frag := range fragments {
		assert.NotContains(t, frag, "%",
			"%s contains a %% that fmt.Sprintf would consume as a verb", name)
		assert.NotEmpty(t, frag, "%s generated nothing, so nothing downstream is being pinned", name)
	}
}

// TestAnnotationCaptureSourceAttributionIsUnchanged pins WHICH source each statement records.
// The structural question — does every annotation-writing statement carry the capture at all — is
// TestAnnotationCaptureIsUsedByEveryAnnotationWritingStatement's, in annotation_capture_guard_test.go,
// which walks internal/ by AST. This one is the narrower claim that a string count can actually
// support: the two category statements attribute to 'category' and the single review statement to
// 'review'.
func TestAnnotationCaptureSourceAttributionIsUnchanged(t *testing.T) {
	src, err := os.ReadFile("transaction_repository.go")
	require.NoError(t, err)
	text := string(src)

	assert.Equal(t, 2, strings.Count(text, "domain.TransactionAnnotationChangeCategory"),
		"both category statements attribute their rows to the category source")
	assert.Equal(t, 1, strings.Count(text, "domain.TransactionAnnotationChangeReview"),
		"SetReviewed is the single statement behind BOTH review endpoints")
	assert.Equal(t, 1, strings.Count(text, "domain.TransactionAnnotationChangeSettlement"),
		"TombstoneByExternalID's settlement move is the fourth capturing statement (#1569 fix "+
			"round 2 / D-4) and files its rows under their own source")
}
