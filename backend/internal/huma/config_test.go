package huma

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAPIDescriptionHasNoAdClaim guards against ad-related copy returning to the API description
// string (#1288 follow-on: the product has no ads, not even during the trial).
func TestAPIDescriptionHasNoAdClaim(t *testing.T) {
	desc := strings.ToLower(NewAPIConfig("v1", "test").Info.Description)
	for _, bad := range []string{"ad-supported", "ad-free", "ads_enabled", "adsense"} {
		assert.NotContains(t, desc, bad, "API description must carry no ad claim (#1288 follow-on)")
	}
}
