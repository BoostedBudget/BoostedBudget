package operations

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
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
	entryPattern := regexp.MustCompile(`(?m)^\s*([a-z_]+): '([^']+)',\s*$`)

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
			matches := entryPattern.FindAllStringSubmatch(string(data), -1)
			actualClient := make([]string, 0, len(matches))
			for _, match := range matches {
				require.Equal(t, match[1], match[2], "generated enum key and value must agree")
				actualClient = append(actualClient, match[2])
			}
			require.Equal(t, expected, actualClient, "committed generated TypeScript union drifted from its exact contract")
		})
	}
}
