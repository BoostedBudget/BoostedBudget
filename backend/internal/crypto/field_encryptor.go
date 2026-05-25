package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	// encryptedPrefix is prepended to all encrypted values stored in the database.
	// This enables dual-read during migration: values without this prefix are treated
	// as plaintext and returned as-is, while values with it are decrypted.
	encryptedPrefix = "ENC$"

	// blindIndexContext is the context string used to derive the HMAC key for blind indexes.
	blindIndexContext = "blind-index"
)

// ErrInvalidKeySize is returned when the raw key is not 32 bytes.
var ErrInvalidKeySize = errors.New("rawKey must be exactly 32 bytes for AES-256")

// FieldEncryptor wraps EncryptionService with typed helpers for repository-layer encryption.
//
// It provides transparent encrypt-on-write and decrypt-on-read for domain model fields,
// plus HMAC-SHA256 blind indexes for equality lookups on encrypted fields.
//
// Encrypted values are stored as "ENC$<base64_ciphertext>" in the database.
// During migration, plaintext values (without "ENC$" prefix) are returned as-is.
type FieldEncryptor struct {
	svc     EncryptionService
	hmacKey []byte
}

// NewFieldEncryptor creates a new FieldEncryptor.
//
// rawKey must be the raw 32-byte AES key (same bytes as used for EncryptionService).
// The HMAC key for blind indexes is derived as HMAC-SHA256(rawKey, "blind-index"),
// ensuring cryptographic separation between encryption and indexing.
func NewFieldEncryptor(svc EncryptionService, rawKey []byte) (*FieldEncryptor, error) {
	if len(rawKey) != 32 {
		return nil, fmt.Errorf("%w: got %d bytes", ErrInvalidKeySize, len(rawKey))
	}
	mac := hmac.New(sha256.New, rawKey)
	mac.Write([]byte(blindIndexContext))
	return &FieldEncryptor{
		svc:     svc,
		hmacKey: mac.Sum(nil),
	}, nil
}

// EncryptString encrypts a string and returns it prefixed with "ENC$".
func (fe *FieldEncryptor) EncryptString(plaintext string) (string, error) {
	ciphertext, err := fe.svc.Encrypt(plaintext)
	if err != nil {
		return "", fmt.Errorf("encrypt string: %w", err)
	}
	return encryptedPrefix + ciphertext, nil
}

// DecryptString decrypts a string encrypted with EncryptString.
//
// If the value does not start with "ENC$", it is returned as-is (plaintext passthrough).
// This enables dual-read during the migration window when some rows are still plaintext.
func (fe *FieldEncryptor) DecryptString(value string) (string, error) {
	if !strings.HasPrefix(value, encryptedPrefix) {
		return value, nil
	}
	ciphertext := strings.TrimPrefix(value, encryptedPrefix)
	plaintext, err := fe.svc.Decrypt(ciphertext)
	if err != nil {
		return "", fmt.Errorf("decrypt string: %w", err)
	}
	return plaintext, nil
}

// EncryptNullableString encrypts a *string, preserving nil as nil (SQL NULL).
func (fe *FieldEncryptor) EncryptNullableString(s *string) (*string, error) {
	if s == nil {
		return nil, nil
	}
	encrypted, err := fe.EncryptString(*s)
	if err != nil {
		return nil, err
	}
	return &encrypted, nil
}

// DecryptNullableString decrypts a *string, preserving nil as nil (SQL NULL).
func (fe *FieldEncryptor) DecryptNullableString(s *string) (*string, error) {
	if s == nil {
		return nil, nil
	}
	decrypted, err := fe.DecryptString(*s)
	if err != nil {
		return nil, err
	}
	return &decrypted, nil
}

// EncryptJSON encrypts a JSON value (as raw bytes) and returns an encrypted string.
// Returns an error if raw is nil or empty.
func (fe *FieldEncryptor) EncryptJSON(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("cannot encrypt nil or empty JSON")
	}
	return fe.EncryptString(string(raw))
}

// DecryptJSON decrypts an encrypted JSON string back to json.RawMessage.
//
// If the value does not start with "ENC$", it is treated as plain JSON
// (plaintext passthrough for dual-read).
func (fe *FieldEncryptor) DecryptJSON(value string) (json.RawMessage, error) {
	plaintext, err := fe.DecryptString(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(plaintext), nil
}

// BlindIndex computes a deterministic HMAC-SHA256 hash of plaintext for equality lookups.
//
// The same plaintext always produces the same blind index with the same key,
// enabling WHERE blind_idx = $1 queries on encrypted columns.
// Returns a hex-encoded 64-character string.
func (fe *FieldEncryptor) BlindIndex(plaintext string) string {
	mac := hmac.New(sha256.New, fe.hmacKey)
	mac.Write([]byte(plaintext))
	return hex.EncodeToString(mac.Sum(nil))
}

// BlindIndexNullable computes a blind index for a nullable string.
// Returns nil when s is nil (preserving SQL NULL semantics).
func (fe *FieldEncryptor) BlindIndexNullable(s *string) *string {
	if s == nil {
		return nil
	}
	idx := fe.BlindIndex(*s)
	return &idx
}

// IsEncrypted returns true if the value was encrypted with EncryptString.
// Used for idempotency checks during data migration.
func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, encryptedPrefix)
}

// BlindIndexWithLabel produces a domain-separated HMAC-SHA256 blind index.
// The label prevents semantic overlap between exact-email and prefix blind indices.
// Use label "email_prefix" for all prefix table reads and writes.
func (fe *FieldEncryptor) BlindIndexWithLabel(label, plaintext string) string {
	mac := hmac.New(sha256.New, fe.hmacKey)
	mac.Write([]byte(label + ":" + plaintext))
	return hex.EncodeToString(mac.Sum(nil))
}

// NormalizeEmailForBlindIndex matches the email normalisation used throughout
// profile_repository.go and backfill.go (lower + trim, no NFC).
// Must be identical everywhere: creation, update, backfill, and admin search.
func NormalizeEmailForBlindIndex(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

const (
	minPrefixLen = 3
	maxPrefixLen = 64
)

// EmailPrefixBlindIndices generates domain-separated blind indices for all
// rune-length prefixes in [minPrefixLen, maxPrefixLen] of the given email.
// Rune-based slicing prevents splitting multi-byte UTF-8 characters.
// Returns nil when the normalised email is shorter than minPrefixLen.
func EmailPrefixBlindIndices(enc *FieldEncryptor, email string) []string {
	normalised := NormalizeEmailForBlindIndex(email)
	runes := []rune(normalised)
	end := len(runes)
	if end > maxPrefixLen {
		end = maxPrefixLen
	}
	if end < minPrefixLen {
		return nil
	}
	indices := make([]string, 0, end-minPrefixLen+1)
	for i := minPrefixLen; i <= end; i++ {
		indices = append(indices, enc.BlindIndexWithLabel("email_prefix", string(runes[:i])))
	}
	return indices
}
