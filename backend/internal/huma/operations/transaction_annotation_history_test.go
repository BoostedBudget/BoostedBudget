package operations

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"
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

// ── The shipped description is this endpoint's only documentation (#1569 round 6, G-G) ──────────
//
// This endpoint has NO frontend renderer. Whatever the OpenAPI description says about
// change_source is the entire explanation any consumer gets, which makes an incomplete
// enumeration there a different class of defect from an incomplete Go comment.
//
// It shipped incomplete: the description named 'category' and 'review' and stopped, while the live
// CHECK constraint admits five values. Commit 238a69c0 corrected the domain COMMENT and left the
// PUBLISHED text untouched — the two drifted precisely because only one of them had a test.
//
// The expected set is DERIVED from the domain constants, not typed out here, so a sixth source
// fails this test on the day it is declared rather than on the day someone re-reads the prose.
// The domain constants are themselves pinned to the database by
// TestAnnotationChangeSourcesMatchTheLiveCheck, so the chain runs: pg_constraint -> domain
// constants -> this description.
func annotationChangeSourceValuesFromDomain(t *testing.T) []string {
	t.Helper()

	const domainFile = "../../domain/transaction_annotation_history.go"
	src, err := os.ReadFile(domainFile)
	require.NoError(t, err, "read %s", domainFile)

	// Matches `TransactionAnnotationChangeX = "value"` in a const block.
	re := regexp.MustCompile(`TransactionAnnotationChange\w+\s*=\s*"([^"]+)"`)
	matches := re.FindAllStringSubmatch(string(src), -1)

	var values []string
	for _, m := range matches {
		values = append(values, m[1])
	}

	// Fail closed: a rename would empty this set, and an empty set is satisfied by any description.
	require.GreaterOrEqual(t, len(values), 5,
		"expected at least the five known change sources in %s, found %v — re-point this guard "+
			"rather than letting it pass on an empty set", domainFile, values)
	return values
}

func TestAnnotationHistoryDescriptionEnumeratesEveryChangeSource(t *testing.T) {
	_, api := humatest.New(t)
	histSvc := new(mockTransactionAnnotationHistoryService)
	RegisterTransactionAnnotationHistoryRoutes(api, NewTransactionAnnotationHistoryHandler(histSvc))

	var description string
	for _, item := range api.OpenAPI().Paths {
		if item.Get != nil && item.Get.OperationID == "getUserTransactionAnnotationHistory" {
			description = item.Get.Description
		}
	}
	require.NotEmpty(t, description,
		"the operation was not found in the generated spec — this guard would otherwise pass vacuously")

	for _, source := range annotationChangeSourceValuesFromDomain(t) {
		assert.Contains(t, description, "'"+source+"'",
			"the published description does not mention change_source value %q. This endpoint has no "+
				"UI, so the description is the only place a consumer can learn the value exists.",
			source)
	}
}
