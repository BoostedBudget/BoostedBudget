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
