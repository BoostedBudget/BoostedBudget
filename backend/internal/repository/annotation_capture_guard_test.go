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
// SCOPE AND ITS LIMITS, stated rather than implied. Two of these were holes until fix round 2 and
// are now covered; the rest are real and remain:
//   - Production files only (no _test.go). A fixture writing rows by hand is a different hazard
//     with its own lesson in this repo; this guard is about production write paths.
//   - internal/ only. cmd/ builds no SQL.
//   - COVERED SINCE FIX ROUND 2 (D-1): SQL held in a package-level const or var. The walk used to
//     enter function bodies only, and file-scope SQL constants are this package's own idiom
//     (ruleExecutionColumns, settlementCategoryMoveCond, transactionAnnotationHistoryColumns), so
//     an author copying local style escaped it entirely.
//   - COVERED SINCE FIX ROUND 2 (D-3): a statement carrying the capture but not the lock. The
//     guard used to accept the capture alone, which let a new write path reopen the concurrency
//     failures fix round 1 closed.
//   - ⚠ STILL OUT OF SCOPE: SQL ASSEMBLED ACROSS SEVERAL STATEMENTS. Each candidate node is
//     rendered and matched on its own, so `q := "UPDATE transactions t "` followed by
//     `q += "SET reviewed = ..."` matches neither half and is reported by nothing. This is not
//     hypothetical in this file — GetReviewSummary builds its WHERE incrementally in exactly that
//     style — it is simply not a shape any annotation write uses today. Widening to it means
//     tracking a variable's accumulated value, which is dataflow analysis rather than a syntactic
//     scan, and would be a different tool.
//   - ⚠ STILL OUT OF SCOPE: SQL the parser cannot see at all — a column name from a variable, a
//     query read from a file, a table name built at runtime. Same limit the tombstone guards have.
//
// Mutation-proven by TestAnnotationCaptureGuard_CatchesEveryConstructedViolation, which runs the same pure
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
	// ⚠ The entry below is a re-derivation, LEG BY LEG, from the statement and from the caller that
	// feeds it — the discipline the two retracted entries further down exist to teach — because the
	// obvious summary ("the sync path re-writes what it just read") is exactly the shape of a reason
	// that reads true and is not. Read the reason's own text for the argument, not this note.
	"repository/transaction_repository.go:UpdateFromSync": "NOT AN OVERWRITE OF A USER ANNOTATION, and the argument is that the rows carrying one are UNREACHABLE by this statement rather than that their loss is acceptable. The tuple it writes is the stored row's OWN tuple: the service copies CategorySourceField, Reviewed, ReviewedAt, the four override columns, Tags and TagsSource off GetByExternalID onto the mapped row BEFORE rules run, so every leg this statement can change is changed by ApplyRulesToBatch — whose first line skips any row with `CategorySourceField == manual || Reviewed` (services/rule_application_service.go:334). A user-authored category and a user's review state are therefore never the pre-image of a change here. The rows it CAN change are ones the product itself categorized, and each such change is attributed to a specific rule (rule_id, rule_name, definition_snapshot, action_type) in rule_execution_history by the same batch, for exactly the rows whose output changed. The residual, stated rather than hidden: a PRIOR rule-produced tuple (reachable when SetReviewed(false) un-reviews a rule-categorized row) is not copied into transaction_annotation_history. That is not an unrecoverable loss — rule_execution_history retains that rule's identity, definition and execution time, so the prior value is reconstructible from a richer record than a bare before/after pair. Carrying the capture instead would file a tag-only rule's tag change, or a category rule's review flip, under one of the five existing change_source labels, and that label would be FALSE; a false record is worse than a missing one, because this table exists to be believed (see transaction_annotation_capture.go:35-36). A truthful label needs a sixth change_source value, which is a migration plus a published-contract change — huma/operations/transaction_annotation_history.go:116 enumerates the five to API consumers — and is not made by this task.",
	// ⚠ TombstoneByExternalID USED TO BE EXEMPT HERE, on the reason "it is FILL-ONLY in every column
	// it touches, so it never overwrites a user annotation and has nothing to record". Review
	// re-derived that from the SQL and it was FALSE — the third false allowlist reason this issue
	// produced. settlementCategoryMoveCond is
	// `(s.category_source = 'manual' AND twin.category_source IS DISTINCT FROM 'manual')`, which is
	// MANUAL-WINS: a twin already carrying a RULE-assigned override — the common case, since rules
	// run in place before the twin is inserted — had all seven category columns replaced with
	// nothing recorded. The entry is gone because the statement now carries the capture (change
	// source 'settlement', migration 000170), not because the reason was reworded.
	//
	// The lesson is the reason, not the entry: I wrote that reason AFTER reading the three gate
	// constants, and still got it wrong, because I read "fill-only" off the reviewed/tags/notes legs
	// and generalised. Re-derive every leg separately.

	// ⚠ DeleteUserCascade USED TO BE EXEMPT HERE, on the reason "GDPR erasure integrity, not a user
	// annotation write ... it IS an unrecorded loss of a surviving user's override, and recording it
	// would be a defensible improvement — but the trigger is the GDPR hard-delete exception, not a
	// user annotation write, so it is out of #1579's scope". Unlike the three false reasons this
	// issue produced, that one was TRUE as written — and it was still the wrong entry, because a
	// scope boundary is not a reason a loss may go unrecorded. #1590 recorded it instead (change
	// source 'erasure', migration 000172), so the entry is gone because the statement now carries
	// the capture.
	//
	// The lesson: an exemption whose own text says "this IS an unrecorded loss" is a ticket, not a
	// justification. Read them for that shape before adding another.
}

// annotationWriteFinding is one `UPDATE transactions` site that assigns a tracked column: either a
// statement inside a function, or a package-level const/var holding the SQL.
type annotationWriteFinding struct {
	key      string // "<path relative to scan root>:<enclosing function or decl name>"
	file     string
	line     int
	columns  []string
	captured bool // the SQL carries annotationCaptureInsert
	locked   bool // the site goes through the lock-first helper
}

func (f annotationWriteFinding) String() string {
	return fmt.Sprintf("%s:%d (%s) assigns %s [capture=%v lock=%v]",
		f.file, f.line, f.key, strings.Join(f.columns, ", "), f.captured, f.locked)
}

// annotationLockHelper is the prefix shared by runCapturingUpdate and runCapturingStatement — the
// two entry points that take the row lock before running a capturing statement. It is a prefix
// rather than an allowlist of names because they are one concept, and a third wrapper in the same
// family should satisfy the guard without an edit here.
const annotationLockHelper = "runCapturing"

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

		rel := relAnnotationPath(root, path)

		// render prints any node, so one helper serves statements, whole functions and GenDecls.
		render := func(n ast.Node) string {
			var sb strings.Builder
			if printErr := printer.Fprint(&sb, fset, n); printErr != nil {
				return ""
			}
			return sb.String()
		}

		// consider records a site if its text is an UPDATE of transactions assigning a tracked
		// column. lockScope is the text searched for the lock-first helper: for a statement that is
		// the WHOLE enclosing function, because the query is built in one statement and executed in
		// another, so searching the statement alone would never find the call.
		consider := func(name string, n ast.Node, lockScope string) {
			text := render(n)
			if text == "" || !annotationUpdateTargetRe.MatchString(text) {
				return
			}
			var cols []string
			for _, col := range annotationTrackedColumns {
				if annotationSetAssignmentRe(col).MatchString(text) {
					cols = append(cols, col)
				}
			}
			if len(cols) == 0 {
				return
			}
			out = append(out, annotationWriteFinding{
				key:      rel + ":" + name,
				file:     rel,
				line:     fset.Position(n.Pos()).Line,
				columns:  cols,
				captured: strings.Contains(text, "annotationCaptureInsert"),
				locked:   strings.Contains(lockScope, annotationLockHelper),
			})
		}

		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Body == nil {
					continue
				}
				fnText := render(d)
				ast.Inspect(d.Body, func(n ast.Node) bool {
					switch n.(type) {
					case *ast.AssignStmt, *ast.ExprStmt, *ast.DeclStmt, *ast.ReturnStmt:
					default:
						return true
					}
					consider(d.Name.Name, n, fnText)
					return true
				})

			case *ast.GenDecl:
				// #1569 fix round 2, D-1: a package-level const or var holding the SQL was
				// INVISIBLE to this guard, because ast.Inspect only ever ran over function bodies.
				// That is not an exotic shape — it is this package's own idiom (ruleExecutionColumns,
				// transactionAnnotationHistoryColumns, settlementCategoryMoveCond are all file-scope
				// SQL constants), so an author copying local style would have escaped the guard.
				//
				// A file-scope decl has no enclosing function, so its lock scope is itself: a const
				// cannot call runCapturingUpdate at all, and a var that carries the capture without
				// it is reported. That is the safe direction — whoever executes the query is out of
				// sight, so the guard cannot confirm the lock and must not assume it.
				if d.Tok != token.CONST && d.Tok != token.VAR {
					continue
				}
				for _, spec := range d.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok || len(vs.Names) == 0 {
						continue
					}
					consider(vs.Names[0].Name, vs, render(vs))
				}
			}
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
	var uncaptured, unlocked []string
	for _, f := range findings {
		if _, exempt := annotationCaptureExemptions[f.key]; exempt {
			assert.False(t, f.captured && f.locked,
				"%s is fully guarded AND carries an exemption; drop the exemption", f.key)
			matchedExemptions[f.key] = true
			continue
		}
		switch {
		case !f.captured:
			uncaptured = append(uncaptured, f.String())
		case !f.locked:
			// #1569 fix round 2, D-3: the capture and the lock are ONE requirement. A statement
			// carrying the capture CTE but issued straight through db.Exec reopens the fabricated-
			// transition and dropped-record failures that fix round 1 closed, and the first version
			// of this guard was satisfied by the capture alone.
			unlocked = append(unlocked, f.String())
		}
	}

	assert.Empty(t, uncaptured,
		"annotation-writing site(s) with no capture (#1569 Task 6 / #1579). Each of these "+
			"overwrites a user's category or review state and records nothing, which is the defect "+
			"#1579 exists to close. Either route the statement through runCapturingUpdate /"+
			"runCapturingStatement with the capture CTE, or add an entry to "+
			"annotationCaptureExemptions saying why it is not an overwrite:\n  %s",
		strings.Join(uncaptured, "\n  "))

	assert.Empty(t, unlocked,
		"annotation-writing site(s) that carry the capture but do NOT go through the lock-first "+
			"helper (%s...). An unlocked capture records a prior value from a snapshot older than "+
			"the write it describes: it can fabricate a transition no write performed, and it can "+
			"suppress the record entirely when the write happens to restore the stale value. Both "+
			"are measured in services/transaction_annotation_history_concurrency_test.go:\n  %s",
		annotationLockHelper, strings.Join(unlocked, "\n  "))

	for key := range annotationCaptureExemptions {
		assert.True(t, matchedExemptions[key],
			"exemption %q matches no annotation-writing site any more. An allowlist entry with "+
				"no edge reads as though a decision were still in force when the code has moved on; "+
				"delete it.", key)
	}
}

// TestAnnotationCaptureGuard_CatchesEveryConstructedViolation constructs each way the guard can be
// defeated and proves it fails on all of them. Without this, the guard above would pass just as
// happily if its regexps had stopped matching anything — the failure mode this project has hit
// three times — and two of the three cases below were live holes that shipped in fix round 1.
//
// The corpus lives under testdata/, which the go tool does not compile, so the violations can be
// written as ordinary Go without breaking the build.
func TestAnnotationCaptureGuard_CatchesEveryConstructedViolation(t *testing.T) {
	findings := findAnnotationWrites(t, filepath.Join("testdata", "annotation_guard"))

	byName := map[string]annotationWriteFinding{}
	for _, f := range findings {
		byName[f.key[strings.LastIndex(f.key, ":")+1:]] = f
	}

	t.Run("no capture at all", func(t *testing.T) {
		f, ok := byName["UnguardedReviewWrite"]
		require.True(t, ok, "the constructed violation was not seen at all")
		assert.False(t, f.captured,
			"an UPDATE transactions SET reviewed in another file, with no capture, must be reported")
		assert.ElementsMatch(t, []string{"reviewed", "reviewed_at"}, f.columns)
	})

	t.Run("D-3: capture without the lock", func(t *testing.T) {
		f, ok := byName["CapturedButUnlockedWrite"]
		require.True(t, ok, "the constructed violation was not seen at all")
		assert.True(t, f.captured, "it does carry the capture CTE — that is what made it pass before")
		assert.False(t, f.locked,
			"a capture issued straight through Exec has no row lock, so its prior value can come "+
				"from a snapshot older than the write it describes")
	})

	t.Run("D-1: SQL in a package-level const", func(t *testing.T) {
		f, ok := byName["zzPackageLevelUnguardedQuery"]
		require.True(t, ok,
			"a file-scope const holding the SQL was invisible: the walk only entered function bodies")
		assert.False(t, f.captured)
		assert.ElementsMatch(t, []string{"reviewed", "reviewed_at"}, f.columns)
	})

	t.Run("G12: an uncaptured whole-array tags replace", func(t *testing.T) {
		f, ok := byName["UnguardedTagsWrite"]
		require.True(t, ok,
			"a tags write was invisible to this guard: annotationTrackedColumns is derived from "+
				"annotationCapturedColumns, which carried no tags column, so `SET tags =` matched "+
				"nothing. UpdateTags sat in that hole for the whole of #1579 — neither captured nor "+
				"exempted, and an absent reason prompts nobody to check it")
		assert.False(t, f.captured)
		assert.ElementsMatch(t, []string{"tags", "tags_source"}, f.columns)
	})

	t.Run("the compliant control is not flagged", func(t *testing.T) {
		f, ok := byName["GuardedReviewWrite"]
		require.True(t, ok)
		assert.True(t, f.captured)
		assert.True(t, f.locked,
			"a red result would prove nothing about the violations if the guard flagged everything")
	})
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
