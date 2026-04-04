package repository

import (
	"strings"
	"testing"

	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/stretchr/testify/require"
)

// testEncryptionService is a simple mock EncryptionService for repository tests.
// Encrypt prefixes with "ENC$" and a separator so that DecryptString's ENC$ passthrough
// still works, and Decrypt strips it.
type testEncryptionService struct{}

func (t *testEncryptionService) Encrypt(plaintext string) (string, error) {
	// Wrap in brackets so the decrypted value is recoverable deterministically.
	return "TENC[" + plaintext + "]", nil
}

func (t *testEncryptionService) Decrypt(ciphertext string) (string, error) {
	if !strings.HasPrefix(ciphertext, "TENC[") || !strings.HasSuffix(ciphertext, "]") {
		return ciphertext, nil // passthrough for already-plain values
	}
	return strings.TrimSuffix(strings.TrimPrefix(ciphertext, "TENC["), "]"), nil
}

// newTestFieldEncryptor returns a FieldEncryptor backed by testEncryptionService.
func newTestFieldEncryptor(t *testing.T) *appcrypto.FieldEncryptor {
	t.Helper()
	svc := &testEncryptionService{}
	key := []byte("12345678901234567890123456789012") // 32-byte AES key
	enc, err := appcrypto.NewFieldEncryptor(svc, key)
	require.NoError(t, err)
	return enc
}
