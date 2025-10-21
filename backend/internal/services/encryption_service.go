// Package services provides business logic and external service integrations.
// This file implements EncryptionService for secure data encryption.
package services

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
)

var (
	// ErrEncryptionKeyRequired is returned when encryption key is missing
	ErrEncryptionKeyRequired = errors.New("encryption key is required")

	// ErrInvalidKeySize is returned when encryption key is not 32 bytes (AES-256)
	ErrInvalidKeySize = errors.New("encryption key must be exactly 32 bytes for AES-256")
)

// EncryptionService defines the interface for encrypting and decrypting sensitive data.
// Uses AES-256-GCM (Galois/Counter Mode) for authenticated encryption.
//
// Example usage:
//
//	service, err := NewEncryptionService(cfg)
//	if err != nil {
//	    return err
//	}
//	encrypted, err := service.Encrypt("sensitive-data")
//	plaintext, err := service.Decrypt(encrypted)
type EncryptionService interface {
	// Encrypt encrypts plaintext and returns base64-encoded ciphertext
	// Returns error if encryption fails
	Encrypt(plaintext string) (string, error)

	// Decrypt decrypts base64-encoded ciphertext and returns plaintext
	// Returns error if decryption fails or authentication check fails
	Decrypt(ciphertext string) (string, error)
}

// encryptionService implements EncryptionService with AES-256-GCM
type encryptionService struct {
	key []byte
}

// NewEncryptionService creates a new EncryptionService with validated configuration.
//
// Validates that:
//   - Encryption key is present (ErrEncryptionKeyRequired)
//   - Encryption key is exactly 32 bytes for AES-256 (ErrInvalidKeySize)
//
// The encryption key should be provided via ENCRYPTION_KEY environment variable
// and must be kept secret. For production, use a secure key management system.
func NewEncryptionService(cfg *config.Config) (EncryptionService, error) {
	// Validate encryption key is present
	if cfg.EncryptionKey == "" {
		return nil, fmt.Errorf("%w: ENCRYPTION_KEY environment variable must be set", ErrEncryptionKeyRequired)
	}

	// Validate key size (must be 32 bytes for AES-256)
	keyBytes := []byte(cfg.EncryptionKey)
	if len(keyBytes) != 32 {
		return nil, fmt.Errorf("%w: got %d bytes, need 32 bytes", ErrInvalidKeySize, len(keyBytes))
	}

	return &encryptionService{
		key: keyBytes,
	}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM and returns base64-encoded ciphertext.
//
// The encrypted data includes:
//   - 12-byte nonce (randomly generated)
//   - Ciphertext
//   - 16-byte authentication tag (GMAC)
//
// Format: base64(nonce + ciphertext + tag)
//
// Returns error if:
//   - Random number generation fails
//   - Encryption operation fails
func (s *encryptionService) Encrypt(plaintext string) (string, error) {
	// Create AES cipher block
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode (Galois/Counter Mode for authenticated encryption)
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate random nonce (number used once)
	// GCM standard nonce size is 12 bytes
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt and authenticate
	// GCM automatically appends authentication tag to ciphertext
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	// Encode as base64 for storage in database
	encoded := base64.StdEncoding.EncodeToString(ciphertext)

	return encoded, nil
}

// Decrypt decrypts base64-encoded ciphertext and returns plaintext.
//
// Verifies the authentication tag to ensure data hasn't been tampered with.
// Returns error if:
//   - Base64 decoding fails
//   - Ciphertext is too short (< nonce size)
//   - Authentication check fails (data tampered or wrong key)
//   - Decryption fails
func (s *encryptionService) Decrypt(ciphertext string) (string, error) {
	// Decode from base64
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

	// Create AES cipher block
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Verify data is long enough to contain nonce
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	// Extract nonce and encrypted data
	nonce, encryptedData := data[:nonceSize], data[nonceSize:]

	// Decrypt and verify authentication tag
	plaintext, err := gcm.Open(nil, nonce, encryptedData, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt (wrong key or tampered data): %w", err)
	}

	return string(plaintext), nil
}
