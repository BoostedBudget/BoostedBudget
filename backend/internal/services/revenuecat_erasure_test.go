package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	rc "github.com/maxnussbaum/BoostedBudget/internal/integration/revenuecat"
	"github.com/stretchr/testify/require"
)

func TestRevenueCatErasureFencePreventsQueuedGrantAndCheckout(t *testing.T) {
	now := time.Now()
	r := &rcTestStore{state: domain.RevenueCatState{ErasureStartedAt: &now, AccessUpdatedAt: now, BillingUpdatedAt: &now}, ops: map[string]domain.RevenueCatOperation{"op": {ID: "op", UserID: "u", Class: "comp", Key: "comp_premium", Stage: "pending"}}}
	p := &rcTestProvider{}
	s := NewRevenueCatService(r, p, rcTestProfile{})
	require.ErrorIs(t, s.ProcessOperation(context.Background(), "u", "op"), domain.ErrRevenueCatErasureStarted)
	require.Empty(t, p.grants)
	require.ErrorIs(t, r.state.CanStartCheckout(now), domain.ErrRevenueCatErasureStarted)
}

type rcErasureIntent struct {
	store  *rcTestStore
	begins int
}

func (r *rcErasureIntent) Begin(_ context.Context, _ string, _ bool) error {
	now := time.Now()
	r.store.state.ErasureStartedAt = &now
	r.begins++
	return nil
}
func (r *rcErasureIntent) Confirm(context.Context, string) error {
	now := time.Now()
	r.store.state.ErasureConfirmedAt = &now
	return nil
}

type rcErasureProvider struct {
	rcTestProvider
	deleted   []string
	deleteErr error
	exists    bool
	existsErr error
}

func (p *rcErasureProvider) CustomerExists(context.Context, string) (bool, error) {
	return p.exists, p.existsErr
}

func (p *rcErasureProvider) DeleteCustomer(_ context.Context, id string) error {
	p.deleted = append(p.deleted, id)
	return p.deleteErr
}
func TestRevenueCatErasureRemoteFailureRetainsIdentityAndRetries404(t *testing.T) {
	ctx := context.Background()
	r := &rcTestStore{}
	p := &rcErasureProvider{deleteErr: errors.New("timeout")}
	authority := NewRevenueCatService(r, p, rcTestProfile{})
	intent := &rcErasureIntent{store: r}
	s := NewRevenueCatErasureService(authority, intent, nil)
	gate := func(context.Context) (bool, error) { return true, nil }
	require.NoError(t, s.Prepare(ctx, "u", false, gate))
	require.NotNil(t, r.state.ErasureStartedAt)
	require.Zero(t, p.ensured, "erasure must not create a previously missing customer")
	require.Error(t, s.Erase(ctx, "u", gate))
	require.NotNil(t, r.state.ErasureStartedAt)
	require.Nil(t, r.state.ErasureConfirmedAt)
	p.deleteErr = &rc.HTTPError{StatusCode: 404}
	require.NoError(t, s.Erase(ctx, "u", gate))
	require.NotNil(t, r.state.ErasureConfirmedAt)
	require.Equal(t, []string{"clerk_identity", "clerk_identity"}, p.deleted)
}

func TestRevenueCatErasureAcceptedDeletionRequiresAbsentReadback(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	r := &rcTestStore{state: domain.RevenueCatState{ErasureStartedAt: &now}}
	p := &rcErasureProvider{exists: true}
	s := NewRevenueCatErasureService(NewRevenueCatService(r, p, rcTestProfile{}), &rcErasureIntent{store: r}, nil)
	gate := func(context.Context) (bool, error) { return true, nil }
	require.Error(t, s.Erase(ctx, "u", gate), "DELETE acceptance only queues asynchronous erasure")
	require.Nil(t, r.state.ErasureConfirmedAt)
	p.existsErr = &rc.HTTPError{StatusCode: 423}
	require.Error(t, s.Erase(ctx, "u", gate), "a locked customer is not confirmed absent")
	require.Nil(t, r.state.ErasureConfirmedAt)
	p.exists, p.existsErr = false, nil
	require.NoError(t, s.Erase(ctx, "u", gate))
	require.NotNil(t, r.state.ErasureConfirmedAt)
}

func TestRevenueCatErasureNeverCreatesMissingRemoteCustomer(t *testing.T) {
	ctx := context.Background()
	r := &rcTestStore{}
	p := &rcErasureProvider{rcTestProvider: rcTestProvider{missingCustomer: true, billingErr: &rc.HTTPError{StatusCode: 404}}, deleteErr: &rc.HTTPError{StatusCode: 404}}
	s := NewRevenueCatErasureService(NewRevenueCatService(r, p, rcTestProfile{}), &rcErasureIntent{store: r}, nil)
	gate := func(context.Context) (bool, error) { return true, nil }
	require.NoError(t, s.Prepare(ctx, "u", false, gate))
	require.NoError(t, s.Erase(ctx, "u", gate))
	require.Zero(t, p.ensured)
}

func TestRevenueCatErasureRetentionReadbackPreservesBillingAndNeverCreatesCustomer(t *testing.T) {
	ctx := context.Background()
	old := time.Now().Add(-time.Hour)
	r := &rcTestStore{state: domain.RevenueCatState{ErasureStartedAt: &old, ErasureConfirmedAt: &old, AccessUpdatedAt: old, BillingUpdatedAt: &old, Billing: []domain.RevenueCatBilling{{Store: "paddle", StoreSubscriptionIdentifier: "sub_retained"}}}}
	p := &rcErasureProvider{rcTestProvider: rcTestProvider{missingCustomer: true, billingErr: &rc.HTTPError{StatusCode: 404}}}
	s := NewRevenueCatService(r, p, rcTestProfile{})
	state, err := s.Refresh(ctx, "u")
	require.NoError(t, err)
	require.True(t, state.SafeForCleanup(time.Now()), "fresh provider absence is authoritative inactivity")
	require.Equal(t, "sub_retained", state.Billing[0].StoreSubscriptionIdentifier)
	require.Zero(t, p.ensured)
	p.fetchErr = errors.New("outage")
	p.missingCustomer = false
	r.state.AccessUpdatedAt = old
	_, err = s.Refresh(ctx, "u")
	require.Error(t, err)
	require.False(t, r.state.SafeForCleanup(time.Now()), "outage must not refresh old inactive evidence")
}

type rcErasureLegacyProfile struct{ user domain.User }

func (p rcErasureLegacyProfile) GetUserByID(context.Context, string) (*domain.User, error) {
	return &p.user, nil
}
func TestRevenueCatErasureVerifiesLegacyPaidReferenceEvenWhenNotImported(t *testing.T) {
	now := time.Now()
	id := "sub_legacy"
	r := &rcTestStore{state: domain.RevenueCatState{ErasureStartedAt: &now, BillingUpdatedAt: &now}}
	p := &rcErasurePaddle{}
	authority := NewRevenueCatService(r, &rcTestProvider{}, rcErasureLegacyProfile{domain.User{ID: "u", ClerkUserID: "clerk", PaddleSubscriptionID: &id, SubscriptionStatus: "inactive"}})
	s := NewRevenueCatErasureService(authority, &rcErasureIntent{store: r}, NewRevenueCatBillingService(nil, p, nil))
	subs, err := s.Subscriptions(context.Background(), "u")
	require.NoError(t, err)
	require.Len(t, subs, 1)
	require.Equal(t, "sub_legacy", subs[0].ID)
	wrongCustomer := "ctm_other_user"
	authority.profiles = rcErasureLegacyProfile{domain.User{ID: "u", ClerkUserID: "clerk", PaddleSubscriptionID: &id, PaddleCustomerID: &wrongCustomer}}
	_, err = s.Subscriptions(context.Background(), "u")
	require.ErrorIs(t, err, domain.ErrRevenueCatUnresolved, "a conflicting legacy customer link must not cancel another customer's subscription")
}
