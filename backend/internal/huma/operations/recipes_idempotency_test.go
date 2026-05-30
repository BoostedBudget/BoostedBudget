package operations

// Issue #1087 NEW-GAP-B: Idempotency wiring for createMCPRecipe
// Tests that the Idempotency-Key header is honoured by the recipe handler.

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
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
) error {
	m.getCalls = append(m.getCalls, key)
	if existing, ok := m.cached[key]; ok {
		*(result.(**domain.MCPRecipe)) = existing
		return nil
	}
	res, err := fn()
	if err != nil {
		return err
	}
	recipe := res.(*domain.MCPRecipe)
	if m.cached == nil {
		m.cached = make(map[string]*domain.MCPRecipe)
	}
	m.cached[key] = recipe
	*(result.(**domain.MCPRecipe)) = recipe
	return nil
}

func (m *mockIdempotencyRepository) GetOrCreateInTx(
	ctx context.Context,
	_ pgx.Tx,
	key, userID, operationID string,
	fn func() (interface{}, error),
	result interface{},
) (bool, error) {
	err := m.GetOrCreate(ctx, key, userID, operationID, fn, result)
	return false, err
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
