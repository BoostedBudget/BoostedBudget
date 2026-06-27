package operations

// Issue #1087 NEW-GAP-B: Idempotency wiring for createMCPRecipe
// Tests that the Idempotency-Key header is honoured by the recipe handler.

import (
	"context"
	"errors"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockIdempotencyRepository for testing
type mockIdempotencyRepository struct {
	getCalls []string
	cached   map[string]*domain.MCPRecipe
}

func (m *mockIdempotencyRepository) GetOrCreate(
	ctx context.Context,
	key, userID, operationID string,
	fn func() (interface{}, error),
	result interface{},
) (bool, error) {
	m.getCalls = append(m.getCalls, key)
	if existing, ok := m.cached[key]; ok {
		*(result.(**domain.MCPRecipe)) = existing
		return true, nil
	}
	res, err := fn()
	if err != nil {
		return false, err
	}
	recipe := res.(*domain.MCPRecipe)
	if m.cached == nil {
		m.cached = make(map[string]*domain.MCPRecipe)
	}
	m.cached[key] = recipe
	*(result.(**domain.MCPRecipe)) = recipe
	return false, nil
}

func (m *mockIdempotencyRepository) GetOrCreateInTx(
	ctx context.Context,
	_ pgx.Tx,
	key, userID, operationID string,
	fn func() (interface{}, error),
	result interface{},
) (bool, error) {
	cacheHit, err := m.GetOrCreate(ctx, key, userID, operationID, fn, result)
	return cacheHit, err
}

func (m *mockIdempotencyRepository) DeleteExpired(_ context.Context) (int64, error) {
	return 0, nil
}

// mockRecipeService implements services.RecipeService for operations tests
type mockRecipeService struct {
	createFn func(ctx context.Context, userID string, req domain.CreateMCPRecipeRequest) (*domain.MCPRecipe, error)
	callCount int
}

func (m *mockRecipeService) Create(ctx context.Context, userID string, req domain.CreateMCPRecipeRequest) (*domain.MCPRecipe, error) {
	m.callCount++
	if m.createFn != nil {
		return m.createFn(ctx, userID, req)
	}
	return &domain.MCPRecipe{ID: "recipe-1", UserID: userID, Name: req.Name, Spec: req.Spec, Enabled: true}, nil
}
func (m *mockRecipeService) List(ctx context.Context, userID string) ([]domain.MCPRecipe, error) {
	return nil, nil
}
func (m *mockRecipeService) Get(ctx context.Context, id string, userID string) (*domain.MCPRecipe, error) {
	return nil, nil
}
func (m *mockRecipeService) Update(ctx context.Context, id string, userID string, req domain.UpdateMCPRecipeRequest) (*domain.MCPRecipe, error) {
	return nil, nil
}
func (m *mockRecipeService) Delete(ctx context.Context, id string, userID string) error { return nil }
func (m *mockRecipeService) Run(ctx context.Context, id string, userID string) (*domain.MCPRecipeRunResult, error) {
	return nil, nil
}

func validRecipeBody() domain.CreateMCPRecipeRequest {
	return domain.CreateMCPRecipeRequest{
		Name: "Test Recipe",
		Spec: []domain.MCPRecipeCall{{OperationID: "getCashFlowSummary"}},
	}
}

// fullMockRecipeService is a testify mock implementing the full services.RecipeService interface.
type fullMockRecipeService struct {
	mock.Mock
}

func (m *fullMockRecipeService) Create(ctx context.Context, userID string, req domain.CreateMCPRecipeRequest) (*domain.MCPRecipe, error) {
	args := m.Called(ctx, userID, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.MCPRecipe), args.Error(1)
}

func (m *fullMockRecipeService) List(ctx context.Context, userID string) ([]domain.MCPRecipe, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.MCPRecipe), args.Error(1)
}

func (m *fullMockRecipeService) Get(ctx context.Context, id string, userID string) (*domain.MCPRecipe, error) {
	args := m.Called(ctx, id, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.MCPRecipe), args.Error(1)
}

func (m *fullMockRecipeService) Update(ctx context.Context, id string, userID string, req domain.UpdateMCPRecipeRequest) (*domain.MCPRecipe, error) {
	args := m.Called(ctx, id, userID, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.MCPRecipe), args.Error(1)
}

func (m *fullMockRecipeService) Delete(ctx context.Context, id string, userID string) error {
	args := m.Called(ctx, id, userID)
	return args.Error(0)
}

func (m *fullMockRecipeService) Run(ctx context.Context, id string, userID string) (*domain.MCPRecipeRunResult, error) {
	args := m.Called(ctx, id, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.MCPRecipeRunResult), args.Error(1)
}

// ===========================================================================
// Recipe handler tests (Priority 3): ListRecipes, GetRecipe, UpdateRecipe,
// DeleteRecipe, RunRecipe
// ===========================================================================

// TestRecipeHandler_ListRecipes_ReturnsRecipes verifies ListRecipes delegates to service.
func TestRecipeHandler_ListRecipes_ReturnsRecipes(t *testing.T) {
	ctx := context.Background()
	svc := new(fullMockRecipeService)
	userID := "user-1"
	recipes := []domain.MCPRecipe{
		{ID: "r1", UserID: userID, Name: "Recipe 1"},
		{ID: "r2", UserID: userID, Name: "Recipe 2"},
	}
	svc.On("List", ctx, userID).Return(recipes, nil)

	handler := NewRecipeHandler(svc)
	input := &ListRecipesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
	}

	result, err := handler.ListRecipes(ctx, input)

	require.NoError(t, err)
	assert.Len(t, result.Body, 2)
	assert.Equal(t, "r1", result.Body[0].ID)
	svc.AssertExpectations(t)
}

// TestRecipeHandler_ListRecipes_ReturnsEmptyArrayWhenNil verifies nil → empty array.
func TestRecipeHandler_ListRecipes_ReturnsEmptyArrayWhenNil(t *testing.T) {
	ctx := context.Background()
	svc := new(fullMockRecipeService)
	userID := "user-1"
	svc.On("List", ctx, userID).Return(nil, nil)

	handler := NewRecipeHandler(svc)
	input := &ListRecipesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
	}

	result, err := handler.ListRecipes(ctx, input)

	require.NoError(t, err)
	assert.NotNil(t, result.Body, "body must not be nil even when service returns nil")
	assert.Empty(t, result.Body)
}

// TestRecipeHandler_GetRecipe_ReturnsRecipe verifies GetRecipe delegates to service.
func TestRecipeHandler_GetRecipe_ReturnsRecipe(t *testing.T) {
	ctx := context.Background()
	svc := new(fullMockRecipeService)
	userID := "user-1"
	recipeID := "r1"
	recipe := &domain.MCPRecipe{ID: recipeID, UserID: userID, Name: "My Recipe"}
	svc.On("Get", ctx, recipeID, userID).Return(recipe, nil)

	handler := NewRecipeHandler(svc)
	input := &GetRecipeInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 recipeID,
	}

	result, err := handler.GetRecipe(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, recipeID, result.Body.ID)
	svc.AssertExpectations(t)
}

// TestRecipeHandler_GetRecipe_NotFound verifies error when recipe not found.
func TestRecipeHandler_GetRecipe_NotFound(t *testing.T) {
	ctx := context.Background()
	svc := new(fullMockRecipeService)
	userID := "user-1"
	recipeID := "r-nonexistent"
	svc.On("Get", ctx, recipeID, userID).Return(nil, errors.New("not found"))

	handler := NewRecipeHandler(svc)
	input := &GetRecipeInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 recipeID,
	}

	_, err := handler.GetRecipe(ctx, input)
	require.Error(t, err)
}

// TestRecipeHandler_UpdateRecipe_UpdatesRecipe verifies UpdateRecipe delegates to service.
func TestRecipeHandler_UpdateRecipe_UpdatesRecipe(t *testing.T) {
	ctx := context.Background()
	svc := new(fullMockRecipeService)
	userID := "user-1"
	recipeID := "r1"
	newName := "Updated Recipe"
	req := domain.UpdateMCPRecipeRequest{Name: &newName}
	updated := &domain.MCPRecipe{ID: recipeID, UserID: userID, Name: newName}
	svc.On("Update", ctx, recipeID, userID, req).Return(updated, nil)

	handler := NewRecipeHandler(svc)
	input := &UpdateRecipeInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 recipeID,
		Body:               req,
	}

	result, err := handler.UpdateRecipe(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, newName, result.Body.Name)
	svc.AssertExpectations(t)
}

// TestRecipeHandler_DeleteRecipe_DeletesRecipe verifies DeleteRecipe delegates to service.
func TestRecipeHandler_DeleteRecipe_DeletesRecipe(t *testing.T) {
	ctx := context.Background()
	svc := new(fullMockRecipeService)
	userID := "user-1"
	recipeID := "r1"
	svc.On("Delete", ctx, recipeID, userID).Return(nil)

	handler := NewRecipeHandler(svc)
	input := &DeleteRecipeInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 recipeID,
	}

	result, err := handler.DeleteRecipe(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, result)
	svc.AssertExpectations(t)
}

// TestRecipeHandler_DeleteRecipe_ServiceError verifies error propagation on delete.
func TestRecipeHandler_DeleteRecipe_ServiceError(t *testing.T) {
	ctx := context.Background()
	svc := new(fullMockRecipeService)
	userID := "user-1"
	recipeID := "r1"
	svc.On("Delete", ctx, recipeID, userID).Return(errors.New("delete failed"))

	handler := NewRecipeHandler(svc)
	input := &DeleteRecipeInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 recipeID,
	}

	_, err := handler.DeleteRecipe(ctx, input)
	require.Error(t, err)
}

// TestRecipeHandler_RunRecipe_ReturnsResult verifies RunRecipe delegates to service.
func TestRecipeHandler_RunRecipe_ReturnsResult(t *testing.T) {
	ctx := context.Background()
	svc := new(fullMockRecipeService)
	userID := "user-1"
	recipeID := "r1"
	runResult := &domain.MCPRecipeRunResult{RecipeID: recipeID}
	svc.On("Run", ctx, recipeID, userID).Return(runResult, nil)

	handler := NewRecipeHandler(svc)
	input := &RunRecipeInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 recipeID,
	}

	result, err := handler.RunRecipe(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, recipeID, result.Body.RecipeID)
	svc.AssertExpectations(t)
}

// TestRecipeHandler_RunRecipe_ServiceError verifies error propagation on run.
func TestRecipeHandler_RunRecipe_ServiceError(t *testing.T) {
	ctx := context.Background()
	svc := new(fullMockRecipeService)
	userID := "user-1"
	recipeID := "r1"
	svc.On("Run", ctx, recipeID, userID).Return(nil, errors.New("run failed"))

	handler := NewRecipeHandler(svc)
	input := &RunRecipeInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 recipeID,
	}

	_, err := handler.RunRecipe(ctx, input)
	require.Error(t, err)
}

// ===========================================================================
// Priority 4: RegisterRecipeRoutes
// ===========================================================================

// TestRegisterRecipeRoutes_RegistersAllRoutes verifies all recipe routes are registered.
func TestRegisterRecipeRoutes_RegistersAllRoutes(t *testing.T) {
	_, api := humatest.New(t)
	svc := &mockRecipeService{}
	handler := NewRecipeHandler(svc)

	// Should not panic
	require.NotPanics(t, func() {
		RegisterRecipeRoutes(api, handler)
	}, "RegisterRecipeRoutes must not panic")
}

func TestRecipeHandler_CreateRecipe_IdempotencyKey(t *testing.T) {
	ctx := context.Background()

	t.Run("calls idempotency repo when Idempotency-Key header is present", func(t *testing.T) {
		idempotencyRepo := &mockIdempotencyRepository{}
		svc := &mockRecipeService{}
		handler := NewRecipeHandler(svc, idempotencyRepo)

		input := &CreateRecipeInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
			IdempotencyInput:   inputs.IdempotencyInput{IdempotencyKey: "idem-key-abc"},
			Body:               validRecipeBody(),
		}

		output, err := handler.CreateRecipe(ctx, input)
		require.NoError(t, err)
		assert.NotNil(t, output)
		assert.Equal(t, 1, len(idempotencyRepo.getCalls), "idempotency repo should have been called once")
		assert.Equal(t, "idem-key-abc", idempotencyRepo.getCalls[0])
	})

	t.Run("returns cached result on second call with same Idempotency-Key", func(t *testing.T) {
		idempotencyRepo := &mockIdempotencyRepository{}
		svc := &mockRecipeService{}
		handler := NewRecipeHandler(svc, idempotencyRepo)

		input := &CreateRecipeInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
			IdempotencyInput:   inputs.IdempotencyInput{IdempotencyKey: "idem-key-xyz"},
			Body:               validRecipeBody(),
		}

		// First call — creates the recipe
		out1, err := handler.CreateRecipe(ctx, input)
		require.NoError(t, err)

		// Second call with same key — should use cache
		out2, err := handler.CreateRecipe(ctx, input)
		require.NoError(t, err)

		assert.Equal(t, out1.Body.ID, out2.Body.ID, "both responses must have same ID")
		assert.Equal(t, 1, svc.callCount, "recipe creation service must only be called once")
	})

	t.Run("bypasses idempotency repo when no Idempotency-Key provided", func(t *testing.T) {
		idempotencyRepo := &mockIdempotencyRepository{}
		svc := &mockRecipeService{}
		handler := NewRecipeHandler(svc, idempotencyRepo)

		input := &CreateRecipeInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
			// No IdempotencyInput / empty key
			Body: validRecipeBody(),
		}

		_, err := handler.CreateRecipe(ctx, input)
		require.NoError(t, err)
		assert.Equal(t, 0, len(idempotencyRepo.getCalls), "idempotency repo must NOT be called without a key")
	})

	t.Run("propagates service error through idempotency wrapper", func(t *testing.T) {
		idempotencyRepo := &mockIdempotencyRepository{}
		svc := &mockRecipeService{
			createFn: func(_ context.Context, _ string, _ domain.CreateMCPRecipeRequest) (*domain.MCPRecipe, error) {
				return nil, errors.New("service error")
			},
		}
		handler := NewRecipeHandler(svc, idempotencyRepo)

		input := &CreateRecipeInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
			IdempotencyInput:   inputs.IdempotencyInput{IdempotencyKey: "idem-key-err"},
			Body:               validRecipeBody(),
		}

		_, err := handler.CreateRecipe(ctx, input)
		assert.Error(t, err, "service errors must propagate")
		assert.Equal(t, 0, len(idempotencyRepo.cached), "failed creates must not be cached")
	})

	t.Run("works without idempotency repo (nil — spec gen path)", func(t *testing.T) {
		svc := &mockRecipeService{}
		handler := NewRecipeHandler(svc) // no idempotencyRepo — variadic empty

		input := &CreateRecipeInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
			IdempotencyInput:   inputs.IdempotencyInput{IdempotencyKey: "some-key"},
			Body:               validRecipeBody(),
		}

		_, err := handler.CreateRecipe(ctx, input)
		require.NoError(t, err)
		assert.Equal(t, 1, svc.callCount, "direct service call when no idempotency repo")
	})
}
