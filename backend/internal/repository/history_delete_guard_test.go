package repository

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

// Issue #1569 Task 19 — Guard G2: every hard DELETE of history is on a list, with a reason.
//
// G1 stops the SCHEMA from destroying history behind a parent delete. G2 stops the CODE from doing
// it in the open. They are different failure modes and neither implies the other: a `DELETE FROM
// holding_snapshots` in Go needs no foreign key at all.
//
// THE RULE. In non-test Go under backend/internal, every `DELETE FROM <history-bearing table>` and
// every `TRUNCATE <history-bearing table>` must appear in `allowedHistoryDeletes`, keyed by the
// function that issues it, with the reason. There is deliberately NO in-comment escape hatch of the
// kind the tombstone READ guards use. A read that forgets a filter is a slip and there are ~60 of
// them; a hard delete of financial history is a decision, there are four of them (#1569 round 12
// task 2 removed DeleteManagedForTransaction's entry — it is no longer a hard delete), and every one
// should be visible in a single list that a reviewer can read in ten seconds.
//
// THE GAP IN THE FUNCTION KEY, precisely. The key is (function, TABLE), so a second delete of a
// DIFFERENT history table inside an allowlisted function IS caught — that is a new key. What used
// to slip through was widening what an existing, already-blessed purge destroys: a second delete of
// the SAME table in the same function, or — far easier to do by accident — dropping a leg from the
// predicate of the delete that is already there. Since #1569 T7.5 each entry names the SQL
// fragments it blesses (see historyDeleteAllowance), and
// TestEveryAllowlistedHistoryDeleteStillCarriesItsPredicate requires EVERY statement under a key to
// carry all of them — so both shapes now fail here, with no database attached. For PurgeAccount the
// blast radius is additionally covered behaviourally, by
// TestHistoryRetentionRoundTrip_PurgeDestroysOnlyWhatItNames, which seeds a sibling institution and
// asserts the purge does not touch it.
//
// SCOPE: backend/internal only. A delete written in cmd/ would not be seen; today the only match
// there is in a test file, so there is no live gap, but the boundary is a walk of repoInternalRoot
// and not a decision that cmd/ is safe.
//
// KEYED BY FUNCTION, NOT BY LINE. Line numbers rot — this plan re-pinned one twice — and a
// file-level key would let a second, unrelated delete slip into an already-listed file. The
// function name is also the only part of the key that a reader can evaluate: `PurgeAccount` and
// `DeleteManagedForTransaction` say what they are; `account_repository.go:1031` does not.
//
// WHAT IS NOT HERE. Tombstoning (`UPDATE ... SET deleted_at = now()`) is the normal path and is not
// a delete. `DELETE FROM` a non-history table is none of this guard's business. And migrations are
// out of scope: a migration that drops history is caught by review and by G1's schema check, not by
// a scan of Go source.
//
// Mutation-checked, twice over: the constructed statement in
// TestHistoryDeleteGuard_FlagsAnUnallowlistedDelete, and a real one — a `DELETE FROM
// rule_execution_history` temporarily added to a live repository method, which turns
// TestNoUnallowlistedHistoryDelete red naming that method. See task-19-report.md.
//
// ⚠ AND A THIRD TIME, WHICH IS THE ONE WORTH READING (#1569 T8.2). T7.5's predicate pin was
// mutation-checked by dropping a leg from the ONLY occurrence of that text in the file — so it went
// red by luck of there being no second one. Both mechanisms that decide WHICH TEXT a statement is
// checked against were too generous: a slice ran past its own terminator to the next DELETE, and
// every literal in a function was joined before scanning. Either one lets a widened destruction
// keep its blessing on the strength of text somewhere else entirely. Both are now bounded (see
// statementBoundaryRegexp and literalUnitsInFunc), and the real corpus was re-mutated against the
// FIXED guard: dropping `AND deleted_at IS NOT NULL` from PurgeAccount's savings_goals delete while
// leaving that text in a sibling error string is GREEN under the old guard and RED under this one.
//
// The lesson generalises past this file: a passing mutant proves the guard catches THAT mutation,
// not that the guard is sound. Mutate the mechanism, not just the instance.

// historyDeleteAllowance is one blessing: WHY this function may hard-delete this history table,
// and WHICH PREDICATE the blessing is scoped to.
//
// ⚠ WHY THE PREDICATE IS PART OF THE ENTRY (#1569 T7.5, user ruling). Until this change an entry
// was a bare reason string keyed by (function, TABLE), so dropping `AND deleted_at IS NOT NULL`
// from PurgeAccount's savings_goals delete — or `AND c.managed_by_reconciliation = true` from
// DeleteManagedForTransaction — kept the existing blessing while widening the destruction. The
// scopes were held only by behavioural integration tests, and those SKIP without DATABASE_URL, so
// a plain `go test ./...` proved nothing about them. `predicate` closes that: an entry cannot
// survive the SQL beneath it widening, because the fragments it names must still be there.
//
// WHAT `predicate` CLAIMS, PRECISELY — and what it does not. Every fragment must appear, as a
// substring, in the normalised text of EVERY statement the key matches. Normalisation strips SQL
// comments (`--` and `/* */`) and collapses runs of whitespace, then upper-cases, so re-wrapping a
// query, re-indenting it, or writing a comment inside it cannot flip a verdict. It does NOT parse
// SQL and it does not know what the predicate MEANS: it can only tell you the text is still there.
// Two consequences worth stating out loud:
//
//   - Polarity must be named. `deleted_at IS NULL` flipped to `IS NOT NULL` is caught only because
//     the fragment spells out which one it blesses. Write the whole leg, never just the column.
//   - A fragment is not a proof of narrowness. `WHERE c.id = $1` can still sit next to a new `OR`.
//     The behavioural integration tests remain the authority on what the statement DOES; this is
//     the cheap, always-runs half that stops a leg from silently disappearing.
type historyDeleteAllowance struct {
	// reason is why this destruction is allowed at all. It describes intent and is NOT checked
	// against the SQL — only `predicate` is.
	reason string
	// predicate is the set of SQL fragments this blessing is scoped to. Must be non-empty; an
	// entry with no fragments would be a blessing of `DELETE FROM <table>`, full stop.
	predicate []string
}

// allowedHistoryDeletes maps "<path under internal/>#<function>|<table>" to the blessing. One entry
// per (function, table) pair.
var allowedHistoryDeletes = map[string]historyDeleteAllowance{
	// The account purge. `PurgeAccount` refuses to run on a live account and writes an audit row in
	// the same transaction; it is the user explicitly asking for one account's data to be destroyed.
	// The three cascade edges in allowedHistoryCascades are the schema half of the same decision.
	"repository/account_repository.go#PurgeAccount|balance_snapshots": {
		reason:    "explicit account purge: balance_snapshots has ON DELETE SET NULL from accounts, so the cascade would orphan rather than remove them",
		predicate: []string{"WHERE account_id = $1"},
	},
	"repository/account_repository.go#PurgeAccount|accounts": {
		reason:    "explicit account purge: this IS the purge",
		predicate: []string{"WHERE id = $1 AND user_id = $2"},
	},
	// Scoped to `deleted_at IS NOT NULL` — the user has ALREADY deleted the goal, which is exactly
	// PurgeGoal's own precondition. A live goal is not matched and still blocks the purge via
	// migration 000150's RESTRICT. Without this the purge was permanently unreachable for any
	// account that had ever had a goal: the tombstone leaves account_id set, so "delete the goal
	// first, then try again" could never be satisfied (#1569 task 21, defect C1).
	//
	// That scope is now pinned in `predicate` as well as by behaviour. The behavioural pair is
	// still the authority on what it MEANS:
	// TestAccountRepository_PurgeAccount_Integration_TombstonedSavingsGoalDoesNotBlock (a deleted
	// goal must not block) and ..._RefusesWhileSavingsGoalReferencesAccount (a live one must).
	// Inverting the predicate turns those red — and, since #1569 T7.5, turns
	// TestEveryAllowlistedHistoryDeleteStillCarriesItsPredicate red without a database.
	"repository/account_repository.go#PurgeAccount|savings_goals": {
		reason:    "explicit account purge, tombstoned goals only: the user already deleted the goal, which is PurgeGoal's precondition; a live goal still blocks via RESTRICT",
		predicate: []string{"account_id = $1", "user_id = $2", "deleted_at IS NOT NULL"},
	},

	// The linked-item purge. Same shape, one level up: the user disconnects AND purges an
	// institution. A plain disconnect soft-removes and keeps everything.
	"repository/linked_item_repository.go#PurgeLinkedItem|balance_snapshots": {
		reason:    "explicit institution purge, same reason as PurgeAccount",
		predicate: []string{"WHERE account_id IN (SELECT id FROM accounts WHERE linked_item_id = $1)"},
	},
	"repository/linked_item_repository.go#PurgeLinkedItem|savings_goals": {
		reason:    "explicit institution purge, tombstoned goals only: same scope and same reason as PurgeAccount's entry above",
		predicate: []string{"user_id = $2", "deleted_at IS NOT NULL", "account_id IN (SELECT id FROM accounts WHERE linked_item_id = $1)"},
	},

	// The savings-goal purge. Distinct from the repository's `Delete`
	// (savings_goal_repository.go:405), which TOMBSTONES (#1569 task 8). ⚠ `DeleteGoal` is the
	// SERVICE verb (services/savings_goal_service.go:499); there is no repository method by that
	// name, and this reason used to claim there was.
	//
	// The `deleted_at IS NOT NULL` precondition is deliberately NOT in this statement — the
	// SELECT ... FOR UPDATE above it already asserted it and refused a live goal, and re-stating it
	// here would only let the two predicates disagree. So `predicate` pins the ownership legs only;
	// the precondition is guarded where it lives.
	"repository/savings_goal_repository.go#PurgeGoal|savings_goals": {
		reason:    "explicit goal purge; the ordinary Delete path on this repository (service verb: DeleteGoal) tombstones instead",
		predicate: []string{"WHERE id = $1 AND user_id = $2"},
	},

	// Contributions.
	// ⚠ RE-DERIVED (#1569 T7.5). This reason used to say "one contribution THEY ENTERED", which is
	// the same true-of-most-rows-asserted-of-all error the ⚠ paragraph below retracts for the
	// sibling entry. The predicate (savings_contribution_repository.go:749-766) keys on `c.id`
	// alone — no `source` leg, no `managed_by_reconciliation` leg — so a row the RECONCILER created
	// is reachable by it too. "One contribution" and "explicit request" survive; "they entered"
	// does not.
	"repository/savings_contribution_repository.go#DeleteContribution|savings_goal_contributions": {
		reason:    "the user deleting one named contribution on a live goal of theirs — the narrowest possible explicit request. NOT necessarily a row they entered: the predicate keys on c.id alone, so a reconciler-created row is reachable by it",
		predicate: []string{"c.id = $1", "c.savings_goal_id = g.id", "g.user_id = $2", "g.deleted_at IS NULL"},
	},
	// ⚠ REMOVED (#1569 round 12 task 2), NOT REWORDED. DeleteManagedForTransaction used to be a real
	// `DELETE FROM savings_goal_contributions` and carried a blessing here. An independent Codex
	// audit of this branch's founding principle ("financial history is retained unless the user
	// EXPLICITLY asks for it to be destroyed") named this exact call site as the headline finding
	// that the principle was NOT satisfied: Plaid reporting a transaction as `removed` is the
	// provider acting, not the user asking for a contribution to be destroyed ("the provider is not
	// the user"). The method now UPDATEs deleted_at/deleted_reason instead — migration 000180 — so
	// there is no hard DELETE left for this guard to see, and no entry belongs here. The old
	// reason ("scoped to a LIVE goal...") justified WHY the destruction was safe, which was never
	// the same question as WHETHER the user had authorised it; Codex's sharpest point was that a
	// business-correctness reason is not a user-authorisation control. See
	// TestSavingsContributionRepository_DeleteManagedForTransaction and
	// TestDeleteManagedForTransaction_DeletedGoalLedgerSurvives_Integration for the new behaviour.
}

// historyDeleteRefRegexp matches a hard delete of one of the named tables. Alternatives are sorted
// longest-first so that `savings_goals` cannot claim the prefix of `savings_goal_contributions`.
//
// ⚠ WHAT THE TABLE MATCHER ACCEPTS (#1569 T7-FIX, finding E). It used to require a BARE identifier,
// so `DELETE FROM public.transactions` and `TRUNCATE TABLE "transactions"` were both invisible to
// G2 and to G4 — a one-token edit that turned the guard off. It now accepts, in any combination:
//
//   - an optional schema qualifier (`public.`, `"public".`, or any other schema — a delete routed
//     through a second schema is still a delete of that table's name and is still reported);
//   - an optional pair of double quotes around either identifier.
//
// ⚠ WHAT IT DOES AND DOES NOT MISS — AND THE EARLIER VERSION OF THIS PARAGRAPH UNDERSTATED THE
// COVERAGE (#1569 T8.5). It used to say that a name split across a comment
// (`public /* x */ . transactions`) "is still missed, and the guard makes no claim about those".
// That is FALSE, and understating a guard is the same defect as overstating it: a reader either
// writes a redundant guard or works around a hole that is not there. Both call sites blank or strip
// comments BEFORE this matcher runs — blankSQLComments (history_delete_sql_guard_test.go, comment
// bytes → spaces, byte offsets preserved) and normaliseSQL (below) — so by the time the regexp sees
// the text the comment is whitespace, and `(?:"?[a-z_][a-z0-9_]*"?\s*\.\s*)?` already tolerates
// whitespace on both sides of the dot. That example MATCHES. So does `DELETE -- c\n FROM
// transactions`. Measured against this function, not reasoned about.
//
// The one real miss is a comment INSIDE the identifier — `DELETE FROM trans/**/actions` — which
// blanking turns into `trans    actions`, two tokens the matcher cannot rejoin. Nothing in the tree
// writes that, and catching it would mean parsing SQL rather than scanning it. Do not extend this
// list with a form you have not run through the matcher first.
func historyDeleteRefRegexp(tables map[string]string) *regexp.Regexp {
	names := make([]string, 0, len(tables))
	for t := range tables {
		names = append(names, t)
	}
	sort.Slice(names, func(i, j int) bool {
		if len(names[i]) != len(names[j]) {
			return len(names[i]) > len(names[j])
		}
		return names[i] < names[j]
	})
	return regexp.MustCompile(`(?is)\b(?:DELETE\s+FROM|TRUNCATE(?:\s+TABLE)?)\s+(?:ONLY\s+)?` +
		`(?:"?[a-z_][a-z0-9_]*"?\s*\.\s*)?"?(` + strings.Join(names, "|") + `)(?:"|\b)`)
}

// sqlBlockCommentRegexp, sqlLineCommentRegexp and sqlWhitespaceRegexp normalise SQL so that
// re-wrapping a query, re-indenting it, or writing a comment inside it cannot flip a predicate
// verdict. Comments are stripped BEFORE whitespace is collapsed, and stripped at all because four
// of the eight blessed statements carry a `-- tombstone-exempt(...)` comment that quotes the very
// predicate legs the guard is looking for — leaving them in would let a comment satisfy the check
// for SQL that no longer contains it.
var (
	sqlBlockCommentRegexp = regexp.MustCompile(`(?s)/\*.*?\*/`)
	sqlLineCommentRegexp  = regexp.MustCompile(`--[^\n]*`)
	sqlWhitespaceRegexp   = regexp.MustCompile(`\s+`)
	// sqlStringLiteralRegexp and sqlDollarQuoteRegexp are #1569 T8-FIX, finding D. This guard scans
	// TEXT, not parsed SQL, so anything a user or a column default can put inside quotes was being
	// read as SQL structure. All three consequences were real and none had a production instance:
	//
	//   - `WHERE note = 'DELETE FROM savings_goal_contributions'` reported an unallowlisted delete
	//     of a table nothing deletes — the direction that gets a guard deleted by the next engineer;
	//   - a `;` inside a literal truncated a real delete's slice, so a predicate fragment past it
	//     read as missing (a false failure again — the previous note here claimed truncation "can
	//     only make the predicate check STRICTER, never bless a widening", which is true and was
	//     beside the point: stricter here means WRONG, not safe);
	//   - a fragment written inside a literal (`AND label = 'deleted_at IS NOT NULL'`) satisfied the
	//     predicate pin for a DELETE that does not carry it.
	//
	// Blanking them is exactly proportionate to a text scanner; writing a SQL parser is not. The
	// standard-SQL escape `''` is matched as part of the literal for tidiness, NOT because the guard
	// depends on it: an escaped literal carries an EVEN number of quotes, so a naive `'[^']*'` splits
	// `'it''s'` into two adjacent matches and stays in phase across it. Measured, not reasoned — see
	// the note in TestHistoryDeleteStatements_IsNotFooledByAQuotedLiteral.
	//
	// ⚠ THE ONE ORDERING THIS DEPENDS ON: comments are stripped BEFORE literals are blanked, so an
	// apostrophe in a comment cannot open a literal. The converse — a `--` INSIDE a literal, which
	// comment-stripping would eat first — is the remaining miss. Nothing in the corpus writes one,
	// and it can only shorten a slice, never lend a predicate.
	sqlStringLiteralRegexp = regexp.MustCompile(`'(?:[^']|'')*'`)
	sqlDollarQuoteRegexp   = regexp.MustCompile(`(?s)\$\$.*?\$\$`)
	// statementBoundaryRegexp is the STATEMENT BOUNDARY, not the table matcher. Two things end a
	// delete's slice, whichever comes first:
	//
	//   - the start of another delete of ANY table, so that a `DELETE FROM holdings` sitting
	//     between two history deletes still ends the preceding slice;
	//   - a semicolon, the actual statement terminator.
	//
	// ⚠ THE SEMICOLON IS #1569 T8.2, AND WITHOUT IT THE PREDICATE PIN COULD BE BYPASSED. A slice
	// that ran to the next DELETE swallowed every following statement in the same SQL string, so a
	// `SELECT ... WHERE deleted_at IS NOT NULL` after an allowlisted purge satisfied that purge's
	// predicate fragment for SQL the DELETE no longer contained — T7.5's blessing kept, its scope
	// gone. Pinned by TestHistoryDeleteStatements_SplitsAtTheStatementTerminator.
	//
	// Comments are stripped by normaliseSQL BEFORE this runs, so a `;` inside a comment is not a
	// boundary. Neither is a `;` inside a string or dollar-quoted literal: normaliseSQL blanks those
	// too (#1569 T8-FIX, finding D — see sqlStringLiteralRegexp). Pinned by
	// TestHistoryDeleteStatements_IsNotFooledByAQuotedLiteral, in both directions: a literal's
	// semicolon must not end a statement and a real one still must.
	statementBoundaryRegexp = regexp.MustCompile(`\b(?:DELETE FROM|TRUNCATE)\b|;`)
)

// normaliseSQL strips SQL comments, blanks quoted literals, collapses whitespace and upper-cases.
// Order matters: comments first (an apostrophe in a comment must not open a literal), literals
// second (their contents are DATA and carry no SQL structure), whitespace last.
func normaliseSQL(sql string) string {
	sql = sqlBlockCommentRegexp.ReplaceAllString(sql, " ")
	sql = sqlLineCommentRegexp.ReplaceAllString(sql, " ")
	sql = sqlDollarQuoteRegexp.ReplaceAllString(sql, " ")
	sql = sqlStringLiteralRegexp.ReplaceAllString(sql, " ")
	sql = sqlWhitespaceRegexp.ReplaceAllString(sql, " ")
	return strings.ToUpper(strings.TrimSpace(sql))
}

// historyDeleteStatements returns, per history table, the normalised text of each hard delete of it
// found in sql. A "statement" runs from the DELETE/TRUNCATE keyword to the next statement boundary
// — another delete, or a semicolon — or to the end of the input.
//
// ⚠ IT USED TO RUN TO THE NEXT DELETE ONLY, and the note here called that "deliberately generous at
// the tail, because over-reading can only make the predicate check MORE permissive, never produce a
// false failure on legitimate SQL". The first half was true and the second half was the whole
// problem: MORE PERMISSIVE is exactly how a widened destruction gets blessed. See
// statementBoundaryRegexp.
func historyDeleteStatements(sql string, tables map[string]string) map[string][]string {
	norm := normaliseSQL(sql)
	out := map[string][]string{}
	for _, m := range historyDeleteRefRegexp(tables).FindAllStringSubmatchIndex(norm, -1) {
		table := strings.ToLower(norm[m[2]:m[3]])
		end := len(norm)
		if next := statementBoundaryRegexp.FindStringIndex(norm[m[1]:]); next != nil {
			end = m[1] + next[0]
		}
		out[table] = append(out[table], norm[m[0]:end])
	}
	return out
}

// historyDeleteSites returns the history tables hard-deleted by one SQL string.
func historyDeleteSites(sql string, tables map[string]string) []string {
	var out []string
	for table := range historyDeleteStatements(sql, tables) {
		out = append(out, table)
	}
	sort.Strings(out)
	return out
}

// historyDeleteSiteFound is what the scan saw for one "<rel>#<func>|<table>" key: the line(s) the
// enclosing function starts on, and the normalised text of each delete statement.
type historyDeleteSiteFound struct {
	lines      []string
	statements []string
}

// sqlAssemblyUnit reports whether n is a statement-sized unit of SQL assembly: the node whose
// string literals may legitimately be halves of ONE query (`A` + helper + `B`). It is the same unit
// the tombstone read-guards use (transaction_tombstone_guard_test.go), deliberately — one notion of
// "what counts as one query", not two.
//
// ⚠ IT IS NOT THE WHOLE STORY, AND SAYING SO WAS #1569 T8-FIX FINDING E. An ExprStmt is one CALL,
// and a call carries SQL PARAMETER VALUES beside its query. literalUnitsInFunc therefore stops at a
// call ARGUMENT before it reaches the enclosing statement; this predicate is what it falls back to.
// The previous note claimed the unit covered "a fmt.Sprintf whose format string and fragments are
// separate literals" — that was the hole, not a feature: a format argument and a value argument are
// different arguments and must not lend each other predicates.
func sqlAssemblyUnit(n ast.Node) bool {
	switch n.(type) {
	case *ast.AssignStmt, *ast.ValueSpec, *ast.ReturnStmt, *ast.ExprStmt:
		return true
	}
	return false
}

// literalUnitsInFunc partitions every string literal in body by its NEAREST enclosing assembly
// unit, and returns one joined SQL string per unit, ordered by position.
//
// ⚠ WHY A PARTITION AND NOT A JOIN (#1569 T8.2, Item B). scanHistoryDeletes used to concatenate
// EVERY literal in a function before scanning, which meant an allowlisted DELETE was checked
// against text it does not contain: a help string, an error message, or an unrelated second query
// in the same function could supply a predicate fragment the DELETE had lost, and the T7.5
// predicate pin passed while the destruction widened. Pinned by
// TestHistoryDeleteScan_DoesNotLendAPredicateFromAnotherLiteral, in both directions — the second
// subtest is the one that matters for keeping this guard alive, because a guard that fires on a
// legitimately concatenated query gets deleted by the next engineer.
//
// ⚠ IT FAILS OPEN NOWHERE. A literal with no enclosing unit — a `case "..."` expression, a switch
// tag, an `if` condition — is NOT dropped; it becomes a unit of its own, so a delete written there
// is still seen. What it does not get is a sibling's text.
//
// ⚠ THE BOUNDARY IS THE ARGUMENT, NOT THE CALL (#1569 T8-FIX, finding E). The nearest enclosing
// CALL ARGUMENT wins over the enclosing statement, so
// `db.Exec(ctx, "DELETE FROM x WHERE user_id = $1", "help: deleted_at IS NOT NULL")` gives three
// units, not one, and the unrelated parameter can no longer satisfy the delete's predicate pin. A
// concatenation INSIDE one argument is still one unit. Pinned in both directions by
// TestHistoryDeleteScan_DoesNotLendAPredicateFromASiblingArgument.
// assemblyOwner returns the node whose literals lit may be joined with: the nearest enclosing CALL
// ARGUMENT, else the nearest enclosing assembly statement, else lit itself. stack holds lit's
// ancestors, outermost first.
func assemblyOwner(lit ast.Node, stack []ast.Node) ast.Node {
	child := lit
	for i := len(stack) - 1; i >= 0; i-- {
		if call, ok := stack[i].(*ast.CallExpr); ok {
			for _, arg := range call.Args {
				if ast.Node(arg) == child {
					return child
				}
			}
		}
		if sqlAssemblyUnit(stack[i]) {
			return stack[i]
		}
		child = stack[i]
	}
	return lit
}

func literalUnitsInFunc(fset *token.FileSet, body *ast.BlockStmt) []string {
	type group struct {
		pos   token.Pos
		parts []string
	}
	groups := map[ast.Node]*group{}
	var stack []ast.Node

	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if str, e := strconv.Unquote(lit.Value); e == nil {
				owner := assemblyOwner(lit, stack)
				if groups[owner] == nil {
					groups[owner] = &group{pos: owner.Pos()}
				}
				groups[owner].parts = append(groups[owner].parts, str)
			}
		}
		stack = append(stack, n)
		return true
	})

	ordered := make([]*group, 0, len(groups))
	for _, g := range groups {
		ordered = append(ordered, g)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].pos < ordered[j].pos })

	out := make([]string, 0, len(ordered))
	for _, g := range ordered {
		out = append(out, strings.Join(g.parts, "\n"))
	}
	return out
}

// scanHistoryDeletes walks non-test Go under root and returns every "<rel>#<func>|<table>" key found.
// Only string LITERALS are inspected, so prose in a comment that quotes `DELETE FROM budgets` — and
// four files do exactly that, explaining why they no longer do it — is correctly ignored. Literals
// are grouped per assembly unit, never per function; see literalUnitsInFunc.
func scanHistoryDeletes(t *testing.T, root string, tables map[string]string) map[string]*historyDeleteSiteFound {
	t.Helper()
	found := map[string]*historyDeleteSiteFound{}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			for _, unit := range literalUnitsInFunc(fset, fn.Body) {
				for tbl, stmts := range historyDeleteStatements(unit, tables) {
					key := rel + "#" + fn.Name.Name + "|" + tbl
					if found[key] == nil {
						found[key] = &historyDeleteSiteFound{}
					}
					found[key].lines = append(found[key].lines, strconv.Itoa(fset.Position(fn.Pos()).Line))
					found[key].statements = append(found[key].statements, stmts...)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking internal/: %v", err)
	}
	return found
}

// TestNoUnallowlistedHistoryDelete is guard G2 itself.
func TestNoUnallowlistedHistoryDelete(t *testing.T) {
	root := repoInternalRoot(t)
	found := scanHistoryDeletes(t, root, historyBearingTables)

	// Fail closed: the allowlist is non-empty, so a scan that finds nothing means the walk broke.
	if len(found) == 0 {
		t.Fatalf("scanned %s and found no history DELETE at all, but %d are allowlisted — the scan is "+
			"broken and would pass vacuously", root, len(allowedHistoryDeletes))
	}

	var unallowlisted, stale []string
	for key, site := range found {
		if _, ok := allowedHistoryDeletes[key]; !ok {
			unallowlisted = append(unallowlisted, key+" (func at line "+strings.Join(site.lines, ",")+")")
		}
	}
	for key := range allowedHistoryDeletes {
		if _, ok := found[key]; !ok {
			stale = append(stale, key)
		}
	}
	sort.Strings(unallowlisted)
	sort.Strings(stale)

	if len(unallowlisted) > 0 {
		t.Errorf("%d hard DELETE(s) of history-bearing tables are not allowlisted (#1569).\n"+
			"#1569 decided that financial history is retained unless the user explicitly asks for it "+
			"to go. If this delete IS that explicit request, add it to allowedHistoryDeletes with the "+
			"reason. If it is not, tombstone instead (UPDATE ... SET deleted_at = now()) or narrow the "+
			"predicate.\n"+
			"⚠ FOLLOW-THROUGH ON THE TOMBSTONE REMEDY, or you trade this failure for another one. "+
			"For a table one of the tombstone read-guards audits (transactions, savings_goals, "+
			"boosts, budgets, budget_categories, custom_categories) the tombstoning UPDATE is itself "+
			"a write target with no `deleted_at IS NULL` of its own — and it MUST NOT have one, since "+
			"adding it would change which rows the write affects. Taken literally this remedy clears "+
			"G2 and turns TestNoTransactionReadForgetsTheTombstoneFilter red. Carry the write-scoped "+
			"exemption with it:\n"+
			"    -- tombstone-exempt(write:<table>): tombstoning IS the write; narrowing it here "+
			"would change WHICH ROWS the statement affects\n"+
			"See transaction_repository.go's UpdateTags for the shape.\n    %s",
			len(unallowlisted), strings.Join(unallowlisted, "\n    "))
	}
	if len(stale) > 0 {
		t.Errorf("%d allowlisted history delete(s) no longer exist (#1569).\n"+
			"A stale entry reads as a standing permission to destroy this history. Delete it:\n    %s",
			len(stale), strings.Join(stale, "\n    "))
	}
}

// missingPredicateFragments returns the fragments of predicate that are NOT present in statement.
// statement is expected to be normalised already; each fragment is normalised here so that an entry
// can be written in ordinary, readable SQL casing and spacing.
func missingPredicateFragments(statement string, predicate []string) []string {
	var missing []string
	for _, frag := range predicate {
		if !strings.Contains(statement, normaliseSQL(frag)) {
			missing = append(missing, frag)
		}
	}
	return missing
}

// TestEveryAllowlistedHistoryDeleteStillCarriesItsPredicate is the second half of guard G2: an
// allowlist entry may not outlive the scope it was granted for.
//
// #1569 T7.5. Before this test, `allowedHistoryDeletes` blessed a (function, TABLE) pair with no
// predicate at all, so deleting `AND deleted_at IS NOT NULL` from PurgeAccount's savings_goals
// delete — one line — kept the blessing and turned a narrow cleanup into a hard delete of LIVE
// goals and their hand-logged contributions. Nothing in `go test ./...` noticed; the only thing
// holding those scopes was a `//go:build integration` test that SKIPS without DATABASE_URL.
//
// Read the failure message, not just the diff: a fragment can also go missing because the statement
// was legitimately rewritten. In that case update the entry's `predicate` — deliberately, naming
// the new legs — rather than deleting the fragment to get green.
func TestEveryAllowlistedHistoryDeleteStillCarriesItsPredicate(t *testing.T) {
	found := scanHistoryDeletes(t, repoInternalRoot(t), historyBearingTables)

	var widened []string
	for key, allowance := range allowedHistoryDeletes {
		site, ok := found[key]
		if !ok {
			continue // TestNoUnallowlistedHistoryDelete reports this as a stale entry.
		}
		// EVERY statement under the key, not just one: a second, wider delete of the same table in
		// the same function is precisely the shape the (function, TABLE) key cannot see.
		for _, stmt := range site.statements {
			if missing := missingPredicateFragments(stmt, allowance.predicate); len(missing) > 0 {
				widened = append(widened, key+"\n        missing: "+strings.Join(missing, " | ")+
					"\n        statement: "+stmt)
			}
		}
	}
	sort.Strings(widened)

	if len(widened) > 0 {
		t.Errorf("%d allowlisted history delete(s) no longer carry the predicate they were "+
			"blessed for (#1569).\n"+
			"The entry in allowedHistoryDeletes still authorises this destruction, but the SQL "+
			"beneath it has widened — which is the exact failure the predicate exists to catch. "+
			"Either restore the missing leg, or change the entry deliberately and say why in its "+
			"reason.\n    %s", len(widened), strings.Join(widened, "\n    "))
	}
}

// ---- Meta-tests: the constructed violations. ---------------------------------------------------

// TestHistoryDeleteGuard_FlagsAnUnallowlistedDelete is the case G2 exists for.
func TestHistoryDeleteGuard_FlagsAnUnallowlistedDelete(t *testing.T) {
	got := historyDeleteSites("DELETE FROM holding_snapshots WHERE account_id = $1", historyBearingTables)
	if len(got) != 1 || got[0] != "holding_snapshots" {
		t.Fatalf("expected the delete to be reported, got %v", got)
	}
}

// TestHistoryDeleteGuard_SeesTheFormsThatDoNotLookLikeADelete covers the three shapes that a
// substring search for "DELETE FROM holding_snapshots" would miss, and the one it must NOT match.
func TestHistoryDeleteGuard_SeesTheFormsThatDoNotLookLikeADelete(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want []string
	}{
		{"USING form", "DELETE FROM savings_goal_contributions c USING savings_goals g WHERE c.savings_goal_id = g.id", []string{"savings_goal_contributions"}},
		{"truncate", "TRUNCATE TABLE balance_snapshots", []string{"balance_snapshots"}},
		{"truncate only", "TRUNCATE ONLY rollover_history", []string{"rollover_history"}},
		// #1569 T7-FIX finding E: the schema-qualified and quoted forms, which a bare-identifier
		// matcher missed entirely — one token of edit was enough to switch both guards off.
		{"schema qualified", "DELETE FROM public.transactions WHERE id = $1", []string{"transactions"}},
		{"quoted identifier", `TRUNCATE TABLE "transactions"`, []string{"transactions"}},
		{"quoted schema and table", `DELETE FROM "public"."balance_snapshots" WHERE id = $1`, []string{"balance_snapshots"}},
		{"qualified truncate only", "TRUNCATE ONLY public.rollover_history", []string{"rollover_history"}},
		{"a qualified prefix must still not match the longer name", "DELETE FROM public.savings_goal_contributions WHERE id = $1", []string{"savings_goal_contributions"}},
		{"a qualified non-history table is still not our business", "DELETE FROM public.notifications WHERE id = $1", nil},
		{"a suffixed name is not the table", "DELETE FROM public.transactions_archive WHERE id = $1", nil},
		{"lowercase, newline-separated", "delete\n  from\n  boost_executions\nwhere id = $1", []string{"boost_executions"}},
		{"prefix must not match the longer name", "DELETE FROM savings_goal_contributions WHERE id = $1", []string{"savings_goal_contributions"}},
		{"budgets must not match budget_categories", "DELETE FROM budget_categories WHERE id = $1", []string{"budget_categories"}},
		{"a read is not a delete", "SELECT * FROM holding_snapshots WHERE account_id = $1", nil},
		{"a tombstone is not a delete", "UPDATE budgets SET deleted_at = now() WHERE id = $1", nil},
		{"another table is not our business", "DELETE FROM notifications WHERE user_id = $1", nil},
		// #1569 T8.5: the doc above used to disclaim these two forms as "still missed". The first
		// is CAUGHT — comments are blanked/stripped before the matcher runs, so the split name is
		// rejoined by whitespace the matcher already tolerates. The second is the one genuine
		// miss, and it is pinned as a miss so that the doc's claim and the code cannot drift
		// apart in either direction.
		{"a name split across a comment IS caught", "DELETE FROM public /* x */ . transactions WHERE id = $1", []string{"transactions"}},
		{"a comment INSIDE the identifier is the one real miss", "DELETE FROM trans/**/actions WHERE id = $1", nil},
		{"a comment between DELETE and FROM is caught", "DELETE -- c\n FROM transactions WHERE id = $1", []string{"transactions"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := historyDeleteSites(tc.sql, historyBearingTables)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestHistoryDeleteGuard_AllowlistEntriesAreWellFormed keeps the allowlist from silently ceasing to
// mean anything: a key whose table is not history-bearing can never match, and an entry with no
// reason is just a suppression.
func TestHistoryDeleteGuard_AllowlistEntriesAreWellFormed(t *testing.T) {
	for key, allowance := range allowedHistoryDeletes {
		if strings.TrimSpace(allowance.reason) == "" {
			t.Errorf("allowlist entry %q has no reason; the reason is the point", key)
		}
		if len(allowance.predicate) == 0 {
			t.Errorf("allowlist entry %q names no predicate. An entry with no predicate blesses "+
				"`DELETE FROM <table>` full stop, which is what #1569 T7.5 removed. Name the legs "+
				"that make the destruction narrow — including their polarity.", key)
		}
		for _, frag := range allowance.predicate {
			if strings.TrimSpace(frag) == "" {
				t.Errorf("allowlist entry %q has an empty predicate fragment, which every "+
					"statement trivially satisfies", key)
			}
		}
		i := strings.LastIndex(key, "|")
		if i < 0 || !strings.Contains(key[:i], "#") {
			t.Errorf("allowlist key %q is not of the form <path>#<func>|<table>", key)
			continue
		}
		if _, ok := historyBearingTables[key[i+1:]]; !ok {
			t.Errorf("allowlist key %q names table %q, which is not history-bearing — the entry can "+
				"never match and is permanently stale", key, key[i+1:])
		}
	}
}

// TestHistoryDeletePredicate_FlagsAWidenedStatement is the constructed violation for the predicate
// half of G2: the real PurgeAccount statement with its `deleted_at IS NOT NULL` scope removed, and
// with the `-- tombstone-exempt` comment that QUOTES that scope left in place, because leaving a
// comment behind is exactly how a widening would arrive in a review.
func TestHistoryDeletePredicate_FlagsAWidenedStatement(t *testing.T) {
	entry := allowedHistoryDeletes["repository/account_repository.go#PurgeAccount|savings_goals"]

	intact := historyDeleteStatements(`
		-- tombstone-exempt(write:savings_goals): the predicate is INVERTED on purpose and is right above --
		-- deleted_at IS NOT NULL.
		DELETE FROM savings_goals
		WHERE account_id = $1 AND user_id = $2 AND deleted_at IS NOT NULL
	`, historyBearingTables)["savings_goals"]
	if len(intact) != 1 {
		t.Fatalf("expected one statement, got %d", len(intact))
	}
	if missing := missingPredicateFragments(intact[0], entry.predicate); len(missing) > 0 {
		t.Fatalf("the intact statement should satisfy its own predicate, missing %v", missing)
	}

	widened := historyDeleteStatements(`
		-- tombstone-exempt(write:savings_goals): the predicate is INVERTED on purpose and is right above --
		-- deleted_at IS NOT NULL.
		DELETE FROM savings_goals
		WHERE account_id = $1 AND user_id = $2
	`, historyBearingTables)["savings_goals"]
	missing := missingPredicateFragments(widened[0], entry.predicate)
	if len(missing) != 1 || missing[0] != "deleted_at IS NOT NULL" {
		t.Fatalf("dropping the tombstone scope must be reported as exactly that fragment; got %v "+
			"(statement: %s)", missing, widened[0])
	}
}

// TestHistoryDeletePredicate_IsRobustToFormatting pins the properties that must NOT flip a verdict:
// re-wrapping, re-indenting, case, and comments. A guard that fires when someone runs a formatter
// gets deleted by the next engineer.
func TestHistoryDeletePredicate_IsRobustToFormatting(t *testing.T) {
	predicate := []string{"c.managed_by_reconciliation = true", "g.deleted_at IS NULL"}
	forms := []string{
		"DELETE FROM savings_goal_contributions c USING savings_goals g WHERE c.managed_by_reconciliation = true AND g.deleted_at IS NULL",
		"delete from savings_goal_contributions c\n\tusing savings_goals g\n\twhere\n\t\tc.managed_by_reconciliation = true\n\t\tand g.deleted_at is null",
		"DELETE FROM savings_goal_contributions c /* why */ USING savings_goals g\n-- a comment\nWHERE c.managed_by_reconciliation = true\n  AND g.deleted_at IS NULL",
	}
	for i, form := range forms {
		stmts := historyDeleteStatements(form, historyBearingTables)["savings_goal_contributions"]
		if len(stmts) != 1 {
			t.Fatalf("form %d: expected one statement, got %d", i, len(stmts))
		}
		if missing := missingPredicateFragments(stmts[0], predicate); len(missing) > 0 {
			t.Errorf("form %d: formatting changed the verdict, missing %v", i, missing)
		}
	}

	// The negative that formatting-robustness must not swallow: a comment QUOTING the leg does not
	// substitute for the leg.
	commentOnly := historyDeleteStatements(
		"DELETE FROM savings_goal_contributions c USING savings_goals g\n-- g.deleted_at IS NULL\nWHERE c.managed_by_reconciliation = true",
		historyBearingTables)["savings_goal_contributions"]
	if missing := missingPredicateFragments(commentOnly[0], predicate); len(missing) != 1 || missing[0] != "g.deleted_at IS NULL" {
		t.Errorf("a comment must not satisfy a predicate fragment; got %v", missing)
	}
}

// TestHistoryDeleteStatements_SplitsAtTheNextDelete proves the slicing that makes a per-statement
// predicate meaningful: two deletes in one function must not lend each other their predicates.
func TestHistoryDeleteStatements_SplitsAtTheNextDelete(t *testing.T) {
	got := historyDeleteStatements(
		"DELETE FROM balance_snapshots WHERE account_id = $1\nDELETE FROM accounts WHERE id = $1 AND user_id = $2",
		historyBearingTables)
	if len(got["balance_snapshots"]) != 1 || strings.Contains(got["balance_snapshots"][0], "ACCOUNTS") {
		t.Fatalf("the balance_snapshots slice must stop at the next DELETE, got %q", got["balance_snapshots"])
	}
	if len(got["accounts"]) != 1 || strings.Contains(got["accounts"][0], "ACCOUNT_ID = $1\n") {
		t.Fatalf("the accounts slice must start at its own DELETE, got %q", got["accounts"])
	}
	// A non-history delete between two history deletes is still a boundary.
	mixed := historyDeleteStatements(
		"DELETE FROM balance_snapshots WHERE account_id = $1\nDELETE FROM holdings WHERE account_id = $2\nDELETE FROM accounts WHERE id = $1",
		historyBearingTables)
	if strings.Contains(mixed["balance_snapshots"][0], "HOLDINGS") {
		t.Fatalf("a delete of a non-history table must end the preceding slice, got %q", mixed["balance_snapshots"][0])
	}
}

// TestHistoryDeleteStatements_SplitsAtTheStatementTerminator is #1569 T8.2 Item B, half one.
//
// Until this change a "statement" ran from its DELETE keyword to the NEXT DELETE, or to the end of
// the input. A semicolon was not a boundary, so anything following the delete inside the same SQL
// string — a SELECT, a second UPDATE, a RETURNING-less follow-up — was folded into the delete's own
// normalised text and could satisfy a predicate fragment the DELETE no longer carries. That is a
// bypass of the exact hardening T7.5 added: the blessing survives while the destruction widens.
//
// The boundary at the next DELETE is kept; the terminator is now whichever comes first.
func TestHistoryDeleteStatements_SplitsAtTheStatementTerminator(t *testing.T) {
	entry := allowedHistoryDeletes["repository/account_repository.go#PurgeAccount|savings_goals"]

	// The widening: the DELETE has lost `AND deleted_at IS NOT NULL`, and a LATER statement in the
	// same string happens to contain that text. No second DELETE, so nothing else ends the slice.
	widened := historyDeleteStatements(`
		DELETE FROM savings_goals WHERE account_id = $1 AND user_id = $2;
		SELECT count(*) FROM savings_goals WHERE account_id = $1 AND deleted_at IS NOT NULL;
	`, historyBearingTables)["savings_goals"]
	if len(widened) != 1 {
		t.Fatalf("expected one delete statement, got %d: %q", len(widened), widened)
	}
	if strings.Contains(widened[0], "SELECT") {
		t.Fatalf("the delete's slice ran past its terminator into the next statement: %q", widened[0])
	}
	missing := missingPredicateFragments(widened[0], entry.predicate)
	if len(missing) != 1 || missing[0] != "deleted_at IS NOT NULL" {
		t.Fatalf("a later statement lent the DELETE a predicate it no longer carries; missing %v "+
			"(statement: %s)", missing, widened[0])
	}

	// GREEN direction: a terminator INSIDE the delete's own text must not truncate it early, and
	// the intact statement must still satisfy its predicate.
	intact := historyDeleteStatements(`
		DELETE FROM savings_goals
		WHERE account_id = $1 AND user_id = $2 AND deleted_at IS NOT NULL;
	`, historyBearingTables)["savings_goals"]
	if len(intact) != 1 {
		t.Fatalf("expected one delete statement, got %d", len(intact))
	}
	if missing := missingPredicateFragments(intact[0], entry.predicate); len(missing) > 0 {
		t.Fatalf("the intact statement must satisfy its own predicate, missing %v", missing)
	}
}

// TestHistoryDeleteScan_DoesNotLendAPredicateFromAnotherLiteral is #1569 T8.2 Item B, half two.
//
// scanHistoryDeletes used to join EVERY string literal in a function before scanning. A help
// string, an error message, a second unrelated query — any of them could contain the text of a
// predicate fragment, and the widened DELETE would be blessed by it. The unit of assembly is now
// the enclosing STATEMENT (the same unit the tombstone read-guards use), so adjacent literals that
// really do build one query are still joined and two unrelated literals are not.
func TestHistoryDeleteScan_DoesNotLendAPredicateFromAnotherLiteral(t *testing.T) {
	entry := allowedHistoryDeletes["repository/account_repository.go#PurgeAccount|savings_goals"]

	scan := func(t *testing.T, body string) *historyDeleteSiteFound {
		t.Helper()
		dir := t.TempDir()
		src := "package fake\n\nfunc PurgeLookalike() {\n" + body + "\n}\n"
		if err := os.WriteFile(filepath.Join(dir, "fake.go"), []byte(src), 0o600); err != nil {
			t.Fatalf("writing fixture: %v", err)
		}
		found := scanHistoryDeletes(t, dir, historyBearingTables)
		site := found["fake.go#PurgeLookalike|savings_goals"]
		if site == nil {
			t.Fatalf("the fixture's delete was not seen at all; keys: %v", found)
		}
		return site
	}

	t.Run("an unrelated literal does not satisfy the predicate", func(t *testing.T) {
		site := scan(t, "\tq := `DELETE FROM savings_goals WHERE account_id = $1 AND user_id = $2`\n"+
			"\thelp := `only tombstoned goals are purged: deleted_at IS NOT NULL`\n"+
			"\t_, _ = q, help")
		if len(site.statements) != 1 {
			t.Fatalf("expected one statement, got %d: %q", len(site.statements), site.statements)
		}
		missing := missingPredicateFragments(site.statements[0], entry.predicate)
		if len(missing) != 1 || missing[0] != "deleted_at IS NOT NULL" {
			t.Fatalf("a sibling literal lent the DELETE a predicate it no longer carries; missing "+
				"%v (statement: %s)", missing, site.statements[0])
		}
	})

	t.Run("adjacent literals of ONE query are still joined", func(t *testing.T) {
		site := scan(t, "\tq := `DELETE FROM savings_goals` +\n"+
			"\t\t` WHERE account_id = $1 AND user_id = $2` +\n"+
			"\t\t` AND deleted_at IS NOT NULL`\n"+
			"\t_ = q")
		if len(site.statements) != 1 {
			t.Fatalf("expected one statement, got %d: %q", len(site.statements), site.statements)
		}
		if missing := missingPredicateFragments(site.statements[0], entry.predicate); len(missing) > 0 {
			t.Fatalf("splitting by statement broke a legitimately concatenated query, missing %v "+
				"(statement: %s)", missing, site.statements[0])
		}
	})
}

// TestHistoryDeleteStatements_IsNotFooledByAQuotedLiteral is #1569 T8-FIX, finding D.
//
// The statement partition scans NORMALISED TEXT, not parsed SQL. A quoted string in a legitimate
// query — a note, a label, a help column's default — can contain the very tokens the partition
// keys on, and both failure directions are real:
//
//   - a `DELETE FROM <history table>` inside a literal is reported as an unallowlisted delete of a
//     table nothing deletes. A guard that fires on legitimate code gets deleted by the next
//     engineer, so this direction is the one that kills the guard;
//   - a `;` inside a literal truncates a real delete's slice early, so a predicate fragment past it
//     reads as missing (false failure again), while a fragment sitting INSIDE a literal must never
//     count as the DELETE carrying it.
//
// The fix is not a SQL parser: normaliseSQL now blanks single-quoted and dollar-quoted literals the
// same way it already blanked comments, which is exactly proportionate to a guard that scans text.
func TestHistoryDeleteStatements_IsNotFooledByAQuotedLiteral(t *testing.T) {
	t.Run("a delete inside a quoted literal is not a delete", func(t *testing.T) {
		sites := historyDeleteSites(
			`UPDATE audit_notes SET body = $1 `+
				`WHERE note = 'DELETE FROM savings_goal_contributions; deleted_at IS NOT NULL'`,
			historyBearingTables)
		if len(sites) != 0 {
			t.Fatalf("a quoted literal must not read as a delete, got %v", sites)
		}
	})

	t.Run("a semicolon inside a quoted literal does not end the statement", func(t *testing.T) {
		entry := allowedHistoryDeletes["repository/account_repository.go#PurgeAccount|savings_goals"]
		stmts := historyDeleteStatements(
			`DELETE FROM savings_goals WHERE account_id = $1 AND user_id = $2 `+
				`AND source <> 'imported;manual' AND deleted_at IS NOT NULL`,
			historyBearingTables)["savings_goals"]
		if len(stmts) != 1 {
			t.Fatalf("expected one statement, got %d: %q", len(stmts), stmts)
		}
		if missing := missingPredicateFragments(stmts[0], entry.predicate); len(missing) > 0 {
			t.Fatalf("a semicolon inside a literal truncated the statement; missing %v (statement: %s)",
				missing, stmts[0])
		}
	})

	t.Run("a predicate fragment inside a quoted literal does not satisfy the predicate", func(t *testing.T) {
		entry := allowedHistoryDeletes["repository/account_repository.go#PurgeAccount|savings_goals"]
		stmts := historyDeleteStatements(
			`DELETE FROM savings_goals WHERE account_id = $1 AND user_id = $2 `+
				`AND label = 'deleted_at IS NOT NULL'`,
			historyBearingTables)["savings_goals"]
		if len(stmts) != 1 {
			t.Fatalf("expected one statement, got %d: %q", len(stmts), stmts)
		}
		missing := missingPredicateFragments(stmts[0], entry.predicate)
		if len(missing) != 1 || missing[0] != "deleted_at IS NOT NULL" {
			t.Fatalf("a quoted literal lent the DELETE a predicate it does not carry; missing %v "+
				"(statement: %s)", missing, stmts[0])
		}
	})

	// ⚠ THE DIRECTION THAT MATTERS MOST FOR A BLANKING FIX: blanking must not make the guard STOP
	// GUARDING. A delete that merely sits near quoted literals is still a delete, and `''` must
	// close a literal where SQL says it does rather than swallowing the rest of the string.
	t.Run("blanking hides no real delete", func(t *testing.T) {
		sites := historyDeleteSites(
			`INSERT INTO audit_notes (a, b) VALUES ('x', 'y'); `+
				`DELETE FROM transactions WHERE account_id = $1`,
			historyBearingTables)
		if len(sites) != 1 || sites[0] != "transactions" {
			t.Fatalf("a real delete beside quoted literals must still be seen, got %v", sites)
		}
		// ⚠ NO SUBTEST FOR THE `''` ESCAPE, DELIBERATELY, AND THE REASON IS WORTH KEEPING. The first
		// draft of this file asserted that the escape-aware matcher was load-bearing. It is not:
		// every `''`-escaped literal contains an EVEN number of quotes, so a naive `'[^']*'` splits
		// `'it''s odd'` into two adjacent matches and stays in phase for the rest of the string.
		// Replacing the matcher with the naive form was run against this whole test and changed
		// nothing (measured, not reasoned). The alternation is kept because it classifies literals
		// correctly, not because any assertion here depends on it — and an assertion that cannot
		// fail is worse than none, since it reads as coverage.
	})

	t.Run("a real statement terminator is still a boundary", func(t *testing.T) {
		stmts := historyDeleteStatements(
			`DELETE FROM savings_goals WHERE label = 'a;b' AND user_id = $1; `+
				`SELECT 1 FROM savings_goals WHERE deleted_at IS NOT NULL`,
			historyBearingTables)["savings_goals"]
		if len(stmts) != 1 {
			t.Fatalf("expected one statement, got %d: %q", len(stmts), stmts)
		}
		if strings.Contains(stmts[0], "SELECT") {
			t.Fatalf("blanking literals must not disable the real terminator: %q", stmts[0])
		}
	})
}

// TestHistoryDeleteScan_DoesNotLendAPredicateFromASiblingArgument is #1569 T8-FIX, finding E.
//
// T8.2 narrowed literal-joining from per FUNCTION to per ast.ExprStmt, and the note called that
// unit "one query". It is not: an ExprStmt is one whole CALL, parameter values included, so
//
//	db.Exec(ctx, "DELETE FROM savings_goals WHERE user_id = $1", "help: deleted_at IS NOT NULL")
//
// still let an unrelated argument supply the predicate a widened DELETE had lost. The unit is now
// the ARGUMENT of a call, so sibling arguments are separate and a concatenation inside ONE argument
// is still joined.
func TestHistoryDeleteScan_DoesNotLendAPredicateFromASiblingArgument(t *testing.T) {
	entry := allowedHistoryDeletes["repository/account_repository.go#PurgeAccount|savings_goals"]

	scan := func(t *testing.T, body string) *historyDeleteSiteFound {
		t.Helper()
		dir := t.TempDir()
		src := "package fake\n\nfunc PurgeLookalike(db any) {\n" + body + "\n}\n"
		if err := os.WriteFile(filepath.Join(dir, "fake.go"), []byte(src), 0o600); err != nil {
			t.Fatalf("writing fixture: %v", err)
		}
		found := scanHistoryDeletes(t, dir, historyBearingTables)
		site := found["fake.go#PurgeLookalike|savings_goals"]
		if site == nil {
			t.Fatalf("the fixture's delete was not seen at all; keys: %v", found)
		}
		return site
	}

	t.Run("a sibling call argument does not satisfy the predicate", func(t *testing.T) {
		site := scan(t, "\t_ = fmt.Sprint(db,\n"+
			"\t\t`DELETE FROM savings_goals WHERE account_id = $1 AND user_id = $2`,\n"+
			"\t\t`help text: deleted_at IS NOT NULL`)")
		if len(site.statements) != 1 {
			t.Fatalf("expected one statement, got %d: %q", len(site.statements), site.statements)
		}
		missing := missingPredicateFragments(site.statements[0], entry.predicate)
		if len(missing) != 1 || missing[0] != "deleted_at IS NOT NULL" {
			t.Fatalf("a sibling ARGUMENT lent the DELETE a predicate it no longer carries; missing "+
				"%v (statement: %s)", missing, site.statements[0])
		}
	})

	t.Run("a concatenation inside ONE argument is still joined", func(t *testing.T) {
		site := scan(t, "\t_ = fmt.Sprint(db,\n"+
			"\t\t`DELETE FROM savings_goals WHERE account_id = $1 AND user_id = $2`+\n"+
			"\t\t\t` AND deleted_at IS NOT NULL`,\n"+
			"\t\t`unrelated`)")
		if len(site.statements) != 1 {
			t.Fatalf("expected one statement, got %d: %q", len(site.statements), site.statements)
		}
		if missing := missingPredicateFragments(site.statements[0], entry.predicate); len(missing) > 0 {
			t.Fatalf("splitting by argument broke a legitimately concatenated query, missing %v "+
				"(statement: %s)", missing, site.statements[0])
		}
	})
}
