package operations

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockItemErrorServiceForReconnect is a mock implementation of services.ItemErrorService,
// scoped to the reconnect-flow tests (Issue #1609). The Handle*/ProcessWebhook methods are never
// exercised by PlaidReconnectHandler and simply panic if called.
type mockItemErrorServiceForReconnect struct {
	reconnectLinkToken    *domain.ReconnectLinkTokenResponse
	reconnectLinkTokenErr error
	repairErr             error

	lastReconnectUserID string
	lastReconnectItemID string
	lastRepairUserID    string
	lastRepairItemID    string
	lastRepairNonce     string
}

func (m *mockItemErrorServiceForReconnect) HandleLoginRequired(ctx context.Context, linkedItem *domain.LinkedItem, errorCode string, errorMessage string) error {
	panic("not used by PlaidReconnectHandler")
}

func (m *mockItemErrorServiceForReconnect) HandleError(ctx context.Context, linkedItem *domain.LinkedItem, errorCode string, errorMessage string) error {
	panic("not used by PlaidReconnectHandler")
}

func (m *mockItemErrorServiceForReconnect) HandlePendingExpiration(ctx context.Context, linkedItem *domain.LinkedItem, errorCode string, errorMessage string) error {
	panic("not used by PlaidReconnectHandler")
}

func (m *mockItemErrorServiceForReconnect) HandlePermissionRevoked(ctx context.Context, linkedItem *domain.LinkedItem, errorCode string, errorMessage string) error {
	panic("not used by PlaidReconnectHandler")
}

func (m *mockItemErrorServiceForReconnect) ProcessWebhook(ctx context.Context, itemID string, errorType string, errorCode string, errorMessage string, receivedAt time.Time) error {
	panic("not used by PlaidReconnectHandler")
}

func (m *mockItemErrorServiceForReconnect) HandleLoginRepaired(ctx context.Context, linkedItem *domain.LinkedItem, webhookReceivedAt time.Time) error {
	panic("not used by PlaidReconnectHandler")
}

func (m *mockItemErrorServiceForReconnect) CreateReconnectLinkToken(ctx context.Context, userID string, itemID string) (*domain.ReconnectLinkTokenResponse, error) {
	m.lastReconnectUserID = userID
	m.lastReconnectItemID = itemID
	if m.reconnectLinkTokenErr != nil {
		return nil, m.reconnectLinkTokenErr
	}
	return m.reconnectLinkToken, nil
}

func (m *mockItemErrorServiceForReconnect) RepairItem(ctx context.Context, userID string, itemID string, repairNonce string) error {
	m.lastRepairUserID = userID
	m.lastRepairItemID = itemID
	m.lastRepairNonce = repairNonce
	return m.repairErr
}

var _ services.ItemErrorService = (*mockItemErrorServiceForReconnect)(nil)

func TestCreateReconnectLinkToken_HandlerSuccess(t *testing.T) {
	mockSvc := &mockItemErrorServiceForReconnect{
		reconnectLinkToken: &domain.ReconnectLinkTokenResponse{LinkToken: "link-token-fresh", RepairNonce: "raw-nonce-abc"},
	}
	handler := NewPlaidReconnectHandler(mockSvc)

	input := &CreateReconnectLinkTokenInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body.ItemID = "plaid-item-789"

	output, err := handler.CreateReconnectLinkToken(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "link-token-fresh", output.Body.LinkToken)
	// Issue #1620: the raw repair nonce must reach the client — it is the only time it exists
	// outside this response, and RepairItem refuses without it.
	assert.Equal(t, "raw-nonce-abc", output.Body.RepairNonce)
	assert.Equal(t, "user-123", mockSvc.lastReconnectUserID)
	assert.Equal(t, "plaid-item-789", mockSvc.lastReconnectItemID)
}

func TestCreateReconnectLinkToken_HandlerNotFoundMapsTo404(t *testing.T) {
	mockSvc := &mockItemErrorServiceForReconnect{
		reconnectLinkTokenErr: repository.ErrLinkedItemNotFound,
	}
	handler := NewPlaidReconnectHandler(mockSvc)

	input := &CreateReconnectLinkTokenInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body.ItemID = "unknown-item"

	output, err := handler.CreateReconnectLinkToken(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Equal(t, humaerrors.MapError(repository.ErrLinkedItemNotFound).Error(), err.Error())
}

func TestCreateReconnectLinkToken_HandlerNotRepairableMapsTo409(t *testing.T) {
	mockSvc := &mockItemErrorServiceForReconnect{
		reconnectLinkTokenErr: services.ErrLinkedItemNotRepairable,
	}
	handler := NewPlaidReconnectHandler(mockSvc)

	input := &CreateReconnectLinkTokenInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body.ItemID = "plaid-item-789"

	output, err := handler.CreateReconnectLinkToken(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Equal(t, humaerrors.MapError(services.ErrLinkedItemNotRepairable).Error(), err.Error())
}

func TestRepairLinkedItem_HandlerSuccess(t *testing.T) {
	mockSvc := &mockItemErrorServiceForReconnect{}
	handler := NewPlaidReconnectHandler(mockSvc)

	input := &RepairLinkedItemInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body.ItemID = "plaid-item-789"
	input.Body.RepairNonce = "raw-nonce-abc"

	output, err := handler.RepairLinkedItem(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.True(t, output.Body.Success)
	assert.Equal(t, "user-123", mockSvc.lastRepairUserID)
	assert.Equal(t, "plaid-item-789", mockSvc.lastRepairItemID)
	// Issue #1620: the handler must forward the nonce verbatim. A handler that silently dropped
	// it would leave every repair refused at the service layer, or — worse, if the service ever
	// treated an empty nonce as "not provided, allow" — reopen the hole entirely.
	assert.Equal(t, "raw-nonce-abc", mockSvc.lastRepairNonce)
}

func TestRepairLinkedItem_HandlerErrorMapsThroughLoggingMapError(t *testing.T) {
	mockSvc := &mockItemErrorServiceForReconnect{
		repairErr: services.ErrLinkedItemNotRepairable,
	}
	handler := NewPlaidReconnectHandler(mockSvc)

	input := &RepairLinkedItemInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body.ItemID = "plaid-item-789"

	output, err := handler.RepairLinkedItem(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Equal(t, humaerrors.MapError(services.ErrLinkedItemNotRepairable).Error(), err.Error())
}
