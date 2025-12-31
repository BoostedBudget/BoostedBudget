// Package auth provides shared authentication utilities for Clerk integration.
package auth

import (
	"github.com/clerk/clerk-sdk-go/v2"
)

// Default values for subscription and region
const (
	// DefaultSubscriptionTier is the default tier for new users
	DefaultSubscriptionTier = "free"
	// DefaultPrimaryRegion is the default region (IAD = us-east-1)
	DefaultPrimaryRegion = "iad"
)

// ExtractEmailFromClaims extracts email from Clerk session claims.
// Clerk includes custom claims in the Custom field if configured via JWT templates.
// Returns empty string if email is not found or claims are nil.
func ExtractEmailFromClaims(claims *clerk.SessionClaims) string {
	if claims == nil || claims.Custom == nil {
		return ""
	}

	// Custom claims are stored as a map
	customClaims, ok := claims.Custom.(map[string]interface{})
	if !ok {
		return ""
	}

	// Try to get email from custom claims
	if email, ok := customClaims["email"].(string); ok && email != "" {
		return email
	}

	return ""
}

// ExtractSubscriptionTier extracts the subscription tier from Clerk claims.
// Returns DefaultSubscriptionTier ("free") if not found.
// The tier should be set in the user's public metadata in Clerk.
func ExtractSubscriptionTier(claims *clerk.SessionClaims) string {
	if claims == nil || claims.Custom == nil {
		return DefaultSubscriptionTier
	}

	customClaims, ok := claims.Custom.(map[string]interface{})
	if !ok {
		return DefaultSubscriptionTier
	}

	tier, ok := customClaims["subscription_tier"].(string)
	if !ok || tier == "" {
		return DefaultSubscriptionTier
	}

	return tier
}

// ExtractPrimaryRegion extracts the primary region from Clerk claims.
// Returns DefaultPrimaryRegion ("iad") if not found.
// The region should be set in the user's public metadata in Clerk.
func ExtractPrimaryRegion(claims *clerk.SessionClaims) string {
	if claims == nil || claims.Custom == nil {
		return DefaultPrimaryRegion
	}

	customClaims, ok := claims.Custom.(map[string]interface{})
	if !ok {
		return DefaultPrimaryRegion
	}

	region, ok := customClaims["primary_region"].(string)
	if !ok || region == "" {
		return DefaultPrimaryRegion
	}

	return region
}
