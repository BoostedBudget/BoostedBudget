package operations

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// Issue #1569 Task 6 (#1579) — the annotation trail's read path.

type mockTransactionAnnotationHistoryService struct {
	mock.Mock
}

func (m *mockTransactionAnnotationHistoryService) GetUserAnnotationHistory(
	ctx context.Context, userID string, limit, offset int,
) ([]*domain.TransactionAnnotationChange, int, error) {
	args := m.Called(ctx, userID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Int(1), args.Error(2)
	}
	return args.Get(0).([]*domain.TransactionAnnotationChange), args.Int(1), args.Error(2)
}

var _ services.TransactionAnnotationHistoryService = (*mockTransactionAnnotationHistoryService)(nil)

func annotationChangeFixture() *domain.TransactionAnnotationChange {
	kind := "pfc"
	prev := "ENTERTAINMENT"
	reviewedAt := time.Date(2026, 3, 2, 9, 30, 0, 0, time.UTC)
	return &domain.TransactionAnnotationChange{
		ID:            "chg-1",
		UserID:        "user-1579",
		TransactionID: "tx-1",
		ChangeSource:  domain.TransactionAnnotationChangeCategory,
		Prior: domain.TransactionAnnotation{
			CategoryOverrideKind: &kind,
			PFCPrimaryOverride:   &prev,
			Reviewed:             true,
			ReviewedAt:           &reviewedAt,
		},
		New:       domain.TransactionAnnotation{Reviewed: true},
		ChangedAt: time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC),
	}
}

// TestGetUserTransactionAnnotationHistory_ReturnsBothHalvesOfTheChange. The whole point of #1579 is
// the BEFORE value, so a payload that carried only the after would satisfy a naive shape assertion
// and still ship the defect.
func TestGetUserTransactionAnnotationHistory_ReturnsBothHalvesOfTheChange(t *testing.T) {
	svc := new(mockTransactionAnnotationHistoryService)
	handler := NewTransactionAnnotationHistoryHandler(svc)

	svc.On("GetUserAnnotationHistory", mock.Anything, "user-1579", 20, 0).
		Return([]*domain.TransactionAnnotationChange{annotationChangeFixture()}, 1, nil)

	out, err := handler.GetUserTransactionAnnotationHistory(context.Background(),
		&GetUserTransactionAnnotationHistoryInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1579"},
			Limit:              20,
		})
	require.NoError(t, err)
	assert.Equal(t, 1, out.Body.Total)
	require.Len(t, out.Body.Changes, 1)

	got := out.Body.Changes[0]
	require.NotNil(t, got.Prior.PFCPrimaryOverride)
	assert.Equal(t, "ENTERTAINMENT", *got.Prior.PFCPrimaryOverride)
	require.NotNil(t, got.Prior.ReviewedAt, "the pre-existing reviewed_at is half of what #1579 loses")
	assert.Equal(t, domain.TransactionAnnotationChangeCategory, got.ChangeSource)

	// And it survives serialisation: a `prior` that JSON drops is the defect wearing a struct.
	encoded, err := json.Marshal(out.Body)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"prior"`)
	assert.Contains(t, string(encoded), `"ENTERTAINMENT"`)
	svc.AssertExpectations(t)
}

// TestGetUserTransactionAnnotationHistory_ScopesToTheAuthenticatedUser: the only user id reaching
// the service is the authenticated one. The input has no id field for anything else to arrive in.
func TestGetUserTransactionAnnotationHistory_ScopesToTheAuthenticatedUser(t *testing.T) {
	svc := new(mockTransactionAnnotationHistoryService)
	handler := NewTransactionAnnotationHistoryHandler(svc)

	svc.On("GetUserAnnotationHistory", mock.Anything, "user-owner", 20, 0).
		Return([]*domain.TransactionAnnotationChange{}, 0, nil)

	_, err := handler.GetUserTransactionAnnotationHistory(context.Background(),
		&GetUserTransactionAnnotationHistoryInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-owner"},
			Limit:              20,
		})
	require.NoError(t, err)

	require.NotEmpty(t, svc.Calls)
	for _, c := range svc.Calls {
		assert.Equal(t, "user-owner", c.Arguments.String(1),
			"the only tenancy filter is the authenticated user id")
	}
}

// TestGetUserTransactionAnnotationHistory_EmptyIsAnArrayNotNull — "nothing was ever overwritten"
// and "the field is missing" are different answers, and a client showing an undo affordance has to
// tell them apart.
func TestGetUserTransactionAnnotationHistory_EmptyIsAnArrayNotNull(t *testing.T) {
	svc := new(mockTransactionAnnotationHistoryService)
	handler := NewTransactionAnnotationHistoryHandler(svc)

	svc.On("GetUserAnnotationHistory", mock.Anything, "user-empty", 20, 0).Return(nil, 0, nil)

	out, err := handler.GetUserTransactionAnnotationHistory(context.Background(),
		&GetUserTransactionAnnotationHistoryInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-empty"},
			Limit:              20,
		})
	require.NoError(t, err)
	require.NotNil(t, out.Body.Changes)

	encoded, err := json.Marshal(out.Body)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"changes":[]`)
}

func TestGetUserTransactionAnnotationHistory_ServiceErrorIsMapped(t *testing.T) {
	svc := new(mockTransactionAnnotationHistoryService)
	handler := NewTransactionAnnotationHistoryHandler(svc)

	svc.On("GetUserAnnotationHistory", mock.Anything, "user-1579", 20, 0).
		Return(nil, 0, errors.New("database is on fire"))

	_, err := handler.GetUserTransactionAnnotationHistory(context.Background(),
		&GetUserTransactionAnnotationHistoryInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1579"},
			Limit:              20,
		})
	require.Error(t, err)
}

func TestGetUserTransactionAnnotationHistory_NilServiceIs500(t *testing.T) {
	handler := NewTransactionAnnotationHistoryHandler(nil)
	_, err := handler.GetUserTransactionAnnotationHistory(context.Background(),
		&GetUserTransactionAnnotationHistoryInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1579"},
			Limit:              20,
		})
	require.Error(t, err)
}

// TestTransactionAnnotationHistoryRoutes_PathIsReachableAndNotSwallowedByATransactionIDWildcard
// proves at the ROUTER level that the endpoint resolves. Every handler test above calls the method
// directly and never touches a router, so none of them is evidence that a request can arrive.
//
// The transaction routes are registered FIRST, on purpose: /api/v1/transactions/{id} is then in the
// tree before the history route, which is the adversarial order for humatest (registration-order
// sensitive, measured in cmd/server's TestRouterPrecedence_HumatestIsRegistrationOrderSensitive).
// A history path under /api/v1/transactions/ would be swallowed here; the hyphenated sibling cannot
// be.
//
// ⚠ Production is humaecho.NewV4, not humatest — do not read a humatest verdict as production
// routing. See TestRouterPrecedence_ProductionEchoAlwaysPrefersTheLiteralSegment.
func TestTransactionAnnotationHistoryRoutes_PathIsReachableAndNotSwallowedByATransactionIDWildcard(t *testing.T) {
	histSvc := new(mockTransactionAnnotationHistoryService)
	histSvc.On("GetUserAnnotationHistory", mock.Anything, "user-routing", 20, 0).
		Return([]*domain.TransactionAnnotationChange{annotationChangeFixture()}, 1, nil)

	// A spy on the WRONG handler, so a misrouted request fails with this test's own message rather
	// than an opaque 404 or a nil dereference.
	txSpy := &routingSpyTransactionService{
		mockTransactionService: mockTransactionService{getErr: errors.New("transaction not found")},
	}

	_, api := humatest.New(t)
	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		next(huma.WithValue(ctx, inputs.UserIDKey, "user-routing"))
	})

	RegisterTransactionRoutes(api, NewTransactionHandler(txSpy))
	RegisterTransactionAnnotationHistoryRoutes(api, NewTransactionAnnotationHistoryHandler(histSvc))

	resp := api.Get("/api/v1/transaction-annotation-history")
	require.Equal(t, 200, resp.Code, "body: %s", resp.Body.String())
	assert.Contains(t, resp.Body.String(), `"total":1`)

	assert.Zero(t, txSpy.getByIDCalls,
		"the annotation-history request was dispatched to GetTransactionByID — the literal path "+
			"was swallowed by the /transactions/{id} wildcard")
	histSvc.AssertExpectations(t)
}

// routingSpyTransactionService records whether the transaction-by-id handler ran, which is how this
// file tells a correctly routed request from one swallowed by a {param} sibling.
type routingSpyTransactionService struct {
	mockTransactionService
	getByIDCalls int
}

func (s *routingSpyTransactionService) GetTransactionByID(ctx context.Context, userID, transactionID string) (*domain.Transaction, error) {
	s.getByIDCalls++
	return s.mockTransactionService.GetTransactionByID(ctx, userID, transactionID)
}
