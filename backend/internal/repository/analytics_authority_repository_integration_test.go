//go:build integration

package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyticsAuthoritySerializesConsentWithdrawalWithEmission(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	userID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region, is_active, is_deleted)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad', TRUE, FALSE)`,
		userID, "clerk_analytics_"+userID, "analytics-"+userID+"@example.com")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", userID) })
	_, err = pool.Exec(ctx, `INSERT INTO privacy_settings (user_id, gdpr_analytics_consent) VALUES ($1, TRUE)`, userID)
	require.NoError(t, err)

	repo := NewAnalyticsAuthorityRepository(pool, newTestFieldEncryptor(t))
	emissionEntered := make(chan struct{})
	releaseEmission := make(chan struct{})
	authorityDone := make(chan error, 1)
	go func() {
		_, authorityErr := repo.WithConsentedSubject(ctx, userID, func(string) {
			close(emissionEntered)
			<-releaseEmission
		})
		authorityDone <- authorityErr
	}()
	<-emissionEntered

	withdrawalDone := make(chan error, 1)
	go func() {
		_, updateErr := pool.Exec(ctx, `UPDATE privacy_settings SET gdpr_analytics_consent = FALSE WHERE user_id = $1`, userID)
		withdrawalDone <- updateErr
	}()
	select {
	case err := <-withdrawalDone:
		t.Fatalf("withdrawal committed before the authorized emission boundary: %v", err)
	case <-time.After(100 * time.Millisecond):
		// Expected: FOR SHARE holds the privacy row until the enqueue callback returns.
	}
	close(releaseEmission)
	require.NoError(t, <-authorityDone)
	require.NoError(t, <-withdrawalDone)

	calledAfterWithdrawal := false
	granted, err := repo.WithConsentedSubject(ctx, userID, func(string) { calledAfterWithdrawal = true })
	require.NoError(t, err)
	assert.False(t, granted)
	assert.False(t, calledAfterWithdrawal)
}
