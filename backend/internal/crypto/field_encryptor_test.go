package crypto_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockEncryptionService is a simple XOR-based mock for testing.
// It uses a known key so we can verify encrypt/decrypt round-trips.
type mockEncryptionService struct {
	failEncrypt bool
	failDecrypt bool
}

func (m *mockEncryptionService) Encrypt(plaintext string) (string, error) {
	if m.failEncrypt {
		return "", errors.New("encrypt failed")
	}
	// Simple reversible transform for testing: prefix with "MOCK_"
	return "MOCK_" + plaintext, nil
}

func (m *mockEncryptionService) Decrypt(ciphertext string) (string, error) {
	if m.failDecrypt {
		return "", errors.New("decrypt failed")
	}
	if !strings.HasPrefix(ciphertext, "MOCK_") {
		return "", errors.New("invalid ciphertext")
	}
	return strings.TrimPrefix(ciphertext, "MOCK_"), nil
}

func newTestEncryptor(t *testing.T) *crypto.FieldEncryptor {
	t.Helper()
	svc := &mockEncryptionService{}
	key := []byte("12345678901234567890123456789012") // 32 bytes
	enc, err := crypto.NewFieldEncryptor(svc, key)
	require.NoError(t, err)
	return enc
}

func TestNewFieldEncryptor_InvalidKeySize(t *testing.T) {
	svc := &mockEncryptionService{}

	_, err := crypto.NewFieldEncryptor(svc, []byte("tooshort"))
	require.Error(t, err)
	assert.ErrorIs(t, err, crypto.ErrInvalidKeySize)
}

func TestNewFieldEncryptor_ValidKey(t *testing.T) {
	svc := &mockEncryptionService{}
	key := []byte("12345678901234567890123456789012")
	enc, err := crypto.NewFieldEncryptor(svc, key)
	require.NoError(t, err)
	assert.NotNil(t, enc)
}

func TestEncryptString_RoundTrip(t *testing.T) {
	enc := newTestEncryptor(t)

	plaintext := "sensitive-account-number-12345"
	encrypted, err := enc.EncryptString(plaintext)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(encrypted, "ENC$"), "encrypted value must start with ENC$")

	decrypted, err := enc.DecryptString(encrypted)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestEncryptString_PlaintextPassthrough(t *testing.T) {
	enc := newTestEncryptor(t)

	// Value without ENC$ prefix (pre-migration plaintext) is returned as-is
	plaintext := "old-plaintext-value"
	decrypted, err := enc.DecryptString(plaintext)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestDecryptString_EncryptFailed(t *testing.T) {
	svc := &mockEncryptionService{failEncrypt: true}
	key := []byte("12345678901234567890123456789012")
	enc, err := crypto.NewFieldEncryptor(svc, key)
	require.NoError(t, err)

	_, err = enc.EncryptString("test")
	require.Error(t, err)
}

func TestDecryptString_DecryptFailed(t *testing.T) {
	svc := &mockEncryptionService{failDecrypt: true}
	key := []byte("12345678901234567890123456789012")
	enc, err := crypto.NewFieldEncryptor(svc, key)
	require.NoError(t, err)

	_, err = enc.DecryptString("ENC$something")
	require.Error(t, err)
}

func TestEncryptNullableString_Nil(t *testing.T) {
	enc := newTestEncryptor(t)

	result, err := enc.EncryptNullableString(nil)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestEncryptNullableString_NonNil(t *testing.T) {
	enc := newTestEncryptor(t)

	value := "account-number"
	encrypted, err := enc.EncryptNullableString(&value)
	require.NoError(t, err)
	require.NotNil(t, encrypted)
	assert.True(t, strings.HasPrefix(*encrypted, "ENC$"))

	decrypted, err := enc.DecryptNullableString(encrypted)
	require.NoError(t, err)
	require.NotNil(t, decrypted)
	assert.Equal(t, value, *decrypted)
}

func TestDecryptNullableString_Nil(t *testing.T) {
	enc := newTestEncryptor(t)

	result, err := enc.DecryptNullableString(nil)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestEncryptJSON_RoundTrip(t *testing.T) {
	enc := newTestEncryptor(t)

	raw := json.RawMessage(`{"location":{"city":"New York","lat":40.7128},"amount":100.50}`)

	encrypted, err := enc.EncryptJSON(raw)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(encrypted, "ENC$"))

	decrypted, err := enc.DecryptJSON(encrypted)
	require.NoError(t, err)
	assert.JSONEq(t, string(raw), string(decrypted))
}

func TestEncryptJSON_NilError(t *testing.T) {
	enc := newTestEncryptor(t)

	_, err := enc.EncryptJSON(nil)
	require.Error(t, err)
}

func TestDecryptJSON_PlaintextPassthrough(t *testing.T) {
	enc := newTestEncryptor(t)

	// Raw JSON without ENC$ prefix (pre-migration) is returned as-is
	raw := `{"city":"Boston","zip":"02101"}`
	decrypted, err := enc.DecryptJSON(raw)
	require.NoError(t, err)
	assert.JSONEq(t, raw, string(decrypted))
}

func TestBlindIndex_Deterministic(t *testing.T) {
	enc := newTestEncryptor(t)

	// Same input always produces the same blind index
	idx1 := enc.BlindIndex("item-abc-123")
	idx2 := enc.BlindIndex("item-abc-123")
	assert.Equal(t, idx1, idx2)

	// Different inputs produce different blind indexes
	idx3 := enc.BlindIndex("item-xyz-456")
	assert.NotEqual(t, idx1, idx3)
}

func TestBlindIndex_Format(t *testing.T) {
	enc := newTestEncryptor(t)

	idx := enc.BlindIndex("test-value")
	// HMAC-SHA256 produces 32 bytes = 64 hex chars
	assert.Len(t, idx, 64)
	// Should be hex characters only
	for _, c := range idx {
		assert.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'),
			"blind index should be lowercase hex, got: %c", c)
	}
}

func TestBlindIndex_DifferentKeys(t *testing.T) {
	svc := &mockEncryptionService{}
	key1 := []byte("12345678901234567890123456789012")
	key2 := []byte("abcdefghijklmnopqrstuvwxyz123456")

	enc1, err := crypto.NewFieldEncryptor(svc, key1)
	require.NoError(t, err)
	enc2, err := crypto.NewFieldEncryptor(svc, key2)
	require.NoError(t, err)

	// Same input with different keys produces different blind indexes
	idx1 := enc1.BlindIndex("test")
	idx2 := enc2.BlindIndex("test")
	assert.NotEqual(t, idx1, idx2)
}

func TestBlindIndexNullable_Nil(t *testing.T) {
	enc := newTestEncryptor(t)

	result := enc.BlindIndexNullable(nil)
	assert.Nil(t, result)
}

func TestBlindIndexNullable_NonNil(t *testing.T) {
	enc := newTestEncryptor(t)

	value := "test-value"
	result := enc.BlindIndexNullable(&value)
	require.NotNil(t, result)
	assert.Equal(t, enc.BlindIndex(value), *result)
}

func TestIsEncrypted(t *testing.T) {
	assert.True(t, crypto.IsEncrypted("ENC$somebase64data"))
	assert.False(t, crypto.IsEncrypted("plaintext"))
	assert.False(t, crypto.IsEncrypted(""))
	assert.False(t, crypto.IsEncrypted("enc$lowercase"))
}

// TestBlindIndexWithLabel_DomainSeparation verifies it differs from BlindIndex.
// NOTE: field_encryptor_test.go is package crypto_test — all exported symbols need crypto. prefix.
func TestBlindIndexWithLabel_DomainSeparation(t *testing.T) {
	enc := newTestEncryptor(t)
	without := enc.BlindIndex("test@example.com")
	withLabel := enc.BlindIndexWithLabel("email_prefix", "test@example.com")
	assert.NotEqual(t, without, withLabel,
		"BlindIndexWithLabel must differ from BlindIndex for the same value")
}

// TestBlindIndexWithLabel_Deterministic verifies same label+value always gives same hash.
func TestBlindIndexWithLabel_Deterministic(t *testing.T) {
	enc := newTestEncryptor(t)
	idx1 := enc.BlindIndexWithLabel("email_prefix", "test@example.com")
	idx2 := enc.BlindIndexWithLabel("email_prefix", "test@example.com")
	assert.Equal(t, idx1, idx2)
}

// TestNormalizeEmailForBlindIndex verifies lower+trim behaviour.
// Package is crypto_test — must use crypto. prefix for package-level functions.
func TestNormalizeEmailForBlindIndex(t *testing.T) {
	assert.Equal(t, "user@example.com", crypto.NormalizeEmailForBlindIndex("  USER@EXAMPLE.COM  "))
	assert.Equal(t, "user@example.com", crypto.NormalizeEmailForBlindIndex("user@example.com"))
	assert.Equal(t, "", crypto.NormalizeEmailForBlindIndex("   "))
}

// TestEmailPrefixBlindIndices_Count verifies correct number of indices.
func TestEmailPrefixBlindIndices_Count(t *testing.T) {
	enc := newTestEncryptor(t)
	// "test@example.com" = 16 runes → indices for lengths 3..16 = 14 indices
	indices := crypto.EmailPrefixBlindIndices(enc, "test@example.com")
	assert.Len(t, indices, 14)
}

// TestEmailPrefixBlindIndices_TooShort returns nil when email has < 3 runes.
func TestEmailPrefixBlindIndices_TooShort(t *testing.T) {
	enc := newTestEncryptor(t)
	assert.Nil(t, crypto.EmailPrefixBlindIndices(enc, "ab"))
	assert.Nil(t, crypto.EmailPrefixBlindIndices(enc, ""))
}

// TestEmailPrefixBlindIndices_CapsAt64 verifies prefix count is capped at 64.
func TestEmailPrefixBlindIndices_CapsAt64(t *testing.T) {
	enc := newTestEncryptor(t)
	long := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa@x.com" // 70+ runes
	indices := crypto.EmailPrefixBlindIndices(enc, long)
	assert.Len(t, indices, 64-3+1, "should produce indices for rune lengths 3..64")
}

// TestEmailPrefixBlindIndices_NormalizesInput verifies case/whitespace normalisation.
func TestEmailPrefixBlindIndices_NormalizesInput(t *testing.T) {
	enc := newTestEncryptor(t)
	lower := crypto.EmailPrefixBlindIndices(enc, "test@example.com")
	upper := crypto.EmailPrefixBlindIndices(enc, "TEST@EXAMPLE.COM")
	require.NotNil(t, lower)
	require.NotNil(t, upper)
	assert.Equal(t, lower, upper, "normalisation must produce identical indices regardless of input case")
}

// TestEmailPrefixBlindIndices_RuneSafe verifies multi-byte (non-ASCII) chars are not split.
func TestEmailPrefixBlindIndices_RuneSafe(t *testing.T) {
	enc := newTestEncryptor(t)
	// "ñoño" = 4 runes but 6 bytes in UTF-8 — prefix for length 3 is "ñon" (3 runes, not 4 bytes)
	indices := crypto.EmailPrefixBlindIndices(enc, "ñoño@x.com") // "ñoño@x.com" = 10 runes
	assert.Len(t, indices, 10-3+1, "must count runes, not bytes")
}
