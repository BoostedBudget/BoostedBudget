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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	rc "github.com/maxnussbaum/BoostedBudget/internal/integration/revenuecat"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// c9AnalyticsCapture is one event as it reached the transport, after the REAL Gateway resolved
// canonical identity and validated the generated contract.
type c9AnalyticsCapture struct {
	subject string
	event   string
	props   map[string]interface{}
}

type c9AnalyticsTransport struct{ captures []c9AnalyticsCapture }

func (t *c9AnalyticsTransport) Capture(subject, event string, props map[string]interface{}) {
	t.captures = append(t.captures, c9AnalyticsCapture{subject: subject, event: event, props: props})
}
func (*c9AnalyticsTransport) Identify(string, map[string]interface{}) {}
func (*c9AnalyticsTransport) Close()                                  {}

type c9AnalyticsAuthority struct{ subject string }

func (a c9AnalyticsAuthority) WithConsentedSubject(_ context.Context, _ string, emit func(string)) (bool, error) {
	emit(a.subject)
	return true, nil
}

func c9CapturedNames(t *c9AnalyticsTransport) []string {
	names := make([]string, 0, len(t.captures))
	for _, capture := range t.captures {
		names = append(names, capture.event)
	}
	return names
}

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
	var clerk, status string
	if err := p.pool.QueryRow(ctx, `SELECT clerk_user_id, subscription_status FROM users WHERE id=$1`, id).Scan(&clerk, &status); err != nil {
		return nil, err
	}
	return &domain.User{ID: id, ClerkUserID: clerk, SubscriptionStatus: status}, nil
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

type c9UserCreator interface {
	CreateUserTx(context.Context, pgx.Tx, *domain.User) error
}

type c9ItemCreator interface {
	Create(context.Context, *domain.LinkedItem) error
}

// c9SeedUserAndItem creates an active user with one live Plaid item through the production write
// paths and registers cleanup. Both C9 tests start from this identical fixture.
func c9SeedUserAndItem(t *testing.T, pool *pgxpool.Pool, users c9UserCreator, items c9ItemCreator, prefix string) *domain.User {
	t.Helper()
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_" + prefix + "_" + suffix,
		Email:              prefix + "-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, users.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM linked_items WHERE user_id=$1", user.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM revenuecat_operations WHERE user_id=$1", user.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM revenuecat_customers WHERE user_id=$1", user.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1", user.ID)
	})
	require.NoError(t, items.Create(ctx, &domain.LinkedItem{
		UserID:      user.ID,
		ItemID:      "plaid-item-" + prefix + "-" + suffix,
		AccessToken: "access-token-" + suffix,
		Provider:    "plaid",
	}))
	return user
}

func TestRetentionCleanupCheck_C9Chain_RevenueCatExpiryDisconnectsPlaid(t *testing.T) {
	pool := connectJobsTestDB(t)
	ctx := context.Background()
	enc := newJobsTestFieldEncryptor(t)
	profileRepo := repository.NewProfileRepository(pool, enc)
	linkedRepo := repository.NewLinkedItemRepository(pool, enc)
	claimsRepo := repository.NewWorkClaimsRepository(pool)

	user := c9SeedUserAndItem(t, pool, profileRepo, linkedRepo, "c9")

	// 1-2. Arm through the real RevenueCat fetch path. The expired grant must be dropped so the
	// inactive projection is persisted, not abort the refresh.
	rcRepo := repository.NewRevenueCatRepository(pool, enc)
	expired := time.Now().Add(-time.Hour)
	svc := services.NewRevenueCatService(rcRepo, c9Provider{expired: rc.Entitlement{LookupKey: "trial_premium", ExpiresAt: &expired}}, c9Profiles{pool: pool})
	transport := &c9AnalyticsTransport{}
	gateway := phclient.NewGateway(transport, c9AnalyticsAuthority{subject: "canonical-c9"}, slog.Default())
	svc.SetAnalytics(gateway)
	_, err := svc.Refresh(ctx, user.ID)
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
	worker.SetAnalytics(gateway)
	require.NoError(t, worker.Work(ctx, newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})))

	// 5. No live Plaid item remains.
	var live int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM linked_items WHERE user_id=$1 AND (status IS NULL OR status NOT IN ('disconnected','replaced'))`,
		user.ID).Scan(&live))
	require.Zero(t, live, "the retention-cleanup disconnect pass must leave no live Plaid item")

	// 6. The full ordered chain reached the transport through the REAL Gateway: resolve → inactive
	// → retention armed → disconnect, with the disconnect LAST.
	require.Equal(t,
		[]string{
			phclient.EventRevenueCatAccessInactive,
			phclient.EventRevenueCatRetentionArmed,
			phclient.EventRevenueCatPlaidCleanupDisconnected,
		},
		c9CapturedNames(transport),
		"the C9 chain must emit access-inactive, then retention-armed, then the disconnect last")
	for _, capture := range transport.captures {
		require.Equal(t, "canonical-c9", capture.subject)
	}
	require.Equal(t, map[string]interface{}{"pending_operations": int64(0)}, transport.captures[0].props)
	require.Equal(t, map[string]interface{}{"deferred_for_pending_operations": false}, transport.captures[1].props)
	require.Equal(t, map[string]interface{}{"marker_cleared": true}, transport.captures[2].props)
	for _, capture := range transport.captures {
		for _, forbidden := range []string{"reason", "actor_id", "email", "amount", "account_id", "transaction_id", "item_id", "customer_id", "plaid_item_id"} {
			require.NotContains(t, capture.props, forbidden)
		}
	}
}

// TestRetentionCleanupCheck_C9Chain_RevenueCatFutureExpiryArmsNothing is the failure QA: a future
// ExpiresAt keeps the user active, so no access, retention, or disconnect event may fire and the
// Plaid item must survive.
func TestRetentionCleanupCheck_C9Chain_RevenueCatFutureExpiryArmsNothing(t *testing.T) {
	pool := connectJobsTestDB(t)
	ctx := context.Background()
	enc := newJobsTestFieldEncryptor(t)
	profileRepo := repository.NewProfileRepository(pool, enc)
	linkedRepo := repository.NewLinkedItemRepository(pool, enc)
	claimsRepo := repository.NewWorkClaimsRepository(pool)

	user := c9SeedUserAndItem(t, pool, profileRepo, linkedRepo, "c9future")

	rcRepo := repository.NewRevenueCatRepository(pool, enc)
	future := time.Now().Add(24 * time.Hour)
	svc := services.NewRevenueCatService(rcRepo, c9Provider{expired: rc.Entitlement{LookupKey: "trial_premium", ExpiresAt: &future}}, c9Profiles{pool: pool})
	transport := &c9AnalyticsTransport{}
	gateway := phclient.NewGateway(transport, c9AnalyticsAuthority{subject: "canonical-c9"}, slog.Default())
	svc.SetAnalytics(gateway)
	_, err := svc.Refresh(ctx, user.ID)
	require.NoError(t, err)

	var status string
	var marker *time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT subscription_status, plaid_cleanup_pending_at FROM users WHERE id=$1`, user.ID).Scan(&status, &marker))
	require.NotEqual(t, "inactive", status, "an unexpired grant must keep the user out of the inactive state")
	require.Nil(t, marker, "retention must not be armed while access is live")

	worker := NewRetentionCleanupCheckWorker(RetentionCleanupCheckWorkerDeps{
		ProfileRepo:     profileRepo,
		DeletionService: new(MockRetentionDeletionService),
		EmailService:    new(MockRetentionEmailService),
		RetentionMonths: 6,
		Logger:          slog.Default(),
	})
	worker.SetPlaidCleanup(c9PlaidDisconnect{repo: linkedRepo, userID: user.ID})
	worker.SetWorkClaimsRepo(claimsRepo)
	worker.SetAnalytics(gateway)
	require.NoError(t, worker.Work(ctx, newTestRetentionCleanupJob(RetentionCleanupCheckArgs{TriggerSource: "periodic"})))

	require.Empty(t, transport.captures, "a live grant must not emit retention or disconnect events")

	var live int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM linked_items WHERE user_id=$1 AND (status IS NULL OR status NOT IN ('disconnected','replaced'))`,
		user.ID).Scan(&live))
	require.Equal(t, 1, live, "a live user's Plaid item must survive the disconnect pass")
}
