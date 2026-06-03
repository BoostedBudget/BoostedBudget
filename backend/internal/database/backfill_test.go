package database

import (
	"context"
	"strings"
	"testing"

	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testEncryptionSvc is a deterministic encryption service for backfill tests.
type testEncryptionSvc struct{}

func (s *testEncryptionSvc) Encrypt(plaintext string) (string, error) {
	return "TENC[" + plaintext + "]", nil
}

func (s *testEncryptionSvc) Decrypt(ciphertext string) (string, error) {
	if strings.HasPrefix(ciphertext, "TENC[") && strings.HasSuffix(ciphertext, "]") {
		return strings.TrimSuffix(strings.TrimPrefix(ciphertext, "TENC["), "]"), nil
	}
	return ciphertext, nil
}

func newTestEnc(t *testing.T) *appcrypto.FieldEncryptor {
	t.Helper()
	enc, err := appcrypto.NewFieldEncryptor(&testEncryptionSvc{}, []byte("12345678901234567890123456789012"))
	require.NoError(t, err)
	return enc
}

// TestEncryptWithBlind_PlaintextInput verifies plaintext is encrypted and blind index is set
func TestEncryptWithBlind_PlaintextInput(t *testing.T) {
	enc := newTestEnc(t)

	encrypted, blindIdx, err := encryptWithBlind(enc, "user@example.com", true)

	require.NoError(t, err)
	assert.True(t, appcrypto.IsEncrypted(encrypted), "output should start with ENC$")
	assert.NotEmpty(t, blindIdx, "blind index should be populated")
	// Blind index should be deterministic — same input produces same hash
	_, blindIdx2, _ := encryptWithBlind(enc, "user@example.com", true)
	assert.Equal(t, blindIdx, blindIdx2)
}

// TestEncryptWithBlind_AlreadyEncrypted verifies already-encrypted values are not double-encrypted
func TestEncryptWithBlind_AlreadyEncrypted(t *testing.T) {
	enc := newTestEnc(t)

	// First encrypt
	encrypted, blindIdx1, err := encryptWithBlind(enc, "user@example.com", true)
	require.NoError(t, err)
	require.True(t, appcrypto.IsEncrypted(encrypted))

	// Re-encrypt (should skip re-encryption, just recompute blind index)
	reEncrypted, blindIdx2, err := encryptWithBlind(enc, encrypted, true)
	require.NoError(t, err)

	assert.Equal(t, encrypted, reEncrypted, "already-encrypted value should not be double-encrypted")
	assert.Equal(t, blindIdx1, blindIdx2, "blind index should match")
}

// TestEncryptWithBlind_LowerCaseNormalization verifies email blind index is case-normalized
func TestEncryptWithBlind_LowerCaseNormalization(t *testing.T) {
	enc := newTestEnc(t)

	_, blindLower, _ := encryptWithBlind(enc, "user@example.com", true)
	_, blindUpper, _ := encryptWithBlind(enc, "USER@EXAMPLE.COM", true)
	_, blindMixed, _ := encryptWithBlind(enc, "User@Example.Com", true)

	assert.Equal(t, blindLower, blindUpper, "blind index should be case-insensitive for lower=true")
	assert.Equal(t, blindLower, blindMixed, "blind index should be case-insensitive for lower=true")
}

// TestBackfillEncryption_NilInputs verifies nil inputs are handled gracefully
func TestBackfillEncryption_NilInputs(t *testing.T) {
	err := BackfillEncryption(nil, nil, nil)
	assert.NoError(t, err, "nil inputs should be a no-op")
}

// TestBackfillEmailPrefixIndices_NilInputs verifies nil inputs are handled gracefully
// (local dev without ENCRYPTION_KEY should be a no-op).
func TestBackfillEmailPrefixIndices_NilInputs(t *testing.T) {
	err := BackfillEmailPrefixIndices(context.Background(), nil, nil)
	assert.NoError(t, err, "nil inputs should be a no-op")
}
