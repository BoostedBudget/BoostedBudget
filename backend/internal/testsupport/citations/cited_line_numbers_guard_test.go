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
//   - Approximate refs (`~:339`) are exempt by their own convention: `~` means "about here", and
//     holding it to an exact standard is exactly the churn that gets a guard suppressed.
//   - An unresolvable path is SILENT. Citations here are written relative to four different roots
//     and often as a bare basename, and a resolver that guesses wrong would report hundreds of
//     phantom failures on the first run. The dispositions guard
//     (repository/cache_completeness_citations_test.go) enforces path resolution within its own
//     scope, where the convention is fixed.
//
// Measured before it was written, and CORRECTED after review: 196 pattern matches in Go comments
// and SQL comments across the corpus — tracked AND untracked, since the corpus is
// sourcefiles.Union — of which 7 are approximate and skipped and 113 resolve to a file on disk and
// are checked. ZERO were out of range. (The header first said "197 … in tracked Go comments": the
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
	`\b((?:[A-Za-z0-9_./-]+/)*[A-Za-z0-9_.-]+\.(?:go|ts|tsx|sql|md|ya?ml)):(~?)(\d+)(?:-(\d+))?\b`)

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

	files, err := sourcefiles.Union(repoRoot, "backend", sourcefiles.HasExt(".go"))
	if err != nil {
		t.Fatalf("listing backend Go source: %v — this guard must fail closed rather than scan nothing", err)
	}

	var offenders []string
	var matched, approximate, checked int

	for _, rel := range files {
		path := filepath.Join(repoRoot, rel)
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", rel, parseErr)
		}
		citingDir := filepath.Dir(path)

		record := func(text string, line int) {
			for _, m := range citedFileLine.FindAllStringSubmatchIndex(text, -1) {
				if m[5] > m[4] { // the `~` of an approximate ref: "about here", exempt by convention.
					approximate++
					continue
				}
				cited := text[m[2]:m[3]]
				lo, _ := strconv.Atoi(text[m[6]:m[7]])
				hi := lo
				if m[8] >= 0 {
					hi, _ = strconv.Atoi(text[m[8]:m[9]])
				}
				end := m[7]
				if m[8] >= 0 {
					end = m[9]
				}
				matched++
				resolved, ok, n := citedLineInRange(citingDir, roots, cited, lo, hi)
				if !resolved {
					continue
				}
				checked++
				if !ok {
					offenders = append(offenders, rel+":"+strconv.Itoa(line)+" cites "+
						text[m[2]:end]+" but that file has "+strconv.Itoa(n)+" lines")
				}
			}
		}

		for _, group := range file.Comments {
			for _, c := range group.List {
				record(c.Text, fset.Position(c.Pos()).Line)
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
				record(span, line)
			}
			return true
		})
	}

	// ⚠ THE FLOOR IS CALIBRATED AGAINST THE RESOLVED COUNT, NOT THE MATCH COUNT, and the difference
	// is the whole point. The first version of this guard quoted 197 in its header and set the floor
	// at 50; 197 was the number of regexp MATCHES, of which only 113 resolve to a file — so a floor
	// of 50 would have absorbed a 55% collapse in the resolver before firing. A vacuity detector
	// calibrated against a phantom is worse than none, because it reads as protection.
	t.Logf("citation corpus: %d matched, %d approximate (~) skipped, %d resolved and checked",
		matched, approximate, checked)
	if checked < 95 {
		t.Fatalf("resolved only %d exact citations; %d matched the pattern and %d resolved when this "+
			"guard was written. The resolver is broken and a pass here would be vacuous",
			checked, 198, 113)
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
	text := "see repository/foo.go:120 and services/bar.go:10-20, but not baz.go:~339"

	var matched, skipped []string
	for _, m := range citedFileLine.FindAllStringSubmatchIndex(text, -1) {
		end := m[7]
		if m[8] >= 0 {
			end = m[9]
		}
		if m[5] > m[4] {
			skipped = append(skipped, text[m[2]:end])
			continue
		}
		matched = append(matched, text[m[2]:end])
	}

	// The approximate ref must be MATCHED and then SKIPPED, not simply unmatched. Asserting only
	// the first list is what let the previous version of this test pass with the skip deleted.
	if got, want := strings.Join(matched, ","), "repository/foo.go:120,services/bar.go:10-20"; got != want {
		t.Fatalf("matched %v, want %s", got, want)
	}
	if got, want := strings.Join(skipped, ","), "baz.go:~339"; got != want {
		t.Fatalf("skipped %v, want %s — the tilde branch is not firing, so it is dead code", got, want)
	}
}
