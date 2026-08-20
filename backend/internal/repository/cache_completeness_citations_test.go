package repository

// Issue #1537 — makes the dispositions' citations resolvable, not decorative.
//
// Every prose disposition cites files, methods and tests as its evidence. Nothing checked those
// citations, and an audit found the predictable result: entries citing a report that was never
// committed, an entry naming a caller that does not call it, and an entry describing a sibling
// guard's classification as something it is not. Prose that cites a file nobody can open is
// indistinguishable from prose that is simply wrong.
//
// WHAT IS ENFORCED, and why only these classes:
//   - Referenced .go and .md paths must resolve on disk. Highest yield: every dangling citation the
//     audit found was in this class, and it has no false positives to speak of.
//   - Vague pointers ("see the report", "task brief", "see below", "TODO") are rejected outright.
//     They are unfalsifiable by construction, and every one found so far pointed at a non-artifact.
//   - An entry whose reasoning DEFERS to other evidence ("see X", "same as X", "mirrors X",
//     "covered by X") must cite at least one resolvable file. This is what breaks the citation
//     CYCLES and hedge chains the audit mapped — two entries naming each other as their only
//     authority, and a nine-deep chain rooted in "see task brief" — without a graph detector. It is
//     deliberately NOT applied to every prose entry: a self-contained claim ("writes
//     job-coordination bookkeeping, not domain data") needs no external citation, and demanding one
//     of all 83 flagged 42 of them, which is noise rather than signal. A guard that cries wolf gets
//     worked around.
//
// WHAT IS DELIBERATELY NOT ENFORCED:
//   - Semantic line-number checking. Refs are written approximately ("~:339"), there are hundreds
//     of them, and the audit showed a checker would not have caught the real drifts anyway while
//     generating constant churn.
//   - Cycle detection as a gate. Of the four suspected cycles, three carried independent evidence
//     on both sides; a detector would have failed them wrongly. The at-least-one-citation rule
//     catches the genuinely vacuous case instead.
//
// A guard that cries wolf gets worked around, so this errs toward silence over noise.

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
)

var (
	// Paths like services/household_service.go, repository/analytics_repository.go,
	// queue/jobs/item_cleanup.go, cache/purge.go — plus bare basenames.
	reCitedGoFile = regexp.MustCompile(`\b((?:[a-z0-9_]+/)*[a-z0-9_]+\.go)\b`)
	reCitedMDFile = regexp.MustCompile(`\b((?:[a-z0-9_./-]+/)*[a-z0-9_.-]+\.md)\b`)
	// Unfalsifiable pointers. "the report"/"task brief" named artifacts that never existed.
	// Deferral markers: the entry leans on evidence recorded elsewhere rather than stating its own.
	reDeferral     = regexp.MustCompile(`(?i)\b(see (also )?(the )?[a-z]|same (as|reasoning)|mirrors\b|covered by\b|per the\b|as documented in\b|sibling\b|supersedes\b)`)
	reVaguePointer = regexp.MustCompile(`(?i)\b(see the report\b|the report's\b|task brief\b|see below\b|TODO\b|FIXME\b)`)
)

// citationSearchRoots are the directories a cited path may be relative to. Details are written
// relative to internal/ ("services/x.go") or to the module root ("cmd/server/main.go"), and
// sometimes as a bare basename.
var citationSearchRoots = []string{".", "..", "../..", "../../.."}

func resolveCitedPath(t *testing.T, cited string) bool {
	t.Helper()
	for _, root := range citationSearchRoots {
		if _, err := os.Stat(filepath.Join(root, cited)); err == nil {
			return true
		}
	}
	// Bare basename, or a path whose leading segments were elided: search the module tree.
	base := filepath.Base(cited)
	found := false
	_ = filepath.WalkDir("../..", func(path string, d os.DirEntry, err error) error {
		if err != nil || found || d == nil {
			return nil //nolint:nilerr // a walk error must not fail the citation
		}
		if !d.IsDir() && d.Name() == base {
			found = true
		}
		return nil
	})
	return found
}

// dispositionDetails extracts each entry's key and full detail string from the map literal,
// concatenating Go `+` string expressions. Details here run to thousands of characters split across
// many `+` operands, so a line-oriented scan sees fragments — the audit noted a citation split as
// "cache-invalidation-" + "dispositions.md".
func dispositionDetails(t *testing.T) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "cache_completeness_repo_test.go", nil, 0)
	if err != nil {
		t.Fatalf("parse guard file: %v", err)
	}

	var flatten func(ast.Expr) string
	flatten = func(e ast.Expr) string {
		switch v := e.(type) {
		case *ast.BasicLit:
			if v.Kind == token.STRING {
				s, uerr := strconv.Unquote(v.Value)
				if uerr != nil {
					return ""
				}
				return s
			}
		case *ast.BinaryExpr:
			return flatten(v.X) + flatten(v.Y)
		case *ast.ParenExpr:
			return flatten(v.X)
		}
		return ""
	}

	out := map[string]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range vs.Names {
			if name.Name != "repoMethodDispositions" || i >= len(vs.Values) {
				continue
			}
			cl, ok := vs.Values[i].(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, el := range cl.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key := flatten(kv.Key)
				entry, ok := kv.Value.(*ast.CompositeLit)
				if !ok || len(entry.Elts) < 2 {
					continue
				}
				out[key] = flatten(entry.Elts[1])
			}
		}
		return true
	})
	if len(out) == 0 {
		t.Fatal("extracted zero disposition details — the AST walk is broken, and every check below " +
			"would pass vacuously")
	}
	return out
}

func TestDispositionCitationsResolve(t *testing.T) {
	details := dispositionDetails(t)

	var unresolved, vague, uncited []string
	for key, detail := range details {
		if strings.TrimSpace(detail) == "" {
			continue // derived entries carry no prose by design
		}

		if m := reVaguePointer.FindString(detail); m != "" {
			vague = append(vague, key+" — unfalsifiable pointer "+strconv.Quote(strings.TrimSpace(m)))
		}

		defers := reDeferral.MatchString(detail)
		resolvable := 0
		for _, m := range reCitedGoFile.FindAllStringSubmatch(detail, -1) {
			if resolveCitedPath(t, m[1]) {
				resolvable++
			} else {
				unresolved = append(unresolved, key+" -> "+m[1])
			}
		}
		for _, m := range reCitedMDFile.FindAllStringSubmatch(detail, -1) {
			if resolveCitedPath(t, m[1]) {
				resolvable++
			} else {
				unresolved = append(unresolved, key+" -> "+m[1])
			}
		}
		if defers && resolvable == 0 {
			uncited = append(uncited, key)
		}
	}

	sort.Strings(unresolved)
	sort.Strings(vague)
	sort.Strings(uncited)

	if len(unresolved) > 0 {
		t.Errorf("%d citation(s) do not resolve to a file on disk:\n  - %s\n\nA disposition citing a "+
			"file nobody can open is indistinguishable from one that is simply wrong. Every dangling "+
			"citation found by audit was in this class, including a report cited as \"the actual "+
			"deliverable\" that exists in no commit.", len(unresolved), strings.Join(unresolved, "\n  - "))
	}
	if len(vague) > 0 {
		t.Errorf("%d disposition(s) rest on an unfalsifiable pointer:\n  - %s\n\nName the file and the "+
			"symbol. Every such pointer found so far referred to a non-artifact.",
			len(vague), strings.Join(vague, "\n  - "))
	}
	if len(uncited) > 0 {
		t.Errorf("%d disposition(s) DEFER to other evidence but cite no resolvable file: %s\n\nProse with no first-hand "+
			"citation is where the citation cycles live — two entries can name each other as their only "+
			"authority and both look reviewed. Cite the code.",
			len(uncited), strings.Join(uncited, ", "))
	}
}
