package services

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/prometheus/client_golang/prometheus/testutil"
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

// currentTestTimestamp returns "now" as a Unix-seconds string, so signature tests that don't
// care about the replay window stay inside the default tolerance regardless of when the test
// suite runs.
func currentTestTimestamp() string {
	return fmt.Sprintf("%d", time.Now().Unix())
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

// TestNewPaddleWebhookService_ZeroToleranceFallsBackToDefault verifies that a config value
// constructed without going through config.Load() (and therefore leaving
// PaddleWebhookTimestampToleranceSeconds at its zero value) doesn't silently disable the replay
// window — it must fall back to the safe default tolerance instead.
func TestNewPaddleWebhookService_ZeroToleranceFallsBackToDefault(t *testing.T) {
	cfg := &config.Config{
		PaddleWebhookSecret:                    "test_secret",
		PaddleWebhookTimestampToleranceSeconds: 0,
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	service, err := NewPaddleWebhookService(cfg, logger)
	require.NoError(t, err)

	svc, ok := service.(*paddleWebhookService)
	require.True(t, ok)
	assert.Equal(t, time.Duration(defaultPaddleWebhookTimestampToleranceSeconds)*time.Second, svc.tolerance)
}

// TestPaddleVerifyWebhook_ValidSignature tests signature verification with valid HMAC
func TestPaddleVerifyWebhook_ValidSignature(t *testing.T) {
	service := setupWebhookService(t)
	timestamp := currentTestTimestamp()
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
	timestamp := currentTestTimestamp()
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
			name:      "Non-h1 third component",
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
	timestamp := currentTestTimestamp()
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
	timestamp := currentTestTimestamp()
	body := ""

	signature := calculateTestSignature("test_webhook_secret_key_1234567890", timestamp, body)

	valid, err := service.VerifyWebhook(signature, body)

	require.NoError(t, err)
	assert.True(t, valid, "Empty body should be valid if signature matches")
}

// TestVerifyWebhook_LargePayload tests verification with large JSON payload
func TestVerifyWebhook_LargePayload(t *testing.T) {
	service := setupWebhookService(t)
	timestamp := currentTestTimestamp()

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
	timestamp := currentTestTimestamp()
	body := `{"event_id":"evt_01h1234","data":{"name":"Test & Co.","description":"Special chars: <>&\"'"}}`

	signature := calculateTestSignature("test_webhook_secret_key_1234567890", timestamp, body)

	valid, err := service.VerifyWebhook(signature, body)

	require.NoError(t, err)
	assert.True(t, valid)
}

// TestParseSignature tests the signature parsing logic
func TestParseSignature(t *testing.T) {
	testCases := []struct {
		name        string
		signature   string
		expectError bool
		expectTS    string
		expectHash  []string
	}{
		{
			name:        "Valid signature",
			signature:   "ts=1614280549;h1=abc123def456",
			expectError: false,
			expectTS:    "1614280549",
			expectHash:  []string{"abc123def456"},
		},
		{
			name:        "Valid signature with rotated h1 values",
			signature:   "ts=1614280549;h1=abc123;h1=def456",
			expectError: false,
			expectTS:    "1614280549",
			expectHash:  []string{"abc123", "def456"},
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
		{
			name:        "Invalid format - non-h1 second component",
			signature:   "ts=1614280549;h2=abc123",
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			timestamp, hashes, err := parseSignature(tc.signature)

			if tc.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectTS, timestamp)
				assert.Equal(t, tc.expectHash, hashes)
			}
		})
	}
}

// TestVerifyWebhook_ConstantTimeComparison tests that signature comparison is timing-safe
func TestVerifyWebhook_ConstantTimeComparison(t *testing.T) {
	service := setupWebhookService(t)
	timestamp := currentTestTimestamp()
	body := `{"event_id":"evt_01h1234"}`

	// Calculate correct hash
	correctSignature := calculateTestSignature("test_webhook_secret_key_1234567890", timestamp, body)

	// Extract hash from correct signature
	_, correctHashes, err := parseSignature(correctSignature)
	require.NoError(t, err)
	correctHash := correctHashes[0]

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

// --- F2: HMAC forgery oracle in logs -----------------------------------------------------

// TestVerifyWebhook_FailedVerification_DoesNotLogHash is the exploit-first regression test for
// F2: a failed signature verification must not emit the correct HMAC (or any derivative of the
// provided hash) into the log output. Before the fix, the log line included
// "expected_hash"/"provided_hash" with the correct HMAC for the attacker-supplied body — a log
// reader could lift that value and use it to forge a future signature for a body of their
// choosing.
func TestVerifyWebhook_FailedVerification_DoesNotLogHash(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	svc := &paddleWebhookService{
		webhookSecret: "test_webhook_secret_key_1234567890",
		logger:        logger,
		tolerance:     5 * time.Second,
		now:           time.Now,
	}

	timestamp := currentTestTimestamp()
	body := `{"event_id":"evt_01h1234","event_type":"subscription.created"}`

	// Compute the real HMAC so we know exactly what value must NOT appear in the logs.
	realSignature := calculateTestSignature(svc.webhookSecret, timestamp, body)
	_, realHashes, err := parseSignature(realSignature)
	require.NoError(t, err)
	realHash := realHashes[0]

	// Provide a WRONG hash so verification fails and the warning path fires.
	wrongSignature := fmt.Sprintf("ts=%s;h1=%s", timestamp, "0000000000000000000000000000000000000000000000000000000000000000")

	valid, err := svc.VerifyWebhook(wrongSignature, body)
	require.Error(t, err)
	assert.False(t, valid)
	assert.ErrorIs(t, err, ErrSignatureVerificationFailed)

	logOutput := logBuf.String()
	require.NotEmpty(t, logOutput, "expected a warning to be logged for the failed verification")
	assert.NotContains(t, logOutput, realHash, "log output must never contain the correct HMAC for an attacker-supplied body")
	assert.NotContains(t, logOutput, "expected_hash")
	assert.NotContains(t, logOutput, "provided_hash")
}

// --- F3: replay window ---------------------------------------------------------------------

// fixedClock returns a clock func pinned to t, for deterministic tolerance-window tests.
func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func newServiceWithClock(t *testing.T, tolerance time.Duration, clock func() time.Time) *paddleWebhookService {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	return &paddleWebhookService{
		webhookSecret: "test_webhook_secret_key_1234567890",
		logger:        logger,
		tolerance:     tolerance,
		now:           clock,
	}
}

// TestVerifyWebhook_StaleTimestamp_Rejected is the exploit-first regression test for F3: a
// signature with a correct HMAC but a `ts=` far in the past (outside the tolerance window) must
// be rejected as a replay risk, even though the HMAC itself is cryptographically valid.
func TestVerifyWebhook_StaleTimestamp_Rejected(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceWithClock(t, 5*time.Second, fixedClock(now))

	staleTS := fmt.Sprintf("%d", now.Add(-1*time.Hour).Unix())
	body := `{"event_id":"evt_stale"}`
	signature := calculateTestSignature(svc.webhookSecret, staleTS, body)

	valid, err := svc.VerifyWebhook(signature, body)

	require.Error(t, err)
	assert.False(t, valid)
	assert.ErrorIs(t, err, ErrTimestampOutOfTolerance)
}

// TestVerifyWebhook_FutureTimestamp_Rejected is the exploit-first regression test for F3's
// "unreasonably future" requirement: a correct-HMAC signature with a `ts=` far in the future
// must also be rejected. The old Plaid-style check (`time.Since(issuedAt) > tolerance`) would
// have accepted this, since time.Since on a future timestamp is negative.
func TestVerifyWebhook_FutureTimestamp_Rejected(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceWithClock(t, 5*time.Second, fixedClock(now))

	futureTS := fmt.Sprintf("%d", now.Add(1*time.Hour).Unix())
	body := `{"event_id":"evt_future"}`
	signature := calculateTestSignature(svc.webhookSecret, futureTS, body)

	valid, err := svc.VerifyWebhook(signature, body)

	require.Error(t, err)
	assert.False(t, valid)
	assert.ErrorIs(t, err, ErrTimestampOutOfTolerance)
}

// TestVerifyWebhook_TimestampTolerance_Boundaries checks the tolerance window's edges: exactly
// at the tolerance in the past or future must be ACCEPTED (inclusive boundary), while one second
// further out in either direction must be REJECTED.
func TestVerifyWebhook_TimestampTolerance_Boundaries(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	tolerance := 5 * time.Second
	body := `{"event_id":"evt_boundary"}`

	testCases := []struct {
		name    string
		offset  time.Duration
		wantErr bool
	}{
		{"exactly at past boundary", -tolerance, false},
		{"exactly at future boundary", tolerance, false},
		{"one second past the past boundary", -(tolerance + time.Second), true},
		{"one second past the future boundary", tolerance + time.Second, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newServiceWithClock(t, tolerance, fixedClock(now))
			ts := fmt.Sprintf("%d", now.Add(tc.offset).Unix())
			signature := calculateTestSignature(svc.webhookSecret, ts, body)

			valid, err := svc.VerifyWebhook(signature, body)

			if tc.wantErr {
				require.Error(t, err)
				assert.False(t, valid)
				assert.ErrorIs(t, err, ErrTimestampOutOfTolerance)
			} else {
				require.NoError(t, err)
				assert.True(t, valid)
			}
		})
	}
}

// TestVerifyWebhook_MalformedTimestamp_RejectedWithoutPanic covers non-numeric, negative, and
// integer-overflow ts= values — all must be rejected as ErrInvalidSignatureFormat, and none must
// panic.
func TestVerifyWebhook_MalformedTimestamp_RejectedWithoutPanic(t *testing.T) {
	svc := newServiceWithClock(t, 5*time.Second, time.Now)
	body := `{"event_id":"evt_malformed_ts"}`

	testCases := []struct {
		name string
		ts   string
	}{
		{"non-numeric", "not-a-number"},
		{"negative", "-100"},
		{"integer overflow", "99999999999999999999999999999999"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			signature := calculateTestSignature(svc.webhookSecret, tc.ts, body)

			assert.NotPanics(t, func() {
				valid, err := svc.VerifyWebhook(signature, body)
				require.Error(t, err)
				assert.False(t, valid)
				assert.ErrorIs(t, err, ErrInvalidSignatureFormat)
			})
		})
	}
}

// --- F3: multi-h1 rotation support -----------------------------------------------------------

// TestVerifyWebhook_RotationHeader_AnyMatchingH1_Verifies is the exploit-first regression test
// for the parser trap: a signature header carrying multiple h1 values (as Paddle emits during
// secret rotation) must verify successfully as long as ANY one of them matches the calculated
// HMAC. Before the fix, parseSignature required exactly 2 ';'-separated components and rejected
// this header outright regardless of whether a valid h1 was present.
func TestVerifyWebhook_RotationHeader_AnyMatchingH1_Verifies(t *testing.T) {
	svc := newServiceWithClock(t, 5*time.Second, time.Now)
	timestamp := currentTestTimestamp()
	body := `{"event_id":"evt_rotation"}`

	correctSignature := calculateTestSignature(svc.webhookSecret, timestamp, body)
	_, correctHashes, err := parseSignature(correctSignature)
	require.NoError(t, err)
	correctHash := correctHashes[0]

	// Old (stale) secret's hash won't match — put it first so the correct one must be found
	// among multiple candidates, not just checked as parts[1].
	staleHash := strings.Repeat("0", len(correctHash))
	rotationSignature := fmt.Sprintf("ts=%s;h1=%s;h1=%s", timestamp, staleHash, correctHash)

	valid, err := svc.VerifyWebhook(rotationSignature, body)

	require.NoError(t, err)
	assert.True(t, valid)
}

// TestVerifyWebhook_RotationHeader_NoMatchingH1_Rejected is the other half of the rotation trap:
// a header with multiple h1 values, none of which match, must still be rejected.
func TestVerifyWebhook_RotationHeader_NoMatchingH1_Rejected(t *testing.T) {
	svc := newServiceWithClock(t, 5*time.Second, time.Now)
	timestamp := currentTestTimestamp()
	body := `{"event_id":"evt_rotation_none"}`

	correctSignature := calculateTestSignature(svc.webhookSecret, timestamp, body)
	_, correctHashes, err := parseSignature(correctSignature)
	require.NoError(t, err)
	hashLen := len(correctHashes[0])

	wrong1 := strings.Repeat("1", hashLen)
	wrong2 := strings.Repeat("2", hashLen)
	rotationSignature := fmt.Sprintf("ts=%s;h1=%s;h1=%s", timestamp, wrong1, wrong2)

	valid, err := svc.VerifyWebhook(rotationSignature, body)

	require.Error(t, err)
	assert.False(t, valid)
	assert.ErrorIs(t, err, ErrSignatureVerificationFailed)
}

// --- Fix-round-1 item 2: PaddleWebhookRejectionsTotal distinguishes rejection reasons ---------

// TestVerifyWebhook_RejectionMetric_InvalidFormat is the exploit-first regression test for a
// malformed-header rejection incrementing observability.PaddleWebhookRejectionsTotal with
// reason="invalid_format", and NOT incrementing the other two reason labels.
func TestVerifyWebhook_RejectionMetric_InvalidFormat(t *testing.T) {
	svc := newServiceWithClock(t, 5*time.Second, time.Now)

	before := testutil.ToFloat64(observability.PaddleWebhookRejectionsTotal.WithLabelValues("invalid_format"))
	beforeTolerance := testutil.ToFloat64(observability.PaddleWebhookRejectionsTotal.WithLabelValues("timestamp_out_of_tolerance"))
	beforeMismatch := testutil.ToFloat64(observability.PaddleWebhookRejectionsTotal.WithLabelValues("signature_mismatch"))

	valid, err := svc.VerifyWebhook("not-a-valid-signature-header", `{"event_id":"evt_metric_format"}`)
	require.Error(t, err)
	assert.False(t, valid)
	assert.ErrorIs(t, err, ErrInvalidSignatureFormat)

	assert.Equal(t, before+1, testutil.ToFloat64(observability.PaddleWebhookRejectionsTotal.WithLabelValues("invalid_format")))
	assert.Equal(t, beforeTolerance, testutil.ToFloat64(observability.PaddleWebhookRejectionsTotal.WithLabelValues("timestamp_out_of_tolerance")))
	assert.Equal(t, beforeMismatch, testutil.ToFloat64(observability.PaddleWebhookRejectionsTotal.WithLabelValues("signature_mismatch")))
}

// TestVerifyWebhook_RejectionMetric_TimestampOutOfTolerance is the exploit-first regression test
// for a stale-but-cryptographically-correct signature incrementing
// PaddleWebhookRejectionsTotal{reason="timestamp_out_of_tolerance"} — this is the clock-skew
// incident signal (fix-round-1 item 2): a real Paddle delivery being dropped for clock reasons
// must be distinguishable from a forged signature in metrics, not just in logs.
func TestVerifyWebhook_RejectionMetric_TimestampOutOfTolerance(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceWithClock(t, 5*time.Second, fixedClock(now))

	before := testutil.ToFloat64(observability.PaddleWebhookRejectionsTotal.WithLabelValues("timestamp_out_of_tolerance"))
	beforeFormat := testutil.ToFloat64(observability.PaddleWebhookRejectionsTotal.WithLabelValues("invalid_format"))
	beforeMismatch := testutil.ToFloat64(observability.PaddleWebhookRejectionsTotal.WithLabelValues("signature_mismatch"))

	body := `{"event_id":"evt_metric_tolerance"}`
	staleTS := fmt.Sprintf("%d", now.Add(-1*time.Hour).Unix())
	signature := calculateTestSignature(svc.webhookSecret, staleTS, body)

	valid, err := svc.VerifyWebhook(signature, body)
	require.Error(t, err)
	assert.False(t, valid)
	assert.ErrorIs(t, err, ErrTimestampOutOfTolerance)

	assert.Equal(t, before+1, testutil.ToFloat64(observability.PaddleWebhookRejectionsTotal.WithLabelValues("timestamp_out_of_tolerance")))
	assert.Equal(t, beforeFormat, testutil.ToFloat64(observability.PaddleWebhookRejectionsTotal.WithLabelValues("invalid_format")))
	assert.Equal(t, beforeMismatch, testutil.ToFloat64(observability.PaddleWebhookRejectionsTotal.WithLabelValues("signature_mismatch")))
}

// TestVerifyWebhook_RejectionMetric_SignatureMismatch is the exploit-first regression test for a
// well-formed, in-window signature with a wrong HMAC incrementing
// PaddleWebhookRejectionsTotal{reason="signature_mismatch"}.
func TestVerifyWebhook_RejectionMetric_SignatureMismatch(t *testing.T) {
	svc := newServiceWithClock(t, 5*time.Second, time.Now)

	before := testutil.ToFloat64(observability.PaddleWebhookRejectionsTotal.WithLabelValues("signature_mismatch"))
	beforeFormat := testutil.ToFloat64(observability.PaddleWebhookRejectionsTotal.WithLabelValues("invalid_format"))
	beforeTolerance := testutil.ToFloat64(observability.PaddleWebhookRejectionsTotal.WithLabelValues("timestamp_out_of_tolerance"))

	timestamp := currentTestTimestamp()
	body := `{"event_id":"evt_metric_mismatch"}`
	signature := fmt.Sprintf("ts=%s;h1=%s", timestamp, "0000000000000000000000000000000000000000000000000000000000000000")

	valid, err := svc.VerifyWebhook(signature, body)
	require.Error(t, err)
	assert.False(t, valid)
	assert.ErrorIs(t, err, ErrSignatureVerificationFailed)

	assert.Equal(t, before+1, testutil.ToFloat64(observability.PaddleWebhookRejectionsTotal.WithLabelValues("signature_mismatch")))
	assert.Equal(t, beforeFormat, testutil.ToFloat64(observability.PaddleWebhookRejectionsTotal.WithLabelValues("invalid_format")))
	assert.Equal(t, beforeTolerance, testutil.ToFloat64(observability.PaddleWebhookRejectionsTotal.WithLabelValues("timestamp_out_of_tolerance")))
}
