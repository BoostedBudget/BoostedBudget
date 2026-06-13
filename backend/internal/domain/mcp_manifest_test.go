package domain_test

// Issue #1087 GAP-7: Go/TS manifest sync check
// Verifies that every operation in MCPOperationManifest that is classified as
// MCPClassRead is also classified as "read" in the TypeScript spec index
// (apps/mcp-server/src/spec/generated/spec-index.json).
//
// This catches the critical drift scenario: an op accidentally marked as
// MCPClassRead in the Go manifest but as "write" (or excluded) in the TS manifest,
// which would allow write recipes to pass Go validation but be denied by the Worker.
//
// The spec-index.json is generated from the TS capability manifest and is
// CI-verified for drift. Using it as the source of truth for cross-language sync.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"gopkg.in/yaml.v3"
)

type specEntry struct {
	OperationID   string `json:"operationId"`
	ManifestClass string `json:"manifestClass"`
	Excluded      bool   `json:"excluded"`
}

type specIndex struct {
	Entries []specEntry `json:"entries"`
}

func loadSpecIndex(t *testing.T) map[string]specEntry {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "../../../../")
	specPath := filepath.Join(repoRoot, "apps/mcp-server/src/spec/generated/spec-index.json")

	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Skipf("spec-index.json not found at %s — run 'bun run src/spec/index-builder.ts' to generate: %v", specPath, err)
	}

	var idx specIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("failed to parse spec-index.json: %v", err)
	}

	byID := make(map[string]specEntry, len(idx.Entries))
	for _, e := range idx.Entries {
		byID[e.OperationID] = e
	}
	return byID
}

func TestMCPManifest_GoReadOpsAreReadInTSManifest(t *testing.T) {
	tsOps := loadSpecIndex(t)

	failures := []string{}

	for opID, class := range domain.MCPOperationManifest {
		if class != domain.MCPClassRead {
			continue // Only checking read ops — writes/excluded don't need TS sync
		}

		tsEntry, found := tsOps[opID]
		if !found {
			failures = append(failures, opID+": Go marks as read, but NOT in TS spec index")
			continue
		}

		if tsEntry.Excluded {
			failures = append(failures, opID+": Go marks as read, but TS marks as excluded")
			continue
		}

		if tsEntry.ManifestClass != "read" {
			failures = append(failures, opID+": Go marks as read, but TS marks as "+tsEntry.ManifestClass)
		}
	}

	if len(failures) > 0 {
		t.Errorf("Go/TS manifest drift detected — %d operations disagree:", len(failures))
		for _, f := range failures {
			t.Errorf("  %s", f)
		}
		t.Error("Fix: update MCPOperationManifest in backend/internal/domain/mcp_manifest.go")
	}
}

func TestMCPManifest_MCPOperationIsReadMatchesTSManifest(t *testing.T) {
	// Verify that MCPOperationIsRead() returns true only for ops that are
	// also "read" in the TS spec index.
	// The Go manifest intentionally includes write ops (classified MCPClassWrite);
	// the recipe service rejects them via MCPOperationIsRead().
	// This test catches the dangerous drift: an op marked read in Go but write/excluded in TS.
	tsOps := loadSpecIndex(t)

	for opID := range domain.MCPOperationManifest {
		if !domain.MCPOperationIsRead(opID) {
			continue // write/excluded ops in Go are correctly rejected by recipe service
		}

		tsEntry, found := tsOps[opID]
		if !found {
			t.Errorf("%s: MCPOperationIsRead()==true in Go, but op not in TS spec index", opID)
			continue
		}

		if tsEntry.Excluded || tsEntry.ManifestClass != "read" {
			t.Errorf("%s: MCPOperationIsRead()==true in Go, but TS classifies as %q (excluded=%v)",
				opID, tsEntry.ManifestClass, tsEntry.Excluded)
		}
	}
}

// openAPISpec is a minimal struct for parsing the paths section of openapi.yaml.
type openAPISpec struct {
	Paths map[string]map[string]openAPIOperation `yaml:"paths"`
}

// openAPIOperation represents a single HTTP operation in the spec.
type openAPIOperation struct {
	OperationID string   `yaml:"operationId"`
	Tags        []string `yaml:"tags"`
}

// loadOpenAPIAnalyticsOps parses api/openapi.yaml and returns all operationIds
// that have the "Analytics" tag.
func loadOpenAPIAnalyticsOps(t *testing.T) []string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	// backend/internal/domain/ → ../../../ = repo root
	repoRoot := filepath.Join(filepath.Dir(thisFile), "../../../")
	specPath := filepath.Join(repoRoot, "api/openapi.yaml")

	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("failed to read api/openapi.yaml at %s: %v", specPath, err)
	}

	var spec openAPISpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		t.Fatalf("failed to parse api/openapi.yaml: %v", err)
	}

	httpMethods := []string{"get", "post", "put", "patch", "delete", "head", "options"}
	var ops []string
	for _, pathItem := range spec.Paths {
		for _, method := range httpMethods {
			op, ok := pathItem[method]
			if !ok {
				continue
			}
			for _, tag := range op.Tags {
				if tag == "Analytics" {
					ops = append(ops, op.OperationID)
					break
				}
			}
		}
	}
	sort.Strings(ops)
	return ops
}

// TestMCPManifest_AnalyticsOpsAreInGoManifest ensures that every operation
// tagged "Analytics" in api/openapi.yaml is classified as MCPClassRead in
// backend/internal/domain/mcp_manifest.go. This catches the common mistake
// of adding a Huma Analytics endpoint but forgetting to update mcp_manifest.go.
func TestMCPManifest_AnalyticsOpsAreInGoManifest(t *testing.T) {
	analyticsOps := loadOpenAPIAnalyticsOps(t)

	if len(analyticsOps) == 0 {
		t.Fatal("no Analytics-tagged operations found in api/openapi.yaml — check the YAML parser")
	}

	var missing []string
	var misclassified []string

	for _, opID := range analyticsOps {
		class, found := domain.MCPOperationManifest[opID]
		if !found {
			missing = append(missing, opID)
			continue
		}
		if class != domain.MCPClassRead {
			misclassified = append(misclassified, fmt.Sprintf("%s (classified as %q, want %q)", opID, class, domain.MCPClassRead))
		}
	}

	if len(missing) > 0 || len(misclassified) > 0 {
		var msgs []string
		for _, id := range missing {
			msgs = append(msgs, "  MISSING: "+id)
		}
		for _, desc := range misclassified {
			msgs = append(msgs, "  MISCLASSIFIED: "+desc)
		}
		t.Errorf(
			"Analytics ops from api/openapi.yaml are not correctly classified as MCPClassRead in mcp_manifest.go:\n%s\n"+
				"Fix: add/update these entries in backend/internal/domain/mcp_manifest.go",
			strings.Join(msgs, "\n"),
		)
	}
}

// TestMCPManifest_Issue972_SavingsTransferEntries verifies that the four new
// Issue #972 operation entries are present with the correct class.
func TestMCPManifest_Issue972_SavingsTransferEntries(t *testing.T) {
	type wantEntry struct {
		operationID string
		wantClass   domain.MCPOperationClass
	}

	want := []wantEntry{
		{"getSavingsImpactAggregate", domain.MCPClassRead},
		{"getSavingsImpactMe", domain.MCPClassRead},
		{"confirmTransferSuggestionMatch", domain.MCPClassWrite},
		{"declineTransferSuggestionMatch", domain.MCPClassWrite}, // GAP CJ: was missing from test
	}

	for _, w := range want {
		class, found := domain.MCPOperationManifest[w.operationID]
		if !found {
			t.Errorf("MCPOperationManifest missing entry for %q (Issue #972)", w.operationID)
			continue
		}
		if class != w.wantClass {
			t.Errorf("MCPOperationManifest[%q] = %q, want %q (Issue #972)", w.operationID, class, w.wantClass)
		}
	}
}

// TestMCPManifest_Issue1210_ReviewWorkflowEntries verifies that the new Issue #1210
// operation entries are present with the correct class so a future rename can't
// silently drop them.
func TestMCPManifest_Issue1210_ReviewWorkflowEntries(t *testing.T) {
	type wantEntry struct {
		operationID string
		wantClass   domain.MCPOperationClass
	}

	want := []wantEntry{
		{"getTransactionReviewSummary", domain.MCPClassRead},
		{"bulkUpdateTransactionCategory", domain.MCPClassWrite},
	}

	for _, w := range want {
		class, found := domain.MCPOperationManifest[w.operationID]
		if !found {
			t.Errorf("MCPOperationManifest missing entry for %q (Issue #1210)", w.operationID)
			continue
		}
		if class != w.wantClass {
			t.Errorf("MCPOperationManifest[%q] = %q, want %q (Issue #1210)", w.operationID, class, w.wantClass)
		}
	}
}

// TestMCPManifest_Issue1267_CancelTransferSuggestion verifies that
// cancelTransferSuggestion is classified as MCPClassWrite. This mutation allows
// the MCP server to cancel an accepted suggestion, transitioning it to rejected.
func TestMCPManifest_Issue1267_CancelTransferSuggestion(t *testing.T) {
	const opID = "cancelTransferSuggestion"
	class, found := domain.MCPOperationManifest[opID]
	if !found {
		t.Errorf("MCPOperationManifest missing entry for %q (Issue #1267 gap R2-F)", opID)
		return
	}
	if class != domain.MCPClassWrite {
		t.Errorf("MCPOperationManifest[%q] = %q, want MCPClassWrite (Issue #1267 gap R2-F)", opID, class)
	}
}

// TestMCPManifest_Issue138InvestmentOps verifies that all 8 Issue #138 investment
// portfolio operations are present in the Go manifest as MCPClassRead.
// Also asserts getTransactions is still read (unified-feed shape change is MCP-safe).
// The 4 analytics helpers are tagged "Analytics" in the Huma spec — this test guards
// against accidental removal from the Go manifest.
func TestMCPManifest_Issue138InvestmentOps(t *testing.T) {
	wantRead := []string{
		// Core investment operations (tag: Investments)
		"getHoldings",
		"getPortfolioSummary",
		"getPortfolioHistory",
		"getInvestmentTransactions",
		// Analytics helpers (tag: Analytics — load-bearing for MCP parity)
		"getPortfolioAllocationBreakdown",
		"getTopMovers",
		"getInvestmentGainLossSummary",
		"getInvestmentIncomeSummary",
		// Unified feed — response shape changed to ActivityItem, but op remains read.
		"getTransactions",
	}

	for _, opID := range wantRead {
		class, found := domain.MCPOperationManifest[opID]
		if !found {
			t.Errorf("MCPOperationManifest missing entry for %q (Issue #138)", opID)
			continue
		}
		if class != domain.MCPClassRead {
			t.Errorf("MCPOperationManifest[%q] = %q, want MCPClassRead (Issue #138)", opID, class)
		}
	}
}
