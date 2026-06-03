package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"gopkg.in/yaml.v3"
)

// RegionDefinition represents a single region's configuration from regions.yaml
type RegionDefinition struct {
	DisplayName    string               `yaml:"display_name"`
	Country        string               `yaml:"country"`
	City           string               `yaml:"city"`
	Flag           string               `yaml:"flag"`
	Infrastructure RegionInfrastructure `yaml:"infrastructure"`
}

// RegionInfrastructure maps a region to its infrastructure env var names.
// Actual credentials are in env vars, not here.
type RegionInfrastructure struct {
	FlyRegion      string `yaml:"fly_region"`
	NeonProjectEnv string `yaml:"neon_project_env"`
	DatabaseURLEnv string `yaml:"database_url_env"`
}

// RegionConfig holds region configuration loaded from config/regions.yaml
type RegionConfig struct {
	ServerRegion  string                      `yaml:"server_region"`
	DefaultRegion string                      `yaml:"default_region"`
	Regions       map[string]RegionDefinition `yaml:"regions"`
	mu            sync.RWMutex                `yaml:"-"`
}

// Global singleton
var (
	globalRegionConfig *RegionConfig
	regionConfigOnce   sync.Once
	regionConfigError  error
)

// LoadRegionConfig loads region configuration from YAML file.
// Returns cached instance on subsequent calls (singleton pattern).
func LoadRegionConfig() (*RegionConfig, error) {
	regionConfigOnce.Do(func() {
		globalRegionConfig, regionConfigError = loadRegionConfigFromFile()
	})
	return globalRegionConfig, regionConfigError
}

func getRegionConfigPath() string {
	if explicit := getEnv("REGIONS_CONFIG", ""); explicit != "" {
		return explicit
	}
	return "config/regions.yaml"
}

func loadRegionConfigFromFile() (*RegionConfig, error) {
	configPath := getRegionConfigPath()

	if !filepath.IsAbs(configPath) {
		configPath = findConfigFile(configPath)
		if _, err := os.Stat(configPath); err != nil {
			wd, _ := os.Getwd()
			return nil, fmt.Errorf("region config file not found at %s (searched from %s)", configPath, wd)
		}
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read region config file %s: %w", configPath, err)
	}

	var rc RegionConfig
	if err := yaml.Unmarshal(data, &rc); err != nil {
		return nil, fmt.Errorf("failed to parse region config YAML: %w", err)
	}

	if err := rc.validate(); err != nil {
		return nil, fmt.Errorf("region config validation failed: %w", err)
	}

	return &rc, nil
}

func (rc *RegionConfig) validate() error {
	if len(rc.Regions) == 0 {
		return fmt.Errorf("no regions defined in configuration")
	}
	if _, ok := rc.Regions[rc.DefaultRegion]; !ok {
		return fmt.Errorf("default_region '%s' not found in regions map", rc.DefaultRegion)
	}
	if _, ok := rc.Regions[rc.ServerRegion]; !ok {
		return fmt.Errorf("server_region '%s' not found in regions map", rc.ServerRegion)
	}
	for code, region := range rc.Regions {
		if region.DisplayName == "" {
			return fmt.Errorf("region '%s' missing display_name", code)
		}
	}
	return nil
}

// IsValidRegion returns true if the region code is in the configured regions map.
func (rc *RegionConfig) IsValidRegion(code string) bool {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	_, ok := rc.Regions[code]
	return ok
}

// GetRegion returns the RegionDefinition for a code, or an error if not found.
func (rc *RegionConfig) GetRegion(code string) (RegionDefinition, error) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	region, ok := rc.Regions[code]
	if !ok {
		return RegionDefinition{}, fmt.Errorf("region '%s' not found", code)
	}
	return region, nil
}

// GetValidRegionCodes returns a sorted slice of all configured region codes.
func (rc *RegionConfig) GetValidRegionCodes() []string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	codes := make([]string, 0, len(rc.Regions))
	for code := range rc.Regions {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

// GetDefaultRegion returns the default region code.
func (rc *RegionConfig) GetDefaultRegion() string {
	return rc.DefaultRegion
}

// GetServerRegion returns the region this server instance runs in.
func (rc *RegionConfig) GetServerRegion() string {
	return rc.ServerRegion
}

// Reload reloads region configuration from disk. Thread-safe.
func (rc *RegionConfig) Reload() error {
	newConfig, err := loadRegionConfigFromFile()
	if err != nil {
		return err
	}
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.ServerRegion = newConfig.ServerRegion
	rc.DefaultRegion = newConfig.DefaultRegion
	rc.Regions = newConfig.Regions
	return nil
}
