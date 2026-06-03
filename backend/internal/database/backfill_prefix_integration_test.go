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

// errDecryptEncryptionService wraps backfillTestEncryptionService and injects a
// decryption error for ciphertexts starting with failPrefix.
// Used to test that BackfillEmailPrefixIndices skips corrupt users without aborting.
type errDecryptEncryptionService struct {
	backfillTestEncryptionService
	failPrefix string
}

func (s *errDecryptEncryptionService) Decrypt(ciphertext string) (string, error) {
	if strings.HasPrefix(ciphertext, s.failPrefix) {
		return "", fmt.Errorf("simulated decrypt error for test")
	}
	return s.backfillTestEncryptionService.Decrypt(ciphertext)
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

// TestBackfillEmailPrefixIndices_DecryptError_Integration verifies that
// BackfillEmailPrefixIndices skips users whose emails cannot be decrypted
// without aborting — processing other users successfully (resultSkippedDecrypt path).
func TestBackfillEmailPrefixIndices_DecryptError_Integration(t *testing.T) {
	pool := connectBackfillTestDB(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	// Error-injecting enc: returns an error for ciphertexts starting with failPrefix.
	// The good user's email is stored as "ENC$TENC[...]" which does NOT start with
	// failPrefix, so it decrypts correctly through the embedded service.
	failPrefix := "FAILME_" + suffix
	errSvc := &errDecryptEncryptionService{failPrefix: failPrefix}
	key := []byte("12345678901234567890123456789012")
	encWithError, err := appcrypto.NewFieldEncryptor(errSvc, key)
	require.NoError(t, err)

	// Good user: inserted with the standard backfill enc; email stored as "ENC$TENC[gooduser-...]".
	// errSvc delegates "TENC[...]" to backfillTestEncryptionService — no error, processed correctly.
	goodUserID := insertTestUserForBackfill(t, pool, newBackfillTestEnc(t), "gooduser-"+suffix+"@example.com")
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", goodUserID) }) //nolint:errcheck

	// Bad user: email is "ENC$FAILME_<suffix>_corrupt" inserted directly.
	// IsEncrypted("ENC$...") = true, so DecryptString strips "ENC$" → "FAILME_..."
	// → errSvc.Decrypt("FAILME_...") returns error → resultSkippedDecrypt path.
	badUserID := uuid.New().String()
	corruptEmail := "ENC$" + failPrefix + "_corrupt"
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, email_blind_idx, email_verified,
		    subscription_tier, subscription_status, primary_region, is_active, is_deleted,
		    created_at, updated_at)
		VALUES ($1, $2, $3, 'badhash', false, 'premium', 'active', 'iad', true, false, NOW(), NOW())`,
		badUserID, "clerk_bad_"+badUserID, corruptEmail,
	)
	require.NoError(t, err)
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", badUserID) }) //nolint:errcheck
	_, _ = pool.Exec(ctx, "DELETE FROM user_email_prefix_indices WHERE user_id = $1", badUserID)

	// Backfill must NOT abort despite one undecryptable user.
	err = BackfillEmailPrefixIndices(ctx, pool, encWithError)
	require.NoError(t, err, "backfill must not abort on a decrypt error")

	// Bad user: skipped — no prefix rows written.
	var badCount int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM user_email_prefix_indices WHERE user_id = $1", badUserID,
	).Scan(&badCount))
	assert.Equal(t, 0, badCount, "corrupt user must have no prefix rows (was skipped)")

	// Good user: processed — prefix rows written despite the other user's decrypt error.
	var goodCount int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM user_email_prefix_indices WHERE user_id = $1", goodUserID,
	).Scan(&goodCount))
	assert.Greater(t, goodCount, 0, "valid user must have prefix rows despite another user's decrypt error")
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
