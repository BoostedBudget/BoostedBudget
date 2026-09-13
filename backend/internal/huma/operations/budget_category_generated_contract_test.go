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
	tests := []struct {
		schemaName string
		clientFile string
		expected   []string
	}{
		{
			schemaName: "BudgetCategory",
			clientFile: "budgetCategoryCategoryKind.ts",
			expected:   []string{"pfc", "custom", "savings", "group", "remainder"},
		},
		{
			schemaName: "CreateBudgetCategoryRequest",
			clientFile: "createBudgetCategoryRequestCategoryKind.ts",
			expected:   []string{"pfc", "custom", "savings", "group", "remainder"},
		},
		{
			schemaName: "CategoryPreviewRequest",
			clientFile: "categoryPreviewRequestCategoryKind.ts",
			expected:   []string{"pfc", "custom", "group", "remainder"},
		},
	}
	schemas := committedSpec(t)
	_, thisFile, _, _ := runtime.Caller(0)
	modelsDir := filepath.Join(filepath.Dir(thisFile), "../../../../packages/api-client/src/models")

	for _, test := range tests {
		t.Run(test.schemaName, func(t *testing.T) {
			schema, ok := schemas[test.schemaName].(map[string]any)
			require.True(t, ok, "%s is missing from committed api/openapi.yaml", test.schemaName)
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
			require.Equal(t, test.expected, actualSpec, "committed OpenAPI category_kind enum drifted")

			data, err := os.ReadFile(filepath.Join(modelsDir, test.clientFile))
			require.NoError(t, err)
			require.NoError(t, validateGeneratedCategoryKindModule(string(data), test.schemaName+"CategoryKind", test.expected))
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
