package jobs

import (
	"context"
	"errors"
	"fmt"
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
	record        *repository.PostHogErasureRecord
	claimErr      error
	accepted      bool
	retryClass    string
	retryAt       time.Time
	maxAttempts   int
	retryState    string
	backlog       repository.PostHogErasureBacklog
	checkpointErr error
}

func (s *stubPostHogErasureStore) Backlog(context.Context, time.Time) (repository.PostHogErasureBacklog, error) {
	return s.backlog, nil
}

func (s *stubPostHogErasureStore) ClaimDue(context.Context, time.Time, time.Duration) (*repository.PostHogErasureRecord, error) {
	return s.record, s.claimErr
}
func (s *stubPostHogErasureStore) MarkPersonUUIDsResolved(_ context.Context, _, _ string, uuids []string, unresolved bool, nextCursor, total int) (bool, error) {
	s.record.PersonUUIDs = uuids
	s.record.RequiresManualEventVerification = s.record.RequiresManualEventVerification || unresolved
	s.record.ResolutionCursor = nextCursor
	if nextCursor == total {
		if len(uuids) == 0 {
			s.record.Phase = "person_delete"
		} else {
			s.record.Phase = "person_baseline"
		}
	}
	return true, nil
}
func (s *stubPostHogErasureStore) MarkDeletionBaselineCheckpoint(_ context.Context, _, _ string, baseline map[string]*time.Time, nextCursor, total int) (bool, error) {
	if s.checkpointErr != nil {
		err := s.checkpointErr
		s.checkpointErr = nil
		return false, err
	}
	s.record.DeletionBaseline = baseline
	s.record.BaselineCursor = nextCursor
	if nextCursor == total {
		s.record.Phase = "person_delete"
	}
	return true, nil
}
func (s *stubPostHogErasureStore) MarkDeletionRequestStarted(context.Context, string, string, map[string]*time.Time) (time.Time, bool, error) {
	return time.Unix(50, 0), true, nil
}
func (s *stubPostHogErasureStore) MarkAccepted(_ context.Context, _, _ string, _ int, requiresManualEvents bool) (bool, error) {
	s.accepted = true
	s.record.RequiresManualEventVerification = s.record.RequiresManualEventVerification || requiresManualEvents
	now := time.Unix(51, 0)
	s.record.AcceptedAt = &now
	s.record.Phase = "person_poll"
	return true, nil
}
func (s *stubPostHogErasureStore) RecordPollPending(context.Context, string, string) (bool, error) {
	return true, nil
}
func (s *stubPostHogErasureStore) MarkPersonPollCheckpoint(_ context.Context, _, _ string, nextCursor, total int, requiresManualEvents bool) (bool, error) {
	if s.checkpointErr != nil {
		err := s.checkpointErr
		s.checkpointErr = nil
		return false, err
	}
	s.record.PollCursor = nextCursor
	s.record.RequiresManualEventVerification = s.record.RequiresManualEventVerification || requiresManualEvents
	if nextCursor == total {
		s.record.Phase = "support_manual"
	}
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
	manualStatus      bool
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
	return phclient.ErasureDeletionObservation{Found: true, Complete: true, RequiresManualVerification: s.manualStatus}, s.err
}

func TestPostHogErasureWorker_PersistsClockAmbiguityAsManualEventRequirement(t *testing.T) {
	startedAt := time.Unix(50, 0)
	acceptedAt := time.Unix(51, 0)
	store := &stubPostHogErasureStore{record: &repository.PostHogErasureRecord{
		ID: "row", PersonUUIDs: []string{"11111111-1111-4111-8111-111111111111"}, LeaseToken: "lease",
		Phase: "person_poll", ProviderRequestStartedAt: &startedAt, AcceptedAt: &acceptedAt,
	}}
	worker := NewPostHogErasureWorker(PostHogErasureWorkerDeps{Store: store, Provider: stubPostHogErasureProvider{manualStatus: true}})
	require.NoError(t, worker.Work(context.Background(), &river.Job[PostHogErasureArgs]{}))
	assert.True(t, store.record.RequiresManualEventVerification)
}

type progressiveResolutionProvider struct{ distinctIDs []string }

func (p *progressiveResolutionProvider) ResolvePersonUUIDs(_ context.Context, _, _ string, distinctIDs []string) (phclient.ErasurePersonResolution, error) {
	p.distinctIDs = append(p.distinctIDs, distinctIDs...)
	return phclient.ErasurePersonResolution{PersonUUIDs: []string{"11111111-1111-4111-8111-111111111111"}}, nil
}
func (*progressiveResolutionProvider) CapturePersonEventDeletionBaseline(context.Context, string, string, []string) (map[string]*time.Time, error) {
	panic("unexpected external call")
}
func (*progressiveResolutionProvider) RequestPersonDeletion(context.Context, string, string, []string) (phclient.ErasureDeleteResult, error) {
	panic("unexpected external call")
}
func (*progressiveResolutionProvider) PersonEventDeletionStatus(context.Context, string, string, []string, map[string]*time.Time, time.Time, time.Time) (phclient.ErasureDeletionObservation, error) {
	panic("unexpected external call")
}

func TestPostHogErasureWorker_CheckpointsLargeIdentityResolutionAcrossLeases(t *testing.T) {
	identities := make([]string, 1000)
	for i := range identities {
		identities[i] = fmt.Sprintf("identity-%d", i)
	}
	store := &stubPostHogErasureStore{record: &repository.PostHogErasureRecord{
		ID: "row", Identities: identities, LeaseToken: "lease", Phase: "person_resolve",
	}}
	provider := &progressiveResolutionProvider{}
	worker := NewPostHogErasureWorker(PostHogErasureWorkerDeps{Store: store, Provider: provider})
	for range 3 {
		require.NoError(t, worker.Work(context.Background(), &river.Job[PostHogErasureArgs]{}))
	}
	assert.Equal(t, 3, store.record.ResolutionCursor)
	assert.Equal(t, identities[:3], provider.distinctIDs)
	assert.Equal(t, "person_resolve", store.record.Phase)
}

type fullErasureLifecycleProvider struct {
	resolvedIDs   []string
	baselineUUIDs []string
	pollUUIDs     []string
	personCount   int
}

func (p *fullErasureLifecycleProvider) ResolvePersonUUIDs(_ context.Context, _, _ string, distinctIDs []string) (phclient.ErasurePersonResolution, error) {
	p.resolvedIDs = append(p.resolvedIDs, distinctIDs...)
	index := len(p.resolvedIDs)
	return phclient.ErasurePersonResolution{PersonUUIDs: []string{fmt.Sprintf("00000000-0000-4000-8000-%012d", index)}}, nil
}
func (p *fullErasureLifecycleProvider) CapturePersonEventDeletionBaseline(_ context.Context, _, _ string, personUUIDs []string) (map[string]*time.Time, error) {
	p.baselineUUIDs = append(p.baselineUUIDs, personUUIDs...)
	return map[string]*time.Time{personUUIDs[0]: nil}, nil
}
func (p *fullErasureLifecycleProvider) RequestPersonDeletion(context.Context, string, string, []string) (phclient.ErasureDeleteResult, error) {
	return phclient.ErasureDeleteResult{PersonsFound: p.personCount, PersonsDeleted: p.personCount, EventsQueued: true, RecordingsQueued: true}, nil
}
func (p *fullErasureLifecycleProvider) PersonEventDeletionStatus(_ context.Context, _, _ string, personUUIDs []string, _ map[string]*time.Time, _, _ time.Time) (phclient.ErasureDeletionObservation, error) {
	p.pollUUIDs = append(p.pollUUIDs, personUUIDs...)
	return phclient.ErasureDeletionObservation{Found: true, Complete: true}, nil
}

func TestPostHogErasureWorker_CheckpointsMaximumSetThroughBaselineAndPoll(t *testing.T) {
	const identityCount = 1000
	identities := make([]string, identityCount)
	for i := range identities {
		identities[i] = fmt.Sprintf("identity-%d", i)
	}
	store := &stubPostHogErasureStore{record: &repository.PostHogErasureRecord{
		ID: "row", Identities: identities, LeaseToken: "lease", Phase: "person_resolve",
	}}
	provider := &fullErasureLifecycleProvider{personCount: identityCount}
	worker := NewPostHogErasureWorker(PostHogErasureWorkerDeps{Store: store, Provider: provider})
	for store.record.Phase != "support_manual" {
		require.NoError(t, worker.Work(context.Background(), &river.Job[PostHogErasureArgs]{}))
	}
	assert.Len(t, provider.resolvedIDs, identityCount)
	assert.Len(t, provider.baselineUUIDs, identityCount)
	assert.Len(t, provider.pollUUIDs, identityCount)
	assert.Equal(t, identityCount, store.record.ResolutionCursor)
	assert.Equal(t, identityCount, store.record.BaselineCursor)
	assert.Equal(t, identityCount, store.record.PollCursor)
}

func TestPostHogErasureWorker_CrashBeforeBaselineCheckpointRetriesSameUUID(t *testing.T) {
	uuid := "11111111-1111-4111-8111-111111111111"
	store := &stubPostHogErasureStore{record: &repository.PostHogErasureRecord{
		ID: "row", Identities: []string{"identity"}, PersonUUIDs: []string{uuid}, LeaseToken: "lease", Phase: "person_baseline",
	}, checkpointErr: errors.New("database unavailable")}
	provider := &fullErasureLifecycleProvider{personCount: 1}
	worker := NewPostHogErasureWorker(PostHogErasureWorkerDeps{Store: store, Provider: provider})
	require.NoError(t, worker.Work(context.Background(), &river.Job[PostHogErasureArgs]{}))
	require.NoError(t, worker.Work(context.Background(), &river.Job[PostHogErasureArgs]{}))
	assert.Equal(t, []string{uuid, uuid}, provider.baselineUUIDs)
	assert.Equal(t, 1, store.record.BaselineCursor)
}

func TestPostHogErasureWorker_CrashBeforePollCheckpointRetriesSameUUID(t *testing.T) {
	uuid := "11111111-1111-4111-8111-111111111111"
	startedAt, acceptedAt := time.Unix(50, 0), time.Unix(51, 0)
	store := &stubPostHogErasureStore{record: &repository.PostHogErasureRecord{
		ID: "row", Identities: []string{"identity"}, PersonUUIDs: []string{uuid}, LeaseToken: "lease", Phase: "person_poll",
		ProviderRequestStartedAt: &startedAt, AcceptedAt: &acceptedAt, DeletionBaseline: map[string]*time.Time{uuid: nil},
	}, checkpointErr: errors.New("database unavailable")}
	provider := &fullErasureLifecycleProvider{personCount: 1}
	worker := NewPostHogErasureWorker(PostHogErasureWorkerDeps{Store: store, Provider: provider})
	require.NoError(t, worker.Work(context.Background(), &river.Job[PostHogErasureArgs]{}))
	require.NoError(t, worker.Work(context.Background(), &river.Job[PostHogErasureArgs]{}))
	assert.Equal(t, []string{uuid, uuid}, provider.pollUUIDs)
	assert.Equal(t, 1, store.record.PollCursor)
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
		LeaseToken: "lease", Phase: "person_baseline",
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
