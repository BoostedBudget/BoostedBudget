package repository

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Issue #1569 Task 6 (#1579), review fix round 1 (F2 / ruling R80) — every statement that
// overwrites a transaction's annotation must record what it overwrote.
//
// ⚠ THIS REPLACES A CLAIM THAT WAS FALSE. The first version of this task asserted in code and in its
// report that "a fourth write path cannot forget to call the writer, because a new statement either
// carries the CTE or it does not update the annotation at all." That was wrong, and review
// disproved it: the only enforcement was a string count of `annotationCaptureInsert(` occurrences
// IN transaction_repository.go ALONE. An `UPDATE transactions SET reviewed = ...` in a worker, a
// new repository, or anywhere else in internal/ was completely unguarded. The claim is now true
// because this test makes it true, not because the design made it true.
//
// WHAT IT CHECKS. Every Go statement under internal/ that builds or issues an `UPDATE transactions`
// assigning a TRACKED annotation column must, in the same statement, call annotationCaptureInsert.
// Both directions fail: an unguarded write is the dangerous one; an exemption that no longer
// matches anything is allowlist rot, which is how the next person justifies adding one.
//
// SCOPE AND ITS LIMITS, stated rather than implied:
//   - Production files only (no _test.go). A fixture writing rows by hand is a different hazard
//     with its own lesson in this repo; this guard is about production write paths.
//   - internal/ only. cmd/ builds no SQL.
//   - It is a SYNTACTIC check over Go string literals. SQL assembled from values the parser cannot
//     see — a column name from a variable, a query read from a file — is invisible to it, exactly as
//     it is to the tombstone guards it borrows its machinery from.
//
// Mutation-proven by TestAnnotationCaptureGuard_CatchesAnUnguardedWrite, which runs the same pure
// function over testdata/annotation_guard: one file carrying the violation, one carrying the
// capture, so the meta-test can tell "flags the violation" from "flags everything".

// annotationTrackedColumns is DERIVED from the production list, minus the one derived column, so a
// column added to transaction_annotation_history automatically enters this guard's universe instead
// of needing a second hand-maintained copy that can quietly disagree.
//
// effective_category_name is the exclusion, and it is deliberate. It is a denormalised display name
// derived from the override, not the user's annotation: renaming a custom category rewrites it on
// every affected transaction (UpdateEffectiveCategoryNameByCustomCategoryID) without any annotation
// changing at all. Requiring a capture there would demand history rows for a rename and — worse —
// would train the next reader to allowlist their way past this guard.
var annotationTrackedColumns = func() []string {
	out := make([]string, 0, len(annotationCapturedColumns))
	for _, col := range annotationCapturedColumns {
		if col == "effective_category_name" {
			continue
		}
		out = append(out, col)
	}
	return out
}()

// annotationCaptureExemptions are the annotation-writing statements that legitimately carry no
// capture, keyed by "<path suffix>:<enclosing function>" so a line shift cannot silently retarget an
// entry. Each reason was re-derived by reading the statement, not inherited.
//
// ⚠ Nothing checks that a reason is TRUE — the same hole the history-cascade allowlist documents.
// Re-derive before relying on one.
var annotationCaptureExemptions = map[string]string{
	"repository/transaction_repository.go:TombstoneByExternalID": "" +
		"the #1578 settlement move, and it is FILL-ONLY in every column it touches, so it never " +
		"overwrites a user annotation and has nothing to record. Re-derived from the three gate " +
		"constants above it: the category tuple moves only under settlementCategoryMoveCond " +
		"(`s.category_source = 'manual' AND twin.category_source IS DISTINCT FROM 'manual'`), so " +
		"the twin never has a manual annotation being replaced; `reviewed = twin.reviewed OR " +
		"settlementReviewMoveCond` is monotonic true-ward and reviewed_at keeps the twin's value " +
		"whenever twin.reviewed; tags move only onto a twin with none. If any of those three gates " +
		"is ever loosened into an overwrite, this entry stops being true and the statement needs " +
		"the capture.",

	"repository/profile_repository.go:DeleteUserCascade": "" +
		"GDPR erasure integrity, not a user annotation write. It clears " +
		"custom_category_override_id / category_override_kind on OTHER users' transactions that " +
		"point at a custom category belonging to the user being erased, because that category is " +
		"about to be hard-deleted and fk_transactions_custom_category_override would refuse the " +
		"DELETE otherwise. It writes effective_category_name = pfc_primary, which is the value the " +
		"generated column independently computes once the override is NULL, so the row is left " +
		"self-consistent. ⚠ It IS an unrecorded loss of a surviving user's override, and recording " +
		"it would be a defensible improvement — but the trigger is the GDPR hard-delete exception " +
		"(ADR 2026-08-01), not a user annotation write, so it is out of #1579's scope and is " +
		"flagged in this task's report rather than silently absorbed here.",
}

// annotationWriteFinding is one `UPDATE transactions` statement that assigns a tracked column.
type annotationWriteFinding struct {
	key      string // "<path suffix>:<enclosing function>"
	file     string
	line     int
	columns  []string
	captured bool
}

func (f annotationWriteFinding) String() string {
	return fmt.Sprintf("%s:%d (%s) assigns %s", f.file, f.line, f.key, strings.Join(f.columns, ", "))
}

var annotationUpdateTargetRe = regexp.MustCompile(`(?is)\bUPDATE\s+transactions\b`)

// annotationSetAssignmentRe matches a column on the left of `=` in a SET item: at the start of a
// line, or just after a comma, or immediately after SET. It deliberately does NOT match
// `WHEN category_override_kind = 'custom'` (a CASE condition) or `AND reviewed = $1` (a WHERE
// predicate) — both of which a bare `col\s*=` matched, and both of which are reads, not writes.
func annotationSetAssignmentRe(col string) *regexp.Regexp {
	return regexp.MustCompile(`(?im)(^|,|\bSET)[\t ]*` + regexp.QuoteMeta(col) + `[\t ]*=`)
}

// findAnnotationWrites walks root for production Go files and returns every statement that builds
// or issues an `UPDATE transactions` assigning a tracked column. Kept pure so the meta-test can
// point it at a constructed corpus.
func findAnnotationWrites(t *testing.T, root string) []annotationWriteFinding {
	t.Helper()
	var out []annotationWriteFinding
	fset := token.NewFileSet()

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// The guard's own corpus must not be scanned by the guard's own run.
		if strings.Contains(filepath.ToSlash(path), "/testdata/") && !strings.Contains(filepath.ToSlash(root), "/testdata/") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return fmt.Errorf("parse %s: %w", path, parseErr)
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch n.(type) {
				case *ast.AssignStmt, *ast.ExprStmt, *ast.DeclStmt, *ast.ReturnStmt:
				default:
					return true
				}
				var sb strings.Builder
				if printErr := printer.Fprint(&sb, fset, n); printErr != nil {
					return true
				}
				text := sb.String()
				if !annotationUpdateTargetRe.MatchString(text) {
					return true
				}
				var cols []string
				for _, col := range annotationTrackedColumns {
					if annotationSetAssignmentRe(col).MatchString(text) {
						cols = append(cols, col)
					}
				}
				if len(cols) == 0 {
					return true
				}
				rel := relAnnotationPath(root, path)
				out = append(out, annotationWriteFinding{
					key:      rel + ":" + fn.Name.Name,
					file:     rel,
					line:     fset.Position(n.Pos()).Line,
					columns:  cols,
					captured: strings.Contains(text, "annotationCaptureInsert"),
				})
				return true
			})
		}
		return nil
	})
	require.NoError(t, err)

	sort.Slice(out, func(i, j int) bool {
		if out[i].file != out[j].file {
			return out[i].file < out[j].file
		}
		return out[i].line < out[j].line
	})
	return out
}

// relAnnotationPath expresses a scanned file relative to the scan root, so an exemption key reads
// the same whichever directory the test is invoked from and does not embed a "../" that would break
// the moment the guard is pointed somewhere else.
func relAnnotationPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// TestAnnotationCaptureIsUsedByEveryAnnotationWritingStatement is the guard.
func TestAnnotationCaptureIsUsedByEveryAnnotationWritingStatement(t *testing.T) {
	findings := findAnnotationWrites(t, "..")
	require.NotEmpty(t, findings,
		"the scan found no annotation-writing statement at all, which means it has stopped looking "+
			"at its corpus rather than that the corpus is clean")

	matchedExemptions := map[string]bool{}
	var unguarded []string
	for _, f := range findings {
		if f.captured {
			assert.NotContains(t, annotationCaptureExemptions, f.key,
				"%s carries the capture AND an exemption; drop the exemption", f.key)
			continue
		}
		if _, exempt := annotationCaptureExemptions[f.key]; exempt {
			matchedExemptions[f.key] = true
			continue
		}
		unguarded = append(unguarded, f.String())
	}

	assert.Empty(t, unguarded,
		"annotation-writing statement(s) with no capture (#1569 Task 6 / #1579). Each of these "+
			"overwrites a user's category or review state and records nothing, which is the defect "+
			"#1579 exists to close. Either wrap the statement in the capture CTE (see "+
			"runCapturingUpdate) or add an entry to annotationCaptureExemptions saying why it is "+
			"not an overwrite:\n  %s", strings.Join(unguarded, "\n  "))

	for key := range annotationCaptureExemptions {
		assert.True(t, matchedExemptions[key],
			"exemption %q matches no annotation-writing statement any more. An allowlist entry with "+
				"no edge reads as though a decision were still in force when the code has moved on; "+
				"delete it.", key)
	}
}

// TestAnnotationCaptureGuard_CatchesAnUnguardedWrite constructs the violation and proves the guard
// fails on it. Without this, the guard above would pass just as happily if its regexps had stopped
// matching anything — the failure mode this project has hit three times.
func TestAnnotationCaptureGuard_CatchesAnUnguardedWrite(t *testing.T) {
	findings := findAnnotationWrites(t, filepath.Join("testdata", "annotation_guard"))
	require.Len(t, findings, 2, "the corpus holds exactly one guarded and one unguarded write")

	byFunc := map[string]annotationWriteFinding{}
	for _, f := range findings {
		byFunc[f.key[strings.LastIndex(f.key, ":")+1:]] = f
	}

	unguarded, ok := byFunc["UnguardedReviewWrite"]
	require.True(t, ok, "the constructed violation was not seen at all")
	assert.False(t, unguarded.captured,
		"the guard did not notice that an UPDATE transactions SET reviewed in another file carries "+
			"no capture — this is the exact violation R80 exists to catch")
	assert.ElementsMatch(t, []string{"reviewed", "reviewed_at"}, unguarded.columns)

	guarded, ok := byFunc["GuardedReviewWrite"]
	require.True(t, ok)
	assert.True(t, guarded.captured,
		"the guard flags a statement that DOES carry the capture, so a red result would prove "+
			"nothing about the violation")
}

// TestAnnotationSetAssignmentRe_DoesNotMatchReadsOfATrackedColumn pins the refinement that took two
// passes to get right. The first version of the scan matched a bare `col\s*=`, which reported
// `Update` (whose CASE reads `WHEN category_override_kind = 'custom'`) and
// `UpdateEffectiveCategoryNameByCustomCategoryID` (whose WHERE reads
// `AND custom_category_override_id = $2`) as annotation writes. Two false positives in a guard whose
// only remedy is an allowlist is how allowlists fill up with entries that are not true.
func TestAnnotationSetAssignmentRe_DoesNotMatchReadsOfATrackedColumn(t *testing.T) {
	re := annotationSetAssignmentRe("category_override_kind")

	assert.False(t, re.MatchString("WHEN category_override_kind = 'custom' THEN"),
		"a CASE condition is a read")
	assert.False(t, re.MatchString("  AND category_override_kind = 'custom'"),
		"a WHERE predicate is a read")

	assert.True(t, re.MatchString("SET category_override_kind = $1,"),
		"the first item of a SET list is a write")
	assert.True(t, re.MatchString("\t\t    category_override_kind      = $1,"),
		"a later item of a SET list, at the start of its own line, is a write")
}

// TestAnnotationTrackedColumnsTracksTheProductionList is the check that makes the derivation above
// worth having: if effective_category_name is ever removed from the captured set, or the exclusion
// is copy-pasted into a second column, this says so rather than letting the guard's universe drift
// away from the table's.
func TestAnnotationTrackedColumnsTracksTheProductionList(t *testing.T) {
	assert.Len(t, annotationTrackedColumns, len(annotationCapturedColumns)-1,
		"exactly one captured column (effective_category_name) is excluded from the guard's universe")
	assert.NotContains(t, annotationTrackedColumns, "effective_category_name")
	for _, col := range annotationCapturedColumns {
		if col == "effective_category_name" {
			continue
		}
		assert.Contains(t, annotationTrackedColumns, col,
			"a captured column that is not derived must be guarded")
	}
}
