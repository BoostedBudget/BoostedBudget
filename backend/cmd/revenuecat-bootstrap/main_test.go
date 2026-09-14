package main

import (
	"context"
	"fmt"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	rc "github.com/maxnussbaum/BoostedBudget/internal/integration/revenuecat"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type bootstrapRepo struct {
	users   []*domain.User
	applies int
}

func (r *bootstrapRepo) Inventory(context.Context) ([]*domain.User, error) { return r.users, nil }
func (r *bootstrapRepo) Apply(context.Context, string) (int, error)        { r.applies++; return 1, nil }

type bootstrapProvider struct {
	paid   bool
	calls  []string
	errors map[string]error
}

func (p *bootstrapProvider) ValidateCatalog(context.Context, map[string][]string, string) error {
	return nil
}
func (p *bootstrapProvider) FetchSubscriptions(_ context.Context, id string) ([]rc.Subscription, error) {
	p.calls = append(p.calls, id)
	if err := p.errors[id]; err != nil {
		return nil, err
	}
	if p.paid {
		return []rc.Subscription{{Status: "active", Store: "paddle"}}, nil
	}
	return nil, nil
}

func TestBootstrapChecksLastCustomerBeforeApplyingFirst(t *testing.T) {
	end := time.Now().Add(time.Hour)
	for _, status := range []int{403, 429, 503, 404} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			repo := &bootstrapRepo{users: []*domain.User{
				{ID: "first", ClerkUserID: "clerk_first", TrialEndsAt: &end},
				{ID: "last", ClerkUserID: "clerk_last", TrialEndsAt: &end},
			}}
			provider := &bootstrapProvider{errors: map[string]error{"clerk_last": &rc.HTTPError{StatusCode: status}}}
			_, err := runBootstrap(context.Background(), repo, provider, nil, "app_paddle", true)
			require.Equal(t, []string{"clerk_first", "clerk_last"}, provider.calls)
			if status == 404 {
				require.NoError(t, err)
				require.Equal(t, 2, repo.applies)
			} else {
				require.Error(t, err)
				require.Zero(t, repo.applies)
			}
		})
	}
}
func TestBootstrapInventoryIsReadOnlyAndChecksRemoteBeforeAnyApply(t *testing.T) {
	end := time.Now().Add(time.Hour)
	repo := &bootstrapRepo{users: []*domain.User{{ID: "internal", ClerkUserID: "clerk", TrialEndsAt: &end}}}
	provider := &bootstrapProvider{}
	result, err := runBootstrap(context.Background(), repo, provider, nil, "app_paddle", false)
	require.NoError(t, err)
	require.Equal(t, 1, result.PlannedGrants)
	require.Zero(t, repo.applies)
	require.Equal(t, []string{"clerk"}, provider.calls)
	provider.paid = true
	_, err = runBootstrap(context.Background(), repo, provider, nil, "app_paddle", true)
	require.Error(t, err)
	require.Zero(t, repo.applies)
	provider.paid = false
	_, err = runBootstrap(context.Background(), repo, provider, nil, "app_paddle", true)
	require.NoError(t, err)
	require.Equal(t, 1, repo.applies)
}
