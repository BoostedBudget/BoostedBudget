package repository

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/sourcefiles"
)

// TestNoTypographicQuotesInGoSource is the Go-level twin of
// scripts/check-typographic-quotes.sh (#1487).
//
// WHY IT EXISTS, and it is not redundancy. The shell guard is real and it works — it is what caught
// the defect that motivated this test. But it runs only from `make test-scripts` and in CI, and
// `go test ./...` cannot reach it. That gap is not theoretical: this branch shipped a U+201D into
// backend/internal/repository/account_repository.go, every local `go test ./...` stayed green, and CI
// went red on one character. A guard the local loop structurally cannot run is a guard that finds
// defects late.
//
// WHAT gofmt DOES. `gofmt -w` rewrites a pair of ASCII single quotes inside a doc comment into
// typographic double quotes. Comments documenting SQL are the usual victims, because '' is how SQL
// spells the empty string:
//
//	// COALESCE(...): 'USD' for currency_code, '' for institution_name
//
// becomes ... U+201D for institution_name. The character is invisible in review, compiles fine, and
// silently corrupts the documented SQL. Do NOT fix an occurrence by running gofmt -- that is the tool
// that produces it. Replace the character by hand with the ASCII '' or `` pair it came from.
//
// ⚠ THE OPERATIONAL RULE: `gofmt -w` IS UNSAFE ON ANY FILE WHOSE COMMENTS CONTAIN A '' PAIR, AND
// CATASTROPHIC ON A DIRECTORY.
//
// A directory-wide run is the bigger blast radius: the tree is not gofmt-clean at HEAD, so
// `gofmt -w .` reformats files you never touched and corrupts every '' pair it passes -- measured
// twice in a single #1569 task, 19 lines across 15 files both times, most of them in code the
// author had no business changing.
//
// ⚠ BUT "just gofmt the one file you are editing" IS NOT A SAFE RULE, and an earlier version of
// this very paragraph said it was. `gofmt -w encrypted_column_literal_guard_test.go` -- a single
// named file, freshly written by its author -- corrupted that file's OWN doc comment on the next
// run, because the comment quotes `email = ''` while explaining a guard about empty strings. The
// blast radius shrinks to one file; the hazard does not go away. The rule is about CONTENT, not
// scope: if the file documents SQL, gofmt will eat its quotes.
//
// Every one of those three runs was caught here, named line by line, which is the only reason none
// of them left the working tree. Related: #1569 T9 was told not to run gofmt on a pre-existing
// dirty file for the same reason, and the repo's own diffs carry unrelated realignment from
// earlier sweeps.
//
// ⚠ AND THIS FILE IS PERMANENTLY `gofmt -l`-DIRTY, BY DESIGN. It documents the corruption, so
// gofmt wants to apply it. Do not "fix" that in a formatting cleanup -- running gofmt here
// rewrites the examples into the very characters they warn about.
//
// SCOPE, deliberately identical to the shell guard: git-tracked .go files under backend/. Untracked
// scratch files are out of scope (you may be mid-edit), and non-.go files legitimately contain
// typographic quotes in prose.
//
// This file itself must never contain the literal characters, or it would report itself. They are
// written as escapes below for exactly that reason.
func TestNoTypographicQuotesInGoSource(t *testing.T) {
	const (
		leftDoubleQuote  = "\u201C"
		rightDoubleQuote = "\u201D"
	)

	// Repo root: backend/internal/repository -> backend/internal -> backend -> root.
	const repoRoot = "../../.."

	goFiles, err := sourcefiles.Union(repoRoot, "backend", sourcefiles.HasExt(".go"))
	if err != nil {
		t.Fatalf("listing backend Go source: %v -- this guard must fail closed rather than "+
			"silently scan nothing", err)
	}

	// Fail closed. A scan of zero files would pass vacuously and read as proof.
	if len(goFiles) < 100 {
		t.Fatalf("scanned only %d tracked .go files under backend/ -- expected hundreds. The scan "+
			"is broken (wrong working directory, or git ls-files returned nothing) and a passing "+
			"result here would be meaningless", len(goFiles))
	}

	// The WORKING COPY is what matters -- it is what you are about to commit. Reading HEAD instead
	// would pass on a corruption you just introduced and have not committed yet, which is the exact
	// moment this guard is supposed to speak up.
	var findings []string
	for _, rel := range goFiles {
		b, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			// Tracked but absent from the working tree (mid-rebase, or a deletion not yet staged).
			// Nothing to scan; the shell guard and CI cover the committed state.
			continue
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, leftDoubleQuote) || strings.Contains(line, rightDoubleQuote) {
				findings = append(findings, rel+":"+strconv.Itoa(i+1)+"  "+strings.TrimSpace(line))
			}
		}
	}

	if len(findings) > 0 {
		t.Errorf("found typographic double quotes (U+201C/U+201D) in %d tracked .go line(s) under "+
			"backend/ -- almost certainly gofmt -w corruption of an ASCII '' or `` pair in a doc "+
			"comment (#1487).\n\nFix BY HAND; do NOT run gofmt -w, which is what produces this.\n\n%s",
			len(findings), strings.Join(findings, "\n"))
	}
}
