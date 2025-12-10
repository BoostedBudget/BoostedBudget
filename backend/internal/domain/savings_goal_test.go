package domain

import (
	"encoding/json"
	"testing"
	"time"
)

// TestSavingsGoal_Validate tests that SavingsGoal validates required fields
func TestSavingsGoal_Validate(t *testing.T) {
	tests := []struct {
		name    string
		goal    SavingsGoal
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid savings goal",
			goal: SavingsGoal{
				UserID:        "user-123",
				Name:          "Emergency Fund",
				TargetAmount:  10000.00,
				CurrentAmount: 2500.00,
				CurrencyCode:  "USD",
				AccountID:     "account-456",
			},
			wantErr: false,
		},
		{
			name: "valid savings goal with deadline",
			goal: SavingsGoal{
				UserID:        "user-123",
				Name:          "Vacation",
				TargetAmount:  5000.00,
				CurrentAmount: 1000.00,
				CurrencyCode:  "USD",
				AccountID:     "account-456",
				Deadline:      timePtr(time.Now().AddDate(1, 0, 0)),
			},
			wantErr: false,
		},
		{
			name: "missing user_id",
			goal: SavingsGoal{
				Name:          "Test Goal",
				TargetAmount:  1000.00,
				CurrencyCode:  "USD",
				AccountID:     "account-456",
			},
			wantErr: true,
			errMsg:  "user_id is required",
		},
		{
			name: "missing name",
			goal: SavingsGoal{
				UserID:       "user-123",
				TargetAmount: 1000.00,
				CurrencyCode: "USD",
				AccountID:    "account-456",
			},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name: "name too long",
			goal: SavingsGoal{
				UserID:       "user-123",
				Name:         string(make([]byte, 101)), // 101 characters
				TargetAmount: 1000.00,
				CurrencyCode: "USD",
				AccountID:    "account-456",
			},
			wantErr: true,
			errMsg:  "name must be 100 characters or less",
		},
		{
			name: "zero target_amount",
			goal: SavingsGoal{
				UserID:       "user-123",
				Name:         "Test Goal",
				TargetAmount: 0,
				CurrencyCode: "USD",
				AccountID:    "account-456",
			},
			wantErr: true,
			errMsg:  "target_amount must be positive",
		},
		{
			name: "negative target_amount",
			goal: SavingsGoal{
				UserID:       "user-123",
				Name:         "Test Goal",
				TargetAmount: -100.00,
				CurrencyCode: "USD",
				AccountID:    "account-456",
			},
			wantErr: true,
			errMsg:  "target_amount must be positive",
		},
		{
			name: "negative current_amount",
			goal: SavingsGoal{
				UserID:        "user-123",
				Name:          "Test Goal",
				TargetAmount:  1000.00,
				CurrentAmount: -50.00,
				CurrencyCode:  "USD",
				AccountID:     "account-456",
			},
			wantErr: true,
			errMsg:  "current_amount cannot be negative",
		},
		{
			name: "missing currency_code",
			goal: SavingsGoal{
				UserID:       "user-123",
				Name:         "Test Goal",
				TargetAmount: 1000.00,
				AccountID:    "account-456",
			},
			wantErr: true,
			errMsg:  "currency_code is required",
		},
		{
			name: "missing account_id",
			goal: SavingsGoal{
				UserID:       "user-123",
				Name:         "Test Goal",
				TargetAmount: 1000.00,
				CurrencyCode: "USD",
			},
			wantErr: true,
			errMsg:  "account_id is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.goal.Validate()
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

// TestSavingsGoal_Progress tests the Progress calculation
func TestSavingsGoal_Progress(t *testing.T) {
	tests := []struct {
		name          string
		targetAmount  float64
		currentAmount float64
		want          float64
	}{
		{
			name:          "zero target",
			targetAmount:  0,
			currentAmount: 100.00,
			want:          0,
		},
		{
			name:          "0% progress",
			targetAmount:  1000.00,
			currentAmount: 0,
			want:          0,
		},
		{
			name:          "25% progress",
			targetAmount:  1000.00,
			currentAmount: 250.00,
			want:          25,
		},
		{
			name:          "50% progress",
			targetAmount:  1000.00,
			currentAmount: 500.00,
			want:          50,
		},
		{
			name:          "100% progress",
			targetAmount:  1000.00,
			currentAmount: 1000.00,
			want:          100,
		},
		{
			name:          "over 100% (exceeded target)",
			targetAmount:  1000.00,
			currentAmount: 1500.00,
			want:          150,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			goal := SavingsGoal{
				TargetAmount:  tt.targetAmount,
				CurrentAmount: tt.currentAmount,
			}
			if got := goal.Progress(); got != tt.want {
				t.Errorf("Progress() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSavingsGoal_RemainingAmount tests the RemainingAmount calculation
func TestSavingsGoal_RemainingAmount(t *testing.T) {
	tests := []struct {
		name          string
		targetAmount  float64
		currentAmount float64
		want          float64
	}{
		{
			name:          "full amount remaining",
			targetAmount:  1000.00,
			currentAmount: 0,
			want:          1000.00,
		},
		{
			name:          "partial progress",
			targetAmount:  1000.00,
			currentAmount: 400.00,
			want:          600.00,
		},
		{
			name:          "target reached",
			targetAmount:  1000.00,
			currentAmount: 1000.00,
			want:          0,
		},
		{
			name:          "exceeded target (returns 0)",
			targetAmount:  1000.00,
			currentAmount: 1500.00,
			want:          0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			goal := SavingsGoal{
				TargetAmount:  tt.targetAmount,
				CurrentAmount: tt.currentAmount,
			}
			if got := goal.RemainingAmount(); got != tt.want {
				t.Errorf("RemainingAmount() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSavingsGoal_IsAchieved tests the IsAchieved check
func TestSavingsGoal_IsAchieved(t *testing.T) {
	tests := []struct {
		name          string
		targetAmount  float64
		currentAmount float64
		want          bool
	}{
		{
			name:          "not achieved",
			targetAmount:  1000.00,
			currentAmount: 500.00,
			want:          false,
		},
		{
			name:          "exactly achieved",
			targetAmount:  1000.00,
			currentAmount: 1000.00,
			want:          true,
		},
		{
			name:          "exceeded",
			targetAmount:  1000.00,
			currentAmount: 1500.00,
			want:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			goal := SavingsGoal{
				TargetAmount:  tt.targetAmount,
				CurrentAmount: tt.currentAmount,
			}
			if got := goal.IsAchieved(); got != tt.want {
				t.Errorf("IsAchieved() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSavingsGoal_IsOnTrack tests the IsOnTrack calculation
func TestSavingsGoal_IsOnTrack(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name          string
		goal          SavingsGoal
		want          bool
	}{
		{
			name: "no deadline (always on track)",
			goal: SavingsGoal{
				TargetAmount:  1000.00,
				CurrentAmount: 100.00,
				Deadline:      nil,
			},
			want: true,
		},
		{
			name: "completed goal (always on track)",
			goal: SavingsGoal{
				TargetAmount:  1000.00,
				CurrentAmount: 1000.00,
				IsCompleted:   true,
				Deadline:      timePtr(now.AddDate(0, 1, 0)),
			},
			want: true,
		},
		{
			name: "past deadline but target reached",
			goal: SavingsGoal{
				TargetAmount:  1000.00,
				CurrentAmount: 1000.00,
				CreatedAt:     now.AddDate(0, -2, 0),
				Deadline:      timePtr(now.AddDate(0, 0, -1)), // yesterday
			},
			want: true,
		},
		{
			name: "past deadline target not reached",
			goal: SavingsGoal{
				TargetAmount:  1000.00,
				CurrentAmount: 500.00,
				CreatedAt:     now.AddDate(0, -2, 0),
				Deadline:      timePtr(now.AddDate(0, 0, -1)), // yesterday
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.goal.IsOnTrack(); got != tt.want {
				t.Errorf("IsOnTrack() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSavingsGoal_DaysUntilDeadline tests the DaysUntilDeadline calculation
func TestSavingsGoal_DaysUntilDeadline(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		deadline *time.Time
		want     int
	}{
		{
			name:     "no deadline",
			deadline: nil,
			want:     -1,
		},
		{
			name:     "deadline in 30 days",
			deadline: timePtr(now.AddDate(0, 0, 30)),
			want:     30,
		},
		{
			name:     "deadline passed",
			deadline: timePtr(now.AddDate(0, 0, -5)),
			want:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			goal := SavingsGoal{Deadline: tt.deadline}
			got := goal.DaysUntilDeadline()
			// Allow for 1 day variance due to time of day
			if tt.want == -1 && got != -1 {
				t.Errorf("DaysUntilDeadline() = %v, want %v", got, tt.want)
			} else if tt.want != -1 && (got < tt.want-1 || got > tt.want+1) {
				t.Errorf("DaysUntilDeadline() = %v, want approximately %v", got, tt.want)
			}
		})
	}
}

// TestSavingsGoal_JSONMarshaling tests JSON serialization
func TestSavingsGoal_JSONMarshaling(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	deadline := now.AddDate(1, 0, 0)
	description := "Save for vacation"

	original := &SavingsGoal{
		ID:            "goal-550e8400-e29b-41d4-a716-446655440000",
		UserID:        "user-650e8400-e29b-41d4-a716-446655440001",
		Name:          "Vacation Fund",
		Description:   &description,
		TargetAmount:  5000.00,
		CurrentAmount: 1250.00,
		CurrencyCode:  "USD",
		AccountID:     "account-750e8400-e29b-41d4-a716-446655440002",
		Deadline:      &deadline,
		IsCompleted:   false,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal SavingsGoal: %v", err)
	}

	// Unmarshal back
	var unmarshaled SavingsGoal
	err = json.Unmarshal(jsonData, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal SavingsGoal: %v", err)
	}

	// Verify critical fields match
	if unmarshaled.ID != original.ID {
		t.Errorf("ID mismatch: got %v, want %v", unmarshaled.ID, original.ID)
	}
	if unmarshaled.UserID != original.UserID {
		t.Errorf("UserID mismatch: got %v, want %v", unmarshaled.UserID, original.UserID)
	}
	if unmarshaled.Name != original.Name {
		t.Errorf("Name mismatch: got %v, want %v", unmarshaled.Name, original.Name)
	}
	if unmarshaled.TargetAmount != original.TargetAmount {
		t.Errorf("TargetAmount mismatch: got %v, want %v", unmarshaled.TargetAmount, original.TargetAmount)
	}
	if unmarshaled.CurrentAmount != original.CurrentAmount {
		t.Errorf("CurrentAmount mismatch: got %v, want %v", unmarshaled.CurrentAmount, original.CurrentAmount)
	}
	if unmarshaled.CurrencyCode != original.CurrencyCode {
		t.Errorf("CurrencyCode mismatch: got %v, want %v", unmarshaled.CurrencyCode, original.CurrencyCode)
	}
	if unmarshaled.AccountID != original.AccountID {
		t.Errorf("AccountID mismatch: got %v, want %v", unmarshaled.AccountID, original.AccountID)
	}
	if unmarshaled.IsCompleted != original.IsCompleted {
		t.Errorf("IsCompleted mismatch: got %v, want %v", unmarshaled.IsCompleted, original.IsCompleted)
	}
}

// Helper function to create time pointer
func timePtr(t time.Time) *time.Time {
	return &t
}
