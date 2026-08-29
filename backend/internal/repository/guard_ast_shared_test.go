package repository

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Issue #1569 round 14 task 2 — ONE shared, argument-aware AST extraction pass for every guard in
// this package that scans production Go source for SQL. Two independent problems converged on one
// fix:
//
//  1. CI WAS RED: `panic: test timed out after 10m0s` on this package under `go test -race`. The
//     eight slowest tests in the package were ALL mechanical guards, and every one of them
//     independently ran `filepath.Walk` + `parser.ParseFile` over the same ~300+ files under
//     backend/internal — TestNoTransactionReadForgetsTheTombstoneFilter,
//     TestNoConfigReadForgetsTheTombstoneFilter, TestNoCustomCategoryReadForgetsTheTombstoneFilter,
//     TestNoSavingsContributionReadForgetsTheTombstoneFilter, TestNoEncryptedColumnComparedToALiteral,
//     TestEveryEncryptedLiteralExemptTokenNamesItsColumn, TestNoUnallowlistedHistoryDelete and
//     TestEveryAllowlistedHistoryDeleteStillCarriesItsPredicate. goProductionFiles parses the
//     internal/ tree exactly ONCE per test binary; every guard above now reads from that cache
//     instead of re-walking and re-parsing it.
//
//  2. CORRECTNESS HAD DIVERGED: an independent Codex audit of this branch's guard-coherence found
//     that the tombstone read/write guards (config/transaction/custom_category/
//     savings_contribution) grouped a statement's string literals by plain Go-statement boundary
//     (AssignStmt/ValueSpec/ReturnStmt/ExprStmt) with NO notion of a CALL ARGUMENT boundary — so a
//     second, unrelated ARGUMENT to the same call could lend a predicate fragment to a widened
//     statement. history_delete_guard_test.go's guard (G2) already closed this exact hole
//     (#1569 T8-FIX finding E: `db.Exec(ctx, "DELETE FROM x WHERE user_id = $1", "help: deleted_at
//     IS NOT NULL")` must not let the help-text argument satisfy x's predicate). literalUnitsIn
//     below is that fix, generalised from literalUnitsInFunc/assemblyOwner so every guard in this
//     package shares it instead of keeping five variants of "what counts as one query" that can
//     drift apart. assemblyOwner/sqlAssemblyUnit themselves are unchanged — moved here verbatim from
//     history_delete_guard_test.go so there is one home for the primitive, not two.

// ---- Shared parse-once corpus -------------------------------------------------------------------

// goProductionFile is one non-test .go file under a scanned root, parsed exactly once.
type goProductionFile struct {
	rel  string // path relative to root, slash-separated
	fset *token.FileSet
	file *ast.File
}

// goProductionFilesCache memoizes the walk+parse of one root for the lifetime of the test binary.
// Every guard in this file's doc comment scans the SAME root (repoInternalRoot(t)), so a single
// cache keyed by root serves all of them; a second root (encryptedColumnCorpus below covers
// internal/+cmd/, a different corpus with a different inclusion rule) gets its own cache rather than
// forcing one cache to serve two incompatible walks.
type goProductionFilesCache struct {
	mu    sync.Mutex
	byKey map[string]*goProductionFilesEntry
}

type goProductionFilesEntry struct {
	once  sync.Once
	files []goProductionFile
	err   error
}

var sharedGoProductionFiles = &goProductionFilesCache{byKey: map[string]*goProductionFilesEntry{}}

// goProductionFiles parses every .go file under root exactly once per (root, skipTestdata,
// includeTests) key and returns the cached result to every subsequent caller, in this test binary
// run.
//
// Neither bool is a hardcoded exclusion, because the guards that share this cache do NOT all agree
// on either one:
//
//   - skipTestdata: the four tombstone guards (config/transaction/custom_category/
//     savings_contribution) always skipped `testdata` — it holds deliberately-broken fixtures for
//     other guards (annotation_capture_guard_test.go's), not production SQL.
//     history_delete_guard_test.go's scanHistoryDeletes never applied that exclusion. Forcing one
//     shared boundary on both would be exactly the "coverage must not shrink" mistake this task
//     exists to avoid — today's testdata fixtures happen to contain no DELETE/TRUNCATE of a
//     history table, so the walks are currently equivalent in what they'd FIND, but a future
//     fixture could break that silently if the exclusion were merged.
//   - includeTests: encrypted_column_literal_guard_test.go's two guards scan `_test.go` files too
//     ("tests included, since that is where the defect lived" — the founding defect was itself a
//     test fixture). Every other guard scans production code only.
//
// A handful of cache entries (one per combination actually used) cost a handful of extra tree
// walks, never five-plus-N re-parses of the whole tree.
func goProductionFiles(t *testing.T, root string, skipTestdata, includeTests bool) []goProductionFile {
	t.Helper()

	key := root
	if skipTestdata {
		key += "|skip-testdata"
	}
	if includeTests {
		key += "|include-tests"
	}
	sharedGoProductionFiles.mu.Lock()
	entry, ok := sharedGoProductionFiles.byKey[key]
	if !ok {
		entry = &goProductionFilesEntry{}
		sharedGoProductionFiles.byKey[key] = entry
	}
	sharedGoProductionFiles.mu.Unlock()

	entry.once.Do(func() {
		var files []goProductionFile
		err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if skipTestdata && info.IsDir() && info.Name() == "testdata" {
				return filepath.SkipDir
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			if !includeTests && strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fset := token.NewFileSet()
			file, parseErr := parser.ParseFile(fset, path, nil, 0)
			if parseErr != nil {
				return parseErr
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			files = append(files, goProductionFile{rel: filepath.ToSlash(rel), fset: fset, file: file})
			return nil
		})
		entry.files, entry.err = files, err
	})

	if entry.err != nil {
		t.Fatalf("walking %s: %v", root, entry.err)
	}
	return entry.files
}

// ---- Shared argument-aware literal-unit extraction ----------------------------------------------

// sqlAssemblyUnit reports whether n is a statement-sized unit of SQL assembly: the node whose string
// literals may legitimately be halves of ONE query (`A` + helper + `B`). Moved here from
// history_delete_guard_test.go (#1569 round 14 task 2) so every guard shares the same primitive.
func sqlAssemblyUnit(n ast.Node) bool {
	switch n.(type) {
	case *ast.AssignStmt, *ast.ValueSpec, *ast.ReturnStmt, *ast.ExprStmt:
		return true
	}
	return false
}

// assemblyOwner returns the node whose literals lit may be joined with: the nearest enclosing CALL
// ARGUMENT, else the nearest enclosing assembly statement (sqlAssemblyUnit), else lit itself. stack
// holds lit's ancestors, outermost first.
//
// ⚠ THE BOUNDARY IS THE ARGUMENT, NOT THE CALL (#1569 T8-FIX, finding E, generalised in round 14
// task 2). The nearest enclosing CALL ARGUMENT wins over the enclosing statement, so
// `db.Exec(ctx, "DELETE FROM x WHERE user_id = $1", "help: deleted_at IS NOT NULL")` gives three
// units, not one, and the unrelated parameter can no longer satisfy x's predicate. A concatenation
// INSIDE one argument is still one unit. It FAILS OPEN NOWHERE: a literal with no enclosing unit (a
// `case "..."` expression, a switch tag, an `if` condition) is not dropped — it becomes a unit of
// its own, so a query written there is still seen; what it does not get is a sibling's text.
func assemblyOwner(lit ast.Node, stack []ast.Node) ast.Node {
	child := lit
	for i := len(stack) - 1; i >= 0; i-- {
		if call, ok := stack[i].(*ast.CallExpr); ok {
			for _, arg := range call.Args {
				if ast.Node(arg) == child {
					return child
				}
			}
		}
		if sqlAssemblyUnit(stack[i]) {
			return stack[i]
		}
		child = stack[i]
	}
	return lit
}

// literalUnit is one grouped run of string literals sharing an assembly owner — the shared notion of
// "what counts as one query" every AST-based SQL guard in this package now uses. pos is the owner's
// position, for line reporting.
type literalUnit struct {
	pos token.Pos
	sql string
}

// literalUnitsIn partitions every string literal under root — a whole *ast.File (package-level
// consts included) or a single *ast.BlockStmt (one function body) — by its nearest enclosing
// assembly owner (assemblyOwner), and returns one joined SQL string per unit, ordered by position.
//
// Because it is a PARTITION, every literal belongs to exactly one unit: unlike the per-file
// ast.Inspect the four tombstone guards used to run independently (which had to stop descending once
// a statement matched, to avoid double-reporting an outer statement's offender through a nested one),
// there is no double-counting to guard against here — a caller can iterate every unit exactly once.
func literalUnitsIn(root ast.Node) []literalUnit {
	type group struct {
		pos   token.Pos
		parts []string
	}
	groups := map[ast.Node]*group{}
	var stack []ast.Node

	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if str, e := strconv.Unquote(lit.Value); e == nil {
				owner := assemblyOwner(lit, stack)
				if groups[owner] == nil {
					groups[owner] = &group{pos: owner.Pos()}
				}
				groups[owner].parts = append(groups[owner].parts, str)
			}
		}
		stack = append(stack, n)
		return true
	})

	ordered := make([]*group, 0, len(groups))
	for _, g := range groups {
		ordered = append(ordered, g)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].pos < ordered[j].pos })

	out := make([]literalUnit, 0, len(ordered))
	for _, g := range ordered {
		out = append(out, literalUnit{pos: g.pos, sql: strings.Join(g.parts, "\n")})
	}
	return out
}

// literalUnitsInFunc keeps history_delete_guard_test.go's per-FuncDecl call sites unchanged: it is
// literalUnitsIn applied to one function body, returning just the joined SQL strings in order. The
// `fset` parameter is unused now that line reporting lives on literalUnit.pos, but the signature is
// kept so scanHistoryDeletes did not need to change at every call site.
func literalUnitsInFunc(_ *token.FileSet, body *ast.BlockStmt) []string {
	units := literalUnitsIn(body)
	out := make([]string, 0, len(units))
	for _, u := range units {
		out = append(out, u.sql)
	}
	return out
}
