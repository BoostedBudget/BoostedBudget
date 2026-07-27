package domain

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSubscriptionFeaturesHasNoAIInsightsField guards against the ai_insights concept
// being reintroduced (#1411). The product does not ship AI-powered insights, and
// backend/internal/config/tier_config.go uses plain yaml.Unmarshal (not
// KnownFields(true)), so a reintroduced ai_insights: key in tier YAML would be silently
// ignored rather than erroring. This struct-level guard is what actually catches it.
func TestSubscriptionFeaturesHasNoAIInsightsField(t *testing.T) {
	typ := reflect.TypeOf(SubscriptionFeatures{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		fieldNameLower := strings.ToLower(f.Name)
		jsonTag := f.Tag.Get("json")
		yamlTag := f.Tag.Get("yaml")
		assert.NotEqual(t, "aiinsights", fieldNameLower,
			"AIInsights field must not exist on SubscriptionFeatures")
		assert.NotContains(t, jsonTag, "ai_insights",
			"no ai_insights json tag on SubscriptionFeatures")
		assert.NotContains(t, yamlTag, "ai_insights",
			"no ai_insights yaml tag on SubscriptionFeatures")
	}
}

// findRepoRoot resolves the repository root from this test file's own location (via
// runtime.Caller), so the artifact paths below work regardless of the directory `go test`
// is invoked from. This file lives at backend/internal/domain/, three levels below the repo
// root.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "could not determine test file location via runtime.Caller")
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
}

// TestSubscriptionTierYAMLHasNoAIInsights guards against ai_insights reappearing in the tier
// YAML configs (#1411 branch review follow-up). As documented on the struct-level test above
// and in tier_config_test.go's TestSubscriptionTierYAMLHasNoAdsEnabled (same gap, ads case),
// backend/internal/config/tier_config.go:133 calls plain yaml.Unmarshal — not
// UnmarshalStrict/KnownFields(true) — so a reintroduced `ai_insights:` key in tier YAML is
// silently ignored by the parser rather than erroring, and would never surface in the
// unmarshalled SubscriptionFeatures struct the test above inspects. Reading the raw file
// bytes is the only way to catch that case, following the same precedent.
func TestSubscriptionTierYAMLHasNoAIInsights(t *testing.T) {
	root := findRepoRoot(t)
	yamlPaths := []string{
		filepath.Join(root, "config", "subscription_tiers.yaml"),
		filepath.Join(root, "config", "subscription_tiers.production.yaml"),
		filepath.Join(root, "config", "subscription_tiers.sandbox.yaml"),
	}
	for _, path := range yamlPaths {
		raw, err := os.ReadFile(path)
		require.NoError(t, err, "could not read %s", path)
		assert.NotContains(t, strings.ToLower(string(raw)), "ai_insights",
			"%s must not contain an ai_insights key (#1411) — the product does not ship AI insights, "+
				"and tier_config.go's plain yaml.Unmarshal would silently ignore it rather than erroring", path)
	}
}

// yamlBlockPattern matches a top-level "<Key>:" declaration line, capturing its indentation
// so extractYAMLSchemaBlock knows where the block starts and how far it is indented.
var yamlBlockPattern = func(key string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^(\s*)` + regexp.QuoteMeta(key) + `:\s*$`)
}

// extractYAMLSchemaBlock extracts the text of a top-level schema definition (e.g.
// "SubscriptionFeatures" under components.schemas) from raw OpenAPI YAML: everything from
// the line declaring the key up to (but not including) the next line at the same or lesser
// indentation. This is a plain-text scan, not a full YAML parse — deliberately consistent
// with the raw-bytes-grep approach the tier YAML test above (and tier_config_test.go) use,
// rather than adding a YAML parsing dependency just for a guard test. Scoping to this one
// schema's block (rather than grepping the whole 5600+ line spec) keeps the failure message
// pointing at exactly the properties/required section that matters, and avoids the test ever
// false-failing on an unrelated, legitimate mention elsewhere in the spec.
func extractYAMLSchemaBlock(t *testing.T, raw, key string) string {
	t.Helper()
	lines := strings.Split(raw, "\n")
	pattern := yamlBlockPattern(key)

	startIdx := -1
	indent := 0
	for i, line := range lines {
		if m := pattern.FindStringSubmatch(line); m != nil {
			startIdx = i
			indent = len(m[1])
			break
		}
	}
	require.NotEqual(t, -1, startIdx, "could not find a %q: schema block in the OpenAPI spec", key)

	end := len(lines)
	for i := startIdx + 1; i < len(lines); i++ {
		trimmed := strings.TrimRight(lines[i], " \t")
		if trimmed == "" {
			continue // blank lines don't end the block
		}
		lineIndent := len(lines[i]) - len(strings.TrimLeft(lines[i], " "))
		if lineIndent <= indent {
			end = i
			break
		}
	}

	return strings.Join(lines[startIdx:end], "\n")
}

// TestOpenAPISchemaHasNoAIInsights guards against ai_insights reappearing in the OpenAPI
// SubscriptionFeatures schema — both its properties map and its required array (#1411
// branch review follow-up). api/openapi.yaml is generated from the Huma routes
// (make generate-openapi) but is also the contract Orval reads to generate the frontend
// TypeScript client, so a reintroduction here would flow straight into generated code even
// though the Go struct and tier YAML guards above are both clean.
func TestOpenAPISchemaHasNoAIInsights(t *testing.T) {
	root := findRepoRoot(t)
	path := filepath.Join(root, "api", "openapi.yaml")
	raw, err := os.ReadFile(path)
	require.NoError(t, err, "could not read %s", path)

	block := extractYAMLSchemaBlock(t, string(raw), "SubscriptionFeatures")
	assert.NotContains(t, strings.ToLower(block), "ai_insights",
		"%s SubscriptionFeatures schema (properties/required) must not contain ai_insights (#1411):\n%s",
		path, block)
}

// TestOrvalSubscriptionFeaturesModelHasNoAIInsights guards against ai_insights reappearing in
// the generated frontend TypeScript model (#1411 branch review follow-up). This file is
// regenerated by `make generate-api-client` whenever the OpenAPI spec changes; this test
// catches both a regenerated model that picked up a reintroduced spec field, and a
// hand-edit of the generated file that bypasses regeneration entirely.
func TestOrvalSubscriptionFeaturesModelHasNoAIInsights(t *testing.T) {
	root := findRepoRoot(t)
	path := filepath.Join(root, "packages", "api-client", "src", "models", "subscriptionFeatures.ts")
	raw, err := os.ReadFile(path)
	require.NoError(t, err, "could not read %s", path)
	assert.NotContains(t, strings.ToLower(string(raw)), "ai_insights",
		"%s must not contain ai_insights (#1411)", path)
}
