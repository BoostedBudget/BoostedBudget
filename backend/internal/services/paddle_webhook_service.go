// Package services provides business logic and external service integrations.
// This file implements PaddleWebhookService for secure webhook signature verification.
package services

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
)

var (
	// ErrWebhookSecretNotConfigured is returned when PADDLE_WEBHOOK_SECRET is not set.
	ErrWebhookSecretNotConfigured = errors.New("paddle webhook secret not configured")

	// ErrInvalidSignatureFormat is returned when the Paddle-Signature header is malformed.
	ErrInvalidSignatureFormat = errors.New("invalid signature format")

	// ErrSignatureVerificationFailed is returned when HMAC verification fails.
	ErrSignatureVerificationFailed = errors.New("signature verification failed")
)

// PaddleWebhookService provides methods for verifying HMAC-SHA256 webhook signatures.
// Signature format: "ts=<timestamp>;h1=<hmac_signature>"
// HMAC is calculated over: "<timestamp>:<request_body>"
type PaddleWebhookService interface {
	// VerifyWebhook verifies the HMAC-SHA256 signature of a Paddle webhook.
	// Returns true if signature is valid, error if format is invalid.
	// Uses constant-time comparison to prevent timing attacks.
	VerifyWebhook(signature, body string) (bool, error)
}

// paddleWebhookService implements PaddleWebhookService
type paddleWebhookService struct {
	webhookSecret string
	logger        *slog.Logger
}

// NewPaddleWebhookService creates a new PaddleWebhookService with validated configuration.
//
// Validates that the webhook secret is present. Returns an error if:
//   - PaddleWebhookSecret is empty (ErrWebhookSecretNotConfigured)
//
// On success, returns a fully initialized service ready for signature verification.
func NewPaddleWebhookService(cfg *config.Config, logger *slog.Logger) (PaddleWebhookService, error) {
	// Validate required webhook secret
	if cfg.PaddleWebhookSecret == "" {
		return nil, fmt.Errorf("%w: PADDLE_WEBHOOK_SECRET is required", ErrWebhookSecretNotConfigured)
	}

	logger.Info("Paddle webhook service initialized")

	return &paddleWebhookService{
		webhookSecret: cfg.PaddleWebhookSecret,
		logger:        logger,
	}, nil
}

// VerifyWebhook verifies the HMAC-SHA256 signature of a Paddle webhook.
//
// Paddle signature format: "ts=1614280549;h1=abc123..."
// HMAC payload format: "1614280549:{"event_type":"...","data":{...}}"
//
// Steps:
// 1. Parse signature header to extract timestamp (ts) and hash (h1)
// 2. Construct payload: "<timestamp>:<request_body>"
// 3. Calculate HMAC-SHA256 using webhook secret
// 4. Compare calculated hash with provided hash (constant-time comparison)
func (s *paddleWebhookService) VerifyWebhook(signature, body string) (bool, error) {
	// Parse signature header
	timestamp, hash, err := parseSignature(signature)
	if err != nil {
		s.logger.Warn("Invalid Paddle signature format",
			"error", err,
		)
		return false, fmt.Errorf("%w: %v", ErrInvalidSignatureFormat, err)
	}

	// Construct signed payload: "timestamp:body"
	signedPayload := fmt.Sprintf("%s:%s", timestamp, body)

	// Calculate HMAC-SHA256
	h := hmac.New(sha256.New, []byte(s.webhookSecret))
	h.Write([]byte(signedPayload))
	calculatedHash := hex.EncodeToString(h.Sum(nil))

	// Constant-time comparison to prevent timing attacks
	valid := hmac.Equal([]byte(calculatedHash), []byte(hash))

	if !valid {
		s.logger.Warn("Paddle webhook signature verification failed",
			"expected_hash", calculatedHash,
			"provided_hash", hash,
		)
		return false, ErrSignatureVerificationFailed
	}

	s.logger.Debug("Paddle webhook signature verified successfully",
		"timestamp", timestamp,
	)

	return true, nil
}

// parseSignature extracts timestamp and hash from Paddle-Signature header.
//
// Expected format: "ts=1614280549;h1=abc123..."
// Returns timestamp and hash values, or error if format is invalid.
func parseSignature(signature string) (timestamp, hash string, err error) {
	// Split signature by semicolon
	parts := strings.Split(signature, ";")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("expected 2 parts separated by ';', got %d", len(parts))
	}

	// Parse timestamp
	tsParts := strings.Split(parts[0], "=")
	if len(tsParts) != 2 || tsParts[0] != "ts" {
		return "", "", fmt.Errorf("invalid timestamp format: %s", parts[0])
	}
	timestamp = tsParts[1]

	// Parse hash
	hashParts := strings.Split(parts[1], "=")
	if len(hashParts) != 2 || hashParts[0] != "h1" {
		return "", "", fmt.Errorf("invalid hash format: %s", parts[1])
	}
	hash = hashParts[1]

	if timestamp == "" || hash == "" {
		return "", "", fmt.Errorf("timestamp or hash is empty")
	}

	return timestamp, hash, nil
}
