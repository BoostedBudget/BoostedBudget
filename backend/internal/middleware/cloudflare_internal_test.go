package middleware

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestExemptedPaths_ContainsHealthOnly pins the exact shape of the shared exempt-path list after
// #1531: only /health and its trailing-slash variant, nothing else.
func TestExemptedPaths_ContainsHealthOnly(t *testing.T) {
	assert.True(t, isExemptPath("/health"))
	assert.True(t, isExemptPath("/health/"))
	assert.Len(t, exemptedPaths, 2, "exemptedPaths must contain exactly /health and /health/ — anything else re-opens the fail-open trap #1531 closed")
}

// TestExemptedPaths_MetricsIsNoLongerExemptBecauseItIsNoLongerPublic pins the #1531 removal:
// /metrics is served only on the internal listener (cmd/server/internal_metrics.go) since #1514
// and is never registered on the public Echo instance CloudflareOnly/IPRateLimiter guard, so
// treating it as exempt here would be a latent fail-open trap if that ever changed.
func TestExemptedPaths_MetricsIsNoLongerExemptBecauseItIsNoLongerPublic(t *testing.T) {
	assert.False(t, isExemptPath("/metrics"))
	assert.False(t, isExemptPath("/metrics/"))
}
