package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRegionConstants(t *testing.T) {
	assert.Equal(t, "iad", RegionIAD)
	assert.Equal(t, "lhr", RegionLHR)
	assert.Equal(t, "syd", RegionSYD)
}

func TestGetRegionInfo(t *testing.T) {
	tests := []struct {
		name           string
		code           string
		expectedInfo   *RegionInfo
		expectedExists bool
	}{
		{
			name: "valid region iad",
			code: "iad",
			expectedInfo: &RegionInfo{
				Code:        "iad",
				DisplayName: "USA (Virginia)",
				Country:     "USA",
				City:        "Virginia",
				FlyRegion:   "iad",
			},
			expectedExists: true,
		},
		{
			name: "valid region lhr",
			code: "lhr",
			expectedInfo: &RegionInfo{
				Code:        "lhr",
				DisplayName: "UK (London)",
				Country:     "UK",
				City:        "London",
				FlyRegion:   "lhr",
			},
			expectedExists: true,
		},
		{
			name: "valid region syd",
			code: "syd",
			expectedInfo: &RegionInfo{
				Code:        "syd",
				DisplayName: "Australia (Sydney)",
				Country:     "Australia",
				City:        "Sydney",
				FlyRegion:   "syd",
			},
			expectedExists: true,
		},
		{
			name:           "invalid region",
			code:           "invalid",
			expectedInfo:   nil,
			expectedExists: false,
		},
		{
			name:           "old AWS-style code should not work",
			code:           "us-east-1",
			expectedInfo:   nil,
			expectedExists: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, exists := GetRegionInfo(tt.code)
			assert.Equal(t, tt.expectedExists, exists)

			if tt.expectedExists {
				assert.NotNil(t, info)
				assert.Equal(t, tt.expectedInfo.Code, info.Code)
				assert.Equal(t, tt.expectedInfo.DisplayName, info.DisplayName)
				assert.Equal(t, tt.expectedInfo.Country, info.Country)
				assert.Equal(t, tt.expectedInfo.City, info.City)
				assert.Equal(t, tt.expectedInfo.FlyRegion, info.FlyRegion)
			} else {
				assert.Nil(t, info)
			}
		})
	}
}

func TestIsValidRegion(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected bool
	}{
		{"valid iad", "iad", true},
		{"valid lhr", "lhr", true},
		{"valid syd", "syd", true},
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
	tests := []struct {
		name     string
		code     string
		expected string
	}{
		{"iad display name", "iad", "USA (Virginia)"},
		{"lhr display name", "lhr", "UK (London)"},
		{"syd display name", "syd", "Australia (Sydney)"},
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
	regions := GetValidRegions()

	assert.Len(t, regions, 3)
	assert.Contains(t, regions, "iad")
	assert.Contains(t, regions, "lhr")
	assert.Contains(t, regions, "syd")
}

func TestDefaultRegion(t *testing.T) {
	assert.Equal(t, "iad", DefaultRegion)
}
