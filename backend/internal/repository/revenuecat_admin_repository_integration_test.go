//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestRevenueCatAdminRepositoryDistinguishesDeliveriesAndProcessingAttempts(t *testing.T) {
	pool, id := revenueCatTestDB(t)
	ctx := context.Background()
	inbox := NewRevenueCatRepository(pool, newTestFieldEncryptor(t))
	admin := NewRevenueCatAdminRepository(pool)
	enqueue := func(context.Context, pgx.Tx, string) error { return nil }
	for range 2 {
		_, err := inbox.AcceptWebhook(ctx, "event", id, "INITIAL_PURCHASE", enqueue)
		require.NoError(t, err)
	}
	require.NoError(t, inbox.CompleteWebhook(ctx, "event", "rate_limited"))
	items, err := admin.ListWebhooks(ctx, 20, 0)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, 2, items[0].Deliveries)
	require.Equal(t, 1, items[0].Attempts)
	require.Nil(t, items[0].ProcessedAt)
	health, err := admin.Health(ctx, time.Now())
	require.NoError(t, err)
	require.Equal(t, int64(2), health.Deliveries)
	require.Equal(t, int64(1), health.Events)
	require.Equal(t, int64(1), health.FailedWebhooks)
	require.Equal(t, int64(0), health.ProcessedEvents)
	require.NoError(t, inbox.CompleteWebhook(ctx, "event", ""))
	event, err := admin.GetWebhook(ctx, "event")
	require.NoError(t, err)
	require.Equal(t, 2, event.Attempts)
	require.NotNil(t, event.ProcessedAt)
}

func TestRevenueCatAdminRepositoryCountsStaleAndPendingSeparately(t *testing.T) {
	pool, id := revenueCatTestDB(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO revenuecat_customers(user_id,grants,access_updated_at,billing_error) VALUES($1,'[{"key":"paid_premium"}]',now()-interval '20 minutes','billing_unavailable')`, id)
	require.NoError(t, err)
	admin := NewRevenueCatAdminRepository(pool)
	health, err := admin.Health(ctx, time.Now())
	require.NoError(t, err)
	require.Equal(t, int64(1), health.Customers)
	require.Equal(t, int64(1), health.StaleActiveCustomers)
	require.Equal(t, int64(1), health.BillingFailures)
	require.Zero(t, health.PendingOperations)
	require.Zero(t, health.Events)
	_, err = admin.ListWebhooks(ctx, 1001, -1)
	require.Error(t, err)
}
