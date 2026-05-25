//go:build integration

package database

// Integration tests for BackfillEmailPrefixIndices.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/database/... -run TestBackfillEmailPrefixIndices_Integration -tags integration -v

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
)

// backfillTestEncryptionService is a deterministic mock EncryptionService for backfill
// integration tests. It uses the same TENC pattern as other repository tests so that
// BackfillEmailPrefixIndices can decrypt emails written by insertTestUserForBackfill.
type backfillTestEncryptionService struct{}

func (s *backfillTestEncryptionService) Encrypt(plaintext string) (string, error) {
	return "TENC[" + plaintext + "]", nil
}

func (s *backfillTestEncryptionService) Decrypt(ciphertext string) (string, error) {
	if strings.HasPrefix(ciphertext, "TENC[") && strings.HasSuffix(ciphertext, "]") {
		return strings.TrimSuffix(strings.TrimPrefix(ciphertext, "TENC["), "]"), nil
	}
	return ciphertext, nil
}

func TestBackfillEmailPrefixIndices_Integration(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	// Insert a test user with no prefix rows
	userID := insertTestUserForBackfill(t, pool, enc, "backfill-"+suffix+"@example.com")
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", userID) }) //nolint:errcheck

	t.Run("backfill writes prefix rows for user without any", func(t *testing.T) {
		err := BackfillEmailPrefixIndices(ctx, pool, enc)
		require.NoError(t, err)

		var count int
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT COUNT(*) FROM user_email_prefix_indices WHERE user_id = $1", userID,
		).Scan(&count))
		assert.Greater(t, count, 0, "backfill should have written prefix rows")
	})

	t.Run("re-run is a no-op (ON CONFLICT DO NOTHING prevents duplicates)", func(t *testing.T) {
		var countBefore int
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT COUNT(*) FROM user_email_prefix_indices WHERE user_id = $1", userID,
		).Scan(&countBefore))

		err := BackfillEmailPrefixIndices(ctx, pool, enc)
		require.NoError(t, err)

		var countAfter int
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT COUNT(*) FROM user_email_prefix_indices WHERE user_id = $1", userID,
		).Scan(&countAfter))
		assert.Equal(t, countBefore, countAfter, "re-run should not create duplicate rows")
	})
}

func connectBackfillTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func newBackfillTestEnc(t *testing.T) *appcrypto.FieldEncryptor {
	t.Helper()
	svc := &backfillTestEncryptionService{}
	key := []byte("12345678901234567890123456789012") // 32-byte AES key
	enc, err := appcrypto.NewFieldEncryptor(svc, key)
	require.NoError(t, err)
	return enc
}

func insertTestUserForBackfill(t *testing.T, pool *pgxpool.Pool, enc *appcrypto.FieldEncryptor, email string) string {
	t.Helper()
	ctx := context.Background()
	userID := uuid.New().String()
	encEmail, err := enc.EncryptString(email)
	require.NoError(t, err)
	emailBlindIdx := enc.BlindIndex(strings.ToLower(strings.TrimSpace(email)))
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, email_blind_idx, email_verified,
		    subscription_tier, subscription_status, primary_region, is_active, is_deleted,
		    created_at, updated_at)
		VALUES ($1, $2, $3, $4, false, 'premium', 'active', 'iad', true, false, NOW(), NOW())`,
		userID, "clerk_backfill_"+userID, encEmail, emailBlindIdx,
	)
	require.NoError(t, err)
	// Ensure no prefix rows exist yet (simulates a pre-backfill state)
	_, err = pool.Exec(ctx, "DELETE FROM user_email_prefix_indices WHERE user_id = $1", userID)
	require.NoError(t, err)
	return userID
}
