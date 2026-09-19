package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	rc "github.com/maxnussbaum/BoostedBudget/internal/integration/revenuecat"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/require"
)

type rcTestStore struct {
	repository.RevenueCatRepository
	mu       sync.Mutex
	state    domain.RevenueCatState
	ops      map[string]domain.RevenueCatOperation
	readHook func()
}

func TestRevenueCatService_UnconfiguredProviderFailsClosedAndRecordsAttempt(t *testing.T) {
	r := &rcTestStore{ops: map[string]domain.RevenueCatOperation{"op": {ID: "op", UserID: "u", Class: "comp", Key: "comp_premium", Stage: "pending"}}}
	s := NewRevenueCatService(r, nil, rcTestProfile{})
	_, err := s.Refresh(context.Background(), "u")
	require.Error(t, err)
	_, err = s.ResolveOwnAccess(context.Background(), "u")
	require.ErrorIs(t, err, domain.ErrRevenueCatUnresolved)
	require.Error(t, s.ProcessOperation(context.Background(), "u", "op"))
	require.Equal(t, "pending", r.ops["op"].Stage)
	require.Equal(t, 1, r.ops["op"].Attempts)
}

func (r *rcTestStore) Read(context.Context, string) (domain.RevenueCatState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.readHook != nil {
		r.readHook()
	}
	return r.state, nil
}

func TestRevenueCatService_CacheReadCrossingDeadlineRequiresRefresh(t *testing.T) {
	for _, kind := range []string{"expiry", "freshness"} {
		t.Run(kind, func(t *testing.T) {
			now := time.Now()
			deadline := now.Add(100 * time.Millisecond)
			state := domain.RevenueCatState{AccessUpdatedAt: now, Grants: []domain.RevenueCatGrant{{Key: "paid_premium"}}}
			if kind == "expiry" {
				state.Grants[0].ExpiresAt = &deadline
			} else {
				state.AccessUpdatedAt = deadline.Add(-domain.RevenueCatMaxAge)
			}
			r := &rcTestStore{state: state, readHook: func() { time.Sleep(time.Until(deadline) + 10*time.Millisecond) }}
			p := &rcTestProvider{fetchErr: errors.New("outage")}
			s := NewRevenueCatService(r, p, rcTestProfile{})
			_, err := s.ResolveOwnAccessAt(context.Background(), "u", now)
			require.ErrorIs(t, err, domain.ErrRevenueCatUnresolved, "pool waits must not extend access or snapshot freshness")
			require.Equal(t, 1, p.fetches, "expired cached state must attempt authoritative refresh")
		})
	}
}
func (r *rcTestStore) WithCustomer(ctx context.Context, _ string, fn func(repository.RevenueCatSession) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return fn(rcTestSession{r})
}
func (r *rcTestStore) RequestOperation(_ context.Context, op *domain.RevenueCatOperation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	op.ID = "op"
	op.Stage = "pending"
	r.ops[op.ID] = *op
	r.state.PendingOperations++
	return nil
}
func (r *rcTestStore) CreateTrialTx(context.Context, pgx.Tx, string, time.Time) error { return nil }

type rcTestSession struct{ r *rcTestStore }

func (s rcTestSession) Read(context.Context) (domain.RevenueCatState, error) { return s.r.state, nil }
func (s rcTestSession) Save(_ context.Context, v domain.RevenueCatState) error {
	s.r.state = v
	return nil
}
func (s rcTestSession) Operation(_ context.Context, id string) (domain.RevenueCatOperation, error) {
	o, ok := s.r.ops[id]
	if !ok {
		return o, errors.New("missing")
	}
	return o, nil
}
func (s rcTestSession) FinishOperation(_ context.Context, id, stage, message, _ string) error {
	o := s.r.ops[id]
	o.Stage = stage
	o.LastError = message
	o.Attempts++
	s.r.ops[id] = o
	if stage == "applied" {
		s.r.state.PendingOperations--
	}
	return nil
}

type rcTestProfile struct{}

func (rcTestProfile) GetUserByID(context.Context, string) (*domain.User, error) {
	return &domain.User{ID: "local-uuid", ClerkUserID: "clerk_identity"}, nil
}

type rcUnavailableProfile struct{}

func (rcUnavailableProfile) GetUserByID(context.Context, string) (*domain.User, error) {
	return nil, errors.New("unavailable identity secret must not persist")
}

func TestRevenueCatService_IdentityFailureRecordsRetryAttempt(t *testing.T) {
	r := &rcTestStore{ops: map[string]domain.RevenueCatOperation{"op": {ID: "op", UserID: "u", Class: "comp", Key: "comp_premium", Stage: "pending"}}}
	p := &rcTestProvider{}
	s := NewRevenueCatService(r, p, rcUnavailableProfile{})
	for i := 1; i <= 2; i++ {
		require.Error(t, s.ProcessOperation(context.Background(), "u", "op"))
		require.Equal(t, i, r.ops["op"].Attempts)
		require.Equal(t, "provider_unavailable", r.ops["op"].LastError)
	}
	require.Zero(t, p.ensured)
	require.Zero(t, p.fetches)
	require.Zero(t, p.granted)
}

type rcTestProvider struct {
	grants                         []rc.Entitlement
	billing                        []rc.Subscription
	fetchErr, billingErr, grantErr error
	seenIDs, revoked               []string
	granted                        int
	fetches                        int
	missingCustomer                bool
	ensured                        int
	billingHook                    func()
}

func (p *rcTestProvider) EnsureCustomer(_ context.Context, id string) error {
	p.seenIDs = append(p.seenIDs, id)
	p.missingCustomer = false
	p.ensured++
	return nil
}

func (p *rcTestProvider) FetchEntitlements(_ context.Context, id string) ([]rc.Entitlement, error) {
	p.seenIDs = append(p.seenIDs, id)
	p.fetches++
	if p.missingCustomer {
		return nil, &rc.HTTPError{StatusCode: 404}
	}
	return append([]rc.Entitlement(nil), p.grants...), p.fetchErr
}
func (p *rcTestProvider) FetchSubscriptions(context.Context, string) ([]rc.Subscription, error) {
	if p.billingHook != nil {
		p.billingHook()
	}
	return p.billing, p.billingErr
}

func TestRevenueCatService_ExpiryCrossedDuringBillingIsNotAuthorized(t *testing.T) {
	end := time.Now().Add(100 * time.Millisecond)
	r := &rcTestStore{}
	p := &rcTestProvider{grants: []rc.Entitlement{{LookupKey: "paid_premium", ExpiresAt: &end}}, billingHook: func() { time.Sleep(time.Until(end) + 10*time.Millisecond) }}
	s := NewRevenueCatService(r, p, rcTestProfile{})
	_, err := s.ResolveOwnAccessAt(context.Background(), "u", time.Now())
	require.ErrorIs(t, err, domain.ErrRevenueCatUnresolved, "authorization must consider time elapsed after the entitlement fetch")
}
func (p *rcTestProvider) Grant(_ context.Context, id, key string, end *time.Time) error {
	p.seenIDs = append(p.seenIDs, id)
	p.granted++
	p.grants = append(p.grants, rc.Entitlement{LookupKey: key, ExpiresAt: end})
	return p.grantErr
}
func (p *rcTestProvider) Revoke(_ context.Context, _ string, key string) error {
	p.revoked = append(p.revoked, key)
	out := p.grants[:0]
	for _, g := range p.grants {
		if g.LookupKey != key {
			out = append(out, g)
		}
	}
	p.grants = out
	return nil
}

func TestRevenueCatService_ConfirmedAccessSurvivesBillingFailure(t *testing.T) {
	ctx := context.Background()
	old := time.Now().Add(-time.Hour)
	r := &rcTestStore{state: domain.RevenueCatState{Billing: []domain.RevenueCatBilling{{ID: "old"}}, BillingUpdatedAt: &old}}
	p := &rcTestProvider{grants: []rc.Entitlement{{LookupKey: "paid_household_2"}}, billingErr: errors.New("secret provider data must not persist")}
	s := NewRevenueCatService(r, p, rcTestProfile{})
	state, err := s.Refresh(ctx, "local-uuid")
	require.NoError(t, err)
	access, err := state.ResolveAccess(time.Now())
	require.NoError(t, err)
	require.Equal(t, "household_2", access.Tier)
	require.Equal(t, "old", state.Billing[0].ID)
	require.Equal(t, old, *state.BillingUpdatedAt)
	require.Equal(t, "provider_unavailable", state.BillingError)
	for _, id := range p.seenIDs {
		require.Equal(t, "clerk_identity", id)
	}
}

func TestRevenueCatService_NewCustomerTrialIsEstablishedBeforeRead(t *testing.T) {
	end := time.Now().Add(60 * 24 * time.Hour).Truncate(time.Millisecond)
	r := &rcTestStore{state: domain.RevenueCatState{PendingOperations: 1}, ops: map[string]domain.RevenueCatOperation{"op": {ID: "op", UserID: "local-uuid", Class: "trial", Key: "trial_premium", ExpiresAt: &end, Stage: "pending"}}}
	p := &rcTestProvider{missingCustomer: true}
	s := NewRevenueCatService(r, p, rcTestProfile{})
	require.NoError(t, s.ProcessOperation(context.Background(), "local-uuid", "op"))
	require.Equal(t, 1, p.ensured)
	require.Equal(t, 1, p.granted)
	require.Equal(t, "applied", r.ops["op"].Stage)
}
func TestRevenueCatService_StaleErrorFailsClosedAndCoalesces(t *testing.T) {
	r := &rcTestStore{state: domain.RevenueCatState{Grants: []domain.RevenueCatGrant{{Key: "comp_household_5"}}, AccessUpdatedAt: time.Now().Add(-time.Hour)}}
	p := &rcTestProvider{fetchErr: errors.New("outage")}
	s := NewRevenueCatService(r, p, rcTestProfile{})
	_, err := s.ResolveOwnAccess(context.Background(), "local-uuid")
	require.ErrorIs(t, err, domain.ErrRevenueCatUnresolved)
	p.fetchErr = nil
	p.grants = []rc.Entitlement{{LookupKey: "paid_premium"}}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, e := s.ResolveOwnAccess(context.Background(), "local-uuid")
			if e != nil || a.Tier != "premium" {
				t.Errorf("access %+v %v", a, e)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 2, p.fetches)
}
func TestRevenueCatService_AmbiguousGrantReadbackAndScopedReplacement(t *testing.T) {
	end := time.Now().Add(24 * time.Hour).Truncate(time.Millisecond)
	old := end.Add(-time.Hour)
	r := &rcTestStore{ops: map[string]domain.RevenueCatOperation{"op": {ID: "op", UserID: "local-uuid", Class: "comp", Key: "comp_premium", ExpiresAt: &end, Stage: "pending"}}, state: domain.RevenueCatState{PendingOperations: 1}}
	p := &rcTestProvider{grants: []rc.Entitlement{{LookupKey: "paid_household_2"}, {LookupKey: "trial_premium", ExpiresAt: &end}, {LookupKey: "comp_premium", ExpiresAt: &old}}, grantErr: errors.New("timeout after provider applied")}
	s := NewRevenueCatService(r, p, rcTestProfile{})
	require.NoError(t, s.ProcessOperation(context.Background(), "local-uuid", "op"))
	require.Equal(t, []string{"comp_premium"}, p.revoked)
	require.Equal(t, 1, p.granted)
	require.Equal(t, "applied", r.ops["op"].Stage)
	require.NoError(t, s.ProcessOperation(context.Background(), "local-uuid", "op"))
	require.Equal(t, 1, p.granted)
	a, err := r.state.ResolveAccess(time.Now())
	require.NoError(t, err)
	require.Equal(t, "paid", a.Source)
	require.Equal(t, "household_2", a.Tier)
}
func TestRevenueCatService_SupersededReplayDoesNotMutate(t *testing.T) {
	r := &rcTestStore{ops: map[string]domain.RevenueCatOperation{"old": {ID: "old", Stage: "superseded"}}}
	p := &rcTestProvider{}
	s := NewRevenueCatService(r, p, rcTestProfile{})
	require.NoError(t, s.ProcessOperation(context.Background(), "u", "old"))
	require.Zero(t, p.fetches)
	require.Zero(t, p.granted)
}
func rcTimePtr(t time.Time) *time.Time { return &t }

// TestExpiredGrantCannotBlockInactiveProjection is the todo-3 hazard test. Failure path (i) in the
// plan: applyRevenueCatGrants used to return ErrRevenueCatUnresolved on ANY expired/invalid grant
// BEFORE persisting, so a fresh provider read that still returns a lapsed grant aborted the whole
// refresh — and the inactive projection (retention arming + plaid_cleanup_pending_at) was never
// written. Failure path (ii) then compounds it: refreshLocked's Save projects the PRIOR grants if
// they still resolve, else early-returns without projecting, so a persistently-returned expired
// grant leaves the user indefinitely ACTIVE.
//
// The fix is single-mechanism: a FRESH provider read DROPS unusable grants (expired, unknown key,
// duplicate) instead of erroring, then persists whatever remains — here, nothing, so access
// resolves inactive. domain.RevenueCatState.ResolveAccess is deliberately untouched: an expired
// CACHED grant must still require a refresh, and this test is that refresh.
//
// Falsifiability: restoring the `return domain.ErrRevenueCatUnresolved` guard in
// applyRevenueCatGrants (removing either drop) makes this FAIL — Refresh returns an error and the
// persisted state keeps resolving the stale prior grant.
func TestExpiredGrantCannotBlockInactiveProjection(t *testing.T) {
	expired := time.Now().Add(-time.Hour)
	for _, tc := range []struct {
		name  string
		grant rc.Entitlement
	}{
		{"expired-grant", rc.Entitlement{LookupKey: "trial_premium", ExpiresAt: rcTimePtr(expired)}},
		{"unknown-key-grant", rc.Entitlement{LookupKey: "paid_not_a_real_tier"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			priorEnd := time.Now().Add(24 * time.Hour)
			// The last CONFIRMED projection: an active paid grant. It must be replaced by the
			// unusable fresh read, not preserved because the refresh aborted.
			r := &rcTestStore{state: domain.RevenueCatState{
				UserID:          "local-uuid",
				AccessUpdatedAt: time.Now().Add(-time.Hour),
				Grants:          []domain.RevenueCatGrant{{Key: "paid_premium", ExpiresAt: &priorEnd}},
			}}
			p := &rcTestProvider{grants: []rc.Entitlement{tc.grant}}
			s := NewRevenueCatService(r, p, rcTestProfile{})

			state, err := s.Refresh(context.Background(), "local-uuid")
			require.NoError(t, err,
				"a fresh provider read containing only an unusable grant must not abort the refresh and strand the prior projection")
			require.Empty(t, state.Grants, "the unusable grant must be dropped, not projected")
			require.Empty(t, state.AccessError, "a dropped grant is not a provider error")

			// The projection that reached Save is what a subsequent read resolves.
			access, resolveErr := r.state.ResolveAccess(time.Now())
			require.NoError(t, resolveErr)
			require.Equal(t, "inactive", access.Status,
				"the persisted projection must be inactive so retention/Plaid arming is driven by Save")
		})
	}
}

// TestRevenueCatService_NonPaddleStoreGrantYieldsWebAccess is the issue #1652 AC2 proof:
// "A user subscribed through Apple or Google is recognized as paid on the web after signing
// into the same Clerk account."
//
// The mechanism is that RevenueCat resolves the SAME entitlement lookup key (paid_premium) for a
// customer regardless of which store produced the purchase, and the web backend derives access
// from that entitlement — never from the store. The Clerk user ID is the RevenueCat App User ID,
// so an Apple/Google subscriber's entitlement is read for the same Clerk identity the web session
// authenticates as. The store is only provider-reported metadata on the separate billing read.
//
// Falsifiability: introducing a store gate anywhere on the access path (e.g. dropping a grant
// whose subscription store is not "paddle") makes this FAIL — access would resolve inactive while
// the non-Paddle billing record is present.
func TestRevenueCatService_NonPaddleStoreGrantYieldsWebAccess(t *testing.T) {
	for _, store := range []string{"APP_STORE", "PLAY_STORE"} {
		t.Run(store, func(t *testing.T) {
			end := time.Now().Add(30 * 24 * time.Hour)
			r := &rcTestStore{}
			p := &rcTestProvider{
				grants: []rc.Entitlement{{LookupKey: "paid_premium", ExpiresAt: &end}},
				billing: []rc.Subscription{{
					ID: "sub_1", ProductID: "prod_1", Status: "active",
					Store: store, Environment: "production", CurrentPeriodEndsAt: &end,
				}},
			}
			s := NewRevenueCatService(r, p, rcTestProfile{})
			state, err := s.Refresh(context.Background(), "local-uuid")
			require.NoError(t, err)
			require.Len(t, state.Billing, 1)
			require.Equal(t, store, state.Billing[0].Store, "the grant must come from a non-Paddle store")
			access, err := state.ResolveAccess(time.Now())
			require.NoError(t, err)
			require.Equal(t, "active", access.Status)
			require.Equal(t, "paid", access.Source)
			require.Equal(t, "premium", access.Tier)
		})
	}
}

func TestRevenueCatService_AdminValidationAndIntentOnly(t *testing.T) {
	r := &rcTestStore{state: domain.RevenueCatState{AccessUpdatedAt: time.Now()}, ops: map[string]domain.RevenueCatOperation{}}
	p := &rcTestProvider{}
	s := NewRevenueCatService(r, p, rcTestProfile{})
	ctx := context.Background()
	_, err := s.RequestGrant(ctx, "u", "paid", "premium", nil, false, "admin", "reason")
	require.Error(t, err)
	_, err = s.RequestGrant(ctx, "u", "comp", "household_5", nil, false, "admin", "")
	require.Error(t, err)
	_, err = s.RequestGrant(ctx, "u", "trial", "premium", nil, false, "admin", "reason")
	require.Error(t, err)
	op, err := s.RequestGrant(ctx, "u", "comp", "household_5", nil, false, "admin", "reason")
	require.NoError(t, err)
	require.Equal(t, "pending", op.Stage)
	require.Zero(t, p.granted)
	a, err := r.state.ResolveAccess(time.Now())
	require.NoError(t, err)
	require.Equal(t, "inactive", a.Status)
}

// TestRevenueCatDesired_PermanentOpMatchesLifetimeSentinel is the issue #1692 regression.
// RevenueCat materialises a `duration:lifetime` comp as a concrete, per-grant jittered expiry
// roughly 200 years out (production value observed: 8097054022838 ms). A permanent (nil-expiry)
// operation must treat that sentinel as its own grant, or it can never reach `applied` and the
// repair worker revokes+regrants the promotion every minute.
func TestRevenueCatDesired_PermanentOpMatchesLifetimeSentinel(t *testing.T) {
	now := time.Now()
	lifetime := time.UnixMilli(8097054022838)
	grants := []rc.Entitlement{{LookupKey: "comp_household_2", ExpiresAt: &lifetime}}
	op := domain.RevenueCatOperation{Class: "comp", Key: "comp_household_2"}
	require.True(t, revenueCatDesired(grants, op, now),
		"a nil-expiry comp must be satisfied by RevenueCat's lifetime sentinel expiry")
}

// TestRevenueCatDesired_PermanentOpRejectsShortFiniteGrant pins the other half of the horizon:
// a genuine finite comp is far below the permanent horizon and must NOT satisfy a nil-expiry op,
// so a permanent request still replaces a short promotion rather than silently accepting it.
func TestRevenueCatDesired_PermanentOpRejectsShortFiniteGrant(t *testing.T) {
	now := time.Now()
	short := now.Add(30 * 24 * time.Hour)
	grants := []rc.Entitlement{{LookupKey: "comp_household_2", ExpiresAt: &short}}
	op := domain.RevenueCatOperation{Class: "comp", Key: "comp_household_2"}
	require.False(t, revenueCatDesired(grants, op, now),
		"a finite comp must not satisfy a permanent operation")
}

// TestRevenueCatDesired_FiniteOpUnchanged guards the out-of-scope boundary: finite operations keep
// requiring an exact millisecond match, with no tolerance introduced by the permanent fix.
func TestRevenueCatDesired_FiniteOpUnchanged(t *testing.T) {
	now := time.Now()
	end := now.Add(30 * 24 * time.Hour)
	op := domain.RevenueCatOperation{Class: "comp", Key: "comp_household_2", ExpiresAt: &end}
	require.True(t, revenueCatDesired([]rc.Entitlement{{LookupKey: "comp_household_2", ExpiresAt: &end}}, op, now))
	mismatch := end.Add(time.Millisecond)
	require.False(t, revenueCatDesired([]rc.Entitlement{{LookupKey: "comp_household_2", ExpiresAt: &mismatch}}, op, now),
		"finite expiries still require an exact millisecond match")
}

func TestRevenueCatGrantMatches(t *testing.T) {
	now := time.Now()
	lifetime := time.UnixMilli(8097054022838)
	short := now.Add(30 * 24 * time.Hour)
	finite := now.Add(60 * 24 * time.Hour)
	permanent := domain.RevenueCatOperation{Class: "comp", Key: "comp_household_2"}
	finiteOp := domain.RevenueCatOperation{Class: "comp", Key: "comp_household_2", ExpiresAt: &finite}
	for _, tc := range []struct {
		name string
		g    rc.Entitlement
		op   domain.RevenueCatOperation
		want bool
	}{
		{"key-mismatch", rc.Entitlement{LookupKey: "comp_premium", ExpiresAt: &lifetime}, permanent, false},
		{"nil-grant-nil-op", rc.Entitlement{LookupKey: "comp_household_2"}, permanent, true},
		{"nil-op-lifetime-sentinel", rc.Entitlement{LookupKey: "comp_household_2", ExpiresAt: &lifetime}, permanent, true},
		{"nil-op-short-finite", rc.Entitlement{LookupKey: "comp_household_2", ExpiresAt: &short}, permanent, false},
		{"finite-exact", rc.Entitlement{LookupKey: "comp_household_2", ExpiresAt: &finite}, finiteOp, true},
		{"finite-mismatch", rc.Entitlement{LookupKey: "comp_household_2", ExpiresAt: &short}, finiteOp, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, revenueCatGrantMatches(tc.g, tc.op, now))
		})
	}
}
