package domain

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
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
