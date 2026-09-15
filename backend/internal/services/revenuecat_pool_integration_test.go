//go:build integration

package services

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	rc "github.com/maxnussbaum/BoostedBudget/internal/integration/revenuecat"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/disposabledb"
	"github.com/stretchr/testify/require"
)

type rcPoolProfile struct{ pool *pgxpool.Pool }

func (p rcPoolProfile) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	u := &domain.User{ID: id}
	err := p.pool.QueryRow(ctx, `SELECT clerk_user_id FROM users WHERE id=$1`, id).Scan(&u.ClerkUserID)
	return u, err
}

type rcPoolCipher struct{}

func (rcPoolCipher) Encrypt(s string) (string, error) { return s, nil }
func (rcPoolCipher) Decrypt(s string) (string, error) { return s, nil }

func TestRevenueCatService_OneConnectionPoolDoesNotDeadlock(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL required")
	}
	cfg, err := disposabledb.ParseURL(url)
	require.NoError(t, err)
	cfg.MaxConns = 1
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	schema := "rc_pool_" + uuid.NewString()[:8]
	_, err = pool.Exec(ctx, `CREATE SCHEMA `+schema)
	require.NoError(t, err)
	pool.Close()
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err = pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`); pool.Close() })
	_, err = pool.Exec(ctx, `CREATE TABLE users(id uuid PRIMARY KEY,had_trial boolean NOT NULL DEFAULT false,clerk_user_id text NOT NULL,
 subscription_tier text NOT NULL DEFAULT 'premium', subscription_status text NOT NULL DEFAULT 'inactive',
 subscription_ends_at timestamptz, manual_tier_override text, manual_tier_ends_at timestamptz,
 retention_started_at timestamptz, retention_expires_at timestamptz, retention_notice_30d_sent_at timestamptz,
 retention_notice_7d_sent_at timestamptz, plaid_cleanup_pending_at timestamptz, updated_at timestamptz DEFAULT now(), is_deleted boolean DEFAULT false)`)
	require.NoError(t, err)
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000208_revenuecat_authority.up.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(migration))
	require.NoError(t, err)
	erasureMigration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000210_revenuecat_erasure_fence.up.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(erasureMigration))
	require.NoError(t, err)
	id := uuid.NewString()
	_, err = pool.Exec(ctx, `INSERT INTO users(id,clerk_user_id) VALUES($1,'clerk_pool_user')`, id)
	require.NoError(t, err)
	enc, err := appcrypto.NewFieldEncryptor(rcPoolCipher{}, []byte("12345678901234567890123456789012"))
	require.NoError(t, err)
	repo := repository.NewRevenueCatRepository(pool, enc)
	p := &rcTestProvider{}
	s := NewRevenueCatService(repo, p, rcPoolProfile{pool})
	t.Run("refresh", func(t *testing.T) {
		bounded, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		_, err := s.Refresh(bounded, id)
		require.NoError(t, err, "profile read must not wait on its own customer transaction")
	})
	t.Run("grant", func(t *testing.T) {
		op := domain.RevenueCatOperation{UserID: id, Class: "comp", Key: "comp_premium", Reason: "pool fixture"}
		require.NoError(t, repo.RequestOperation(ctx, &op))
		bounded, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		require.NoError(t, s.ProcessOperation(bounded, id, op.ID), "grant identity lookup must precede customer transaction")
	})
	t.Run("persistent identity failures yield to new work", func(t *testing.T) {
		old := domain.RevenueCatOperation{UserID: id, Class: "comp", Key: "comp_premium", Reason: "old identity failure", CreatedAt: time.Now().Add(-time.Hour)}
		next := domain.RevenueCatOperation{UserID: id, Class: "trial", Key: "trial_premium", Reason: "new intent"}
		require.NoError(t, repo.RequestOperation(ctx, &old))
		require.NoError(t, repo.RequestOperation(ctx, &next))
		failing := NewRevenueCatService(repo, p, rcUnavailableProfile{})
		for range 2 {
			require.Error(t, failing.ProcessOperation(ctx, id, old.ID))
		}
		pending, err := repo.PendingOperations(ctx, 1)
		require.NoError(t, err)
		require.Len(t, pending, 1)
		require.Equal(t, next.ID, pending[0].ID)
		var attempts int
		var message string
		require.NoError(t, pool.QueryRow(ctx, `SELECT attempts,last_error FROM revenuecat_operations WHERE id=$1`, old.ID).Scan(&attempts, &message))
		require.Equal(t, 2, attempts)
		require.Equal(t, "provider_unavailable", message)
	})
}

// rcExpiredGrantProvider is the concurrency-safe provider double used by
// TestConcurrentRefreshes_ArmOneRetentionEpisode. The real provider interface may be called from
// more than one refresh goroutine in that test, so its mutable counters are mutex-guarded (the
// shared rcTestProvider in revenuecat_service_test.go is single-goroutine only).
type rcExpiredGrantProvider struct {
	mu      sync.Mutex
	grant   rc.Entitlement
	fetches int
}

func (p *rcExpiredGrantProvider) EnsureCustomer(context.Context, string) error { return nil }

func (p *rcExpiredGrantProvider) FetchEntitlements(context.Context, string) ([]rc.Entitlement, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fetches++
	return []rc.Entitlement{p.grant}, nil
}

func (p *rcExpiredGrantProvider) FetchSubscriptions(context.Context, string) ([]rc.Subscription, error) {
	return nil, nil
}

func (p *rcExpiredGrantProvider) Grant(context.Context, string, string, *time.Time) error { return nil }
func (p *rcExpiredGrantProvider) Revoke(context.Context, string, string) error             { return nil }

// rcConcurrencyFixture is an isolated schema + users table + RevenueCat migration set, plus a
// test-local trigger that records every retention-arm transition. A trigger is used because the
// arm writes clock_timestamp() internally and there is no episode counter in production — counting
// the arms is the only way to prove "exactly ONE episode" rather than merely "a clock is set".
type rcConcurrencyFixture struct {
	pool *pgxpool.Pool
	repo repository.RevenueCatRepository
	id   string
}

func newRCConcurrencyFixture(t *testing.T, url string) rcConcurrencyFixture {
	t.Helper()
	ctx := context.Background()
	cfg, err := disposabledb.ParseURL(url)
	require.NoError(t, err)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	schema := "rc_conc_" + uuid.NewString()[:8]
	_, err = pool.Exec(ctx, `CREATE SCHEMA `+schema)
	require.NoError(t, err)
	pool.Close()
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err = pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`); pool.Close() })
	_, err = pool.Exec(ctx, `CREATE TABLE users(id uuid PRIMARY KEY,had_trial boolean NOT NULL DEFAULT false,clerk_user_id text NOT NULL,
 subscription_tier text NOT NULL DEFAULT 'premium', subscription_status text NOT NULL DEFAULT 'inactive',
 subscription_ends_at timestamptz, manual_tier_override text, manual_tier_ends_at timestamptz,
 retention_started_at timestamptz, retention_expires_at timestamptz, retention_notice_30d_sent_at timestamptz,
 retention_notice_7d_sent_at timestamptz, plaid_cleanup_pending_at timestamptz, updated_at timestamptz DEFAULT now(), is_deleted boolean DEFAULT false)`)
	require.NoError(t, err)
	for _, name := range []string{"000208_revenuecat_authority.up.sql", "000210_revenuecat_erasure_fence.up.sql"} {
		migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		require.NoError(t, err)
		_, err = pool.Exec(ctx, string(migration))
		require.NoError(t, err)
	}
	_, err = pool.Exec(ctx, `CREATE TABLE retention_arm_audit(id bigserial PRIMARY KEY, armed_at timestamptz NOT NULL DEFAULT clock_timestamp())`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `CREATE FUNCTION count_retention_arm() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.subscription_status = 'inactive'
			   AND OLD.subscription_status IN ('active','trial','trialing')
			   AND NEW.retention_started_at IS DISTINCT FROM OLD.retention_started_at THEN
				INSERT INTO retention_arm_audit DEFAULT VALUES;
			END IF;
			RETURN NEW;
		END $$`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `CREATE TRIGGER count_retention_arm BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION count_retention_arm()`)
	require.NoError(t, err)
	id := uuid.NewString()
	_, err = pool.Exec(ctx, `INSERT INTO users(id,clerk_user_id,subscription_status) VALUES($1,'clerk_rc_conc','active')`, id)
	require.NoError(t, err)
	enc, err := appcrypto.NewFieldEncryptor(rcPoolCipher{}, []byte("12345678901234567890123456789012"))
	require.NoError(t, err)
	return rcConcurrencyFixture{pool: pool, repo: repository.NewRevenueCatRepository(pool, enc), id: id}
}

// TestConcurrentRefreshes_ArmOneRetentionEpisode is the todo-3 work-claim replacement proof. The
// old trial-expiration work claim guarded the (now-deleted) ExpireTrial flow; its only external side effect (the
// Plaid disconnect) is now driven by RetentionCleanupCheckWorker under its OWN claim, so the
// remaining question is whether the retention ARMING itself is single-writer.
//
// The row-lock reasoning this test exercises:
//   - RevenueCatRepository.WithCustomer takes SELECT ... FOR UPDATE on the revenuecat_customers
//     row, so two refreshes for one user serialize end to end.
//   - Save's projection locks the users row FOR NO KEY UPDATE and decides the arm from the
//     PRE-UPDATE subscription_status, so once the first refresh arms the clock the second sees
//     'inactive' and PRESERVES it (it is not a new episode).
//
// Falsifiability: a second arm writes another retention_started_at and inserts a second
// retention_arm_audit row, failing the count == 1 assertion; today, before the drop-guard fix,
// both refreshes abort and the count is 0.
func TestConcurrentRefreshes_ArmOneRetentionEpisode(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL required")
	}
	fx := newRCConcurrencyFixture(t, url)
	ctx := context.Background()

	expired := time.Now().Add(-time.Hour)
	p := &rcExpiredGrantProvider{grant: rc.Entitlement{LookupKey: "trial_premium", ExpiresAt: &expired}}
	s := NewRevenueCatService(fx.repo, p, rcPoolProfile{fx.pool})

	const refreshers = 2
	var wg sync.WaitGroup
	errs := make([]error, refreshers)
	start := make(chan struct{})
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = s.Refresh(ctx, fx.id)
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		require.NoErrorf(t, err, "concurrent refresh %d must succeed", i)
	}

	var arms int
	require.NoError(t, fx.pool.QueryRow(ctx, `SELECT count(*) FROM retention_arm_audit`).Scan(&arms))
	require.Equal(t, 1, arms, "two concurrent refreshes must arm exactly ONE retention episode")

	var status string
	var started, marker *time.Time
	require.NoError(t, fx.pool.QueryRow(ctx,
		`SELECT subscription_status, retention_started_at, plaid_cleanup_pending_at FROM users WHERE id=$1`,
		fx.id).Scan(&status, &started, &marker))
	require.Equal(t, "inactive", status)
	require.NotNil(t, started)
	require.NotNil(t, marker)

	// A later refresh whose projection is already inactive must preserve, never restart, the
	// episode — the second half of "repeated inactive refreshes never restart its clock".
	_, err := s.Refresh(ctx, fx.id)
	require.NoError(t, err)
	require.NoError(t, fx.pool.QueryRow(ctx, `SELECT count(*) FROM retention_arm_audit`).Scan(&arms))
	require.Equal(t, 1, arms, "a repeat refresh must not start a second retention episode")
}
