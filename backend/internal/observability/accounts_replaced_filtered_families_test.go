package observability

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Guard for Issue #1567.
//
// The AccountsReplacedFiltered doc comment in metrics.go enumerates the families its
// "family" label may carry. That list drifted: two families (transaction_match,
// balance_summary) were documented but could never emit, and two that DO emit
// (investments_with_securities, investments_keyset) were missing from the list. This
// test makes the doc comment and the code disagree loudly instead of silently, in both
// directions:
//
//   - every family named in the doc comment has >= 1 WithLabelValues call in backend/
//   - every WithLabelValues label emitted on this metric appears in the doc comment
//
// The doc comment itself is the input, so editing the comment is what the guard reads.
// Both the parse and the scan fail closed: zero families parsed or zero call sites found
// is treated as a failure, never as a vacuous pass.

// familyListHeader marks the start of the parsed block in the metrics.go doc comment.
const familyListHeader = "Emitted families"

// docFamilyLine matches one entry of the family list, e.g.
//
//	//   accounts_list  - GetAccountsWithInstitutionFiltered (active-only path)
var docFamilyLine = regexp.MustCompile(`^\s*//\s{2,}([a-z][a-z0-9_]*)\s+-\s+\S`)

// backendRoot resolves the backend module root from this package's directory.
func backendRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving backend root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("backend root %q does not look like the Go module root: %v", root, err)
	}
	return root
}

// parseDocumentedFamilies extracts the family names from the AccountsReplacedFiltered
// doc comment in metrics.go. It consumes the contiguous run of list lines that follows
// the familyListHeader line and stops at the first line that is not a list entry, so
// later blocks in the same comment (e.g. "Removed families, and why") are not picked up.
func parseDocumentedFamilies(t *testing.T, metricsPath string) []string {
	t.Helper()

	data, err := os.ReadFile(metricsPath)
	if err != nil {
		t.Fatalf("reading %s: %v", metricsPath, err)
	}
	lines := strings.Split(string(data), "\n")

	headerIdx := -1
	for i, line := range lines {
		if strings.Contains(line, "//") && strings.Contains(line, familyListHeader) {
			headerIdx = i
			break
		}
	}
	if headerIdx == -1 {
		t.Fatalf("could not find %q header in %s — the doc comment this guard parses was "+
			"renamed or removed; update familyListHeader (fail-closed: refusing to pass vacuously)",
			familyListHeader, metricsPath)
	}

	var families []string
	for i := headerIdx + 1; i < len(lines); i++ {
		m := docFamilyLine.FindStringSubmatch(lines[i])
		if m == nil {
			break
		}
		families = append(families, m[1])
	}

	if len(families) == 0 {
		t.Fatalf("parsed zero families from the %q list in %s — the list format changed and "+
			"this guard would otherwise pass vacuously", familyListHeader, metricsPath)
	}
	return families
}

// scanEmittedFamilies walks backend/**/*.go and collects every label literal passed to
// AccountsReplacedFiltered.WithLabelValues, mapped to the files it was found in.
func scanEmittedFamilies(t *testing.T, root string) map[string][]string {
	t.Helper()

	// Built by concatenation so this test file's own source cannot match the pattern
	// it searches for (which would make the scan self-satisfying).
	callSite := regexp.MustCompile(
		`AccountsReplaced` + `Filtered\.With` + `LabelValues\(\s*"([^"]*)"\s*\)`)

	emitted := make(map[string][]string)
	filesScanned := 0

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Skip vendored/tooling trees; they never emit application metrics.
			switch info.Name() {
			case "vendor", "node_modules", ".git":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		filesScanned++
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		for _, m := range callSite.FindAllStringSubmatch(string(data), -1) {
			emitted[m[1]] = append(emitted[m[1]], rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}

	if filesScanned == 0 {
		t.Fatalf("scanned zero .go files under %s — the scan matched nothing, which is a "+
			"guard failure, not a pass", root)
	}
	if len(emitted) == 0 {
		t.Fatalf("found zero AccountsReplacedFiltered.WithLabelValues call sites in %d .go "+
			"files under %s — either the metric lost all its call sites or the scan pattern "+
			"is stale; failing closed rather than passing vacuously", filesScanned, root)
	}
	return emitted
}

func TestAccountsReplacedFilteredFamiliesMatchCallSites(t *testing.T) {
	root := backendRoot(t)
	metricsPath := filepath.Join(root, "internal", "observability", "metrics.go")

	documented := parseDocumentedFamilies(t, metricsPath)
	emitted := scanEmittedFamilies(t, root)

	documentedSet := make(map[string]bool, len(documented))
	for _, f := range documented {
		if documentedSet[f] {
			t.Errorf("family %q is listed twice in the metrics.go doc comment", f)
		}
		documentedSet[f] = true
	}

	// Direction 1: documented -> emitted.
	for _, f := range documented {
		if len(emitted[f]) == 0 {
			t.Errorf("family %q is documented in %s but no "+
				"AccountsReplacedFiltered.WithLabelValues(%q) call exists anywhere in backend/. "+
				"Either instrument the call site or remove the family from the doc comment "+
				"(recording why, as Issue #1567 did for transaction_match and balance_summary).",
				f, metricsPath, f)
		}
	}

	// Direction 2: emitted -> documented.
	for f, files := range emitted {
		if !documentedSet[f] {
			t.Errorf("family %q is emitted from %v but is not listed under %q in %s. "+
				"Add it to the doc comment so the label set stays discoverable.",
				f, files, familyListHeader, metricsPath)
		}
	}

	if t.Failed() {
		t.Logf("documented families: %v", documented)
		emittedNames := make([]string, 0, len(emitted))
		for f := range emitted {
			emittedNames = append(emittedNames, f)
		}
		t.Logf("emitted families: %v", emittedNames)
	}
}
