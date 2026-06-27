package auth_test

import (
	"context"
	"testing"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeSessionClaims creates a clerk.SessionClaims with the given Custom payload.
func makeSessionClaims(custom any) *clerk.SessionClaims {
	return &clerk.SessionClaims{Custom: custom}
}

// --- ExtractEmailFromClaims ---

func TestExtractEmailFromClaims(t *testing.T) {
	tests := []struct {
		name     string
		claims   *clerk.SessionClaims
		expected string
	}{
		{
			name:     "nil claims returns empty string",
			claims:   nil,
			expected: "",
		},
		{
			name:     "nil Custom returns empty string",
			claims:   &clerk.SessionClaims{Custom: nil},
			expected: "",
		},
		{
			name: "CustomClaims with email set",
			claims: makeSessionClaims(&auth.CustomClaims{
				Email: "user@example.com",
			}),
			expected: "user@example.com",
		},
		{
			name: "CustomClaims with empty email falls through to empty string",
			claims: makeSessionClaims(&auth.CustomClaims{
				Email: "",
			}),
			expected: "",
		},
		{
			name: "map with email key returns email",
			claims: makeSessionClaims(map[string]interface{}{
				"email": "mapuser@example.com",
			}),
			expected: "mapuser@example.com",
		},
		{
			name: "map without email key returns empty string",
			claims: makeSessionClaims(map[string]interface{}{
				"other_key": "value",
			}),
			expected: "",
		},
		{
			name:     "unknown type returns empty string",
			claims:   makeSessionClaims("unknown-string-type"),
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := auth.ExtractEmailFromClaims(tc.claims)
			assert.Equal(t, tc.expected, got)
		})
	}
}

// --- ExtractSubscriptionTier ---

func TestExtractSubscriptionTier(t *testing.T) {
	tests := []struct {
		name     string
		claims   *clerk.SessionClaims
		expected string
	}{
		{
			name:     "nil claims returns default tier",
			claims:   nil,
			expected: auth.DefaultSubscriptionTier,
		},
		{
			name:     "nil Custom returns default tier",
			claims:   &clerk.SessionClaims{Custom: nil},
			expected: auth.DefaultSubscriptionTier,
		},
		{
			name: "CustomClaims with tier set returns tier",
			claims: makeSessionClaims(&auth.CustomClaims{
				SubscriptionTier: "premium",
			}),
			expected: "premium",
		},
		{
			name: "CustomClaims with empty tier returns default",
			claims: makeSessionClaims(&auth.CustomClaims{
				SubscriptionTier: "",
			}),
			expected: auth.DefaultSubscriptionTier,
		},
		{
			name: "map with subscription_tier key returns tier",
			claims: makeSessionClaims(map[string]interface{}{
				"subscription_tier": "household_small",
			}),
			expected: "household_small",
		},
		{
			name: "map without subscription_tier returns default",
			claims: makeSessionClaims(map[string]interface{}{
				"email": "user@example.com",
			}),
			expected: auth.DefaultSubscriptionTier,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := auth.ExtractSubscriptionTier(tc.claims)
			assert.Equal(t, tc.expected, got)
		})
	}
}

// --- ExtractPrimaryRegion ---

func TestExtractPrimaryRegion(t *testing.T) {
	defaultRegion := auth.DefaultPrimaryRegion()
	require.NotEmpty(t, defaultRegion, "DefaultPrimaryRegion() should return a non-empty string")

	tests := []struct {
		name     string
		claims   *clerk.SessionClaims
		expected string
	}{
		{
			name:     "nil claims returns default region",
			claims:   nil,
			expected: defaultRegion,
		},
		{
			name:     "nil Custom returns default region",
			claims:   &clerk.SessionClaims{Custom: nil},
			expected: defaultRegion,
		},
		{
			name: "CustomClaims with region set returns region",
			claims: makeSessionClaims(&auth.CustomClaims{
				PrimaryRegion: "fra",
			}),
			expected: "fra",
		},
		{
			name: "CustomClaims with empty region returns default",
			claims: makeSessionClaims(&auth.CustomClaims{
				PrimaryRegion: "",
			}),
			expected: defaultRegion,
		},
		{
			name: "map with primary_region key returns region",
			claims: makeSessionClaims(map[string]interface{}{
				"primary_region": "sin",
			}),
			expected: "sin",
		},
		{
			name: "map without primary_region returns default",
			claims: makeSessionClaims(map[string]interface{}{
				"email": "user@example.com",
			}),
			expected: defaultRegion,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := auth.ExtractPrimaryRegion(tc.claims)
			assert.Equal(t, tc.expected, got)
		})
	}
}

// --- CustomClaimsConstructor ---

func TestCustomClaimsConstructor(t *testing.T) {
	ctx := context.Background()
	result := auth.CustomClaimsConstructor(ctx)
	require.NotNil(t, result, "CustomClaimsConstructor should return a non-nil value")

	// Should return a *CustomClaims
	claims, ok := result.(*auth.CustomClaims)
	require.True(t, ok, "CustomClaimsConstructor should return *auth.CustomClaims")
	assert.NotNil(t, claims)
}

// --- DefaultPrimaryRegion ---

func TestDefaultPrimaryRegion(t *testing.T) {
	region := auth.DefaultPrimaryRegion()
	assert.NotEmpty(t, region, "DefaultPrimaryRegion() should return a non-empty string")
}

// --- DefaultSubscriptionTier constant ---

func TestDefaultSubscriptionTier(t *testing.T) {
	assert.Equal(t, "free", auth.DefaultSubscriptionTier)
}
