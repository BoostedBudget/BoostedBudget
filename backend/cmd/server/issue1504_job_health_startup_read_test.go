package main

// Issue #1504 follow-up — THE STARTUP ZERO.
//
// startJobHealthMetricsCollector used to do nothing at all until its first five-minute tick, while
// every gauge it feeds was a registered plain Gauge exporting 0 from process start. So each process
// spent up to five minutes asserting "no backlog, no residue, nothing expired" with nothing having
// looked — and for observability.DeletionResidueRecords, whose prescribed rule is
// `increase(deletion_residue_records[15m]) > 0`, the later 0 → N step read as N NEW INCIDENTS. A
// restart fired an alert about something that had not happened.
//
// The fix has two halves and this file pins both: one read BEFORE the ticker starts, so the first
// published sample is a measurement; and observability.UnmeasuredGauge, so a read that FAILS leaves
// the series absent rather than substituting a zero. A zero published by a failed read is the same
// defect wearing a different hat.

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/observability"
)

// stubJobHealthRepo is repository.JobHealthRepository with canned answers. errAll makes every read
// fail, which is the only way to exercise the "a failed read must publish nothing" half.
type stubJobHealthRepo struct {
	compGrantAge time.Duration
	plaidCount   int
	plaidOldest  time.Duration
	residueCount int
	residueAge   time.Duration
	deadLetters  int

	// #1652 todo 12: the RevenueCat backlog gauges.
	webhookInboxPending   int
	webhookInboxOldest    time.Duration
	operationsPending     int
	operationsErrored     int
	operationsOldest      time.Duration
	compGrantsOutstanding int
	refreshOverdueUsers   int

	errAll error
}

func (s *stubJobHealthRepo) GetOldestExpiredCompGrantAge(context.Context) (time.Duration, error) {
	return s.compGrantAge, s.errAll
}

func (s *stubJobHealthRepo) GetOutstandingPlaidCleanupStats(context.Context) (int, time.Duration, error) {
	return s.plaidCount, s.plaidOldest, s.errAll
}

func (s *stubJobHealthRepo) GetDeletionResidueStats(context.Context) (int, time.Duration, error) {
	return s.residueCount, s.residueAge, s.errAll
}

func (s *stubJobHealthRepo) GetDeadLetterQueueSize(context.Context) (int, error) {
	return s.deadLetters, s.errAll
}

func (s *stubJobHealthRepo) GetRevenueCatWebhookInboxStats(context.Context) (int, time.Duration, error) {
	return s.webhookInboxPending, s.webhookInboxOldest, s.errAll
}

func (s *stubJobHealthRepo) GetRevenueCatOperationsStats(context.Context) (int, int, time.Duration, error) {
	return s.operationsPending, s.operationsErrored, s.operationsOldest, s.errAll
}

func (s *stubJobHealthRepo) GetRevenueCatCompGrantsOutstanding(context.Context) (int, error) {
	return s.compGrantsOutstanding, s.errAll
}

func (s *stubJobHealthRepo) GetRevenueCatRefreshOverdueUsers(context.Context) (int, error) {
	return s.refreshOverdueUsers, s.errAll
}

// resetJobHealthGauges returns every gauge this collector feeds to its UNPUBLISHED state, so each
// test starts from "nothing has measured this" rather than from a previous test's reading. Without
// this the absence assertions below would be satisfiable by test ordering.
func resetJobHealthGauges(t *testing.T) {
	t.Helper()
	reset := func() {
		observability.RevenueCatOldestExpiredCompGrantSeconds.DeleteLabelValues()
		observability.PlaidCleanupOutstandingUsers.DeleteLabelValues()
		observability.PlaidCleanupOldestOutstandingSeconds.DeleteLabelValues()
		observability.DeletionResidueRecords.DeleteLabelValues()
		observability.DeletionResidueNewestSeconds.DeleteLabelValues()
		observability.DeadLetterQueueSize.DeleteLabelValues()
		observability.RevenueCatWebhookInboxPending.DeleteLabelValues()
		observability.RevenueCatOldestUnprocessedWebhookSeconds.DeleteLabelValues()
		observability.RevenueCatOperationsPending.DeleteLabelValues()
		observability.RevenueCatOperationsErrored.DeleteLabelValues()
		observability.RevenueCatOldestPendingOperationAgeSeconds.DeleteLabelValues()
		observability.RevenueCatCompGrantsOutstanding.DeleteLabelValues()
		observability.RevenueCatRefreshOverdueUsers.DeleteLabelValues()
	}
	reset()
	t.Cleanup(reset)
}

// ⚠ THE HEADLINE: the gauges reflect a SEEDED POPULATION long before the first tick could elapse.
//
// The deadline is two seconds against a five-minute ticker, so this cannot be satisfied by the
// periodic path — only by a read that happens before the loop is entered.
func TestStartJobHealthMetricsCollector_PublishesAMeasurementBeforeTheFirstTick(t *testing.T) {
	resetJobHealthGauges(t)

	repo := &stubJobHealthRepo{
		compGrantAge: 90 * time.Minute,
		plaidCount:   2,
		plaidOldest:  3 * time.Hour,
		residueCount: 3,
		residueAge:   45 * time.Second,
		deadLetters:  7,

		webhookInboxPending:   4,
		webhookInboxOldest:    2 * time.Minute,
		operationsPending:     5,
		operationsErrored:     1,
		operationsOldest:      10 * time.Minute,
		compGrantsOutstanding: 6,
		refreshOverdueUsers:   8,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startJobHealthMetricsCollector(ctx, repo, slog.New(slog.DiscardHandler))

	require.Eventually(t, func() bool {
		return testutil.CollectAndCount(observability.DeletionResidueRecords) == 1
	}, 2*time.Second, 5*time.Millisecond,
		"the residue gauge must carry a real reading within a query's time of startup — five minutes of confident 0 is what makes increase() fire on a restart")

	assert.Equal(t, float64(3), testutil.ToFloat64(observability.DeletionResidueRecords))
	assert.Equal(t, float64(45), testutil.ToFloat64(observability.DeletionResidueNewestSeconds))

	// The other three gauges the same collector feeds ride on the same initial read. They are asserted
	// here rather than in their own test because a fix that seeded only the residue pair would leave
	// exactly the same defect on the other three.
	assert.Equal(t, float64(2), testutil.ToFloat64(observability.PlaidCleanupOutstandingUsers))
	assert.Equal(t, float64(3*60*60), testutil.ToFloat64(observability.PlaidCleanupOldestOutstandingSeconds))
	assert.Equal(t, float64(90*60), testutil.ToFloat64(observability.RevenueCatOldestExpiredCompGrantSeconds))
	assert.Equal(t, float64(7), testutil.ToFloat64(observability.DeadLetterQueueSize))

	// #1652 todo 12: the RevenueCat backlog gauges ride the same initial read.
	assert.Equal(t, float64(4), testutil.ToFloat64(observability.RevenueCatWebhookInboxPending))
	assert.Equal(t, float64(2*60), testutil.ToFloat64(observability.RevenueCatOldestUnprocessedWebhookSeconds))
	assert.Equal(t, float64(5), testutil.ToFloat64(observability.RevenueCatOperationsPending))
	assert.Equal(t, float64(1), testutil.ToFloat64(observability.RevenueCatOperationsErrored))
	assert.Equal(t, float64(10*60), testutil.ToFloat64(observability.RevenueCatOldestPendingOperationAgeSeconds))
	assert.Equal(t, float64(6), testutil.ToFloat64(observability.RevenueCatCompGrantsOutstanding))
	assert.Equal(t, float64(8), testutil.ToFloat64(observability.RevenueCatRefreshOverdueUsers))
}

// ⚠ THE SIBLING THAT MAKES THE TEST ABOVE FAIL IN BOTH DIRECTIONS. A genuinely clean database must
// still PUBLISH — a zero that was measured is a real and useful reading, and a fix that achieved
// "never a startup zero" by never publishing zero at all would break the alert's ability to clear.
func TestStartJobHealthMetricsCollector_ACleanDatabaseStillPublishesZero(t *testing.T) {
	resetJobHealthGauges(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startJobHealthMetricsCollector(ctx, &stubJobHealthRepo{}, slog.New(slog.DiscardHandler))

	require.Eventually(t, func() bool {
		return testutil.CollectAndCount(observability.DeletionResidueRecords) == 1
	}, 2*time.Second, 5*time.Millisecond,
		"a measured zero must be published — otherwise nothing can ever tell 'clean' from 'not looked'")
	assert.Equal(t, float64(0), testutil.ToFloat64(observability.DeletionResidueRecords))
	assert.Equal(t, float64(0), testutil.ToFloat64(observability.PlaidCleanupOutstandingUsers))
	assert.Equal(t, float64(0), testutil.ToFloat64(observability.DeadLetterQueueSize))
}

// ⚠ AND A FAILED STARTUP READ MUST NOT PUBLISH A ZERO EITHER. This is the half that a plain Gauge
// cannot express at all: registered, it exports 0 whether or not the query succeeded, so "the
// database is unreachable" and "there is no residue" are the same scrape. Absent-until-measured makes
// them different, which is what the prescribed absent() companion rule reads.
func TestStartJobHealthMetricsCollector_AFailedStartupReadPublishesNothing(t *testing.T) {
	resetJobHealthGauges(t)

	logged := make(chan struct{}, 8)
	handler := slog.NewTextHandler(writerFunc(func(p []byte) (int, error) {
		select {
		case logged <- struct{}{}:
		default:
		}
		return len(p), nil
	}), nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startJobHealthMetricsCollector(ctx,
		&stubJobHealthRepo{residueCount: 7, errAll: errors.New("connection refused")}, slog.New(handler))

	// Wait for the collector to have actually TRIED — otherwise "no metric published" would be
	// satisfied by the goroutine not having run yet, which is not what is being asserted.
	select {
	case <-logged:
	case <-time.After(2 * time.Second):
		t.Fatal("the collector never logged a failed startup read — it cannot have attempted one")
	}

	assert.Equal(t, 0, testutil.CollectAndCount(observability.DeletionResidueRecords),
		"a failed read must leave the series absent; publishing 0 would assert that no GDPR deletion has ever been left with a cancelled subscription, which nothing checked")
	assert.Equal(t, 0, testutil.CollectAndCount(observability.DeletionResidueNewestSeconds))
	assert.Equal(t, 0, testutil.CollectAndCount(observability.PlaidCleanupOutstandingUsers))
	assert.Equal(t, 0, testutil.CollectAndCount(observability.RevenueCatOldestExpiredCompGrantSeconds))
	assert.Equal(t, 0, testutil.CollectAndCount(observability.DeadLetterQueueSize))
	assert.Equal(t, 0, testutil.CollectAndCount(observability.RevenueCatWebhookInboxPending))
	assert.Equal(t, 0, testutil.CollectAndCount(observability.RevenueCatOperationsPending))
	assert.Equal(t, 0, testutil.CollectAndCount(observability.RevenueCatOperationsErrored))
	assert.Equal(t, 0, testutil.CollectAndCount(observability.RevenueCatCompGrantsOutstanding))
	assert.Equal(t, 0, testutil.CollectAndCount(observability.RevenueCatRefreshOverdueUsers))
}

// writerFunc adapts a function to io.Writer so the test can observe slog output without a buffer and
// a mutex.
type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }
