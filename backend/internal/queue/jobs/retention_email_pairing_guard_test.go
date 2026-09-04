package jobs

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Issue #1569 round 5 — the "two copies, one truth, nothing watching" shape, one more time.
//
// WHAT THE TWO COPIES ARE. Whether a cancelled account has an email address on file is decided in
// two places that cannot share a helper:
//
//   - canSendRetentionNotice (this package) — `user.Email == ""` — is the SENDER's gate. A user it
//     refuses gets no 30-day and no 7-day notice, and since #1569 gated the deletion sweep on both
//     markers, that user is retained indefinitely.
//   - GetRetentionDeletionsWithheldPage (internal/repository/profile_repository.go) — sets
//     `HasEmailOnFile: u.Email != ""` — is the ADMIN's DIAGNOSIS. It is the only surface in the
//     codebase that tells an operator WHY a withheld account is stuck.
//
// No shared helper is possible: one is a method on RetentionCleanupCheckWorker, the other runs in
// the repository package over a decrypted domain.User, and the dependency would point the wrong
// way. The repository side already carries a comment saying "if that check ever grows (a syntactic
// validity test, say), this must grow with it or this column starts reporting a reason the sender
// does not act on" — a comment is not enforcement, and this is the enforcement.
//
// WHY IT MATTERS THAT THEY AGREE, precisely. The failure is not cosmetic. If the sender's gate
// grows a condition the diagnosis does not (say the sender starts refusing a syntactically invalid
// address), the admin page reports HasEmailOnFile = true for an account that will never receive a
// notice and will therefore never be deleted — the operator is told the one reason it is NOT stuck.
// If the diagnosis grows a condition the sender does not, the operator chases a cause that is not
// blocking anything. Both directions are silent.
//
// ⚠ WHAT THIS DOES NOT CHECK. It is syntactic. It cannot decide whether the two expressions MEAN
// the same thing, only that neither moved without the other; if they are both changed to two
// different new expressions and both pinned entries below are updated to match, the guard is
// satisfied and the reviewer is the one who has to be right. What it buys is that one side cannot
// move ALONE and silently — which is exactly what nothing was watching.

// retentionEmailPairing is the pinned pair. Each side is the receiver-stripped source text of the
// expression that decides "has an address on file", and the two are logical negations of each
// other: the sender's gate is the WITHHOLDING side, the diagnosis is the HAVING side.
//
// ⚠ Updating an entry here is not the fix for a failure — it is the LAST step of the fix. Change
// both expressions first, satisfy yourself they are still negations, then update both entries in
// the same commit so the diff shows the pair moving together.
var retentionEmailPairing = struct {
	senderWithholds string // canSendRetentionNotice's refusal condition
	adminHas        string // HasEmailOnFile's value expression
}{
	senderWithholds: `Email == ""`,
	adminHas:        `Email != ""`,
}

const (
	senderGateFile = "retention_cleanup_check.go"
	adminDiagFile  = "../../repository/profile_repository.go"
)

// strippedExpr renders an expression and removes the leading receiver selector, so
// `user.Email == ""` and `u.Email != ""` are comparable to each other and to the pinned text.
func strippedExpr(fset *token.FileSet, e ast.Expr) string {
	var b strings.Builder
	if err := printer.Fprint(&b, fset, e); err != nil {
		return "<unprintable>"
	}
	s := strings.Join(strings.Fields(b.String()), " ")
	// Only the receiver of the FIRST selector is stripped; `u.Email` -> `Email`. A selector deeper
	// in the expression (`u.Profile.Email`) keeps its shape, which is the honest thing to do — that
	// IS a different expression.
	for _, recv := range []string{"user.", "u.", "w.", "usr."} {
		s = strings.ReplaceAll(s, recv, "")
	}
	return s
}

// senderWithholdingCondition finds, in the given source, the refusal condition inside
// canSendRetentionNotice that tests the user's Email. Returns "" when there is none, which is
// itself a failure the caller reports — a gate that stopped checking is the loudest change of all.
func senderWithholdingCondition(src string) (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "sender.go", src, 0)
	if err != nil {
		return "", err
	}
	var found []string
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "canSendRetentionNotice" || fn.Body == nil {
			return true
		}
		ast.Inspect(fn.Body, func(m ast.Node) bool {
			ifs, ok := m.(*ast.IfStmt)
			if !ok {
				return true
			}
			cond := strippedExpr(fset, ifs.Cond)
			if strings.Contains(cond, "Email") {
				found = append(found, cond)
			}
			return true
		})
		return false
	})
	if len(found) == 0 {
		return "", fmt.Errorf("canSendRetentionNotice contains no condition mentioning Email")
	}
	if len(found) > 1 {
		return "", fmt.Errorf("canSendRetentionNotice now has %d Email conditions (%v); the pairing "+
			"is no longer a single expression and this guard must be widened before it can pass",
			len(found), found)
	}
	return found[0], nil
}

// adminHasEmailExpression finds the value assigned to the HasEmailOnFile field in a composite
// literal.
func adminHasEmailExpression(src string) (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "admin.go", src, 0)
	if err != nil {
		return "", err
	}
	var found []string
	ast.Inspect(f, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != "HasEmailOnFile" {
			return true
		}
		found = append(found, strippedExpr(fset, kv.Value))
		return true
	})
	if len(found) == 0 {
		return "", fmt.Errorf("no composite literal assigns HasEmailOnFile")
	}
	if len(found) > 1 {
		return "", fmt.Errorf("HasEmailOnFile is now assigned in %d places (%v); the pairing is no "+
			"longer a single expression and this guard must be widened before it can pass",
			len(found), found)
	}
	return found[0], nil
}

// TestRetentionEmailGateAndAdminDiagnosisStayPaired is the guard itself, run over the real sources.
func TestRetentionEmailGateAndAdminDiagnosisStayPaired(t *testing.T) {
	senderSrc, err := os.ReadFile(senderGateFile)
	require.NoError(t, err, "read %s", senderGateFile)
	adminSrc, err := os.ReadFile(adminDiagFile)
	require.NoError(t, err, "read %s", adminDiagFile)

	const why = "\n\nThese two decide the SAME thing — does this cancelled account have an address " +
		"we can send a retention notice to — in two packages, with no shared helper possible " +
		"(one is a method on the worker). If they disagree, the admin withheld page reports a " +
		"reason the sender does not act on, or misses the one that is actually blocking the " +
		"account's deletion. Change BOTH, then update retentionEmailPairing in the same commit."

	senderCond, err := senderWithholdingCondition(string(senderSrc))
	require.NoError(t, err, "canSendRetentionNotice's email gate could not be located"+why)
	assert.Equal(t, retentionEmailPairing.senderWithholds, senderCond,
		"canSendRetentionNotice's email refusal condition changed."+why)

	adminExpr, err := adminHasEmailExpression(string(adminSrc))
	require.NoError(t, err, "HasEmailOnFile's value expression could not be located"+why)
	assert.Equal(t, retentionEmailPairing.adminHas, adminExpr,
		"HasEmailOnFile's value expression changed."+why)
}

// TestRetentionEmailPairingGuard_CatchesConstructedViolations is the meta-test: it builds the
// violations the guard exists to catch and proves the extractors report them, so a guard that has
// quietly stopped seeing its corpus cannot pass as a working one.
func TestRetentionEmailPairingGuard_CatchesConstructedViolations(t *testing.T) {
	t.Run("a widened sender gate is reported", func(t *testing.T) {
		src := `package jobs
func (w *RetentionCleanupCheckWorker) canSendRetentionNotice(jobID int64, noticeName string, user *domain.User) bool {
	if w.emailService == nil { return false }
	if user.Email == "" || !strings.Contains(user.Email, "@") { return false }
	return true
}`
		got, err := senderWithholdingCondition(src)
		require.NoError(t, err)
		assert.NotEqual(t, retentionEmailPairing.senderWithholds, got,
			"a gate that grew a syntactic-validity test must not compare equal to the pinned one")
	})

	t.Run("a sender gate that stopped checking at all is reported", func(t *testing.T) {
		src := `package jobs
func (w *RetentionCleanupCheckWorker) canSendRetentionNotice(jobID int64, noticeName string, user *domain.User) bool {
	if w.emailService == nil { return false }
	return true
}`
		_, err := senderWithholdingCondition(src)
		require.Error(t, err, "a removed gate is the loudest change of all and must not read as 'unchanged'")
	})

	t.Run("a widened admin diagnosis is reported", func(t *testing.T) {
		src := `package repository
func f() { _ = RetentionWithheldAccount{HasEmailOnFile: u.Email != "" && u.EmailVerified} }`
		got, err := adminHasEmailExpression(src)
		require.NoError(t, err)
		assert.NotEqual(t, retentionEmailPairing.adminHas, got)
	})

	t.Run("the extractors accept today's real expressions", func(t *testing.T) {
		// The other direction, and the one that makes the three above mean something: a guard that
		// rejected everything would pass all of them while flagging nothing real.
		src := `package jobs
func (w *RetentionCleanupCheckWorker) canSendRetentionNotice(jobID int64, noticeName string, user *domain.User) bool {
	if w.emailService == nil { return false }
	if user.Email == "" { return false }
	return true
}`
		got, err := senderWithholdingCondition(src)
		require.NoError(t, err)
		assert.Equal(t, retentionEmailPairing.senderWithholds, got)

		adminGot, err := adminHasEmailExpression(
			`package repository
func f() { _ = RetentionWithheldAccount{HasEmailOnFile: u.Email != ""} }`)
		require.NoError(t, err)
		assert.Equal(t, retentionEmailPairing.adminHas, adminGot)
	})
}
