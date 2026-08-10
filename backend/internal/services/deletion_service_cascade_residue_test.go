package services

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
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

// SIBLING ONE, MANDATORY — this is the assertion that stops the ..._cascade_failed kind from
// becoming "raised on every cascade error", which is exactly what b76e1b8a's reasoning rules out. A
// deadlock (40P01) or a serialization failure (40001) is the transient case the existing rule was
// written for: the claim is released, the next run retries the whole sequence, and the deletion is
// still expected to complete, so telling an operator "and the database is broken" would be false.
//
// ⚠ ROUND 15, FINDING A4 — WHAT THIS TEST USED TO ASSERT AND WHY IT CHANGED. It used to assert that
// NOTHING at all was recorded on a transient cascade error. Round 14's argument for that silence was
// sound under its own premise — a permanent artifact is wrong for an incident that resolves itself,
// BECAUSE THE RETRY RECONCILES — but nothing implemented the premise, and it fails outright when no
// retry is ever authorised: if the user cancels their deletion, or resubscribes, before the next
// sweep, neither sweep selects them again and no operator work item exists anywhere. The user is left
// with no subscription and no trace but a log line.
//
// Round 15 makes the premise true rather than assumed: the landing records are written when each
// irreversible call is issued, and DeleteUserCascade deletes them inside the transaction that
// deletes the user — so a successful retry retracts them itself. The decision is therefore RECORD ON
// TRANSIENT ERRORS TOO, and round 14's classifier is kept doing the job it was actually designed
// for: separating "the deletion is over" from "the deletion is STUCK", which is the extra
// ..._cascade_failed row and nothing else.
//
// ⚠ THE ASSERTION IS MEMBERSHIP, NOT A COUNT. It names the effect that must be present and the one
// that must be absent, so a change that broadened or narrowed recording cannot satisfy it by moving
// both together.
func TestExecuteDeletion_CascadeFailsTransientlyAfterPaddleCancel_StillHasTheLandingRecord(t *testing.T) {
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
			assert.Contains(t, *h.callOrder, "PaddleCancel", "the cancel really did land — this is the case the record is about")
			h.repo.AssertCalled(t, "RecordDeletionResidue", mock.Anything, h.userID,
				repository.DeletionResiduePaddleSubscriptionCancelled)
			h.repo.AssertNotCalled(t, "RecordDeletionResidue", mock.Anything, mock.Anything,
				repository.DeletionResiduePaddleSubscriptionCancelledCascadeFailed)
		})
	}
}

// A cancelled or timed-out context is transient in the same sense and is not a PgError at all, so it
// exercises the non-SQLSTATE half of the classifier. This is the shutdown case: the same deadline
// that killed the cascade also killed the job, and the next run will re-do the whole sequence — but
// the landing records are on file either way, and this is the case that proves it, since the context
// they would naively have been written on is the dead one (finding A5).
func TestExecuteDeletion_CascadeFailsOnContextCancellation_StillHasTheLandingRecord(t *testing.T) {
	ctx := context.Background()
	h := cascadeErrorHarness(t, ctx, fmt.Errorf("DeleteUserCascade: begin: %w", context.DeadlineExceeded))

	require.Error(t, h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested))
	h.repo.AssertCalled(t, "RecordDeletionResidue", mock.Anything, h.userID,
		repository.DeletionResiduePaddleSubscriptionCancelled)
	h.repo.AssertNotCalled(t, "RecordDeletionResidue", mock.Anything, mock.Anything,
		repository.DeletionResiduePaddleSubscriptionCancelledCascadeFailed)
}

// SIBLING TWO, MANDATORY — the record must follow the EFFECT, not the failure.
//
// ⚠ THIS TEST PASSED WITH A FALSE JUSTIFICATION, AND THAT IS ROUND 15's FINDING A1. It used to
// assert that a cascade error on a user whose SUBSCRIPTION was never cancelled recorded nothing, on
// the stated grounds that "nothing irreversible happened to them". Read the harness: raw status
// "cancelled" skips the cancel, and then ArchiveAndAnonymizeCustomer still runs — the act
// cleanupPaddleResources itself labels "THE ONE ACTION NOTHING CAN UNDO". Something irreversible
// happened to them; the mechanism simply had no vocabulary for it, so the test was pinning the
// defect as if it were the requirement.
//
// ⚠ IT IS NOT THE SIBLING OF TestExecuteDeletion_NoSubscriptionCancelled_RecordsNoResidueOnRevocation,
// though the two look near-identical. That one aborts at the archive GATE — the archive never
// happens — so its "records nothing" is correct and it is left exactly as it was.
func TestExecuteDeletion_CascadeFailsWithNoSubscriptionCancelled_RecordsTheArchiveResidue(t *testing.T) {
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
	h.repo.AssertCalled(t, "RecordDeletionResidue", mock.Anything, h.userID,
		repository.DeletionResiduePaddleCustomerArchived)
	// No subscription was cancelled, so neither subscription kind may be claimed — the record still
	// follows the effect, which is what the original test was reaching for and got wrong.
	h.repo.AssertNotCalled(t, "RecordDeletionResidue", mock.Anything, mock.Anything,
		repository.DeletionResiduePaddleSubscriptionCancelled)
	h.repo.AssertNotCalled(t, "RecordDeletionResidue", mock.Anything, mock.Anything,
		repository.DeletionResiduePaddleSubscriptionCancelledCascadeFailed)
	// ⚠ ROUND 15, TIER E ITEM 8 — AND IT MUST ALSO GET A *STUCK* ROW. 42501 is not a recognised
	// transient error, so this deletion is not over: it will re-run and fail the same way until the
	// database fault is fixed. Before this, the stuck row was gated on the subscription cancel alone,
	// so an operator filtering for stuck deletions saw every ex-subscriber's and none of these —
	// even though the archive-only shape is the DETERMINISTIC retention-sweep case, not an exotic one.
	h.repo.AssertCalled(t, "RecordDeletionResidue", mock.Anything, h.userID,
		repository.DeletionResiduePaddleCustomerArchivedCascadeFailed)
}

// ⚠ SIBLING, MANDATORY — the boundary of item 8's fix, and the one that stops it from becoming
// "record a stuck row on every cascade error". A RECOGNISED-TRANSIENT failure after an archive-only
// run must get its landing record and NO stuck row: the retry premise genuinely holds there, and
// DeleteUserCascade retracts the landing record inside the transaction that finally succeeds. This
// is the assertion the widened branch could otherwise have satisfied by dropping the classifier.
func TestExecuteDeletion_CascadeFailsTransientlyAfterArchiveOnly_RecordsNoStuckRow(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarnessWithStatus(t, context.Background(), "cancelled")

	h.repo.ExpectedCalls = nil
	h.repo.On("GetUserByID", ctx, h.userID).Return(h.user, nil)
	h.repo.On("ClaimForDeletion", ctx, h.userID).Return(true, nil)
	h.repo.On("RecordDeletionResidue", mock.Anything, h.userID, mock.Anything).Return(nil).Maybe()
	// 40001 (serialization_failure) is the classifier's canonical recognised-transient case.
	h.repo.On("DeleteUserCascade", ctx, h.userID, mock.Anything).Return(false, wrapPgError("40001"))

	require.Error(t, h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested))

	h.repo.AssertCalled(t, "RecordDeletionResidue", mock.Anything, h.userID,
		repository.DeletionResiduePaddleCustomerArchived)
	h.repo.AssertNotCalled(t, "RecordDeletionResidue", mock.Anything, mock.Anything,
		repository.DeletionResiduePaddleCustomerArchivedCascadeFailed)
}

// ⚠ SIBLING, MANDATORY — the OTHER direction. A run that cancelled AND archived is stuck on both
// counts and must get BOTH stuck rows: the operator has a subscription to restore and a Paddle
// customer they can no longer recover, and collapsing either into the other hides one of the two.
func TestExecuteDeletion_CascadeFailsAfterBothPaddleEffects_RecordsBothStuckRows(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)

	h.repo.ExpectedCalls = nil
	h.repo.On("GetUserByID", ctx, h.userID).Return(h.user, nil)
	h.repo.On("ClaimForDeletion", ctx, h.userID).Return(true, nil)
	h.repo.On("RecordDeletionResidue", mock.Anything, h.userID, mock.Anything).Return(nil).Maybe()
	h.repo.On("DeleteUserCascade", ctx, h.userID, mock.Anything).Return(false, wrapPgError("42501"))

	require.Error(t, h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested))

	h.repo.AssertCalled(t, "RecordDeletionResidue", mock.Anything, h.userID,
		repository.DeletionResiduePaddleSubscriptionCancelledCascadeFailed)
	h.repo.AssertCalled(t, "RecordDeletionResidue", mock.Anything, h.userID,
		repository.DeletionResiduePaddleCustomerArchivedCascadeFailed)
}

// SIBLING THREE, MANDATORY — the ordinary success path. Its counterpart for the revocation half is
// TestExecuteDeletion_OrdinarySuccess_RecordsNoResidue; this one is stated separately because the new
// branch is on the ERROR return of the same call and a classifier that recorded on `err == nil` would
// pass that test and fail this one. The "leaves no row at all" half is
// TestExecuteDeletion_Integration_OrdinarySuccess_LeavesNoResidueRow, on a real Postgres — see that
// test for why a mock cannot express it.
func TestExecuteDeletion_OrdinarySuccess_RecordsNoCascadeFailedResidue(t *testing.T) {
	ctx := context.Background()
	h := newRevocationHarness(t, ctx)

	require.NoError(t, h.svc.ExecuteDeletion(ctx, h.userID, repository.DeletionKindUserRequested))

	assert.Contains(t, *h.callOrder, "DeleteUserCascade")
	h.repo.AssertNotCalled(t, "RecordDeletionResidue", mock.Anything, mock.Anything,
		repository.DeletionResiduePaddleSubscriptionCancelledCascadeFailed)
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
	// Scoped to the POST-cascade effect on purpose. The PRE-issue writes are fail-closed since
	// the EE re-review — a failure there refuses the Paddle call and the run never reaches the
	// cascade at all — so failing every residue write indiscriminately would make this test
	// assert the wrong abort. What is under test here is the post-hoc write, which stays
	// best-effort because the external work has already landed and cannot be un-issued.
	h.repo.On("RecordDeletionResidue", mock.Anything, h.userID,
		repository.DeletionResiduePaddleSubscriptionCancelledCascadeFailed).
		Return(errors.New("audit insert failed"))
	h.repo.On("RecordDeletionResidue", mock.Anything, h.userID, mock.Anything).Return(nil)
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

// ⚠ THE SQLSTATE LEG MUST NOT BE DECIDED BY WHICHEVER PgError THE WALK REACHES FIRST.
// Round 14, finding CC-2. The leg used to be a bare `errors.As`, which for a tree containing
// `Unwrap() []error` binds the FIRST assignable node in slice order and never looks at the rest —
// the same first-match unsoundness the round-13 fifth pass removed from the ConnectError leg's
// net.Error check (see anyErrorInTree's doc comment). A chain carrying a transient SQLSTATE from one
// host and a PERMANENT one from another therefore answered "transient" whenever the transient one
// happened to come first, suppressing the residue for a fault no retry clears.
//
// The corrected rule is deliberately NOT the sibling leg's quantifier. The ConnectError leg says
// "any transient node wins"; this leg says "any NON-transient SQLSTATE loses". Both are the
// fail-closed reading of their own evidence, and they differ because the evidence differs: a socket
// errno describes one host's reachability and says nothing about the others, whereas a server-issued
// SQLSTATE like 28P01 invalid_password or 3D000 invalid_catalog_name is a statement about the
// DEPLOYMENT that a second host being briefly refused does not soften. Adopting "any transient node
// wins" here would suppress the residue in exactly the 28P01 case the round-13 re-review restored it
// for. The change can therefore only ever turn a `true` into a `false` — i.e. add a spurious residue
// row, never remove a real one, which is the direction the parent function's doc comment argues for
// at length.
//
// ⚠ WHAT THESE FIXTURES PROVE, AND WHAT THEY DO NOT — stated the way round 13 stated it for its own,
// because the limitation is inherited rather than avoided. `errors.Join` gives the two per-host
// errors as SIBLINGS under a synthetic root, with each PgError nested one level down inside
// wrapPgError's `fmt.Errorf`. So these fixtures DO exercise the walk descending into every branch of
// an `Unwrap() []error` node, and they DO fix the sibling order the old first-match depended on.
// They do NOT reproduce pgconn's containment exactly: production nests each host's PgError beneath
// that host's *pgconn.ConnectError, and ConnectError's cause field is unexported, so no external
// package can build that shape (the same wall documented at
// TestConnectErrorIsRecognisedTransient_AllowlistsCausesRatherThanTheShape). The fixture is a
// faithful model of the aggregation and an approximation of the per-host wrapper — not a proof of
// the latter.
//
// ⚠ REACHABILITY: a joined multi-host chain requires a multi-host DATABASE_URL, for which this repo
// has no evidence (every configured value — docker-compose, QA, CI, both fly.toml files — names a
// single host). The test pins the classifier's contract, not an observed production incident.
func TestCascadeErrorIsRecognisedTransient_AJoinedChainIsDecidedByItsPermanentSQLSTATE(t *testing.T) {
	permanent := wrapPgError("42501") // insufficient_privilege — never clears on retry
	transient := wrapPgError("57P03") // cannot_connect_now — clears when the server finishes starting

	// The ordering pair. Both orders must give the SAME answer; before the fix the first of these
	// returned true purely because the transient SQLSTATE was in slice position 0.
	assert.False(t, cascadeErrorIsRecognisedTransient(errors.Join(transient, permanent)),
		"a permanent SQLSTATE later in a joined chain must still record — first-match is not the rule")
	assert.False(t, cascadeErrorIsRecognisedTransient(errors.Join(permanent, transient)),
		"and the same chain in the other order must answer identically")

	// Paired control for THIS claim, not for the tool: the fix must not turn every joined chain into
	// a recording one. A chain whose SQLSTATEs are ALL transient stays silent, so the assertions
	// above are the permanent member doing the work and not the `Unwrap() []error` shape itself.
	assert.True(t, cascadeErrorIsRecognisedTransient(errors.Join(transient, wrapPgError("40001"))),
		"a joined chain of exclusively transient SQLSTATEs is still transient")

	// A PgError with no usable code carries no evidence either way and must not veto its sibling —
	// the pre-fix code fell through to the ConnectError leg in that case, and that behaviour is kept.
	assert.True(t, cascadeErrorIsRecognisedTransient(errors.Join(wrapPgError(""), transient)),
		"a codeless PgError is not evidence of permanence and must not suppress a transient sibling")
}

// ⚠ THE CANCELLATION/DEADLINE LEG MUST NOT PRE-EMPT THE PERMANENT-SQLSTATE VETO.
// Round 14, batch CE, found by the Codex adversarial review of 2740c4cb..50e6db93.
//
// CC-2 made the SQLSTATE leg walk the whole tree so that a permanent SQLSTATE anywhere in a joined
// multi-host chain vetoes a transient sibling. But the context.Canceled / context.DeadlineExceeded
// check sat ABOVE that walk and used `errors.Is`, which since Go 1.20 traverses `Unwrap() []error`
// too. So a chain containing BOTH a per-host dial timeout AND a permanent 28P01 short-circuited to
// "transient" at the deadline check and the SQLSTATE walk never ran — the residue was dropped for a
// fault that repeats on every retry. That is the failure direction the parent function's doc comment
// says must never happen, and it is reachable in exactly the shape CC-2 was written for: pgconn
// gives each candidate host its own connect-timeout context and joins every host's error
// (pgconn.go:251, pgconn.go:269 in pgx v5.9.2), so a timeout and an authentication failure end up in
// one tree.
//
// ⚠ AND THAT IS AS FAR AS THE FIXTURE GOES — corrected here rather than left standing, because the
// first draft of this comment said the two leaves "arrive as siblings" and the CE RE-REVIEW refuted
// it. They are COUSINS, not siblings: pgx v5.9.2 wraps the join in a ConnectError (pgconn.go:162),
// the join's children are per-dial wrappers (pgconn.go:316), and the timeout or startup PgError sits
// one level beneath each of those (pgconn.go:317, pgconn.go:483). `errors.Join` + `fmt.Errorf` here
// gives an EQUIVALENT abstract tree — both joined branches, plus unary wrappers the walk traverses
// transparently — and it proves the PRECEDENCE this test is about. It does not reproduce pgconn's
// containment, and it cannot: ConnectError's cause field is unexported, the same wall documented at
// TestCascadeErrorIsRecognisedTransient_AJoinedChainIsDecidedByItsPermanentSQLSTATE. Reachability
// still needs a multi-host DATABASE_URL for which this repo has no evidence; every configured value
// names a single host.
//
// The fix is ordering only: the SQLSTATE veto now runs first. Cancellation and deadline expiry with
// no permanent SQLSTATE in the chain remain transient, which is what the leg was for — pinned by the
// controls below. ⚠ Which control kills which mutation, stated rather than claimed collectively: the
// two BARE cases fail if the cancellation/deadline leg is deleted; the joined-with-40001 case fails
// if the hoisted veto is broadened to reject allowlisted classes. The third does NOT kill leg
// deletion — class 40 would still return true through the positive SQLSTATE branch — so it is a
// polarity control, not a second copy of the first two.
func TestCascadeErrorIsRecognisedTransient_DeadlineDoesNotMaskAPermanentSQLSTATE(t *testing.T) {
	timedOut := fmt.Errorf("dial host-a: %w", context.DeadlineExceeded)
	cancelled := fmt.Errorf("dial host-a: %w", context.Canceled)
	permanent := wrapPgError("28P01") // invalid_password — never clears on retry

	assert.False(t, cascadeErrorIsRecognisedTransient(errors.Join(timedOut, permanent)),
		"a per-host deadline must not suppress a permanent SQLSTATE elsewhere in the chain")
	assert.False(t, cascadeErrorIsRecognisedTransient(errors.Join(permanent, timedOut)),
		"and the same chain in the other order must answer identically")
	assert.False(t, cascadeErrorIsRecognisedTransient(errors.Join(cancelled, permanent)),
		"context.Canceled behaves the same way — the veto is on the SQLSTATE, not on the leg")

	// Paired controls for THIS claim rather than for the tool: the leg must still do its job when no
	// permanent SQLSTATE is present, or the assertions above would also pass if the leg were removed.
	assert.True(t, cascadeErrorIsRecognisedTransient(timedOut),
		"a bare deadline is still transient")
	assert.True(t, cascadeErrorIsRecognisedTransient(cancelled),
		"a bare cancellation is still transient")
	assert.True(t, cascadeErrorIsRecognisedTransient(errors.Join(timedOut, wrapPgError("40001"))),
		"a deadline joined with an exclusively transient SQLSTATE is still transient")
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
// ⚠ WHAT THIS TEST DOES AND DOES NOT MODEL, corrected after the round-13 Codex third pass called the
// original wording overstated — it was. errors.Join does NOT reproduce pgconn's containment shape:
// production nests the PgError INSIDE the ConnectError (ConnectError.Unwrap), whereas Join makes the
// two SIBLINGS under a new root (errors.Join's Unwrap() []error). errors.As searches the whole tree
// either way, which is why this works, and the precedence between the two branches is genuinely what
// it constrains — the reverted ordering fails it, executed. But it is a precedence test, not a
// production-shape test, and it is written here as one. The shape itself cannot be built from outside
// pgconn: ConnectError's cause field is unexported, so no external package can populate it.
// Note deliberately: the joined value's Error() is never called anywhere in this test, because
// ConnectError.Error() dereferences that unexported field.
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

	// Paired control for THIS claim: the same ConnectError with NO SQLSTATE beside it, carrying a
	// recognised-transient socket cause, is still transient — so the assertions above are driven by
	// the SQLSTATE and not by having broken the ConnectError branch outright.
	assert.True(t, cascadeErrorIsRecognisedTransient(errors.Join(bareConnect, syscall.ECONNREFUSED)),
		"a ConnectError with no SQLSTATE and a transient socket cause must remain transient")
}

// "NO SQLSTATE" IS NOT EVIDENCE OF TRANSIENCE, and the second version of this fix wrongly assumed it
// was. The round-13 Codex THIRD pass named the permanent no-SQLSTATE shapes: NXDOMAIN, an expired or
// untrusted TLS certificate, a hostname mismatch, an unsupported SASL/channel-binding configuration,
// a protocol the server will never speak. All of those reach the classifier as a *pgconn.ConnectError
// with no *pgconn.PgError anywhere in the chain, and none of them clear on retry — so classifying
// them transient would silently suppress the residue for a landed Paddle cancellation.
//
// The ConnectError leg is therefore an allowlist of RECOGNISED transient causes, in the same
// fail-closed shape as transientCascadeSQLSTATEClasses. This test asserts both directions.
//
// ⚠ WHAT `with` PROVES, stated plainly because the fourth pass caught the first version overclaiming
// the sibling test one function up and this one had the same flaw. errors.Join makes the ConnectError
// and the cause SIBLINGS; production nests the cause beneath ConnectError.Unwrap. The implementation
// searches the whole outer tree for a ConnectError and then, independently, the same tree for a
// recognised cause — so these assertions prove CO-OCCURRENCE, not that the cause belongs to the
// ConnectError. That is the behaviour under test and it is what the production code does, but it is
// weaker than a nested fixture would be, and a nested one cannot be built: ConnectError's cause field
// is unexported. TestCascadeErrorIsRecognisedTransient_ConnectionAttemptFailuresAreTransient covers
// one genuinely nested chain, from a real refused connection.
func TestConnectErrorIsRecognisedTransient_AllowlistsCausesRatherThanTheShape(t *testing.T) {
	bareConnect := &pgconn.ConnectError{Config: &pgconn.Config{}}
	with := func(cause error) error { return errors.Join(bareConnect, cause) }

	// TRANSIENT: the socket-level "not right now" set.
	for _, cause := range []error{
		syscall.ECONNREFUSED, syscall.ECONNRESET, syscall.EHOSTUNREACH,
		syscall.ENETUNREACH, syscall.ETIMEDOUT, syscall.EPIPE,
	} {
		assert.True(t, cascadeErrorIsRecognisedTransient(with(cause)),
			"errno %v is a recognised transient connection failure", cause)
	}
	assert.True(t, cascadeErrorIsRecognisedTransient(
		with(&net.DNSError{Err: "server misbehaving", IsTemporary: true})),
		"a TEMPORARY DNS failure is transient")
	assert.True(t, cascadeErrorIsRecognisedTransient(
		with(&net.DNSError{Err: "i/o timeout", IsTimeout: true})),
		"a DNS TIMEOUT is transient")

	// PERSISTENT: every shape the third pass named. None of these self-heal, so all must record.
	assert.False(t, cascadeErrorIsRecognisedTransient(
		with(&net.DNSError{Err: "no such host", IsNotFound: true})),
		"NXDOMAIN is a permanent answer, not a blip — verified against the real chain: pgconn.Connect "+
			"to a non-existent host yields a ConnectError carrying IsNotFound=true, IsTemporary=false")
	assert.False(t, cascadeErrorIsRecognisedTransient(
		with(x509.CertificateInvalidError{Reason: x509.Expired})),
		"an expired certificate never fixes itself")
	assert.False(t, cascadeErrorIsRecognisedTransient(
		with(x509.HostnameError{Host: "db.internal"})),
		"a certificate hostname mismatch never fixes itself")
	assert.False(t, cascadeErrorIsRecognisedTransient(
		with(x509.UnknownAuthorityError{})),
		"an untrusted CA never fixes itself")
	assert.False(t, cascadeErrorIsRecognisedTransient(
		with(errors.New("unsupported SASL authentication mechanism"))),
		"a permanent auth/protocol configuration failure must record, not be assumed transient")

	// A GRACEFUL startup disconnect — the shape a rolling restart or a failover actually produces, and
	// the allowlist's worst omission until the round-13 Codex fourth pass named it. pgproto3 turns a
	// connection closed mid-handshake into io.ErrUnexpectedEOF inside the ConnectError; it is neither
	// an errno nor a net.Error timeout, so before this it wrote FALSE PERMANENT RESIDUE on an ordinary
	// deploy.
	assert.True(t, cascadeErrorIsRecognisedTransient(with(io.ErrUnexpectedEOF)),
		"a graceful disconnect during the startup handshake is a restart, not a permanent fault")
	assert.True(t, cascadeErrorIsRecognisedTransient(with(io.EOF)),
		"the plain-EOF spelling of the same restart")

	// MULTI-HOST: pgconn joins the errors from EVERY attempted host into one ConnectError, so a chain
	// can hold an NXDOMAIN from a fallback host beside an ECONNREFUSED from the primary. That whole
	// case is recoverable and must not read permanent because of the fallback's DNS answer. The
	// earlier draft returned on the DNS branch first and got this wrong.
	assert.True(t, cascadeErrorIsRecognisedTransient(
		errors.Join(bareConnect, &net.DNSError{Err: "no such host", IsNotFound: true}, syscall.ECONNREFUSED)),
		"a mixed multi-host chain must be read by its recoverable half")
	// THE SAME MULTI-HOST CASE, BUT WITH A TIMEOUT INSTEAD OF AN ERRNO — which the first version of
	// the reordering still got wrong, and the round-13 Codex FIFTH pass caught. errors.As stops at the
	// FIRST match: a *net.DNSError is itself a net.Error, so `errors.As(err, &netErr)` bound the
	// NXDOMAIN (Timeout() == false) and the genuine timeout from the primary host, further along the
	// joined chain, was never examined. Everyday timeout, false permanent residue. Both timeout-shaped
	// predicates now walk the WHOLE tree instead of taking errors.As's first hit.
	assert.True(t, cascadeErrorIsRecognisedTransient(
		errors.Join(bareConnect,
			&net.DNSError{Err: "no such host", IsNotFound: true},
			&net.DNSError{Err: "i/o timeout", IsTimeout: true})),
		"a joined chain whose FIRST net.Error is NXDOMAIN must still see a later timeout")
	assert.True(t, cascadeErrorIsRecognisedTransient(
		errors.Join(bareConnect,
			&net.DNSError{Err: "no such host", IsNotFound: true},
			&net.OpError{Op: "dial", Err: &timeoutOnlyError{}})),
		"…including a non-DNS net.Error timeout after an NXDOMAIN")

	// …and the control that pairs with THAT claim: NXDOMAIN ALONE is still permanent, so the
	// assertion above is the errno doing the work and not the DNS branch having been broken.
	assert.False(t, cascadeErrorIsRecognisedTransient(
		errors.Join(bareConnect, &net.DNSError{Err: "no such host", IsNotFound: true})),
		"NXDOMAIN alone is still permanent")

	// The control that pairs with THIS claim rather than with the tool: the very same causes attached
	// to something that is NOT a ConnectError must still record, so the assertions above are the
	// ConnectError leg doing the work and not some unrelated errno matching.
	assert.False(t, cascadeErrorIsRecognisedTransient(
		fmt.Errorf("DeleteUserCascade: %w", syscall.ECONNREFUSED)),
		"ECONNREFUSED outside a ConnectError is not a recognised cascade transient")
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

// timeoutOnlyError is a net.Error that is a TIMEOUT and nothing else — no errno, no DNS fields — so
// an assertion using it can only pass through the generic net.Error timeout predicate.
type timeoutOnlyError struct{}

func (*timeoutOnlyError) Error() string   { return "i/o timeout" }
func (*timeoutOnlyError) Timeout() bool   { return true }
func (*timeoutOnlyError) Temporary() bool { return true }
