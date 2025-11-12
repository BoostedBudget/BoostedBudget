package testutils

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// CreateTestUser creates a test user with specified subscription tier
// Tier options: "free", "premium_lite", "premium", "premium_plus"
// Returns a fully initialized User with sensible defaults
func CreateTestUser(t testing.TB, tier string) *domain.User {
	t.Helper()

	now := time.Now()
	userID := uuid.New().String()
	auth0UserID := fmt.Sprintf("auth0|test-%s", uuid.New().String()[:8])
	email := fmt.Sprintf("test-%s@example.com", uuid.New().String()[:8])

	return &domain.User{
		ID:                 userID,
		Auth0UserID:        auth0UserID,
		Email:              email,
		EmailVerified:      true,
		SubscriptionTier:   tier,
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad", // US East (default region for tests)
		IsActive:           true,
		IsDeleted:          false,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

// CreateTestLinkedItem creates a test linked item for a user
// Returns a LinkedItem with Plaid provider and active status
func CreateTestLinkedItem(t testing.TB, userID string) *domain.LinkedItem {
	t.Helper()

	now := time.Now()
	itemID := uuid.New().String()
	accessToken := fmt.Sprintf("access-sandbox-%s", uuid.New().String()[:16])

	return &domain.LinkedItem{
		ID:                   uuid.New().String(),
		UserID:               userID,
		ItemID:               itemID,
		AccessTokenEncrypted: accessToken, // In real tests, this would be encrypted
		InstitutionID:        "ins_3",     // Chase sandbox institution
		InstitutionName:      "Chase",
		Provider:             "plaid",
		Status:               "active",
		CreatedAt:            now,
		UpdatedAt:            now,
	}
}

// CreateTestLinkedItemWithError creates a linked item in error state
// Used for testing error handling scenarios (e.g., LOGIN_REQUIRED)
func CreateTestLinkedItemWithError(t testing.TB, userID string, errorCode string) *domain.LinkedItem {
	t.Helper()

	item := CreateTestLinkedItem(t, userID)
	item.Status = "login_required"
	errorCodeStr := errorCode
	errorMsg := "the login details of this item have changed and a user login is required"
	now := time.Now()

	item.ErrorCode = &errorCodeStr
	item.ErrorMessage = &errorMsg
	item.LastErrorAt = &now
	item.RetryCount = 1

	return item
}

// CreateTestAccount creates a test account for a user and linked item
// Returns a checking account with realistic balance
func CreateTestAccount(t testing.TB, userID, linkedItemID string) *domain.Account {
	t.Helper()

	now := time.Now()
	accountID := uuid.New().String()
	plaidAccountID := fmt.Sprintf("plaid-acc-%s", uuid.New().String()[:8])
	currentBalance := 5000.00
	availableBalance := 4800.00
	mask := "4242"

	return &domain.Account{
		ID:               accountID,
		LinkedItemID:     linkedItemID,
		UserID:           userID,
		PlaidAccountID:   plaidAccountID,
		Name:             "Test Checking Account",
		Type:             "depository",
		Subtype:          strPtr("checking"),
		Mask:             &mask,
		CurrentBalance:   &currentBalance,
		AvailableBalance: &availableBalance,
		CurrencyCode:     "USD",
		InstitutionName:  "Chase",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}

// CreateTestCreditAccount creates a test credit card account
// Used for testing credit-specific scenarios (pending transactions, etc.)
func CreateTestCreditAccount(t testing.TB, userID, linkedItemID string) *domain.Account {
	t.Helper()

	account := CreateTestAccount(t, userID, linkedItemID)
	account.Name = "Test Credit Card"
	account.Type = "credit"
	subtype := "credit card"
	account.Subtype = &subtype

	// Credit cards have negative current balance (owed amount)
	currentBalance := -500.00
	availableBalance := 9500.00 // $10k limit - $500 used
	account.CurrentBalance = &currentBalance
	account.AvailableBalance = &availableBalance

	return account
}

// CreateTestTransaction creates a test transaction for an account
// Overrides allows customizing specific fields (e.g., {"amount": -100.50, "pending": true})
func CreateTestTransaction(t testing.TB, accountID string, userID string, linkedItemID string, overrides map[string]interface{}) *domain.Transaction {
	t.Helper()

	now := time.Now()
	externalID := fmt.Sprintf("plaid-txn-%s", uuid.New().String()[:8])

	txn := &domain.Transaction{
		ID:                    uuid.New().String(),
		UserID:                userID,
		AccountID:             accountID,
		LinkedItemID:          linkedItemID,
		Source:                domain.SourcePlaid,
		ExternalTransactionID: &externalID,
		Amount:                -50.00, // Default: $50 debit
		Date:                  now.AddDate(0, 0, -1), // Yesterday
		Name:                  "Test Transaction",
		Pending:               false,
		CreatedAt:             now,
		UpdatedAt:             now,
	}

	// Apply overrides
	applyTransactionOverrides(txn, overrides)

	return txn
}

// CreateTestTransactionWithUserNotes creates a transaction with user notes
// Used for testing user notes preservation during syncs
func CreateTestTransactionWithUserNotes(t testing.TB, accountID string, userID string, linkedItemID string, notes string) *domain.Transaction {
	t.Helper()

	txn := CreateTestTransaction(t, accountID, userID, linkedItemID, nil)
	txn.UserNotes = &notes
	txn.Name = "Transaction with Notes"

	return txn
}

// CreateTestPendingTransaction creates a pending transaction
// Used for testing pending-to-posted transitions
func CreateTestPendingTransaction(t testing.TB, accountID string, userID string, linkedItemID string) *domain.Transaction {
	t.Helper()

	return CreateTestTransaction(t, accountID, userID, linkedItemID, map[string]interface{}{
		"pending": true,
		"name":    "Pending Transaction",
		"amount":  -45.00,
	})
}

// CreateTestTransactionBatch creates multiple test transactions
// Useful for testing pagination, bulk operations, etc.
func CreateTestTransactionBatch(t testing.TB, accountID string, userID string, linkedItemID string, count int) []*domain.Transaction {
	t.Helper()

	transactions := make([]*domain.Transaction, count)
	now := time.Now()

	for i := 0; i < count; i++ {
		transactions[i] = CreateTestTransaction(t, accountID, userID, linkedItemID, map[string]interface{}{
			"name":   fmt.Sprintf("Transaction %d", i+1),
			"amount": float64(-10 * (i + 1)), // -$10, -$20, -$30, etc.
			"date":   now.AddDate(0, 0, -i),  // Spread across days
		})
	}

	return transactions
}

// applyTransactionOverrides applies custom field values to a transaction
func applyTransactionOverrides(txn *domain.Transaction, overrides map[string]interface{}) {
	if overrides == nil {
		return
	}

	if v, ok := overrides["amount"].(float64); ok {
		txn.Amount = v
	}
	if v, ok := overrides["name"].(string); ok {
		txn.Name = v
	}
	if v, ok := overrides["pending"].(bool); ok {
		txn.Pending = v
	}
	if v, ok := overrides["date"].(time.Time); ok {
		txn.Date = v
	}
	if v, ok := overrides["merchant_name"].(string); ok {
		txn.MerchantName = &v
	}
	if v, ok := overrides["user_notes"].(string); ok {
		txn.UserNotes = &v
	}
	if v, ok := overrides["external_transaction_id"].(string); ok {
		txn.ExternalTransactionID = &v
	}
}

// strPtr returns a pointer to a string
func strPtr(s string) *string {
	return &s
}

// MustParseTime parses a time string in RFC3339 format or panics
// Useful for test data with specific dates
func MustParseTime(t testing.TB, timeStr string) time.Time {
	t.Helper()

	parsed, err := time.Parse(time.RFC3339, timeStr)
	if err != nil {
		t.Fatalf("failed to parse time %q: %v", timeStr, err)
	}
	return parsed
}

// TimePtr returns a pointer to a time.Time
func TimePtr(t time.Time) *time.Time {
	return &t
}
