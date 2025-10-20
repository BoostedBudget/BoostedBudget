package services

import (
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEncryptionService_EncryptDecryptRoundTrip verifies that encryption and decryption work correctly together
func TestEncryptionService_EncryptDecryptRoundTrip(t *testing.T) {
	// Given: A valid encryption service with 32-byte key
	key := "12345678901234567890123456789012" // 32 bytes for AES-256
	cfg := &config.Config{EncryptionKey: key}

	service, err := NewEncryptionService(cfg)
	require.NoError(t, err, "service creation should succeed with valid key")

	plaintext := "plaid-sandbox-abc123-access-token"

	// When: We encrypt and then decrypt
	encrypted, err := service.Encrypt(plaintext)
	require.NoError(t, err, "encryption should succeed")
	assert.NotEqual(t, plaintext, encrypted, "encrypted text should differ from plaintext")

	decrypted, err := service.Decrypt(encrypted)
	require.NoError(t, err, "decryption should succeed")

	// Then: Decrypted text matches original
	assert.Equal(t, plaintext, decrypted, "decrypted text should match original plaintext")
}

// TestEncryptionService_DecryptWithWrongKey verifies that decryption fails with wrong key
func TestEncryptionService_DecryptWithWrongKey(t *testing.T) {
	// Given: Two encryption services with different keys
	key1 := "12345678901234567890123456789012"
	key2 := "ABCDEFGHIJKLMNOPQRSTUVWXYZ123456"

	cfg1 := &config.Config{EncryptionKey: key1}
	cfg2 := &config.Config{EncryptionKey: key2}

	service1, err := NewEncryptionService(cfg1)
	require.NoError(t, err)

	service2, err := NewEncryptionService(cfg2)
	require.NoError(t, err)

	plaintext := "plaid-sandbox-secret-token"

	// When: Encrypt with key1, try to decrypt with key2
	encrypted, err := service1.Encrypt(plaintext)
	require.NoError(t, err)

	_, err = service2.Decrypt(encrypted)

	// Then: Decryption should fail
	assert.Error(t, err, "decryption with wrong key should fail")
}

// TestEncryptionService_MissingKey verifies that service creation fails without encryption key
func TestEncryptionService_MissingKey(t *testing.T) {
	// Given: Config with empty encryption key
	cfg := &config.Config{EncryptionKey: ""}

	// When: Try to create encryption service
	_, err := NewEncryptionService(cfg)

	// Then: Should return error
	assert.Error(t, err, "service creation should fail with missing key")
	assert.Contains(t, err.Error(), "encryption key", "error should mention encryption key")
}

// TestEncryptionService_EmptyPlaintext verifies handling of empty string
func TestEncryptionService_EmptyPlaintext(t *testing.T) {
	// Given: Valid encryption service
	key := "12345678901234567890123456789012"
	cfg := &config.Config{EncryptionKey: key}

	service, err := NewEncryptionService(cfg)
	require.NoError(t, err)

	// When: Encrypt empty string
	encrypted, err := service.Encrypt("")
	require.NoError(t, err, "encrypting empty string should succeed")

	decrypted, err := service.Decrypt(encrypted)
	require.NoError(t, err, "decrypting empty string should succeed")

	// Then: Should get empty string back
	assert.Equal(t, "", decrypted, "should get empty string back")
}

// TestEncryptionService_KeyValidation verifies that key must be exactly 32 bytes
func TestEncryptionService_KeyValidation(t *testing.T) {
	testCases := []struct {
		name      string
		key       string
		shouldErr bool
	}{
		{
			name:      "valid 32-byte key",
			key:       "12345678901234567890123456789012",
			shouldErr: false,
		},
		{
			name:      "too short (16 bytes)",
			key:       "1234567890123456",
			shouldErr: true,
		},
		{
			name:      "too long (64 bytes)",
			key:       "1234567890123456789012345678901212345678901234567890123456789012",
			shouldErr: true,
		},
		{
			name:      "empty key",
			key:       "",
			shouldErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Given: Config with test key
			cfg := &config.Config{EncryptionKey: tc.key}

			// When: Try to create service
			_, err := NewEncryptionService(cfg)

			// Then: Check if error matches expectation
			if tc.shouldErr {
				assert.Error(t, err, "should return error for invalid key length")
			} else {
				assert.NoError(t, err, "should succeed with valid 32-byte key")
			}
		})
	}
}
