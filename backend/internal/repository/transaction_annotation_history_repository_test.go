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
		domain.TransactionAnnotationChangeErasure,
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
	assert.Equal(t, 2, strings.Count(text, "domain.TransactionAnnotationChangeSettlement"),
		"BOTH halves of the settlement move file their rows under the settlement source: "+
			"TombstoneByExternalID's tombstone-side move (#1569 fix round 2 / D-4) and "+
			"PullForwardSettledPendingAnnotations, the storage-side mirror that closes the "+
			"cross-sync case (#1586). They are one event reached by two routes, so a reader must "+
			"not have to know which route ran to interpret the row.")
}

// TestSettlementMoveSetClausesAgree is the drift guard the two settlement hooks need and cannot get
// any other way (#1586).
//
// THE SHAPE OF THE PROBLEM. The same event — a pending charge settling — reaches the annotation move
// by two routes: TombstoneByExternalID when the twin is already stored, and
// PullForwardSettledPendingAnnotations when it arrives in a later sync. They must write the SAME
// twelve columns under the SAME three refusal rules, or the user's annotation depends on the order
// Plaid happened to deliver two records in, which is not a property any user could reason about.
//
// WHY THE SET CLAUSE IS NOT FACTORED INTO A SHARED CONSTANT, which is the obvious fix and is wrong
// here. TestAnnotationCaptureIsUsedByEveryAnnotationWritingStatement finds annotation writes by
// matching `<column> =` against the RENDERED TEXT of each statement. Move the SET clause into a
// package-level string and BOTH statements stop containing any tracked column assignment: they
// vanish from that guard's universe entirely, and the guard keeps passing on the other findings —
// the exact "a guard can stop guarding silently" failure this repository has already hit. So the
// duplication is deliberate, and this test is the price of it.
//
// The three refusal PREDICATES are genuinely shared (settlementCategoryMoveCond,
// settlementTagsMoveCond, settlementReviewMoveCond); it is only the SET clause around them that is
// written twice.
func TestSettlementMoveSetClausesAgree(t *testing.T) {
	src, err := os.ReadFile("transaction_repository.go")
	require.NoError(t, err)

	// Each clause runs from the `user_notes` assignment that opens it to the `FROM` that ends it.
	clauseRe := regexp.MustCompile(`(?s)SET user_notes = COALESCE\(twin\.user_notes, s\.user_notes\),(.*?)\n\t*(?:--|FROM)`)
	matches := clauseRe.FindAllStringSubmatch(string(src), -1)
	require.Len(t, matches, 2,
		"expected exactly two settlement SET clauses (TombstoneByExternalID and "+
			"PullForwardSettledPendingAnnotations). Finding a different number means either a third "+
			"hook appeared without being compared, or this regexp has stopped matching its corpus "+
			"and the comparison below is vacuous")

	normalise := func(s string) string {
		return strings.Join(strings.Fields(s), " ")
	}
	assert.Equal(t, normalise(matches[0][1]), normalise(matches[1][1]),
		"the tombstone-side move and the storage-side mirror must write the same columns under the "+
			"same conditions; a user's annotation must not depend on which order Plaid delivered "+
			"the removal and the posted twin in")

	// A comparison of two empty strings is equal and proves nothing. Pin that the clause really
	// carries the twelve-column move.
	for _, col := range []string{
		"category_override_kind", "pfc_primary_override", "pfc_detailed_override",
		"custom_category_override_id", "category_source", "category_override_date",
		"effective_category_name", "tags", "tags_source", "reviewed", "reviewed_at",
	} {
		assert.Contains(t, matches[0][1], col+" ",
			"the compared clause must actually contain the moved columns")
	}
}
