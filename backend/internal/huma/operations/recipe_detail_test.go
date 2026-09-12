package operations

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// This fixture is also consumed by the MCP projection/size regression suite.
func TestRecipeProductionResponseFixture(t *testing.T) {
	fixture, err := os.ReadFile("testdata/mcp_recipe_response.json")
	require.NoError(t, err)
	var recipe domain.MCPRecipe
	require.NoError(t, json.Unmarshal(fixture, &recipe))
	e, api := newProductionAPI()
	svc := &fullMockRecipeService{}
	svc.On("Get", mock.Anything, recipe.ID, recipe.UserID).Return(&recipe, nil).Once()
	RegisterRecipeRoutes(api, NewRecipeHandler(svc))
	req := httptest.NewRequest("GET", "/api/v1/mcp/recipes/"+recipe.ID, nil)
	req = req.WithContext(context.WithValue(req.Context(), inputs.UserIDKey, recipe.UserID))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.JSONEq(t, string(fixture), rec.Body.String())
	svc.AssertExpectations(t)
}

func TestRecipeProductionDetailInput(t *testing.T) {
	for _, detail := range []string{"", ",\"detail\":null", ",\"detail\":\"compact\"", ",\"detail\":\"full\"", ",\"detail\":\"\"", ",\"detail\":\"verbose\"", ",\"detail\":\"null\""} {
		t.Run(detail, func(t *testing.T) {
			e, api := newProductionAPI()
			svc := &mockRecipeService{}
			RegisterRecipeRoutes(api, NewRecipeHandler(svc))
			req := httptest.NewRequest("POST", "/api/v1/mcp/recipes", strings.NewReader(`{"name":"Synthetic review","spec":[{"operation_id":"getAccounts"`+detail+`}]}`))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(context.WithValue(req.Context(), inputs.UserIDKey, "synthetic-user"))
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if detail == ",\"detail\":\"\"" || detail == ",\"detail\":\"verbose\"" || detail == ",\"detail\":\"null\"" {
				require.Equal(t, 422, rec.Code, rec.Body.String())
				require.Zero(t, svc.callCount)
				return
			}
			require.Equal(t, 200, rec.Code, rec.Body.String())
			var body map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			expected := detail
			if detail == ",\"detail\":null" {
				expected = ""
			}
			require.JSONEq(t, `[{"operation_id":"getAccounts"`+expected+`}]`, string(body["spec"]))
			require.Contains(t, body, "$schema")
		})
	}
}

func TestRecipeProductionDetailSchemaAllowsJSONNull(t *testing.T) {
	_, api := newProductionAPI()
	RegisterRecipeRoutes(api, NewRecipeHandler(&mockRecipeService{}))
	schema := api.OpenAPI().Components.Schemas.Map()["MCPRecipeCall"].Properties["detail"]
	require.True(t, schema.Nullable)
	require.Equal(t, []any{"compact", "full", nil}, schema.Enum)
	require.Nil(t, schema.Default)
}
