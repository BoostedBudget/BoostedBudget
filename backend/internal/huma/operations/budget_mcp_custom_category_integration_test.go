//go:build integration

package operations

// Integration tests for the MCP CreateBudget path's custom-category ownership check
// (Issue #1531 / #1540 follow-up).
//
// budget.go's MCP-transactional branch previously constructed txBudgetService with a nil
// CustomCategoryRepository (services.NewBudgetService(txBudgetRepo, nil, nil, ...)). Before
// the #1540 IDOR guard was added, CreateBudget never touched that repo, so nil was safe. Once
// the guard started calling s.customCategoryRepo.GetNamesByIDs unconditionally whenever a
// request included a custom_category_id, the MCP path began nil-dereferencing (panic) instead
// of running the ownership check — the same class of bug the comment three lines below (R5-1,
// for goalOwnershipChecker) explicitly warns against.
//
// These tests exercise the real handler against a live Postgres transaction (not a mock),
// because the defect and its fix live in what gets constructed and passed into
// services.NewBudgetService inside budget.go — a mock repo would mask the nil exactly like the
// production wiring did.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/huma/operations/... -tags integration -p 1 -run TestCreateBudget_MCPOrigin_CustomCategory -v

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/database"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// connectBudgetIntegrationDB returns a pool connected to DATABASE_URL, skipping the test
// when it's unset (mirrors the connectTestDB/connectIntegrationDB helper duplicated per
// package elsewhere in this repo — see internal/repository and internal/services).
func connectBudgetIntegrationDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	pool, err := database.NewPoolFromURL(context.Background(), dsn)
	require.NoError(t, err, "connect to test database")
	t.Cleanup(pool.Close)
	return pool
}

// seedBudgetMCPCategoryFixture creates two users and one custom_categories row owned by
// the first, returning (ownerUserID, otherUserID, ownedCustomCategoryID).
func seedBudgetMCPCategoryFixture(t *testing.T, pool *pgxpool.Pool) (string, string, string) {
	t.Helper()
	ctx := context.Background()

	ownerID := uuid.New()
	otherID := uuid.New()
	for _, u := range []uuid.UUID{ownerID, otherID} {
		_, err := pool.Exec(ctx, `
			INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
			VALUES ($1, $2, $3, 'premium', 'active', 'iad')
		`, u, "test|"+u.String(), "int-mcp-budget-"+u.String()+"@example.com")
		require.NoError(t, err, "seed user %s", u)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1)`, []uuid.UUID{ownerID, otherID})
	})

	ownedCategoryID := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO custom_categories (id, user_id, name)
		VALUES ($1, $2, 'Owned MCP Category')
	`, ownedCategoryID, ownerID)
	require.NoError(t, err, "seed owned custom category")

	return ownerID.String(), otherID.String(), ownedCategoryID.String()
}

// newMCPCreateBudgetInput builds a CreateBudgetInput carrying valid MCP-origin headers
// and a single "custom" category pointing at customCategoryID.
func newMCPCreateBudgetInput(userID, workerSecret, customCategoryID string) *CreateBudgetInput {
	return &CreateBudgetInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		MCPContextInput:    inputs.MCPContextInput{MCPGrantID: "grant-1531", MCPWorkerSecret: workerSecret},
		Body: domain.CreateBudgetRequest{
			Name:      "MCP Custom Category Budget",
			Period:    domain.BudgetPeriodMonthly,
			StartDate: domain.FlexibleDate(time.Now()),
			Amount:    1000,
			Categories: []domain.CreateBudgetCategoryRequest{
				{
					CategoryKind:     "custom",
					CustomCategoryID: &customCategoryID,
					AllocatedAmount:  100,
				},
			},
		},
	}
}

// TestCreateBudget_MCPOrigin_CustomCategory_Owned verifies that an MCP-origin CreateBudget
// carrying a custom_category_id the caller actually owns succeeds after the fix (R5-1-style
// wiring: the MCP branch must construct a real CustomCategoryRepository over the tx, not nil).
// Before the fix this panics (nil s.customCategoryRepo dereference in budget_service.go).
func TestCreateBudget_MCPOrigin_CustomCategory_Owned(t *testing.T) {
	pool := connectBudgetIntegrationDB(t)
	ownerID, _, ownedCategoryID := seedBudgetMCPCategoryFixture(t, pool)

	const workerSecret = "test-worker-secret-1531"
	svc := &mockBudgetService{}
	syncSvc := &mockBudgetSyncService{}
	txSvc := services.NewTransactionService(&mockTransactionRepoForBudget{}, &mockCustomCategoryRepoForBudget{})

	handler := NewBudgetHandler(svc, syncSvc, txSvc, BudgetHandlerDeps{
		DB:              pool,
		MCPAuditRepo:    &mockMCPAuditRepositoryForBudget{},
		MCPWorkerSecret: workerSecret,
	})

	input := newMCPCreateBudgetInput(ownerID, workerSecret, ownedCategoryID)

	out, err := handler.CreateBudget(context.Background(), input)

	require.NoError(t, err, "an owned custom_category_id must succeed on the MCP path")
	require.NotNil(t, out)
	require.NotNil(t, out.Body)
	assert.Equal(t, ownerID, out.Body.UserID)
}

// TestCreateBudget_MCPOrigin_CustomCategory_Unowned is the security half (brief case 2): an
// MCP-origin CreateBudget referencing a custom_category_id owned by a DIFFERENT user must be
// rejected with domain.ErrCategoryNotFound — not merely "not panic". Before the fix this
// panics; a nil-guard-only fix would make it succeed (silent IDOR), which is exactly the wrong
// fix the brief calls out. Asserting the exact error value proves the ownership check actually
// ran on the MCP path, rather than the call having been skipped.
func TestCreateBudget_MCPOrigin_CustomCategory_Unowned(t *testing.T) {
	pool := connectBudgetIntegrationDB(t)
	ownerID, otherID, ownedCategoryID := seedBudgetMCPCategoryFixture(t, pool)
	_ = ownerID

	const workerSecret = "test-worker-secret-1531"
	svc := &mockBudgetService{}
	syncSvc := &mockBudgetSyncService{}
	txSvc := services.NewTransactionService(&mockTransactionRepoForBudget{}, &mockCustomCategoryRepoForBudget{})

	handler := NewBudgetHandler(svc, syncSvc, txSvc, BudgetHandlerDeps{
		DB:              pool,
		MCPAuditRepo:    &mockMCPAuditRepositoryForBudget{},
		MCPWorkerSecret: workerSecret,
	})

	// The caller is otherID; ownedCategoryID is owned by ownerID, NOT otherID — unowned.
	input := newMCPCreateBudgetInput(otherID, workerSecret, ownedCategoryID)

	out, err := handler.CreateBudget(context.Background(), input)

	require.Error(t, err, "an unowned custom_category_id must be rejected on the MCP path")
	assert.Nil(t, out)

	// Assert the specific mapped error (domain.ErrCategoryNotFound -> 404 "Category not found",
	// internal/huma/errors/mapper.go), not merely that CreateBudget returned *an* error. This is
	// what proves the #1540 ownership check actually ran on the MCP path — a bare nil-guard
	// "fix" that skips the check instead would make this request succeed (silent IDOR), which
	// would fail this specific assertion even though it would stop the panic.
	var humaErr huma.StatusError
	require.ErrorAs(t, err, &humaErr, "expected a mapped huma.StatusError, not a raw/unmapped error")
	assert.Equal(t, http.StatusNotFound, humaErr.GetStatus())
	assert.Contains(t, humaErr.Error(), "Category not found")
}
