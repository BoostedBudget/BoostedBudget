package domain_test

// Issue #1087 GAP-7 / Issue #1610 P3-T6: Go/TS manifest sync check.
//
// mcp_manifest_generated.go (MCPOperationManifest) is GENERATED from
// apps/mcp-server/src/spec/generated/spec-index.json by
// backend/scripts/generate-mcp-manifest/main.go — it is no longer a hand-maintained
// mirror. TestMCPManifest_MatchesGeneratedFromSpecIndex below regenerates the expected
// map from spec-index.json in-memory and asserts full bidirectional equality against
// the checked-in MCPOperationManifest: every entry, in both directions, for every
// class (read/write/destructive/excluded) — not just the read subset a hand-maintained
// mirror's drift test used to check. This is what let read-class operations
// (getMerchants, listMCPRecipes, getMyHousehold, getAccountBalanceHistory, and others)
// go missing from the old hand-maintained map for so long: the old cross-check only
// checked "is every Go read op *also* read in TS" — an op TS has and Go lacks entirely
// was invisible to it, and (separately) a path bug made its loadSpecIndex() always
// t.Skip() in every environment, so it had literally never run. Both of the old test
// functions are removed; the full-equality test below replaces them.
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

// goClassFor mirrors backend/scripts/generate-mcp-manifest/main.go's goClassFor exactly
// (kept as a small duplicate rather than an import — that generator is a `package main`
// and cannot be imported). If the two diverge, TestMCPManifest_MatchesGeneratedFromSpecIndex
// below will not catch that divergence directly, but a stale-manifest failure still
// surfaces via `go run scripts/generate-mcp-manifest/main.go` producing a diff against the
// committed file (see "make validate-mcp-artifacts").
func goClassFor(t *testing.T, e specEntry) domain.MCPOperationClass {
	t.Helper()
	if e.Excluded {
		return domain.MCPClassExcluded
	}
	switch e.ManifestClass {
	case "read":
		return domain.MCPClassRead
	case "write":
		return domain.MCPClassWrite
	case "destructive":
		return domain.MCPClassDestructive
	default:
		t.Fatalf("spec-index entry %q has unknown manifestClass %q", e.OperationID, e.ManifestClass)
		return ""
	}
}

type specIndex struct {
	Entries []specEntry `json:"entries"`
}

func loadSpecIndex(t *testing.T) map[string]specEntry {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	// backend/internal/domain/ → ../../../ = repo root. This was previously "../../../../"
	// (one level too many, landing outside the repo entirely) which made this helper —
	// and every test built on it — silently t.Skip() in every environment, always. That
	// is the deeper reason the Go/TS manifest drift (Issue #1610 P3-T6) survived a
	// "cross-check [that] ALREADY EXISTS": the check had never actually run.
	repoRoot := filepath.Join(filepath.Dir(thisFile), "../../../")
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

// TestMCPManifest_MatchesGeneratedFromSpecIndex is the bidirectional replacement for
// the old one-directional (read-only, Go→TS) drift tests. It regenerates the expected
// manifest from spec-index.json in-memory (mirroring
// backend/scripts/generate-mcp-manifest/main.go's own logic) and requires the checked-in
// MCPOperationManifest to match it EXACTLY: no TS op missing from Go, no Go op absent
// from (or stale relative to) TS, and no op classified differently — across all four
// classes, not just "read". A hand-maintained mirror could drift silently in either
// direction; a generated map can only drift if someone edits the generated file by hand
// without regenerating, or forgets to regenerate after a TS manifest change — which is
// exactly what this test (and "make validate-mcp-artifacts") catches.
func TestMCPManifest_MatchesGeneratedFromSpecIndex(t *testing.T) {
	tsOps := loadSpecIndex(t)

	want := make(map[string]domain.MCPOperationClass, len(tsOps))
	for opID, e := range tsOps {
		want[opID] = goClassFor(t, e)
	}

	got := domain.MCPOperationManifest

	var missingInGo, staleInGo, misclassified []string
	for opID, wantClass := range want {
		gotClass, ok := got[opID]
		if !ok {
			missingInGo = append(missingInGo, opID)
			continue
		}
		if gotClass != wantClass {
			misclassified = append(misclassified, fmt.Sprintf("%s: Go=%q, want %q (from TS spec index)", opID, gotClass, wantClass))
		}
	}
	for opID := range got {
		if _, ok := want[opID]; !ok {
			staleInGo = append(staleInGo, opID)
		}
	}

	sort.Strings(missingInGo)
	sort.Strings(staleInGo)
	sort.Strings(misclassified)

	if len(missingInGo) > 0 {
		t.Errorf("MCPOperationManifest is missing %d op(s) present in the TS spec index: %v", len(missingInGo), missingInGo)
	}
	if len(staleInGo) > 0 {
		t.Errorf("MCPOperationManifest has %d stale op(s) no longer in the TS spec index: %v", len(staleInGo), staleInGo)
	}
	if len(misclassified) > 0 {
		t.Errorf("MCPOperationManifest has %d misclassified op(s):", len(misclassified))
		for _, m := range misclassified {
			t.Errorf("  %s", m)
		}
	}
	if len(missingInGo) > 0 || len(staleInGo) > 0 || len(misclassified) > 0 {
		t.Error("Fix: run 'cd backend && go run scripts/generate-mcp-manifest/main.go' and commit the regenerated mcp_manifest_generated.go")
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

// TestMCPManifest_Issue1300_SavingsProgressEntry verifies the getSavingsProgressSummary
// analytics operation is registered as MCPClassRead (Issue #1300, Codex C11).
func TestMCPManifest_Issue1300_SavingsProgressEntry(t *testing.T) {
	const opID = "getSavingsProgressSummary"

	class, found := domain.MCPOperationManifest[opID]
	if !found {
		t.Fatalf("MCPOperationManifest missing entry for %q (Issue #1300 Codex C11)", opID)
	}
	if class != domain.MCPClassRead {
		t.Errorf("MCPOperationManifest[%q] = %q, want MCPClassRead (Issue #1300)", opID, class)
	}
}
