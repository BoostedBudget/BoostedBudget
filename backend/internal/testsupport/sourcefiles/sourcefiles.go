// Package sourcefiles lists the corpus the repository's text guards scan.
//
// Issue #1569 T8. Four guards built their corpus from `git ls-files` alone:
// TestEveryCitedTestNameResolves, TestNoNewNoMatcherAssertNotCalled (a CLAUDE.md-listed guard),
// TestNoTypographicQuotesInGoSource and the Markdown citation guard. An untracked file was
// therefore invisible to all four, while every OTHER guard in the tree — which use filepath.Walk —
// saw it. The consequence is a guard that passes locally on the very tree that then fails in CI:
// two citations written during this issue went red only after `git add`, which is the worst
// possible moment for a guard to start speaking.
//
// The fix is `git ls-files` PLUS `git ls-files --others --exclude-standard`, not filepath.Walk.
// Walking looked like the obvious answer and was tried first; it descends into build output and
// dependencies, and — the reason it was abandoned — it reads GITIGNORED files. docs/plans/ is
// ignored in this repository, and the walk immediately reported two "broken citations" inside a
// scratch planning document that is not part of the repository at all. Asking git for the untracked
// set instead gets exactly the files a `git add -A` would stage: everything the guard's verdict
// should cover, and nothing it should not.
package sourcefiles

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Union returns the slash-separated paths, relative to repoRoot, of every tracked-or-untracked
// file under dir (itself relative to repoRoot) for which match reports true. Paths git reports but
// that no longer exist (a staged deletion, a mid-rebase tree) are dropped, so every returned path
// is readable.
func Union(repoRoot, dir string, match func(rel string) bool) ([]string, error) {
	seen := map[string]bool{}

	for _, args := range [][]string{
		{"ls-files", "-z", "--", dir},
		{"ls-files", "-z", "--others", "--exclude-standard", "--", dir},
	} {
		out, err := exec.Command("git", append([]string{"-C", repoRoot}, args...)...).Output()
		if err != nil {
			return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		for _, rel := range strings.Split(string(out), "\x00") {
			if rel == "" || !match(rel) {
				continue
			}
			if _, statErr := os.Stat(filepath.Join(repoRoot, rel)); statErr != nil {
				continue
			}
			seen[rel] = true
		}
	}

	files := make([]string, 0, len(seen))
	for rel := range seen {
		files = append(files, rel)
	}
	sort.Strings(files)
	return files, nil
}

// HasExt is the common match: a plain extension test.
func HasExt(ext string) func(string) bool {
	return func(rel string) bool { return strings.HasSuffix(rel, ext) }
}
