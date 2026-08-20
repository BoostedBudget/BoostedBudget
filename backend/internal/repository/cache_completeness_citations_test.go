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
//   - Semantic line-number checking BEYOND range. Refs are often written approximately ("~:339"),
//     there are hundreds of them, and asserting that a cited line contains a particular construct
//     generates churn on every unrelated edit above it.
//
//     ⚠ But the surrounding claim here was FALSIFIED and has been narrowed. It used to say a line
//     checker "would not have caught the real drifts anyway". It would have caught these, all found
//     in one later audit of this very branch: three citations to
//     internal/repository/budget_repository.go at lines 1194, 1208 and 1212, in a file that
//     #1565's deletions shortened to 1137 lines, and eleven main.go citations left pointing at a comment fragment,
//     at `DB: db,` and at `})` after this branch moved main.go by ~30 lines.
//
//     So an IN-RANGE check is now enforced (TestExactCitationsAreInRange below) for exact
//     `file.go:NNN` refs — cheap, zero false positives, and it catches precisely the class that
//     actually occurred. Approximate `~:NNN` refs are exempt by the `~` convention: it means
//     "about here", and holding it to an exact standard is what would generate the churn.
//
//     The main.go line numbers were not re-anchored but REMOVED: the ordering they were cited for
//     is proved mechanically by TestMutatingRoutesRegisteredAfterMiddlewareChain, so the number
//     carried no information a guard did not already carry, and only drifted.
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
	reVaguePointer = regexp.MustCompile(`(?i)\b(see the report\b|the report's\b|in report\b|flagged in report\b|per the report\b|task brief\b|see below\b|TODO\b|FIXME\b)`)
)

// citationSearchRoots are the directories a cited path may be relative to. Details are written
// relative to internal/ ("services/x.go") or to the module root ("cmd/server/main.go"), and
// sometimes as a bare basename.
// Details are written relative to internal/ ("services/x.go"), to internal/huma/
// ("operations/budget.go", "inputs/mcp_context.go"), or to the module root ("cmd/server/main.go"),
// and sometimes as a bare basename. All of those are established shorthand in this file, so all
// resolve; what does NOT resolve is a path with directory segments that match nothing.
var citationSearchRoots = []string{".", "..", "../..", "../../..", "../huma"}

func resolveCitedPath(t *testing.T, cited string) bool {
	t.Helper()
	for _, root := range citationSearchRoots {
		if _, err := os.Stat(filepath.Join(root, cited)); err == nil {
			return true
		}
	}
	// A citation WITH directory segments must resolve exactly. An earlier version fell back to
	// searching the tree for any file with the same basename, so "bogus/path/README.md" passed as
	// long as SOME README.md existed — validating the wrong artifact is worse than not validating
	// (Codex review, #7). A BARE filename ("account_service.go") is still resolved by tree search:
	// that shorthand is used throughout these details, it is unambiguous in this codebase, and
	// rejecting it outright flagged 63 legitimate citations.
	if strings.ContainsRune(cited, '/') {
		return false
	}
	base := filepath.Base(cited)
	found := false
	_ = filepath.WalkDir("../..", func(path string, d os.DirEntry, err error) error {
		if err != nil || found || d == nil {
			return nil //nolint:nilerr // a walk error must not silently fail the citation
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

// registeredOperation is one huma.Operation literal found in internal/huma/operations.
type registeredOperation struct {
	method  string
	path    string
	secured bool
}

// scanOperations extracts every huma.Operation literal from the operations package, brace-balanced.
//
// Brace-balanced and NOT regex-delimited on purpose: a non-greedy `huma.Operation{...}` match stops
// at the first `}`, which is the one inside `Security: []map[string][]string{{"clerk": {}}}` — so a
// naive scan silently truncates every operation that declares Security before Path, which is most of
// them. Measured while building this: the naive version found 117 of 195 operations and reported
// paths for none of the ones that mattered.
func scanOperations(t *testing.T) map[string]registeredOperation {
	t.Helper()
	dir := filepath.Join("..", "huma", "operations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read operations dir: %v", err)
	}
	out := map[string]registeredOperation{}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			t.Fatalf("read %s: %v", n, err)
		}
		for id, op := range scanOperationsInSource(string(b)) {
			out[id] = op
		}
	}
	if len(out) == 0 {
		t.Fatal("scanned zero huma.Operation literals — the scan is broken and every check below " +
			"would pass vacuously")
	}
	return out
}

// maskLineComments blanks the body of every `//` comment with spaces, PRESERVING LENGTH so every
// byte offset in the original source stays valid.
//
// Adversarial review (Codex) found the defect this exists to fix: brace counting ran on raw source,
// so a `{` or `}` inside a comment shifted the depth counter and could terminate the operation
// literal early or run it past its end. An earlier fix of mine stripped comments only from the
// already-extracted Security slice — which fixed the inner scan and left the outer one broken. Mask
// once, up front, and both scans are correct by construction.
func maskLineComments(src string) string {
	out := []byte(src)
	for i := 0; i+1 < len(out); i++ {
		if out[i] != '/' || out[i+1] != '/' {
			continue
		}
		for j := i; j < len(out) && out[j] != '\n'; j++ {
			out[j] = ' '
		}
	}
	return string(out)
}

func scanOperationsInSource(raw string) map[string]registeredOperation {
	reOpen := regexp.MustCompile(`huma\.Operation\{`)
	reID := regexp.MustCompile(`OperationID:\s*"([^"]+)"`)
	rePath := regexp.MustCompile(`Path:\s*"([^"]+)"`)
	reMethod := regexp.MustCompile(`Method:\s*(?:http\.Method(\w+)|"(\w+)")`)
	reSecurity := regexp.MustCompile(`Security:\s*\[\]map\[string\]\[\]string\{`)

	out := map[string]registeredOperation{}
	{
		src := maskLineComments(raw)
		for _, loc := range reOpen.FindAllStringIndex(src, -1) {
			i := loc[1] - 1
			depth, j := 1, i+1
			for ; j < len(src) && depth > 0; j++ {
				switch src[j] {
				case '{':
					depth++
				case '}':
					depth--
				}
			}
			body := src[i:j]
			id := reID.FindStringSubmatch(body)
			if id == nil {
				continue
			}
			op := registeredOperation{}
			if m := rePath.FindStringSubmatch(body); m != nil {
				op.path = m[1]
			}
			if m := reMethod.FindStringSubmatch(body); m != nil {
				op.method = strings.ToUpper(m[1] + m[2])
			}
			// Security must be read BRACE-BALANCED, not to end-of-line. Go's `.` does not span
			// newlines, and this codebase routinely writes
			//     Security: []map[string][]string{
			//         {"bearerAuth": {}},
			//     },
			// so a line-scoped capture reads empty and reports a secured operation as unsecured.
			// That false positive fired on updateUserAccount while this was being written — the same
			// regex-versus-brace-balanced trap that truncated the operation scan above.
			if loc := reSecurity.FindStringIndex(body); loc != nil {
				k := loc[1] - 1
				d2, e2 := 1, k+1
				for ; e2 < len(body) && d2 > 0; e2++ {
					switch body[e2] {
					case '{':
						d2++
					case '}':
						d2--
					}
				}
				// Comments are already masked out of `src`, so this brace test cannot be
				// fooled by a `{` in a comment.
				op.secured = strings.Contains(body[k+1:e2-1], "{")
			}
			out[id[1]] = op
		}
	}
	return out
}

// TestDispositionOpsResolve is the machine-checkable half of a CoveredByHTTPMiddleware claim.
func TestDispositionOpsResolve(t *testing.T) {
	ops := scanOperations(t)

	var problems []string
	for key, named := range dispositionOps {
		if _, ok := repoMethodDispositions[key]; !ok {
			problems = append(problems, key+" is named in dispositionOps but has no disposition entry")
			continue
		}
		if len(named) == 0 {
			problems = append(problems, key+" has an empty ops list — omit the key instead of asserting nothing")
		}
		for _, id := range named {
			op, ok := ops[id]
			if !ok {
				problems = append(problems, key+" cites operation "+strconv.Quote(id)+
					", which does not exist in internal/huma/operations")
				continue
			}
			switch op.method {
			case "GET", "HEAD", "OPTIONS", "":
				problems = append(problems, key+" cites "+id+" whose method is "+strconv.Quote(op.method)+
					" — the cache-invalidation middleware skips those, so it cannot provide coverage")
			}
			if !op.secured {
				problems = append(problems, key+" cites "+id+
					", which declares no non-empty Security — the middleware's gate is len(Security)==0, "+
					"so it never runs for that operation")
			}
		}
	}

	// The two maps must not drift: a middleware-coverage claim has to name its operation.
	for key, d := range repoMethodDispositions {
		if d.kind != dispositionCoveredByHTTPMiddleware {
			continue
		}
		if _, ok := dispositionOps[key]; !ok {
			problems = append(problems, key+" is dispositionCoveredByHTTPMiddleware but names no operation "+
				"in dispositionOps — the coverage claim is then prose only, checkable by nothing")
		}
	}

	sort.Strings(problems)
	if len(problems) > 0 {
		t.Errorf("%d disposition/operation problem(s):\n  - %s", len(problems), strings.Join(problems, "\n  - "))
	}
}

// TestOperationScanIsNotFooledByBracesInComments pins the defect Codex's adversarial review found in
// the FIRST attempt at this fix.
//
// The original scan counted braces over raw source. My first correction stripped comments only from
// the already-extracted `Security` slice — so the inner scan was fixed while the OUTER scan, which
// decides where the operation literal ends, still counted braces inside comments.
//
// ONLY THE TRUNCATION DIRECTION IS TESTED, and that is a deliberate, measured choice rather than an
// omission. A stray `}` in a comment ends the literal early, so Path/Method/Security below it become
// invisible and a secured operation reads unsecured — case 1, which fails when maskLineComments is
// reverted to the identity function (verified).
//
// I tried and FAILED to construct a harmful case for the opposite direction. An unbalanced `{` does
// extend the literal past its end, but the extension is benign for this scanner: `FindAllStringIndex`
// still matches the next operation's own `huma.Operation{` independently, so nothing is lost, and the
// merged body cannot inherit the neighbour's Security because the comment's own
// `Security: []map[string][]string{` text is the FIRST `reSecurity` match and anchors the inner scan
// locally. Two drafts of an "extends" case passed under the mutant before I observed the scan output
// directly instead of reasoning about it. Rather than keep an assertion that cannot fail, the finding
// is recorded here: masking fixes both directions, but only one of them was ever exploitable.
func TestOperationScanIsNotFooledByBracesInComments(t *testing.T) {
	cases := []struct {
		name        string
		src         string
		wantID      string
		wantSecured bool
		wantPath    bool // whether Path/Method must be populated (false when the op declares none)
	}{
		{
			// A `}` in a comment ends the literal early: without masking, Path, Method and Security
			// are never seen — the operation reads unsecured with an empty path.
			name: "closing brace in comment truncates the literal",
			src: `var _ = huma.Operation{
	OperationID: "probeOp",
	// historical note: the old shape was map[string]string} and is gone
	Method:      http.MethodPost,
	Path:        "/api/v1/probe",
	Security:    []map[string][]string{{"clerk": {}}},
}`,
			wantID: "probeOp", wantSecured: true, wantPath: true,
		},
		{
			// Control, the direction the earlier nit-fix aimed at: an EMPTY Security literal whose
			// trailing comment contains a brace must still read UNSECURED. Masking must not degrade
			// into "any brace anywhere means secured".
			name: "empty security with a braced comment stays unsecured",
			src: `var _ = huma.Operation{
	OperationID: "unsecuredOp",
	Method:      http.MethodPost,
	Path:        "/api/v1/unsecured",
	Security:    []map[string][]string{}, // was {"clerk": {}} before the MCP change
}`,
			wantID: "unsecuredOp", wantSecured: false, wantPath: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scanOperationsInSource(tc.src)
			op, ok := got[tc.wantID]
			if !ok {
				t.Fatalf("operation %q not scanned at all; scanned %v", tc.wantID, keysOf(got))
			}
			if tc.wantPath && op.path == "" {
				t.Errorf("Path empty — the literal was truncated before reaching Path")
			}
			if tc.wantPath && op.method != "POST" {
				t.Errorf("Method = %q, want POST — the literal was truncated before reaching Method", op.method)
			}
			if op.secured != tc.wantSecured {
				t.Errorf("secured = %v, want %v — brace counting was fooled by a comment. "+
					"A false `true` here means the guard would pass an UNSECURED mutating operation.",
					op.secured, tc.wantSecured)
			}
		})
	}
}

func keysOf(m map[string]registeredOperation) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestExactCitationsAreInRange checks that every EXACT `file.go:NNN` citation points at a line the
// file actually has.
//
// It exists because the "a line checker would not have caught the real drifts" claim above turned
// out to be wrong. One audit of this branch found fourteen stale refs: three into
// budget_repository.go past its new end (#1565's deletions shortened it by ~70 lines) and eleven
// main.go refs left pointing at a comment fragment, at `DB: db,` and at `})`.
//
// Scope is deliberately narrow — in-range only, and only for refs written EXACTLY. It does not
// assert the line contains any particular construct: many citations legitimately point at the doc
// comment ABOVE a function, and pinning content would fire on every unrelated edit. Refs written
// `~:NNN` are exempt, because `~` already declares them approximate.
func TestExactCitationsAreInRange(t *testing.T) {
	// (?<!~) is unavailable in Go's regexp, so the tilde is captured and rejected in code.
	re := regexp.MustCompile(`(~?)\b((?:[a-z0-9_]+/)*[a-z0-9_]+\.go):(\d+)\b`)

	type ref struct {
		path string
		line int
	}
	seen := map[ref]int{}
	for _, file := range []string{
		"cache_completeness_repo_test.go",
		"cache_completeness_write_set_test.go",
		"cache_completeness_citations_test.go",
	} {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			if m[1] == "~" {
				continue // approximate by convention
			}
			n, err := strconv.Atoi(m[3])
			if err != nil {
				continue
			}
			seen[ref{m[2], n}]++
		}
	}

	// Fail closed: no refs found means the extraction broke, not that the prose is clean.
	if len(seen) < 50 {
		t.Fatalf("extracted only %d exact file:line citations — expected well over a hundred; the "+
			"extraction is broken and a pass here would be meaningless", len(seen))
	}

	var stale []string
	for r, count := range seen {
		resolved := ""
		for _, root := range citationSearchRoots {
			c := filepath.Join(root, r.path)
			if _, err := os.Stat(c); err == nil {
				resolved = c
				break
			}
		}
		if resolved == "" {
			// Bare-basename shorthand is resolved (and enforced) by TestDispositionCitationsResolve;
			// this test only judges line numbers for paths it can locate unambiguously.
			continue
		}
		b, err := os.ReadFile(resolved)
		if err != nil {
			continue
		}
		if n := len(strings.Split(string(b), "\n")); r.line > n {
			stale = append(stale, r.path+":"+strconv.Itoa(r.line)+" (cited "+strconv.Itoa(count)+
				"x) — file has only "+strconv.Itoa(n)+" lines")
		}
	}

	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("%d citation(s) point past the end of the file they name. A line number that cannot "+
			"be opened is worse than none: it reads as evidence. Re-anchor it, write it as ~:NNN if "+
			"it is approximate, or drop the number if a guard already proves the point:\n  - %s",
			len(stale), strings.Join(stale, "\n  - "))
	}
}
