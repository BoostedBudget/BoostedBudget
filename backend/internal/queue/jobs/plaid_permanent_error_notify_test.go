package jobs

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/mockassert"
	"github.com/stretchr/testify/mock"
)

// mockItemErrorNotifier mocks ItemErrorNotifier (Issue #1609 P3-T2).
type mockItemErrorNotifier struct {
	mock.Mock
}

func (m *mockItemErrorNotifier) HandleLoginRequired(ctx context.Context, linkedItem *domain.LinkedItem, errorCode string, errorMessage string) error {
	args := m.Called(ctx, linkedItem, errorCode, errorMessage)
	return args.Error(0)
}

func (m *mockItemErrorNotifier) HandleError(ctx context.Context, linkedItem *domain.LinkedItem, errorCode string, errorMessage string) error {
	args := m.Called(ctx, linkedItem, errorCode, errorMessage)
	return args.Error(0)
}

func testLinkedItemForNotify() *domain.LinkedItem {
	return &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
		ItemID: "plaid-item-789",
	}
}

// TestNotifyPermanentPlaidError_InvalidAccessToken_RoutesToHandleLoginRequired asserts the
// mapping the brief specifies directly: INVALID_ACCESS_TOKEN means only the user can fix it,
// exactly like ITEM_LOGIN_REQUIRED, so it routes through the same handler.
func TestNotifyPermanentPlaidError_InvalidAccessToken_RoutesToHandleLoginRequired(t *testing.T) {
	ctx := context.Background()
	m := new(mockItemErrorNotifier)
	linkedItem := testLinkedItemForNotify()
	plaidErr := &domain.PlaidSyncError{ErrorType: domain.PlaidErrorPermanent, ErrorCode: "INVALID_ACCESS_TOKEN"}

	m.On("HandleLoginRequired", ctx, linkedItem, "INVALID_ACCESS_TOKEN", mock.AnythingOfType("string")).Return(nil)

	notifyPermanentPlaidError(ctx, m, slog.Default(), linkedItem, plaidErr)

	m.AssertExpectations(t)
	mockassert.NoCallTo(t, &m.Mock, "HandleError")
}

// TestNotifyPermanentPlaidError_ItemLoginRequired_RoutesToHandleLoginRequired covers the other
// permanent code that means the same thing.
func TestNotifyPermanentPlaidError_ItemLoginRequired_RoutesToHandleLoginRequired(t *testing.T) {
	ctx := context.Background()
	m := new(mockItemErrorNotifier)
	linkedItem := testLinkedItemForNotify()
	plaidErr := &domain.PlaidSyncError{ErrorType: domain.PlaidErrorPermanent, ErrorCode: "ITEM_LOGIN_REQUIRED"}

	m.On("HandleLoginRequired", ctx, linkedItem, "ITEM_LOGIN_REQUIRED", mock.AnythingOfType("string")).Return(nil)

	notifyPermanentPlaidError(ctx, m, slog.Default(), linkedItem, plaidErr)

	m.AssertExpectations(t)
	mockassert.NoCallTo(t, &m.Mock, "HandleError")
}

// TestNotifyPermanentPlaidError_ProductsNotSupported_DoesNotNotify is the brief's explicit
// exclusion: the connection is fine, only one product is unavailable, and there is no matching
// notification type. Must not call either handler.
func TestNotifyPermanentPlaidError_ProductsNotSupported_DoesNotNotify(t *testing.T) {
	ctx := context.Background()
	m := new(mockItemErrorNotifier)
	linkedItem := testLinkedItemForNotify()
	plaidErr := &domain.PlaidSyncError{ErrorType: domain.PlaidErrorPermanent, ErrorCode: "PRODUCTS_NOT_SUPPORTED"}

	notifyPermanentPlaidError(ctx, m, slog.Default(), linkedItem, plaidErr)

	mockassert.NoCallTo(t, &m.Mock, "HandleLoginRequired")
	mockassert.NoCallTo(t, &m.Mock, "HandleError")
}

// TestNotifyPermanentPlaidError_OtherPermanentCode_FallsBackToHandleError covers a future/unknown
// permanent code that isn't one of the two reauth codes or PRODUCTS_NOT_SUPPORTED — it must still
// notify (via the general handler) rather than silently notifying nothing.
func TestNotifyPermanentPlaidError_OtherPermanentCode_FallsBackToHandleError(t *testing.T) {
	ctx := context.Background()
	m := new(mockItemErrorNotifier)
	linkedItem := testLinkedItemForNotify()
	plaidErr := &domain.PlaidSyncError{ErrorType: domain.PlaidErrorPermanent, ErrorCode: "SOME_FUTURE_PERMANENT_CODE"}

	m.On("HandleError", ctx, linkedItem, "SOME_FUTURE_PERMANENT_CODE", mock.AnythingOfType("string")).Return(nil)

	notifyPermanentPlaidError(ctx, m, slog.Default(), linkedItem, plaidErr)

	m.AssertExpectations(t)
	mockassert.NoCallTo(t, &m.Mock, "HandleLoginRequired")
}

// TestNotifyPermanentPlaidError_NilNotifier_NoPanic covers a worker constructed without an
// ItemErrorSvc dependency (e.g. in a test, or a future deploy path) — must be a safe no-op, not a
// nil-pointer panic that would take down the whole job.
func TestNotifyPermanentPlaidError_NilNotifier_NoPanic(t *testing.T) {
	ctx := context.Background()
	linkedItem := testLinkedItemForNotify()
	plaidErr := &domain.PlaidSyncError{ErrorType: domain.PlaidErrorPermanent, ErrorCode: "ITEM_LOGIN_REQUIRED"}

	notifyPermanentPlaidError(ctx, nil, slog.Default(), linkedItem, plaidErr)
}

// TestNotifyPermanentPlaidError_HandlerError_IsNonBlocking asserts the brief's constraint
// directly: a notification failure must be logged, not propagated — notifyPermanentPlaidError
// has no return value, so the only observable behavior is that it does not panic and the mock
// expectation (that the handler WAS called) is satisfied.
func TestNotifyPermanentPlaidError_HandlerError_IsNonBlocking(t *testing.T) {
	ctx := context.Background()
	m := new(mockItemErrorNotifier)
	linkedItem := testLinkedItemForNotify()
	plaidErr := &domain.PlaidSyncError{ErrorType: domain.PlaidErrorPermanent, ErrorCode: "ITEM_LOGIN_REQUIRED"}

	m.On("HandleLoginRequired", ctx, linkedItem, "ITEM_LOGIN_REQUIRED", mock.AnythingOfType("string")).
		Return(errors.New("notification db error"))

	notifyPermanentPlaidError(ctx, m, slog.Default(), linkedItem, plaidErr)

	m.AssertExpectations(t)
}
