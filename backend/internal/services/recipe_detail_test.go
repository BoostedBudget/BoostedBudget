package services

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestRecipeServiceDetailValidation(t *testing.T) {
	for _, detail := range []string{"", "verbose", "compact", "full"} {
		for _, action := range []string{"create", "update", "update stored", "run"} {
			t.Run(action+"/"+detail, func(t *testing.T) {
				var spec []domain.MCPRecipeCall
				encoded, err := json.Marshal(detail)
				require.NoError(t, err)
				require.NoError(t, json.Unmarshal([]byte(`[{"operation_id":"getAccounts","detail":`+string(encoded)+`}]`), &spec))
				writes := 0
				repo := &mockRecipeRepository{
					getFn: func(context.Context, string, string) (*domain.MCPRecipe, error) {
						return &domain.MCPRecipe{Spec: spec}, nil
					},
					createFn: func(context.Context, *domain.MCPRecipe) error { writes++; return nil },
					updateFn: func(context.Context, *domain.MCPRecipe) error { writes++; return nil },
				}
				svc := NewRecipeService(repo)
				switch action {
				case "create":
					_, err = svc.Create(context.Background(), "u", domain.CreateMCPRecipeRequest{Spec: spec})
				case "update":
					_, err = svc.Update(context.Background(), "r", "u", domain.UpdateMCPRecipeRequest{Spec: spec})
				case "update stored":
					name := "renamed"
					_, err = svc.Update(context.Background(), "r", "u", domain.UpdateMCPRecipeRequest{Name: &name})
				case "run":
					_, err = svc.Run(context.Background(), "r", "u")
				}
				if detail == "" || detail == "verbose" {
					require.ErrorContains(t, err, "detail")
					require.Zero(t, writes)
				} else {
					require.NoError(t, err)
				}
			})
		}
	}
}
