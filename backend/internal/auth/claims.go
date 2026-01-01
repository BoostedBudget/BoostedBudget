// Package auth provides shared authentication utilities for Clerk integration.
package auth

import (
	"context"

	"github.com/clerk/clerk-sdk-go/v2"
)

// Default values for subscription and region
const (
	// DefaultSubscriptionTier is the default tier for new users
	DefaultSubscriptionTier = "free"
	// DefaultPrimaryRegion is the default region (IAD = us-east-1)
	DefaultPrimaryRegion = "iad"
)

// CustomClaims represents the custom claims from BoostedBudget JWT template.
// This struct must match the JWT template configured in Clerk Dashboard.
//
// JWT Template format:
//
//	{
//	  "email": "{{user.primary_email_address}}",
//	  "email_verified": "{{user.email_verified}}",
//	  "subscription_tier": "{{user.public_metadata.subscription_tier}}",
//	  "primary_region": "{{user.public_metadata.primary_region}}"
//	}
type CustomClaims struct {
	Email            string `json:"email"`
	EmailVerified    bool   `json:"email_verified"`
	SubscriptionTier string `json:"subscription_tier"`
	PrimaryRegion    string `json:"primary_region"`
}

// CustomClaimsConstructor returns a new CustomClaims instance for JWT parsing.
// This function is passed to jwt.VerifyParams to enable parsing of custom JWT template claims.
func CustomClaimsConstructor(_ context.Context) any {
	return &CustomClaims{}
}

// ExtractEmailFromClaims extracts email from Clerk session claims.
// Clerk includes custom claims in the Custom field if configured via JWT templates.
// Returns empty string if email is not found or claims are nil.
//
// Supports two claim formats:
//   - *CustomClaims (production with CustomClaimsConstructor)
//   - map[string]interface{} (tests, legacy)
func ExtractEmailFromClaims(claims *clerk.SessionClaims) string {
	if claims == nil || claims.Custom == nil {
		return ""
	}

	// Try typed CustomClaims first (production with CustomClaimsConstructor)
	if customClaims, ok := claims.Custom.(*CustomClaims); ok {
		return customClaims.Email
	}

	// Fallback: handle map[string]interface{} (tests, legacy)
	if customClaims, ok := claims.Custom.(map[string]interface{}); ok {
		if email, ok := customClaims["email"].(string); ok && email != "" {
			return email
		}
	}

	return ""
}

// ExtractSubscriptionTier extracts the subscription tier from Clerk claims.
// Returns DefaultSubscriptionTier ("free") if not found.
// The tier should be set in the user's public metadata in Clerk.
//
// Supports two claim formats:
//   - *CustomClaims (production with CustomClaimsConstructor)
//   - map[string]interface{} (tests, legacy)
func ExtractSubscriptionTier(claims *clerk.SessionClaims) string {
	if claims == nil || claims.Custom == nil {
		return DefaultSubscriptionTier
	}

	// Try typed CustomClaims first (production with CustomClaimsConstructor)
	if customClaims, ok := claims.Custom.(*CustomClaims); ok {
		if customClaims.SubscriptionTier != "" {
			return customClaims.SubscriptionTier
		}
		return DefaultSubscriptionTier
	}

	// Fallback: handle map[string]interface{} (tests, legacy)
	if customClaims, ok := claims.Custom.(map[string]interface{}); ok {
		if tier, ok := customClaims["subscription_tier"].(string); ok && tier != "" {
			return tier
		}
	}

	return DefaultSubscriptionTier
}

// ExtractPrimaryRegion extracts the primary region from Clerk claims.
// Returns DefaultPrimaryRegion ("iad") if not found.
// The region should be set in the user's public metadata in Clerk.
//
// Supports two claim formats:
//   - *CustomClaims (production with CustomClaimsConstructor)
//   - map[string]interface{} (tests, legacy)
func ExtractPrimaryRegion(claims *clerk.SessionClaims) string {
	if claims == nil || claims.Custom == nil {
		return DefaultPrimaryRegion
	}

	// Try typed CustomClaims first (production with CustomClaimsConstructor)
	if customClaims, ok := claims.Custom.(*CustomClaims); ok {
		if customClaims.PrimaryRegion != "" {
			return customClaims.PrimaryRegion
		}
		return DefaultPrimaryRegion
	}

	// Fallback: handle map[string]interface{} (tests, legacy)
	if customClaims, ok := claims.Custom.(map[string]interface{}); ok {
		if region, ok := customClaims["primary_region"].(string); ok && region != "" {
			return region
		}
	}

	return DefaultPrimaryRegion
}
