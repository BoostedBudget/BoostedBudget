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
//     ⚠ #1569 T8 EXTENDED THE IN-RANGE CHECK BEYOND THIS FILE. TestCitedLineNumbersAreInRange
//     (internal/testsupport/citations) applies the same rule to every exact `file.go:NNN` citation
//     in a Go comment anywhere under backend/, plus SQL comments inside string literals. It found
//     zero out-of-range citations at the time it was written, so it is a ratchet rather than a
//     cleanup. It does NOT subsume this file's check, which also enforces path resolution and the
//     deferral rules within the dispositions' own convention.
//
//     ⚠ AND THE SEMANTIC HALF REMAINS OPEN, measured. Re-deriving every anchor in
//     cache_completeness_repo_test.go by construct — taking the cited file AS OF the commit that
//     last touched the citing line, reading the text at the cited line, and locating that text in
//     the current file — found 55 anchors whose line no longer holds that text. An in-range guard
//     cannot see any of this: every one of the 55 was in range. The durable fix is to cite the
//     METHOD, which TestMethodCitationsPointInsideTheirMethod already checks and which does not
//     drift.
//
//     ⚠ EIGHT REMAIN, AND THEY ARE ENUMERATED HERE — the first pass said "12 not re-derivable"
//     without listing them, which made the claim unauditable and, on review, wrong. What the
//     construct method cannot decide, INTENT sometimes can: eleven more were fixed by reading what
//     the prose says the anchor is FOR (`cmd/server/main.go:2191` said RegisterBudgetRoutes and pointed at
//     RegisterMerchantRoutes; `profile_repository.go:1225/1287` wanted the MarkForDeletion and
//     CancelDeletion func declarations, which are the most distinctive anchor text there is, and
//     were stale at birth). Four re-points from the first pass were text-faithful but landed in the
//     wrong construct and are corrected. These eight are what is left:
//
//       repo_test.go:197 → analytics_repository.go:568  (`AND ua.user_id = ANY($3)`, now nowhere)
//       repo_test.go:240 → account_service.go:576       (anchor was a blank line)
//       repo_test.go:240 → account_repository.go:244    (anchor was a blank line)
//       repo_test.go:297 → cmd/server/main.go:2080                 (anchor was a blank line)
//       repo_test.go:300 → cmd/server/main.go:2080                 (anchor was a blank line)
//       repo_test.go:330 → savings_goal_service.go:487  (anchor was a bare `)`)
//       repo_test.go:355 → user_account_service.go:262  (anchor was `return nil`)
//       repo_test.go:358 → backfill.go:143              (old text now appears twice)
//
//     A blank line and a bare `)` carry no identity, so there is nothing to locate; guessing would
//     produce a confident wrong number, which is worse than a stale one that reads as stale.
//
//     ⚠ AND THE STANDARD IS INTENT, NOT UNIQUENESS. A UNIQUE CANDIDATE IS NOT EVIDENCE OF INTENT.
//     Two anchors here were STALE AT BIRTH -- `profile_repository.go:1225`/`:1287` never pointed at
//     MarkForDeletion/CancelDeletion even on the day they were written -- and for a stale-at-birth
//     anchor the construct method is actively misleading: it faithfully tracks the text that WAS at
//     that line, finds exactly one landing spot for it, and hands you a confident number for a
//     citation whose author meant something else entirely. Those two were fixed from the PROSE
//     (it names the methods), not from the text. `analytics_repository.go:825` is the same shape
//     and was NOT fixed: a plausible unique candidate exists 56 lines away, and nothing identifies
//     what the citation meant, so it is left visibly stale rather than confidently wrong.
//
//     ⚠ AND THE SAME LINE NUMBER SHOWS THE STANDARD CUTTING BOTH WAYS, which is why it is worth
//     spelling out. There were TWO citations of `analytics_repository.go:825`, in different files,
//     and they got opposite treatments -- correctly:
//       - the one described above has no prose naming a target, so intent is unrecoverable and it
//         stays stale;
//       - `custom_category_tombstone_integration_test.go`'s `:717`/`:732` DID name their target in
//         prose ("buildSpendingFilter") while pointing at `householdGrantsCTE`, so intent was
//         recoverable and #1585 re-pointed them to `:907`/`:922` FROM THE PROSE.
//     So "was NOT fixed" above is a statement about THAT citation, not about the line number. The
//     rule is not "leave 717 alone"; it is "fix from intent where the prose supplies it, and leave
//     it visibly stale where nothing does".
//
//     So an IN-RANGE check is enforced here too (TestExactCitationsAreInRange below) for exact
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
	"go/scanner"
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
//
// ⚠ READ THIS BEFORE TRUSTING A GREEN RUN: this test proves a cited line is IN RANGE, never that it
// is CORRECT — an in-range number pointing at wholly unrelated code passes it silently, which is how
// roughly twenty stale citations survived an entire audit of this branch (five in
// budget_repository.go, three in transaction_repository.go, and one paragraph copy-pasted to eight
// sites). TestCitedSQLFragmentsAreNearTheirCitedLine below closes that hole for SQL-bearing
// citations only; every other class of citation still has nothing but this range bound.
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

	var stale, ambiguous []string
	rangeChecked := 0
	for r, count := range seen {
		// SHARED RESOLVER, not a local re-implementation (#1537 T96). This loop used to inline a
		// citationSearchRoots-only lookup, which cannot resolve the bare-basename shorthand these
		// details use everywhere — so 31 of ~199 distinct exact refs fell through `continue` and got
		// NO range check from ANY test in this file. One of them — the SetBalanceSnapshotRepo ref in
		// repoMethodDispositions — named a bare main.go basename and pointed four lines short of the
		// call; it is now written with a qualified path. ⚠ De-numbered deliberately: this file is in
		// the guards' own scan list, so a file.go:NNN example written into a comment here becomes a
		// finding about itself. Write examples as prose, never as a live-looking citation.
		// resolveCitedFileForRange does resolve unambiguous
		// basenames, and the sibling guards have used it all along; only this one had its own copy.
		resolved, candidates := resolveCitedFileForRangeWithCount(r.path)
		if resolved == "" {
			// AMBIGUITY IS REPORTED, NOT SKIPPED. A basename with several candidates (main.go has
			// four) resolved to "" and was silently dropped, so a `dashboard.go` ref with a five-digit line passed every guard
			// in this file. It cannot be range-checked against an arbitrary candidate — but that is a
			// defect in the CITATION, which should name enough path to be resolvable, so it is
			// surfaced rather than swallowed. The sibling guards' rule throughout this file is fall
			// back or account, never skip.
			if candidates > 1 {
				ambiguous = append(ambiguous, r.path+":"+strconv.Itoa(r.line)+" (cited "+
					strconv.Itoa(count)+"x) — basename matches "+strconv.Itoa(candidates)+
					" files, so no guard can range-check it")
			}
			// Zero candidates is TestDispositionCitationsResolve's business, and it fails there.
			continue
		}
		b, err := os.ReadFile(resolved)
		if err != nil {
			continue
		}
		rangeChecked++
		n := len(strings.Split(string(b), "\n"))
		switch {
		case r.line < 1:
			// LOWER BOUND. There was none: only `r.line > n` was checked, so
			// a `WHERE id = $1` fragment cited at transaction_repository.go line 0 — a plainly wrong
			// citation — passed this guard AND the fragment guard (whose linesAround returned a silent
			// skip for line < 1). A demonstrated fail-open, closed on both sides.
			stale = append(stale, r.path+":"+strconv.Itoa(r.line)+" (cited "+strconv.Itoa(count)+
				"x) — line numbers are 1-based; this names no line at all")
		case r.line > n:
			stale = append(stale, r.path+":"+strconv.Itoa(r.line)+" (cited "+strconv.Itoa(count)+
				"x) — file has only "+strconv.Itoa(n)+" lines")
		}
	}

	t.Logf("range-check coverage: %d distinct exact refs extracted, %d resolved and range-checked, "+
		"%d unresolvable-because-ambiguous", len(seen), rangeChecked, len(ambiguous))

	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("%d citation(s) name a line the file does not have. A line number that cannot "+
			"be opened is worse than none: it reads as evidence. Re-anchor it, write it as ~:NNN if "+
			"it is approximate, or drop the number if a guard already proves the point:\n  - %s",
			len(stale), strings.Join(stale, "\n  - "))
	}
	if len(ambiguous) > 0 {
		sort.Strings(ambiguous)
		t.Errorf("%d citation(s) name a basename too ambiguous to range-check, so they are checked by "+
			"nothing. Qualify the path (add enough directory to make it unique) so a guard can read "+
			"it:\n  - %s", len(ambiguous), strings.Join(ambiguous, "\n  - "))
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
// function rather than at its declaration — `budget_service.go:251` is the
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
// methodCitationPair is one extracted `Method` <-> `file.go:NNN` (or `file.go:NNN-MMM`) pairing.
// hi == lo for a single-line citation.
type methodCitationPair struct {
	method, path string
	lo, hi       int
}

// extractMethodCitationPairs pulls every method/line pairing out of one guard file's raw text.
//
// BOTH ORDERS. The original regex required method-then-file, and the entire "#1537 cluster A"
// family — every entry that traces a caller chain — writes file-then-method:
//
//	`services/account_service.go:554` (`LinkAccounts`)
//
// so 10 of those pairs were invisible. Measured after accepting both orders: the newly-visible
// pairs had drifted 14-75 lines. A guard whose coverage depends on which side of the parenthesis
// the reader put the method name is not measuring the prose, it is measuring the punctuation.
//
// RANGES TOO (#1569 round 6). Both regexes used to end at `:(\d+)` followed by a REQUIRED closing
// backtick, so a range citation — the form written as file dot go colon 1766 dash 1802 — could not
// match either of them and the pair was never collected at all. That was not a small gap: the
// corpus held 64 range citations, and collecting them took the guard's checkable count from 33 to
// 57. The entry in
// cache_completeness_repo_test.go that reports this hole found it the hard way — a range citation
// naming GetBudgetVsActual while spanning an unrelated row-scan loop sailed past the guard that
// caught the two BARE citations of the same method in the same sentence. Coverage that depends on
// whether the author wrote one line number or two is punctuation-measuring again.
func extractMethodCitationPairs(text string) []methodCitationPair {
	goPath := "((?:[a-z0-9_]+/)*[a-z0-9_]+\\.go)"
	ident := "[A-Za-z_][A-Za-z0-9_]*"
	patterns := []*regexp.Regexp{
		// `Type.Method` (`file.go:NNN`) / (`file.go:NNN-MMM`)
		regexp.MustCompile("`(?:" + ident + "\\.)?(" + ident + ")`[,;]?\\s*\\(?`" + goPath + ":(\\d+)(?:-(\\d+))?`"),
		// `file.go:NNN` (`Method`) / `file.go:NNN-MMM` (`Type.Method`)
		regexp.MustCompile("`" + goPath + ":(\\d+)(?:-(\\d+))?`[,;]?\\s*\\(`(?:" + ident + "\\.)?(" + ident + ")`"),
	}
	// group[i] tells which capture group holds each field for patterns[i].
	methodGroup := []int{1, 4}
	pathGroup := []int{2, 1}
	loGroup := []int{3, 2}
	hiGroup := []int{4, 3}

	var pairs []methodCitationPair
	for i, re := range patterns {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			lo, err := strconv.Atoi(m[loGroup[i]])
			if err != nil {
				continue
			}
			hi := lo
			if m[hiGroup[i]] != "" {
				if h, e := strconv.Atoi(m[hiGroup[i]]); e == nil {
					hi = h
				}
			}
			if hi < lo {
				lo, hi = hi, lo // a reversed range is still a claim about that interval
			}
			pairs = append(pairs, methodCitationPair{m[methodGroup[i]], m[pathGroup[i]], lo, hi})
		}
	}
	return pairs
}

// An alias table used to live here (#1569 R6B/B1): it let a citation name the Scope.Kind a
// `cache.ScopeFor` call RESOLVES TO ("ScopeUser"/"ScopeHousehold") rather than the constructor
// written at the cited line, on the theory that "a ScopeFor call is nearby" plus an argument-aware
// regex could tell the two kinds apart. It was REMOVED in #1569 R6C: the review proved by execution
// that `ScopeFor`'s second argument is a nilable `*domain.HouseholdScope`, so no regex over source
// text can decide which kind a given call resolves to — a gofmt-wrapped nil, a named nil variable,
// and a nested first-position call each defeated a different version of the attempted parser, and
// one of those false negatives left a genuine wrong-kind citation accepted. A guard that can be
// argued past by reformatting the call it inspects is not a guard. The fix moved to the citations
// themselves: the four real call sites now name the constructor, `ScopeFor`, directly, so the
// existing identifierNearRange fallback below — a plain substring search, no argument parsing —
// checks the LINE with no inference required. See
// internal/repository/cache_completeness_repo_test.go's BudgetRepository.Update/.CreateCategory/
// .UpdateCategorySpentAmount/.CreateWithCategories entries for the corrected citations.

// methodCitationProblem is the guard's whole relation for ONE pair, given a resolved on-disk path.
// It returns ("", false) when the pair is not checkable at all, and otherwise (problem, true) with
// problem empty when the citation is sound.
//
// WHY OVERLAP AND NOT CONTAINMENT-OF-BOTH-ENDS. A range citation names a REGION of interest — the
// method's body, its query literal, the paragraph the reader should look at. Requiring both ends
// inside the function would fire on the very common and correct habit of starting a range at the
// doc comment or ending it one line past the closing brace, which is drift-free noise. Requiring
// neither end would check nothing. Interval overlap is the relation that says what the guard means:
// the cited region and the method must share at least one line, or the citation is attributing a
// region to a function that region does not touch.
//
// WHY CONTAINMENT AND NOT PROXIMITY, for the single-line case. Proximity ("the identifier must
// appear within +/-8 lines") was measured before adoption: 10 of 17 pairs would have failed, and
// every one was a FALSE positive, because these citations deliberately point at a CALL SITE inside
// the named function rather than at its declaration. The citations were better than the checker.
func methodCitationProblem(resolved, method string, lo, hi int) (string, bool) {
	fset := token.NewFileSet()
	file, parseErr := parser.ParseFile(fset, resolved, nil, 0)
	if parseErr != nil {
		return "", false // not our business to police unparseable files
	}
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != method || fn.Body == nil {
			continue
		}
		fnLo, fnHi := fset.Position(fn.Pos()).Line, fset.Position(fn.End()).Line
		if hi < fnLo || lo > fnHi {
			return citedSpan(lo, hi) + " is cited for " + method + ", but " + method +
				"'s body spans " + strconv.Itoa(fnLo) + "-" + strconv.Itoa(fnHi) +
				" — the cited lines do not overlap the function they are attributed to", true
		}
		return "", true
	}

	// A SILENT SKIP here was the guard's second blind spot: when the named method was absent from
	// the cited file, the loop simply fell through, so "`Foo`, see `bar.go:12`" passed whether or
	// not bar.go contained any trace of Foo.
	//
	// It is NOT, however, an error outright, and the briefing that asked for that was wrong:
	// measured over all pairs, absence fires on the most common legitimate idiom in this file —
	// citing a method at its CALL SITE in another file (`budgetService.UpdateCategory`
	// (`budget_service.go:721`) is `s.budgetRepo.UpdateCategory(...)`, and budget_service.go
	// naturally declares no such function). Five of the nine absences were of that kind.
	//
	// So absence falls back to the weaker relation that IS available: the identifier must appear
	// within a few lines of the cited region. Proximity was rejected as the PRIMARY relation for
	// good reason (see above — it punishes call-site citations by demanding the declaration), but as
	// a fallback where containment is impossible it is exactly right, and it caught four stale refs
	// that the silent skip had been swallowing.
	if identifierNearRange(resolved, method, lo, hi, 3) {
		return "", true
	}
	return citedSpan(lo, hi) + " is cited for " + method +
		", which the file neither declares nor mentions within 3 lines of that ref — " +
		"the citation names nothing a reader can find", true
}

func citedSpan(lo, hi int) string {
	if hi == lo {
		return strconv.Itoa(lo)
	}
	return strconv.Itoa(lo) + "-" + strconv.Itoa(hi)
}

// TestMethodCitationsPointInsideTheirMethod applies methodCitationProblem to the real corpus.
//
// Coverage is modest by construction: it needs `Type.Method` adjacent to the citation. It costs
// nothing and grows as more citations adopt that form. Line refs with no method name (a bare SQL
// predicate, say) are not covered here — TestExactCitationsAreInRange bounds those, and one
// off-by-one predicate ref that survived both checks was found by hand, so neither guard should be
// read as making prose trustworthy.
func TestMethodCitationsPointInsideTheirMethod(t *testing.T) {
	var pairs []methodCitationPair
	for _, g := range citedGuardFiles() {
		b, err := os.ReadFile(g.path)
		if err != nil {
			t.Fatalf("read %s: %v", g.path, err)
		}
		pairs = append(pairs, extractMethodCitationPairs(string(b))...)
	}

	var violations []string
	checked := 0
	for _, p := range pairs {
		resolved := resolveCitedFileForRange(p.path)
		if resolved == "" || !strings.HasSuffix(resolved, ".go") {
			continue // path shorthand is TestDispositionCitationsResolve's business
		}
		problem, ok := methodCitationProblem(resolved, p.method, p.lo, p.hi)
		if !ok {
			continue
		}
		checked++
		if problem != "" {
			violations = append(violations, p.path+":"+problem)
		}
	}

	// FLOOR RATIONALE (#1537 round 10). Floors were 15/20/8 against measured counts of 34/117/20 —
	// the bare-ref floor was 17% of reality, so the scanner could have silently lost four fifths of
	// its coverage and still reported success. That is the same fail-open shape these guards exist
	// to catch. Now ~75% of measured, which trips on a scanner regression while leaving room to
	// legitimately remove citations. Counts were obtained by raising each floor until the Fatalf
	// printed the real number; re-measure that way rather than trusting these comments.
	//
	// ⚠ RAISED FROM 25 TO 43 WITH THE RANGE WIDENING (#1569 round 6), measured the same way — by
	// raising the floor until the Fatalf printed the real number, both before and after. Ranges took
	// the checkable count from 33 to 57. The old floor of 25 was 76% of 33 and would have been
	// cleared by 33, i.e. by the range half of the extraction dying completely; 43 is ~75% of 57 and
	// is not. A floor that a REGRESSION still clears is not a floor.
	if checked < 43 {
		t.Fatalf("only %d method citations were checkable — the extraction is broken, "+
			"and a pass would be meaningless", checked)
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Errorf("%d method citation(s) point outside the method they name:\n  - %s",
			len(violations), strings.Join(violations, "\n  - "))
	}
}

// readSourceWithCommentsBlanked reads a .go file and returns its bytes with every comment's text
// overwritten with spaces (newlines inside block comments are preserved so line numbers in the
// result still line up with the original file). Falls back to the raw bytes when the file has no
// tokens at all — an empty file, or one containing only whitespace.
//
// ⚠ CORRECTED #1569 R6C. The doc comment here used to say this falls back "if the file cannot be
// tokenized (e.g. a syntax error mid-edit)". That is not what `go/scanner` does: it is a LEXER, not
// a parser, and it has no notion of "cannot be tokenized" — run on badly-broken source (an unclosed
// brace, a dangling operator, whatever mid-edit produces) it still emits a token stream, just one an
// `ast.Parse` would reject. `!sawToken` cannot fire on that kind of brokenness; the only way to reach
// it is a file with literally zero tokens. Even a comment-only file still emits COMMENT tokens under
// ScanComments, so in practice `!sawToken` fires only on a genuinely empty or whitespace-only file.
//
// WHY THIS EXISTS (#1569 R6B/B2). identifierNearRange is the fallback relation for a citation whose
// named method the file does not DECLARE — it used to run over raw source, so a citation for a
// method the file merely MENTIONS IN A COMMENT (e.g. a doc comment cross-referencing another type's
// method by name) satisfied the same check as a real call site. Blanking comments first means the
// fallback can only be satisfied by identifiers that appear in actual code.
//
// ⚠ CORRECTED #1569 R6C — that last sentence overclaims. "Actual code" here does NOT exclude string
// literals: this guard's own corpus cites SQL table/column names that live nowhere but inside raw
// string literals (e.g. `subscription_entitlements` at entitlement_repository.go:114, inside a
// backtick SQL string, is a real, load-bearing citation with no non-string occurrence anywhere in
// the file). Blanking `token.STRING` alongside comments was tried and reverted for exactly that
// reason — it turned that citation and others like it into false positives on the real corpus, which
// is worse than the hole it would have closed. So the fallback is satisfied by any identifier
// occurrence outside a COMMENT specifically — including one that appears only inside a string
// literal, such as `s := "CommentOnlyThing"`. That is narrower than "actual code" suggests but wider
// than "declared or called"; a reader relying on this fallback for precision should know a string
// literal counts.
func readSourceWithCommentsBlanked(path string) ([]byte, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	file := fset.AddFile(path, fset.Base(), len(src))
	var sc scanner.Scanner
	sc.Init(file, src, nil, scanner.ScanComments)
	out := append([]byte(nil), src...)
	sawToken := false
	for {
		pos, tok, lit := sc.Scan()
		if tok == token.EOF {
			break
		}
		sawToken = true
		if tok != token.COMMENT {
			continue
		}
		start := file.Offset(pos)
		for i := 0; i < len(lit) && start+i < len(out); i++ {
			if out[start+i] != '\n' {
				out[start+i] = ' '
			}
		}
	}
	if !sawToken {
		return src, nil // genuinely empty/whitespace-only file: nothing to blank
	}
	return out, nil
}

// identifierNearRange reports whether ident appears within window lines either side of the cited
// interval [lo, hi] in path, with comments blanked out first — a citation is checking for CODE, and
// a comment merely mentioning the identifier's name is not evidence the file contains it. Used only
// as the fallback relation for a cited name the file does not declare (a call-site or table-name
// citation).
func identifierNearRange(path, ident string, lo, hi, window int) bool {
	b, err := readSourceWithCommentsBlanked(path)
	if err != nil {
		return true // unreadable file is TestDispositionCitationsResolve's business, not ours
	}
	lines := strings.Split(string(b), "\n")
	lo, hi = lo-1-window, hi-1+window
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
var reBareLineRef = regexp.MustCompile("`:(\\d+(?:\\s*[-,]\\s*\\d+)*)`")

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
		for _, part := range strings.FieldsFunc(e.nums, func(r rune) bool { return r == '-' || r == ',' || r == ' ' || r == '\t' }) {
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
// and there are 117 of them (measured by raising this test's own floor until it printed the count —
// an earlier version of this comment said 63, which was a figure from a partial pass and was never
// re-derived). FIVE were out of range, not the two I predicted; one was a number the citations
// preamble specifically boasts of having fixed: `:1212` against budget_repository.go, still there,
// just written bare. A guard whose coverage depends on the citation's punctuation is not a guard.
//
// ⚠ Both numbers above were wrong in this comment until #1537 round 10, when review caught that the
// prose said 63 while the test's own Fatalf says 117. That is this file's subject matter appearing in
// this file's own documentation — recorded rather than quietly corrected.
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
				// #1537 round 17: `checked++` used to happen HERE, before the rejections below, so
				// the floor counted ATTEMPTS rather than valid refs — ninety `:0` refs would have
				// satisfied a floor of 90 while every single one was rejected. It now increments
				// only for a ref that was actually range-checked.
				if ln < 1 {
					// #1537 round 16: line 0 was rejected in the exact and SQL guards and MISSED
					// here — the third guard in this file. Found by adversarial review after the
					// other two were fixed: the half-swept class this branch keeps hitting.
					stale = append(stale, key+": bare `:"+strconv.Itoa(ln)+"` is not a line number "+
						"(files are 1-based), so nothing can check it against "+path)
					continue
				}
				checked++
				if ln > total {
					stale = append(stale, key+": bare `:"+strconv.Itoa(ln)+"` resolves against "+path+
						", which has only "+strconv.Itoa(total)+" lines")
				}
			}
		}
	}

	if checked < 90 {
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
	resolved, _ := resolveCitedFileForRangeWithCount(cited)
	return resolved
}

// resolveCitedFileForRangeWithCount is resolveCitedFileForRange plus the number of basename
// candidates it found, so a caller can tell "this citation names nothing" (0) apart from "this
// citation names four different files" (>1). The distinction matters because the second is a
// reportable defect in the citation and the first is already reported elsewhere; collapsing both to
// "" is what let an ambiguous ref be range-checked by nothing at all.
func resolveCitedFileForRangeWithCount(cited string) (string, int) {
	for _, root := range citationSearchRoots {
		c := filepath.Join(root, cited)
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, 1
		}
	}
	if strings.ContainsRune(cited, '/') {
		return "", 0
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
		return hits[0], 1
	}
	return "", len(hits)
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

	if checked < 15 {
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

// ── Cited SQL fragments must be AT the line they cite ───────────────────────────────────────────

// reBacktickedFragment matches one backticked span. Spans in these details are well-formed pairs,
// so a non-overlapping scan yields each span exactly once.
var reBacktickedFragment = regexp.MustCompile("`([^`]{2,200})`")

// reSQLSyntaxFragment decides whether a backticked span is a quotation of SQL rather than prose,
// an identifier or a route. It is the whole reason this guard is usable: see the doc comment on
// TestCitedSQLFragmentsAreNearTheirCitedLine for the measurement that rejected the loose version.
var reSQLSyntaxFragment = regexp.MustCompile(
	`SELECT|LEFT JOIN|INNER JOIN|JOIN|WHERE|ORDER BY|DISTINCT ON|GROUP BY|ON CONFLICT|` +
		`INSERT INTO|UPDATE |DELETE FROM|DESC NULLS|AND [a-z_][a-z0-9_]*\.`)

// reCitationAfterFragment is the citation permitted to follow a quoted fragment. Between fragment
// and citation it allows whitespace, an opening paren/bracket, and — this is the part that was
// missing — ORDINARY SENTENCE PUNCTUATION: a comma, a semicolon, an em- or en-dash. It then accepts
// either `file.go:NNN…` or a bare `:NNN…`, with an optional `~` in any of the three places these
// details write one (`~file.go:296`, `file.go~:296`, `file.go:~296`, and the bare `~:296`).
//
// ⚠ THE OTHER LINE-CITATION GUARD USED TO DISAGREE WITH THIS GRAMMAR. testsupport/citations'
// citedFileLine accepted `file.go:~296` only: `~file.go:296` was read as an EXACT citation (a
// false positive waiting to happen) and `file.go~:296` matched nothing at all. Reconciled in the
// #1569 whole-branch review — that guard now treats all three tilde positions as approximate. The
// bare forms stay this guard's business alone, since they name no file to resolve.
//
// WHY PUNCTUATION IS ALLOWED (#1537 T96). The original version permitted only whitespace and
// `(`/`[`, which made the guard's coverage a function of how the author punctuated the sentence.
// Measured: `WHERE user_id = $3 AND is_primary = true`, `user_identity_repository.go:328-334` —
// separated by a single COMMA — was never checked, and it was hiding a 41-line drift (the quoted
// text lives at :369-370; :330 is the VALUES list of an unrelated INSERT). This file already
// records the lesson in TestBareLineRefsAreInRange's own doc comment: "A guard whose coverage
// depends on the citation's punctuation is not a guard." It applied here too.
//
// A RANGE OR LIST (`:1470-1471`, `:115,233,689`) is accepted and EVERY value in it is checked, not
// just the first. Measured: one entry cites `:115,233,689,810,935,1103,1315,1423,1469,1519` for ten
// personal-scope `LEFT JOIN accounts` sites; the real lines are 113,228,681,800,922,1087,1291,1395,
// 1439,1488, so all ten are wrong — yet the first-value-only version passed, because 115 happens to
// be within +/-4 of 113. Of the discarded tail, :935 is an error return, :1103 is blank, and :1315
// and :1519 are closing braces. Checking one value of a ten-value citation is not checking it.
var reCitationAfterFragment = regexp.MustCompile(
	"^\\s*(?:[,;]|—|–)?\\s*[(\\[]?\\s*`(~?)((?:[a-z0-9_.-]+/)*[a-z0-9_.-]+\\.go)?(~?):(~?)(\\d+(?:\\s*[-,]\\s*\\d+)*)`")

// Capture-group indices into reCitationAfterFragment. Named because the tilde may appear in three
// places and an off-by-one here silently turns an approximate ref into an exact one.
const (
	citGroupTildeBeforePath = 1
	citGroupPath            = 2
	citGroupTildeAfterPath  = 3
	citGroupTildeAfterColon = 4
	citGroupLines           = 5
)

// Window half-widths. `~` means "about here", so it WIDENS the window; it does not exempt the ref.
//
// WHY WIDEN RATHER THAN EXEMPT (#1537 T96). Neither `file.go:~296` nor `~:296` matched the adjacency
// regex at all, so every approximate ref was unguarded — and the citation whose rot motivated this
// entire guard was written `:~296`. Under an exemption rule, "add a tilde" is the cheapest way to
// turn a failing citation green, which is a guard that pays authors to disable it. Under a widening
// rule the tilde costs its author nothing when it is honest and still catches the drifts that
// actually occurred here, which were 5 to 56 lines out — well outside even the wide window.
const (
	sqlFragmentWindowExact  = 4
	sqlFragmentWindowApprox = 10
)

// lineWindowSkip marks a linesAround failure that belongs to ANOTHER guard (an unresolvable path is
// TestDispositionCitationsResolve's, a line past EOF is TestExactCitationsAreInRange's /
// TestBareLineRefsAreInRange's). Any other non-empty problem string is a condition the CALLER must
// report: silently skipping it is how a citation to line 0 of transaction_repository.go passed
// every guard in this file.
const lineWindowSkip = "skip"

// linesAround returns the flattened text of path's lines in [line-window, line+window], 1-based,
// with comments masked. The second return is "" on success, lineWindowSkip when the text is
// unavailable for a reason another guard owns, and otherwise a human-readable problem the caller is
// required to REPORT.
//
// An impossible line number (< 1) is that reportable case, and it was a demonstrated fail-open:
// a `WHERE id = $1` fragment cited at transaction_repository.go line 0 is a plainly wrong
// citation, and it passed both
// this guard (line < 1 returned "skip") and TestExactCitationsAreInRange (which bounded only
// line > len(file)). Neither guard had a lower bound at all.
func linesAround(path string, line, window int) (string, string) {
	resolved := resolveCitedFileForRange(path)
	if resolved == "" || !strings.HasSuffix(resolved, ".go") {
		return "", lineWindowSkip
	}
	b, err := os.ReadFile(resolved)
	if err != nil {
		return "", lineWindowSkip
	}
	if line < 1 {
		return "", "line " + strconv.Itoa(line) + " is not a line number (files are 1-based)"
	}
	lines := strings.Split(sqlHaystack(string(b)), "\n")
	if line > len(lines) {
		// #1537 round 17: this used to delegate ("past EOF is the in-range guards' business") and
		// that delegation is UNSOUND for a multi-value citation. The in-range guard extracts refs
		// with reAnyCitedPath, whose line group is `(?::(\d+))?` — ONE value, no tail. So a
		// past-EOF value in `foo.go:422, 99999` was seen by nobody: this guard skipped it as
		// somebody else's job, and the somebody else could not parse it. Report it here, where the
		// value has already been parsed.
		return "", "line " + strconv.Itoa(line) + " is past the end of " + resolved + " (" +
			strconv.Itoa(len(lines)) + " lines)"
	}
	lo, hi := line-1-window, line-1+window
	if lo < 0 {
		lo = 0
	}
	if hi >= len(lines) {
		hi = len(lines) - 1
	}
	return flattenSQLForCompare(strings.Join(lines[lo:hi+1], "\n")), ""
}

// citationInstanceCount counts every file:line and bare `:NNN` citation instance in the details,
// expanding multi-value refs. It is the DENOMINATOR of this guard's self-report: without it, a
// coverage claim ("this checks 15 citations") is a numerator with nothing to compare against, and
// every such claim made about this guard from the outside — including by its author — has been wrong.
func citationInstanceCount(details map[string]string) int {
	total := 0
	countValues := func(s string) int {
		n := 0
		for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == ',' || r == ' ' || r == '\t' }) {
			if _, err := strconv.Atoi(part); err == nil {
				n++
			}
		}
		return n
	}
	rePathWithLines := regexp.MustCompile(
		`\b(?:[a-z0-9_.-]+/)*[a-z0-9_.-]+\.(?:go|md|sql|yaml|yml|json|ts|tsx):(\d+(?:\s*[-,]\s*\d+)*)`)
	for _, detail := range details {
		for _, m := range rePathWithLines.FindAllStringSubmatch(detail, -1) {
			total += countValues(m[1])
		}
		for _, m := range reBareLineRef.FindAllStringSubmatch(detail, -1) {
			total += countValues(m[1])
		}
	}
	return total
}

// TestCitedSQLFragmentsAreNearTheirCitedLine closes the hole every other guard in this file leaves
// open: TestExactCitationsAreInRange proves a cited line is IN RANGE, never that it is CORRECT, and
// TestCitedSQLPredicatesExistInTheirFile proves a quoted fragment is SOMEWHERE in the cited file.
// Neither notices a citation that is in-range, in the right file, and pointing at unrelated code —
// which is exactly what ~20 stale citations looked like while a whole audit passed over them. This
// guard closes that hole only for SQL-BEARING citations; every other class of citation is still
// range-checked and nothing more.
//
// The rule: a backticked SQL fragment followed by `file.go:NNN` (or a bare `:NNN`, resolved against
// the last .go file the entry named) claims that text is AT that line, so the fragment must appear
// within the window of EVERY line value the citation lists.
//
// THE LOOSE VERSION WAS MEASURED AND REJECTED. Checking every backticked span adjacent to a citation
// yields 160 candidates and 121 "findings", almost all of them the scanner capturing the PROSE
// between two code spans. Requiring SQL syntax narrows it to a handful of real ones.
//
// ⚠ THAT 121 WAS MEASURED ON THE WRONG BASIS, and a `reFragmentIsProse` filter was added on the
// strength of it. The 121 came from scanning RAW SOURCE LINES, where a `+`-concatenated detail is
// cut mid-span and backtick parity is broken, so the scanner really was capturing prose. The guard
// that shipped scans FLATTENED details, where parity is balanced. Re-measured on the basis the guard
// actually uses (#1537 T96): 936 backticked spans, 42 pass the SQL-syntax filter, 23 have an
// accepted adjacent citation, and ZERO of either set is captured prose. Meanwhile the filter — which
// rejected any fragment containing ", " — was excluding real, comma-rich SQL, including
// `(a.status = 'active') DESC NULLS LAST, a.id`: the citation that has rotted twice and that
// reCitedSQLPredicate grew a third shape specifically to cover. It prevented nothing and hid the one
// fragment most in need of watching, so it is gone. Measure on the basis the code uses, not the
// basis the earlier draft used.
//
// SELF-MEASURING BY CONSTRUCTION. The funnel below is logged on every run and the floor is asserted
// against DISTINCT anchors, not raw hits. Both are responses to measured fail-opens: the old floor
// was `checked < 10` against 15 hits that were only 13 distinct anchors (one paragraph is
// copy-pasted three times), so rewriting five citations from `frag` (`file.go:N`) to
// `frag`, `file.go:N` would have halved real coverage and stayed green. A guard nobody can
// mis-describe is one that prints its own coverage.
func TestCitedSQLFragmentsAreNearTheirCitedLine(t *testing.T) {
	details := allDispositionDetails(t)

	type anchor struct {
		frag, path string
		line       int
	}
	distinct := map[anchor]bool{}

	var stale []string
	spans, sqlShaped, withCitation, checked := 0, 0, 0, 0
	for key, detail := range details {
		goPaths := reAnyCitedPath.FindAllStringSubmatchIndex(detail, -1)
		// lastGoPathBefore resolves a bare `:NNN` the way a reader does: against the most recent .go
		// file the entry named.
		lastGoPathBefore := func(at int) string {
			out := ""
			for _, m := range goPaths {
				if m[0] >= at {
					break
				}
				if p := detail[m[2]:m[3]]; strings.HasSuffix(p, ".go") {
					out = p
				}
			}
			return out
		}

		for _, m := range reBacktickedFragment.FindAllStringSubmatchIndex(detail, -1) {
			spans++
			frag := detail[m[2]:m[3]]
			if !reSQLSyntaxFragment.MatchString(frag) || reSQLNoise.MatchString(frag) {
				continue
			}
			sqlShaped++
			cm := reCitationAfterFragment.FindStringSubmatchIndex(detail[m[1]:])
			if cm == nil {
				continue
			}
			group := func(i int) string {
				if cm[2*i] < 0 {
					return ""
				}
				return detail[m[1]+cm[2*i] : m[1]+cm[2*i+1]]
			}
			path := group(citGroupPath)
			if path == "" {
				path = lastGoPathBefore(m[0])
			}
			if path == "" {
				continue
			}
			withCitation++
			window := sqlFragmentWindowExact
			if group(citGroupTildeBeforePath) != "" || group(citGroupTildeAfterPath) != "" ||
				group(citGroupTildeAfterColon) != "" {
				window = sqlFragmentWindowApprox
			}
			// EVERY value of a multi-value citation, not just the first.
			for _, part := range strings.FieldsFunc(group(citGroupLines), func(r rune) bool {
				// Space and tab included (#1537 round 17). reCitationAfterFragment was widened in
				// round 16 to ACCEPT `foo.go:10, 99999` but this splitter was left splitting on
				// `-` and `,` only, so the second part arrived as " 99999", strconv.Atoi errored,
				// and the `continue` below DROPPED it silently. That turned an honest skip (the
				// pre-round-16 behaviour, where the citation matched nothing) into a FALSE PASS.
				// Half-widening a grammar without widening its consumer is worse than neither.
				return r == '-' || r == ',' || r == ' ' || r == '\t'
			}) {
				line, err := strconv.Atoi(part)
				if err != nil {
					continue
				}
				distinct[anchor{flattenSQLForCompare(frag), path, line}] = true
				text, problem := linesAround(path, line, window)
				if problem == lineWindowSkip {
					continue
				}
				checked++
				if problem != "" {
					stale = append(stale, key+": "+strconv.Quote(strings.TrimSpace(frag))+" is cited at "+
						path+":"+part+", and "+problem)
					continue
				}
				if !strings.Contains(text, flattenSQLForCompare(frag)) {
					stale = append(stale, key+": "+strconv.Quote(strings.TrimSpace(frag))+" is cited at "+
						path+":"+strconv.Itoa(line)+", but that text is not within +/-"+
						strconv.Itoa(window)+" lines of it")
				}
			}
		}
	}

	// The funnel, printed every run. Read it before believing any prose about this guard's reach.
	t.Logf("coverage funnel: %d backticked spans scanned -> %d SQL-shaped -> %d with an accepted "+
		"adjacent citation -> %d line values checked (%d distinct fragment/file/line anchors), out of "+
		"%d line-citation instances present in the dispositions",
		spans, sqlShaped, withCitation, checked, len(distinct), citationInstanceCount(details))

	// FLOOR RATIONALE. Asserted on DISTINCT anchors, because raw hits are inflated by copy-pasted
	// paragraphs — 15 hits were 13 anchors — so a hit-count floor can be satisfied while real
	// coverage halves. Set near 75% of the measured count, the same ratio and for the same reason as
	// TestMethodCitationsPointInsideTheirMethod's floor (25 against a measured 34). Measured here:
	// 18 distinct anchors, so the floor is 14 (the number was 19 before the last conversion; the
	// funnel line above is authoritative and this comment is not). It trips on a scanner regression while leaving room to
	// legitimately delete citations. Re-measure by reading the funnel line above, not by trusting
	// this comment.
	//
	// ⚠ This floor was 22, calibrated against 30 anchors, and was LOWERED deliberately — the only
	// legitimate reason to lower one. #1537 T95 converted 12 of those 30 anchors to identifier-based
	// references (the ten-value analytics list, whose ten line numbers were all wrong, and the
	// thrice-rotted balance_snapshot pair), so the corpus genuinely shrank rather than the extraction
	// regressing. A floor cannot be derived from the current measurement — that would be circular,
	// since its whole job is to notice when the measurement drops — so it is hard-coded and MUST be
	// re-tuned by hand whenever citations are deliberately removed. If you are lowering it for any
	// other reason, you are hiding a regression.
	if len(distinct) < 14 {
		t.Fatalf("only %d distinct SQL fragment/file/line anchors were checkable (funnel above); the "+
			"extraction has regressed and a pass here would be meaningless", len(distinct))
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("%d cited SQL fragment(s) are in the right file but not at the cited line:\n  - %s\n\n"+
			"An in-range line number pointing at unrelated code reads as evidence and is not. "+
			"Re-anchor the number to the line that actually holds the quoted text.",
			len(stale), strings.Join(stale, "\n  - "))
	}
}

// TestMethodCitationRelation_RangesAndAliases is the widened guard's own falsification.
//
// A guard that has never been shown to fail is a comment. This branch shipped six of those, so the
// range widening (#1569 round 6) is pinned from BOTH sides here: every case below is a citation the
// guard must reject, or one it must accept, constructed against a real parsed Go file rather than
// asserted about in prose. The corpus half — that no REAL range citation is a false positive — is
// TestMethodCitationRangesInTheCorpusAreClean below; a synthetic pass proves nothing about the
// prose the guard actually polices.
func TestMethodCitationRelation_RangesAndAliases(t *testing.T) {
	// Line numbers are what this test is about, so the fixture is written with them counted.
	//  1 package sample
	//  2 (blank)
	//  3 // Doc comment for Target.
	//  4 func Target() {
	//  5 	_ = 1
	//  6 }
	//  7 (blank)
	//  8 func Other() {
	//  9 	_ = ScopeFor("u", nil)
	// 10 }
	// 11 (blank)
	// 12 // CommentOnlyThing is mentioned here but never declared or called anywhere in this file.
	// 13 (blank)
	// 14 func StringOnly() {
	// 15 	s := "StringLiteralOnlyThing"
	// 16 	_ = s
	// 17 }
	src := "package sample\n\n// Doc comment for Target.\nfunc Target() {\n\t_ = 1\n}\n\n" +
		"func Other() {\n\t_ = ScopeFor(\"u\", nil)\n}\n\n" +
		"// CommentOnlyThing is mentioned here but never declared or called anywhere in this file.\n\n" +
		"func StringOnly() {\n\ts := \"StringLiteralOnlyThing\"\n\t_ = s\n}\n"
	path := filepath.Join(t.TempDir(), "sample.go")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cases := []struct {
		name          string
		method        string
		lo, hi        int
		wantCheckable bool
		wantViolation bool
		whyItMatters  string
	}{
		{"range entirely before the function", "Target", 1, 2, true, true,
			"a range that stops short of the declaration is the exact drift the widening exists to catch"},
		{"range entirely after the function", "Target", 8, 10, true, true,
			"the GetBudgetVsActual range that started this — it named a method and spanned an unrelated body"},
		{"range starting in the doc comment", "Target", 3, 5, true, false,
			"citing from the doc comment down into the body is the common, correct habit; overlap must accept it"},
		{"range ending one line past the closing brace", "Target", 5, 7, true, false,
			"an off-by-one at the tail is noise, not drift — requiring both ends inside would fire on it"},
		{"range exactly the body", "Target", 4, 6, true, false, "the unambiguous correct case"},
		{"single line inside the body", "Target", 5, 5, true, false,
			"the pre-widening behaviour must be unchanged: hi == lo is the old relation"},
		{"single line outside the body", "Target", 9, 9, true, true,
			"likewise for the old relation's failing case"},

		// #1569 R6C: the alias class (naming the Scope.Kind a `ScopeFor` call resolves to, e.g.
		// citing `ScopeUser` over a `ScopeFor(userID, nil)` line) was REMOVED, not fixed. The
		// review proved by execution that the second argument to ScopeFor is a nilable pointer, so
		// no static text rule can tell ScopeUser from ScopeHousehold from the source alone — every
		// variant tried (bare identifier text, a second-arg regex, gofmt-wrapped calls, a named nil
		// variable, a nested first-position call) either accepted the wrong kind or silently stopped
		// checking. The fix is at the citation, not the guard: the four real citations that used to
		// name the resolved kind now name the constructor, `ScopeFor`, directly — see
		// internal/repository/cache_completeness_repo_test.go's BudgetRepository entries. That
		// leaves nothing here for the alias class to test; only the plain fallback below remains.
		{"non-alias identifier absent entirely", "NoSuchThing", 1, 2, true, true,
			"the fallback proximity relation is unchanged for everything outside any alias table"},

		// #1569 R6B/B2: the fallback must search CODE, not comments — a citation naming a method
		// the file does not contain must not pass just because a nearby comment happens to mention
		// the same name in prose.
		{"non-alias identifier appears only inside a comment", "CommentOnlyThing", 12, 12, true, true,
			"a comment merely mentioning the name is not evidence the file contains the code"},

		// #1569 R6C/item 6: readSourceWithCommentsBlanked's doc comment used to claim the fallback
		// "can only be satisfied by identifiers that appear in actual code", which reads as
		// excluding string literals. It does not: this guard's own corpus relies on citations that
		// name a SQL table/column identifier findable ONLY inside a backtick string (e.g.
		// `subscription_entitlements` at entitlement_repository.go:114). Blanking string literals
		// to make the doc comment literally true was tried and reverted because it turned that real
		// citation into a false positive. So this case pins the CHOSEN behaviour — a string literal
		// counts — not the rejected one.
		{"identifier that appears only inside a string literal still counts", "StringLiteralOnlyThing", 15, 15, true, false,
			"the doc comment now says a string literal satisfies the fallback; this is the corpus reason why"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			problem, ok := methodCitationProblem(path, tc.method, tc.lo, tc.hi)
			if ok != tc.wantCheckable {
				t.Fatalf("checkable = %v, want %v — %s", ok, tc.wantCheckable, tc.whyItMatters)
			}
			if got := problem != ""; got != tc.wantViolation {
				t.Errorf("violation = %v (%q), want %v — %s", got, problem, tc.wantViolation, tc.whyItMatters)
			}
		})
	}
}

// TestMethodCitationExtraction_CollectsRangesInBothOrders pins the half of the widening that is
// pure extraction. It is separate from the relation test because the failure it guards against is
// silent: before #1569 round 6 both regexes required a closing backtick immediately after the line
// digits, so a range citation produced NO pair at all — the guard reported success having looked at
// nothing. A relation that is right about pairs it never sees is worth nothing.
func TestMethodCitationExtraction_CollectsRangesInBothOrders(t *testing.T) {
	text := "prose `svc.DoThing` (`internal/services/a_service.go:10-20`) more prose, " +
		"and `internal/repository/b_repository.go:30-40` (`Repo.DoOther`), " +
		"and a single `Plain` (`c.go:7`)."

	got := extractMethodCitationPairs(text)
	want := []methodCitationPair{
		{"DoThing", "internal/services/a_service.go", 10, 20},
		{"Plain", "c.go", 7, 7},
		{"DoOther", "internal/repository/b_repository.go", 30, 40},
	}
	if len(got) != len(want) {
		t.Fatalf("extracted %d pairs, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("pair %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestMethodCitationRangesInTheCorpusAreClean is the false-positive half, measured against the REAL
// prose rather than a fixture.
//
// The risk the widening carries is not that it under-fires; it is that it fires on legitimate
// citations, because this branch has the recorded lesson that a guard which fires on correct code
// gets disabled by the next engineer. So this asserts the specific thing that could go wrong: every
// RANGE-form pair in the corpus is accepted. It also pins a non-zero range count, because "no range
// pair is a violation" is trivially true of an extraction that collects no range pairs — the exact
// fail-open the widening was fixing.
func TestMethodCitationRangesInTheCorpusAreClean(t *testing.T) {
	ranges, violations := 0, []string{}
	for _, g := range citedGuardFiles() {
		b, err := os.ReadFile(g.path)
		if err != nil {
			t.Fatalf("read %s: %v", g.path, err)
		}
		for _, p := range extractMethodCitationPairs(string(b)) {
			if p.hi == p.lo {
				continue
			}
			resolved := resolveCitedFileForRange(p.path)
			if resolved == "" || !strings.HasSuffix(resolved, ".go") {
				continue
			}
			problem, ok := methodCitationProblem(resolved, p.method, p.lo, p.hi)
			if !ok {
				continue
			}
			ranges++
			if problem != "" {
				violations = append(violations, p.path+":"+problem)
			}
		}
	}

	// Measured 2026-08-26 (#1569 R6C, after the ScopeUser/ScopeHousehold alias class was removed
	// and the four citations that relied on it were re-pointed at `ScopeFor` directly): 12
	// checkable range pairs. This is a floor, not an equality — citations get added. There is no
	// separate alias-class floor any more: those four citations are now ordinary `ScopeFor`
	// range pairs, checked by the same relation as everything else in this loop.
	if ranges < 12 {
		t.Fatalf("only %d checkable RANGE pairs — the range extraction has regressed, and this "+
			"test's pass would be vacuous", ranges)
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Errorf("%d REAL range citation(s) were rejected. Before relaxing the guard, check each "+
			"one is genuinely wrong — a false positive here is how a guard gets disabled:\n  - %s",
			len(violations), strings.Join(violations, "\n  - "))
	}
}
