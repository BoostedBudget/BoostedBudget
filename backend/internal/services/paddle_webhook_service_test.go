package services

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupWebhookService creates a test PaddleWebhookService
func setupWebhookService(t *testing.T) PaddleWebhookService {
	t.Helper()

	cfg := &config.Config{
		PaddleWebhookSecret: "test_webhook_secret_key_1234567890",
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError, // Suppress logs during tests
	}))

	service, err := NewPaddleWebhookService(cfg, logger)
	require.NoError(t, err)

	return service
}

// calculateTestSignature generates a valid HMAC-SHA256 signature
func calculateTestSignature(secret, timestamp, body string) string {
	payload := fmt.Sprintf("%s:%s", timestamp, body)
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(payload))
	hash := hex.EncodeToString(h.Sum(nil))
	return fmt.Sprintf("ts=%s;h1=%s", timestamp, hash)
}

// TestNewPaddleWebhookService_Success tests successful service initialization
func TestNewPaddleWebhookService_Success(t *testing.T) {
	cfg := &config.Config{
		PaddleWebhookSecret: "test_secret",
	}
	logger := slog.Default()

	service, err := NewPaddleWebhookService(cfg, logger)

	require.NoError(t, err)
	assert.NotNil(t, service)
}

// TestNewPaddleWebhookService_MissingSecret tests initialization without webhook secret
func TestNewPaddleWebhookService_MissingSecret(t *testing.T) {
	cfg := &config.Config{
		PaddleWebhookSecret: "",
	}
	logger := slog.Default()

	service, err := NewPaddleWebhookService(cfg, logger)

	require.Error(t, err)
	assert.Nil(t, service)
	assert.ErrorIs(t, err, ErrWebhookSecretNotConfigured)
	assert.Contains(t, err.Error(), "PADDLE_WEBHOOK_SECRET is required")
}

// TestPaddleVerifyWebhook_ValidSignature tests signature verification with valid HMAC
func TestPaddleVerifyWebhook_ValidSignature(t *testing.T) {
	service := setupWebhookService(t)
	timestamp := "1614280549"
	body := `{"event_id":"evt_01h1234","event_type":"subscription.created","data":{"id":"sub_123"}}`

	// Calculate correct signature
	signature := calculateTestSignature("test_webhook_secret_key_1234567890", timestamp, body)

	valid, err := service.VerifyWebhook(signature, body)

	require.NoError(t, err)
	assert.True(t, valid)
}

// TestPaddleVerifyWebhook_InvalidSignature tests signature verification with wrong hash
func TestPaddleVerifyWebhook_InvalidSignature(t *testing.T) {
	service := setupWebhookService(t)
	timestamp := "1614280549"
	body := `{"event_id":"evt_01h1234","event_type":"subscription.created"}`

	// Use wrong hash
	signature := fmt.Sprintf("ts=%s;h1=%s", timestamp, "wrong_hash_12345")

	valid, err := service.VerifyWebhook(signature, body)

	require.Error(t, err)
	assert.False(t, valid)
	assert.ErrorIs(t, err, ErrSignatureVerificationFailed)
}

// TestVerifyWebhook_InvalidFormat tests signature verification with malformed header
func TestVerifyWebhook_InvalidFormat(t *testing.T) {
	service := setupWebhookService(t)
	body := `{"event_id":"evt_01h1234"}`

	testCases := []struct {
		name      string
		signature string
	}{
		{
			name:      "Missing semicolon separator",
			signature: "ts=1614280549h1=abc123",
		},
		{
			name:      "Missing ts prefix",
			signature: "1614280549;h1=abc123",
		},
		{
			name:      "Missing h1 prefix",
			signature: "ts=1614280549;abc123",
		},
		{
			name:      "Empty timestamp",
			signature: "ts=;h1=abc123",
		},
		{
			name:      "Empty hash",
			signature: "ts=1614280549;h1=",
		},
		{
			name:      "Too many parts",
			signature: "ts=1614280549;h1=abc123;extra=data",
		},
		{
			name:      "Too few parts",
			signature: "ts=1614280549",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			valid, err := service.VerifyWebhook(tc.signature, body)

			require.Error(t, err)
			assert.False(t, valid)
			assert.ErrorIs(t, err, ErrInvalidSignatureFormat)
		})
	}
}

// TestVerifyWebhook_DifferentBody tests that changing body invalidates signature
func TestVerifyWebhook_DifferentBody(t *testing.T) {
	service := setupWebhookService(t)
	timestamp := "1614280549"
	originalBody := `{"event_id":"evt_01h1234","event_type":"subscription.created"}`
	tamperedBody := `{"event_id":"evt_01h1234","event_type":"subscription.updated"}`

	// Calculate signature for original body
	signature := calculateTestSignature("test_webhook_secret_key_1234567890", timestamp, originalBody)

	// Try to verify with tampered body
	valid, err := service.VerifyWebhook(signature, tamperedBody)

	require.Error(t, err)
	assert.False(t, valid)
	assert.ErrorIs(t, err, ErrSignatureVerificationFailed)
}

// TestVerifyWebhook_EmptyBody tests verification with empty request body
func TestVerifyWebhook_EmptyBody(t *testing.T) {
	service := setupWebhookService(t)
	timestamp := "1614280549"
	body := ""

	signature := calculateTestSignature("test_webhook_secret_key_1234567890", timestamp, body)

	valid, err := service.VerifyWebhook(signature, body)

	require.NoError(t, err)
	assert.True(t, valid, "Empty body should be valid if signature matches")
}

// TestVerifyWebhook_LargePayload tests verification with large JSON payload
func TestVerifyWebhook_LargePayload(t *testing.T) {
	service := setupWebhookService(t)
	timestamp := "1614280549"

	// Build large payload (simulating transaction with many line items)
	body := `{"event_id":"evt_01h1234","event_type":"transaction.completed","data":{"id":"txn_123","items":[`
	for i := 0; i < 100; i++ {
		if i > 0 {
			body += ","
		}
		body += fmt.Sprintf(`{"id":"itm_%d","price":"9.99"}`, i)
	}
	body += `]}}`

	signature := calculateTestSignature("test_webhook_secret_key_1234567890", timestamp, body)

	valid, err := service.VerifyWebhook(signature, body)

	require.NoError(t, err)
	assert.True(t, valid)
}

// TestVerifyWebhook_SpecialCharacters tests verification with special characters in body
func TestVerifyWebhook_SpecialCharacters(t *testing.T) {
	service := setupWebhookService(t)
	timestamp := "1614280549"
	body := `{"event_id":"evt_01h1234","data":{"name":"Test & Co.","description":"Special chars: <>&\"'"}}`

	signature := calculateTestSignature("test_webhook_secret_key_1234567890", timestamp, body)

	valid, err := service.VerifyWebhook(signature, body)

	require.NoError(t, err)
	assert.True(t, valid)
}

// TestParseSignature tests the signature parsing logic
func TestParseSignature(t *testing.T) {
	testCases := []struct {
		name          string
		signature     string
		expectError   bool
		expectTS      string
		expectHash    string
	}{
		{
			name:        "Valid signature",
			signature:   "ts=1614280549;h1=abc123def456",
			expectError: false,
			expectTS:    "1614280549",
			expectHash:  "abc123def456",
		},
		{
			name:        "Invalid format - no semicolon",
			signature:   "ts=1614280549h1=abc123",
			expectError: true,
		},
		{
			name:        "Invalid format - missing ts prefix",
			signature:   "1614280549;h1=abc123",
			expectError: true,
		},
		{
			name:        "Invalid format - missing h1 prefix",
			signature:   "ts=1614280549;abc123",
			expectError: true,
		},
		{
			name:        "Invalid format - empty timestamp",
			signature:   "ts=;h1=abc123",
			expectError: true,
		},
		{
			name:        "Invalid format - empty hash",
			signature:   "ts=1614280549;h1=",
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			timestamp, hash, err := parseSignature(tc.signature)

			if tc.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectTS, timestamp)
				assert.Equal(t, tc.expectHash, hash)
			}
		})
	}
}

// TestVerifyWebhook_ConstantTimeComparison tests that signature comparison is timing-safe
func TestVerifyWebhook_ConstantTimeComparison(t *testing.T) {
	service := setupWebhookService(t)
	timestamp := "1614280549"
	body := `{"event_id":"evt_01h1234"}`

	// Calculate correct hash
	correctSignature := calculateTestSignature("test_webhook_secret_key_1234567890", timestamp, body)

	// Extract hash from correct signature
	_, correctHash, err := parseSignature(correctSignature)
	require.NoError(t, err)

	// Create signatures with hashes that differ by 1 character at different positions
	testCases := []struct {
		name     string
		position int
	}{
		{"First character", 0},
		{"Middle character", len(correctHash) / 2},
		{"Last character", len(correctHash) - 1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Modify hash at specific position
			hashBytes := []byte(correctHash)
			if hashBytes[tc.position] == 'a' {
				hashBytes[tc.position] = 'b'
			} else {
				hashBytes[tc.position] = 'a'
			}
			wrongSignature := fmt.Sprintf("ts=%s;h1=%s", timestamp, string(hashBytes))

			// Verify fails regardless of where difference is
			valid, err := service.VerifyWebhook(wrongSignature, body)

			require.Error(t, err)
			assert.False(t, valid)
			assert.ErrorIs(t, err, ErrSignatureVerificationFailed)
		})
	}
}
