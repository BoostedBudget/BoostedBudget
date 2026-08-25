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
// Measured before it was written: 197 exact citations in tracked Go comments, ZERO out of range.
// So this ships as a ratchet, not a cleanup, and its value is entirely in what it refuses next.
// Mutation-checked by TestCitedLineInRange_Detects and by adding a `:99999` citation to a real
// file and watching the corpus check name it.

// citedFileLine matches `some/path/file.ext:NNN` and `…:NNN-MMM`. The negative lookbehind cannot be
// expressed in RE2, so the `~` exemption is applied by inspecting the byte before the match.
var citedFileLine = regexp.MustCompile(
	`\b((?:[A-Za-z0-9_./-]+/)*[A-Za-z0-9_.-]+\.(?:go|ts|tsx|sql|md|ya?ml)):(\d+)(?:-(\d+))?\b`)

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
	checked := 0

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
				if m[0] > 0 && text[m[0]-1] == '~' {
					continue
				}
				cited := text[m[2]:m[3]]
				lo, _ := strconv.Atoi(text[m[4]:m[5]])
				hi := lo
				if m[6] >= 0 {
					hi, _ = strconv.Atoi(text[m[6]:m[7]])
				}
				end := m[5]
				if m[6] >= 0 {
					end = m[7]
				}
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

	if checked < 50 {
		t.Fatalf("resolved only %d exact citations; there were 197 when this guard was written. "+
			"The resolver is broken and a pass here would be vacuous", checked)
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
	var got []string
	for _, m := range citedFileLine.FindAllStringSubmatchIndex(text, -1) {
		if m[0] > 0 && text[m[0]-1] == '~' {
			continue
		}
		end := m[5]
		if m[6] >= 0 {
			end = m[7]
		}
		got = append(got, text[m[2]:end])
	}
	want := "repository/foo.go:120,services/bar.go:10-20"
	if strings.Join(got, ",") != want {
		t.Fatalf("got %v, want %s", got, want)
	}
}
