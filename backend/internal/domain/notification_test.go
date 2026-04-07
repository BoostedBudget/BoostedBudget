package domain

import (
	"testing"
)

// TestNotification_Validate_InteractionPattern tests validation of interaction_pattern field
func TestNotification_Validate_InteractionPattern(t *testing.T) {
	base := Notification{
		UserID:  "user-123",
		Type:    "budget_alert",
		Title:   "Alert",
		Message: "You are over budget",
	}

	tests := []struct {
		name    string
		pattern string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid informational pattern",
			pattern: "informational",
			wantErr: false,
		},
		{
			name:    "valid action_required pattern",
			pattern: "action_required",
			wantErr: false,
		},
		{
			name:    "valid acknowledgeable pattern",
			pattern: "acknowledgeable",
			wantErr: false,
		},
		{
			name:    "valid accept_reject pattern",
			pattern: "accept_reject",
			wantErr: false,
		},
		{
			name:    "invalid pattern returns error",
			pattern: "unknown_pattern",
			wantErr: true,
			errMsg:  "invalid interaction_pattern",
		},
		{
			name:    "empty pattern defaults to informational (no error)",
			pattern: "",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := base
			n.InteractionPattern = tt.pattern

			err := n.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.errMsg)
				}
				if err.Error() != tt.errMsg {
					t.Fatalf("expected error %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got %q", err.Error())
				}
				// Empty pattern should be defaulted to informational
				if tt.pattern == "" && n.InteractionPattern != "informational" {
					t.Fatalf("expected InteractionPattern to be defaulted to 'informational', got %q", n.InteractionPattern)
				}
			}
		})
	}
}

// TestNotification_Validate_TransferSuggestionType tests that transfer_suggestion is a valid notification type
func TestNotification_Validate_TransferSuggestionType(t *testing.T) {
	n := Notification{
		UserID:             "user-123",
		Type:               "transfer_suggestion",
		Title:              "Transfer Suggestion",
		Message:            "We suggest a transfer",
		InteractionPattern: "accept_reject",
	}

	err := n.Validate()
	if err != nil {
		t.Fatalf("expected transfer_suggestion to be a valid type, got error: %v", err)
	}
}

// TestNotification_Validate_RequiredFields tests required field validation
func TestNotification_Validate_RequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		n       Notification
		wantErr bool
		errMsg  string
	}{
		{
			name: "missing user_id",
			n: Notification{
				Type:    "budget_alert",
				Title:   "Test",
				Message: "Test message",
			},
			wantErr: true,
			errMsg:  "user_id is required",
		},
		{
			name: "missing type",
			n: Notification{
				UserID:  "user-123",
				Title:   "Test",
				Message: "Test message",
			},
			wantErr: true,
			errMsg:  "type is required",
		},
		{
			name: "missing title",
			n: Notification{
				UserID:  "user-123",
				Type:    "budget_alert",
				Message: "Test message",
			},
			wantErr: true,
			errMsg:  "title is required",
		},
		{
			name: "missing message",
			n: Notification{
				UserID: "user-123",
				Type:   "budget_alert",
				Title:  "Test",
			},
			wantErr: true,
			errMsg:  "message is required",
		},
		{
			name: "invalid type",
			n: Notification{
				UserID:  "user-123",
				Type:    "not_a_type",
				Title:   "Test",
				Message: "Test message",
			},
			wantErr: true,
			errMsg:  "invalid notification type",
		},
		{
			name: "valid notification",
			n: Notification{
				UserID:  "user-123",
				Type:    "budget_alert",
				Title:   "Test",
				Message: "Test message",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.n.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.errMsg)
				}
				if err.Error() != tt.errMsg {
					t.Fatalf("expected error %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got %q", err.Error())
				}
			}
		})
	}
}
