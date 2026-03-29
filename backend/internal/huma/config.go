// Package huma provides Huma API configuration and utilities for OpenAPI generation.
package huma

import (
	"github.com/danielgtaylor/huma/v2"
)

// NewAPIConfig creates the Huma API configuration for BoostedBudget.
// It configures API metadata, servers, and documentation endpoints.
func NewAPIConfig(version, environment string) huma.Config {
	config := huma.DefaultConfig("BoostedBudget API", version)

	// Configure servers based on environment
	config.Servers = []*huma.Server{
		{URL: "https://api.boostedbudget.com", Description: "Production (iad/us-east-1)"},
		{URL: "http://localhost:8080", Description: "QA / Local development"},
	}

	// API description with subscription tier info
	config.Info.Description = `BoostedBudget API - Global budgeting platform with intelligent automation rules and GDPR compliance.

## Authentication

All protected endpoints require Clerk JWT Bearer token in the Authorization header:
` + "```" + `
Authorization: Bearer <your-jwt-token>
` + "```" + `

## Subscription Tiers

| Tier | Rules | Accounts | Sync Frequency | Price |
|------|-------|----------|----------------|-------|
| free | 1 | 3 | 1x/day | $0 (ad-supported) |
| premium_lite | 5 | 15 | 2x/day | $4.99/mo |
| premium | 15 | 30 | 6x/day | $9.99/mo |
| premium_plus | Unlimited | Unlimited | every 10min | $19.99/mo |

## Regions

All data is stored in the us-east-1 (iad) region. Multi-region support planned for Phase 7+.
`

	config.Info.Contact = &huma.Contact{
		Name:  "BoostedBudget Support",
		Email: "support@boostedbudget.com",
		URL:   "https://boostedbudget.com/support",
	}

	config.Info.License = &huma.License{
		Name: "Proprietary",
		URL:  "https://boostedbudget.com/terms",
	}

	// Configure documentation endpoint
	config.DocsPath = "/docs"

	// Add security scheme for Clerk
	config.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"clerk": {
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "JWT",
			Description:  "Clerk JWT Bearer token. Obtain from Clerk after authentication.",
		},
	}

	return config
}
