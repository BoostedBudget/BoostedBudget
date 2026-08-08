package jobs

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// ─── Issue #1497 item 4: monitorOrphanedPlaidItems — the orphaned-Plaid-Item alerting pass
// (Step 0e) ──────────────────────────────────────────────────────────────────────────────────────
//
// THIS IS THE ITEM #1497'S OWN TITLE NAMES, and two earlier passes over that ticket both missed it.
// plaid_orphaned_items.retry_attempt_count has been WRITTEN by RecordRetryFailure since #1497
// landed and was read by NO METRIC at all: jobs/plaid_orphan_retry.go emits only the generic
// JobsCompleted/JobDuration every worker emits, so an Item could be re-attempted indefinitely, or a
// row could sit needing a human for a month, with nothing anywhere reporting it.
//
// ⚠ THE HOME IS THIS WORKER, NOT THE RETRY SWEEP, and that is forced rather than chosen: the
// manual-intervention population is INVISIBLE to the retry sweep by construction —
// GetRetryablePage selects only rows with a live, unexpired credential, which is precisely the
// complement of the state this alert exists to report. A metric published from the sweep could
// never read anything but zero for the number that matters. This worker is also where this
// codebase already publishes DB-derived monitoring gauges (monitorMalformedTierOverrides Step 0c,
// monitorOrphanedFKlessRows Step 0d), and its cadence matches the retry sweep's own daily cadence,
// so a gauge over attempt counts is refreshed as often as the counts can change.

// MockOrphanedPlaidItemLister mocks the one-verb read this monitoring pass needs.
type MockOrphanedPlaidItemLister struct {
	mock.Mock
}

func (m *MockOrphanedPlaidItemLister) List(ctx context.Context, limit, offset int) ([]domain.PlaidOrphanedItem, int, error) {
	args := m.Called(ctx, limit, offset)
	items, _ := args.Get(0).([]domain.PlaidOrphanedItem)
	return items, args.Int(1), args.Error(2)
}

// setupOrphanMonitorMocks stubs every OTHER step of Work() as a no-op so each assertion below is
// about Step 0e alone.
func setupOrphanMonitorMocks(ctx context.Context, mockRepo *MockRetentionProfileRepository) {
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

func newOrphanMonitorWorker(lister OrphanedPlaidItemLister, logBuf *bytes.Buffer, mockRepo *MockRetentionProfileRepository) *RetentionCleanupCheckWorker {
	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:           mockRepo,
		DeletionService:       new(MockRetentionDeletionService),
		EmailService:          new(MockRetentionEmailService),
		RetentionMonths:       6,
		Logger:                slog.New(slog.NewTextHandler(logBuf, nil)),
		PlaidOrphanedItemRepo: lister,
	})
	worker.SetPlaidCleanup(new(MockRetentionPlaidCleanup))
	return worker
}

// orphanRowLive is auto_retrying at `now`.
func orphanRowLive(now time.Time, attempts int) domain.PlaidOrphanedItem {
	lastRetry := now.Add(-20 * time.Minute)
	return domain.PlaidOrphanedItem{
		ID:                "live",
		AccessToken:       "tok",
		TokenExpiresAt:    now.Add(20 * 24 * time.Hour),
		OrphanedAt:        now.Add(-1 * 24 * time.Hour),
		LastRetryAt:       &lastRetry,
		RetryAttemptCount: attempts,
	}
}

// orphanRowNeedsHuman is manual_intervention_required at `now` (credential past its deadline).
func orphanRowNeedsHuman(now time.Time, attempts int) domain.PlaidOrphanedItem {
	lastRetry := now.Add(-2 * time.Hour)
	return domain.PlaidOrphanedItem{
		ID:                "needs-human",
		TokenExpiresAt:    now.Add(-2 * time.Hour),
		OrphanedAt:        now.Add(-31 * 24 * time.Hour),
		LastRetryAt:       &lastRetry,
		RetryAttemptCount: attempts,
	}
}

// orphanRowDone is resolved at `now`.
func orphanRowDone(now time.Time, attempts int) domain.PlaidOrphanedItem {
	resolved := now.Add(-4 * time.Hour)
	return domain.PlaidOrphanedItem{
		ID:                "done",
		TokenExpiresAt:    now.Add(-4 * time.Hour),
		OrphanedAt:        now.Add(-33 * 24 * time.Hour),
		ResolvedAt:        &resolved,
		RetryAttemptCount: attempts,
	}
}

// THE CORE ASSERTION FOR ITEM 4: the attempt count and the manual-intervention state both reach
// Prometheus, with the right numbers, from a seeded population.
func TestRetentionCleanupCheck_MonitorOrphanedPlaidItems_PublishesStateAndAttemptGauges(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	mockRepo := new(MockRetentionProfileRepository)
	setupOrphanMonitorMocks(ctx, mockRepo)

	lister := new(MockOrphanedPlaidItemLister)
	items := []domain.PlaidOrphanedItem{
		orphanRowLive(now, 4),
		orphanRowNeedsHuman(now, 23),
		orphanRowNeedsHuman(now, 1),
		orphanRowNeedsHuman(now, 1),
		orphanRowDone(now, 7),
	}
	lister.On("List", mock.Anything, domain.PlaidOrphanedItemHealthScanCap, 0).Return(items, len(items), nil)

	var logBuf bytes.Buffer
	worker := newOrphanMonitorWorker(lister, &logBuf, mockRepo)

	require.NoError(t, worker.Work(ctx, newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})))

	assert.Equal(t, float64(1), testutil.ToFloat64(observability.PlaidOrphanedItemsByAdminState.WithLabelValues("auto_retrying")))
	assert.Equal(t, float64(3), testutil.ToFloat64(observability.PlaidOrphanedItemsByAdminState.WithLabelValues("manual_intervention_required")),
		"three Items nothing automated can finish — each one may still be billing an erased user")
	assert.Equal(t, float64(1), testutil.ToFloat64(observability.PlaidOrphanedItemsByAdminState.WithLabelValues("resolved")))

	// ⚠ THE SIBLING THAT REFUTES THE CONFLATION. 1, 3 and 4 (the unresolved total) are all
	// distinct here, so a pass that published one "unresolved" gauge for both actionable states
	// cannot satisfy these three assertions. auto_retrying means DO NOTHING; manual_intervention_
	// required means GO REMOVE THIS ITEM AT PLAID.
	assert.NotEqual(t,
		testutil.ToFloat64(observability.PlaidOrphanedItemsByAdminState.WithLabelValues("auto_retrying")),
		testutil.ToFloat64(observability.PlaidOrphanedItemsByAdminState.WithLabelValues("manual_intervention_required")))

	// Item 4's own number: the worst UNRESOLVED attempt count, 23 — not the resolved row's 7 and
	// not the last row scanned.
	assert.Equal(t, float64(23), testutil.ToFloat64(observability.PlaidOrphanedItemMaxRetryAttempts))

	assert.Contains(t, logBuf.String(), "level=WARN",
		"outstanding manual-intervention rows must be logged too — a gauge nobody has an alert on is still invisible in the logs")

	lister.AssertExpectations(t)
}

// THE HEALTHY STEADY STATE, and the sibling that makes the test above falsifiable: a clean table
// must RESET the gauges rather than leave a prior pass's non-zero reading latched. A gauge that only
// ever rises is the "mark outlives the work" defect in alerting form — an operator who fixes every
// Item would never see the alert clear.
func TestRetentionCleanupCheck_MonitorOrphanedPlaidItems_CleanTableResetsTheGauges(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockRetentionProfileRepository)
	setupOrphanMonitorMocks(ctx, mockRepo)

	lister := new(MockOrphanedPlaidItemLister)
	lister.On("List", mock.Anything, domain.PlaidOrphanedItemHealthScanCap, 0).
		Return([]domain.PlaidOrphanedItem(nil), 0, nil)

	var logBuf bytes.Buffer
	worker := newOrphanMonitorWorker(lister, &logBuf, mockRepo)

	// Prime both gauges non-zero, modelling a PRIOR pass that found work, so this proves a reset
	// rather than "never touched".
	observability.PlaidOrphanedItemsByAdminState.WithLabelValues("manual_intervention_required").Set(9)
	observability.PlaidOrphanedItemMaxRetryAttempts.WithLabelValues().Set(41)

	require.NoError(t, worker.Work(ctx, newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})))

	assert.Equal(t, float64(0), testutil.ToFloat64(observability.PlaidOrphanedItemsByAdminState.WithLabelValues("manual_intervention_required")),
		"an empty table must clear the gauge, or the alert never resolves once it has fired")
	assert.Equal(t, float64(0), testutil.ToFloat64(observability.PlaidOrphanedItemMaxRetryAttempts),
		"and the attempt gauge likewise — a latched 41 would keep paging someone about an Item that no longer exists")
	assert.NotContains(t, logBuf.String(), "level=WARN")
}

// NON-FATAL, mirroring Step 0c/0d: this pass makes no write, so a read failure must not block the
// GDPR 30-day/7-day notices or expired-account deletion that run after it.
func TestRetentionCleanupCheck_MonitorOrphanedPlaidItems_ListFailure_IsNonFatal(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockRetentionProfileRepository)
	setupOrphanMonitorMocks(ctx, mockRepo)

	lister := new(MockOrphanedPlaidItemLister)
	lister.On("List", mock.Anything, domain.PlaidOrphanedItemHealthScanCap, 0).
		Return([]domain.PlaidOrphanedItem(nil), 0, errors.New("decrypt plaid_item_id: cipher: message authentication failed"))

	var logBuf bytes.Buffer
	worker := newOrphanMonitorWorker(lister, &logBuf, mockRepo)

	require.NoError(t, worker.Work(ctx, newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})),
		"an observability read failure must not fail the job that also sends GDPR notices and deletes expired accounts")

	// SIBLING: the later steps really did run, so "non-fatal" is proved rather than asserted.
	mockRepo.AssertCalled(t, "GetUsersNeedingRetention30dNotice", ctx)
	mockRepo.AssertCalled(t, "GetUsersWithExpiredRetentionPage", ctx, mock.Anything, mock.Anything)
	assert.Contains(t, logBuf.String(), "level=ERROR", "the failure must be visible, not swallowed")
}

// ⚠ A FAILED READ MUST NOT PUBLISH A ZERO. This is the Prometheus-side equivalent of
// PlaidOrphanedItemMetrics.Available: setting the gauges to 0 on a failed read would tell an alert
// "nothing needs a human", which is a claim nothing checked. The gauges are left at their previous
// value so `absent()`/staleness — and the Error log — are what report the failure.
func TestRetentionCleanupCheck_MonitorOrphanedPlaidItems_ListFailure_DoesNotPublishAConfidentZero(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockRetentionProfileRepository)
	setupOrphanMonitorMocks(ctx, mockRepo)

	lister := new(MockOrphanedPlaidItemLister)
	lister.On("List", mock.Anything, domain.PlaidOrphanedItemHealthScanCap, 0).
		Return([]domain.PlaidOrphanedItem(nil), 0, errors.New("connection reset by peer"))

	var logBuf bytes.Buffer
	worker := newOrphanMonitorWorker(lister, &logBuf, mockRepo)

	observability.PlaidOrphanedItemsByAdminState.WithLabelValues("manual_intervention_required").Set(6)
	observability.PlaidOrphanedItemMaxRetryAttempts.WithLabelValues().Set(15)

	require.NoError(t, worker.Work(ctx, newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})))

	assert.Equal(t, float64(6), testutil.ToFloat64(observability.PlaidOrphanedItemsByAdminState.WithLabelValues("manual_intervention_required")),
		"a failed read must not overwrite the last known reading with a reassuring zero")
	assert.Equal(t, float64(15), testutil.ToFloat64(observability.PlaidOrphanedItemMaxRetryAttempts))
}

// THE SIZE TRIPWIRE, Prometheus side. Past the cap the page in hand is a truncated view, so
// publishing counts from it would understate the manual-intervention population — the one direction
// this gauge must never err in.
func TestRetentionCleanupCheck_MonitorOrphanedPlaidItems_PastTheScanCap_PublishesNothingAndLogsLoudly(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	mockRepo := new(MockRetentionProfileRepository)
	setupOrphanMonitorMocks(ctx, mockRepo)

	lister := new(MockOrphanedPlaidItemLister)
	lister.On("List", mock.Anything, domain.PlaidOrphanedItemHealthScanCap, 0).
		Return([]domain.PlaidOrphanedItem{orphanRowNeedsHuman(now, 1)}, domain.PlaidOrphanedItemHealthScanCap+7, nil)

	var logBuf bytes.Buffer
	worker := newOrphanMonitorWorker(lister, &logBuf, mockRepo)

	observability.PlaidOrphanedItemsByAdminState.WithLabelValues("manual_intervention_required").Set(4)

	require.NoError(t, worker.Work(ctx, newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})))

	assert.Equal(t, float64(4), testutil.ToFloat64(observability.PlaidOrphanedItemsByAdminState.WithLabelValues("manual_intervention_required")),
		"a truncated scan must publish nothing — 1 of >500 rows is a more convincing lie than no reading at all")
	assert.Contains(t, logBuf.String(), "level=ERROR")
}

// An unwired repository skips the pass entirely rather than publishing zeroes — the same
// "unknown != zero" rule monitorMalformedTierOverrides already applies to its own unconfigured case.
func TestRetentionCleanupCheck_MonitorOrphanedPlaidItems_NilRepo_SkipsWithoutPublishing(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockRetentionProfileRepository)
	setupOrphanMonitorMocks(ctx, mockRepo)

	var logBuf bytes.Buffer
	worker := newOrphanMonitorWorker(nil, &logBuf, mockRepo)

	observability.PlaidOrphanedItemsByAdminState.WithLabelValues("manual_intervention_required").Set(3)

	require.NoError(t, worker.Work(ctx, newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})))

	assert.Equal(t, float64(3), testutil.ToFloat64(observability.PlaidOrphanedItemsByAdminState.WithLabelValues("manual_intervention_required")),
		"an unwired repo must not publish a 0 that reads as a clean bill of health")
	assert.Contains(t, logBuf.String(), "level=WARN")
}

// ─── Issue #1497 item 2: the operator-attested state must reach these gauges ──────────────────────
//
// ⚠ WHY THIS TEST IS PART OF THE ITEM-2 BATCH AND NOT AN AFTERTHOUGHT. The GaugeVec is labelled by
// domain.OrphanedItemAdminState, and its label values are enumerated BY HAND in
// monitorOrphanedPlaidItems — the compiler cannot notice a missing one. So a fourth state added to
// the domain and not added here does not fail to build: the attested rows simply vanish from
// Prometheus, and the published per-state gauges stop summing to the population, which is the
// silent-default-bucket failure this batch was explicitly told to prevent.

// orphanRowAttested is a row an operator has claimed to have handled at Plaid. Its credential is
// dead, so WITHOUT the attestation it would be counted as manual_intervention_required — which is
// what the control assertion below relies on.
func orphanRowAttested(now time.Time, attempts int) domain.PlaidOrphanedItem {
	attested := now.Add(-2 * time.Hour)
	return domain.PlaidOrphanedItem{
		ID:                     "attested",
		TokenExpiresAt:         now.Add(-6 * time.Hour),
		OrphanedAt:             now.Add(-31 * 24 * time.Hour),
		RetryAttemptCount:      attempts,
		OperatorResolvedAt:     &attested,
		OperatorResolvedBy:     "user_2adminClerkID",
		OperatorResolutionNote: "Removed by hand in Plaid's dashboard.",
	}
}

func TestRetentionCleanupCheck_MonitorOrphanedPlaidItems_PublishesTheOperatorAttestedState(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	// Reset every label this test reads, so a value latched by an earlier test in this package cannot
	// make the assertions below pass without this pass having published anything.
	for _, state := range []string{"auto_retrying", "manual_intervention_required", "resolved", "operator_attested"} {
		observability.PlaidOrphanedItemsByAdminState.WithLabelValues(state).Set(-1)
	}

	mockRepo := new(MockRetentionProfileRepository)
	setupOrphanMonitorMocks(ctx, mockRepo)

	lister := new(MockOrphanedPlaidItemLister)
	items := []domain.PlaidOrphanedItem{
		orphanRowAttested(now, 12),
		orphanRowAttested(now, 3),
		orphanRowNeedsHuman(now, 5),
		orphanRowLive(now, 2),
		orphanRowDone(now, 9),
	}
	lister.On("List", mock.Anything, domain.PlaidOrphanedItemHealthScanCap, 0).Return(items, len(items), nil)

	var logBuf bytes.Buffer
	worker := newOrphanMonitorWorker(lister, &logBuf, mockRepo)

	require.NoError(t, worker.Work(ctx, newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})))

	attested := testutil.ToFloat64(observability.PlaidOrphanedItemsByAdminState.WithLabelValues("operator_attested"))
	manual := testutil.ToFloat64(observability.PlaidOrphanedItemsByAdminState.WithLabelValues("manual_intervention_required"))
	auto := testutil.ToFloat64(observability.PlaidOrphanedItemsByAdminState.WithLabelValues("auto_retrying"))
	resolved := testutil.ToFloat64(observability.PlaidOrphanedItemsByAdminState.WithLabelValues("resolved"))

	assert.Equal(t, float64(2), attested,
		"two attested rows must be published under their own label, not dropped into no bucket at all")
	assert.Equal(t, float64(1), manual,
		"and an attested row must NOT still be counted as work a human has to do — that is the whole point")
	assert.Equal(t, float64(1), auto)
	assert.Equal(t, float64(1), resolved,
		"nor may a claim be published as a proved removal")

	// The per-state gauges must still account for the whole population: a state nobody published is a
	// state nobody can see, and this is the assertion that notices.
	assert.Equal(t, float64(len(items)), attested+manual+auto+resolved)

	// Item 4's gauge still sees the attested row's attempt history — deliberately, because the sweep
	// keeps retrying it (see GetRetryablePage's decision). 12 is the worst UNRESOLVED count here; the
	// resolved row's 9 is exempt and must not win.
	assert.Equal(t, float64(12), testutil.ToFloat64(observability.PlaidOrphanedItemMaxRetryAttempts),
		"an unverified claim must not silence the gauge capable of exposing it as false")

	lister.AssertExpectations(t)
}

// ⚠ THE STARTUP ZERO, THE SIBLING GAUGE'S VERSION OF IT.
//
// monitorOrphanedPlaidItems' doc comment says it publishes NOTHING on a failed or truncated read,
// and the alert rules for these gauges rest on `absent(...)` being the signal that the pass has
// stopped. That was true of PlaidOrphanedItemsByAdminState for free — a GaugeVec collects nothing
// until a label set exists — and FALSE of PlaidOrphanedItemMaxRetryAttempts, which as a plain Gauge
// was registered at 0 from process start. So before the first pass it asserted "no Item is being
// retried into the ground", and this publisher runs on the retention worker's DAILY cadence: a
// process restarted more often than once a day published nothing but that fabricated zero, and reset
// the rule's own `for: 6h` clock each time. It is now an observability.UnmeasuredGauge.
//
// SIBLING, in the same test: after a real pass the gauge is present and carries the measurement, so
// this cannot be satisfied by a gauge that is simply never published.
func TestRetentionCleanupCheck_MonitorOrphanedPlaidItems_MaxRetryGaugeIsAbsentUntilAPassHasRun(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	// Return the gauge to its process-start state — no child, nothing collected — so this test is
	// asserting the declaration's behaviour rather than the order the package's tests happened to run
	// in.
	observability.PlaidOrphanedItemMaxRetryAttempts.DeleteLabelValues()
	require.Equal(t, 0, testutil.CollectAndCount(observability.PlaidOrphanedItemMaxRetryAttempts),
		"an unmeasured gauge must be ABSENT from the scrape, not present with a confident 0 — absent() is what the alert rule reads")

	mockRepo := new(MockRetentionProfileRepository)
	setupOrphanMonitorMocks(ctx, mockRepo)

	lister := new(MockOrphanedPlaidItemLister)
	lister.On("List", mock.Anything, domain.PlaidOrphanedItemHealthScanCap, 0).
		Return([]domain.PlaidOrphanedItem{orphanRowNeedsHuman(now, 17)}, 1, nil)

	var logBuf bytes.Buffer
	worker := newOrphanMonitorWorker(lister, &logBuf, mockRepo)
	require.NoError(t, worker.Work(ctx, newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})))

	require.Equal(t, 1, testutil.CollectAndCount(observability.PlaidOrphanedItemMaxRetryAttempts),
		"and once a pass HAS read the table the gauge must appear — absence must mean 'not measured', never 'never published'")
	assert.Equal(t, float64(17), testutil.ToFloat64(observability.PlaidOrphanedItemMaxRetryAttempts))
}
