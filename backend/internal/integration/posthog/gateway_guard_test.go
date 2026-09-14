package posthog

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProductionWiringNeverInjectsRawPostHogClient(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "..", "cmd", "server", "main.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(source), "SetAnalytics(phClient)")
	assert.NotContains(t, string(source), "NewGDPRClient(phClient")
}

func TestOnlyClientAdapterConstructsPostHogSDKMessages(t *testing.T) {
	backendRoot := filepath.Join("..", "..", "..")
	err := filepath.WalkDir(backendRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return walkErr
		}
		if filepath.Base(path) == "client.go" && filepath.Base(filepath.Dir(path)) == "posthog" {
			return nil
		}
		source, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		assert.NotContains(t, string(source), "github.com/posthog/posthog-go", path)
		assert.NotContains(t, string(source), "posthoggo.Capture", path)
		return nil
	})
	require.NoError(t, err)
}

func TestLegacyRawCaptureProducersArePinned(t *testing.T) {
	// Task #1650 grandfathered these legacy producers while forcing every new producer through
	// ProductAnalytics.CaptureEvent and a generated payload. A path missing from this map, or an
	// extra Capture call in an existing path, is a migration regression that needs an explicit
	// contract review rather than silently expanding the raw API surface.
	want := map[string]int{
		"internal/huma/operations/transfer_suggestion.go":   2,
		"internal/queue/jobs/email_change_propagation.go":   1,
		"internal/queue/jobs/subscription_webhook.go":       5,
		"internal/services/boost_execution_service.go":      1,
		"internal/services/data_export_service.go":          1,
		"internal/services/deletion_service.go":             3,
		"internal/services/household_service.go":            1,
		"internal/services/investment_sync_service.go":      2,
		"internal/services/liability_sync_service.go":       1,
		"internal/services/merchant_service.go":             7,
		"internal/services/past_due_service.go":             2,
		"internal/services/registration_service.go":         1,
		"internal/services/rule_application_service.go":     2,
		"internal/services/savings_goal_service.go":         1,
		"internal/services/transaction_service.go":          3,
		"internal/services/transfer_correlation_service.go": 2,
		"internal/workers/plaid_sync_provider.go":           3,
		"internal/workers/webhook_processor.go":             2,
	}

	backendRoot := filepath.Join("..", "..", "..")
	got := make(map[string]int)
	err := filepath.WalkDir(backendRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return walkErr
		}
		rel, relErr := filepath.Rel(backendRoot, path)
		require.NoError(t, relErr)
		if strings.HasPrefix(filepath.ToSlash(rel), "internal/integration/posthog/") {
			return nil // Gateway/transport compatibility adapters, not event producers.
		}
		source, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		if count := strings.Count(string(source), ".Capture("); count > 0 {
			got[filepath.ToSlash(rel)] = count
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestOperationalFailuresAreNotPostHogProducers(t *testing.T) {
	backendRoot := filepath.Join("..", "..", "..")
	banned := []string{"EventInvestmentSyncFailed", "EventLiabilitySyncFailed", "EventPaddleCleanupFailed", "EventDeadLetterCreated"}
	err := filepath.WalkDir(backendRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return walkErr
		}
		if filepath.Base(path) == "events_generated.go" {
			return nil
		}
		source, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		for _, symbol := range banned {
			assert.NotContains(t, string(source), symbol, path)
		}
		return nil
	})
	require.NoError(t, err)
}
