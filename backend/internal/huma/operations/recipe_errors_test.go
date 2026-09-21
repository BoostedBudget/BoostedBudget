package operations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// #1699 MCP-3: recipe failures must surface as actionable 4xx (400/404/409), never a generic 500,
// and GET on a missing recipe must be 404 rather than a panicking 200-null.

// newRecipeErrorRouter builds a production-config echo router with recipe routes registered and a
// test user already present in the request context, so the handler reaches the service.
func newRecipeErrorRouter(svc *fullMockRecipeService) *echo.Echo {
	e, api := newProductionAPI()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()
			c.SetRequest(req.WithContext(context.WithValue(req.Context(), inputs.UserIDKey, "user-1")))
			return next(c)
		}
	})
	RegisterRecipeRoutes(api, NewRecipeHandler(svc))
	return e
}

// recipeErrRequest issues an in-process request and reports whether the handler panicked. Huma
// panics when a handler returns a nil output with a nil error — the exact (nil,nil) defect this
// ticket fixes — so the harness must recover it rather than crash the test binary.
func recipeErrRequest(t *testing.T, e *echo.Echo, method, path, body string) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
				t.Logf("handler panicked: %v", r)
			}
		}()
		e.ServeHTTP(rec, req)
	}()
	return rec, panicked
}

func recipeErrDetail(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return fmt.Sprint(body["detail"])
}

func TestRecipeHandler_GetRecipe_MissingIs404(t *testing.T) {
	svc := new(fullMockRecipeService)
	svc.On("Get", mock.Anything, "unknown", "user-1").Return(nil, nil)

	rec, panicked := recipeErrRequest(t, newRecipeErrorRouter(svc), http.MethodGet, "/api/v1/mcp/recipes/unknown", "")
	require.False(t, panicked, "a missing recipe must not panic the handler")
	require.Equal(t, http.StatusNotFound, rec.Code, "a missing recipe must be 404, not 200-null")
}

func TestRecipeHandler_GetRecipe_SentinelIs404(t *testing.T) {
	svc := new(fullMockRecipeService)
	svc.On("Get", mock.Anything, "unknown", "user-1").Return(nil, domain.ErrRecipeNotFound)

	rec, panicked := recipeErrRequest(t, newRecipeErrorRouter(svc), http.MethodGet, "/api/v1/mcp/recipes/unknown", "")
	require.False(t, panicked)
	require.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, recipeErrDetail(t, rec), "Recipe not found")
}

func TestRecipeHandler_CreateRecipe_InvalidSpecIs400(t *testing.T) {
	svc := new(fullMockRecipeService)
	svc.On("Create", mock.Anything, "user-1", mock.Anything).Return(nil,
		fmt.Errorf("invalid recipe spec: operation %q is a write-class operation: %w", "createBudget", domain.ErrInvalidRecipeSpec))

	rec, panicked := recipeErrRequest(t, newRecipeErrorRouter(svc), http.MethodPost, "/api/v1/mcp/recipes", `{"name":"R","spec":[{"operation_id":"createBudget"}]}`)
	require.False(t, panicked)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, recipeErrDetail(t, rec), "createBudget", "400 detail must name the offending operation")
}

func TestRecipeHandler_CreateRecipe_DuplicateNameIs409(t *testing.T) {
	svc := new(fullMockRecipeService)
	svc.On("Create", mock.Anything, "user-1", mock.Anything).Return(nil,
		fmt.Errorf("recipe name already exists: %w", domain.ErrDuplicateRecipeName))

	rec, panicked := recipeErrRequest(t, newRecipeErrorRouter(svc), http.MethodPost, "/api/v1/mcp/recipes", `{"name":"R","spec":[{"operation_id":"getAccounts"}]}`)
	require.False(t, panicked)
	require.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, recipeErrDetail(t, rec), "already exists")
}

func TestRecipeHandler_UpdateRecipe_MissingIs404(t *testing.T) {
	svc := new(fullMockRecipeService)
	svc.On("Update", mock.Anything, "unknown", "user-1", mock.Anything).Return(nil, domain.ErrRecipeNotFound)

	rec, panicked := recipeErrRequest(t, newRecipeErrorRouter(svc), http.MethodPut, "/api/v1/mcp/recipes/unknown", `{"name":"R"}`)
	require.False(t, panicked)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRecipeHandler_RunRecipe_MissingIs404(t *testing.T) {
	svc := new(fullMockRecipeService)
	svc.On("Run", mock.Anything, "unknown", "user-1").Return(nil, domain.ErrRecipeNotFound)

	rec, panicked := recipeErrRequest(t, newRecipeErrorRouter(svc), http.MethodPost, "/api/v1/mcp/recipes/unknown/run", "")
	require.False(t, panicked)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRecipeHandler_CreateRecipe_GenuineErrorIs500(t *testing.T) {
	svc := new(fullMockRecipeService)
	svc.On("Create", mock.Anything, "user-1", mock.Anything).Return(nil, fmt.Errorf("database exploded"))

	rec, panicked := recipeErrRequest(t, newRecipeErrorRouter(svc), http.MethodPost, "/api/v1/mcp/recipes", `{"name":"R","spec":[{"operation_id":"getAccounts"}]}`)
	require.False(t, panicked)
	require.Equal(t, http.StatusInternalServerError, rec.Code, "a genuine internal error must stay 500")
}
