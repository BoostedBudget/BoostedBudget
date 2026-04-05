package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// initTestRegistry sets up an iad-only region registry for tests.
// Call this in any test that exercises region logic.
func initTestRegistry() {
	InitRegionRegistry(
		map[string]RegionInfo{
			"iad": {
				Code:        "iad",
				DisplayName: "USA (Virginia)",
				Country:     "USA",
				City:        "Virginia",
				Flag:        "🇺🇸",
				FlyRegion:   "iad",
			},
		},
		"iad",
		"iad",
	)
}

func TestInitRegionRegistry(t *testing.T) {
	initTestRegistry()
	assert.True(t, IsValidRegion("iad"))
	assert.False(t, IsValidRegion("lhr"))
	assert.False(t, IsValidRegion("syd"))
}

func TestGetRegionInfo(t *testing.T) {
	initTestRegistry()

	tests := []struct {
		name           string
		code           string
		expectedExists bool
		expectedName   string
	}{
		{"valid region iad", "iad", true, "USA (Virginia)"},
		{"lhr no longer valid", "lhr", false, ""},
		{"syd no longer valid", "syd", false, ""},
		{"invalid region", "invalid", false, ""},
		{"old AWS-style code", "us-east-1", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, exists := GetRegionInfo(tt.code)
			assert.Equal(t, tt.expectedExists, exists)
			if tt.expectedExists {
				assert.NotNil(t, info)
				assert.Equal(t, tt.expectedName, info.DisplayName)
			} else {
				assert.Nil(t, info)
			}
		})
	}
}

func TestIsValidRegion(t *testing.T) {
	initTestRegistry()

	tests := []struct {
		name     string
		code     string
		expected bool
	}{
		{"valid iad", "iad", true},
		{"lhr no longer valid", "lhr", false},
		{"syd no longer valid", "syd", false},
		{"invalid code", "invalid", false},
		{"old AWS code us-east-1", "us-east-1", false},
		{"old AWS code eu-west-1", "eu-west-1", false},
		{"old AWS code ap-southeast-1", "ap-southeast-1", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsValidRegion(tt.code))
		})
	}
}

func TestGetDisplayName(t *testing.T) {
	initTestRegistry()

	tests := []struct {
		name     string
		code     string
		expected string
	}{
		{"iad display name", "iad", "USA (Virginia)"},
		{"lhr returns code (graceful fallback)", "lhr", "lhr"},
		{"invalid returns code", "invalid", "invalid"},
		{"old AWS code returns code", "us-east-1", "us-east-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, GetDisplayName(tt.code))
		})
	}
}

func TestGetValidRegions(t *testing.T) {
	initTestRegistry()

	regions := GetValidRegions()
	assert.Len(t, regions, 1)
	assert.Contains(t, regions, "iad")
	assert.NotContains(t, regions, "lhr")
	assert.NotContains(t, regions, "syd")
}

func TestDefaultRegionCode(t *testing.T) {
	initTestRegistry()
	assert.Equal(t, "iad", DefaultRegionCode())
}

func TestDefaultRegionCode_Fallback(t *testing.T) {
	// Even with nil registry, returns "iad" as safe fallback
	regionRegistry = nil
	assert.Equal(t, "iad", DefaultRegionCode())
	initTestRegistry() // restore
}

func TestIsValidRegion_NilRegistry(t *testing.T) {
	regionRegistry = nil
	assert.False(t, IsValidRegion("iad")) // nil registry returns false
	initTestRegistry()                    // restore
}
