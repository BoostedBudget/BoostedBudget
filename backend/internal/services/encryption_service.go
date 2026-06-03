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
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
)

// EncryptionService is the interface for encrypting and decrypting sensitive data.
// It is defined in the crypto package to avoid circular dependencies.
// This type alias is provided for backward compatibility within the services package.
type EncryptionService = appcrypto.EncryptionService

var (
	// ErrEncryptionKeyRequired is returned when encryption key is missing
	ErrEncryptionKeyRequired = errors.New("encryption key is required")

	// ErrInvalidKeySize is returned when encryption key is not 32 bytes (AES-256)
	ErrInvalidKeySize = errors.New("encryption key must be exactly 32 bytes for AES-256")
)

// encryptionService implements crypto.EncryptionService with AES-256-GCM
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
func NewEncryptionService(cfg *config.Config) (appcrypto.EncryptionService, error) {
	if cfg.EncryptionKey == "" {
		return nil, fmt.Errorf("%w: ENCRYPTION_KEY environment variable must be set", ErrEncryptionKeyRequired)
	}

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
// Format: base64(nonce + ciphertext + tag)
func (s *encryptionService) Encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	encoded := base64.StdEncoding.EncodeToString(ciphertext)
	return encoded, nil
}

// Decrypt decrypts base64-encoded ciphertext and returns plaintext.
//
// Verifies the authentication tag to ensure data hasn't been tampered with.
func (s *encryptionService) Decrypt(ciphertext string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, encryptedData := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, encryptedData, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt (wrong key or tampered data): %w", err)
	}

	return string(plaintext), nil
}
