package domain

import (
	"encoding/json"
	"testing"
	"time"
)

// TestLinkedItem_JSONMarshaling tests that LinkedItem struct
// correctly marshals and unmarshals to/from JSON
func TestLinkedItem_JSONMarshaling(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	original := &LinkedItem{
		ID:                   "550e8400-e29b-41d4-a716-446655440000",
		UserID:               "650e8400-e29b-41d4-a716-446655440001",
		ItemID:               "plaid-item-id-123",
		AccessTokenEncrypted: "encrypted-base64-token-data",
		InstitutionID:        "ins_109508",
		InstitutionName:      "Chase",
		Cursor:               strPtr("cursor-abc-123"),
		LastSyncedAt:         &now,
		CreatedAt:            now,
		UpdatedAt:            now,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal LinkedItem: %v", err)
	}

	// Unmarshal back
	var unmarshaled LinkedItem
	err = json.Unmarshal(jsonData, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal LinkedItem: %v", err)
	}

	// Verify critical fields match
	if unmarshaled.ID != original.ID {
		t.Errorf("ID mismatch: got %v, want %v", unmarshaled.ID, original.ID)
	}
	if unmarshaled.UserID != original.UserID {
		t.Errorf("UserID mismatch: got %v, want %v", unmarshaled.UserID, original.UserID)
	}
	if unmarshaled.ItemID != original.ItemID {
		t.Errorf("ItemID mismatch: got %v, want %v", unmarshaled.ItemID, original.ItemID)
	}
	if unmarshaled.InstitutionName != original.InstitutionName {
		t.Errorf("InstitutionName mismatch: got %v, want %v", unmarshaled.InstitutionName, original.InstitutionName)
	}

	// Verify AccessTokenEncrypted is NOT in JSON (json:"-" tag)
	var jsonMap map[string]interface{}
	json.Unmarshal(jsonData, &jsonMap)
	if _, exists := jsonMap["access_token_encrypted"]; exists {
		t.Error("AccessTokenEncrypted should not appear in JSON (security)")
	}
}

// TestLinkedItem_RequiredFields tests that LinkedItem validates required fields
func TestLinkedItem_RequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		item    LinkedItem
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid linked item",
			item: LinkedItem{
				UserID:               "user-123",
				ItemID:               "item-456",
				AccessTokenEncrypted: "encrypted-token",
				InstitutionID:        "ins_123",
			},
			wantErr: false,
		},
		{
			name: "missing user_id",
			item: LinkedItem{
				ItemID:               "item-456",
				AccessTokenEncrypted: "encrypted-token",
			},
			wantErr: true,
			errMsg:  "user_id",
		},
		{
			name: "missing item_id",
			item: LinkedItem{
				UserID:               "user-123",
				AccessTokenEncrypted: "encrypted-token",
			},
			wantErr: true,
			errMsg:  "item_id",
		},
		{
			name: "missing access_token",
			item: LinkedItem{
				UserID: "user-123",
				ItemID: "item-456",
			},
			wantErr: true,
			errMsg:  "access_token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.item.Validate()
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

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && containsHelper(s, substr)))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Helper function to create string pointers for tests
func strPtr(s string) *string {
	return &s
}
