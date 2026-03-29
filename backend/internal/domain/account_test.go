package domain

import (
	"encoding/json"
	"testing"
	"time"
)

// TestAccount_JSONMarshaling tests that Account struct
// correctly marshals and unmarshals to/from JSON matching the API spec
func TestAccount_JSONMarshaling(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	original := &Account{
		ID:               "acc-550e8400-e29b-41d4-a716-446655440000",
		LinkedItemID:     "item-650e8400-e29b-41d4-a716-446655440001",
		UserID:           "user-750e8400-e29b-41d4-a716-446655440002",
		PlaidAccountID:   "plaid-acc-xyz123",
		Name:             "Chase Checking",
		OfficialName:     strPtr("Chase Total Checking®"),
		Type:             "depository",
		Subtype:          strPtr("checking"),
		Mask:             strPtr("1234"),
		CurrentBalance:   float64Ptr(1500.50),
		AvailableBalance: float64Ptr(1450.25),
		CurrencyCode:     "USD",
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal Account: %v", err)
	}

	// Unmarshal back
	var unmarshaled Account
	err = json.Unmarshal(jsonData, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal Account: %v", err)
	}

	// Verify critical fields match
	if unmarshaled.ID != original.ID {
		t.Errorf("ID mismatch: got %v, want %v", unmarshaled.ID, original.ID)
	}
	if unmarshaled.UserID != original.UserID {
		t.Errorf("UserID mismatch: got %v, want %v", unmarshaled.UserID, original.UserID)
	}
	if unmarshaled.PlaidAccountID != original.PlaidAccountID {
		t.Errorf("PlaidAccountID mismatch: got %v, want %v", unmarshaled.PlaidAccountID, original.PlaidAccountID)
	}
	if unmarshaled.Name != original.Name {
		t.Errorf("Name mismatch: got %v, want %v", unmarshaled.Name, original.Name)
	}
	if unmarshaled.Type != original.Type {
		t.Errorf("Type mismatch: got %v, want %v", unmarshaled.Type, original.Type)
	}
}

// TestAccount_Validate tests that Account validates required fields and account types
func TestAccount_Validate(t *testing.T) {
	tests := []struct {
		name    string
		account Account
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid depository account",
			account: Account{
				UserID:         "user-123",
				LinkedItemID:   "item-456",
				PlaidAccountID: "plaid-acc-789",
				Name:           "Chase Checking",
				Type:           "depository",
				CurrencyCode:   "USD",
			},
			wantErr: false,
		},
		{
			name: "valid credit account",
			account: Account{
				UserID:         "user-123",
				LinkedItemID:   "item-456",
				PlaidAccountID: "plaid-acc-789",
				Name:           "Chase Credit Card",
				Type:           "credit",
				CurrencyCode:   "USD",
			},
			wantErr: false,
		},
		{
			name: "valid loan account",
			account: Account{
				UserID:         "user-123",
				LinkedItemID:   "item-456",
				PlaidAccountID: "plaid-acc-789",
				Name:           "Home Mortgage",
				Type:           "loan",
				CurrencyCode:   "USD",
			},
			wantErr: false,
		},
		{
			name: "missing user_id",
			account: Account{
				LinkedItemID:   "item-456",
				PlaidAccountID: "plaid-acc-789",
				Name:           "Account",
				Type:           "depository",
			},
			wantErr: true,
			errMsg:  "user_id",
		},
		{
			name: "missing name",
			account: Account{
				UserID:         "user-123",
				LinkedItemID:   "item-456",
				PlaidAccountID: "plaid-acc-789",
				Type:           "depository",
			},
			wantErr: true,
			errMsg:  "name",
		},
		{
			name: "invalid account type",
			account: Account{
				UserID:         "user-123",
				LinkedItemID:   "item-456",
				PlaidAccountID: "plaid-acc-789",
				Name:           "Account",
				Type:           "invalid_type",
			},
			wantErr: true,
			errMsg:  "type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.account.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errMsg != "" {
				if err == nil || !contains(err.Error(), tt.errMsg) {
					t.Errorf("Error should contain '%s', got: %v", tt.errMsg, err)
				}
			}
		})
	}
}

// Helper function to create float64 pointers for tests
func float64Ptr(f float64) *float64 {
	return &f
}

// Helper function to create int pointers for tests
func intPtr(i int) *int {
	return &i
}

// boolPtr is defined in privacy_test.go in the same package

// TestUpdateAccountRequest_Validate tests validation of account customization requests
func TestUpdateAccountRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     UpdateAccountRequest
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid empty request (all nil)",
			req:     UpdateAccountRequest{},
			wantErr: false,
		},
		{
			name: "valid nickname",
			req: UpdateAccountRequest{
				Nickname: strPtr("My Checking"),
			},
			wantErr: false,
		},
		{
			name: "valid nickname with spaces (will be trimmed)",
			req: UpdateAccountRequest{
				Nickname: strPtr("  My Savings  "),
			},
			wantErr: false,
		},
		{
			name: "valid empty string nickname (clear)",
			req: UpdateAccountRequest{
				Nickname: strPtr(""),
			},
			wantErr: false,
		},
		{
			name: "nickname too long (over 255 chars)",
			req: UpdateAccountRequest{
				Nickname: strPtr(string(make([]byte, 256))),
			},
			wantErr: true,
			errMsg:  "nickname",
		},
		{
			name: "valid hex color lowercase",
			req: UpdateAccountRequest{
				CustomColor: strPtr("#3b82f6"),
			},
			wantErr: false,
		},
		{
			name: "valid hex color uppercase",
			req: UpdateAccountRequest{
				CustomColor: strPtr("#3B82F6"),
			},
			wantErr: false,
		},
		{
			name: "valid empty color (clear)",
			req: UpdateAccountRequest{
				CustomColor: strPtr(""),
			},
			wantErr: false,
		},
		{
			name: "invalid hex color - no hash",
			req: UpdateAccountRequest{
				CustomColor: strPtr("3B82F6"),
			},
			wantErr: true,
			errMsg:  "custom_color",
		},
		{
			name: "invalid hex color - wrong length",
			req: UpdateAccountRequest{
				CustomColor: strPtr("#FFF"),
			},
			wantErr: true,
			errMsg:  "custom_color",
		},
		{
			name: "invalid hex color - invalid chars",
			req: UpdateAccountRequest{
				CustomColor: strPtr("#GGGGGG"),
			},
			wantErr: true,
			errMsg:  "custom_color",
		},
		{
			name: "valid icon - wallet",
			req: UpdateAccountRequest{
				CustomIcon: strPtr("wallet"),
			},
			wantErr: false,
		},
		{
			name: "valid icon - piggy-bank",
			req: UpdateAccountRequest{
				CustomIcon: strPtr("piggy-bank"),
			},
			wantErr: false,
		},
		{
			name: "valid empty icon (clear)",
			req: UpdateAccountRequest{
				CustomIcon: strPtr(""),
			},
			wantErr: false,
		},
		{
			name: "invalid icon - not in allowed set",
			req: UpdateAccountRequest{
				CustomIcon: strPtr("invalid-icon"),
			},
			wantErr: true,
			errMsg:  "custom_icon",
		},
		{
			name: "valid emoji",
			req: UpdateAccountRequest{
				CustomEmoji: strPtr("💰"),
			},
			wantErr: false,
		},
		{
			name: "valid empty emoji (clear)",
			req: UpdateAccountRequest{
				CustomEmoji: strPtr(""),
			},
			wantErr: false,
		},
		{
			name: "emoji too long (over 10 chars)",
			req: UpdateAccountRequest{
				CustomEmoji: strPtr("💰💰💰💰💰"),
			},
			wantErr: true,
			errMsg:  "custom_emoji",
		},
		{
			name: "valid is_hidden true",
			req: UpdateAccountRequest{
				IsHidden: boolPtr(true),
			},
			wantErr: false,
		},
		{
			name: "valid is_hidden false",
			req: UpdateAccountRequest{
				IsHidden: boolPtr(false),
			},
			wantErr: false,
		},
		{
			name: "valid combined update",
			req: UpdateAccountRequest{
				Nickname:    strPtr("Emergency Fund"),
				CustomColor: strPtr("#10B981"),
				CustomIcon:  strPtr("piggy-bank"),
				CustomEmoji: strPtr("🐷"),
				IsHidden:    boolPtr(false),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errMsg != "" {
				if err == nil || !contains(err.Error(), tt.errMsg) {
					t.Errorf("Error should contain '%s', got: %v", tt.errMsg, err)
				}
			}
		})
	}
}

// TestValidAccountIcons tests the valid account icons map
func TestValidAccountIcons(t *testing.T) {
	expectedIcons := []string{
		"wallet", "piggy-bank", "credit-card", "bank", "building",
		"briefcase", "dollar", "euro", "pound", "yen",
		"chart", "trending-up", "trending-down", "lock", "unlock",
		"star", "heart", "home", "car", "plane",
		"shopping-cart", "gift", "coffee", "utensils", "graduation-cap",
	}

	for _, icon := range expectedIcons {
		if !ValidAccountIcons[icon] {
			t.Errorf("Expected icon '%s' to be valid", icon)
		}
	}

	// Test that invalid icons return false
	invalidIcons := []string{"invalid", "random", "test"}
	for _, icon := range invalidIcons {
		if ValidAccountIcons[icon] {
			t.Errorf("Expected icon '%s' to be invalid", icon)
		}
	}
}
