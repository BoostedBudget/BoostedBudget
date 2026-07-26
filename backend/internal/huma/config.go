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
	config.Info.Description = `BoostedBudget API - Global budgeting platform with user-defined automation rules and GDPR compliance.

## Authentication

All protected endpoints require Clerk JWT Bearer token in the Authorization header:
` + "```" + `
Authorization: Bearer <your-jwt-token>
` + "```" + `

## Subscription Tiers

No free tier. New users get a 60-day Premium trial.

| Tier        | Users | Transaction History | Price     |
|-------------|-------|---------------------|-----------|
| premium     | 1     | 730 days            | $8.99/mo  |
| household_2 | 2     | 730 days            | $12.99/mo |
| household_3 | 3     | 730 days            | $14.99/mo |
| household_4 | 4     | 730 days            | $16.99/mo |
| household_5 | 5+    | 730 days            | $18.99/mo |

## Regions

Use the GET /api/v1/regions endpoint to discover available data residency regions.
Multi-region support planned for Phase 7+.
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
