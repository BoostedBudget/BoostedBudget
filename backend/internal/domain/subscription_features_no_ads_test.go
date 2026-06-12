package domain

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSubscriptionFeaturesHasNoAdsField guards against the ads concept being reintroduced.
// The product has no ads — not even during the trial (#1288 follow-on).
func TestSubscriptionFeaturesHasNoAdsField(t *testing.T) {
	typ := reflect.TypeOf(SubscriptionFeatures{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		fieldNameLower := strings.ToLower(f.Name)
		jsonTag := f.Tag.Get("json")
		yamlTag := f.Tag.Get("yaml")
		assert.NotEqual(t, "adsenabled", fieldNameLower,
			"AdsEnabled field must not exist on SubscriptionFeatures")
		assert.NotContains(t, jsonTag, "ads_enabled",
			"no ads_enabled json tag on SubscriptionFeatures")
		assert.NotContains(t, yamlTag, "ads_enabled",
			"no ads_enabled yaml tag on SubscriptionFeatures")
	}
}
