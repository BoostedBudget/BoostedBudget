package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubPostHogErasureStore struct {
	record      *repository.PostHogErasureRecord
	claimErr    error
	accepted    bool
	retryClass  string
	retryAt     time.Time
	maxAttempts int
	backlog     repository.PostHogErasureBacklog
}

func (s *stubPostHogErasureStore) Backlog(context.Context, time.Time) (repository.PostHogErasureBacklog, error) {
	return s.backlog, nil
}

func (s *stubPostHogErasureStore) ClaimDue(context.Context, time.Time, time.Duration) (*repository.PostHogErasureRecord, error) {
	return s.record, s.claimErr
}
func (s *stubPostHogErasureStore) MarkAccepted(context.Context, string, string, time.Time) (bool, error) {
	s.accepted = true
	return true, nil
}
func (s *stubPostHogErasureStore) RecordRetry(_ context.Context, _, _, class string, at time.Time, max int) error {
	s.retryClass, s.retryAt, s.maxAttempts = class, at, max
	return nil
}

type stubPostHogErasureProvider struct{ err error }

func (s stubPostHogErasureProvider) RequestPersonDeletion(context.Context, string, string, []string) (phclient.ErasureDeleteResult, error) {
	return phclient.ErasureDeleteResult{EventsQueued: s.err == nil, RecordingsQueued: s.err == nil}, s.err
}

func TestPostHogErasureWorker_AcceptanceMovesToManualVerificationNotComplete(t *testing.T) {
	store := &stubPostHogErasureStore{
		record:  &repository.PostHogErasureRecord{ID: "row", Identities: []string{"clerk", "legacy"}, LeaseToken: "lease"},
		backlog: repository.PostHogErasureBacklog{Counts: map[string]int64{"manual_pending": 3}, OldestSeconds: 90},
	}
	worker := NewPostHogErasureWorker(PostHogErasureWorkerDeps{Store: store, Provider: stubPostHogErasureProvider{}, Now: func() time.Time { return time.Unix(100, 0) }})
	err := worker.Work(context.Background(), &river.Job[PostHogErasureArgs]{Args: PostHogErasureArgs{TriggerSource: "periodic"}})
	require.NoError(t, err)
	assert.True(t, store.accepted)
	assert.Empty(t, store.retryClass)
	assert.Equal(t, float64(3), testutil.ToFloat64(observability.PostHogErasureRows.WithLabelValues("manual_pending")))
	assert.Equal(t, float64(90), testutil.ToFloat64(observability.PostHogErasureOldestOutstandingSeconds))
}

func TestPostHogErasureWorker_MissingConfigAndProviderFailureStayDurable(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider postHogErasureProvider
		class    string
	}{
		{"missing", nil, "configuration"},
		{"rate limited", stubPostHogErasureProvider{err: &phclient.ErasureProviderError{Class: "rate_limited", RetryAfter: time.Hour}}, "rate_limited"},
		{"unknown", stubPostHogErasureProvider{err: errors.New("boom identity@example.com")}, "provider"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &stubPostHogErasureStore{record: &repository.PostHogErasureRecord{ID: "row", Identities: []string{"secret-sub"}, LeaseToken: "lease", AttemptCount: 2}}
			now := time.Unix(100, 0)
			worker := NewPostHogErasureWorker(PostHogErasureWorkerDeps{Store: store, Provider: tc.provider, Now: func() time.Time { return now }})
			err := worker.Work(context.Background(), &river.Job[PostHogErasureArgs]{Args: PostHogErasureArgs{}})
			require.NoError(t, err, "per-row provider failures are persisted and must not lose the sweep")
			assert.Equal(t, tc.class, store.retryClass)
			assert.Greater(t, store.retryAt, now)
			assert.Equal(t, 12, store.maxAttempts)
			assert.False(t, store.accepted)
		})
	}
}
