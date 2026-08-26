package operations

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// committedSpec parses the COMMITTED api/openapi.yaml — the artifact `make generate-api-client`
// reads and the one a downstream consumer actually receives — rather than the spec humatest
// generates in-process. The two are held byte-identical by the `openapi-validate` job in
// .github/workflows/test.yml (it runs scripts/generate-openapi/main.go then
// `git diff --exit-code api/openapi.yaml`), so this is not a drift check and deliberately does not
// duplicate one; it is an assertion about the SHAPE of the published contract, which stays wrong in
// exactly the same way whether the artifact is fresh or stale.
func committedSpec(t *testing.T) map[string]any {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	// backend/internal/huma/operations/ → ../../../../ = repo root
	specPath := filepath.Join(filepath.Dir(thisFile), "../../../../", "api/openapi.yaml")

	data, err := os.ReadFile(specPath)
	require.NoError(t, err, "api/openapi.yaml must be readable at %s — it is the contract clients are generated from", specPath)

	var doc map[string]any
	require.NoError(t, yaml.Unmarshal(data, &doc))

	components, _ := doc["components"].(map[string]any)
	require.NotNil(t, components)
	schemas, _ := components["schemas"].(map[string]any)
	require.NotNil(t, schemas)
	return schemas
}

// TestTransferSuggestionOutputs_DoNotRequireAFieldHumaCannotEmit pins the five transfer-suggestion
// response bodies against the rule established for next_cursor on the retention-withheld listing:
//
// huma renders a *struct field as a BARE $ref (huma@v2.39.1 schema.go:623 — object nullability is
// explicitly unsupported), so unlike a *string, whose schema carries "null" in its type union,
// there is no encoding of "present but empty" for it. A nil pointer therefore comes out ABSENT, not
// null. Marking such a field `required` publishes a contract that rejects a body the server really
// sends, and generates a TypeScript model typing the field non-optional for a client to
// dereference. The live nil path here is the MCP idempotency replay in transfer_suggestion.go: a
// cache hit whose stored value is null calls buildOutput(nil, false).
//
// The fix is the same one next_cursor took — `omitempty`, which drops the field from `required`
// (schema.go:908) and makes "absent" the schema-valid encoding.
func TestTransferSuggestionOutputs_DoNotRequireAFieldHumaCannotEmit(t *testing.T) {
	schemas := committedSpec(t)

	for _, name := range []string{
		"AcceptTransferSuggestionOutputBody",
		"CancelTransferSuggestionOutputBody",
		"ConfirmTransferSuggestionMatchOutputBody",
		"DeclineTransferSuggestionMatchOutputBody",
		"RejectTransferSuggestionOutputBody",
	} {
		schema, ok := schemas[name].(map[string]any)
		require.True(t, ok, "%s is missing from the committed spec — the operation was renamed or dropped and this guard has stopped guarding", name)

		props, _ := schema["properties"].(map[string]any)
		require.Contains(t, props, "suggestion", "%s must still carry a suggestion property, or this guard is watching the wrong schema", name)

		for _, req := range requiredNames(schema) {
			require.NotEqual(t, "suggestion", req,
				"%s.suggestion is `required` in the COMMITTED spec but is backed by a *domain.TransferSuggestion, which huma renders as a bare $ref with no \"null\" in its type union. The MCP idempotency replay emits this body with suggestion ABSENT, so the published contract rejects a response the server sends. Add omitempty, as next_cursor did.", name)
		}
	}
}

func requiredNames(schema map[string]any) []string {
	raw, _ := schema["required"].([]any)
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		if s, ok := r.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// TestTransferSuggestionFile_NoRequiredPointerToStructField is the same rule enforced at its SOURCE
// rather than on one enumerated list of schema names, so a field added to this file tomorrow is
// covered without anyone remembering to extend the list above.
//
// It fires only for a pointer to a type the committed spec renders as an OBJECT. A pointer to a
// scalar or to a string alias is deliberately exempt and must NOT be "fixed": huma renders those as
// type: [string, "null"], null IS expressible, and `required` is correct for them (the reason
// UserAccountShareView.Visibility *ShareVisibility is right as written).
func TestTransferSuggestionFile_NoRequiredPointerToStructField(t *testing.T) {
	schemas := committedSpec(t)
	_, thisFile, _, _ := runtime.Caller(0)
	src := filepath.Join(filepath.Dir(thisFile), "transfer_suggestion.go")

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, src, nil, parser.SkipObjectResolution)
	require.NoError(t, err)

	offenders := requiredPointerToObjectOffenders(fset, file, schemas)

	sort.Strings(offenders)
	require.Empty(t, offenders,
		"each of these is a pointer to a type the contract publishes as an object, with no omitempty, so huma keeps it in `required` while a nil emits it ABSENT — a body the published schema rejects. Add omitempty (a nullable scalar or string alias is exempt and is not reported here):\n%s",
		strings.Join(offenders, "\n"))
}

// requiredPointerToObjectOffenders walks file's struct types and reports every exported field that
// is a pointer to a type schemas publishes as an OBJECT and that huma will keep in `required`: a
// tagged field with none of `omitempty`, `omitzero`, or an explicit `required:"false"` override, OR
// an untagged field with no `required:"false"` override either (an untagged field cannot carry
// omitempty/omitzero at all, so the struct tag is its only way out).
//
// #1569 R6B/B3: the untagged branch is not a hedge — an exported, untagged field is NOT exempt from
// this check. huma (like encoding/json) falls back to the literal Go field name as the property
// name when there is no json tag at all, and there is no way to express omitempty without a tag, so
// an untagged exported pointer-to-object field is unconditionally required in the generated schema.
// The guard used to skip every untagged field before ever reaching this logic (`if f.Tag == nil {
// continue }`), which is exactly the shape this branch now catches.
//
// #1569 R6C/item 8: this branch only checked the json tag for `omitempty`, but huma's own
// required-field logic (SchemaFromType, huma/v2@v2.39.1 schema.go:895-921) honours THREE ways to
// make a field optional, checked in this order: `omitempty` in the json tag, `omitzero` in the json
// tag, and — evaluated LAST, so it wins even over neither of the first two — an explicit
// `required:"false"` struct tag (and, symmetrically, `required:"true"` FORCES a field required even
// with omitempty/omitzero present). A field tagged only `json:"x,omitzero"` was a false positive
// here; a field tagged `json:"x,omitempty" required:"true"` was a false negative. Both are now
// checked, in the same order and with the same override precedence as huma itself.
func requiredPointerToObjectOffenders(fset *token.FileSet, file *ast.File, schemas map[string]any) []string {
	var offenders []string
	ast.Inspect(file, func(n ast.Node) bool {
		st, ok := n.(*ast.StructType)
		if !ok || st.Fields == nil {
			return true
		}
		for _, f := range st.Fields.List {
			star, ok := f.Type.(*ast.StarExpr)
			if !ok {
				continue
			}
			var typeName string
			switch base := star.X.(type) {
			case *ast.Ident:
				typeName = base.Name
			case *ast.SelectorExpr:
				typeName = base.Sel.Name
			default:
				continue
			}
			schema, ok := schemas[typeName].(map[string]any)
			if !ok || schema["type"] != "object" {
				// Not published as an object: either not in the contract at all, or a
				// nullable scalar/alias, for which `required` is legitimate.
				continue
			}

			var jsonName string
			optional := false
			if f.Tag == nil {
				if len(f.Names) == 0 || !f.Names[0].IsExported() {
					continue // embedded or unexported: a different shape this check does not name
				}
				jsonName = f.Names[0].Name
			} else {
				structTag := reflect.StructTag(strings.Trim(f.Tag.Value, "`"))
				tag := structTag.Get("json")
				if tag == "" || tag == "-" {
					continue
				}
				parts := strings.Split(tag, ",")
				jsonName = parts[0]
				// Same order huma itself checks (schema.go:895-921): omitempty, then omitzero,
				// then an explicit `required` tag, which overrides whatever the json tag said —
				// in EITHER direction, so `required:"true"` re-requires a field that omitempty
				// or omitzero would otherwise have exempted.
				optional = slicesContains(parts[1:], "omitempty") || slicesContains(parts[1:], "omitzero")
				if v, ok := structTag.Lookup("required"); ok {
					optional = v == "false"
				}
			}
			if optional {
				continue
			}
			name := "<embedded>"
			if len(f.Names) > 0 {
				name = f.Names[0].Name
			}
			tagDisplay := jsonName
			if f.Tag == nil {
				tagDisplay = jsonName + " (no json tag — huma uses the Go field name verbatim)"
			}
			offenders = append(offenders, name+" *"+typeName+" `json:\""+tagDisplay+"\"` at "+fset.Position(f.Pos()).String())
		}
		return true
	})
	return offenders
}

// TestRequiredPointerToObjectOffenders_CatchesTheUntaggedShape is the guard's own falsification for
// #1569 R6B/B3: a synthetic file with an EXPORTED, UNTAGGED pointer-to-object field must be caught,
// proving the untagged branch introduced above actually changes the guard's behaviour rather than
// being dead code that happens to compile.
func TestRequiredPointerToObjectOffenders_CatchesTheUntaggedShape(t *testing.T) {
	schemas := map[string]any{
		"domain.Thing": map[string]any{"type": "object"},
		"Thing":        map[string]any{"type": "object"},
	}

	src := "package sample\n\n" +
		"type WithOmitEmpty struct {\n\tOK *Thing `json:\"ok,omitempty\"`\n}\n\n" +
		"type WithoutOmitEmpty struct {\n\tBad *Thing `json:\"bad\"`\n}\n\n" +
		"type Untagged struct {\n\tSuggestion *Thing\n}\n"

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sample.go", src, parser.SkipObjectResolution)
	require.NoError(t, err)

	offenders := requiredPointerToObjectOffenders(fset, file, schemas)
	sort.Strings(offenders)

	require.Len(t, offenders, 2, "exactly the tagged-without-omitempty and the untagged fields must be reported, not the omitempty one:\n%s", strings.Join(offenders, "\n"))
	joined := strings.Join(offenders, "\n")
	require.Contains(t, joined, "Bad *Thing", "the tagged, non-omitempty field must still be caught")
	require.Contains(t, joined, "Suggestion *Thing", "the untagged exported field must be caught — this is the #1569 R6B/B3 fix")
	require.NotContains(t, joined, "OK *Thing", "the omitempty field must not be reported")
}

// TestRequiredPointerToObjectOffenders_HandlesOmitzeroAndRequiredOverride is the guard's own
// falsification for #1569 R6C/item 8: huma honours `omitzero` and an explicit `required` struct
// tag override (schema.go:895-921), in that order, and this branch was rewritten in the same round
// without either. Before the fix, `WithOmitZero` was a false positive (reported despite huma
// treating it as optional) and `RequiredTrueDespiteOmitEmpty` was a false negative (not reported
// despite huma keeping it required); `RequiredFalseNoOmit` proves the override works the other
// direction too, with no omitempty/omitzero present at all.
func TestRequiredPointerToObjectOffenders_HandlesOmitzeroAndRequiredOverride(t *testing.T) {
	schemas := map[string]any{
		"domain.Thing": map[string]any{"type": "object"},
		"Thing":        map[string]any{"type": "object"},
	}

	src := "package sample\n\n" +
		"type WithOmitZero struct {\n\tOK *Thing `json:\"ok,omitzero\"`\n}\n\n" +
		"type RequiredFalseNoOmit struct {\n\tAlsoOK *Thing `json:\"also_ok\" required:\"false\"`\n}\n\n" +
		"type RequiredTrueDespiteOmitEmpty struct {\n\tBad *Thing `json:\"bad,omitempty\" required:\"true\"`\n}\n"

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sample.go", src, parser.SkipObjectResolution)
	require.NoError(t, err)

	offenders := requiredPointerToObjectOffenders(fset, file, schemas)
	sort.Strings(offenders)
	joined := strings.Join(offenders, "\n")

	require.Len(t, offenders, 1, "exactly the required:\"true\" field must be reported:\n%s", joined)
	require.Contains(t, joined, "Bad *Thing", "required:\"true\" must re-require a field even though omitempty is present")
	require.NotContains(t, joined, "OK *Thing", "omitzero alone must exempt the field, same as omitempty")
	require.NotContains(t, joined, "AlsoOK", "required:\"false\" must exempt the field even with no omitempty/omitzero at all")
}

func slicesContains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
