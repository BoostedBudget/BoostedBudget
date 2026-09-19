package posthog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Issue #1652 Todo 13 (vi) — self-guard for gateway_guard_test.go's pinned raw-producer map.
//
// TestLegacyRawCaptureProducersArePinned compares the walked producer inventory by EXACT equality
// against a pinned `want` map. That is only a guard if an unpinned producer actually lands in the
// walked map. This test drives the same walk function against a synthetic tree containing one raw
// `Capture(` call and proves the walked map contains it — so adding a raw producer without pinning
// it breaks the exact-equality and turns the guard red.
func TestRawCaptureWalkFlagsAnUnpinnedProducer(t *testing.T) {
	dir := t.TempDir()
	unpinned := filepath.Join(dir, "unpinned_producer.go")
	require.NoError(t, os.WriteFile(unpinned, []byte(`package fixture

import phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"

func f(analytics interface{ Capture(string, string, map[string]interface{}) }, userID string) {
	analytics.Capture(userID, phclient.EventUnpinnedProducer, nil)
}
`), 0o644))

	got := walkRawCaptureProducers(t, dir)

	require.Contains(t, got, "unpinned_producer.go", "the walk must inventory the synthetic producer")
	assert.Equal(t, []string{"Capture:phclient.EventUnpinnedProducer"}, got["unpinned_producer.go"])
	assert.NotEqual(t, map[string][]string{}, got,
		"an unpinned raw producer must appear in the walked map, breaking the pinned exact-equality")
}

// TestRawCaptureWalkIgnoresTestAndAdapterFiles proves the walk's two exclusions still hold for the
// synthetic tree: a raw producer in a _test.go file, or under internal/integration/posthog/, is not
// inventoried — so the meta-test above is testing the same universe the pinned guard walks.
func TestRawCaptureWalkIgnoresTestAndAdapterFiles(t *testing.T) {
	dir := t.TempDir()
	source := []byte(`package fixture

import phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"

func f(analytics interface{ Capture(string, string, map[string]interface{}) }, userID string) {
	analytics.Capture(userID, phclient.EventIgnored, nil)
}
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "producer_test.go"), source, 0o644))
	adapterDir := filepath.Join(dir, "internal", "integration", "posthog")
	require.NoError(t, os.MkdirAll(adapterDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(adapterDir, "adapter.go"), source, 0o644))

	got := walkRawCaptureProducers(t, dir)
	assert.Empty(t, got)
}
