//go:build integration

package jobs

// retention_cleanup_check_c9_chain_integration_test.go — the todo-3 C9 chain proof. Every earlier
// test of this path started from a hand-built domain.RevenueCatState and called Save directly, and
// every earlier retention/Plaid test started from a raw SQL marker. Nothing connected the two: no
// test drove a grant EXPIRY through the real RevenueCat fetch path
// (RevenueCatService.Refresh -> applyRevenueCatGrants) and then proved a live Plaid item is actually
// gone. This test is that chain, end to end:
//
//  1. a paid/trial/comp grant lapses and the fresh provider read still returns it;
//  2. refresh drops it and persists subscription_status='inactive' + plaid_cleanup_pending_at;
//  3. the marker is backdated past the 72h plaidDisconnectRepairGracePeriod;
//  4. the real RetentionCleanupCheckWorker disconnect pass runs;
//  5. no live linked_items row remains.
//
// It reuses the existing connectJobsTestDB harness (jobs_integration_helpers_test.go)
// rather than building a new one; only the Plaid HTTP boundary is doubled, and even that double uses
// the production LinkedItemRepository writes.
//
// Run:
//
//	DATABASE_URL=postgres://... go test ./internal/queue/jobs/... -run TestRetentionCleanupCheck_C9Chain -tags integration -v

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	rc "github.com/maxnussbaum/BoostedBudget/internal/integration/revenuecat"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// c9Provider returns exactly one EXPIRED trial grant from the fresh provider read — the shape that
// used to make applyRevenueCatGrants return ErrRevenueCatUnresolved before persisting.
type c9Provider struct{ expired rc.Entitlement }

func (c9Provider) EnsureCustomer(context.Context, string) error { return nil }

func (p c9Provider) FetchEntitlements(context.Context, string) ([]rc.Entitlement, error) {
	return []rc.Entitlement{p.expired}, nil
}

func (c9Provider) FetchSubscriptions(context.Context, string) ([]rc.Subscription, error) {
	return nil, nil
}

func (c9Provider) Grant(context.Context, string, string, *time.Time) error { return nil }
func (c9Provider) Revoke(context.Context, string, string) error            { return nil }

// c9Profiles resolves the RevenueCat customer identity from the real users row.
type c9Profiles struct{ pool *pgxpool.Pool }

func (p c9Profiles) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	var clerk string
	if err := p.pool.QueryRow(ctx, `SELECT clerk_user_id FROM users WHERE id=$1`, id).Scan(&clerk); err != nil {
		return nil, err
	}
	return &domain.User{ID: id, ClerkUserID: clerk}, nil
}

// c9PlaidDisconnect doubles only the Plaid HTTP boundary. Its database effects are the production
// ones: it fetches via LinkedItemRepository.GetActiveItemsByUserID and marks the items disconnected
// via MarkItemsDisconnected. It scopes to the user this test created so a DB-wide candidate scan in
// the worker cannot touch unrelated fixtures, and it honours the worker's own per-item recheck.
type c9PlaidDisconnect struct {
	repo   repository.LinkedItemRepository
	userID string
}

func (d c9PlaidDisconnect) DisconnectAllUserItemsIfEligible(ctx context.Context, userID string, recheck func(context.Context) (bool, error)) error {
	if userID != d.userID {
		return nil
	}
	eligible, err := recheck(ctx)
	if err != nil || !eligible {
		return err
	}
	items, err := d.repo.GetActiveItemsByUserID(ctx, userID)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	return d.repo.MarkItemsDisconnected(ctx, ids)
}

func TestRetentionCleanupCheck_C9Chain_RevenueCatExpiryDisconnectsPlaid(t *testing.T) {
	pool := connectJobsTestDB(t)
	ctx := context.Background()
	enc := newJobsTestFieldEncryptor(t)
	profileRepo := repository.NewProfileRepository(pool, enc)
	linkedRepo := repository.NewLinkedItemRepository(pool, enc)
	claimsRepo := repository.NewWorkClaimsRepository(pool)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_c9_" + suffix,
		Email:              "c9-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, profileRepo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM linked_items WHERE user_id=$1", user.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM revenuecat_operations WHERE user_id=$1", user.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM revenuecat_customers WHERE user_id=$1", user.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1", user.ID)
	})

	item := &domain.LinkedItem{
		UserID:      user.ID,
		ItemID:      "plaid-item-c9-" + suffix,
		AccessToken: "access-token-" + suffix,
		Provider:    "plaid",
	}
	require.NoError(t, linkedRepo.Create(ctx, item))

	// 1-2. Arm through the real RevenueCat fetch path. The expired grant must be dropped so the
	// inactive projection is persisted, not abort the refresh.
	rcRepo := repository.NewRevenueCatRepository(pool, enc)
	expired := time.Now().Add(-time.Hour)
	svc := services.NewRevenueCatService(rcRepo, c9Provider{expired: rc.Entitlement{LookupKey: "trial_premium", ExpiresAt: &expired}}, c9Profiles{pool: pool})
	_, err = svc.Refresh(ctx, user.ID)
	require.NoError(t, err, "a fresh provider read with only an expired grant must still persist the inactive projection")

	var status string
	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT subscription_status, plaid_cleanup_pending_at FROM users WHERE id=$1`, user.ID).Scan(&status, &marker))
	require.Equal(t, "inactive", status, "the RC-driven projection must flip the user inactive")
	require.NotNil(t, marker, "the RC-driven projection must stamp plaid_cleanup_pending_at")

	// 3. Backdate the marker past the 72h grace window.
	_, err = pool.Exec(ctx, `UPDATE users SET plaid_cleanup_pending_at = now() - interval '100 hours' WHERE id=$1`, user.ID)
	require.NoError(t, err)

	// 4. Run the real retention-cleanup disconnect pass.
	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     profileRepo,
		DeletionService: new(MockRetentionDeletionService),
		EmailService:    new(MockRetentionEmailService),
		RetentionMonths: 6,
		Logger:          slog.Default(),
	})
	worker.SetPlaidCleanup(c9PlaidDisconnect{repo: linkedRepo, userID: user.ID})
	worker.SetWorkClaimsRepo(claimsRepo)
	require.NoError(t, worker.Work(ctx, newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})))

	// 5. No live Plaid item remains.
	var live int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM linked_items WHERE user_id=$1 AND (status IS NULL OR status NOT IN ('disconnected','replaced'))`,
		user.ID).Scan(&live))
	require.Zero(t, live, "the retention-cleanup disconnect pass must leave no live Plaid item")
}
