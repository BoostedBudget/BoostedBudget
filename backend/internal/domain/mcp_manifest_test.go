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
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
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
