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
	"strconv"
	"strings"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
)

var (
	// ErrWebhookSecretNotConfigured is returned when PADDLE_WEBHOOK_SECRET is not set.
	ErrWebhookSecretNotConfigured = errors.New("paddle webhook secret not configured")

	// ErrInvalidSignatureFormat is returned when the Paddle-Signature header is malformed —
	// this covers both structural problems (missing ts/h1 components) and a ts value that
	// cannot be parsed as a valid, in-range Unix timestamp (non-numeric, negative, overflow).
	ErrInvalidSignatureFormat = errors.New("invalid signature format")

	// ErrSignatureVerificationFailed is returned when HMAC verification fails (no h1 value
	// in the header matches the HMAC we calculate for the request).
	ErrSignatureVerificationFailed = errors.New("signature verification failed")

	// ErrTimestampOutOfTolerance is returned when the signature's `ts=` value is
	// well-formed and in range, but falls outside the configured replay/clock-skew tolerance
	// — either too old (a captured/replayed signature) or unreasonably far in the future
	// (issue #1514, F3).
	ErrTimestampOutOfTolerance = errors.New("webhook timestamp outside tolerance window")
)

// Reason labels for observability.PaddleWebhookRejectionsTotal — see that metric's doc comment
// for what each one means operationally.
const (
	paddleWebhookRejectionReasonInvalidFormat           = "invalid_format"
	paddleWebhookRejectionReasonTimestampOutOfTolerance = "timestamp_out_of_tolerance"
	paddleWebhookRejectionReasonSignatureMismatch       = "signature_mismatch"
)

// defaultPaddleWebhookTimestampToleranceSeconds is used when the caller-supplied config value
// is zero or negative (unset, or a literal-constructed config.Config that never went through
// config.Load's validation). Mirrors Paddle's documented SDK default of 5 seconds — see the
// comment on config.Config.PaddleWebhookTimestampToleranceSeconds for the caveat that this is
// the vendor's stated behavior, not something independently verified against live traffic.
const defaultPaddleWebhookTimestampToleranceSeconds = 5

// PaddleWebhookService provides methods for verifying HMAC-SHA256 webhook signatures.
// Signature format: "ts=<timestamp>;h1=<hmac_signature>" — Paddle rotates webhook secrets by
// emitting more than one h1 component during the rotation window (e.g.
// "ts=<timestamp>;h1=<old>;h1=<new>"); VerifyWebhook accepts the signature if ANY h1 value
// matches. NOTE: the exact wire format Paddle uses during rotation is not verified against a
// vendored SDK or captured traffic in this codebase — parseSignature is written defensively to
// accept one-or-more semicolon-separated `h1=` components after `ts=` and to reject anything it
// cannot parse, rather than asserting a specific rotation format as fact.
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
	// tolerance bounds how far a signature's ts= may drift from now() in either direction
	// before it is rejected. Always positive — see NewPaddleWebhookService.
	tolerance time.Duration
	// now is an injectable clock seam so the tolerance window is testable without sleeping.
	// Defaults to time.Now; tests in this package (same package, not _test) may construct
	// paddleWebhookService directly and override this field.
	now func() time.Time
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

	// Defense-in-depth: config.Load() already bounds this to
	// (0, config.PaddleWebhookTimestampToleranceMaxSeconds] and logs a warning on an
	// out-of-range value, but a literal-constructed config.Config (tests, or any future caller
	// that skips Load()) bypasses that validation entirely — clamp here too so an unbounded
	// value can never silently disable the replay window (issue #1514 fix-round-1 item 1).
	toleranceSeconds := cfg.PaddleWebhookTimestampToleranceSeconds
	if toleranceSeconds <= 0 || toleranceSeconds > config.PaddleWebhookTimestampToleranceMaxSeconds {
		toleranceSeconds = defaultPaddleWebhookTimestampToleranceSeconds
	}

	logger.Info("Paddle webhook service initialized",
		"timestamp_tolerance_seconds", toleranceSeconds,
	)

	return &paddleWebhookService{
		webhookSecret: cfg.PaddleWebhookSecret,
		logger:        logger,
		tolerance:     time.Duration(toleranceSeconds) * time.Second,
		now:           time.Now,
	}, nil
}

// VerifyWebhook verifies the HMAC-SHA256 signature of a Paddle webhook.
//
// Paddle signature format: "ts=1614280549;h1=abc123..." (or with multiple h1 components during
// secret rotation — see parseSignature).
// HMAC payload format: "1614280549:{"event_type":"...","data":{...}}"
//
// Steps:
//  1. Parse signature header to extract timestamp (ts) and one-or-more candidate hashes (h1)
//  2. Parse and range-check the timestamp, then reject it if outside the tolerance window
//     (issue #1514, F3) — this rejects both replayed-old and clock-skewed-future signatures
//  3. Construct payload: "<timestamp>:<request_body>"
//  4. Calculate HMAC-SHA256 using webhook secret
//  5. Compare calculated hash against every candidate hash (constant-time comparison); valid
//     if any one matches
//
// Issue #1514, F2: on failure this logs a warning with NO derivative of either the calculated or
// provided hash (no value, prefix, or length) — logging the correct HMAC for an attacker-chosen
// body turns any log reader (Sentry, log aggregation, a leaked archive) into a webhook signer.
func (s *paddleWebhookService) VerifyWebhook(signature, body string) (bool, error) {
	// Parse signature header
	timestamp, hashes, err := parseSignature(signature)
	if err != nil {
		observability.PaddleWebhookRejectionsTotal.WithLabelValues(paddleWebhookRejectionReasonInvalidFormat).Inc()
		s.logger.Warn("Invalid Paddle signature format",
			"error", err,
		)
		return false, fmt.Errorf("%w: %v", ErrInvalidSignatureFormat, err)
	}

	tsUnix, err := parseTimestamp(timestamp)
	if err != nil {
		observability.PaddleWebhookRejectionsTotal.WithLabelValues(paddleWebhookRejectionReasonInvalidFormat).Inc()
		s.logger.Warn("Invalid Paddle signature timestamp",
			"error", err,
		)
		return false, fmt.Errorf("%w: %v", ErrInvalidSignatureFormat, err)
	}

	if err := s.checkTimestampTolerance(tsUnix); err != nil {
		observability.PaddleWebhookRejectionsTotal.WithLabelValues(paddleWebhookRejectionReasonTimestampOutOfTolerance).Inc()
		s.logger.Warn("Paddle webhook signature rejected: timestamp outside tolerance window",
			"timestamp", tsUnix,
			"tolerance_seconds", int(s.tolerance.Seconds()),
		)
		return false, err
	}

	// Construct signed payload: "timestamp:body"
	signedPayload := fmt.Sprintf("%s:%s", timestamp, body)

	// Calculate HMAC-SHA256
	h := hmac.New(sha256.New, []byte(s.webhookSecret))
	h.Write([]byte(signedPayload))
	calculatedHash := hex.EncodeToString(h.Sum(nil))
	calculatedHashBytes := []byte(calculatedHash)

	// Constant-time comparison to prevent timing attacks. During secret rotation Paddle may
	// present more than one h1 candidate; accept if any one matches.
	valid := false
	for _, candidate := range hashes {
		if hmac.Equal(calculatedHashBytes, []byte(candidate)) {
			valid = true
			break
		}
	}

	if !valid {
		observability.PaddleWebhookRejectionsTotal.WithLabelValues(paddleWebhookRejectionReasonSignatureMismatch).Inc()
		// Issue #1514, F2: deliberately do NOT log calculatedHash or any candidate hash here —
		// see the doc comment above.
		s.logger.Warn("Paddle webhook signature verification failed",
			"candidate_count", len(hashes),
		)
		return false, ErrSignatureVerificationFailed
	}

	s.logger.Debug("Paddle webhook signature verified successfully",
		"timestamp", timestamp,
	)

	return true, nil
}

// minSaneUnixTimestamp is 2000-01-01T00:00:00Z in Unix seconds. checkTimestampTolerance uses it
// as a floor on `ts=` well below any value this service should ever legitimately see (Paddle
// didn't exist yet), so it costs nothing in false rejections while giving the overflow guard
// below a fixed, human-auditable lower edge.
const minSaneUnixTimestamp int64 = 946684800

// epochBoundSlackSeconds pads the upper sane-epoch bound past s.now()+s.tolerance so the
// pre-check below never fights with the exact ±tolerance comparison that follows it — it only
// exists to keep `eventTime` inside a range where `time.Time` arithmetic (Sub, After, Before)
// cannot overflow, not to do the real tolerance decision itself.
const epochBoundSlackSeconds int64 = 86400

// checkTimestampTolerance rejects tsUnix if it is farther than s.tolerance from s.now() in
// EITHER direction. Symmetric by design (issue #1514, F3): a too-old timestamp is a replay risk,
// and — unlike backend/internal/services/webhook_service.go's Plaid JWT check, which only
// bounds time.Since(issuedAt) and so accepts arbitrarily future-dated tokens — an unbounded
// future timestamp is just as dangerous here and must be rejected too.
//
// Bug fixed here (issue #1514, G2, fix-round-2): for a `ts=` far enough in the future,
// `now.Sub(eventTime)` is unrepresentable as an int64 nanosecond count and time.Duration
// SATURATES to its minimum value instead of overflowing to a large positive one. The old code
// then did `if delta < 0 { delta = -delta }` to get an absolute value — but negating
// math.MinInt64 overflows right back to itself (there is no positive int64 equal to its
// magnitude), so `delta` stayed negative, `delta > s.tolerance` was false, and an arbitrarily
// far-future timestamp was WRONGLY ACCEPTED. The fix below rejects tsUnix against a sane epoch
// range BEFORE any Duration arithmetic runs, so no Duration saturation/overflow is ever allowed
// to decide the outcome — the bound is checked in int64-seconds space, not nanoseconds, and
// s.tolerance is capped at config.PaddleWebhookTimestampToleranceMaxSeconds (300s) elsewhere, so
// nowUnix+tolerance+slack cannot itself overflow int64.
//
// Operational note (issue #1514 fix-round-1 item 2): if this server's clock drifts persistently
// out of sync with Paddle's, EVERY genuine webhook delivery starts failing this check, not just
// a forged one — and observability.PaddleWebhookRejectionsTotal's
// "timestamp_out_of_tolerance" label, sustained and non-zero while "signature_mismatch" stays
// flat, is the intended signal to page on for exactly that scenario. What happens next at
// Paddle's end if every retry of a given event also lands outside the tolerance window (does
// Paddle keep retrying, back off, or eventually give up and mark the event un-deliverable?) is
// NOT established with confidence anywhere in this codebase — there is no vendored Paddle SDK
// or captured retry-exhaustion traffic to check it against, so treat any specific retry-count or
// give-up-time claim about Paddle's behavior as unverified rather than settled fact.
func (s *paddleWebhookService) checkTimestampTolerance(tsUnix int64) error {
	now := s.now()

	// Bound tsUnix to a sane epoch range in int64-seconds space FIRST, before constructing
	// eventTime or doing any time.Duration arithmetic on it. This is what stops the
	// saturate-then-negate overflow described above: once tsUnix is known to be within
	// roughly [2000-01-01, now+tolerance+slack], `now.Sub(eventTime)` can no longer saturate.
	maxSaneUnixTimestamp := now.Unix() + int64(s.tolerance.Seconds()) + epochBoundSlackSeconds
	if tsUnix < minSaneUnixTimestamp || tsUnix > maxSaneUnixTimestamp {
		return fmt.Errorf("%w: timestamp %d is outside the sane epoch range [%d, %d]",
			ErrTimestampOutOfTolerance, tsUnix, minSaneUnixTimestamp, maxSaneUnixTimestamp)
	}

	eventTime := time.Unix(tsUnix, 0)
	delta := now.Sub(eventTime)
	if delta < 0 {
		delta = -delta
	}

	if delta > s.tolerance {
		return fmt.Errorf("%w: timestamp is %s from server clock (tolerance %s)",
			ErrTimestampOutOfTolerance, delta, s.tolerance)
	}
	return nil
}

// parseSignature extracts the timestamp and one-or-more candidate hashes from a
// Paddle-Signature header.
//
// Expected shape: "ts=<unix_seconds>;h1=<hex_hmac>[;h1=<hex_hmac>...]" — the leading component
// must be `ts=`, and every remaining semicolon-separated component must be an `h1=` value.
// Multiple h1 components are accepted (Paddle emits more than one during secret rotation);
// VerifyWebhook treats the signature as valid if ANY of them matches. This parser intentionally
// rejects anything that doesn't fit that shape rather than guessing at other rotation formats
// Paddle might use — see the doc comment on PaddleWebhookService for that caveat.
func parseSignature(signature string) (timestamp string, hashes []string, err error) {
	parts := strings.Split(signature, ";")
	if len(parts) < 2 {
		return "", nil, fmt.Errorf("expected at least 2 parts separated by ';', got %d", len(parts))
	}

	tsParts := strings.SplitN(parts[0], "=", 2)
	if len(tsParts) != 2 || tsParts[0] != "ts" || tsParts[1] == "" {
		return "", nil, fmt.Errorf("invalid timestamp format: %s", parts[0])
	}
	timestamp = tsParts[1]

	for _, part := range parts[1:] {
		hashParts := strings.SplitN(part, "=", 2)
		if len(hashParts) != 2 || hashParts[0] != "h1" || hashParts[1] == "" {
			return "", nil, fmt.Errorf("invalid hash format: %s", part)
		}
		hashes = append(hashes, hashParts[1])
	}

	if len(hashes) == 0 {
		return "", nil, fmt.Errorf("no h1 value present")
	}

	return timestamp, hashes, nil
}

// parseTimestamp parses the ts= component as a bounded Unix-seconds integer, rejecting
// non-numeric, negative, and integer-overflow values without panicking.
func parseTimestamp(raw string) (int64, error) {
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("timestamp %q is not a valid integer: %w", raw, err)
	}
	if v < 0 {
		return 0, fmt.Errorf("timestamp %q is negative", raw)
	}
	return v, nil
}
