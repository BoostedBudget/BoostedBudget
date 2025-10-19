package domain

// Region code constants (Fly.io IATA codes)
const (
	RegionIAD = "iad" // US East (Virginia)
	RegionLHR = "lhr" // UK (London)
	RegionSYD = "syd" // Australia (Sydney)

	// DefaultRegion is the default region for new users (MVP single region)
	DefaultRegion = RegionIAD
)

// RegionInfo contains detailed information about a region
type RegionInfo struct {
	Code        string // Fly.io IATA code (e.g., "iad")
	DisplayName string // Human-readable name (e.g., "USA (Virginia)")
	Country     string // Country name
	City        string // City/region name
	FlyRegion   string // Fly.io region code (same as Code, kept for consistency)
}

// Regions is the authoritative map of all supported regions
var Regions = map[string]RegionInfo{
	RegionIAD: {
		Code:        RegionIAD,
		DisplayName: "USA (Virginia)",
		Country:     "USA",
		City:        "Virginia",
		FlyRegion:   RegionIAD,
	},
	RegionLHR: {
		Code:        RegionLHR,
		DisplayName: "UK (London)",
		Country:     "UK",
		City:        "London",
		FlyRegion:   RegionLHR,
	},
	RegionSYD: {
		Code:        RegionSYD,
		DisplayName: "Australia (Sydney)",
		Country:     "Australia",
		City:        "Sydney",
		FlyRegion:   RegionSYD,
	},
}

// GetRegionInfo returns the region information for a given region code
// Returns (nil, false) if the region code is not valid
func GetRegionInfo(code string) (*RegionInfo, bool) {
	info, exists := Regions[code]
	if !exists {
		return nil, false
	}
	return &info, true
}

// IsValidRegion checks if a region code is valid
func IsValidRegion(code string) bool {
	_, exists := Regions[code]
	return exists
}

// GetDisplayName returns the display name for a region code
// Returns the code itself if not found (graceful degradation)
func GetDisplayName(code string) string {
	if info, exists := GetRegionInfo(code); exists {
		return info.DisplayName
	}
	return code
}

// GetValidRegions returns a slice of all valid region codes
func GetValidRegions() []string {
	codes := make([]string, 0, len(Regions))
	for code := range Regions {
		codes = append(codes, code)
	}
	return codes
}
