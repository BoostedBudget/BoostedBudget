package repository

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/pfc"
)

// Issue #1609 Item 11 — the OpenAPI `enum` on custom_categories.parent_pfc_primary, the Go
// validation in domain.CustomCategory.Validate, and the DB CHECK are three statements of ONE set,
// and only the DB CHECK is authoritative at write time. The first two are both derived from
// pfc.PrimaryEnums(), so they cannot disagree with each other; this guard is what stops THAT set
// from drifting away from the CHECK migration 000065 actually installed.
//
// It reads the migration text, not the live database, so it runs without Postgres — the value here
// is catching a PFCv3 taxonomy refresh that adds a primary to the Go registry and forgets the
// migration (the API would then advertise a value every write rejects), or the reverse.
func TestPFCPrimaryEnumsMatchTheCustomCategoryParentCheck(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(backendDirRoot(t), "migrations", "000065_pfc_v2_canonical.up.sql"))
	if err != nil {
		t.Fatalf("reading migration 000065: %v", err)
	}

	// The CHECK body: `CHECK (parent_pfc_primary IS NULL OR parent_pfc_primary IN ( ... ))`.
	body := regexp.MustCompile(`(?s)CHECK\s*\(parent_pfc_primary IS NULL OR parent_pfc_primary IN \((.*?)\)\)`).
		FindStringSubmatch(string(raw))
	if body == nil {
		t.Fatal("could not locate the valid_custom_category_parent_pfc CHECK in migration 000065 — " +
			"if the constraint was renamed or re-issued elsewhere, re-point this guard rather than deleting it")
	}

	fromCheck := regexp.MustCompile(`'([A-Z_]+)'`).FindAllStringSubmatch(body[1], -1)
	got := make([]string, 0, len(fromCheck))
	for _, m := range fromCheck {
		got = append(got, m[1])
	}
	sort.Strings(got)

	want := pfc.PrimaryEnums()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the PFC primary set has drifted.\n  migration 000065 CHECK: %v\n  pfc.PrimaryEnums():     %v\n"+
			"domain.CustomCategory.Validate and the OpenAPI enum both derive from pfc.PrimaryEnums(), "+
			"so a value in one list and not the other is either advertised-but-unwritable or writable-but-unadvertised.",
			got, want)
	}
}
