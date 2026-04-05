package dateutil_test

import (
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/dateutil"
)

func TestParseDate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		field   string
		wantErr bool
		wantDay int
	}{
		{
			name:    "valid date",
			input:   "2024-03-15",
			field:   "start_date",
			wantErr: false,
			wantDay: 15,
		},
		{
			name:    "empty string returns error",
			input:   "",
			field:   "start_date",
			wantErr: true,
		},
		{
			name:    "wrong format returns error",
			input:   "15/03/2024",
			field:   "start_date",
			wantErr: true,
		},
		{
			name:    "RFC3339 not accepted",
			input:   "2024-03-15T00:00:00Z",
			field:   "start_date",
			wantErr: true,
		},
		{
			name:    "invalid month returns error",
			input:   "2024-13-01",
			field:   "end_date",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := dateutil.ParseDate(tt.input, tt.field)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseDate(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseDate(%q) unexpected error: %v", tt.input, err)
				return
			}
			if got.Day() != tt.wantDay {
				t.Errorf("ParseDate(%q) day = %d, want %d", tt.input, got.Day(), tt.wantDay)
			}
		})
	}
}

func TestParseOptionalDate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		field   string
		wantNil bool
		wantErr bool
	}{
		{
			name:    "empty string returns nil",
			input:   "",
			field:   "start_date",
			wantNil: true,
			wantErr: false,
		},
		{
			name:    "valid date returns pointer",
			input:   "2024-06-01",
			field:   "start_date",
			wantNil: false,
			wantErr: false,
		},
		{
			name:    "invalid format returns error",
			input:   "not-a-date",
			field:   "start_date",
			wantNil: false,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := dateutil.ParseOptionalDate(tt.input, tt.field)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseOptionalDate(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseOptionalDate(%q) unexpected error: %v", tt.input, err)
				return
			}
			if tt.wantNil && got != nil {
				t.Errorf("ParseOptionalDate(%q) expected nil, got %v", tt.input, got)
			}
			if !tt.wantNil && got == nil {
				t.Errorf("ParseOptionalDate(%q) expected non-nil, got nil", tt.input)
			}
		})
	}
}

func TestParseDateRange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		startStr string
		endStr   string
		wantErr  bool
	}{
		{
			name:     "valid range",
			startStr: "2024-01-01",
			endStr:   "2024-03-31",
			wantErr:  false,
		},
		{
			name:     "same day is valid",
			startStr: "2024-06-15",
			endStr:   "2024-06-15",
			wantErr:  false,
		},
		{
			name:     "end before start returns error",
			startStr: "2024-06-01",
			endStr:   "2024-05-01",
			wantErr:  true,
		},
		{
			name:     "empty start returns error",
			startStr: "",
			endStr:   "2024-03-31",
			wantErr:  true,
		},
		{
			name:     "empty end returns error",
			startStr: "2024-01-01",
			endStr:   "",
			wantErr:  true,
		},
		{
			name:     "invalid start format returns error",
			startStr: "01-01-2024",
			endStr:   "2024-03-31",
			wantErr:  true,
		},
		{
			name:     "invalid end format returns error",
			startStr: "2024-01-01",
			endStr:   "31/03/2024",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			start, end, err := dateutil.ParseDateRange(tt.startStr, tt.endStr)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseDateRange(%q, %q) expected error, got nil", tt.startStr, tt.endStr)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseDateRange(%q, %q) unexpected error: %v", tt.startStr, tt.endStr, err)
				return
			}
			if start.IsZero() || end.IsZero() {
				t.Errorf("ParseDateRange(%q, %q) returned zero time", tt.startStr, tt.endStr)
			}
		})
	}
}

func TestParseOptionalDateRange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		startStr   string
		endStr     string
		wantNilS   bool
		wantNilE   bool
		wantErr    bool
	}{
		{
			name:     "both empty returns nils",
			startStr: "",
			endStr:   "",
			wantNilS: true,
			wantNilE: true,
			wantErr:  false,
		},
		{
			name:     "both valid returns pointers",
			startStr: "2024-01-01",
			endStr:   "2024-12-31",
			wantNilS: false,
			wantNilE: false,
			wantErr:  false,
		},
		{
			name:     "start only",
			startStr: "2024-01-01",
			endStr:   "",
			wantNilS: false,
			wantNilE: true,
			wantErr:  false,
		},
		{
			name:    "invalid start returns error",
			startStr: "not-a-date",
			endStr:   "2024-12-31",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			start, end, err := dateutil.ParseOptionalDateRange(tt.startStr, tt.endStr)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseOptionalDateRange(%q, %q) expected error, got nil", tt.startStr, tt.endStr)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseOptionalDateRange(%q, %q) unexpected error: %v", tt.startStr, tt.endStr, err)
				return
			}
			if tt.wantNilS && start != nil {
				t.Errorf("start: expected nil, got %v", start)
			}
			if !tt.wantNilS && start == nil {
				t.Errorf("start: expected non-nil")
			}
			if tt.wantNilE && end != nil {
				t.Errorf("end: expected nil, got %v", end)
			}
			if !tt.wantNilE && end == nil {
				t.Errorf("end: expected non-nil")
			}
		})
	}
}

func TestErrorMessageFormat(t *testing.T) {
	t.Parallel()

	_, err := dateutil.ParseDate("bad", "start_date")
	if err == nil {
		t.Fatal("expected error")
	}
	errMsg := err.Error()
	if errMsg == "" {
		t.Error("error message should not be empty")
	}

	// Verify field name appears in message
	if !containsStr(errMsg, "start_date") {
		t.Errorf("error message %q should contain field name 'start_date'", errMsg)
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr2(s, substr))
}

func containsStr2(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Ensure ParseDateRange returns times that reflect the parsed dates.
func TestParseDateRange_Values(t *testing.T) {
	t.Parallel()
	start, end, err := dateutil.ParseDateRange("2024-01-15", "2024-03-20")
	if err != nil {
		t.Fatal(err)
	}
	if start.Year() != 2024 || start.Month() != time.January || start.Day() != 15 {
		t.Errorf("start = %v, want 2024-01-15", start)
	}
	if end.Year() != 2024 || end.Month() != time.March || end.Day() != 20 {
		t.Errorf("end = %v, want 2024-03-20", end)
	}
}
