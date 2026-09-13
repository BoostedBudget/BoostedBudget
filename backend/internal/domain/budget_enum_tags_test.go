package domain

// Issue #1630, Task 8: IncomeSource.Type and BudgetCategory/CreateBudgetCategoryRequest/
// UpdateBudgetCategoryRequest.MatchLogic were published in api/openapi.yaml as bare
// `type: string`, with no `enum`. The legal values (pfc_primary/tag/manual;
// category_only/tag_only/and/or) existed only in Go const declarations and Validate()'s error
// messages — undiscoverable from the published contract itself, which is exactly what left the
// issue's reporter guessing "category" and "custom" for income source type. The fix is the Go
// struct tag (Huma reads `enum:`), not the MCP spec-index builder, which already surfaces `enum`
// correctly when the spec has one — mirrors custom_category_pfc_enum_tag_test.go's pattern.
import (
	"reflect"
	"testing"
)

func TestIncomeSourceTypeEnumTagPublishesLegalValues(t *testing.T) {
	f, ok := reflect.TypeOf(IncomeSource{}).FieldByName("Type")
	if !ok {
		t.Fatal("IncomeSource has no Type field")
	}
	const want = "pfc_primary,tag,manual"
	if got := f.Tag.Get("enum"); got != want {
		t.Errorf("IncomeSource.Type enum tag drifted.\n got: %q\nwant: %q", got, want)
	}
}

func TestMatchLogicEnumTagsPublishLegalValues(t *testing.T) {
	const want = "category_only,tag_only,and,or"

	cases := []struct {
		what string
		typ  reflect.Type
	}{
		{"BudgetCategory (response)", reflect.TypeOf(BudgetCategory{})},
		{"CreateBudgetCategoryRequest", reflect.TypeOf(CreateBudgetCategoryRequest{})},
		{"UpdateBudgetCategoryRequest", reflect.TypeOf(UpdateBudgetCategoryRequest{})},
		{"CategoryPreviewRequest", reflect.TypeOf(CategoryPreviewRequest{})},
	}

	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			f, ok := c.typ.FieldByName("MatchLogic")
			if !ok {
				t.Fatalf("%s has no MatchLogic field", c.what)
			}
			if got := f.Tag.Get("enum"); got != want {
				t.Errorf("%s.MatchLogic enum tag drifted.\n got: %q\nwant: %q", c.what, got, want)
			}
		})
	}
}

func TestCategoryKindEnumTagsPublishLegalValues(t *testing.T) {
	cases := []struct {
		what string
		typ  reflect.Type
		want string
	}{
		{"BudgetCategory (response)", reflect.TypeOf(BudgetCategory{}), "pfc,custom,savings,group,remainder"},
		{"CreateBudgetCategoryRequest", reflect.TypeOf(CreateBudgetCategoryRequest{}), "pfc,custom,savings,group,remainder"},
		{"CategoryPreviewRequest", reflect.TypeOf(CategoryPreviewRequest{}), "pfc,custom,group,remainder"},
	}

	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			f, ok := c.typ.FieldByName("CategoryKind")
			if !ok {
				t.Fatalf("%s has no CategoryKind field", c.what)
			}
			if got := f.Tag.Get("enum"); got != c.want {
				t.Errorf("%s.CategoryKind enum tag drifted.\n got: %q\nwant: %q", c.what, got, c.want)
			}
		})
	}
}

// TestMatchLogicWritableFieldsDocumentTheTagsRequirement pins Task 8's second requirement: the
// "and"/"tag_only" -> non-empty tags requirement (Issue #1630 task 0's ACTUAL mechanism behind
// the issue's "and -> 400" claim) must be discoverable from the published schema, not just from
// a 500 after the fact.
func TestMatchLogicWritableFieldsDocumentTheTagsRequirement(t *testing.T) {
	cases := []struct {
		what string
		typ  reflect.Type
	}{
		{"CreateBudgetCategoryRequest", reflect.TypeOf(CreateBudgetCategoryRequest{})},
		{"UpdateBudgetCategoryRequest", reflect.TypeOf(UpdateBudgetCategoryRequest{})},
	}

	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			f, ok := c.typ.FieldByName("MatchLogic")
			if !ok {
				t.Fatalf("%s has no MatchLogic field", c.what)
			}
			doc := f.Tag.Get("doc")
			if doc == "" {
				t.Fatalf("%s.MatchLogic needs a doc tag naming the tags requirement for and/tag_only", c.what)
			}
			if !containsAll(doc, "tag_only", "and", "tags") {
				t.Errorf("%s.MatchLogic doc tag %q must mention tag_only, and, and tags", c.what, doc)
			}
		})
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}
