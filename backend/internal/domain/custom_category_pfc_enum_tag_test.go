package domain

import (
	"reflect"
	"strings"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/pfc"
)

// Issue #1609 Item 11 — the OpenAPI schema must advertise which values parent_pfc_primary accepts,
// so `search()` and every generated client can offer them instead of round-tripping an invalid one
// to the database. Huma reads that list from a STATIC `enum:` struct tag, which is exactly the kind
// of hand-maintained list that goes stale the day the PFC taxonomy is refreshed. This pins the tag
// to pfc.PrimaryEnums(), which TestPFCPrimaryEnumsMatchTheCustomCategoryParentCheck in turn pins to
// migration 000065's CHECK — so the advertised set, the Go validation and the DB constraint are one
// set with two mechanical joints rather than three independent lists.
//
// THE REQUEST DTOs ALSO ADMIT "". That is not sloppiness: the category form sends the empty string
// for whichever parent dimension is unused (see the "form sends '' for the unused dimension" case
// in custom_category_service_test.go), and the service normalises it to NULL via emptyStringToNil.
// Omitting "" from the request enum would make Huma reject that request at the boundary, before the
// service ever saw it. The RESPONSE type carries no "": the column is NULL or a real primary, never
// the empty string — the DB CHECK forbids it.
func TestParentPFCPrimaryEnumTagsMatchTheRegistry(t *testing.T) {
	primaries := strings.Join(pfc.PrimaryEnums(), ",")

	cases := []struct {
		what string
		typ  reflect.Type
		want string
	}{
		{"CustomCategory (response)", reflect.TypeOf(CustomCategory{}), primaries},
		{"CreateCustomCategoryRequest", reflect.TypeOf(CreateCustomCategoryRequest{}), primaries + ","},
		{"UpdateCustomCategoryRequest", reflect.TypeOf(UpdateCustomCategoryRequest{}), primaries + ","},
	}

	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			f, ok := c.typ.FieldByName("ParentPFCPrimary")
			if !ok {
				t.Fatalf("%s has no ParentPFCPrimary field", c.what)
			}
			got := f.Tag.Get("enum")
			if got != c.want {
				t.Errorf("%s.ParentPFCPrimary enum tag drifted.\n got: %q\nwant: %q\n"+
					"Re-derive from pfc.PrimaryEnums(); the trailing comma on the request types is the "+
					"empty string, which clears the field.", c.what, got, c.want)
			}
			if f.Tag.Get("doc") == "" {
				t.Errorf("%s.ParentPFCPrimary needs a doc tag — an enum with no explanation of what "+
					"nesting under TRANSFER_IN now DOES (it makes the transactions transfers) is how "+
					"this field got misread as a cosmetic label in the first place.", c.what)
			}
		})
	}
}
