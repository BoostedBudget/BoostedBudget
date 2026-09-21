package services

// Recipe service tests (Issue #1087: MCP saved read-only workflow recipes)
// RED: These tests drive the recipe_service.go implementation.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

type mockRecipeRepository struct {
	createFn func(ctx context.Context, recipe *domain.MCPRecipe) error
	listFn   func(ctx context.Context, userID string) ([]domain.MCPRecipe, error)
	getFn    func(ctx context.Context, id string, userID string) (*domain.MCPRecipe, error)
	updateFn func(ctx context.Context, recipe *domain.MCPRecipe) error
	deleteFn func(ctx context.Context, id string, userID string) error
}

func (m *mockRecipeRepository) Create(ctx context.Context, recipe *domain.MCPRecipe) error {
	if m.createFn != nil {
		return m.createFn(ctx, recipe)
	}
	return nil
}

func (m *mockRecipeRepository) List(ctx context.Context, userID string) ([]domain.MCPRecipe, error) {
	if m.listFn != nil {
		return m.listFn(ctx, userID)
	}
	return nil, nil
}

func (m *mockRecipeRepository) Get(ctx context.Context, id string, userID string) (*domain.MCPRecipe, error) {
	if m.getFn != nil {
		return m.getFn(ctx, id, userID)
	}
	return nil, nil
}

func (m *mockRecipeRepository) Update(ctx context.Context, recipe *domain.MCPRecipe) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, recipe)
	}
	return nil
}

func (m *mockRecipeRepository) Delete(ctx context.Context, id string, userID string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id, userID)
	}
	return nil
}

func validRecipeSpec() []domain.MCPRecipeCall {
	return []domain.MCPRecipeCall{
		{OperationID: "getCashFlowSummary", Params: map[string]interface{}{"start_date": "2024-01-01", "end_date": "2024-01-31"}},
	}
}

func TestRecipeService_Create(t *testing.T) {
	ctx := context.Background()

	t.Run("creates a valid read-only recipe", func(t *testing.T) {
		var saved *domain.MCPRecipe
		repo := &mockRecipeRepository{
			createFn: func(_ context.Context, r *domain.MCPRecipe) error {
				saved = r
				return nil
			},
		}
		svc := NewRecipeService(repo)

		req := domain.CreateMCPRecipeRequest{
			Name: "Weekly Cash Flow",
			Spec: validRecipeSpec(),
		}
		created, err := svc.Create(ctx, "user-1", req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if created.UserID != "user-1" {
			t.Errorf("expected UserID user-1, got %q", created.UserID)
		}
		if created.Name != "Weekly Cash Flow" {
			t.Errorf("expected name 'Weekly Cash Flow', got %q", created.Name)
		}
		if saved == nil {
			t.Error("expected repo.Create to be called")
		}
	})

	t.Run("R4-4: rejects recipe with non-empty schedule (scheduling not available in v1)", func(t *testing.T) {
		repo := &mockRecipeRepository{}
		svc := NewRecipeService(repo)
		schedule := "0 9 * * 1" // weekly Monday 9am
		req := domain.CreateMCPRecipeRequest{
			Name:     "Weekly Summary",
			Spec:     validRecipeSpec(),
			Schedule: &schedule,
		}
		_, err := svc.Create(ctx, "user-1", req)
		if err == nil {
			t.Error("expected error: scheduled recipes are not available in v1")
		}
	})

	t.Run("rejects recipe with write-class operation", func(t *testing.T) {
		repo := &mockRecipeRepository{}
		svc := NewRecipeService(repo)

		req := domain.CreateMCPRecipeRequest{
			Name: "Bad Recipe",
			Spec: []domain.MCPRecipeCall{
				{OperationID: "createBudget", Body: map[string]interface{}{"name": "test"}},
			},
		}
		_, err := svc.Create(ctx, "user-1", req)
		if err == nil {
			t.Error("expected error: recipe spec must only contain read-class operations (v1)")
		}
	})

	t.Run("rejects recipe with excluded operation", func(t *testing.T) {
		repo := &mockRecipeRepository{}
		svc := NewRecipeService(repo)

		req := domain.CreateMCPRecipeRequest{
			Name: "Bad Recipe",
			Spec: []domain.MCPRecipeCall{
				{OperationID: "checkRole"},
			},
		}
		_, err := svc.Create(ctx, "user-1", req)
		if err == nil {
			t.Error("expected error: excluded operations not allowed in recipes")
		}
	})

	t.Run("rejects empty spec", func(t *testing.T) {
		repo := &mockRecipeRepository{}
		svc := NewRecipeService(repo)

		req := domain.CreateMCPRecipeRequest{Name: "Empty", Spec: nil}
		_, err := svc.Create(ctx, "user-1", req)
		if err == nil {
			t.Error("expected error for empty spec")
		}
	})

	t.Run("#1610 P3-T6: accepts recipes using read ops missing from the stale Go/TS mirror", func(t *testing.T) {
		// getMerchants, listMCPRecipes, getMyHousehold, getAccountBalanceHistory and 16 other
		// read-class TS-manifest operations were absent from the hand-maintained Go mirror
		// (backend/internal/domain/mcp_manifest.go), so validateSpec rejected them with
		// "operation ... is not available via MCP" even though they are read-only and valid.
		readOnlyButPreviouslyMissing := []string{
			"getMerchants",
			"listMCPRecipes",
			"getMyHousehold",
			"getAccountBalanceHistory",
		}
		for _, opID := range readOnlyButPreviouslyMissing {
			repo := &mockRecipeRepository{}
			svc := NewRecipeService(repo)
			req := domain.CreateMCPRecipeRequest{
				Name: "Recipe using " + opID,
				Spec: []domain.MCPRecipeCall{{OperationID: opID}},
			}
			if _, err := svc.Create(ctx, "user-1", req); err != nil {
				t.Errorf("operation %q: expected read-only recipe to be accepted, got error: %v", opID, err)
			}
		}
	})

	t.Run("propagates repository error", func(t *testing.T) {
		repo := &mockRecipeRepository{
			createFn: func(_ context.Context, _ *domain.MCPRecipe) error {
				return errors.New("db error")
			},
		}
		svc := NewRecipeService(repo)
		req := domain.CreateMCPRecipeRequest{Name: "Recipe", Spec: validRecipeSpec()}
		_, err := svc.Create(ctx, "user-1", req)
		if err == nil {
			t.Error("expected error from repository")
		}
	})
}

func TestRecipeService_ErrorSentinels(t *testing.T) {
	ctx := context.Background()

	strPtr := func(s string) *string { return &s }

	t.Run("invalid spec failures carry ErrInvalidRecipeSpec", func(t *testing.T) {
		cases := []struct {
			name string
			spec []domain.MCPRecipeCall
		}{
			{"empty spec", nil},
			{"bad detail", []domain.MCPRecipeCall{{OperationID: "getCashFlowSummary", Detail: strPtr("verbose")}}},
			{"excluded operation", []domain.MCPRecipeCall{{OperationID: "checkRole"}}},
			{"write-class operation", []domain.MCPRecipeCall{{OperationID: "createBudget", Body: map[string]interface{}{"name": "x"}}}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				svc := NewRecipeService(&mockRecipeRepository{})
				_, err := svc.Create(ctx, "user-1", domain.CreateMCPRecipeRequest{Name: "R", Spec: tc.spec})
				if err == nil {
					t.Fatal("expected error")
				}
				if !errors.Is(err, domain.ErrInvalidRecipeSpec) {
					t.Errorf("expected ErrInvalidRecipeSpec, got %v", err)
				}
				if errors.Is(err, domain.ErrRecipeNotFound) {
					t.Errorf("invalid-spec error must not match ErrRecipeNotFound: %v", err)
				}
			})
		}
	})

	t.Run("schedule rejection carries ErrInvalidRecipeSpec", func(t *testing.T) {
		schedule := "0 9 * * 1"
		svc := NewRecipeService(&mockRecipeRepository{})
		_, err := svc.Create(ctx, "user-1", domain.CreateMCPRecipeRequest{Name: "R", Spec: validRecipeSpec(), Schedule: &schedule})
		if !errors.Is(err, domain.ErrInvalidRecipeSpec) {
			t.Errorf("expected ErrInvalidRecipeSpec for schedule, got %v", err)
		}
	})

	t.Run("Update on missing recipe carries ErrRecipeNotFound", func(t *testing.T) {
		svc := NewRecipeService(&mockRecipeRepository{getFn: func(_ context.Context, _ string, _ string) (*domain.MCPRecipe, error) {
			return nil, nil
		}})
		_, err := svc.Update(ctx, "missing", "user-1", domain.UpdateMCPRecipeRequest{})
		if !errors.Is(err, domain.ErrRecipeNotFound) {
			t.Errorf("expected ErrRecipeNotFound, got %v", err)
		}
	})

	t.Run("Run on missing recipe carries ErrRecipeNotFound", func(t *testing.T) {
		svc := NewRecipeService(&mockRecipeRepository{getFn: func(_ context.Context, _ string, _ string) (*domain.MCPRecipe, error) {
			return nil, nil
		}})
		_, err := svc.Run(ctx, "missing", "user-1")
		if !errors.Is(err, domain.ErrRecipeNotFound) {
			t.Errorf("expected ErrRecipeNotFound, got %v", err)
		}
	})

	t.Run("Run on stored write-class spec carries ErrInvalidRecipeSpec", func(t *testing.T) {
		svc := NewRecipeService(&mockRecipeRepository{getFn: func(_ context.Context, id string, _ string) (*domain.MCPRecipe, error) {
			return &domain.MCPRecipe{ID: id, UserID: "user-1", Name: "Stale", Spec: []domain.MCPRecipeCall{{OperationID: "createBudget"}}}, nil
		}})
		_, err := svc.Run(ctx, "stale", "user-1")
		if !errors.Is(err, domain.ErrInvalidRecipeSpec) {
			t.Errorf("expected ErrInvalidRecipeSpec, got %v", err)
		}
	})
}

func TestRecipeService_Update(t *testing.T) {
	ctx := context.Background()

	t.Run("R4-4: rejects update with non-empty schedule", func(t *testing.T) {
		repo := &mockRecipeRepository{
			getFn: func(_ context.Context, id string, _ string) (*domain.MCPRecipe, error) {
				return &domain.MCPRecipe{ID: id, UserID: "user-1", Name: "R", Spec: validRecipeSpec()}, nil
			},
		}
		svc := NewRecipeService(repo)
		schedule := "0 9 * * 1"
		_, err := svc.Update(ctx, "r1", "user-1", domain.UpdateMCPRecipeRequest{Schedule: &schedule})
		if err == nil {
			t.Error("expected error: scheduled recipes are not available in v1")
		}
	})
}

func TestRecipeService_List(t *testing.T) {
	ctx := context.Background()

	t.Run("returns user recipes", func(t *testing.T) {
		repo := &mockRecipeRepository{
			listFn: func(_ context.Context, userID string) ([]domain.MCPRecipe, error) {
				if userID != "user-1" {
					t.Errorf("expected user-1, got %q", userID)
				}
				return []domain.MCPRecipe{
					{ID: "r1", UserID: "user-1", Name: "Recipe 1"},
					{ID: "r2", UserID: "user-1", Name: "Recipe 2"},
				}, nil
			},
		}
		svc := NewRecipeService(repo)
		list, err := svc.List(ctx, "user-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(list) != 2 {
			t.Errorf("expected 2 recipes, got %d", len(list))
		}
	})
}

func TestRecipeService_Delete(t *testing.T) {
	ctx := context.Background()

	t.Run("deletes existing recipe", func(t *testing.T) {
		deleted := false
		repo := &mockRecipeRepository{
			deleteFn: func(_ context.Context, id string, userID string) error {
				if id != "r1" || userID != "user-1" {
					t.Errorf("unexpected args: id=%q userID=%q", id, userID)
				}
				deleted = true
				return nil
			},
		}
		svc := NewRecipeService(repo)
		err := svc.Delete(ctx, "r1", "user-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !deleted {
			t.Error("expected delete to be called")
		}
	})
}

func TestRecipeService_Run(t *testing.T) {
	ctx := context.Background()

	t.Run("run returns recipe spec for execution", func(t *testing.T) {
		repo := &mockRecipeRepository{
			getFn: func(_ context.Context, id string, _ string) (*domain.MCPRecipe, error) {
				if id != "r1" {
					t.Errorf("expected id r1, got %q", id)
				}
				return &domain.MCPRecipe{
					ID:        "r1",
					UserID:    "user-1",
					Name:      "Weekly Summary",
					Spec:      validRecipeSpec(),
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}, nil
			},
		}
		svc := NewRecipeService(repo)
		result, err := svc.Run(ctx, "r1", "user-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Calls) == 0 {
			t.Error("expected spec calls in run result")
		}
		if result.Calls[0].OperationID != "getCashFlowSummary" {
			t.Errorf("expected getCashFlowSummary, got %q", result.Calls[0].OperationID)
		}
	})

	t.Run("GAP-R3-10: rejects stored recipe that contains a write-class op at run time", func(t *testing.T) {
		// Defense-in-depth: even if a recipe was saved before a manifest change, Run()
		// must re-validate and reject any spec containing a write-class operation.
		// This prevents a stored recipe from bypassing the v1 read-only constraint.
		repo := &mockRecipeRepository{
			getFn: func(_ context.Context, id string, _ string) (*domain.MCPRecipe, error) {
				return &domain.MCPRecipe{
					ID:     id,
					UserID: "user-1",
					Name:   "Stale Recipe",
					// Stored spec contains a write-class op (e.g. saved before manifest change)
					Spec: []domain.MCPRecipeCall{
						{OperationID: "createBudget", Body: map[string]interface{}{"name": "bad"}},
					},
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}, nil
			},
		}
		svc := NewRecipeService(repo)
		_, err := svc.Run(ctx, "stale-recipe", "user-1")
		if err == nil {
			t.Error("expected Run to reject a recipe spec containing a write-class operation")
		}
	})

	t.Run("GAP-R3-10: rejects stored recipe that contains an excluded op at run time", func(t *testing.T) {
		repo := &mockRecipeRepository{
			getFn: func(_ context.Context, id string, _ string) (*domain.MCPRecipe, error) {
				return &domain.MCPRecipe{
					ID:     id,
					UserID: "user-1",
					Name:   "Excluded Op Recipe",
					Spec: []domain.MCPRecipeCall{
						{OperationID: "checkRole"}, // excluded admin op
					},
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}, nil
			},
		}
		svc := NewRecipeService(repo)
		_, err := svc.Run(ctx, "excluded-recipe", "user-1")
		if err == nil {
			t.Error("expected Run to reject a recipe spec containing an excluded operation")
		}
	})
}
