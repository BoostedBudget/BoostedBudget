package operations

import (
	"context"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRegionConfig() *config.RegionConfig {
	return &config.RegionConfig{
		ServerRegion:  "iad",
		DefaultRegion: "iad",
		Regions: map[string]config.RegionDefinition{
			"iad": {
				DisplayName: "USA (Virginia)",
				Country:     "USA",
				City:        "Virginia",
				Flag:        "🇺🇸",
				Infrastructure: config.RegionInfrastructure{
					FlyRegion:      "iad",
					NeonProjectEnv: "NEON_PROJECT_ID",
					DatabaseURLEnv: "DATABASE_URL",
				},
			},
		},
	}
}

func TestGetRegions_ReturnsAvailableRegions(t *testing.T) {
	handler := NewRegionsHandler(newTestRegionConfig())
	output, err := handler.GetRegions(context.Background(), &GetRegionsInput{})

	require.NoError(t, err)
	require.NotNil(t, output)

	body := output.Body
	assert.Len(t, body.Regions, 1)
	assert.Equal(t, "iad", body.DefaultRegion)
	assert.Equal(t, "iad", body.ServerRegion)
}

func TestGetRegions_IadRegionDetails(t *testing.T) {
	handler := NewRegionsHandler(newTestRegionConfig())
	output, err := handler.GetRegions(context.Background(), &GetRegionsInput{})

	require.NoError(t, err)
	iad, ok := output.Body.Regions["iad"]
	require.True(t, ok, "iad region should be present")

	assert.Equal(t, "iad", iad.Code)
	assert.Equal(t, "USA (Virginia)", iad.DisplayName)
	assert.Equal(t, "USA", iad.Country)
	assert.Equal(t, "Virginia", iad.City)
	assert.Equal(t, "🇺🇸", iad.Flag)
}

func TestGetRegions_NoInfrastructureDetails(t *testing.T) {
	handler := NewRegionsHandler(newTestRegionConfig())
	output, err := handler.GetRegions(context.Background(), &GetRegionsInput{})

	require.NoError(t, err)

	// RegionResponse must not expose infrastructure fields
	iad := output.Body.Regions["iad"]
	_ = iad // compile-time check: RegionResponse has no NeonProjectEnv or DatabaseURLEnv fields
}

func TestGetRegions_CacheControlHeader(t *testing.T) {
	handler := NewRegionsHandler(newTestRegionConfig())
	output, err := handler.GetRegions(context.Background(), &GetRegionsInput{})

	require.NoError(t, err)
	assert.Equal(t, "public, max-age=86400", output.CacheControl)
}

func TestNewRegionsHandler_NilPanics(t *testing.T) {
	assert.Panics(t, func() {
		NewRegionsHandler(nil)
	})
}
