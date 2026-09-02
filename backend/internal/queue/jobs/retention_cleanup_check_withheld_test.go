package jobs

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// ─── Issue #1569 T1 (defect C1): monitorRetentionDeletionsWithheld, Step 2b ─────────────────────
//
// THE HALF THAT IS NOT OPTIONAL. The gate in GetUsersWithExpiredRetentionPage /
// retentionDeletionClaimPredicate stops an unwarned account being hard-deleted. On its own that
// converts one silent failure (deleted with no warning) into another (retained forever with nothing
// reporting it), because in every environment today NO EmailService is passed to this worker, so
// canSendRetentionNotice returns false before the work-claim and the markers can never be stamped.
// This pass is what makes the held population visible.
//
// ⚠ NO MOCK EMAIL SERVICE IS INJECTED IN THIS FILE, and that is the point. Every pre-existing test
// of this worker stubs one, which is exactly why nothing could see the production shape: with a
// mock present the notices "send", the markers get stamped, and the defect is invisible. These
// tests construct the worker the way main.go does — EmailService left nil.
//
// ⚠ WHY THE REASON LABELS EXIST. `retention_notice_*_sent_at IS NULL` means two different things.
// While retention_expires_at > NOW() the notice sweep can still reach the user (both
// GetUsersNeedingRetention30dNotice and its 7d sibling bound their population by exactly that), so
// the state is healthy and transient: notice_pending. Once the clock has run out those queries can
// never select the user again, so the missing notice will not arrive by any existing path and the
// gate holds the row indefinitely: notice_impossible_*. An alert on the undifferentiated NULL count
// would fire on every ordinary cancellation and would be worthless.

// newWithheldMonitorWorker builds the worker with NO email service, the way cmd/server/main.go
// wires it in production today.
func newWithheldMonitorWorker(logBuf *bytes.Buffer, mockRepo *MockRetentionProfileRepository) *RetentionCleanupCheckWorker {
	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     mockRepo,
		DeletionService: new(MockRetentionDeletionService),
		RetentionMonths: 6,
		Logger:          slog.New(slog.NewTextHandler(logBuf, nil)),
	})
	worker.SetPlaidCleanup(new(MockRetentionPlaidCleanup))
	return worker
}

// setupWithheldMonitorMocks stubs every OTHER step of Work() as a no-op so each assertion is about
// Step 2b alone.
func setupWithheldMonitorMocks(ctx context.Context, mockRepo *MockRetentionProfileRepository) {
	mockRepo.On("GetUsersNeedingRetentionClockRepair", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("StampMissingPlaidCleanupMarkers", ctx).Return(0, nil)
	mockRepo.On("ClearCompletedPlaidCleanupMarkers", ctx).Return(0, nil)
	mockRepo.On("ClearMarkersOnEligibleStatus", ctx).Return(0, nil)
	mockRepo.On("GetUsersWithMalformedTierOverride", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetOrphanedFKlessUserScopedRowCounts", ctx).Return(repository.OrphanedFKlessUserScopedRowCounts{}, nil)
	mockRepo.On("GetUsersNeedingPlaidDisconnectRepair", ctx, plaidDisconnectRepairGracePeriod).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention30dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersNeedingRetention7dNotice", ctx).Return([]*domain.User{}, nil)
	mockRepo.On("GetUsersWithExpiredRetentionPage", ctx, mock.Anything, mock.Anything).Return([]*domain.User{}, nil)
}

// resetWithheldGauges clears every reason child so a reading left by a sibling test cannot be
// mistaken for this test's own publication.
func resetWithheldGauges() {
	observability.RetentionDeletionsWithheld.Reset()
}

// THE CORE ASSERTION FOR T1's ALERT HALF: with no email service wired and accounts held by the
// gate, the withheld population reaches Prometheus under the reason that names the actual remedy,
// and it is logged at WARN as well.
func TestRetentionCleanupCheck_MonitorWithheld_NoEmailService_AlertsAsImpossible(t *testing.T) {
	ctx := context.Background()
	resetWithheldGauges()

	mockRepo := new(MockRetentionProfileRepository)
	setupWithheldMonitorMocks(ctx, mockRepo)
	mockRepo.On("CountRetentionDeletionsWithheld", ctx).Return(repository.RetentionWithholdingCounts{
		WithheldNoticeMissing: 4,
		NoticePending:         9,
	}, nil)

	var logBuf bytes.Buffer
	worker := newWithheldMonitorWorker(&logBuf, mockRepo)
	require.Nil(t, worker.emailService,
		"this test is only meaningful against the production wiring — a mock here would restore the very blind spot that hid the defect")

	require.NoError(t, worker.Work(ctx, newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})))

	assert.Equal(t, float64(4),
		testutil.ToFloat64(observability.RetentionDeletionsWithheld.WithLabelValues(observability.RetentionWithheldNoEmailService)),
		"with no email service wired, every withheld account is held for that one process-wide reason, and wiring the service is the whole remedy")
	assert.Equal(t, float64(0),
		testutil.ToFloat64(observability.RetentionDeletionsWithheld.WithLabelValues(observability.RetentionWithheldWindowPassed)),
		"window_passed means a service IS wired and still cannot help; attributing this population to it would point an operator at the wrong remedy")

	// ⚠ THE ASSERTION THAT REFUTES THE CONFLATION. 9 healthy accounts are awaiting a notice that is
	// still ahead of them; reporting them under any notice_impossible reason would fire the alert on
	// every ordinary cancellation. A pass that published one undifferentiated NULL-marker count
	// cannot satisfy this line and the line above at the same time.
	assert.Equal(t, float64(9),
		testutil.ToFloat64(observability.RetentionDeletionsWithheld.WithLabelValues(observability.RetentionWithheldNoticePending)),
		"a user whose retention clock is still running is not withheld — the notice sweep can still reach them")

	logged := logBuf.String()
	// ⚠ Asserting only `level=WARN` here would be VACUOUS: the orphaned-Plaid-Item pass logs a WARN
	// on every run of this fixture (no repository is wired), so a bare level check stays green even
	// with Step 2b removed entirely. The assertion names THIS pass's own message.
	assert.Contains(t, logged, `level=WARN msg="retention hard-deletions are being WITHHELD`,
		"a gauge nobody has wired an alert to is still invisible; this repo has no alert-rule file, so the log line is the other half")
	assert.Contains(t, logged, "withheld_no_email_service=4")

	mockRepo.AssertExpectations(t)
}

// THE OTHER BRANCH, which had no coverage at all in the first version of this file: with an email
// service WIRED, the same withheld population must report under window_passed instead — because
// wiring the service is necessary but NOT sufficient. GetUsersNeedingRetention30dNotice and its 7d
// sibling both require retention_expires_at > NOW(), and every withheld account is past that, so a
// wired service still cannot reach them. The two labels name two different operator actions
// (deploy an email service vs. build a selection path for the backlog), so reporting one population
// under the other would send an operator to the wrong remedy.
//
// ⚠ This is the ONE test in this file that injects an email service, and it does so precisely
// because the branch under test is "a service exists" — not to make notices succeed.
func TestRetentionCleanupCheck_MonitorWithheld_EmailServiceWired_AlertsAsWindowPassed(t *testing.T) {
	ctx := context.Background()
	resetWithheldGauges()

	mockRepo := new(MockRetentionProfileRepository)
	setupWithheldMonitorMocks(ctx, mockRepo)
	mockRepo.On("CountRetentionDeletionsWithheld", ctx).Return(repository.RetentionWithholdingCounts{
		WithheldNoticeMissing: 6,
		NoticePending:         2,
	}, nil)

	var logBuf bytes.Buffer
	worker := newWithheldMonitorWorker(&logBuf, mockRepo)
	worker.emailService = new(MockRetentionEmailService)
	require.NotNil(t, worker.emailService)

	require.NoError(t, worker.Work(ctx, newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})))

	assert.Equal(t, float64(6),
		testutil.ToFloat64(observability.RetentionDeletionsWithheld.WithLabelValues(observability.RetentionWithheldWindowPassed)),
		"with a service wired the whole withheld population is past the notice queries' reach, and that is a different remedy from wiring the service")
	assert.Equal(t, float64(0),
		testutil.ToFloat64(observability.RetentionDeletionsWithheld.WithLabelValues(observability.RetentionWithheldNoEmailService)),
		"a service IS wired, so blaming its absence would be a false diagnosis")
	assert.Equal(t, float64(2),
		testutil.ToFloat64(observability.RetentionDeletionsWithheld.WithLabelValues(observability.RetentionWithheldNoticePending)),
		"the healthy population is unaffected by which impossible-reason applies")

	assert.Contains(t, logBuf.String(), "withheld_window_passed=6")
	assert.Contains(t, logBuf.String(), "withheld_no_email_service=0")
}

// The clean case must CLEAR the alert, not latch it: an operator who wires the email service and
// lets the backlog drain has to see the reading go to zero. A gauge only ever written when non-zero
// would hold the alarm on forever, which is this branch's signature defect (a mark that outlives the
// work) in alerting form.
func TestRetentionCleanupCheck_MonitorWithheld_NothingWithheld_PublishesZeroAndDoesNotWarn(t *testing.T) {
	ctx := context.Background()
	resetWithheldGauges()
	observability.RetentionDeletionsWithheld.WithLabelValues(observability.RetentionWithheldNoEmailService).Set(7)

	mockRepo := new(MockRetentionProfileRepository)
	setupWithheldMonitorMocks(ctx, mockRepo)
	mockRepo.On("CountRetentionDeletionsWithheld", ctx).Return(repository.RetentionWithholdingCounts{}, nil)

	var logBuf bytes.Buffer
	worker := newWithheldMonitorWorker(&logBuf, mockRepo)

	require.NoError(t, worker.Work(ctx, newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})))

	assert.Equal(t, float64(0),
		testutil.ToFloat64(observability.RetentionDeletionsWithheld.WithLabelValues(observability.RetentionWithheldNoEmailService)),
		"a drained backlog must clear the alert, or the gauge latches forever")
	assert.NotContains(t, logBuf.String(), "retention hard-deletions are being WITHHELD",
		"nothing is withheld, so there is nothing to warn about")
}

// A failed count must publish NOTHING rather than zeroes. Setting the gauges to 0 here would tell an
// alert "no account is being withheld", which is a claim nothing checked — the same unknown-is-not-
// zero rule monitorOrphanedPlaidItems and monitorMalformedTierOverrides already apply. The gauges
// keep their last reading and staleness/absent() plus the Error log report the breakage.
func TestRetentionCleanupCheck_MonitorWithheld_CountFails_LeavesGaugesUntouched(t *testing.T) {
	ctx := context.Background()
	resetWithheldGauges()
	observability.RetentionDeletionsWithheld.WithLabelValues(observability.RetentionWithheldNoEmailService).Set(5)

	mockRepo := new(MockRetentionProfileRepository)
	setupWithheldMonitorMocks(ctx, mockRepo)
	mockRepo.On("CountRetentionDeletionsWithheld", ctx).
		Return(repository.RetentionWithholdingCounts{}, errors.New("boom"))

	var logBuf bytes.Buffer
	worker := newWithheldMonitorWorker(&logBuf, mockRepo)

	// Non-fatal for the same reason every other monitoring step here is: an observability check must
	// never block GDPR notices or expired-account deletion.
	require.NoError(t, worker.Work(ctx, newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})))

	assert.Equal(t, float64(5),
		testutil.ToFloat64(observability.RetentionDeletionsWithheld.WithLabelValues(observability.RetentionWithheldNoEmailService)),
		"a failed read must not overwrite the last real reading with a fabricated zero")
	assert.Contains(t, logBuf.String(), "level=ERROR")
}
