package domain

// RegionInfo contains display and routing information for a data residency region.
type RegionInfo struct {
	Code        string // Fly.io IATA code (e.g., "iad")
	DisplayName string // Human-readable name (e.g., "USA (Virginia)")
	Country     string // Country name
	City        string // City/region name
	Flag        string // Country flag emoji
	FlyRegion   string // Fly.io region code (same as Code)
}

// RegionRegistry holds the set of configured regions, populated at startup via InitRegionRegistry.
type RegionRegistry struct {
	regions       map[string]RegionInfo
	defaultRegion string
	serverRegion  string
}

// regionRegistry is the package-level singleton, set by InitRegionRegistry at startup.
var regionRegistry *RegionRegistry

// InitRegionRegistry must be called once at startup (from main.go) after loading RegionConfig.
// It populates the domain-level region data used across the application.
func InitRegionRegistry(regions map[string]RegionInfo, defaultRegion, serverRegion string) {
	regionRegistry = &RegionRegistry{
		regions:       regions,
		defaultRegion: defaultRegion,
		serverRegion:  serverRegion,
	}
}

// DefaultRegionCode returns the configured default region code.
// Falls back to "iad" if the registry has not been initialized (test safety).
func DefaultRegionCode() string {
	if regionRegistry == nil {
		return "iad"
	}
	return regionRegistry.defaultRegion
}

// IsValidRegion returns true if code is in the configured regions map.
// Returns false if the registry has not been initialized.
func IsValidRegion(code string) bool {
	if regionRegistry == nil {
		return false
	}
	_, ok := regionRegistry.regions[code]
	return ok
}

// GetRegionInfo returns the RegionInfo for code.
// Returns (nil, false) if code is not found or registry is nil.
func GetRegionInfo(code string) (*RegionInfo, bool) {
	if regionRegistry == nil {
		return nil, false
	}
	info, ok := regionRegistry.regions[code]
	if !ok {
		return nil, false
	}
	return &info, true
}

// GetDisplayName returns the human-readable name for code.
// Falls back to returning code itself if not found (graceful degradation).
func GetDisplayName(code string) string {
	if info, ok := GetRegionInfo(code); ok {
		return info.DisplayName
	}
	return code
}

// GetValidRegions returns a slice of all configured region codes.
func GetValidRegions() []string {
	if regionRegistry == nil {
		return nil
	}
	codes := make([]string, 0, len(regionRegistry.regions))
	for code := range regionRegistry.regions {
		codes = append(codes, code)
	}
	return codes
}
