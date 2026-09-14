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
	retryState  string
	backlog     repository.PostHogErasureBacklog
}

func (s *stubPostHogErasureStore) Backlog(context.Context, time.Time) (repository.PostHogErasureBacklog, error) {
	return s.backlog, nil
}

func (s *stubPostHogErasureStore) ClaimDue(context.Context, time.Time, time.Duration) (*repository.PostHogErasureRecord, error) {
	return s.record, s.claimErr
}
func (s *stubPostHogErasureStore) MarkPersonUUIDsResolved(_ context.Context, _, _ string, uuids []string, unresolved bool) (bool, error) {
	s.record.PersonUUIDs = uuids
	s.record.RequiresManualEventVerification = unresolved
	return true, nil
}
func (s *stubPostHogErasureStore) MarkDeletionRequestStarted(context.Context, string, string, map[string]*time.Time) (time.Time, bool, error) {
	return time.Unix(50, 0), true, nil
}
func (s *stubPostHogErasureStore) MarkAccepted(context.Context, string, string, int) (bool, error) {
	s.accepted = true
	return true, nil
}
func (s *stubPostHogErasureStore) RecordPollPending(context.Context, string, string) (bool, error) {
	return true, nil
}
func (s *stubPostHogErasureStore) MarkPersonEventsVerified(context.Context, string, string) (bool, error) {
	return true, nil
}
func (s *stubPostHogErasureStore) RecordRetry(_ context.Context, _, _, class string, at time.Time, max int) (string, error) {
	s.retryClass, s.retryAt, s.maxAttempts = class, at, max
	if s.retryState == "" {
		s.retryState = "retrying"
	}
	return s.retryState, nil
}

type stubPostHogErasureProvider struct {
	err               error
	noPerson          bool
	zeroAfterAccepted bool
}

func (s stubPostHogErasureProvider) ResolvePersonUUIDs(context.Context, string, string, []string) (phclient.ErasurePersonResolution, error) {
	if s.noPerson {
		return phclient.ErasurePersonResolution{HasUnresolvedIdentities: true}, s.err
	}
	return phclient.ErasurePersonResolution{PersonUUIDs: []string{"11111111-1111-4111-8111-111111111111"}}, s.err
}
func (s stubPostHogErasureProvider) CapturePersonEventDeletionBaseline(context.Context, string, string, []string) (map[string]*time.Time, error) {
	return map[string]*time.Time{}, s.err
}

func (s stubPostHogErasureProvider) RequestPersonDeletion(context.Context, string, string, []string) (phclient.ErasureDeleteResult, error) {
	if s.noPerson || s.zeroAfterAccepted {
		return phclient.ErasureDeleteResult{EventsQueued: true, RecordingsQueued: true}, s.err
	}
	return phclient.ErasureDeleteResult{PersonsFound: 1, PersonsDeleted: 1, EventsQueued: s.err == nil, RecordingsQueued: s.err == nil}, s.err
}

func TestPostHogErasureWorker_NoPersonStillSubmitsDistinctIDDeletion(t *testing.T) {
	store := &stubPostHogErasureStore{record: &repository.PostHogErasureRecord{ID: "row", Identities: []string{"event-only"}, LeaseToken: "lease", Phase: "person_resolve"}}
	provider := stubPostHogErasureProvider{noPerson: true}
	worker := NewPostHogErasureWorker(PostHogErasureWorkerDeps{Store: store, Provider: provider})
	require.NoError(t, worker.Work(context.Background(), &river.Job[PostHogErasureArgs]{}))
	assert.Empty(t, store.record.PersonUUIDs)
	store.record.Phase = "person_delete"
	require.NoError(t, worker.Work(context.Background(), &river.Job[PostHogErasureArgs]{}))
	assert.True(t, store.accepted)
}
func (s stubPostHogErasureProvider) PersonEventDeletionStatus(context.Context, string, string, []string, map[string]*time.Time, time.Time, time.Time) (phclient.ErasureDeletionObservation, error) {
	return phclient.ErasureDeletionObservation{Found: true, Complete: true}, s.err
}

func TestPostHogErasureWorker_ReconcilesLost202PersistenceWhenRetryFindsNoPerson(t *testing.T) {
	store := &stubPostHogErasureStore{record: &repository.PostHogErasureRecord{ID: "row", Identities: []string{"deleted"}, PersonUUIDs: []string{"11111111-1111-4111-8111-111111111111"}, LeaseToken: "lease", Phase: "person_delete"}}
	worker := NewPostHogErasureWorker(PostHogErasureWorkerDeps{Store: store, Provider: stubPostHogErasureProvider{zeroAfterAccepted: true}})
	require.NoError(t, worker.Work(context.Background(), &river.Job[PostHogErasureArgs]{}))
	assert.True(t, store.accepted)
}

type deadlineErasureProvider struct{ deadline time.Time }

func (p *deadlineErasureProvider) ResolvePersonUUIDs(ctx context.Context, _ string, _ string, _ []string) (phclient.ErasurePersonResolution, error) {
	p.deadline, _ = ctx.Deadline()
	return phclient.ErasurePersonResolution{}, ctx.Err()
}
func (*deadlineErasureProvider) CapturePersonEventDeletionBaseline(context.Context, string, string, []string) (map[string]*time.Time, error) {
	panic("unexpected external call")
}
func (*deadlineErasureProvider) RequestPersonDeletion(context.Context, string, string, []string) (phclient.ErasureDeleteResult, error) {
	panic("unexpected external call")
}
func (*deadlineErasureProvider) PersonEventDeletionStatus(context.Context, string, string, []string, map[string]*time.Time, time.Time, time.Time) (phclient.ErasureDeletionObservation, error) {
	panic("unexpected external call")
}
func TestPostHogErasureWorker_BoundsProviderPhaseBelowLease(t *testing.T) {
	provider := &deadlineErasureProvider{}
	now := time.Now()
	store := &stubPostHogErasureStore{record: &repository.PostHogErasureRecord{ID: "row", Identities: []string{"id"}, LeaseToken: "lease", Phase: "person_resolve"}}
	require.NoError(t, NewPostHogErasureWorker(PostHogErasureWorkerDeps{Store: store, Provider: provider}).Work(context.Background(), &river.Job[PostHogErasureArgs]{}))
	require.False(t, provider.deadline.IsZero())
	assert.Less(t, provider.deadline.Sub(now), postHogErasureLease)
}

type expiringBaselineErasureProvider struct{ deleteCalls int }

func (*expiringBaselineErasureProvider) ResolvePersonUUIDs(context.Context, string, string, []string) (phclient.ErasurePersonResolution, error) {
	panic("unexpected external call")
}

func (*expiringBaselineErasureProvider) CapturePersonEventDeletionBaseline(ctx context.Context, _ string, _ string, _ []string) (map[string]*time.Time, error) {
	<-ctx.Done()
	return map[string]*time.Time{}, nil
}

func (p *expiringBaselineErasureProvider) RequestPersonDeletion(context.Context, string, string, []string) (phclient.ErasureDeleteResult, error) {
	p.deleteCalls++
	return phclient.ErasureDeleteResult{}, nil
}

func (*expiringBaselineErasureProvider) PersonEventDeletionStatus(context.Context, string, string, []string, map[string]*time.Time, time.Time, time.Time) (phclient.ErasureDeletionObservation, error) {
	panic("unexpected external call")
}

func TestPostHogErasureWorker_DoesNotStartAnotherProviderCallAfterPhaseDeadline(t *testing.T) {
	store := &stubPostHogErasureStore{record: &repository.PostHogErasureRecord{
		ID: "row", Identities: []string{"id"}, PersonUUIDs: []string{"11111111-1111-4111-8111-111111111111"},
		LeaseToken: "lease", Phase: "person_delete",
	}}
	provider := &expiringBaselineErasureProvider{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	require.NoError(t, NewPostHogErasureWorker(PostHogErasureWorkerDeps{Store: store, Provider: provider}).Work(ctx, &river.Job[PostHogErasureArgs]{}))
	assert.Zero(t, provider.deleteCalls)
}

type persistedBaselineErasureProvider struct {
	captureCalls int
	gotBaseline  map[string]*time.Time
}

func (*persistedBaselineErasureProvider) ResolvePersonUUIDs(context.Context, string, string, []string) (phclient.ErasurePersonResolution, error) {
	panic("unexpected external call")
}

func (p *persistedBaselineErasureProvider) CapturePersonEventDeletionBaseline(context.Context, string, string, []string) (map[string]*time.Time, error) {
	p.captureCalls++
	return nil, errors.New("baseline must not be recaptured")
}

func (*persistedBaselineErasureProvider) RequestPersonDeletion(context.Context, string, string, []string) (phclient.ErasureDeleteResult, error) {
	return phclient.ErasureDeleteResult{}, nil
}

func (p *persistedBaselineErasureProvider) PersonEventDeletionStatus(_ context.Context, _, _ string, _ []string, baseline map[string]*time.Time, _, _ time.Time) (phclient.ErasureDeletionObservation, error) {
	p.gotBaseline = baseline
	return phclient.ErasureDeletionObservation{Found: true, Complete: true}, nil
}

func TestPostHogErasureWorker_ReusesPersistedBaselineAfterAcceptancePersistenceCrash(t *testing.T) {
	startedAt := time.Unix(50, 0)
	priorDeletion := time.Unix(25, 0)
	personUUID := "11111111-1111-4111-8111-111111111111"
	baseline := map[string]*time.Time{personUUID: &priorDeletion}
	store := &stubPostHogErasureStore{record: &repository.PostHogErasureRecord{
		ID: "row", Identities: []string{"already-deleted"}, PersonUUIDs: []string{personUUID},
		LeaseToken: "lease", Phase: "person_delete", ProviderRequestStartedAt: &startedAt,
		DeletionBaseline: baseline,
	}}
	provider := &persistedBaselineErasureProvider{}
	worker := NewPostHogErasureWorker(PostHogErasureWorkerDeps{Store: store, Provider: provider})

	require.NoError(t, worker.Work(context.Background(), &river.Job[PostHogErasureArgs]{}))
	assert.Zero(t, provider.captureCalls)
	assert.Equal(t, baseline, provider.gotBaseline)
	assert.True(t, store.accepted)
}

func TestPostHogErasureWorker_AcceptanceMovesToManualVerificationNotComplete(t *testing.T) {
	store := &stubPostHogErasureStore{
		record:  &repository.PostHogErasureRecord{ID: "row", Identities: []string{"clerk", "legacy"}, PersonUUIDs: []string{"11111111-1111-4111-8111-111111111111"}, LeaseToken: "lease", Phase: "person_delete"},
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
			store := &stubPostHogErasureStore{record: &repository.PostHogErasureRecord{ID: "row", Identities: []string{"secret-sub"}, LeaseToken: "lease", AttemptCount: 2, Phase: "person_resolve"}}
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

func TestPostHogErasureWorker_EmitsDeadLetterAtRetryBoundary(t *testing.T) {
	before := testutil.ToFloat64(observability.PostHogErasureAttempts.WithLabelValues("person_resolve", "dead_letter", "configuration"))
	store := &stubPostHogErasureStore{record: &repository.PostHogErasureRecord{ID: "row", Identities: []string{"secret"}, LeaseToken: "lease", AttemptCount: 12, Phase: "person_resolve"}, retryState: "dead_letter"}
	worker := NewPostHogErasureWorker(PostHogErasureWorkerDeps{Store: store, Provider: nil})
	require.NoError(t, worker.Work(context.Background(), &river.Job[PostHogErasureArgs]{}))
	after := testutil.ToFloat64(observability.PostHogErasureAttempts.WithLabelValues("person_resolve", "dead_letter", "configuration"))
	assert.Equal(t, before+1, after)
}
