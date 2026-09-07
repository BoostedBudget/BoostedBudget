package domain

import (
	"strings"
	"testing"
)

func TestCustomCategory_Validate_Hierarchy(t *testing.T) {
	tests := []struct {
		name    string
		cat     CustomCategory
		wantErr bool
	}{
		{
			name:    "top-level custom (no parent) is valid",
			cat:     CustomCategory{UserID: "u1", Name: "Coffee"},
			wantErr: false,
		},
		{
			name:    "nested under PFC primary only is valid",
			cat:     CustomCategory{UserID: "u1", Name: "Coffee", ParentPFCPrimary: strPtr("FOOD_AND_DRINK")},
			wantErr: false,
		},
		{
			name:    "nested under custom parent only is valid",
			cat:     CustomCategory{UserID: "u1", Name: "Coffee", ParentID: strPtr("11111111-1111-1111-1111-111111111111")},
			wantErr: false,
		},
		{
			name: "both PFC parent and custom parent is invalid (mutual exclusivity)",
			cat: CustomCategory{
				UserID:           "u1",
				Name:             "Coffee",
				ParentPFCPrimary: strPtr("FOOD_AND_DRINK"),
				ParentID:         strPtr("11111111-1111-1111-1111-111111111111"),
			},
			wantErr: true,
		},
		{
			name: "self-parent is invalid",
			cat: CustomCategory{
				ID:       "22222222-2222-2222-2222-222222222222",
				UserID:   "u1",
				Name:     "Coffee",
				ParentID: strPtr("22222222-2222-2222-2222-222222222222"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cat.Validate()
			if tt.wantErr && err == nil {
				t.Fatalf("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

// Issue #1609 Item 11 — parent_pfc_primary is no longer a cosmetic hint: analytics resolves a
// custom-categorised transaction's transfer classification through it (effectiveTransferExpr), and
// budget roll-up already did. A value the DB CHECK would reject (migration 000065's
// valid_custom_category_parent_pfc) must therefore be refused HERE, with a message naming the
// field, rather than reaching Postgres and returning an opaque constraint error.
func TestCustomCategory_Validate_ParentPFCPrimary(t *testing.T) {
	base := func(parent *string) *CustomCategory {
		return &CustomCategory{UserID: "user-1", Name: "Paycheck", ParentPFCPrimary: parent}
	}

	t.Run("nil is valid (top-level category)", func(t *testing.T) {
		if err := base(nil).Validate(); err != nil {
			t.Fatalf("expected nil parent_pfc_primary to be valid, got %v", err)
		}
	})

	t.Run("empty string is valid (form sends it for the unused dimension)", func(t *testing.T) {
		empty := ""
		if err := base(&empty).Validate(); err != nil {
			t.Fatalf("expected empty parent_pfc_primary to be valid, got %v", err)
		}
	})

	for _, valid := range []string{"INCOME", "TRANSFER_IN", "TRANSFER_OUT", "FOOD_AND_DRINK", "OTHER"} {
		t.Run("accepts "+valid, func(t *testing.T) {
			v := valid
			if err := base(&v).Validate(); err != nil {
				t.Fatalf("expected %q to be a valid parent_pfc_primary, got %v", valid, err)
			}
		})
	}

	for _, invalid := range []string{"NOT_A_PRIMARY", "income", "Transfer_In", "FOOD_AND_DRINK_COFFEE"} {
		t.Run("rejects "+invalid, func(t *testing.T) {
			v := invalid
			err := base(&v).Validate()
			if err == nil {
				t.Fatalf("expected %q to be rejected as a parent_pfc_primary", invalid)
			}
			if !strings.Contains(err.Error(), "parent_pfc_primary") {
				t.Fatalf("error must name the offending field, got %v", err)
			}
		})
	}
}
