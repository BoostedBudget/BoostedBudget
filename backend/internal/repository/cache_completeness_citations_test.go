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
	// "same over-capacity bookkeeping as" and "structurally identical to" are deferrals in every
	// sense that matters — the entry's verdict rests on ANOTHER entry's reasoning — and both slipped
	// past the list above by not using any of its verbs.
	reDeferral = regexp.MustCompile(`(?i)\b(see (also )?(the )?[a-z]|same (as|reasoning)|same [a-z-]+ bookkeeping as\b|structurally identical to\b|mirrors\b|covered by\b|per the\b|as documented in\b|sibling\b|supersedes\b)`)
	// "Task 10b-2a-iii" / "Task 10b-2b" as a BARE lead ("Task 10b-2a-iii: sole caller is …") is the
	// same unfalsifiable shape as "see the report", and for the same reason: this very file records
	// that the artifact those task labels point at, task-10b-3-report.md, was never committed in any
	// commit. Eight-plus entries opened with one. A task label is not evidence; the file is.
	// Deliberately NOT matched when the label appears with its disclaimer attached (the entries that
	// say "Task 10b-2a-iii; that report was never committed — see <docs file>"), which is why the
	// pattern requires the label to be followed by a colon, comma, or close-paren rather than by
	// prose that qualifies it.
	reVaguePointer = regexp.MustCompile(`(?i)(\b(see the report\b|the report's\b|in report\b|flagged in report\b|per the report\b|task brief\b|see below\b|TODO\b|FIXME\b)|\bTask 10b-2[ab](-iii)?\s*[:.,)])`)
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

// citedGuardFile is one prose-carrying disposition map: the file holding it, the map variable's
// name, and a short label used in failure messages.
//
// The jobs guard (internal/queue/jobs) is listed here because it had NO citation guard of its own
// and was carrying stale refs — investment_holdings_sync.go cited investment_sync_service.go:339
// for a bump that lives at :235, and investment_transactions_sync.go cited :204 for one at :370.
// Nothing was looking. Parsing another package's file as plain text costs nothing and the AST walk
// does not care which package the file declares.
type citedGuardFile struct {
	path   string
	mapVar string
	label  string
}

func citedGuardFiles() []citedGuardFile {
	return []citedGuardFile{
		{"cache_completeness_repo_test.go", "repoMethodDispositions", "repo"},
		{filepath.Join("..", "queue", "jobs", "cache_completeness_jobs_test.go"), "jobFileDispositions", "jobs"},
	}
}

// NOTE on the extractors below: entry details are Go `+` string expressions running to thousands of
// characters across many operands, so a LINE-oriented scan sees fragments — an audit once found a
// citation split as "cache-invalidation-" + "dispositions.md". dispositionDetailsFrom concatenates
// the operands first, which is why every guard in this file goes through it.
//
// A single-file `dispositionDetails` wrapper used to live here; it was superseded by
// allDispositionDetails once the guards began covering the jobs file too, and removed in #1537
// round 9 rather than left as an unused alias.

// allDispositionDetails is every prose entry across every guard file, keyed "label/entryKey" so a
// failure message names which guard the entry lives in.
func allDispositionDetails(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, g := range citedGuardFiles() {
		for k, v := range dispositionDetailsFrom(t, g.path, g.mapVar) {
			out[g.label+"/"+k] = v
		}
	}
	return out
}

func dispositionDetailsFrom(t *testing.T, path, mapVar string) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse guard file %s: %v", path, err)
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
			if name.Name != mapVar || i >= len(vs.Values) {
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
		t.Fatalf("extracted zero disposition details from %s's %s — the AST walk is broken, and every "+
			"check below would pass vacuously", path, mapVar)
	}
	return out
}

func TestDispositionCitationsResolve(t *testing.T) {
	details := allDispositionDetails(t)

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
	return out
}

// TestDispositionOpsResolve is the machine-checkable half of a CoveredByHTTPMiddleware claim.
func TestDispositionOpsResolve(t *testing.T) {
	ops := scanOperations(t)

	var problems []string
	for key, named := range dispositionOps {
		d, ok := repoMethodDispositions[key]
		if !ok {
			problems = append(problems, key+" is named in dispositionOps but has no disposition entry")
			continue
		}
		// Naming an operation here IS the assertion "this entry's coverage comes from that
		// operation's invalidation middleware". For any other disposition kind that assertion is
		// about nothing, and nine of the 44 keys were making it — all DerivedNoCacheImpact, whose
		// verdict is proven by write-set disjointness and does not involve middleware at all. Both
		// directions are now pinned: CoveredByHTTPMiddleware entries MUST appear (below), and only
		// they MAY appear (here).
		if d.kind != dispositionCoveredByHTTPMiddleware {
			problems = append(problems, key+" names an operation in dispositionOps but is not "+
				"dispositionCoveredByHTTPMiddleware — naming a covering operation asserts exactly "+
				"that coverage, so either correct the disposition or drop the key")
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
	// The jobs guard is in this list because it was NOT, and it was carrying stale citations of its
	// own with nothing looking at them (#1537 T41). citedGuardFiles() supplies its path.
	files := []string{
		"cache_completeness_repo_test.go",
		"cache_completeness_write_set_test.go",
		"cache_completeness_citations_test.go",
	}
	for _, g := range citedGuardFiles() {
		if g.path != "cache_completeness_repo_test.go" {
			files = append(files, g.path)
		}
	}
	for _, file := range files {
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

// TestMethodCitationsPointInsideTheirMethod checks that a citation of the form
//
//	`someService.DoThing` (`internal/services/some_service.go:248`)
//
// names a line that actually falls inside DoThing's body.
//
// WHY CONTAINMENT AND NOT PROXIMITY. I first tried "the identifier must appear within +/-8 lines of
// the cited line" and measured it before adopting it: 10 of 17 pairs would have failed. Every one
// was a FALSE positive, because these citations deliberately point at a CALL SITE inside the named
// function rather than at its declaration — `budget_service.go:248` is the
// `s.budgetRepo.CreateWithCategories(...)` call inside `CreateBudget`, which begins at :105. That is
// the more useful citation, and a proximity check punishes it. The citations were better than my
// checker.
//
// Containment is the right relation, and measured 11/11 with zero failures on adoption. It stays
// silent on correct citations of both kinds — declaration or call site — and speaks only when a line
// has drifted outside the function it is attributed to, which is the failure mode that actually
// occurred on this branch when #1565's deletions and a metric cleanup moved several hundred lines.
//
// Coverage is modest by construction: it needs `Type.Method` adjacent to the citation, which is 11
// pairs today. It costs nothing and grows as more citations adopt that form. Line refs with no
// method name (a bare SQL predicate, say) are not covered here — TestExactCitationsAreInRange bounds
// those, and one off-by-one predicate ref that survived both checks was found by hand this round, so
// neither guard should be read as making prose trustworthy.
func TestMethodCitationsPointInsideTheirMethod(t *testing.T) {
	// BOTH orders. The original regex required method-then-file, and the entire "#1537 cluster A"
	// family — every entry that traces a caller chain — writes file-then-method:
	//     `services/account_service.go:554` (`LinkAccounts`)
	// so 10 of those pairs were invisible. Measured after accepting both orders: the newly-visible
	// pairs had drifted 14-75 lines. A guard whose coverage depends on which side of the parenthesis
	// the reader put the method name is not measuring the prose, it is measuring the punctuation.
	goPath := "((?:[a-z0-9_]+/)*[a-z0-9_]+\\.go)"
	ident := "[A-Za-z_][A-Za-z0-9_]*"
	patterns := []*regexp.Regexp{
		// `Type.Method` (`file.go:NNN`)
		regexp.MustCompile("`(?:" + ident + "\\.)?(" + ident + ")`[,;]?\\s*\\(?`" + goPath + ":(\\d+)`"),
		// `file.go:NNN` (`Method`) / (`Type.Method`)
		regexp.MustCompile("`" + goPath + ":(\\d+)`[,;]?\\s*\\(`(?:" + ident + "\\.)?(" + ident + ")`"),
	}
	// order[i] tells which capture group holds the method for patterns[i].
	methodGroup := []int{1, 3}
	pathGroup := []int{2, 1}
	lineGroup := []int{3, 2}

	type pair struct {
		method, path, line string
	}
	var pairs []pair
	for _, g := range citedGuardFiles() {
		b, err := os.ReadFile(g.path)
		if err != nil {
			t.Fatalf("read %s: %v", g.path, err)
		}
		for i, re := range patterns {
			for _, m := range re.FindAllStringSubmatch(string(b), -1) {
				pairs = append(pairs, pair{m[methodGroup[i]], m[pathGroup[i]], m[lineGroup[i]]})
			}
		}
	}

	var violations []string
	checked := 0
	for _, p := range pairs {
		resolved := resolveCitedFileForRange(p.path)
		if resolved == "" || !strings.HasSuffix(resolved, ".go") {
			continue // path shorthand is TestDispositionCitationsResolve's business
		}
		line, convErr := strconv.Atoi(p.line)
		if convErr != nil {
			continue
		}

		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, resolved, nil, 0)
		if parseErr != nil {
			continue // not our business to police unparseable files
		}
		found := false
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Name.Name != p.method || fn.Body == nil {
				continue
			}
			found = true
			lo, hi := fset.Position(fn.Pos()).Line, fset.Position(fn.End()).Line
			checked++
			if line < lo || line > hi {
				violations = append(violations, p.path+":"+p.line+" is cited for "+p.method+
					", but "+p.method+"'s body spans "+strconv.Itoa(lo)+"-"+strconv.Itoa(hi)+
					" — the line has drifted out of the function it is attributed to")
			}
			break
		}
		// A SILENT SKIP here was the guard's second blind spot: when the named method was absent from
		// the cited file, the loop simply fell through, so "`Foo`, see `bar.go:12`" passed whether or
		// not bar.go contained any trace of Foo.
		//
		// It is NOT, however, an error outright, and the briefing that asked for that was wrong:
		// measured over all pairs, absence fires on the most common legitimate idiom in this file —
		// citing a method at its CALL SITE in another file (`budgetService.UpdateCategory`
		// (`budget_service.go:672`) is `s.budgetRepo.UpdateCategory(...)`, and budget_service.go
		// naturally declares no such function). Five of the nine absences were of that kind.
		//
		// So absence falls back to the weaker relation that IS available: the identifier must appear
		// within a few lines of the cited one. Proximity was rejected as the PRIMARY relation for good
		// reason (see the note above — it punishes call-site citations by demanding the declaration),
		// but as a fallback where containment is impossible it is exactly right, and it caught four
		// stale refs that the silent skip had been swallowing.
		if !found {
			checked++
			if !identifierNearLine(resolved, p.method, line, 3) {
				violations = append(violations, p.path+":"+p.line+" is cited for "+p.method+
					", which the file neither declares nor mentions within 3 lines of that ref — "+
					"the citation names nothing a reader can find")
			}
		}
	}

	if checked < 15 {
		t.Fatalf("only %d method citations were containment-checkable — the extraction is broken, "+
			"and a pass would be meaningless", checked)
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Errorf("%d method citation(s) point outside the method they name:\n  - %s",
			len(violations), strings.Join(violations, "\n  - "))
	}
}

// identifierNearLine reports whether ident appears within window lines either side of line in path.
// Used only as the fallback relation for a cited name the file does not declare (a call-site or
// table-name citation).
func identifierNearLine(path, ident string, line, window int) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return true // unreadable file is TestDispositionCitationsResolve's business, not ours
	}
	lines := strings.Split(string(b), "\n")
	lo, hi := line-1-window, line-1+window
	if lo < 0 {
		lo = 0
	}
	if hi >= len(lines) {
		hi = len(lines) - 1
	}
	if lo > hi {
		return false
	}
	return strings.Contains(strings.Join(lines[lo:hi+1], "\n"), ident)
}

// ── Bare `:NNN` refs ────────────────────────────────────────────────────────────────────────────

// reAnyCitedPath matches a cited path of any extension. The bare-ref scanner needs the non-Go
// matches too: a `migrations/….up.sql:67-70` citation carries its own line numbers, and without
// consuming them the scanner would read the `:67-70` as a bare ref.
var reAnyCitedPath = regexp.MustCompile(`\b((?:[a-z0-9_.-]+/)*[a-z0-9_.-]+\.(?:go|md|sql|yaml|yml|json|ts|tsx))(?::(\d+))?\b`)

// reBareLineRef matches a backticked, filename-less line reference — `:1212`, `:472-478`,
// `:115,233,689`. Backticks are required deliberately: an unquoted ":123" appears in prose for other
// reasons (issue numbers, percentages), and this guard errs toward silence.
var reBareLineRef = regexp.MustCompile("`:(\\d+(?:[-,]\\d+)*)`")

// bareRefsWithOwner walks one detail string in order and pairs every bare `:NNN` ref with the .go
// file most recently named before it. Returns (file, line) pairs; refs with no resolvable owning .go
// file (none named yet, or the most recent path was a .sql/.md) are skipped.
func bareRefsWithOwner(detail string) map[string][]int {
	type event struct {
		at   int
		path string // "" for a bare ref
		nums string
	}
	var events []event
	for _, m := range reAnyCitedPath.FindAllStringSubmatchIndex(detail, -1) {
		events = append(events, event{at: m[0], path: detail[m[2]:m[3]]})
	}
	for _, m := range reBareLineRef.FindAllStringSubmatchIndex(detail, -1) {
		events = append(events, event{at: m[0], nums: detail[m[2]:m[3]]})
	}
	sort.Slice(events, func(i, j int) bool { return events[i].at < events[j].at })

	out := map[string][]int{}
	current := ""
	for _, e := range events {
		if e.path != "" {
			if strings.HasSuffix(e.path, ".go") {
				current = e.path
			}
			// A non-Go path (a migration, a doc) does NOT take ownership and does NOT reset. These
			// details routinely name a migration parenthetically mid-sentence and then carry on
			// citing the repository file under discussion — `custom_category_repository.go:520` …
			// `migrations/…up.sql:67-70` … `:565`, where `:565` is plainly the repository again.
			// Resetting here skipped exactly that ref, and it was one of the out-of-range ones.
			continue
		}
		if current == "" {
			continue
		}
		for _, part := range strings.FieldsFunc(e.nums, func(r rune) bool { return r == '-' || r == ',' }) {
			if n, err := strconv.Atoi(part); err == nil {
				out[current] = append(out[current], n)
			}
		}
	}
	return out
}

// TestBareLineRefsAreInRange closes the hole TestExactCitationsAreInRange left wide open.
//
// That test requires the shape `file.go:NNN`. A ref DETACHED from its filename — `(`:1212`), written
// that way precisely because the file was named a clause earlier — matches neither citation guard,
// and there were 63 of them. Two were out of range, and one of those was a number the citations
// preamble specifically boasts of having fixed: `:1212` against budget_repository.go, still there,
// just written bare. A guard whose coverage depends on the citation's punctuation is not a guard.
//
// Resolution rule: a bare ref belongs to the .go file most recently named in the SAME detail string.
// That is exactly how a reader resolves it, and it is why the tracker resets on a .sql/.md citation
// rather than reaching back past it.
func TestBareLineRefsAreInRange(t *testing.T) {
	details := allDispositionDetails(t)

	lineCounts := map[string]int{}
	countLines := func(path string) int {
		if n, ok := lineCounts[path]; ok {
			return n
		}
		n := -1
		if resolved := resolveCitedFileForRange(path); resolved != "" {
			if b, err := os.ReadFile(resolved); err == nil {
				n = len(strings.Split(string(b), "\n"))
			}
		}
		lineCounts[path] = n
		return n
	}

	var stale []string
	checked := 0
	for key, detail := range details {
		for path, lines := range bareRefsWithOwner(detail) {
			total := countLines(path)
			if total < 0 {
				continue // unresolvable path is TestDispositionCitationsResolve's business
			}
			for _, ln := range lines {
				checked++
				if ln > total {
					stale = append(stale, key+": bare `:"+strconv.Itoa(ln)+"` resolves against "+path+
						", which has only "+strconv.Itoa(total)+" lines")
				}
			}
		}
	}

	if checked < 20 {
		t.Fatalf("only %d bare line refs were resolvable — there are dozens; the scanner is broken "+
			"and a pass here would be meaningless", checked)
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("%d bare line ref(s) point past the end of the file they inherit:\n  - %s",
			len(stale), strings.Join(stale, "\n  - "))
	}
}

// resolveCitedFileForRange resolves a cited path to something on disk. Unlike resolveCitedPath it
// RETURNS the path (the range checks need to read the file), and it resolves a bare basename only
// when the tree search finds exactly ONE file with that name — an ambiguous basename is skipped
// rather than range-checked against an arbitrary one of several candidates.
func resolveCitedFileForRange(cited string) string {
	for _, root := range citationSearchRoots {
		c := filepath.Join(root, cited)
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	if strings.ContainsRune(cited, '/') {
		return ""
	}
	var hits []string
	_ = filepath.WalkDir("../..", func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil //nolint:nilerr // a walk error must not silently pass the citation
		}
		if !d.IsDir() && d.Name() == cited {
			hits = append(hits, path)
		}
		return nil
	})
	if len(hits) == 1 {
		return hits[0]
	}
	return ""
}

// ── Cited SQL predicates ────────────────────────────────────────────────────────────────────────

// reCitedSQLPredicate matches a SQL predicate a disposition quotes as evidence, in the two shapes
// these details actually use:
//
//  1. backticked, starting with a SQL keyword:  `WHERE id = $3 AND user_id = $4`
//  2. bare, a qualified column compared to a string literal:  AND a.status = 'active'
//
// Shape 2 is unbackticked on purpose. The claim that rotted three times on this branch —
// "the join keeps AND a.status = 'active'" — was written without backticks every single time, so a
// backtick-only guard would have watched it drift.
var reCitedSQLPredicate = regexp.MustCompile(
	"`((?i:WHERE|AND|LEFT JOIN|JOIN|ON CONFLICT|ORDER BY|GROUP BY|NOT EXISTS|INSERT INTO|DELETE FROM|UPDATE|SELECT DISTINCT ON|DISTINCT ON)\\s[^`]{2,160})`" +
		"|((?i:AND|WHERE)\\s+[a-z_][a-z0-9_]*\\.[a-z_][a-z0-9_]*\\s*=\\s*'[^']{1,40}')" +
		// Shape 3: a backticked fragment comparing a QUALIFIED column to a string literal, with no
		// leading keyword — `(a.status = 'active') DESC NULLS LAST, a.id`. The replacement text for
		// the drifted #1561 claim is exactly this shape, and leaving it unchecked would put the
		// correction on the same unverified footing as the error it replaces.
		"|`([^`]{0,40}[a-z_][a-z0-9_]*\\.[a-z_][a-z0-9_]*\\s*=\\s*'[^']{1,40}'[^`]{0,120})`")

// reSQLNoise rejects predicate-shaped matches that are prose about SQL rather than a quotation of
// it: a fragment with an ellipsis, a placeholder, or a `<alias>` metavariable is not claiming to be
// literal text.
var reSQLNoise = regexp.MustCompile(`\.\.\.|…|<[a-z_]+>|\$n\b|\$x\b|\$y\b`)

// sqlHaystack returns a file's source with everything that is NOT executable text blanked out:
// Go line comments, Go block comments, and SQL `--` comments inside raw string literals. Length is
// preserved so nothing shifts.
//
// Masking SQL `--` comments is the whole point, not a detail. balance_snapshot_repository.go's
// query carries a long `--` comment that says, verbatim, "#1561 removed AND a.status = 'active'
// from the household ANALYTICS joins" — so a guard that searched raw source would find the
// predicate in the very comment that REPUDIATES it and report the citation as verified. That is
// how prose-vs-code drift survives a mechanical check.
func sqlHaystack(src string) string {
	out := []byte(src)
	blankToEOL := func(i int) int {
		j := i
		for ; j < len(out) && out[j] != '\n'; j++ {
			out[j] = ' '
		}
		return j
	}
	for i := 0; i+1 < len(out); i++ {
		switch {
		case out[i] == '/' && out[i+1] == '/':
			i = blankToEOL(i)
		case out[i] == '-' && out[i+1] == '-':
			i = blankToEOL(i)
		case out[i] == '/' && out[i+1] == '*':
			j := i + 2
			for ; j+1 < len(out) && (out[j] != '*' || out[j+1] != '/'); j++ {
				if out[j] != '\n' {
					out[j] = ' '
				}
			}
			out[i], out[i+1] = ' ', ' '
			i = j
		}
	}
	return string(out)
}

var reWhitespaceRun = regexp.MustCompile(`\s+`)

// reImmediateCitation is the gap allowed between a quoted SQL fragment and the file it is attributed
// to: whitespace, an opening paren, an opening backtick. Nothing else.
var reImmediateCitation = regexp.MustCompile("^[\\s]*[(\\[]?[\\s]*`?$")

func flattenSQLForCompare(s string) string {
	return strings.ToLower(strings.TrimSpace(reWhitespaceRun.ReplaceAllString(s, " ")))
}

// TestCitedSQLPredicatesExistInTheirFile is the guard class that was missing entirely, and it is the
// one that matters most: nothing checked that a SQL fragment a disposition quotes as its evidence is
// actually IN the file it attributes it to.
//
// That is precisely the citation that rotted, three separate times, in six places at once:
// "GetAggregatedHistoryForHousehold's join keeps `AND a.status = 'active'`
// (balance_snapshot_repository.go:~296)". The join has no such predicate — status is expressed as an
// ORDER BY preference — and the `~` convention exempted the line number from the range check, so both
// existing guards were silent. A quoted predicate is a factual claim about a file's text, and it is
// the cheapest kind of claim there is to verify.
//
// ATTRIBUTION IS DELIBERATELY NARROW: a fragment is checked only when a .go path follows it
// IMMEDIATELY — nothing between them but whitespace, an opening paren and a backtick, i.e. the
// `predicate` (`file.go:NNN`) idiom and nothing else. Anything looser was measured and rejected:
// "nearest cited file" flagged 30 entries, and 28 of those were fragments quoted from the entry's
// own repository file while the nearby citation pointed at a SERVICE, for the caller's provenance
// rather than the predicate's home. Attributing to the entry's subject file instead was worse in the
// opposite direction — account_repository.go does contain `AND a.status = 'active'` (`:819`), so the
// very claim this guard exists to catch would have passed. The narrow rule speaks only when the
// prose itself says "this text is in that file".
func TestCitedSQLPredicatesExistInTheirFile(t *testing.T) {
	details := allDispositionDetails(t)

	haystacks := map[string]string{}
	haystackFor := func(path string) (string, bool) {
		if h, ok := haystacks[path]; ok {
			return h, h != ""
		}
		h := ""
		if resolved := resolveCitedFileForRange(path); resolved != "" && strings.HasSuffix(resolved, ".go") {
			if b, err := os.ReadFile(resolved); err == nil {
				h = flattenSQLForCompare(sqlHaystack(string(b)))
			}
		}
		haystacks[path] = h
		return h, h != ""
	}

	var missing []string
	checked := 0
	for key, detail := range details {
		goPaths := reAnyCitedPath.FindAllStringSubmatchIndex(detail, -1)
		// ownerFor returns the .go path cited IMMEDIATELY after `end`, or "" if the next thing in the
		// prose is anything other than the citation itself.
		ownerFor := func(end int) string {
			for _, m := range goPaths {
				if m[0] < end {
					continue
				}
				if !reImmediateCitation.MatchString(detail[end:m[0]]) {
					return ""
				}
				if !strings.HasSuffix(detail[m[2]:m[3]], ".go") {
					return ""
				}
				return detail[m[2]:m[3]]
			}
			return ""
		}

		for _, m := range reCitedSQLPredicate.FindAllStringSubmatchIndex(detail, -1) {
			frag := ""
			for _, g := range []int{2, 4, 6} {
				if m[g] >= 0 {
					frag = detail[m[g]:m[g+1]]
					break
				}
			}
			if frag == "" || reSQLNoise.MatchString(frag) {
				continue
			}
			owner := ownerFor(m[1])
			if owner == "" {
				continue
			}
			hay, ok := haystackFor(owner)
			if !ok {
				continue
			}
			checked++
			if !strings.Contains(hay, flattenSQLForCompare(frag)) {
				missing = append(missing, key+": "+strconv.Quote(strings.TrimSpace(frag))+
					" is attributed to "+owner+", which does not contain it (outside comments)")
			}
		}
	}

	if checked < 8 {
		t.Fatalf("only %d cited SQL predicates were checkable — these details quote dozens; the "+
			"extraction is broken and a pass would be meaningless", checked)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("%d cited SQL predicate(s) are not in the file they are attributed to:\n  - %s\n\n"+
			"A quoted predicate is a claim about a file's text. Re-read the file and rewrite the "+
			"claim; do not annotate it with a correction, which is how three of these got wrong.",
			len(missing), strings.Join(missing, "\n  - "))
	}
}
