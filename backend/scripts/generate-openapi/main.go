// Package main generates the OpenAPI spec from Huma route definitions.
// This is used to export the spec to api/openapi.yaml for version control and CI validation.
//
// Usage: go run scripts/generate-openapi/main.go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/labstack/echo/v4"
	humaconfig "github.com/maxnussbaum/BoostedBudget/internal/huma"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/operations"
)

func main() {
	// Create Echo instance (not started, just for Huma adapter)
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// Initialize Huma API with production-like config
	api := humaecho.New(e, humaconfig.NewAPIConfig("1.0.0", "production"))

	// Register all routes (with dummy handlers, for spec generation only)
	operations.RegisterAllRoutesForSpec(api)

	// Generate YAML spec
	yamlBytes, err := api.OpenAPI().YAML()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating OpenAPI spec: %v\n", err)
		os.Exit(1)
	}

	// Determine output path (relative to backend directory)
	// When run from backend/, output to ../api/openapi.yaml
	outputPath := filepath.Join("..", "api", "openapi.yaml")

	// Ensure api/ directory exists
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output directory: %v\n", err)
		os.Exit(1)
	}

	// Write spec to file
	if err := os.WriteFile(outputPath, yamlBytes, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing OpenAPI spec: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Generated OpenAPI spec: %s\n", outputPath)
}
