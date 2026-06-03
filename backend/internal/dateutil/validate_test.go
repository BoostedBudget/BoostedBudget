package dateutil_test

import (
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/dateutil"
)

func TestValidateDateRange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		start   time.Time
		end     time.Time
		wantErr bool
	}{
		{
			name:    "valid range",
			start:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			end:     time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
			wantErr: false,
		},
		{
			name:    "same day is valid",
			start:   time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC),
			end:     time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC),
			wantErr: false,
		},
		{
			name:    "end before start returns error",
			start:   time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			end:     time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC),
			wantErr: true,
		},
		{
			name:    "one day apart is valid",
			start:   time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
			end:     time.Date(2024, 3, 2, 0, 0, 0, 0, time.UTC),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := dateutil.ValidateDateRange(tt.start, tt.end)
			if tt.wantErr && err == nil {
				t.Errorf("ValidateDateRange() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateDateRange() unexpected error: %v", err)
			}
		})
	}
}
