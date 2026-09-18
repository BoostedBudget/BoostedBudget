package services

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRevenueCatService_RecordsLifecycleFlowCountersOnAccessLoss drives a genuine active→inactive
// flip and asserts the flow counters move on the same once-per-transition predicate as the product
// events. Analytics is deliberately left unconfigured: these counters describe the system's grant
// lifecycle, so gating them on the consent-gated analytics client would silently zero them for
// every deployment without PostHog.
func TestRevenueCatService_RecordsLifecycleFlowCountersOnAccessLoss(t *testing.T) {
	for _, tc := range []struct {
		name        string
		priorStatus string
		priorGrants []domain.RevenueCatGrant
		wantClass   string
	}{
		{
			name:        "comp expiry",
			priorStatus: "active",
			priorGrants: []domain.RevenueCatGrant{{Key: "comp_premium"}},
			wantClass:   observability.RevenueCatGrantClassComp,
		},
		{
			name:        "paid lapse",
			priorStatus: "active",
			priorGrants: []domain.RevenueCatGrant{{Key: "paid_premium"}},
			wantClass:   observability.RevenueCatGrantClassPaid,
		},
		{
			name:        "trial expiry",
			priorStatus: "trial",
			priorGrants: []domain.RevenueCatGrant{{Key: "trial_premium"}},
			wantClass:   observability.RevenueCatGrantClassTrial,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &rcTestStore{state: domain.RevenueCatState{
				AccessUpdatedAt: time.Now().Add(-time.Hour),
				Grants:          tc.priorGrants,
			}}
			s := NewRevenueCatService(store, &rcTestProvider{}, rcStatusProfile{status: tc.priorStatus})

			armedBefore := testutil.ToFloat64(observability.RevenueCatRetentionEpisodesArmedTotal)
			expiredBefore := testutil.ToFloat64(observability.RevenueCatGrantsExpiredTotal.WithLabelValues(tc.wantClass))

			_, err := s.Refresh(context.Background(), "target-user")
			require.NoError(t, err)

			assert.Equal(t, armedBefore+1, testutil.ToFloat64(observability.RevenueCatRetentionEpisodesArmedTotal),
				"access loss must arm the retention safety net exactly once")
			assert.Equal(t, expiredBefore+1, testutil.ToFloat64(observability.RevenueCatGrantsExpiredTotal.WithLabelValues(tc.wantClass)))
		})
	}
}

// TestRevenueCatService_RepeatedInactiveRefreshDoesNotReCount pins the deduplication: an
// already-inactive user's later refresh must not re-count the arming.
func TestRevenueCatService_RepeatedInactiveRefreshDoesNotReCount(t *testing.T) {
	store := &rcTestStore{state: domain.RevenueCatState{AccessUpdatedAt: time.Now().Add(-time.Hour)}}
	s := NewRevenueCatService(store, &rcTestProvider{}, rcStatusProfile{status: "inactive"})

	armedBefore := testutil.ToFloat64(observability.RevenueCatRetentionEpisodesArmedTotal)
	_, err := s.Refresh(context.Background(), "target-user")
	require.NoError(t, err)
	assert.Equal(t, armedBefore, testutil.ToFloat64(observability.RevenueCatRetentionEpisodesArmedTotal))
}

func TestRevenueCatService_RecordsResolutionOutcome(t *testing.T) {
	store := &rcTestStore{state: domain.RevenueCatState{
		AccessUpdatedAt: time.Now(),
		Grants:          []domain.RevenueCatGrant{{Key: "paid_premium"}},
	}}
	s := NewRevenueCatService(store, &rcTestProvider{}, rcStatusProfile{status: "active"})

	before := testutil.ToFloat64(observability.RevenueCatResolutionTotal.WithLabelValues(observability.RevenueCatResolutionResolved))
	_, err := s.ResolveOwnAccess(context.Background(), "target-user")
	require.NoError(t, err)
	assert.Equal(t, before+1, testutil.ToFloat64(observability.RevenueCatResolutionTotal.WithLabelValues(observability.RevenueCatResolutionResolved)))
	assert.Greater(t, testutil.CollectAndCount(observability.RevenueCatRefreshDurationSeconds), 0,
		"a refresh performed during resolution must be timed")
}

// TestRevenueCatService_RecordsRefreshOutcome pins the three-way refresh outcome, including the
// not_due case (a still-valid cached projection, no provider I/O) which must NOT read as an error.
func TestRevenueCatService_RecordsRefreshOutcome(t *testing.T) {
	notDueBefore := testutil.ToFloat64(observability.RevenueCatRefreshesTotal.WithLabelValues(observability.RevenueCatRefreshNotDue))
	store := &rcTestStore{state: domain.RevenueCatState{
		AccessUpdatedAt: time.Now(),
		Grants:          []domain.RevenueCatGrant{{Key: "paid_premium"}},
	}}
	s := NewRevenueCatService(store, &rcTestProvider{}, rcStatusProfile{status: "active"})
	_, err := s.ResolveOwnAccess(context.Background(), "target-user")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, testutil.ToFloat64(observability.RevenueCatRefreshesTotal.WithLabelValues(observability.RevenueCatRefreshNotDue)), notDueBefore)
}
