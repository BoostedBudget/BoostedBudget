//go:build integration

package operations

// Issue #1630, Task 0: reproduce (or strike) four claims from the issue against the REAL
// handler -> service -> repository -> domain.Validate() -> errors.MapError chain, using a
// live Postgres connection (not mocks). At the time task 0 wrote these tests this was
// evidence-gathering only — no production code was changed. Task 7 then fixed the root cause
// (domain.ErrInvalidBudget), which changed the P6 and P2 tests' expected status code from 500
// to 400; those two tests were updated in place (not left pinning the pre-fix defect) rather
// than duplicated, since the mechanism they exercise didn't change, only the status code did.
// See .superpowers/sdd/brainstorming-writing-plans-using-super-fancy-melody/
// task-0-report.md for the original findings table.
//
// Static reading at planning time (recorded in the task brief) already contradicted:
//   P5: "monthly budget without end_date -> 422"
//   P6: "match_logic:\"and\" -> 400"
//   P2: "IncomeSourceType \"category\" -> 422 but \"custom\" -> 500"
// Each is confirmed here at runtime by driving the handler with a real DB-backed service, and
// reading the exact status code the mapper actually attaches to the returned error via
// huma.StatusError, exactly as internal/huma/operations/admin_subscriptions_test.go and
// analytics_test.go already do for other status-code assertions in this package.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/huma/operations/... -tags integration \
//	  -run TestClaim1630 -p 1 -v

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/database"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// connectClaim1630DB returns a pool connected to DATABASE_URL, skipping the test when unset
// (mirrors connectTestDB/connectIntegrationDB duplicated per package elsewhere in this repo).
func connectClaim1630DB(t *testing.T) *pgxpool.Pool {
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

// seedClaim1630User inserts one user row inside tx and returns its id.
func seedClaim1630User(t *testing.T, tx pgx.Tx) string {
	t.Helper()
	ctx := context.Background()
	userID := uuid.New().String()
	_, err := tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID, "claim1630-"+userID+"@example.com")
	require.NoError(t, err, "seed user")
	return userID
}

// seedClaim1630Budget inserts a minimal budget row owned by userID and returns its id.
func seedClaim1630Budget(t *testing.T, tx pgx.Tx, userID string) string {
	t.Helper()
	ctx := context.Background()
	budgetID := uuid.New().String()
	startDate := time.Now()
	createdAt := time.Now()
	_, err := tx.Exec(ctx, `
		INSERT INTO budgets (id, user_id, name, period, start_date, amount, expected_income, currency_code, is_active, created_at, updated_at, series_id)
		VALUES ($1, $2, 'Fixture Budget', 'monthly', $3, 1000, 1000, 'USD', true, $4, $4, $1)
	`, budgetID, userID, startDate, createdAt)
	require.NoError(t, err, "seed budget")
	return budgetID
}

// testSlogLogger is a discard logger for wiring a real budgetService in these tests.
func testSlogLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// newClaim1630Handler wires a BudgetHandler with a REAL budgetService backed by the given
// Postgres transaction so domain.Validate() and the mapper both actually run.
func newClaim1630Handler(db repository.DB) *BudgetHandler {
	budgetRepo := repository.NewBudgetRepository(db)
	customCategoryRepo := repository.NewCustomCategoryRepository(db)
	svc := services.NewBudgetService(budgetRepo, customCategoryRepo, nil, testSlogLogger())
	syncSvc := &mockBudgetSyncService{}
	txSvc := services.NewTransactionService(&mockTransactionRepoForBudget{}, &mockCustomCategoryRepoForBudget{})
	return NewBudgetHandler(svc, syncSvc, txSvc)
}

// TestClaim1630_P5_MonthlyWithoutEndDate_DoesNotReproduce confirms the issue's claim that a
// monthly budget without end_date returns 422. domain/budget.go's Validate() only requires
// end_date for BudgetPeriodCustom (the other rule is merely "end_date must be after start_date
// IF SET"), so this is expected to SUCCEED, not 422.
func TestClaim1630_P5_MonthlyWithoutEndDate_DoesNotReproduce(t *testing.T) {
	pool := connectClaim1630DB(t)
	tx, err := pool.Begin(context.Background())
	require.NoError(t, err)
	defer tx.Rollback(context.Background()) //nolint:errcheck

	userID := seedClaim1630User(t, tx)
	handler := newClaim1630Handler(tx)

	input := &CreateBudgetInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		Body: domain.CreateBudgetRequest{
			Name:         "P5 Monthly No End Date",
			Period:       domain.BudgetPeriodMonthly,
			StartDate:    time.Now(),
			Amount:       1000,
			CurrencyCode: "USD",
			// EndDate deliberately omitted.
		},
	}

	out, err := handler.CreateBudget(context.Background(), input)

	// CLAIM: 422. ACTUAL: no error at all (budget created successfully).
	require.NoError(t, err, "P5 claim does NOT reproduce: monthly budget with no end_date creates successfully")
	require.NotNil(t, out)
	assert.Equal(t, "P5 Monthly No End Date", out.Body.Name)
}

// TestClaim1630_P6_MatchLogicAnd_EmptyTags_Is400AfterErrInvalidBudget confirms the issue's
// claim that match_logic:"and" returns 400. Per domain/budget.go BudgetCategory.Validate(),
// "and" is a legal MatchLogic value; the failure is that tags are required whenever
// match_logic is "tag_only" or "and". At the time task 0 wrote this test, that failure was a
// bare errors.New with no sentinel, which internal/huma/errors/mapper.go's switch did not
// recognize -> fell through to huma.Error500InternalServerError, so this test originally
// pinned 500 (see TestClaim1630_P6_MatchLogicAnd_EmptyTags_Is500NotStruct400 in git history).
// Task 7 (#1630) introduced domain.ErrInvalidBudget, which every Validate() failure in
// domain/budget.go now wraps, and mapper.go now maps it to 400 — so the claimed status code
// is now CORRECT, even though the issue's premise about WHY (tags required) was right and the
// mechanism (unwrapped error -> 500) is what needed fixing, not the target status code.
func TestClaim1630_P6_MatchLogicAnd_EmptyTags_Is400AfterErrInvalidBudget(t *testing.T) {
	pool := connectClaim1630DB(t)
	tx, err := pool.Begin(context.Background())
	require.NoError(t, err)
	defer tx.Rollback(context.Background()) //nolint:errcheck

	userID := seedClaim1630User(t, tx)
	handler := newClaim1630Handler(tx)
	budgetID := seedClaim1630Budget(t, tx, userID)

	input := &AddCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 budgetID,
	}
	input.Body = domain.CreateBudgetCategoryRequest{
		CategoryKind:    "pfc",
		PFCPrimary:      ptrStr("FOOD_AND_DRINK"),
		AllocatedAmount: 100,
		MatchLogic:      domain.MatchLogicAnd,
		// Tags deliberately omitted/empty.
	}

	out, err := handler.AddCategory(context.Background(), input)

	require.Error(t, err, "match_logic=and with empty tags must fail validation")
	assert.Nil(t, out)

	var statusErr huma.StatusError
	require.True(t, errors.As(err, &statusErr), "expected huma.StatusError, got %T: %v", err, err)
	// CLAIM: 400. ACTUAL (post #1630 task 7): 400.
	assert.Equal(t, http.StatusBadRequest, statusErr.GetStatus(),
		"post-task-7: empty tags + match_logic=and now maps to 400 via domain.ErrInvalidBudget, "+
			"not the pre-fix 500 (unwrapped errors.New falling through mapper's default case)")
	assert.Contains(t, statusErr.Error(), "tags", "the field name must be discoverable in the body")
}

// TestClaim1630_P6_MatchLogicAnd_WithTags_Succeeds is the control case: match_logic="and" is
// itself legal and succeeds once tags are supplied, proving "and" is not rejected outright
// (the issue's literal claim was "match_logic:\"and\" -> 400", which would imply "and" itself
// is invalid).
func TestClaim1630_P6_MatchLogicAnd_WithTags_Succeeds(t *testing.T) {
	pool := connectClaim1630DB(t)
	tx, err := pool.Begin(context.Background())
	require.NoError(t, err)
	defer tx.Rollback(context.Background()) //nolint:errcheck

	userID := seedClaim1630User(t, tx)
	handler := newClaim1630Handler(tx)
	budgetID := seedClaim1630Budget(t, tx, userID)

	input := &AddCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 budgetID,
	}
	input.Body = domain.CreateBudgetCategoryRequest{
		CategoryKind:    "pfc",
		PFCPrimary:      ptrStr("FOOD_AND_DRINK"),
		AllocatedAmount: 100,
		MatchLogic:      domain.MatchLogicAnd,
		Tags:            []string{"groceries"},
	}

	out, err := handler.AddCategory(context.Background(), input)

	require.NoError(t, err, "match_logic=and with tags present must succeed")
	require.NotNil(t, out)
	assert.Equal(t, domain.MatchLogicAnd, out.Body.MatchLogic)
}

// TestClaim1630_P2_IncomeSourceType_CategoryVsCustom_BothInvalid confirms the issue's claim
// that IncomeSourceType "category" -> 422 while "custom" -> 500. Both are invalid
// IncomeSourceType values (valid ones are pfc_primary/tag/manual); at the time task 0 wrote
// this test, IncomeSource.Validate() returned the identical bare errors.New("invalid income
// source type") for either string, which the mapper's switch did not recognize -> default ->
// 500 for BOTH, so this test originally pinned 500 for both (see git history). Task 7 (#1630)
// introduced domain.ErrInvalidBudget, which IncomeSource.Validate() now wraps, and mapper.go
// now maps it to 400 for both — the issue's differentiated 422/500 claim remains wrong (both
// STILL map to the identical code), but that code is now 400, not 500.
func TestClaim1630_P2_IncomeSourceType_CategoryVsCustom_BothInvalid(t *testing.T) {
	pool := connectClaim1630DB(t)

	makeInput := func(userID, badType string) *CreateBudgetInput {
		return &CreateBudgetInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
			Body: domain.CreateBudgetRequest{
				Name:         "P2 Income Source Type",
				Period:       domain.BudgetPeriodMonthly,
				StartDate:    time.Now(),
				Amount:       1000,
				CurrencyCode: "USD",
				IncomeSources: []domain.IncomeSource{
					{Type: domain.IncomeSourceType(badType), Value: "whatever"},
				},
			},
		}
	}

	for _, badType := range []string{"category", "custom"} {
		t.Run(badType, func(t *testing.T) {
			tx, err := pool.Begin(context.Background())
			require.NoError(t, err)
			defer tx.Rollback(context.Background()) //nolint:errcheck

			userID := seedClaim1630User(t, tx)
			handler := newClaim1630Handler(tx)

			out, err := handler.CreateBudget(context.Background(), makeInput(userID, badType))

			require.Error(t, err, "invalid income source type %q must fail validation", badType)
			assert.Nil(t, out)

			var statusErr huma.StatusError
			require.True(t, errors.As(err, &statusErr), "expected huma.StatusError, got %T: %v", err, err)
			// CLAIM: "category"->422, "custom"->500. ACTUAL (post #1630 task 7): both 400.
			assert.Equal(t, http.StatusBadRequest, statusErr.GetStatus(),
				"income source type %q must map to 400 via domain.ErrInvalidBudget (post-task-7), "+
					"not the pre-fix 500 — and must be IDENTICAL to the other invalid type", badType)
			assert.Contains(t, statusErr.Error(), "pfc_primary",
				"legal values must be discoverable in the error body")
		})
	}
}
