package domain

import "testing"

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
