package services

import (
	"context"
	"errors"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/stretchr/testify/require"
	"log/slog"
	"testing"
)

type rcAnalytics struct {
	phclient.Analytics
	events     []string
	properties []map[string]interface{}
	users      []string
}

func (a *rcAnalytics) Capture(user string, event string, p map[string]interface{}) {
	a.events = append(a.events, event)
	a.properties = append(a.properties, p)
	a.users = append(a.users, user)
}

type rcAnalyticsConsent struct {
	allowed  bool
	err      error
	users    []string
	contexts []context.Context
}

func (c *rcAnalyticsConsent) HasAnalyticsConsent(ctx context.Context, user string) (bool, error) {
	c.users = append(c.users, user)
	c.contexts = append(c.contexts, ctx)
	return c.allowed, c.err
}
func TestRevenueCatAnalyticsDistinguishesQueuedFromAppliedAndOmitsReason(t *testing.T) {
	r := &rcTestStore{ops: map[string]domain.RevenueCatOperation{}}
	s := NewRevenueCatService(r, &rcTestProvider{}, rcTestProfile{})
	a := &rcAnalytics{}
	s.SetGDPRAnalytics(phclient.NewGDPRClient(a, &rcAnalyticsConsent{allowed: true}, slog.Default()))
	op, err := s.RequestGrant(context.Background(), "u", "comp", "premium", nil, false, "admin", "private sensitive explanation")
	require.NoError(t, err)
	require.Equal(t, []string{phclient.EventRevenueCatOperationQueued, phclient.EventAdminSubscriptionOverrideAppliedServer}, a.events)
	require.NoError(t, s.ProcessOperation(context.Background(), "u", op.ID))
	require.Equal(t, []string{phclient.EventRevenueCatOperationQueued, phclient.EventAdminSubscriptionOverrideAppliedServer, phclient.EventRevenueCatOperationApplied}, a.events)
	require.NoError(t, s.ProcessOperation(context.Background(), "u", op.ID))
	require.Len(t, a.events, 3)
	for _, p := range a.properties {
		require.NotContains(t, p, "reason")
		require.NotContains(t, p, "actor_id")
	}
}

func TestRevenueCatAdminOverrideIsScopedToAdminInitiatedGrants(t *testing.T) {
	r := &rcTestStore{ops: map[string]domain.RevenueCatOperation{}}
	s := NewRevenueCatService(r, &rcTestProvider{}, rcTestProfile{})
	a := &rcAnalytics{}
	s.SetGDPRAnalytics(phclient.NewGDPRClient(a, &rcAnalyticsConsent{allowed: true}, slog.Default()))

	op, err := s.RequestGrant(context.Background(), "target-user", "comp", "premium", nil, false, "acting-admin", "private reason")
	require.NoError(t, err)
	require.Equal(t, []string{phclient.EventRevenueCatOperationQueued, phclient.EventAdminSubscriptionOverrideAppliedServer}, a.events)
	require.Equal(t, map[string]interface{}{"tier": "premium", "had_expiry": false}, a.properties[1])
	require.NotContains(t, a.properties[1], "actor_id")
	require.NotContains(t, a.properties[1], "reason")

	// ProcessOperation is the shared sink for every operation, including system-initiated ones.
	// It must never emit the admin-override event.
	require.NoError(t, s.ProcessOperation(context.Background(), "target-user", op.ID))
	require.Equal(t, []string{phclient.EventRevenueCatOperationApplied}, a.events[2:])

	// A revoke emits the removed variant.
	_, err = s.RequestGrant(context.Background(), "target-user", "comp", "premium", nil, true, "acting-admin", "private reason")
	require.NoError(t, err)
	require.Equal(t, phclient.EventAdminSubscriptionOverrideRemovedServer, a.events[len(a.events)-1])
}

func TestRevenueCatAnalyticsChecksTargetConsentForEveryOperationEvent(t *testing.T) {
	for _, tc := range []struct {
		name            string
		allowed, failed bool
		consentErr      error
	}{
		{name: "opted out"}, {name: "consented", allowed: true}, {name: "failed and opted out", failed: true}, {name: "failed and consented", allowed: true, failed: true}, {name: "consent lookup fails", consentErr: errors.New("privacy unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := &rcTestStore{ops: map[string]domain.RevenueCatOperation{}}
			p := &rcTestProvider{}
			s := NewRevenueCatService(r, p, rcTestProfile{})
			a := &rcAnalytics{}
			consent := &rcAnalyticsConsent{allowed: tc.allowed, err: tc.consentErr}
			s.SetGDPRAnalytics(phclient.NewGDPRClient(a, consent, slog.Default()))
			op, err := s.RequestGrant(ctx, "target-user", "comp", "premium", nil, false, "acting-admin", "private reason")
			require.NoError(t, err)
			if tc.failed {
				p.fetchErr = errors.New("provider unavailable")
			}
			err = s.ProcessOperation(ctx, "target-user", op.ID)
			require.Equal(t, tc.failed, err != nil)
			require.Equal(t, []string{"target-user", "target-user", "target-user"}, consent.users)
			require.Equal(t, []context.Context{ctx, ctx, ctx}, consent.contexts)
			if !tc.allowed || tc.consentErr != nil {
				require.Empty(t, a.events)
				return
			}
			event := phclient.EventRevenueCatOperationApplied
			if tc.failed {
				event = phclient.EventRevenueCatOperationFailed
			}
			require.Equal(t, []string{phclient.EventRevenueCatOperationQueued, phclient.EventAdminSubscriptionOverrideAppliedServer, event}, a.events)
			require.Equal(t, []string{"target-user", "target-user", "target-user"}, a.users)
		})
	}
}

func TestRevenueCatAnalyticsUnconfiguredIsNoOp(t *testing.T) {
	r := &rcTestStore{ops: map[string]domain.RevenueCatOperation{}}
	s := NewRevenueCatService(r, &rcTestProvider{}, rcTestProfile{})
	s.SetGDPRAnalytics(nil)
	op, err := s.RequestGrant(context.Background(), "target", "comp", "premium", nil, false, "admin", "reason")
	require.NoError(t, err)
	require.NoError(t, s.ProcessOperation(context.Background(), "target", op.ID))
}
