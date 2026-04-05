package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRegionConfig() *RegionConfig {
	return &RegionConfig{
		ServerRegion:  "iad",
		DefaultRegion: "iad",
		Regions: map[string]RegionDefinition{
			"iad": {
				DisplayName: "USA (Virginia)",
				Country:     "USA",
				City:        "Virginia",
				Flag:        "🇺🇸",
				Infrastructure: RegionInfrastructure{
					FlyRegion:      "iad",
					NeonProjectEnv: "NEON_PROJECT_ID",
					DatabaseURLEnv: "DATABASE_URL",
				},
			},
		},
	}
}

func TestLoadRegionConfig(t *testing.T) {
	// Reset singleton for test isolation
	globalRegionConfig = nil
	regionConfigOnce = sync.Once{}

	config, err := LoadRegionConfig()
	require.NoError(t, err)
	require.NotNil(t, config)
	assert.Equal(t, "iad", config.DefaultRegion)
	assert.Equal(t, "iad", config.ServerRegion)
	assert.Len(t, config.Regions, 1)

	// Verify singleton - second call returns same instance
	config2, err := LoadRegionConfig()
	require.NoError(t, err)
	assert.Same(t, config, config2)
}

func TestRegionConfig_GetRegion(t *testing.T) {
	rc := newTestRegionConfig()

	region, err := rc.GetRegion("iad")
	require.NoError(t, err)
	assert.Equal(t, "USA (Virginia)", region.DisplayName)
	assert.Equal(t, "USA", region.Country)
	assert.Equal(t, "Virginia", region.City)
	assert.Equal(t, "🇺🇸", region.Flag)
	assert.Equal(t, "iad", region.Infrastructure.FlyRegion)
	assert.Equal(t, "NEON_PROJECT_ID", region.Infrastructure.NeonProjectEnv)
	assert.Equal(t, "DATABASE_URL", region.Infrastructure.DatabaseURLEnv)
}

func TestRegionConfig_GetRegion_NotFound(t *testing.T) {
	rc := newTestRegionConfig()

	_, err := rc.GetRegion("lhr")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "lhr")

	_, err = rc.GetRegion("syd")
	assert.Error(t, err)
}

func TestRegionConfig_IsValidRegion(t *testing.T) {
	rc := newTestRegionConfig()

	assert.True(t, rc.IsValidRegion("iad"))
	assert.False(t, rc.IsValidRegion("lhr"))
	assert.False(t, rc.IsValidRegion("syd"))
	assert.False(t, rc.IsValidRegion(""))
	assert.False(t, rc.IsValidRegion("us-east-1"))
}

func TestRegionConfig_GetValidRegionCodes(t *testing.T) {
	rc := newTestRegionConfig()

	codes := rc.GetValidRegionCodes()
	assert.Equal(t, []string{"iad"}, codes)
}

func TestRegionConfig_GetDefaultRegion(t *testing.T) {
	rc := newTestRegionConfig()
	assert.Equal(t, "iad", rc.GetDefaultRegion())
}

func TestRegionConfig_GetServerRegion(t *testing.T) {
	rc := newTestRegionConfig()
	assert.Equal(t, "iad", rc.GetServerRegion())
}

func TestRegionConfig_Validate_NoRegions(t *testing.T) {
	rc := &RegionConfig{
		ServerRegion:  "iad",
		DefaultRegion: "iad",
		Regions:       map[string]RegionDefinition{},
	}
	err := rc.validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no regions")
}

func TestRegionConfig_Validate_MissingDefault(t *testing.T) {
	rc := &RegionConfig{
		ServerRegion:  "iad",
		DefaultRegion: "lhr", // not in regions map
		Regions: map[string]RegionDefinition{
			"iad": {DisplayName: "USA (Virginia)"},
		},
	}
	err := rc.validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "default_region")
}

func TestRegionConfig_Validate_MissingServerRegion(t *testing.T) {
	rc := &RegionConfig{
		ServerRegion:  "lhr", // not in regions map
		DefaultRegion: "iad",
		Regions: map[string]RegionDefinition{
			"iad": {DisplayName: "USA (Virginia)"},
		},
	}
	err := rc.validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "server_region")
}

func TestRegionConfig_Validate_MissingDisplayName(t *testing.T) {
	rc := &RegionConfig{
		ServerRegion:  "iad",
		DefaultRegion: "iad",
		Regions: map[string]RegionDefinition{
			"iad": {DisplayName: ""}, // missing
		},
	}
	err := rc.validate()
	assert.Error(t, err)
}

func TestRegionConfig_InvalidPath(t *testing.T) {
	// Reset singleton
	globalRegionConfig = nil
	regionConfigOnce = sync.Once{}

	os.Setenv("REGIONS_CONFIG", "/nonexistent/path/regions.yaml")
	defer os.Unsetenv("REGIONS_CONFIG")

	_, err := LoadRegionConfig()
	assert.Error(t, err)

	// Reset for other tests
	globalRegionConfig = nil
	regionConfigOnce = sync.Once{}
}

func TestRegionConfig_Reload(t *testing.T) {
	// Write a temp regions file
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "regions.yaml")
	content := `
server_region: "iad"
default_region: "iad"
regions:
  iad:
    display_name: "USA (Virginia)"
    country: "USA"
    city: "Virginia"
    flag: "🇺🇸"
    infrastructure:
      fly_region: "iad"
      neon_project_env: "NEON_PROJECT_ID"
      database_url_env: "DATABASE_URL"
`
	require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0600))

	// Reset singleton and load with override
	globalRegionConfig = nil
	regionConfigOnce = sync.Once{}
	os.Setenv("REGIONS_CONFIG", yamlPath)
	defer func() {
		os.Unsetenv("REGIONS_CONFIG")
		globalRegionConfig = nil
		regionConfigOnce = sync.Once{}
	}()

	rc, err := LoadRegionConfig()
	require.NoError(t, err)
	assert.Equal(t, "iad", rc.DefaultRegion)

	// Reload works without error
	err = rc.Reload()
	assert.NoError(t, err)
}
