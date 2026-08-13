package operations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"

	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// recordInvalidWebhookAttempt persists bounded, non-identifying metadata about a rejected
// (unsigned or invalid-signature) webhook delivery — Issue #1514.
//
// It NEVER receives the raw payload or any attacker-chosen identifier (e.g. a Paddle event_id):
// only the provider name, the body length, and a SHA-256 digest of the body are recorded, and
// the underlying store enforces an amortised row bound per provider: trimming back to the cap
// runs only every invalidWebhookAttemptTrimInterval-th Record call, so the table can legitimately
// reach roughly cap + trimInterval - 1 rows (currently 10000 + 50 - 1) before a trim fires, plus
// further overshoot from concurrent writers racing between trims — see
// repository.InvalidWebhookAttemptRepository for the exact schedule and concurrency caveats.
//
// The Prometheus counter is always incremented, even if repo is nil or the write fails, so the
// security signal survives a degraded or unconfigured audit store.
func recordInvalidWebhookAttempt(ctx context.Context, repo repository.InvalidWebhookAttemptRepository, provider string, body []byte, logger *slog.Logger) {
	observability.InvalidWebhookAttemptsTotal.WithLabelValues(provider).Inc()

	if repo == nil {
		return
	}

	digest := sha256.Sum256(body)
	if err := repo.Record(ctx, provider, len(body), hex.EncodeToString(digest[:])); err != nil {
		if logger != nil {
			logger.Error("failed to record invalid webhook attempt", "provider", provider, "error", err)
		}
	}
}
