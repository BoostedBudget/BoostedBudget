package repository

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Issue #1569 Task 8.
//
// Migration 000158 turns four destructive config DELETEs into tombstones: savings_goals, boosts,
// budgets and budget_categories are now soft-deleted so that savings_goal_contributions,
// boost_executions, rollover_history and budget_period_events survive the delete instead of being
// cascaded away.
//
// That only works if EVERY read excludes tombstoned rows by default. A single query that forgets
// is worse than not having the column at all: the user deletes a goal, the UI keeps showing it,
// and the delete button appears not to work. There are ~60 SQL sites reading these four tables
// across the repository, database and analytics packages, and reviewing them by eye is exactly the
// enumeration that goes stale the first time someone adds a query.
//
// THE RULE — the same mechanism as TestNoTransactionReadForgetsTheTombstoneFilter, which is this
// guard for transactions. The check is PER REFERENCE, not per statement. For every FROM, JOIN or
// USING reference to one of the four tables in non-test Go source under internal/ (a DELETE's own
// FROM excepted — that is a write), the enclosing statement must contain either:
//
//   - a predicate `<alias>.deleted_at IS NULL` qualified to THAT reference's alias, or
//   - an exemption naming THAT reference: `tombstone-exempt(<table>[ <alias>]): <reason>`.
//
// Per-reference is load-bearing, and this is not hypothetical. The original form asked only whether
// the statement contained the substring `deleted_at IS NULL` anywhere. That let
// transfer_suggestion_repository.go's GetByUserIDStatuses join `boosts` with NO predicate and still
// pass, because an adjacent `transactions` join — added by a different task — carried the string.
// A live read was returning tombstoned boosts while this guard was green. The transaction guard had
// the identical weakness, latent: a mutation that strips the transactions predicate while adding an
// accounts one passes the old form and fails this one.
//
// Still syntactic, not semantic: it cannot tell a correct predicate from one in the wrong clause.
// It CAN tell which reference an exemption covers, because the token names it. Its job is to make
// FORGETTING impossible; getting it RIGHT is what the behavioural tests are for.
//
// Mutation-checked: deleting `AND <alias>.deleted_at IS NULL` from any one of the audited queries
// turns this test RED with that file, line and alias named.
//
// ⚠⚠ NAMED, PERMANENT BLIND SPOT (#1569 round 12 task 5): this AST scan joins the string
// *ast.BasicLit literals found within a single Go statement (AssignStmt/ValueSpec/ReturnStmt/
// ExprStmt) and pattern-matches the result. `data_export_repository.go`'s GDPR export builds its
// SQL at RUNTIME instead: `DataExportRowQuery` does
// `fmt.Sprintf("SELECT to_jsonb(t)\nFROM %s t\nWHERE %s\nORDER BY %s", tbl.Table,
// tbl.OwnershipPredicate, tbl.OrderBy)`, where `tbl.Table` (e.g. "savings_goals") is a struct
// field literal from a WHOLLY SEPARATE statement (the `dataExportTables` slice literal) than the
// one holding the `"FROM %s t"` format string. So the PRIMARY-TABLE READ that DataExportRowQuery
// builds is invisible to this guard, its three siblings
// (TestNoTransactionReadForgetsTheTombstoneFilter, TestNoCustomCategoryReadForgetsTheTombstoneFilter,
// TestNoSavingsContributionReadForgetsTheTombstoneFilter) and any future one on this mechanism —
// for every table the generic export touches.
//
// ⚠ SCOPED DELIBERATELY, because the obvious wider claim is FALSE and was written here first
// (#1569 round 12 task 5 review). It is NOT true that "no statement ever contains `FROM <exported
// table>`": the `dataExportTables` slice literal itself contains `FROM budgets b` and
// `FROM savings_goals g` inside the OwnershipPredicate SUBQUERIES for budget_categories,
// budget_period_events, savings_goal_contributions and savings_goal_category_detach — that IS one
// statement this scan walks, it DOES see those, and they already carry their own
// `tombstone-exempt(...)` tokens. Only the Sprintf'd primary read is out of reach.
//
// This is deliberately left unaddressed rather than "fixed": the export's BEHAVIOUR is correct
// as-is (a GDPR Article 15 export must include tombstoned rows — the ownership predicates set in
// `data_export_repository.go` carry no `deleted_at` filter for exactly that reason, and that is
// intentional, not an oversight this guard should flag). Teaching this AST scan to reconstruct
// runtime-assembled SQL is not soundly derivable from source text (the whole point of a static
// guard), so the fix is to state the blind spot rather than to fake coverage of it. If
// `DataExportRowQuery` or `dataExportTables` ever moves to a shape a static scan CAN see (e.g. a
// literal query per table), this comment block should be deleted rather than carried forward.
var configTombstoneTables = []string{
	"savings_goals",
	"boosts",
	"budgets",
	"budget_categories",
}

// sqlKeywordsAfterTableName are the tokens that can legally follow a table name in this codebase's
// SQL and are NOT an alias. Without this list `FROM boosts WHERE ...` would be read as "table
// boosts aliased WHERE", and the guard would then demand `WHERE.deleted_at IS NULL`.
var sqlKeywordsAfterTableName = map[string]bool{
	"as": true, "on": true, "where": true, "set": true, "group": true, "order": true,
	"left": true, "right": true, "inner": true, "outer": true, "full": true, "cross": true,
	"join": true, "using": true, "limit": true, "offset": true, "having": true, "union": true,
	"returning": true, "values": true, "and": true, "or": true, "with": true, "for": true,
	"natural": true, "lateral": true, "window": true, "except": true, "intersect": true,
}

// tombstoneReadRef finds one read reference to a tombstoned table and captures its alias, if any.
// FROM and JOIN are the obvious two; USING is here because `DELETE FROM child c USING parent p` is
// a read of `parent` that the FROM/JOIN pair misses entirely (savings_contribution_repository.go's
// DeleteContribution is exactly that shape).
func tombstoneReadRefRegexp(tables []string) *regexp.Regexp {
	return regexp.MustCompile(
		`(?is)(?:\b(?:FROM|JOIN|USING)|(,))\s+(` + strings.Join(tables, "|") +
			`)\b(?:\s+(?:AS\s+)?([A-Za-z_][A-Za-z0-9_]*))?`)
}

// tombstoneUnqualifiedPredicate matches `deleted_at IS NULL` that is NOT qualified by any alias —
// the form an unaliased single-table read uses. The leading `[^.\w]` guard is the entire point:
// without it, `txn.deleted_at IS NULL` would satisfy a check meant for a different table.
var tombstoneUnqualifiedPredicate = regexp.MustCompile(`(?is)(^|[^.\w])deleted_at\s+IS\s+NULL`)

// tombstoneExemptToken matches an exemption and captures the references it covers:
// `tombstone-exempt(<table>[ <alias>], ...): <reason>`. The `\s*` before the paren is deliberate:
// `tombstone-exempt (transactions):` with one stray space used to match NEITHER this pattern nor
// the bare-form detector, so it excused nothing and was reported by nothing.
var tombstoneExemptToken = namedExemptToken("tombstone-exempt")

// namedExemptToken builds the matcher for a `<name>(<targets>): <reason>` exemption. Shared,
// because a second guard (encrypted_column_literal_guard_test.go) needs the identical shape and a
// second hand-rolled copy is how the two drift.
func namedExemptToken(name string) *regexp.Regexp {
	return regexp.MustCompile(regexp.QuoteMeta(name) + `\s*\(([^)]*)\)\s*:`)
}

// namedExemptTargets returns the lowercased, whitespace-collapsed targets named by every
// `<name>(...)` token in text.
func namedExemptTargets(text string, token *regexp.Regexp) map[string]bool {
	targets := map[string]bool{}
	for _, m := range token.FindAllStringSubmatch(text, -1) {
		for _, t := range strings.Split(m[1], ",") {
			if key := strings.Join(strings.Fields(strings.ToLower(t)), " "); key != "" {
				targets[key] = true
			}
		}
	}
	return targets
}

// namedExemptIsBare reports whether text uses `name` in any form OTHER than the parenthesised one.
// Built by deletion rather than by a second pattern, so `name:`, `name (x)` with no colon and a
// bare `name` are all caught without three regexps that can disagree.
func namedExemptIsBare(text, name string, token *regexp.Regexp) bool {
	return strings.Contains(token.ReplaceAllString(text, ""), name)
}

// tombstoneReadOffenders returns, for one SQL statement, the read references to `tables` that carry
// no tombstone predicate OF THEIR OWN.
//
// THIS IS PER-REFERENCE AND ALIAS-QUALIFIED, and that is not a refinement — it is the whole
// guarantee. An earlier version of both this guard and task 7's asked only whether the string
// "deleted_at IS NULL" appeared ANYWHERE in the statement. That is satisfied by any other table's
// predicate, so a statement that joins `boosts` unfiltered alongside
// `JOIN transactions txn ... AND txn.deleted_at IS NULL` passed while being exactly the bug the
// guard exists to catch. It was not hypothetical: transfer_suggestion_repository.go's
// GetByUserIDStatuses was live on this branch, green under the loose guard, and RED under this one.
//
// THE EXEMPTION IS PER REFERENCE TOO (#1569 T8). It used to be a bare `tombstone-exempt:` token
// that short-circuited the whole statement on the theory that the cases it covers are
// statement-level facts. They are not: a statement grows. Injecting a second, wholly unfiltered
// `transactions unguarded_probe` into UpdateCategory — whose token is about resolving a CATEGORY
// name — left the transaction guard green. The token must now name what it excuses:
// `tombstone-exempt(<table>[ <alias>], ...): <reason>`. See
// TestTombstoneExemptionIsPerReference and TestEveryTombstoneExemptTokenNamesItsReference in
// tombstone_exempt_scope_guard_test.go.
func tombstoneReadOffenders(sql string, tables []string, deleteRef *regexp.Regexp) []string {
	exempt := tombstoneExemptRefs(sql)
	// Exemption tokens live in comments, so they are read from the raw text; EVERYTHING ELSE is
	// read from the comment-stripped text. Prose is not SQL: `-- Adding prior.deleted_at IS NULL
	// here would narrow the UPDATE` was satisfying this guard's predicate check on two live
	// statements, so a comment explaining why a predicate is ABSENT was accepted as the predicate.
	code := stripSQLComments(sql)
	// A DELETE's own FROM is a write, not a read.
	read := deleteRef.ReplaceAllString(code, "")

	var offenders []string
	for _, m := range tombstoneReadRefRegexp(tables).FindAllStringSubmatch(read, -1) {
		comma, table, alias := m[1] == ",", m[2], m[3]
		if sqlKeywordsAfterTableName[strings.ToLower(alias)] {
			alias = ""
		}
		// A comma-introduced reference is only trusted when it carries an alias. `FROM a, b p` is a
		// real join list that FROM/JOIN/USING alone misses (DeleteBySourceAndExternalID's
		// `FROM tombstoned s, transactions prior` was invisible for exactly this reason), but a bare
		// `, transactions` also matches a qualified column (`EXCLUDED.x, transactions.y`) and plain
		// English in an email template. Requiring the alias keeps the join list and drops both.
		if comma && alias == "" {
			continue
		}
		var ok bool
		if alias != "" {
			ok = regexp.MustCompile(`(?is)\b` + regexp.QuoteMeta(alias) + `\.deleted_at\s+IS\s+NULL`).MatchString(code)
		} else {
			ok = tombstoneUnqualifiedPredicate.MatchString(code) ||
				regexp.MustCompile(`(?is)\b`+regexp.QuoteMeta(table)+`\.deleted_at\s+IS\s+NULL`).MatchString(code)
		}
		if !ok {
			ref := table
			if alias != "" {
				ref = table + " " + alias
			}
			if exempt[strings.ToLower(ref)] {
				continue
			}
			offenders = append(offenders, ref)
		}
	}
	return offenders
}

// ---- Vacuity floors ---------------------------------------------------------------------------

// tombstoneRefsSeen counts the references to `tables` in one statement — read references and write
// targets alike, guarded or not, exempt or not. It is the corpus measure the three headline guards
// are calibrated against, and it exists because they had NO floor at all (#1569 whole-branch review
// I3).
//
// PROVEN FAIL-OPEN, which is why this is not a nicety: planting a real
// `DELETE FROM transactions WHERE user_id = $1` in linked_item_repository.go AND narrowing `root`
// to internal/queue left TestNoTransactionReadForgetsTheTombstoneFilter GREEN — passing with #1569's
// founding defect live in the tree. Every sibling guard in this package already fails closed on a
// collapsed corpus (encrypted_column_literal_guard_test.go, typographic_quotes_guard_test.go,
// no_matcher_guard_test.go, and TestNoDeadTombstoneExemption right here); these three did not.
//
// The floor is on REFERENCES rather than files because it then catches both halves: a narrowed or
// broken walk (fewer files, so fewer references) and a reference regexp that stops matching (same
// files, no references). A file count alone would miss the second.
func tombstoneRefsSeen(sql string, tables []string, deleteRef *regexp.Regexp) int {
	code := stripSQLComments(sql)
	n := len(tombstoneReadRefRegexp(tables).FindAllString(deleteRef.ReplaceAllString(code, ""), -1))
	return n + len(tombstoneWriteTargetRegexp(tables).FindAllString(code, -1))
}

// requireTombstoneCorpus fails the guard closed when its corpus has collapsed. `was` records what
// was measured when the floor was set, so a future reader can tell a deliberate shrink from a
// broken walk instead of guessing at the constant.
//
// ---- FLOOR POLICY (#1569 round 17 task 5) — read this before you change a number ----------------
//
// `minFiles`/`minRefs` are the durable artefact; `wasFiles`/`wasRefs` are only the measurement the
// floor was derived from and WILL drift upward as Go files are added. Drift in `was*` is expected
// and is not a defect. What matters is that `min*` still detects a MEANINGFUL shrink.
//
// The policy, applied identically to all four callers:
//
//	min = floor(0.9 * live)        // a corpus may shed up to 10%; more than that is "broken"
//
// So when a caller red-lines, DO NOT just lower the number to whatever makes it green. Re-measure
// the live corpus (each guard t.Logf()s it), then set `was* = live` and `min* = floor(0.9*live)`.
// That keeps the detection band constant instead of letting it widen one panicked edit at a time.
//
// Why 10% and not 0% or 30%: a floor set AT live is brittle — the next legitimate refactor that
// deletes one repository file red-lines a guard that is working correctly. A floor far below live
// is half-vacated: savings_goal_contributions sat at min=11 against 15 live references, so the
// corpus could have shed 27% of its references and still passed. Every real vacuity failure mode
// this floor exists for (a narrowed walk root, a reference regexp that stops matching, a guard
// pointed at the wrong directory) collapses the corpus by far more than 10%, so a 10% band is
// wide enough to absorb honest churn and tight enough to catch all of them.
func requireTombstoneCorpus(t *testing.T, guard string, files, refs, minFiles, minRefs, wasFiles, wasRefs int) {
	t.Helper()
	t.Logf("%s corpus: %d production Go files scanned, %d reference(s) to the audited table(s)",
		guard, files, refs)
	if files < minFiles || refs < minRefs {
		t.Fatalf("%s scanned %d file(s) and found %d reference(s); %d and %d were present when the "+
			"floor was set. The walk or the reference pattern is broken and a pass here would be "+
			"vacuous — this guard was measured passing with a real `DELETE FROM transactions` live "+
			"in the tree once the corpus was narrowed (#1569 I3)",
			guard, files, refs, wasFiles, wasRefs)
	}
}

func TestNoConfigReadForgetsTheTombstoneFilter(t *testing.T) {
	root := repoInternalRoot(t)

	// `\b` after the name stops `budgets` from matching inside `budget_categories` and
	// `savings_goals` from matching `savings_goal_contributions`.
	deleteRef := regexp.MustCompile(`(?is)\bDELETE\s+FROM\s+(` + strings.Join(configTombstoneTables, "|") + `)\b`)

	var offenders []string
	pfiles := goProductionFiles(t, root, true, false)
	filesScanned := len(pfiles)
	var refsSeen int

	// #1569 round 14 task 2: literalUnitsIn partitions every string literal in the file by its
	// argument-aware assembly owner (see guard_ast_shared_test.go) and returns each unit exactly
	// once, so — unlike the per-file ast.Inspect this guard used to run — there is no double-count
	// to guard against by stopping the descent early.
	for _, pf := range pfiles {
		for _, u := range literalUnitsIn(pf.file) {
			line := pf.fset.Position(u.pos).Line
			sql := u.sql
			refsSeen += tombstoneRefsSeen(sql, configTombstoneTables, deleteRef)

			bad := tombstoneReadOffenders(sql, configTombstoneTables, deleteRef)
			bad = append(bad, tombstoneWriteOffenders(sql, configTombstoneTables)...)
			for _, ref := range bad {
				offenders = append(offenders, pf.rel+":"+strconv.Itoa(line)+" ("+ref+")")
			}
		}
	}

	// #1685/#1686 T29: re-measured live corpus is 460 files / 105 refs (was 374/88), so the old
	// floor's band had widened to 25% — half-vacated per the FLOOR POLICY above. Recalibrated to
	// floor(0.9*live): 414/94, with was*=live. This RAISES the floor; nothing is lowered.
	requireTombstoneCorpus(t, "TestNoConfigReadForgetsTheTombstoneFilter", filesScanned, refsSeen, 414, 94, 460, 105)

	if len(offenders) > 0 {
		t.Fatalf(
			"%d read reference(s) to %v carry no tombstone predicate OF THEIR OWN (#1569).\n"+
				"Add `AND <that reference's alias>.deleted_at IS NULL` — another table's predicate in\n"+
				"the same statement does NOT count — or, if the predicate is genuinely supplied\n"+
				"elsewhere, an SQL comment naming THIS reference inside the literal:\n"+
				"  -- tombstone-exempt(<table>[ <alias>]): <why>            (excuses a READ reference)\n"+
				"  -- tombstone-exempt(write:<table>[ <alias>]): <why>      (excuses an UPDATE/DELETE\n"+
				"     TARGET; the two key spaces are separate, and an offender reported as\n"+
				"     `write:...` is only excused by the `write:` form)\n%s",
			len(offenders), configTombstoneTables, strings.Join(offenders, "\n  "))
	}
}

// tombstoneExemptRefs returns the lowercased `table` / `table alias` keys that the statement's
// `tombstone-exempt(...)` tokens excuse. A bare `tombstone-exempt:` yields nothing on purpose —
// see TestEveryTombstoneExemptTokenNamesItsReference, which reports it rather than letting it
// quietly blanket the statement.
func tombstoneExemptRefs(sql string) map[string]bool {
	return namedExemptTargets(sql, tombstoneExemptToken)
}

// tombstoneWriteTargetRegexp finds the TARGET of an UPDATE or a DELETE and captures its alias.
// Plan lines 489-493; #1569 T8 fix round 1.
//
// A read that forgets the tombstone filter shows the user a row they deleted. A WRITE that forgets
// it does something different and in one direction worse: it mutates rows the user believes are
// gone. The guard never looked at a write target at all — DELETE's FROM was explicitly stripped as
// "a write, not a read", and an UPDATE's target matched no FROM/JOIN/USING — so
// `UPDATE transactions SET deleted_at = NOW()` yielded zero offenders and a note in
// transaction_repository.go recorded the hole rather than closing it.
func tombstoneWriteTargetRegexp(tables []string) *regexp.Regexp {
	return regexp.MustCompile(
		`(?is)\b(?:UPDATE|DELETE\s+FROM)\s+(?:ONLY\s+)?(` + strings.Join(tables, "|") +
			`)\b(?:\s+(?:AS\s+)?([A-Za-z_][A-Za-z0-9_]*))?`)
}

// tombstoneWriteOffenders returns the write targets in one statement that carry no tombstone
// predicate of their own and no exemption naming them. The reported reference is prefixed `write:`
// so a failure cannot be mistaken for the read rule, which has a different remedy.
//
// THE EXEMPTION KEY IS `write:<table>[ <alias>]`, IN ITS OWN NAMESPACE (#1569 whole-branch review
// I4). It used to be the same key the read rule consults, so one unaliased
// `tombstone-exempt(transactions)` excused the write target AND every unfiltered read reference to
// the same table in that statement, in both directions, no matter how narrow the reason underneath
// it was. Measured: injecting a wholly unfiltered `EXISTS (SELECT 1 FROM transactions ...)` read
// into the tombstoning UPDATE in transaction_repository.go — whose token's reason is explicitly
// about the WRITE target — left all three tombstone guards green. T8's per-reference fix closed the
// ALIASED case only incidentally (the two roles happened to use different aliases); twelve live
// tokens were unaliased and blanketed both. The remedies differ, so considering one role is not
// considering the other. Constructed both ways in TestTombstoneExemptionIsPerReference.
func tombstoneWriteOffenders(sql string, tables []string) []string {
	exempt := tombstoneExemptRefs(sql)
	code := stripSQLComments(sql)

	var offenders []string
	for _, m := range tombstoneWriteTargetRegexp(tables).FindAllStringSubmatch(code, -1) {
		table, alias := m[1], m[2]
		if sqlKeywordsAfterTableName[strings.ToLower(alias)] {
			alias = ""
		}
		ref := table
		if alias != "" {
			ref = table + " " + alias
		}
		if exempt["write:"+strings.ToLower(ref)] {
			continue
		}
		var ok bool
		if alias != "" {
			ok = regexp.MustCompile(`(?is)\b` + regexp.QuoteMeta(alias) + `\.deleted_at\s+IS\s+NULL`).MatchString(code)
		} else {
			ok = tombstoneUnqualifiedPredicate.MatchString(code) ||
				regexp.MustCompile(`(?is)\b`+regexp.QuoteMeta(table)+`\.deleted_at\s+IS\s+NULL`).MatchString(code)
		}
		if !ok {
			offenders = append(offenders, "write:"+ref)
		}
	}
	return offenders
}

// sqlLineComment and sqlBlockComment strip SQL comments so that PROSE ABOUT a predicate is never
// mistaken for the predicate. Both live statements that motivated this contained the sentence
// "Adding prior.deleted_at IS NULL here would narrow WHICH ROWS THE UPDATE AFFECTS" inside a
// `--` comment, and that sentence was satisfying the check for the `transactions prior` reference
// four lines below it.
var (
	sqlLineComment  = regexp.MustCompile(`--[^\n]*`)
	sqlBlockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
)

func stripSQLComments(sql string) string {
	return sqlLineComment.ReplaceAllString(sqlBlockComment.ReplaceAllString(sql, " "), " ")
}
