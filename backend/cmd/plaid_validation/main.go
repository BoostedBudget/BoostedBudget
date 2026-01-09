// Package main provides the Plaid staging validation CLI.
// Use this tool to verify Plaid integration health in staging environments.
//
// Usage:
//
//	go run ./cmd/plaid_validation/... [flags] [scenarios...]
//
// Flags:
//
//	--verbose   Enable detailed output
//	--json      Output results as JSON
//	--timeout   Timeout per scenario (default: 30s)
//
// Scenarios:
//
//	all                  Run all validators (default)
//	connectivity         Verify Plaid API authentication
//	link-token           Test link token creation
//	sync-initial         Test initial transaction sync
//	sync-incremental     Test incremental sync with cursor
//	sync-pagination      Test multi-page pagination
//	error-classification Test error handling
//	webhook-fire         Fire test webhook (sandbox only)
//
// Examples:
//
//	go run ./cmd/plaid_validation/...                         # Run all
//	go run ./cmd/plaid_validation/... connectivity            # Run one
//	go run ./cmd/plaid_validation/... --verbose all           # Verbose
//	go run ./cmd/plaid_validation/... --json all > results.json
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/maxnussbaum/BoostedBudget/cmd/plaid_validation/validators"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

func main() {
	// Parse flags
	verbose := flag.Bool("verbose", false, "Enable detailed output")
	jsonOutput := flag.Bool("json", false, "Output results as JSON")
	timeout := flag.Duration("timeout", 30*time.Second, "Timeout per scenario")
	flag.Parse()

	// Get scenarios from remaining args
	scenarios := flag.Args()
	if len(scenarios) == 0 {
		scenarios = []string{"all"}
	}

	// Create context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nInterrupted, cleaning up...")
		cancel()
	}()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		fmt.Fprintf(os.Stderr, "Ensure PLAID_CLIENT_ID, PLAID_SECRET, and PLAID_ENV are set.\n")
		os.Exit(1)
	}

	// Create Plaid service
	plaidService, err := services.NewPlaidService(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create Plaid service: %v\n", err)
		os.Exit(1)
	}

	// Create validation options
	opts := &validators.ValidationOptions{
		Verbose:      *verbose,
		JSONOutput:   *jsonOutput,
		Timeout:      *timeout,
		Config:       cfg,
		PlaidService: plaidService,
	}

	// Print header
	if !*jsonOutput {
		fmt.Println("Plaid Staging Validation")
		fmt.Printf("Environment: %s\n", cfg.PlaidEnv)
		fmt.Printf("Scenarios: %v\n", scenarios)
		fmt.Println()
	}

	// Run validators
	results := validators.RunValidations(ctx, scenarios, opts)

	// Output results
	exitCode := validators.PrintResults(results, opts)
	os.Exit(exitCode)
}
