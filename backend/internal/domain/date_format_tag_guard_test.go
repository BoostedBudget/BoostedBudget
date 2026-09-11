package domain

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// THE format:date TAG GUARD (#1631).
//
// A date-only value carried as a Go `string` needs `format:"date"` on its tag, because that tag is
// the only thing that makes the generated OpenAPI describe it as a date and the generated clients
// treat it as one. Without it the field is an untyped string: correct on the wire, undocumented in
// the contract, and invisible to any client-side date handling.
//
// ⚠ WHY THIS GUARD EXISTS RATHER THAN A ONE-OFF SWEEP. This gap was "fixed" three times on this
// branch and was still incomplete each time, because each fix worked from a hand-built or
// script-built LIST of offenders and every list was wrong in a different way:
//
//  1. The first pass fixed only the three sites a gap note happened to cite.
//  2. My mechanical scan of the generated spec produced a 27-schema list that MISSED four fields —
//     and all four were the untagged HALF OF A TAGGED PAIR (start_date untagged beside a tagged
//     end_date, period_start beside period_end, last_date beside first_date). A list that can miss
//     the other half of a pair it already found is not a list anyone should trust.
//  3. A spot-check of mine concluded two Paddle fields were date-only from the `example:` in their
//     doc tag. Reading the ASSIGNMENT showed they come from the Paddle SDK's RFC 3339 TimePeriod
//     and can carry a full instant. Tagging them would have made the spec LIE, which is worse than
//     the missing tag.
//
// So the enumeration is done here, from the source, on every run. No list to go stale.
//
// What it does NOT check: whether the VALUE is really a date. That is the judgment in (3) above and
// it cannot be automated — which is why the exemption list below carries the evidence per entry
// rather than just a name, and why an entry is a claim a human made that nothing here verifies.
func TestEveryDateShapedStringFieldDeclaresFormatDate(t *testing.T) {
	violations, checked, err := findUntaggedDateFields([]string{".", "../huma/operations"}, rfc3339StringFields())
	if err != nil {
		t.Fatalf("scanning for date-shaped string fields: %v", err)
	}
	// A guard that measures nothing reads as a pass forever.
	if checked == 0 {
		t.Fatal("found no date-shaped string fields at all. Either the naming convention changed or " +
			"this scan is broken; refusing to pass without checking anything.")
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("date-shaped string field(s) missing `format:\"date\"` (%d checked):\n  %s\n\n"+
			"Add format:\"date\" so the generated OpenAPI and clients describe it as a date. If the "+
			"field can actually carry an RFC 3339 instant, do NOT tag it — add it to "+
			"rfc3339StringFields() with the population site as evidence.",
			checked, strings.Join(violations, "\n  "))
	}
}

// The guard's meta-test: constructs the violation and proves it is reported, then proves the
// exemption works. Without this, a refactor that stopped detecting anything would look identical
// to a clean tree.
func TestDateFormatTagGuardCatchesTheViolationItDescribes(t *testing.T) {
	cases := []struct {
		name       string
		src        string
		exempt     map[string]string
		wantIssues int
	}{
		{
			name:       "untagged date-shaped string is caught",
			src:        "package p\n\ntype R struct {\n\tStartDate string `json:\"start_date\"`\n}\n",
			wantIssues: 1,
		},
		{
			name:       "tagged one is clean",
			src:        "package p\n\ntype R struct {\n\tStartDate string `json:\"start_date\" format:\"date\"`\n}\n",
			wantIssues: 0,
		},
		{
			name:       "pointer form is checked too",
			src:        "package p\n\ntype R struct {\n\tLastDate *string `json:\"last_date,omitempty\"`\n}\n",
			wantIssues: 1,
		},
		{
			name:       "a json:\"-\" field is not on the wire and is not checked",
			src:        "package p\n\ntype R struct {\n\tStartDate string `json:\"-\"`\n}\n",
			wantIssues: 0,
		},
		{
			name:       "a non-date-shaped name is not checked",
			src:        "package p\n\ntype R struct {\n\tName string `json:\"name\"`\n}\n",
			wantIssues: 0,
		},
		{
			name:       "an exempted field is allowed to stay untagged",
			src:        "package p\n\ntype R struct {\n\tStartDate string `json:\"start_date\"`\n}\n",
			exempt:     map[string]string{"R.start_date": "RFC 3339 from an upstream SDK"},
			wantIssues: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(tc.src), 0o600); err != nil {
				t.Fatal(err)
			}
			got, _, err := findUntaggedDateFields([]string{dir}, tc.exempt)
			if err != nil {
				t.Fatalf("scan: %v", err)
			}
			if len(got) != tc.wantIssues {
				t.Fatalf("expected %d violation(s), got %d: %v", tc.wantIssues, len(got), got)
			}
		})
	}
}

// rfc3339StringFields lists date-SHAPED string fields that legitimately carry a full RFC 3339
// instant, keyed "<Struct>.<json name>". Tagging one of these `format:"date"` would make the
// generated contract assert something false.
//
// ⚠ Each entry names the POPULATION SITE, not just a reason. Every one of these was established by
// reading the assignment — a doc-tag `example:` proved misleading for exactly this set.
func rfc3339StringFields() map[string]string {
	return map[string]string{
		"GetCurrentSubscriptionOutputBody.current_period_end": "Paddle SDK TimePeriod.EndsAt, documented " +
			"as an RFC 3339 datetime string; date-only only on the trial-user branch, so the field is mixed-format.",
		"GetCurrentSubscriptionOutputBody.next_billing_date": "Paddle SDK TimePeriod.EndsAt, same mixed-format " +
			"source as current_period_end above.",
		"PreviewUpdateOutputBody.next_charge_date": "Paddle SDK TimePeriod.StartsAt, RFC 3339.",
		"ListAdminRetentionWithheldOutputBody.as_of": "asOf.Format(time.RFC3339) — a full instant " +
			"describing when the listing was taken, not a calendar day.",
		"ListPlaidOrphanedItemsOutputBody.as_of": "asOf.Format(time.RFC3339) — a full instant, as above.",

		// ⚠ These two are a DIFFERENT kind of exemption from the five above, and the distinction
		// matters. They are not mixed-format — the only caller sends YYYY-MM-DD
		// (apps/web/src/features/budgets/hooks/usePreviewCategories.ts documents its own parameter
		// as "(YYYY-MM-DD)"). They are exempt because this is a REQUEST body, where `format:"date"`
		// does not merely describe the field: huma validates an incoming body against the generated
		// schema BEFORE unmarshaling, so the tag would start REJECTING anything that is not a bare
		// date. That is a stricter contract, not a documentation fix, and tightening request
		// validation is outside what #1631 set out to do.
		//
		// ⚠ This paragraph used to end by claiming domain.FlexibleDate "deliberately produces a
		// format-less schema" for the budget and savings-goal request bodies. That described THIS
		// branch's original FlexibleDate, which was discarded in the staging merge in favour of
		// #1630's — and #1630's does the opposite: it implements huma.SchemaProvider and publishes
		// `anyOf: [{type: string, format: date}, {type: string, format: date-time}]`, so those
		// fields ARE format-documented and accept both forms. The fields exempted here are plain
		// `string`, not FlexibleDate, so the stricter-validation reasoning above is theirs alone.
		"CategoryPreviewRequest.start_date": "request body; format:\"date\" would add strict huma " +
			"pre-unmarshal validation rather than documentation. Caller sends YYYY-MM-DD.",
		"CategoryPreviewRequest.end_date": "request body; same reasoning as start_date above.",
	}
}

// dateShapedName matches json field names that denote a calendar day. Deliberately conservative:
// it is better to miss an oddly-named field than to demand `format:"date"` on something that is
// not a date at all.
var dateShapedName = regexp.MustCompile(`(^|_)(date|deadline|as_of|occurred_on)($|_)|^(first|last|next|start|end|period_start|period_end)_date$`)

func findUntaggedDateFields(dirs []string, exempt map[string]string) ([]string, int, error) {
	var violations []string
	checked := 0
	fset := token.NewFileSet()

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, 0, err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			file, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				continue // a parse failure is the compiler's message to give, not this guard's
			}
			ast.Inspect(file, func(n ast.Node) bool {
				ts, ok := n.(*ast.TypeSpec)
				if !ok {
					return true
				}
				st, isStruct := ts.Type.(*ast.StructType)
				if !isStruct {
					return true
				}
				for _, f := range st.Fields.List {
					if f.Tag == nil || !isStringOrStringPtr(f.Type) {
						continue
					}
					tag := f.Tag.Value
					jsonName := jsonNameFromTag(tag)
					if jsonName == "" || jsonName == "-" || !dateShapedName.MatchString(jsonName) {
						continue
					}
					checked++
					if strings.Contains(tag, `format:"date"`) {
						continue
					}
					key := ts.Name.Name + "." + jsonName
					if _, ok := exempt[key]; ok {
						continue
					}
					violations = append(violations,
						fset.Position(f.Pos()).String()+": "+key)
				}
				return true
			})
		}
	}
	return violations, checked, nil
}

func isStringOrStringPtr(expr ast.Expr) bool {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	id, ok := expr.(*ast.Ident)
	return ok && id.Name == "string"
}

var jsonTagRe = regexp.MustCompile(`json:"([^"]*)"`)

func jsonNameFromTag(tag string) string {
	m := jsonTagRe.FindStringSubmatch(tag)
	if m == nil {
		return ""
	}
	return strings.Split(m[1], ",")[0]
}
