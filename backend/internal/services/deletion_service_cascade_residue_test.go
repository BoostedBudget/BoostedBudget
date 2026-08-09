package services

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// ─── The cascade-ERROR half of the post-gate residue (round 12, finding AB-1) ────────────────────
//
// #1504 made the two paths on which a user SURVIVES a run that had already cancelled their Paddle
// subscription record a residue: the archive gate declining, and the cascade's own guard refusing
// (`deleted == false`). It deliberately left the third path — DeleteUserCascade returning an ERROR —
// recording nothing, on the stated grounds that such a run is still expected to complete on retry
// and a permanent operator artifact would be wrong for a transient failure.
//
// #1507 then made a failed in-transaction audit write ABORT the cascade, which created a class of
// cascade error that is NOT transient: a revoked INSERT privilege on audit_logs, a new CHECK or RLS
// policy, a trigger. b76e1b8a named that consequence and deliberately did not act on it. The result
// is the harm #1504's two metrics exist to make visible, occurring with both metrics reading zero:
// account kept, subscription irreversibly cancelled at Paddle, banks disconnected — repeating on
// every retry and every sweep for as long as the failure lasts.
//
// ⚠ WHAT THESE TESTS DO NOT ASK FOR. They must NOT be satisfiable by recording residue on every
// cascade error: that is precisely what b76e1b8a's reasoning rules out, and
// TestExecuteDeletion_CascadeFailsTransientlyAfterPaddleCancel_RecordsNoResidue below is the sibling
// that kills that mutation. The distinction drawn is by SQLSTATE class — see
// cascadeErrorIsRecognisedTransient in deletion_service.go for why the unknown case records.

// wrapPgError returns the error a repository layer actually hands back: a pgconn.PgError wrapped in
// the fmt.Errorf chain DeleteUserCascade builds around it. Testing against the bare sentinel would
// pass on a classifier that only ever looks at the top-level error, which no production caller sees.
func wrapPgError(code string) error {
	return fmt.Errorf("DeleteUserCascade: write the terminal audit entry: %w",
		&pgconn.PgError{Code: code, Message: "injected by test"})
}

// cascadeErrorHarness programs the repository so the cascade fails with `cascadeErr` AFTER the whole
// Plaid + Paddle sequence has run and the subscription cancel has landed.
func cascadeErrorHarness(t *testing.T, ctx context.Context, cascadeErr error) *revocationHarness {
	t.Helper()
	h := newRevocationHarness(t, ctx)

	h.repo.ExpectedCalls = nil
	h.repo.On("GetUserByID", ctx, h.userID).Return(h.user, nil)
	h.repo.On("ClaimForDeletion", ctx, h.userID).Return(true, nil)
	h.repo.On("RecordDeletionResidue", mock.Anything, h.userID, mock.Anything).Return(nil).Maybe()
	h.repo.On("DeleteUserCascade", ctx, h.userID, mock.Anything).Return(false, cascadeErr).
		Run(func(mock.Arguments) { *h.callOrder = append(*h.callOrder, "DeleteUserCascade") })

	return h
}

// THE RED TEST. 42501 is insufficient_privilege — a revoked INSERT on audit_logs, the first of the
// three persistent modes b76e1b8a's own commit message enumerates, and the one its test injects.
func TestExecuteDeletion_CascadeFailsPersistentlyAfterPaddleCancel_RecordsTheResidue(t *testing.T) {
	ctx := context.Background()
	h := cascadeErrorHarness(t, ctx, wrapPgError("42501"))

	err := h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested)
	require.Error(t, err, "a cascade error is still an error — this change adds visibility, not a new outcome")

	assert.Equal(t, []string{"PlaidRemoveAll", "PaddleCancel", "PaddleArchive", "DeleteUserCascade"},
		*h.callOrder, "the subscription really was cancelled before the cascade failed")
	h.repo.AssertCalled(t, "RecordDeletionResidue", mock.Anything, h.userID,
		repository.DeletionResiduePaddleSubscriptionCancelledCascadeFailed)
}

// A CHECK/RLS violation (23514) and a trigger's raise_exception (P0001) are the other two persistent
// modes named. Both must record, and by the same kind — an operator triaging these does not care
// which constraint fired, only that retrying will not clear it.
func TestExecuteDeletion_CascadeFailsWithConstraintOrTrigger_RecordsTheResidue(t *testing.T) {
	for name, code := range map[string]string{
		"check_violation":  "23514",
		"raise_exception":  "P0001",
		"undefined_table":  "42P01",
		"insufficient_res": "42501",
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			h := cascadeErrorHarness(t, ctx, wrapPgError(code))

			require.Error(t, h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested))
			h.repo.AssertCalled(t, "RecordDeletionResidue", mock.Anything, h.userID,
				repository.DeletionResiduePaddleSubscriptionCancelledCascadeFailed)
		})
	}
}

// SIBLING ONE, MANDATORY — this is the assertion that stops the fix from becoming
// "record on every cascade error", which is exactly what b76e1b8a's reasoning rules out. A deadlock
// (40P01) or a serialization failure (40001) is the transient case the existing rule was written
// for: the claim is released, the next run retries the whole sequence, and the deletion is still
// expected to complete. Recording there would put a permanent artifact in front of an operator for
// an incident that resolves itself seconds later.
func TestExecuteDeletion_CascadeFailsTransientlyAfterPaddleCancel_RecordsNoResidue(t *testing.T) {
	for name, code := range map[string]string{
		"deadlock_detected":     "40P01",
		"serialization_failure": "40001",
		"connection_failure":    "08006",
		"too_many_connections":  "53300",
		"query_canceled":        "57014",
		"lock_not_available":    "55P03",
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			h := cascadeErrorHarness(t, ctx, wrapPgError(code))

			require.Error(t, h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested))
			assert.Contains(t, *h.callOrder, "PaddleCancel", "the cancel really did land — this is the case that would otherwise record")
			h.repo.AssertNotCalled(t, "RecordDeletionResidue", mock.Anything, mock.Anything, mock.Anything)
		})
	}
}

// A cancelled or timed-out context is transient in the same sense and is not a PgError at all, so it
// exercises the non-SQLSTATE half of the classifier. This is the shutdown case: the same deadline
// that killed the cascade also killed the job, and the next run will re-do the whole sequence.
func TestExecuteDeletion_CascadeFailsOnContextCancellation_RecordsNoResidue(t *testing.T) {
	ctx := context.Background()
	h := cascadeErrorHarness(t, ctx, fmt.Errorf("DeleteUserCascade: begin: %w", context.DeadlineExceeded))

	require.Error(t, h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested))
	h.repo.AssertNotCalled(t, "RecordDeletionResidue", mock.Anything, mock.Anything, mock.Anything)
}

// SIBLING TWO, MANDATORY — the record must follow the EFFECT, not the failure. A cascade error on a
// user whose subscription was never cancelled (raw status "cancelled", so cleanupPaddleResources
// skips the cancel entirely) records nothing: nothing irreversible happened to them.
func TestExecuteDeletion_CascadeFailsWithNoSubscriptionCancelled_RecordsNoResidue(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarnessWithStatus(t, context.Background(), "cancelled")

	h.repo.ExpectedCalls = nil
	h.repo.On("GetUserByID", ctx, h.userID).Return(h.user, nil)
	h.repo.On("ClaimForDeletion", ctx, h.userID).Return(true, nil)
	h.repo.On("RecordDeletionResidue", mock.Anything, h.userID, mock.Anything).Return(nil).Maybe()
	h.repo.On("DeleteUserCascade", ctx, h.userID, mock.Anything).Return(false, wrapPgError("42501")).
		Run(func(mock.Arguments) { *h.callOrder = append(*h.callOrder, "DeleteUserCascade") })

	require.Error(t, h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested))
	h.paddle.AssertNotCalled(t, "CancelSubscriptionImmediately", mock.Anything, mock.Anything)
	h.repo.AssertNotCalled(t, "RecordDeletionResidue", mock.Anything, mock.Anything, mock.Anything)
}

// SIBLING THREE, MANDATORY — the ordinary success path. Its counterpart for the revocation half is
// TestExecuteDeletion_OrdinarySuccess_RecordsNoResidue; this one is stated separately because the new
// branch is on the ERROR return of the same call and a classifier that recorded on `err == nil` would
// pass that test and fail this one.
func TestExecuteDeletion_OrdinarySuccess_RecordsNoCascadeFailedResidue(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)

	require.NoError(t, h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested))

	assert.Contains(t, *h.callOrder, "DeleteUserCascade")
	h.repo.AssertNotCalled(t, "RecordDeletionResidue", mock.Anything, mock.Anything, mock.Anything)
}

// A residue write that itself fails must not change the outcome the caller sees — the cascade error
// is still the error that propagates. Mirrors the revocation half's
// TestExecuteDeletion_ResidueWriteFails_StillReturnsTheLegitimateOutcome.
func TestExecuteDeletion_CascadeResidueWriteFails_StillReturnsTheCascadeError(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)

	h.repo.ExpectedCalls = nil
	h.repo.On("GetUserByID", ctx, h.userID).Return(h.user, nil)
	h.repo.On("ClaimForDeletion", ctx, h.userID).Return(true, nil)
	h.repo.On("RecordDeletionResidue", mock.Anything, h.userID, mock.Anything).
		Return(errors.New("audit insert failed"))
	h.repo.On("DeleteUserCascade", ctx, h.userID, mock.Anything).Return(false, wrapPgError("42501"))

	err := h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested)
	require.ErrorContains(t, err, "failed to cascade delete user data")
	h.repo.AssertCalled(t, "RecordDeletionResidue", mock.Anything, h.userID,
		repository.DeletionResiduePaddleSubscriptionCancelledCascadeFailed)
}

// ─── The classifier itself, in isolation ────────────────────────────────────────────────────────

// The DEFAULT DIRECTION is the load-bearing decision and it is asserted directly rather than only
// through ExecuteDeletion: an error the classifier does not recognise is treated as NOT transient,
// so it records. See cascadeErrorIsRecognisedTransient's doc comment for the argument.
func TestCascadeErrorIsRecognisedTransient_UnknownErrorsAreNotTransient(t *testing.T) {
	for _, err := range []error{
		errors.New("something nobody anticipated"),
		fmt.Errorf("DeleteUserCascade: erase dead_letter_jobs: %w", errors.New("boom")),
		wrapPgError("23505"), // unique_violation: a real, persistent constraint failure
		wrapPgError(""),      // a PgError with no code carries no evidence of transience
	} {
		assert.False(t, cascadeErrorIsRecognisedTransient(err), "unrecognised: %v", err)
	}
}

// The recognised-transient set, asserted as MEMBERSHIP of specific codes rather than as a count, so
// that broadening the predicate cannot move the assertion and the baseline together.
func TestCascadeErrorIsRecognisedTransient_RecognisedClasses(t *testing.T) {
	for _, code := range []string{"08006", "08003", "40001", "40P01", "53300", "53200", "55P03", "57014", "57P01"} {
		assert.True(t, cascadeErrorIsRecognisedTransient(wrapPgError(code)), "SQLSTATE %s", code)
	}
	assert.True(t, cascadeErrorIsRecognisedTransient(context.Canceled))
	assert.True(t, cascadeErrorIsRecognisedTransient(context.DeadlineExceeded))
}

// SQLSTATE class 08 ("connection exception") is in the allowlist, but the most common way a
// connection failure actually reaches this classifier carries NO SQLSTATE AT ALL. DeleteUserCascade
// opens its transaction (profile_repository.go, `BeginTx`) only AFTER the external cleanup, so a
// pool acquisition that cannot reach Postgres fails there — and pgx reports that as a
// *pgconn.ConnectError, not a *pgconn.PgError. `errors.As(err, &pgErr)` therefore misses it and the
// syntactic default records.
//
// That is the exact false positive the round-13 Codex review asked about: the Paddle cancel has
// landed, the database blips for one run, a PERMANENT operator artifact is written, and the very
// next run completes the deletion — leaving a residue row describing a user who no longer exists and
// a harm that never happened. Nothing ever clears it.
//
// The allowlist's own intent settles the direction: whoever put "08" in it meant connection failures
// to be transient. This closes the gap between that intent and the shape the error actually arrives
// in. It does NOT widen the default — an unrecognised error still records, which is the asymmetry
// cascadeErrorIsRecognisedTransient's doc comment argues for.
func TestCascadeErrorIsRecognisedTransient_ConnectionAttemptFailuresAreTransient(t *testing.T) {
	// A GENUINE *pgconn.ConnectError, produced the way production produces one, rather than a
	// hand-built literal: ConnectError's inner error field is unexported, so a literal cannot carry a
	// cause and would not be the shape this classifier has to survive. Port 1 is refused immediately
	// and needs no server, so this is deterministic and offline.
	_, connErr := pgconn.Connect(context.Background(),
		"postgres://u:p@127.0.0.1:1/nodb?sslmode=disable&connect_timeout=1")
	require.Error(t, connErr)
	var asConnect *pgconn.ConnectError
	require.True(t, errors.As(connErr, &asConnect),
		"control for THIS claim: the error pgx returns for an unreachable host really is a *pgconn.ConnectError")
	var asPg *pgconn.PgError
	require.False(t, errors.As(connErr, &asPg),
		"control for THIS claim: it carries no SQLSTATE, which is exactly why the class-08 allowlist entry misses it")

	assert.True(t, cascadeErrorIsRecognisedTransient(connErr),
		"a bare *pgconn.ConnectError must be recognised as transient")
	assert.True(t, cascadeErrorIsRecognisedTransient(
		fmt.Errorf("DeleteUserCascade: begin transaction: %w", connErr)),
		"a WRAPPED *pgconn.ConnectError must be recognised as transient — the repository wraps everything")

	// The control that pairs with THIS claim, not with the tool: widening to ConnectError must not
	// have widened the default. A persistent constraint failure, and a plain error, still record.
	assert.False(t, cascadeErrorIsRecognisedTransient(wrapPgError("23505")),
		"widening to ConnectError must not make a unique_violation transient")
	assert.False(t, cascadeErrorIsRecognisedTransient(errors.New("something nobody anticipated")),
		"widening to ConnectError must not make an unrecognised error transient")
}

// THE CASE THE FIRST VERSION OF THAT FIX GOT WRONG, pinned so it cannot come back.
//
// pgconn puts the SERVER'S startup ErrorResponse inside a ConnectError, so a rotated password
// (28P01) or a dropped database (3D000) reaches this classifier as a ConnectError that DOES carry a
// SQLSTATE. Those are permanent: no retry clears them. The first version of the ConnectError branch
// ran BEFORE the SQLSTATE lookup and returned true for every ConnectError, which silently dropped
// both the human-facing Error log and the residue for exactly that permanent case. The #1432
// round-13 Codex RE-REVIEW refuted it and the order was inverted; this test is what holds the order.
//
// errors.Join models "a ConnectError that contains a PgError" for errors.As without needing
// ConnectError's unexported cause field, which no external package can populate. Note deliberately:
// the joined value's Error() is never called anywhere in this test, because ConnectError.Error()
// dereferences that unexported field.
func TestCascadeErrorIsRecognisedTransient_ConnectErrorCarryingASQLSTATEUsesTheSQLSTATE(t *testing.T) {
	bareConnect := &pgconn.ConnectError{Config: &pgconn.Config{}}

	for _, tc := range []struct {
		code      string
		transient bool
		why       string
	}{
		{"28P01", false, "invalid_password: a rotated credential is permanent and needs a human"},
		{"3D000", false, "invalid_catalog_name: a dropped database is permanent and needs a human"},
		{"57P03", true, "cannot_connect_now: the server is starting up, and class 57 is allowlisted"},
		{"08006", true, "connection_failure carried in a ConnectError is still class 08"},
	} {
		joined := errors.Join(bareConnect, wrapPgError(tc.code))
		assert.Equal(t, tc.transient, cascadeErrorIsRecognisedTransient(joined),
			"SQLSTATE %s inside a ConnectError — %s", tc.code, tc.why)
	}

	// Paired control for THIS claim: the same bare ConnectError with NO SQLSTATE beside it is still
	// transient, so the assertions above are driven by the SQLSTATE and not by having broken the
	// ConnectError branch outright.
	assert.True(t, cascadeErrorIsRecognisedTransient(bareConnect),
		"a ConnectError with no SQLSTATE must remain transient")
}

// A nil error is not a failure at all. Asserted because the call site's guard is `err != nil` and a
// classifier that answered "not transient" for nil would record a residue on every SUCCESSFUL
// deletion if that guard were ever refactored away.
func TestCascadeErrorIsRecognisedTransient_NilIsTransient(t *testing.T) {
	assert.True(t, cascadeErrorIsRecognisedTransient(nil))
}

// The action string this writes must fit audit_logs.action, VARCHAR(100) (migration 000003). The new
// effect is the longest in the vocabulary, so a future longer one would fail loudly at INSERT time in
// production and only there; this catches it in the unit suite.
func TestDeletionResidueEffects_FitTheAuditLogsActionColumn(t *testing.T) {
	for _, effect := range []repository.DeletionResidueEffect{
		repository.DeletionResiduePaddleSubscriptionCancelled,
		repository.DeletionResiduePaddleSubscriptionCancelledCascadeFailed,
	} {
		assert.LessOrEqual(t, len("user.deletion_residue."+string(effect)), 100,
			"audit_logs.action is VARCHAR(100)")
	}
}
