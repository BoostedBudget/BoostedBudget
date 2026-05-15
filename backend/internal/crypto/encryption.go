// Package crypto provides encryption interfaces and field-level encryption utilities
// for the BoostedBudget application.
package crypto

// EncryptionService defines the interface for encrypting and decrypting sensitive data.
// Uses AES-256-GCM (Galois/Counter Mode) for authenticated encryption.
//
// This interface is defined here (rather than in the services package) to avoid
// circular dependencies: repository → crypto, services → crypto.
type EncryptionService interface {
	// Encrypt encrypts plaintext and returns base64-encoded ciphertext
	Encrypt(plaintext string) (string, error)

	// Decrypt decrypts base64-encoded ciphertext and returns plaintext
	Decrypt(ciphertext string) (string, error)
}
