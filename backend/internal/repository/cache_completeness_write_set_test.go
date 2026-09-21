package repository

// Issue #1537 — mechanical write-set derivation, replacing prose for the majority of entries.
//
// WHY THIS EXISTS: cache_completeness_repo_test.go can only check that a disposition's detail
// string is non-empty. It cannot tell a correct classification from a confident falsehood — and
// three demonstrably false claims were already sitting in that map as "reviewed" prose. This file
// derives the answer instead of narrating it:
//
//   cached-read tables  ∩  a method's write tables  ==  ∅   ⇒   NoCacheImpact, no prose needed
//
// A derived entry carries no reasoning to rot, and it BREAKS the day someone adds a cached read
// over a table it writes — which prose can never do.
//
// ⚠ FAIL CLOSED. The dangerous error here is a false NEGATIVE in write-set extraction: if the
// extractor cannot see a method's writes, its write set looks empty, an empty set is trivially
// disjoint from anything, and the derivation would manufacture exactly the wrong answer —
// "NoCacheImpact" for a method that really does write a cached table. So a mutating method whose
// write set cannot be resolved is a hard FAILURE, never a pass. Five such methods were found while
// building this (composed SQL through helpers, and delegation inside a transaction); they are the
// reason this rule exists rather than a hypothetical.
//
// WHAT IT STILL DOES NOT PROVE:
//   - Table granularity, not column. A write to a cached-read table lands in the human-reviewed
//     set even when the specific column is never projected; that is the safe direction.
//   - SQL built by runtime string concatenation across function boundaries is invisible to a
//     static scan. That is why unresolved is a failure rather than a shrug.
//   - Nothing here says a bump is correctly SCOPED. Disjointness only says no cached read can
//     observe the write at all.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
)

// cachedReadRepoFiles are the repository files whose SELECTs back a cache.GetOrLoad call site.
//
// Derived by hand from the only 19 GetOrLoad sites in the tree and pinned here because a static
// scan cannot follow services -> repository across packages:
//   - services/analytics_service_cached.go (17 methods) -> analytics_repository.go, and
//     balance_snapshot_repository.go via analyticsService.SetBalanceSnapshotRepo
//   - services/dashboard_preferences_service_cached.go  -> dashboard_preferences_repository.go
//   - services/identity_resolver_cached.go              -> user_identity_repository.go
//
// TestCachedReadRepoFileListHasNotDrifted guards the list against a new *_cached.go appearing.
var cachedReadRepoFiles = []string{
	"analytics_repository.go",
	// #1626: GetCategoryHierarchy's SQL lives in its own file, and it IS a cached read. Its read
	// set adds no new table — transactions and custom_categories were already in
	// expectedCachedReadTables — but the premise-checking scans below walk THIS list, so omitting
	// the file would exempt the new SQL from them.
	"analytics_category_path.go",
	"balance_snapshot_repository.go",
	"dashboard_preferences_repository.go",
	"user_identity_repository.go",
}

// deleteCascadeReachesCachedTables maps a table to the cached-read tables that a DELETE on it also
// writes, THROUGH FOREIGN-KEY CASCADES — rows the statement never names.
//
// ⚠ THIS EXISTS BECAUSE THE TEXT-ONLY DERIVATION WAS UNSOUND. An adversarial audit of the
// pre-existing dispositions caught it: LinkedItemRepository.Delete's SQL mentions only
// linked_items, but `accounts` has ON DELETE CASCADE and `transactions` has ON DELETE SET NULL
// against linked_items(id) — so that one statement writes two cached-read tables invisibly. Every
// DELETE-shaped write set derived from statement text alone was therefore under-reported, in the
// dangerous direction: an under-reported write set looks disjoint and yields a false NoCacheImpact.
//
// Recursion follows CASCADE only. SET NULL / SET DEFAULT update the child row rather than deleting
// it, so the chain stops there — the child is written, but its own children are untouched.
//
// Derived from pg_constraint against a real migrated database, then pinned here so the unit test
// stays hermetic. TestDeleteCascadeMapMatchesLiveSchema (integration build tag) recomputes it from
// the live catalogue and fails on drift, so a new FK or a changed ON DELETE action cannot silently
// invalidate a derived disposition. Do not hand-edit: regenerate it.
//
// Issue #1569, migration 000150: accounts.savings_goals was widened from ON DELETE CASCADE to
// RESTRICT (so a DELETE on accounts no longer reaches budget_categories through savings_goals —
// RESTRICT edges are excluded entirely from this derivation's `confdeltype IN ('c','n','d')`
// filter), and balance_snapshots.account_id changed from CASCADE to SET NULL (so recursion through
// it stops there rather than continuing). Both "accounts" and "linked_items" (which reaches
// budget_categories only transitively, through accounts) lost "budget_categories" from their
// reachable-cached-tables set as a result. Regenerated against the live schema post-migration
// (see TestDeleteCascadeMapMatchesLiveSchema, which caught the drift).
var deleteCascadeReachesCachedTables = map[string][]string{
	// #1626 Phase 3 added account_status_history to the cached-read set (see
	// expectedCachedReadTables); this map records only the cascade targets a cached read can
	// observe, so the three roots that reach it — accounts, linked_items, users — gain it here.
	// Re-derived from pg_constraint by TestDeleteCascadeMapMatchesLiveSchema.
	"accounts":          {"account_status_history", "balance_snapshots", "transactions"},
	"budgets":           {"budget_categories"},
	"custom_categories": {"budget_categories"},
	"households":        {"household_account_shares"},
	"linked_items":      {"account_status_history", "accounts", "balance_snapshots", "transactions"},
	"merchants":         {"merchant_name_overrides"},
	"savings_goals":     {"budget_categories"},
	"user_accounts":     {"balance_snapshots", "household_account_shares"},
	// linked_items joined this list in #1569 T10.2 — not because any FK changed, but because
	// linked_items became a CACHED-READ table (see expectedCachedReadTables). This map records
	// only the cascade targets that a cached read can observe, so widening that set widens this
	// one. Re-derived from pg_constraint by TestDeleteCascadeMapMatchesLiveSchema.
	"users": {"account_status_history", "accounts", "balance_snapshots", "budget_categories",
		"budgets", "custom_categories",
		"household_account_shares", "linked_items", "merchant_name_overrides", "transactions",
		"user_accounts", "user_dashboard_preferences", "user_identities"},
}

var (
	reCreateTable = regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?"?([a-z_][a-z0-9_]*)"?`)
	reRenameTable = regexp.MustCompile(`(?i)ALTER\s+TABLE\s+"?([a-z_][a-z0-9_]*)"?\s+RENAME\s+TO\s+"?([a-z_][a-z0-9_]*)"?`)
	reFromJoin    = regexp.MustCompile(`(?i)\b(?:FROM|JOIN)\s+"?([a-z_][a-z0-9_]*)"?`)
	reWriteTarget = regexp.MustCompile(`(?i)\b(?:INSERT\s+INTO|UPDATE|DELETE\s+FROM|TRUNCATE(?:\s+TABLE)?|MERGE\s+INTO)\s+"?([a-z_][a-z0-9_]*)"?`)
	// Separate from reWriteTarget because only a DELETE (or TRUNCATE) triggers ON DELETE actions.
	reDeleteTarget = regexp.MustCompile(`(?i)\b(?:DELETE\s+FROM|TRUNCATE(?:\s+TABLE)?)\s+"?([a-z_][a-z0-9_]*)"?`)
	// Row-locking clauses are NORMALISED AWAY rather than used to skip a statement: a
	// package-level const can legitimately hold `WITH x AS (SELECT ... FOR UPDATE) UPDATE users ...`
	// (see admin_user_repository.go's updateUserTierQuery), so skipping on "FOR UPDATE" would
	// discard a genuine write.
	reRowLock  = regexp.MustCompile(`(?i)FOR\s+(?:NO\s+KEY\s+)?UPDATE|FOR\s+SHARE`)
	reLineComm = regexp.MustCompile(`(?m)//[^\n]*`)
	reCTEName  = regexp.MustCompile(`(?i)(?:WITH|,)\s+"?([a-z_][a-z0-9_]*)"?\s+AS\s*\(`)
	// Fragment matching: used by packageSQLFragments and fragmentsUsedIn.
	//
	// These three deliberately use INTERPRETED string literals with doubled backslashes, not raw
	// strings. staticcheck's S1007 suggests a raw string here, but it cannot apply: these patterns
	// must match a BACKTICK (they find Go raw-string SQL constants), and a Go raw string cannot
	// contain a backtick. Satisfying the linter would mean concatenating "`" fragments around raw
	// parts, which is strictly less readable than the doubled escapes.
	//nolint:staticcheck // S1007: pattern must match a backtick, impossible inside a raw string.
	reSingle = regexp.MustCompile("(?m)^(?:const|var)\\s+(\\w+)\\s*=\\s*`([^`]*)`")
	//nolint:staticcheck // S1007: see reSingle above.
	reBlock = regexp.MustCompile("(?ms)^(?:const|var)\\s*\\(\\n(.*?)^\\)")
	//nolint:staticcheck // S1007: pattern must match a backtick, impossible inside a raw string.
	reInner = regexp.MustCompile("(?m)^\\t(\\w+)\\s*=\\s*`([^`]*)`")
	// Called delegation: followed in writeTablesFor.
	reCall       = regexp.MustCompile(`\b(?:[a-z]\w*\.)?([a-z]\w*)\s*\(`)
	reMethodCall = regexp.MustCompile(`\.([A-Z]\w*)\s*\(`)
)

func normalizeSQL(s string) string { return reRowLock.ReplaceAllString(s, " ") }

// schemaTables parses migrations for the real table namespace, applying renames — `boosts` and
// `boost_executions` exist only via `ALTER TABLE rules RENAME TO boosts` (migration 000095), so a
// CREATE-TABLE-only scan reports a namespace that has been stale since that migration landed.
func schemaTables(t *testing.T) map[string]bool {
	t.Helper()
	dir := filepath.Join("..", "..", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	created := map[string]bool{}
	renames := map[string]string{}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // apply renames in migration order
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			t.Fatalf("read %s: %v", n, err)
		}
		s := string(b)
		for _, m := range reCreateTable.FindAllStringSubmatch(s, -1) {
			created[strings.ToLower(m[1])] = true
		}
		for _, m := range reRenameTable.FindAllStringSubmatch(s, -1) {
			renames[strings.ToLower(m[1])] = strings.ToLower(m[2])
		}
	}
	for from, to := range renames {
		delete(created, from)
		created[to] = true
	}
	if len(created) < 40 {
		t.Fatalf("parsed only %d tables from migrations — the parse is broken, and a truncated table "+
			"namespace silently shrinks the cached-read set, which turns real overlaps into false "+
			"disjointness", len(created))
	}
	return created
}

// packageSQLFragments returns every package-level backtick string in this package, keyed by
// identifier. Both the read scan and the write scan expand these: SQL here is routinely assembled
// from const fragments (merchant_sql_helpers.go's merchantSortJoins supplies the merchants /
// merchant_name_overrides JOINs that analytics_repository.go never spells out), and missing them
// under-reports both sides.
func packageSQLFragments(t *testing.T, srcs map[string]string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, src := range srcs {
		for _, m := range reSingle.FindAllStringSubmatch(src, -1) {
			out[m[1]] = m[2]
		}
		for _, b := range reBlock.FindAllStringSubmatch(src, -1) {
			for _, m := range reInner.FindAllStringSubmatch(b[1], -1) {
				out[m[1]] = m[2]
			}
		}
	}
	return out
}

// fragmentsUsedIn returns the SQL fragment values whose identifiers appear in text as word boundaries.
// Consolidates the common pattern of finding which fragments are referenced in a text blob.
//
// ⚠ #1569 Round 11 Task 5: a bare `\bname\b` match alone is NOT sufficient. This package follows a
// real convention — a const `fooCTE` paired with a wrapper func `fooCTESQL(observedExpr string)
// string` that call sites invoke as `fooCTESQL(...)` rather than referencing the bare const — and
// `\b` does NOT fire between "CTE" and the immediately-following "SQL(" because both sides are word
// characters. Before this fix, `accountChainSupersessionCTESQL("TRUE")`'s four real call sites in
// balance_snapshot_repository.go never triggered fragment expansion at all; the CTE text reached the
// corpus ONLY because three unrelated comments happened to spell the bare const name followed by a
// non-word character ("accountChainSupersessionCTE." / "accountChainSupersessionCTE walks"), and
// comments are not stripped on this read path. VALIDATED BY MUTATION: editing those three comments to
// remove the bare name reproducibly turned TestCachedReadTableSetMatchesExpected red ("LOST 1
// table(s): [linked_items]"); restoring them turned it green again. The second alternative below
// closes that hole by also matching the `nameSQL(` call form directly, so the corpus no longer
// depends on comment text at all.
func fragmentsUsedIn(text string, frags map[string]string) []string {
	var result []string
	for name, val := range frags {
		quoted := regexp.QuoteMeta(name)
		pattern := `\b` + quoted + `\b|\b` + quoted + `SQL\(`
		if regexp.MustCompile(pattern).MatchString(text) {
			result = append(result, val)
		}
	}
	return result
}

func expandFragments(text string, frags map[string]string) string {
	var sb strings.Builder
	sb.WriteString(text)
	for _, val := range fragmentsUsedIn(text, frags) {
		sb.WriteString("\n")
		sb.WriteString(val)
	}
	return sb.String()
}

// cachedReadTables is the set of tables any cache.GetOrLoad-backed read can observe.
func cachedReadTables(t *testing.T, srcs map[string]string, frags map[string]string, schema map[string]bool) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, f := range cachedReadRepoFiles {
		src, ok := srcs[f]
		if !ok {
			t.Fatalf("cachedReadRepoFiles names %q, which does not exist — the list has drifted", f)
		}
		text := normalizeSQL(expandFragments(src, frags))
		ctes := map[string]bool{}
		for _, m := range reCTEName.FindAllStringSubmatch(text, -1) {
			ctes[strings.ToLower(m[1])] = true
		}
		for _, m := range reFromJoin.FindAllStringSubmatch(text, -1) {
			name := strings.ToLower(m[1])
			if schema[name] && !ctes[name] {
				out[name] = true
			}
		}
	}
	return out
}

type funcKey struct{ recv, name string }

func repoFuncBodies(t *testing.T, srcs map[string]string) map[funcKey]string {
	t.Helper()
	re := regexp.MustCompile(`\nfunc\s+(?:\((\w+)\s+\*?(\w+)\)\s+)?(\w+)\s*\(`)
	out := map[funcKey]string{}
	for _, src := range srcs {
		for _, loc := range re.FindAllStringSubmatchIndex(src, -1) {
			recv := ""
			if loc[4] >= 0 {
				recv = src[loc[4]:loc[5]]
			}
			name := src[loc[6]:loc[7]]
			// Find the body's opening brace by walking the signature's parens to depth 0 first.
			// Taking the first '{' after the name is wrong whenever the signature itself contains
			// one — `fn func() (interface{}, error)` made IdempotencyRepository.GetOrCreate's body
			// parse as the empty `{}` of `interface{}`, so its write set came back unresolved.
			i := -1
			depthParen := 1 // the '(' that the name regex consumed
			for k := loc[1]; k < len(src); k++ {
				switch src[k] {
				case '(':
					depthParen++
				case ')':
					depthParen--
				case '{':
					if depthParen == 0 {
						i = k
					}
				case ';':
					if depthParen == 0 {
						k = len(src)
					}
				}
				if i >= 0 {
					break
				}
			}
			if i < 0 {
				continue
			}
			depth, j := 1, i+1
			for ; j < len(src) && depth > 0; j++ {
				switch src[j] {
				case '{':
					depth++
				case '}':
					depth--
				}
			}
			out[funcKey{recv, name}] = reLineComm.ReplaceAllString(src[i:j], "")
		}
	}
	return out
}

// writeTablesFor resolves the tables a method writes, following intra-package delegation and
// package-level SQL constants. Both hops are load-bearing: AuditLogRepository.Log has no SQL in its
// own body (the INSERT is in the unexported writeAuditLogEntry helper), and
// InvalidWebhookAttemptRepository.Record executes a package-level const.
func writeTablesFor(bodies map[funcKey]string, frags map[string]string, schema map[string]bool,
	recv, name string, seen map[funcKey]bool, depth int) (map[string]bool, bool) {

	tables := map[string]bool{}
	if depth > 5 {
		return tables, false
	}
	key := funcKey{recv, name}
	if seen[key] {
		return tables, false
	}
	if seen == nil {
		seen = map[funcKey]bool{}
	}
	seen[key] = true
	body, ok := bodies[key]
	if !ok {
		return tables, false
	}

	resolved := false
	harvest := func(text string) {
		norm := normalizeSQL(text)
		for _, m := range reWriteTarget.FindAllStringSubmatch(norm, -1) {
			n := strings.ToLower(m[1])
			if schema[n] {
				tables[n] = true
				resolved = true
			}
		}
		// A DELETE also writes every table reachable from it by an ON DELETE action — rows the
		// statement never names. Omitting this under-reports DELETE write sets, the direction that
		// manufactures a false NoCacheImpact.
		for _, m := range reDeleteTarget.FindAllStringSubmatch(norm, -1) {
			for _, child := range deleteCascadeReachesCachedTables[strings.ToLower(m[1])] {
				tables[child] = true
				resolved = true
			}
		}
	}
	harvest(body)
	for _, val := range fragmentsUsedIn(body, frags) {
		harvest(val)
	}

	follow := func(rt, callee string) {
		sub, subResolved := writeTablesFor(bodies, frags, schema, rt, callee, seen, depth+1)
		if subResolved {
			resolved = true
			for k := range sub {
				tables[k] = true
			}
		}
	}
	// Unexported helpers and package-level funcs (AuditLogRepository.Log -> writeAuditLogEntry).
	for _, m := range reCall.FindAllStringSubmatch(body, -1) {
		follow(recv, m[1])
		follow("", m[1])
	}
	// EXPORTED method calls on the same receiver type — transaction-scoped self-delegation.
	// budgetRepository.CreateWithCategories opens a tx, builds `txRepo := &budgetRepository{db: tx}`
	// and calls txRepo.Create / txRepo.CreateCategory; a lowercase-only callee regex saw no SQL at
	// all and reported the method unresolved. Followed only against the SAME receiver, so an
	// injected collaborator's tables are not misattributed here.
	for _, m := range reMethodCall.FindAllStringSubmatch(body, -1) {
		follow(recv, m[1])
	}
	// An explicit local concrete value supplies the receiver type without guessing
	// an injected interface. Custom-category deletion uses this shape to refresh
	// budget siblings in the same transaction; those indirect writes count too.
	for _, local := range regexp.MustCompile(`\b([a-zA-Z_]\w*)\s*:=\s*&?([a-zA-Z_]\w*)\s*\{`).FindAllStringSubmatch(body, -1) {
		calls := regexp.MustCompile(`\b` + regexp.QuoteMeta(local[1]) + `\.([a-zA-Z_]\w*)\s*\(`)
		for _, call := range calls.FindAllStringSubmatch(body, -1) {
			follow(local[2], call[1])
		}
	}
	return tables, resolved
}

func TestWriteSetFollowsExplicitCrossRepositoryTransactionDelegate(t *testing.T) {
	bodies := map[funcKey]string{
		{"customCategoryRepository", "DeleteWithReassignment"}: "UPDATE custom_categories SET deleted_at=NOW(); budgetRepo := &budgetRepository{db:tx}; budgetRepo.refreshBudgetActualsInTx(ctx,tx,id)",
		{"budgetRepository", "refreshBudgetActualsInTx"}:       "UPDATE budgets SET income_amount=$1; UPDATE budget_categories SET spent_amount=$1",
	}
	tables, resolved := writeTablesFor(bodies, nil, map[string]bool{"budgets": true, "budget_categories": true, "custom_categories": true}, "customCategoryRepository", "DeleteWithReassignment", nil, 0)
	if !resolved || !tables["budgets"] || !tables["budget_categories"] || !tables["custom_categories"] {
		t.Fatalf("transaction delegate writes missing: %v", tables)
	}
}

// TestRefreshBudgetActualsPropagatesReasons pins that the reasons write is
// visible to the cache-completeness write-set derivation on the REAL source:
// the atomic writer writes budgets.actuals_incomplete_reasons in the same
// transaction as spent_amount, so refreshBudgetActualsInTx's derived write set
// must still include budgets. A reasons write hidden behind a helper the static
// scan cannot follow would leave the write set empty and manufacture a false
// NoCacheImpact for the method the inventory classifies.
func TestRefreshBudgetActualsPropagatesReasons(t *testing.T) {
	srcs := nonTestRepoSources(t)
	frags := packageSQLFragments(t, srcs)
	schema := schemaTables(t)
	bodies := repoFuncBodies(t, srcs)
	tables, resolved := writeTablesFor(bodies, frags, schema, "budgetRepository", "refreshBudgetActualsInTx", map[funcKey]bool{}, 0)
	if !resolved {
		t.Fatal("refreshBudgetActualsInTx's write set must resolve")
	}
	if !tables["budgets"] {
		t.Fatal("the reasons write must keep budgets in refreshBudgetActualsInTx's write set")
	}
	if !tables["budget_categories"] {
		t.Fatal("the spent write must keep budget_categories in refreshBudgetActualsInTx's write set")
	}
}

// implReceiverFor resolves iface to its concrete receiver type.
//
// Requires the candidate to implement EVERY method of the interface. An earlier version scored
// candidates by "most methods matched", which is badly wrong: generic names like Create/Update/
// Delete are shared across dozens of repositories, so the biggest repository type won every
// contest — UserTagRepository resolved to transactionRepository and inherited its write set,
// putting five user_tags writes into the human-review pile for a table no cached read touches.
// Name-mangling alone is also insufficient (SubscriptionOverrideRepository is implemented by
// postgresSubscriptionOverrideRepository, MerchantRepo by MerchantRepository), hence
// implements-all with a lowercase-affinity tiebreak.
func implReceiverFor(bodies map[funcKey]string, ifaceMethods map[string]map[string]bool, iface string) string {
	want := ifaceMethods[iface]
	if len(want) == 0 {
		return ""
	}
	have := map[string]map[string]bool{}
	for k := range bodies {
		if k.recv == "" {
			continue
		}
		if have[k.recv] == nil {
			have[k.recv] = map[string]bool{}
		}
		have[k.recv][k.name] = true
	}

	var candidates []string
	for recv, methods := range have {
		complete := true
		for m := range want {
			if !methods[m] {
				complete = false
				break
			}
		}
		if complete {
			candidates = append(candidates, recv)
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	sort.Strings(candidates)
	// Tiebreak on name affinity: prefer a receiver whose lowercased name contains the
	// interface's lowercased name, or vice versa (covers Repo/Repository and postgres* prefixes).
	li := strings.ToLower(iface)
	for _, c := range candidates {
		lc := strings.ToLower(c)
		if strings.Contains(lc, li) || strings.Contains(li, lc) {
			return c
		}
	}
	trimmed := strings.TrimSuffix(strings.TrimSuffix(li, "repository"), "repo")
	for _, c := range candidates {
		if trimmed != "" && strings.Contains(strings.ToLower(c), trimmed) {
			return c
		}
	}
	return candidates[0]
}

func interfaceMethodSets(srcs map[string]string) map[string]map[string]bool {
	reIface := regexp.MustCompile(`\btype\s+(\w+)\s+interface\s*\{`)
	reMeth := regexp.MustCompile(`(?m)^\s*([A-Z]\w*)\s*\(`)
	out := map[string]map[string]bool{}
	for _, src := range srcs {
		for _, loc := range reIface.FindAllStringSubmatchIndex(src, -1) {
			name := src[loc[2]:loc[3]]
			depth, i := 1, loc[1]
			for ; i < len(src) && depth > 0; i++ {
				switch src[i] {
				case '{':
					depth++
				case '}':
					depth--
				}
			}
			blk := reLineComm.ReplaceAllString(src[loc[1]:i-1], "")
			if out[name] == nil {
				out[name] = map[string]bool{}
			}
			for _, m := range reMeth.FindAllStringSubmatch(blk, -1) {
				out[name][m[1]] = true
			}
		}
	}
	return out
}

func nonTestRepoSources(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read repository dir: %v", err)
	}
	out := map[string]string{}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		b, err := os.ReadFile(n)
		if err != nil {
			t.Fatalf("read %s: %v", n, err)
		}
		out[n] = string(b)
	}
	return out
}

// resolveWriteSetsOnce/resolveWriteSetsCache memoize resolveWriteSets for the lifetime of the test
// binary (#1569 round 14 task 2). Before this, TestCachedReadTableSetMatchesExpected,
// TestEveryMutatingMethodWriteSetResolves and TestDerivedNoCacheImpactIsActuallyDisjoint each
// recomputed the ENTIRE derivation independently — re-reading every non-test .go file in this
// package, re-parsing every migration for schemaTables, and re-running the regex-based function-body
// and interface-method extraction three times — 38.6s of the package's 126s under `go test -race`
// for work that produces the identical answer every time within one run.
//
// This is a caching fix, not the AST-sharing fix: this file does not use go/ast at all (it works on
// raw source text via regexp, unlike the tombstone/history-delete/encrypted-column guards), so there
// is no argument-aware unit boundary to share here. The nil-check below is deliberate: if the first
// call Fatals partway through resolveWriteSetsUncached (a t.Fatalf inside it Goexits the calling
// test's goroutine, which still runs the deferred sync.Once bookkeeping and marks the Once "done"
// without ever assigning the cache), a LATER caller must not read that as "computed, zero entries" —
// that would manufacture exactly the "guard that cannot fail" failure mode this round exists to
// close. It instead Fatals again, naming which corpus never got established.
var (
	resolveWriteSetsOnce       sync.Once
	resolveWriteSetsWrites     map[string]map[string]bool
	resolveWriteSetsResolved   map[string]bool
	resolveWriteSetsReadTables map[string]bool
)

// resolveWriteSets returns, for every key in repoMethodDispositions, the tables it writes and
// whether extraction resolved anything at all. Memoized — see resolveWriteSetsOnce above.
func resolveWriteSets(t *testing.T) (map[string]map[string]bool, map[string]bool, map[string]bool) {
	t.Helper()
	resolveWriteSetsOnce.Do(func() {
		resolveWriteSetsWrites, resolveWriteSetsResolved, resolveWriteSetsReadTables = resolveWriteSetsUncached(t)
	})
	if resolveWriteSetsWrites == nil {
		t.Fatalf("resolveWriteSets' shared computation never completed (a prior call must have " +
			"Fatal'd partway through) — this test cannot proceed without the shared corpus; see the " +
			"first test in this binary run that failed")
	}
	return resolveWriteSetsWrites, resolveWriteSetsResolved, resolveWriteSetsReadTables
}

func resolveWriteSetsUncached(t *testing.T) (map[string]map[string]bool, map[string]bool, map[string]bool) {
	t.Helper()
	srcs := nonTestRepoSources(t)
	frags := packageSQLFragments(t, srcs)
	schema := schemaTables(t)
	bodies := repoFuncBodies(t, srcs)
	ifaces := interfaceMethodSets(srcs)
	readTables := cachedReadTables(t, srcs, frags, schema)

	writes := map[string]map[string]bool{}
	resolved := map[string]bool{}
	for key := range repoMethodDispositions {
		parts := strings.SplitN(key, ".", 2)
		if len(parts) != 2 {
			t.Fatalf("malformed disposition key %q", key)
		}
		recv := implReceiverFor(bodies, ifaces, parts[0])
		if recv == "" {
			writes[key] = map[string]bool{}
			resolved[key] = false
			continue
		}
		tbls, ok := writeTablesFor(bodies, frags, schema, recv, parts[1], map[funcKey]bool{}, 0)
		writes[key] = tbls
		resolved[key] = ok
	}
	return writes, resolved, readTables
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// expectedCachedReadTables pins the derived read set. Every dispositionDerivedNoCacheImpact entry
// in the map is only as sound as this set: if extraction silently under-reports it, real overlaps
// become false disjointness and the guard would certify NoCacheImpact for writes a cached read can
// see. Two independent derivations (this scan, and a hand trace of all 19 cache.GetOrLoad sites)
// produced exactly this list, so a mismatch means one of them broke — investigate, do not just
// update the literal.
//
// ⚠ `linked_items` joined this list in #1569 T10.2 and it is the newest and least obvious member.
// The balance reads no longer collapse a re-link chain by DISTINCT ON; they apply
// accountChainSupersessionCTE, whose item_closure layer walks `linked_items.replaced_by_id`. A
// cached balance/net-worth answer is therefore sensitive to that ONE column — see
// linkedItemsSuccessionColumnsOnly and TestCachedReadsReadOnlyTheSuccessionColumnsOfLinkedItems,
// which is what makes the nine column-scoped dispositions below re-derivable rather than asserted.
//
// ⚠ `account_status_history` joined this list in #1626 Phase 3 and is now the newest member. The
// balance reads gained a per-account carry-forward, and a CARRIED balance — unlike an observed one
// — is bounded by the account's recorded 'active' interval, so a cached balance/net-worth answer is
// sensitive to that table (see aggregatedHistorySQLBase's removal bound and accountLiveOnDateSQL).
// No Go statement in this package WRITES it: it is written entirely by the triggers
// account_status_history_insert_trigger / _update_trigger on `accounts`, so it appears in no
// method's text-derived write set and instead widens deleteCascadeReachesCachedTables below.
var expectedCachedReadTables = []string{
	"account_status_history", "accounts", "balance_snapshots", "budget_categories", "budgets",
	"custom_categories", "household_account_shares", "linked_items", "merchant_name_overrides",
	"merchants", "transactions", "user_accounts", "user_dashboard_preferences", "user_identities",
}

// linkedItemsSuccessionColumnsOnly is the premise every column-scoped `linked_items` disposition
// rests on: the ONLY columns of that table any cached read touches are `id` and `replaced_by_id`.
// A write that changes neither cannot change a cached answer, however loudly the table-grained
// write-set derivation says otherwise.
//
// It is a machine-checked premise, not a claim — that is the whole point of stating it here rather
// than repeating a sentence in ten map entries. See TestCachedReadsReadOnlyTheSuccessionColumnsOfLinkedItems.
var linkedItemsSuccessionColumnsOnly = map[string]bool{"id": true, "replaced_by_id": true}

// TestCachedReadsReadOnlyTheSuccessionColumnsOfLinkedItems proves the premise above by scanning the
// same expanded SQL corpus the write-set derivation scans. Every `<alias>.<column>` reference whose
// alias is bound to `linked_items` must name a column in linkedItemsSuccessionColumnsOnly.
//
// The failure direction matters: if a cached read starts reading `linked_items.status` (say), this
// goes red and the nine dispositions below lose their justification IN THE SAME RUN, rather than
// quietly becoming false the way a prose reason would.
func TestCachedReadsReadOnlyTheSuccessionColumnsOfLinkedItems(t *testing.T) {
	srcs := nonTestRepoSources(t)
	frags := packageSQLFragments(t, srcs)
	aliasRe := regexp.MustCompile(`(?i)\blinked_items\s+(?:AS\s+)?([a-z_][a-z0-9_]*)\b`)
	checked := 0
	for _, f := range cachedReadRepoFiles {
		src, ok := srcs[f]
		if !ok {
			t.Fatalf("cachedReadRepoFiles names %q, which does not exist", f)
		}
		text := normalizeSQL(expandFragments(src, frags))
		for _, m := range aliasRe.FindAllStringSubmatch(text, -1) {
			alias := m[1]
			if strings.EqualFold(alias, "SET") || strings.EqualFold(alias, "WHERE") {
				continue
			}
			colRe := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(alias) + `\.([a-z_][a-z0-9_]*)`)
			for _, c := range colRe.FindAllStringSubmatch(text, -1) {
				checked++
				if !linkedItemsSuccessionColumnsOnly[strings.ToLower(c[1])] {
					t.Errorf("cached read in %s references linked_items.%s via alias %q. Only %v are "+
						"allowed: the ten LinkedItemRepository dispositions in "+
						"cache_completeness_repo_test.go are column-scoped and rest on this. Either "+
						"revert the read, or re-classify every one of them.",
						f, c[1], alias, sortedKeys(linkedItemsSuccessionColumnsOnly))
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no linked_items column references found in the cached-read corpus — this guard " +
			"would pass vacuously. If the succession walk no longer reads linked_items, drop " +
			"linked_items from expectedCachedReadTables and restore the derived dispositions.")
	}
}

func TestCachedReadTableSetMatchesExpected(t *testing.T) {
	_, _, got := resolveWriteSets(t)
	want := map[string]bool{}
	for _, w := range expectedCachedReadTables {
		want[w] = true
	}
	var missing, extra []string
	for _, w := range expectedCachedReadTables {
		if !got[w] {
			missing = append(missing, w)
		}
	}
	for g := range got {
		if !want[g] {
			extra = append(extra, g)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("cached-read table extraction LOST %d table(s): %v.\nThis is the dangerous "+
			"direction: a table missing here makes every write to it look disjoint, so the guard "+
			"would certify dispositionDerivedNoCacheImpact for writes a cached read can actually "+
			"observe. Note merchants / merchant_name_overrides arrive only via the merchantSortJoins "+
			"const in merchant_sql_helpers.go, not as literal text in analytics_repository.go.",
			len(missing), missing)
	}
	if len(extra) > 0 {
		t.Errorf("cached-read table extraction GAINED %d table(s): %v. Either a cached read genuinely "+
			"reads something new (then update expectedCachedReadTables AND re-check every derived "+
			"entry, because some may no longer be disjoint), or the scan is picking up a CTE/alias "+
			"as a table.", len(extra), extra)
	}
}

func TestEveryMutatingMethodWriteSetResolves(t *testing.T) {
	writes, resolved, _ := resolveWriteSets(t)
	var unresolved []string
	for key := range repoMethodDispositions {
		if !resolved[key] {
			unresolved = append(unresolved, key)
		}
		_ = writes
	}
	sort.Strings(unresolved)
	if len(unresolved) > 0 {
		t.Errorf("%d method(s) have an UNRESOLVED write set: %v\n\n"+
			"FAIL CLOSED, deliberately. An unresolved write set is indistinguishable from an EMPTY "+
			"one, and an empty set is trivially disjoint from the cached-read tables — so treating "+
			"this as a pass would manufacture dispositionDerivedNoCacheImpact for a method that "+
			"really does write a cached table. Every one of these was resolvable once the extractor "+
			"learned the right hop (package-level SQL consts; unexported helpers, as in "+
			"AuditLogRepository.Log -> writeAuditLogEntry; same-receiver exported delegation inside a "+
			"transaction, as in budgetRepository.CreateWithCategories; and paren-aware body finding, "+
			"since `fn func() (interface{}, error)` in a signature otherwise truncates the body to "+
			"the empty braces of `interface{}`). Teach the extractor the missing hop rather than "+
			"exempting the method.", len(unresolved), unresolved)
	}
}

func TestDerivedNoCacheImpactIsActuallyDisjoint(t *testing.T) {
	writes, resolved, readTables := resolveWriteSets(t)
	derived := 0
	for key, d := range repoMethodDispositions {
		if d.kind != dispositionDerivedNoCacheImpact {
			continue
		}
		derived++
		if !resolved[key] {
			t.Errorf("%s is dispositionDerivedNoCacheImpact but its write set could not be resolved — "+
				"the derivation is vacuous, not proven", key)
			continue
		}
		var hits []string
		for w := range writes[key] {
			if readTables[w] {
				hits = append(hits, w)
			}
		}
		if len(hits) > 0 {
			sort.Strings(hits)
			t.Errorf("%s is dispositionDerivedNoCacheImpact, but it writes %v which a cached read "+
				"OBSERVES (overlap: %v). Either a cached read started touching that table, or the "+
				"classification was wrong. This needs a human disposition, not a derived one.",
				key, sortedKeys(writes[key]), hits)
		}
	}
	if derived == 0 {
		t.Fatal("no dispositionDerivedNoCacheImpact entries found — this guard would pass vacuously; " +
			"if the kind is genuinely unused, delete it rather than leaving an inert check")
	}
	t.Logf("re-derived and confirmed disjoint: %d entries", derived)
}

// TestCachedReadRepoFileListHasNotDrifted fails when a new cached service appears, because
// cachedReadRepoFiles was hand-derived from the cache.GetOrLoad call sites and a static scan in
// THIS package cannot follow services -> repository across packages. A new *_cached.go could
// introduce a cached read over a table currently believed unread, silently invalidating every
// derived entry that writes it.
func TestCachedReadRepoFileListHasNotDrifted(t *testing.T) {
	dir := filepath.Join("..", "services")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read services dir: %v", err)
	}
	// Pinned GetOrLoad call-site counts, per cached service. The filename check below cannot see a
	// NEW cached read added INSIDE an existing file — which is the likelier drift, and the one that
	// silently invalidates every derived entry writing a newly-read table (Codex review, HIGH #2).
	// A cached read can also enter through dependency injection rather than a new import: that is
	// exactly how balance_snapshots joins the read set, via analyticsService.SetBalanceSnapshotRepo.
	wantGetOrLoadCounts := map[string]int{
		// 18 since #1626 added GetCategoryHierarchy. Re-derived rather than merely incremented:
		// its read set is transactions + custom_categories, both ALREADY in
		// expectedCachedReadTables, so no previously-disjoint write becomes overlapping and every
		// dispositionDerivedNoCacheImpact entry still holds.
		"analytics_service_cached.go":             18,
		"dashboard_preferences_service_cached.go": 1,
		"identity_resolver_cached.go":             1,
	}
	for name, want := range wantGetOrLoadCounts {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("cannot read cached service %s: %v", name, err)
			continue
		}
		if got := strings.Count(string(b), "cache.GetOrLoad("); got != want {
			t.Errorf("%s has %d cache.GetOrLoad call site(s), pinned at %d. A cached read was added or "+
				"removed. Re-derive expectedCachedReadTables and cachedReadRepoFiles, then re-check every "+
				"dispositionDerivedNoCacheImpact entry: a newly cached read can turn a previously "+
				"disjoint write into an overlapping one, and the derivation would not notice.",
				name, got, want)
		}
	}

	knownCachedServices := map[string]bool{
		"analytics_service_cached.go":             true,
		"dashboard_preferences_service_cached.go": true,
		"identity_resolver_cached.go":             true,
	}
	var unexpected []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, "_cached.go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		if !knownCachedServices[n] {
			unexpected = append(unexpected, n)
		}
	}
	sort.Strings(unexpected)
	if len(unexpected) > 0 {
		t.Errorf("new cached service file(s) %v — re-derive cachedReadRepoFiles and "+
			"expectedCachedReadTables, then re-check every dispositionDerivedNoCacheImpact entry: a "+
			"newly cached read can turn a previously disjoint write into an overlapping one",
			unexpected)
	}
	for n := range knownCachedServices {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Errorf("known cached service %s is gone (%v) — cachedReadRepoFiles may now be stale, "+
				"and a read set that is too LARGE wrongly forces methods into human review while a "+
				"read set that is too small is unsafe", n, err)
		}
	}
}
