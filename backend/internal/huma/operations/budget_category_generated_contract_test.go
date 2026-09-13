package operations

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommittedBudgetCategoryKindsMatchGeneratedClient(t *testing.T) {
	want := map[string][]string{
		"BudgetCategory":              {"pfc", "custom", "savings", "group", "remainder"},
		"CreateBudgetCategoryRequest": {"pfc", "custom", "savings", "group", "remainder"},
		"CategoryPreviewRequest":      {"pfc", "custom", "group", "remainder"},
	}
	clientFiles := map[string]string{
		"BudgetCategory":              "budgetCategoryCategoryKind.ts",
		"CreateBudgetCategoryRequest": "createBudgetCategoryRequestCategoryKind.ts",
		"CategoryPreviewRequest":      "categoryPreviewRequestCategoryKind.ts",
	}
	schemas := committedSpec(t)
	_, thisFile, _, _ := runtime.Caller(0)
	modelsDir := filepath.Join(filepath.Dir(thisFile), "../../../../packages/api-client/src/models")

	for schemaName, expected := range want {
		t.Run(schemaName, func(t *testing.T) {
			schema, ok := schemas[schemaName].(map[string]any)
			require.True(t, ok, "%s is missing from committed api/openapi.yaml", schemaName)
			properties, ok := schema["properties"].(map[string]any)
			require.True(t, ok)
			categoryKind, ok := properties["category_kind"].(map[string]any)
			require.True(t, ok)
			rawEnum, ok := categoryKind["enum"].([]any)
			require.True(t, ok)
			actualSpec := make([]string, 0, len(rawEnum))
			for _, value := range rawEnum {
				actualSpec = append(actualSpec, value.(string))
			}
			require.Equal(t, expected, actualSpec, "committed OpenAPI category_kind enum drifted")

			data, err := os.ReadFile(filepath.Join(modelsDir, clientFiles[schemaName]))
			require.NoError(t, err)
			require.NoError(t, validateGeneratedCategoryKindModule(string(data), schemaName+"CategoryKind", expected))
		})
	}
}

func validateGeneratedCategoryKindModule(source, constName string, expected []string) error {
	quotedName := regexp.QuoteMeta(constName)
	constPattern := regexp.MustCompile(`(?ms)^[\t ]*export\s+const\s+` + quotedName + `\s*=\s*\{(.*?)\}\s+as\s+const\s*;`)
	constMatches := constPattern.FindAllStringSubmatch(source, -1)
	if len(constMatches) != 1 {
		return fmt.Errorf("expected one exported %s const object followed by as const, got %d", constName, len(constMatches))
	}

	typePattern := regexp.MustCompile(`(?ms)^[\t ]*export\s+type\s+` + quotedName + `\s*=\s*([^;]+);`)
	typeMatches := typePattern.FindAllStringSubmatch(source, -1)
	if len(typeMatches) != 1 {
		return fmt.Errorf("expected one exported %s type, got %d", constName, len(typeMatches))
	}
	typeExpression := strings.Join(strings.Fields(typeMatches[0][1]), " ")
	wantType := "typeof " + constName + "[keyof typeof " + constName + "]"
	if typeExpression != wantType {
		return fmt.Errorf("%s type is %q, want %q", constName, typeExpression, wantType)
	}

	entryPattern := regexp.MustCompile(`^([a-z_]+)\s*:\s*'([^']+)'\s*,\s*$`)
	actual := make([]string, 0, len(expected))
	for _, line := range strings.Split(strings.ReplaceAll(constMatches[0][1], "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		entry := entryPattern.FindStringSubmatch(line)
		if entry == nil {
			return fmt.Errorf("unexpected content in %s const object: %q", constName, line)
		}
		if entry[1] != entry[2] {
			return fmt.Errorf("%s enum key %q does not equal value %q", constName, entry[1], entry[2])
		}
		actual = append(actual, entry[2])
	}
	if !slices.Equal(actual, expected) {
		return fmt.Errorf("%s entries are %q, want %q", constName, actual, expected)
	}
	return nil
}

func TestValidateGeneratedCategoryKindModuleRejectsMalformedSource(t *testing.T) {
	constName := "BudgetCategoryCategoryKind"
	validEntries := "  pfc: 'pfc',\n  custom: 'custom',\n"
	validType := "export type BudgetCategoryCategoryKind = typeof BudgetCategoryCategoryKind[keyof typeof BudgetCategoryCategoryKind];\n"
	expected := []string{"pfc", "custom"}
	tests := map[string]string{
		"missing as const":                   validType + "export const " + constName + " = {\n" + validEntries + "};\n",
		"widened type":                       "export type BudgetCategoryCategoryKind = string;\nexport const " + constName + " = {\n" + validEntries + "} as const;\n",
		"decoy entries outside named object": validType + "const WrongCategoryKind = {\n" + validEntries + "} as const;\nexport const " + constName + " = {} as const;\n",
		"mismatched key and value":           validType + "export const " + constName + " = {\n  pfc: 'custom',\n  custom: 'custom',\n} as const;\n",
		"extra entry":                        validType + "export const " + constName + " = {\n" + validEntries + "  remainder: 'remainder',\n} as const;\n",
	}
	validModule := validType + "export const " + constName + " = {\n" + validEntries + "} as const;\n"
	require.NoError(t, validateGeneratedCategoryKindModule(strings.ReplaceAll(validModule, "\n", "\r\n"), constName, expected),
		"canonical generated shape must tolerate CRLF line endings")

	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			require.Error(t, validateGeneratedCategoryKindModule(source, constName, expected))
		})
	}
}
