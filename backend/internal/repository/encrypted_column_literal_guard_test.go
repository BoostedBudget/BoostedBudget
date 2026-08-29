package repository

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Issue #1569 T8 — the wrong-write-path fixture class, narrowed to the part a guard can actually
// decide.
//
// THE DEFECT. A fixture wrote `email = ''` straight into users.email and the test then asserted a
// count of rows with no address. users.email holds CIPHERTEXT: EncryptString returns "ENC$"+ct even
// for the empty string, so no row production can create ever satisfies `email = ''`. The predicate
// was dead, the assertion was vacuous, and the test was green. profile_repository.go:4638 records
// the same discovery on the PRODUCTION side — a live `email IS NULL OR email = ''` branch that
// could never fire.
//
// THE RULE. In any Go source under internal/ or cmd/ — tests included, since that is where the
// defect lived — an encrypted column may not be COMPARED TO A BARE SQL LITERAL. `email = $1` is
// fine (the parameter can carry ciphertext). `email IS NULL` is fine (NULL is representable).
// `email = ''` is not, and neither is `clerk_user_id = 'user_abc'`.
//
// ⚠ WHAT THIS DOES NOT COVER, stated plainly because a narrow honest guard beats a broad vague one.
// The wrong-write-path class had five instances this round and only one of them is this shape:
//
//   - It says nothing about a fixture that INSERTS plaintext into an encrypted column via a
//     parameter. There are ~117 such `INSERT INTO users` fixtures and nearly all are harmless —
//     they never assert on the column's value — so flagging them would be noise, and a guard that
//     cries wolf gets worked around.
//   - It says nothing about a fixture that hand-builds a row the only caller cannot produce
//     (T5's pre-categorized transaction, T7's `removed` account still carrying a balance). Those
//     need to know what the production write path WOULD have written, which no syntactic rule can
//     decide. The database catches some of them itself: transactions_tombstone_consistency rejected
//     a bare `SET deleted_at = NOW()` fixture, and a CHECK constraint invalidated two T4 mutants.
//     Where a constraint can express the invariant, a constraint is the better guard.
//   - It says nothing about an assertion satisfied by text the component did not render for that
//     reason (a `toMatch(/2025/)` matched by an unrelated header). That is a frontend concern and
//     a different mechanism entirely.
//
// Mutation-checked: TestEncryptedColumnLiteral_Detects constructs each case, and adding
// `WHERE email = ''` to any scanned file turns the corpus check red naming the file and line.

// encryptedColumns are the columns whose stored value is `ENC$`+ciphertext, each with the
// EncryptString call site that makes it so. Hand-maintained, because the Go-field-to-SQL-column
// mapping is not mechanically derivable; the citations are here so it can be re-derived.
var encryptedColumns = map[string]string{
	"email":                  "profile_repository.go:761 / user_identity_repository.go:243 / household_invites via :93",
	"invitee_email":          "household_invite_repository.go:93",
	"clerk_user_id":          "profile_repository.go:767, user_identity_repository.go:237",
	"provider_user_id":       "user_identity_repository.go:248",
	"paddle_customer_id":     "profile_repository.go:1050",
	"paddle_subscription_id": "profile_repository.go:1199",
	"plaid_account_id":       "account_repository.go:394",
	"institution_id":         "user_account_repository.go:77",
	"transaction_id":         "paddle_transaction_repository.go:109",
}

// encryptedLiteralComparison matches `<col> <op> '<literal>'`, optionally alias-qualified. The
// single quote is the whole point: a `$1` placeholder on the right-hand side is legitimate.
func encryptedLiteralComparison(cols []string) *regexp.Regexp {
	sort.Strings(cols)
	return regexp.MustCompile(
		`(?is)(?:[A-Za-z_][A-Za-z0-9_]*\.)?\b(` + strings.Join(cols, "|") +
			`)\s*(?:=|<>|!=|\bLIKE\b|\bILIKE\b)\s*'`)
}

// encryptedLiteralExemptToken is the opt-out, and it is PER COLUMN:
// `encrypted-literal-exempt(<column>, ...): <reason>`.
//
// ⚠ THIS WAS A BLANKET `encrypted-literal-exempt:` FOR ONE COMMIT, and that is worth recording
// rather than quietly fixing. It was character-for-character the per-statement short-circuit that
// the commit introducing this guard's sibling had just deleted from tombstoneReadOffenders, with a
// message explaining why it is wrong — and it failed the same way: a reason naming only
// `clerk_user_id` silently excused `email = ''` in the same statement, which is the LITERAL
// founding defect of this guard. Reproduced and reverted in T8 fix round 1. It shares
// namedExemptToken with the tombstone guard now, so there is one mechanism, not two.
var encryptedLiteralExemptToken = namedExemptToken("encrypted-literal-exempt")

// encryptedColumnLiteralOffenders returns the encrypted columns one SQL statement compares to a
// literal, minus the ones an exemption NAMES.
func encryptedColumnLiteralOffenders(sql string, cols []string) []string {
	exempt := namedExemptTargets(sql, encryptedLiteralExemptToken)
	var out []string
	for _, m := range encryptedLiteralComparison(cols).FindAllStringSubmatch(stripSQLComments(sql), -1) {
		if exempt[strings.ToLower(m[1])] {
			continue
		}
		out = append(out, m[1])
	}
	return out
}

func TestEncryptedColumnLiteral_Detects(t *testing.T) {
	cols := []string{"email", "clerk_user_id"}

	cases := []struct {
		name string
		sql  string
		want string
	}{
		{"the empty-string fixture that started this", `UPDATE users SET email = '' WHERE id = $1`, "email"},
		{"a qualified comparison", `SELECT 1 FROM users u WHERE u.clerk_user_id = 'user_abc'`, "clerk_user_id"},
		{"the dead production branch", `SELECT count(*) FROM users WHERE email IS NULL OR email = ''`, "email"},
		{"a parameter is legitimate", `SELECT 1 FROM users WHERE email = $1`, ""},
		{"IS NULL is legitimate", `SELECT 1 FROM users WHERE email IS NULL`, ""},
		{"an unrelated column is not matched", `SELECT 1 FROM users WHERE subscription_tier = 'premium'`, ""},
		{"a comment mentioning the shape is not the shape", "SELECT 1 FROM users\n-- email = '' was dead\nWHERE id = $1", ""},
		{
			"a named exemption excuses only the column it names",
			"SELECT 1 FROM users\n" +
				"-- encrypted-literal-exempt(clerk_user_id): this column is plaintext in the fixture.\n" +
				"WHERE clerk_user_id = 'user_abc' AND email = ''",
			"email",
		},
		{
			"a bare token excuses nothing",
			"SELECT 1 FROM users\n" +
				"-- encrypted-literal-exempt: a reason that names no column.\n" +
				"WHERE clerk_user_id = 'user_abc' AND email = ''",
			"clerk_user_id,email",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(encryptedColumnLiteralOffenders(tc.sql, cols), ",")
			if got != tc.want {
				t.Fatalf("offenders = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNoEncryptedColumnComparedToALiteral(t *testing.T) {
	// internal/ AND cmd/. The docstring claimed both from the start while the walk covered only
	// internal/, and cmd/server/main.go is 2000+ lines of wiring — exactly where a hand-written
	// diagnostic query would live. Verified by planting the identical literal under cmd/server and
	// watching it go from GREEN to reported.
	backendRoot := filepath.Dir(repoInternalRoot(t))
	roots := []string{
		filepath.Join(backendRoot, "internal"),
		filepath.Join(backendRoot, "cmd"),
	}

	cols := make([]string, 0, len(encryptedColumns))
	for c := range encryptedColumns {
		cols = append(cols, c)
	}

	var offenders []string
	scanned := 0

	// #1569 round 14 task 2: shared parse-once cache instead of this test's own filepath.Walk +
	// parser.ParseFile. skipTestdata=false and includeTests=true reproduce this test's ORIGINAL
	// corpus exactly (it never excluded testdata, and it scans _test.go files on purpose — see the
	// doc comment above).
	for _, root := range roots {
		for _, pf := range goProductionFiles(t, root, false, true) {
			rel := filepath.Join(filepath.Base(root), pf.rel)
			scanned++
			// This file's own meta-test fixtures ARE the violation, deliberately. Excluding it is
			// the same compromise the citation guard makes for its fabricated names: a guard whose
			// examples trip itself gets its examples deleted, and then it has no examples.
			if rel == "internal/repository/encrypted_column_literal_guard_test.go" {
				continue
			}

			ast.Inspect(pf.file, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				s, e := strconv.Unquote(lit.Value)
				if e != nil {
					return true
				}
				for _, col := range encryptedColumnLiteralOffenders(s, cols) {
					offenders = append(offenders,
						rel+":"+strconv.Itoa(pf.fset.Position(lit.Pos()).Line)+" ("+col+")")
				}
				return true
			})
		}
	}
	if scanned < 100 {
		t.Fatalf("scanned only %d Go files; the walk is broken and a pass here would be vacuous", scanned)
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Fatalf(
			"%d SQL comparison(s) put an ENCRYPTED column against a bare literal (#1569 T8).\n"+
				"That column stores \"ENC$\"+ciphertext, so no row production can create will ever\n"+
				"match — the predicate is dead and any assertion resting on it is vacuous. Compare\n"+
				"against a parameter carrying the encrypted value, or add an SQL comment\n"+
				"an SQL comment naming THAT column if it really is plaintext here:\n"+
				"  -- encrypted-literal-exempt(<column>, ...): <why>\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}

// TestEveryEncryptedLiteralExemptTokenNamesItsColumn is the symmetry with
// TestEveryTombstoneExemptTokenNamesItsReference. Both directions of the exemption already fail
// closed — a bare token excuses nothing and a wrong-column token excuses only what it names — so
// nothing can hide behind a malformed one. What was missing is that a malformed one is never
// REPORTED, so it sits in the source reading like protection while providing none.
//
// It also rejects a token naming a column that is not encrypted, which is the dead-exemption case:
// `encrypted-literal-exempt(subscription_tier)` excuses nothing, because that column was never
// audited.
func TestEveryEncryptedLiteralExemptTokenNamesItsColumn(t *testing.T) {
	backendRoot := filepath.Dir(repoInternalRoot(t))
	roots := []string{
		filepath.Join(backendRoot, "internal"),
		filepath.Join(backendRoot, "cmd"),
	}

	var offenders []string
	scanned := 0

	// #1569 round 14 task 2: shared parse-once cache. skipTestdata=true and includeTests=true
	// reproduce this test's ORIGINAL corpus exactly (it did skip testdata, unlike its sibling above,
	// and it scans _test.go files on purpose).
	for _, root := range roots {
		for _, pf := range goProductionFiles(t, root, true, true) {
			rel := filepath.Join(filepath.Base(root), pf.rel)
			scanned++
			// Same self-exclusion as the corpus check above, and for the same reason: this file's
			// fixtures are the malformed forms, deliberately.
			if rel == "internal/repository/encrypted_column_literal_guard_test.go" {
				continue
			}

			ast.Inspect(pf.file, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				text, unqErr := strconv.Unquote(lit.Value)
				if unqErr != nil || !strings.Contains(text, "encrypted-literal-exempt") {
					return true
				}
				at := rel + ":" + strconv.Itoa(pf.fset.Position(lit.Pos()).Line)

				if namedExemptIsBare(text, "encrypted-literal-exempt", encryptedLiteralExemptToken) {
					offenders = append(offenders, at+" uses `encrypted-literal-exempt` in a form other "+
						"than `encrypted-literal-exempt(<column>, ...):`, which excuses nothing")
				}
				for col := range namedExemptTargets(text, encryptedLiteralExemptToken) {
					if _, isEncrypted := encryptedColumns[col]; !isEncrypted {
						offenders = append(offenders, at+" exempts "+strconv.Quote(col)+
							", which is not an encrypted column — the exemption is dead")
					}
				}
				return true
			})
		}
	}
	if scanned < 100 {
		t.Fatalf("scanned only %d Go files; the walk is broken and a pass here would be vacuous", scanned)
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Fatalf("%d malformed or dead encrypted-literal exemption(s) (#1569 T8). The token must "+
			"NAME the encrypted column it excuses:\n"+
			"  -- encrypted-literal-exempt(<column>, ...): <why>\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}
