package citations

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/sourcefiles"
)

// Issue #1569 T8 — the OTHER axis of citation rot.
//
// Two guards already exist and neither looks at a line number.
// TestEveryCitedTestNameResolves checks NAMES. TestMethodCitationsPointInsideTheirMethod checks
// METHOD ATTRIBUTION. Every `file.go:NNNN`, every `:NNNN-MMMM` range and every sibling-file
// reference drifts between them completely unwatched, which is why one file in this repository
// kept rotting while the whole suite stayed green — its anchors drifted four separate times during
// this issue alone, and twice a citation was broken by the very commit that wrote it.
//
// WHAT THIS GUARD DOES: a cited line must EXIST. If a comment says `budget_repository.go:1194` and
// that file has 1137 lines, the citation is unambiguously wrong and this test says so, naming both
// ends. That is the class that actually occurred: #1565's deletions shortened budget_repository.go
// under three citations, and a ~30-line shift in main.go left eleven citations pointing at a
// comment fragment, at `DB: db,` and at `})`.
//
// WHAT IT DELIBERATELY DOES NOT DO, and this is the honest boundary:
//
//   - It cannot tell a WRONG line from a right one when both are in range. An anchor that slid
//     forty lines and now points at an unrelated function is invisible here. Only the human, or a
//     structural anchor (cite the METHOD, which the sibling guard checks), catches that.
//   - Approximate refs are exempt by their own convention: `~` means "about here", and holding it
//     to an exact standard is exactly the churn that gets a guard suppressed. ALL THREE positions
//     the tree writes count — `~file.go:339`, `file.go~:339` and `file.go:~339` — reconciled with
//     the sibling guard's documented grammar in the #1569 whole-branch review. The BARE forms
//     (`:339`, `~:339`) name no file, so there is nothing here to resolve; they are checked for
//     adjacency by repository/cache_completeness_citations_test.go instead.
//   - An unresolvable path is SILENT. Citations here are written relative to four different roots
//     and often as a bare basename, and a resolver that guesses wrong would report hundreds of
//     phantom failures on the first run. The dispositions guard
//     (repository/cache_completeness_citations_test.go) enforces path resolution within its own
//     scope, where the convention is fixed.
//
// Measured before it was written, CORRECTED after review, and RE-MEASURED when the tilde grammar
// was widened: 387 pattern matches in Go comments and SQL comments, scoped Markdown and frontend
// .ts/.tsx across the corpus — tracked AND untracked, since the corpus is sourcefiles.Union — of
// which 15 are approximate and skipped and 190 resolve to a file on disk and are checked. ZERO were out of range. (The header first said "197 … in tracked Go comments": the
// count was the MATCH count, 43% of which never resolve, and "tracked" had been made false by the
// same round that widened the corpus. Numbers re-measured by the t.Logf in the guard itself, so
// they can be re-derived rather than believed.)
// So this ships as a ratchet, not a cleanup, and its value is entirely in what it refuses next.
// Mutation-checked by TestCitedLineInRange_Detects and by adding a `:99999` citation to a real
// file and watching the corpus check name it.

// citedFileLine matches `some/path/file.ext:NNN`, `…:NNN-MMM` and the APPROXIMATE form
// `…:~NNN`, capturing the tilde so the caller can skip it.
//
// ⚠ The first version of this pattern was `:(\d+)` with a separate "is the byte before the path a
// tilde?" check. That check was DEAD CODE: `(\d+)` cannot match `~339`, so `file.go:~339` was
// excluded by the regexp and never by the branch, and deleting the branch from both guard and
// meta-test left both PASSING. The convention is real — six `file.ext:~NNN` refs exist in the tree
// — so the tilde is captured here instead, which makes the skip a branch that can be observed to
// fire.
var citedFileLine = regexp.MustCompile(
	`(~?)\b((?:[A-Za-z0-9_./-]+/)*[A-Za-z0-9_.-]+\.(?:go|ts|tsx|sql|md|ya?ml))(~?):(~?)(\d+)(?:-(\d+))?\b`)

// Capture-group indices into citedFileLine. Named rather than numbered because the tilde may
// appear in three places and an off-by-one silently turns an approximate ref into an exact one —
// which is the mistake the sibling guard's own index constants were introduced to prevent
// (repository/cache_completeness_citations_test.go, citGroupTildeBeforePath and friends).
const (
	citGroupTildeBeforePath = 1
	citGroupPath            = 2
	citGroupTildeAfterPath  = 3
	citGroupTildeAfterColon = 4
	citGroupLo              = 5
	citGroupHi              = 6
)

// citedRefIsApproximate reports whether a submatch carries `~` in ANY of its three positions.
// `~` means "about here", so the ref is exempt from the exact-range rule by convention.
func citedRefIsApproximate(m []int) bool {
	for _, g := range []int{citGroupTildeBeforePath, citGroupTildeAfterPath, citGroupTildeAfterColon} {
		if m[2*g+1] > m[2*g] {
			return true
		}
	}
	return false
}

// citedLineInRange reports whether a citation resolves and, if it does, whether it is in range.
// resolved is false when no root produced a file, which the guard treats as silence.
func citedLineInRange(citingDir string, roots []string, cited string, lo, hi int) (resolved, ok bool, lines int) {
	for _, r := range append([]string{citingDir}, roots...) {
		p := filepath.Join(r, cited)
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			continue
		}
		b, readErr := os.ReadFile(p)
		if readErr != nil {
			continue
		}
		n := strings.Count(string(b), "\n")
		if len(b) > 0 && !strings.HasSuffix(string(b), "\n") {
			n++
		}
		return true, lo >= 1 && hi <= n && lo <= hi, n
	}
	return false, false, 0
}

func TestCitedLineNumbersAreInRange(t *testing.T) {
	const repoRoot = "../../../.."
	roots := []string{repoRoot,
		filepath.Join(repoRoot, "backend"),
		filepath.Join(repoRoot, "backend", "internal"),
		filepath.Join(repoRoot, "apps", "web"),
		filepath.Join(repoRoot, "apps", "web", "src"),
	}

	goFiles, err := sourcefiles.Union(repoRoot, "backend", sourcefiles.HasExt(".go"))
	if err != nil {
		t.Fatalf("listing backend Go source: %v — this guard must fail closed rather than scan nothing", err)
	}

	// MARKDOWN AND FRONTEND SOURCE ARE IN SCOPE TOO (#1569 T8 fix round 1). The rule is about a
	// line number that cannot exist, and that is language-independent. The gap mattered: the
	// primary artefact of #1569 is an ADR — a Markdown file, full of file:line citations — and no
	// line-citation guard could see it at all. Measured when this was added: 120 matches in scoped
	// Markdown (46 resolved) and 50 in .ts/.tsx (24 resolved), zero out of range in either.
	textFiles, err := sourcefiles.Union(repoRoot, ".", func(rel string) bool {
		if strings.HasSuffix(rel, ".md") {
			return strings.HasPrefix(rel, "docs/") || strings.HasPrefix(rel, "backend/") ||
				strings.HasPrefix(rel, "apps/") || rel == "CLAUDE.md"
		}
		return (strings.HasSuffix(rel, ".ts") || strings.HasSuffix(rel, ".tsx")) &&
			(strings.HasPrefix(rel, "apps/") || strings.HasPrefix(rel, "packages/"))
	})
	if err != nil {
		t.Fatalf("listing Markdown and frontend source: %v", err)
	}

	var offenders []string
	var matched, approximate, checked int

	record := func(rel, citingDir, text string, line int) {
		for _, m := range citedFileLine.FindAllStringSubmatchIndex(text, -1) {
			if citedRefIsApproximate(m) { // "about here", exempt by convention in all three slots.
				approximate++
				continue
			}
			cited := text[m[2*citGroupPath]:m[2*citGroupPath+1]]
			lo, _ := strconv.Atoi(text[m[2*citGroupLo]:m[2*citGroupLo+1]])
			hi := lo
			end := m[2*citGroupLo+1]
			if m[2*citGroupHi] >= 0 {
				hi, _ = strconv.Atoi(text[m[2*citGroupHi]:m[2*citGroupHi+1]])
				end = m[2*citGroupHi+1]
			}
			matched++
			resolved, ok, n := citedLineInRange(citingDir, roots, cited, lo, hi)
			if !resolved {
				continue
			}
			checked++
			if !ok {
				offenders = append(offenders, rel+":"+strconv.Itoa(line)+" cites "+
					text[m[2*citGroupPath]:end]+" but that file has "+strconv.Itoa(n)+" lines")
			}
		}
	}

	for _, rel := range goFiles {
		path := filepath.Join(repoRoot, rel)
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", rel, parseErr)
		}
		citingDir := filepath.Dir(path)

		for _, group := range file.Comments {
			for _, c := range group.List {
				record(rel, citingDir, c.Text, fset.Position(c.Pos()).Line)
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, e := strconv.Unquote(lit.Value)
			if e != nil {
				return true
			}
			line := fset.Position(lit.Pos()).Line
			for _, span := range sqlComment.FindAllString(s, -1) {
				record(rel, citingDir, span, line)
			}
			return true
		})
	}

	for _, rel := range textFiles {
		path := filepath.Join(repoRoot, rel)
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		citingDir := filepath.Dir(path)
		for i, line := range strings.Split(string(b), "\n") {
			record(rel, citingDir, line, i+1)
		}
	}

	// ⚠ THE FLOOR IS CALIBRATED AGAINST THE RESOLVED COUNT, NOT THE MATCH COUNT, and the difference
	// is the whole point. The first version of this guard quoted 197 in its header and set the floor
	// at 50; 197 was the number of regexp MATCHES, of which only 113 then resolved to a file — so a floor
	// of 50 would have absorbed a 55% collapse in the resolver before firing. A vacuity detector
	// calibrated against a phantom is worse than none, because it reads as protection.
	t.Logf("citation corpus: %d matched, %d approximate (~) skipped, %d resolved and checked",
		matched, approximate, checked)
	if checked < 160 {
		t.Fatalf("resolved only %d exact citations; %d matched the pattern and %d resolved when this "+
			"guard was written. The resolver is broken and a pass here would be vacuous",
			checked, 387, 190)
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Fatalf("%d citation(s) point past the end of the file they name (#1569 T8).\n"+
			"A line number that cannot exist is a citation nobody can follow. Re-anchor it by\n"+
			"finding the text it MEANT — never by arithmetic on the old number — or cite the\n"+
			"method instead, which does not drift:\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}

// TestCitedLineInRange_Detects is the constructed case. A guard that only ever ran against a green
// corpus would be indistinguishable from one that resolves nothing, which is why the corpus check
// also fails closed on a low resolved-count.
func TestCitedLineInRange_Detects(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "short.go"), []byte("a\nb\nc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	roots := []string{dir}

	cases := []struct {
		name              string
		cited             string
		lo, hi            int
		resolved, inRange bool
	}{
		{"a line inside the file", "short.go", 2, 2, true, true},
		{"the last line", "short.go", 3, 3, true, true},
		{"one past the end", "short.go", 4, 4, true, false},
		{"a range spilling past the end", "short.go", 2, 9, true, false},
		{"an inverted range", "short.go", 3, 1, true, false},
		{"a file that does not resolve is silent", "nope.go", 1, 1, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolved, ok, _ := citedLineInRange(dir, roots, tc.cited, tc.lo, tc.hi)
			if resolved != tc.resolved || ok != tc.inRange {
				t.Fatalf("citedLineInRange(%s:%d-%d) = (resolved=%v, ok=%v), want (%v, %v)",
					tc.cited, tc.lo, tc.hi, resolved, ok, tc.resolved, tc.inRange)
			}
		})
	}
}

// TestCitedFileLine_MatchesAndSkipsTheApproximateForm pins the extraction. A regexp that matched
// nothing would make the guard pass on a tree full of dangling anchors.
func TestCitedFileLine_MatchesAndSkipsTheApproximateForm(t *testing.T) {
	// ALL THREE TILDE POSITIONS, because the tree writes all three (#1569 whole-branch review).
	// This guard used to accept `file.go:~N` only, and the two guards disagreed about the grammar:
	// cache_completeness_citations_test.go's reCitationAfterFragment documents four spellings and
	// treats every one as approximate, while here `~file.go:296` was read as an EXACT citation
	// (the tilde is not a path character, so the path matched and the tilde group stayed empty)
	// and `file.go~:296` matched nothing at all and was invisible. One of those over-reports and
	// the other under-reports; neither is the documented convention.
	//
	// The bare forms (`:296`, `~:296`) are deliberately still out of scope HERE: with no filename
	// there is nothing to resolve and nothing to range-check. They are covered by the adjacency
	// rule in repository/cache_completeness_citations_test.go, which reads them relative to the
	// file the citation lives in.
	text := "see repository/foo.go:120 and services/bar.go:10-20, " +
		"but not baz.go:~339, nor ~qux.go:12, nor quux.go~:13"

	var matched, skipped []string
	for _, m := range citedFileLine.FindAllStringSubmatchIndex(text, -1) {
		end := m[2*citGroupLo+1]
		if m[2*citGroupHi] >= 0 {
			end = m[2*citGroupHi+1]
		}
		ref := text[m[2*citGroupTildeBeforePath]:end]
		if citedRefIsApproximate(m) {
			skipped = append(skipped, ref)
			continue
		}
		matched = append(matched, ref)
	}

	// The approximate refs must be MATCHED and then SKIPPED, not simply unmatched. Asserting only
	// the first list is what let the previous version of this test pass with the skip deleted.
	if got, want := strings.Join(matched, ","), "repository/foo.go:120,services/bar.go:10-20"; got != want {
		t.Fatalf("matched %v, want %s", got, want)
	}
	if got, want := strings.Join(skipped, ","), "baz.go:~339,~qux.go:12,quux.go~:13"; got != want {
		t.Fatalf("skipped %v, want %s — every documented tilde position must be matched and then "+
			"skipped; one that is unmatched is invisible and one that is read as exact is a false "+
			"positive waiting to happen", got, want)
	}
}
