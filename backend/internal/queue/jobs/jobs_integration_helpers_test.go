//go:build integration

package jobs

// Shared integration-test helpers for this package. These were originally declared in
// manual_tier_override_expiry_check_integration_test.go; #1652 deleted that file (its worker is
// gone) but connectJobsTestDB and newJobsTestFieldEncryptor are used by many other integration
// suites here, so they live on in this dedicated helper file.

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/database"
)

// stubEncryptionService is a minimal EncryptionService double for these tests — mirrors
// internal/repository's own test_helpers_test.go stub (unexported there, so re-declared here for
// this package).
type stubEncryptionService struct{}

func (s *stubEncryptionService) Encrypt(plaintext string) (string, error) {
	return "TENC[" + plaintext + "]", nil
}

func (s *stubEncryptionService) Decrypt(ciphertext string) (string, error) {
	const prefix, suffix = "TENC[", "]"
	if len(ciphertext) < len(prefix)+len(suffix) || ciphertext[:len(prefix)] != prefix || ciphertext[len(ciphertext)-len(suffix):] != suffix {
		return ciphertext, nil // passthrough for already-plain values
	}
	return ciphertext[len(prefix) : len(ciphertext)-len(suffix)], nil
}

func connectJobsTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	// Issue #1461: route through database.NewPoolFromURL (not a bare pgxpool.New) so this pool
	// pins session TimeZone=UTC exactly like the production pool does.
	pool, err := database.NewPoolFromURL(context.Background(), dsn)
	require.NoError(t, err, "connect to test database")
	t.Cleanup(pool.Close)
	return pool
}

func newJobsTestFieldEncryptor(t *testing.T) *appcrypto.FieldEncryptor {
	t.Helper()
	key := []byte("12345678901234567890123456789012") // 32-byte AES key
	enc, err := appcrypto.NewFieldEncryptor(&stubEncryptionService{}, key)
	require.NoError(t, err)
	return enc
}
