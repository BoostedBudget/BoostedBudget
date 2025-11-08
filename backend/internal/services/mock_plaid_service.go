// Package services provides business logic and external service integrations.
// This file implements MockPlaidService for E2E testing without real Plaid API calls.
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/plaid/plaid-go/v23/plaid"
)

// mockPlaidService implements PlaidService interface for E2E testing.
// It generates synthetic transaction data without calling the real Plaid API.
//
// Usage:
//
//	service := NewMockPlaidService(cfg)
//	response, err := service.TransactionsSync(ctx, accessToken, cursor, days)
//
// Issue #331: E2E Test Scenarios Framework
type mockPlaidService struct {
	config *config.Config
}

// NewMockPlaidService creates a new mock Plaid service for E2E testing.
// This service generates synthetic transaction data based on tier limits without
// calling the real Plaid API.
//
// Parameters:
//   - cfg: Application configuration (used for tier limits)
//
// Returns a PlaidService implementation that can be used in place of the real
// Plaid service during E2E tests.
//
// Issue #331: E2E Test Scenarios Framework
func NewMockPlaidService(cfg *config.Config) PlaidService {
	return &mockPlaidService{
		config: cfg,
	}
}

// GetClient returns nil for mock service (not needed for testing).
// This satisfies the PlaidService interface but isn't used in E2E tests.
func (m *mockPlaidService) GetClient() *plaid.APIClient {
	return nil
}

// CreateLinkToken generates a mock link token for E2E testing.
// Returns a synthetic link token without calling the real Plaid API.
func (m *mockPlaidService) CreateLinkToken(ctx context.Context, userID string) (*domain.LinkTokenResponse, error) {
	// Generate mock link token
	return &domain.LinkTokenResponse{
		LinkToken:  fmt.Sprintf("mock-link-token-%s-%d", userID, time.Now().Unix()),
		Expiration: time.Now().Add(30 * time.Minute),
		RequestID:  fmt.Sprintf("mock-request-%d", time.Now().Unix()),
	}, nil
}

// CreateUpdateModeLinkToken generates a mock update mode link token for E2E testing.
// Returns a synthetic link token without calling the real Plaid API.
func (m *mockPlaidService) CreateUpdateModeLinkToken(ctx context.Context, userID string, accessToken string) (*domain.LinkTokenResponse, error) {
	// Validate parameters
	if userID == "" {
		return nil, fmt.Errorf("user ID cannot be empty")
	}
	if accessToken == "" {
		return nil, fmt.Errorf("access token cannot be empty")
	}

	// Generate mock update mode link token
	return &domain.LinkTokenResponse{
		LinkToken:  fmt.Sprintf("mock-update-link-token-%s-%d", userID, time.Now().Unix()),
		Expiration: time.Now().Add(30 * time.Minute),
		RequestID:  fmt.Sprintf("mock-request-%d", time.Now().Unix()),
	}, nil
}

// RemoveItem simulates removing a Plaid item (no-op for mock).
// Returns success without calling the real Plaid API.
func (m *mockPlaidService) RemoveItem(ctx context.Context, accessToken string) error {
	// Validate access token
	if accessToken == "" {
		return fmt.Errorf("access token cannot be empty")
	}

	// Mock: Always succeeds
	return nil
}

// TransactionsSync generates synthetic transaction data for E2E testing.
// Creates mock transactions based on tier limits without calling the real Plaid API.
//
// Parameters:
//   - ctx: Context for cancellation
//   - accessToken: Access token (used to identify item/user in tests)
//   - cursor: Pagination cursor (empty for first sync)
//   - daysRequested: Historical depth (30/365/730 based on tier)
//
// Returns a mock TransactionsSyncResponse with generated transactions matching
// the tier's historical depth limit.
//
// Issue #331: E2E Test Scenarios Framework
func (m *mockPlaidService) TransactionsSync(
	ctx context.Context,
	accessToken string,
	cursor string,
	daysRequested int,
) (*plaid.TransactionsSyncResponse, error) {
	// Validate access token
	if accessToken == "" {
		return nil, fmt.Errorf("access token cannot be empty")
	}

	// For mock, generate transactions based on daysRequested
	// This respects tier limits: free=30 days, premium=365 days, etc.
	transactions := m.generateMockTransactions(daysRequested)

	// Build mock response
	nextCursor := fmt.Sprintf("mock-cursor-%d", time.Now().Unix())
	hasMore := false
	requestId := fmt.Sprintf("mock-request-%d", time.Now().Unix())

	response := &plaid.TransactionsSyncResponse{
		Added:      transactions,
		Modified:   []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{},
		NextCursor: nextCursor,
		HasMore:    hasMore,
		RequestId:  requestId,
	}

	return response, nil
}

// FireWebhook simulates firing a Plaid webhook for E2E testing.
// Returns success without calling the real Plaid API.
//
// Parameters:
//   - ctx: Context for cancellation
//   - accessToken: Access token (identifies the item)
//   - webhookCode: Webhook code (INITIAL_UPDATE, DEFAULT_UPDATE, etc.)
//
// Returns a mock webhook response indicating success.
//
// Issue #331: E2E Test Scenarios Framework
func (m *mockPlaidService) FireWebhook(
	ctx context.Context,
	accessToken string,
	webhookCode string,
) (*plaid.SandboxItemFireWebhookResponse, error) {
	// Validate parameters
	if accessToken == "" {
		return nil, fmt.Errorf("access token cannot be empty")
	}
	if webhookCode == "" {
		return nil, fmt.Errorf("webhook code cannot be empty")
	}

	// Build mock response
	webhookFired := true
	requestId := fmt.Sprintf("mock-request-%d", time.Now().Unix())

	response := &plaid.SandboxItemFireWebhookResponse{
		WebhookFired: webhookFired,
		RequestId:    requestId,
	}

	return response, nil
}

// generateMockTransactions creates synthetic transaction data for testing.
// Generates one transaction per day for the specified number of days.
//
// Parameters:
//   - days: Number of days of transaction history to generate
//
// Returns a slice of mock Plaid transactions.
func (m *mockPlaidService) generateMockTransactions(days int) []plaid.Transaction {
	if days <= 0 {
		return []plaid.Transaction{}
	}

	transactions := make([]plaid.Transaction, 0, days)
	now := time.Now()

	// Generate one transaction per day (going backwards from today)
	for i := 0; i < days; i++ {
		date := now.AddDate(0, 0, -i)
		dateStr := date.Format("2006-01-02")

		// Create minimal mock transaction with only required fields
		transactionId := fmt.Sprintf("mock-txn-%d", i)
		accountId := "mock-account-001"
		amount := float64(10 + i%20) // $10-$30
		dateField := dateStr
		name := fmt.Sprintf("Mock Transaction %d", i+1)

		transaction := plaid.Transaction{
			TransactionId: transactionId,
			AccountId:     accountId,
			Amount:        amount,
			Date:          dateField,
			Name:          name,
		}

		transactions = append(transactions, transaction)
	}

	return transactions
}
