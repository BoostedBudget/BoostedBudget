package matching

import (
	"testing"
)

// =============================================================================
// Issue #495: Shared string matching utilities - TDD Tests
// =============================================================================

func TestContainsAll(t *testing.T) {
	tests := []struct {
		name     string
		a        []string
		b        []string
		expected bool
	}{
		{
			name:     "empty b returns true",
			a:        []string{"Food and Drink", "Restaurants"},
			b:        []string{},
			expected: true,
		},
		{
			name:     "nil b returns true",
			a:        []string{"Food and Drink"},
			b:        nil,
			expected: true,
		},
		{
			name:     "empty a with non-empty b returns false",
			a:        []string{},
			b:        []string{"Food and Drink"},
			expected: false,
		},
		{
			name:     "exact match",
			a:        []string{"Food and Drink", "Restaurants"},
			b:        []string{"Food and Drink", "Restaurants"},
			expected: true,
		},
		{
			name:     "superset contains subset",
			a:        []string{"Food and Drink", "Restaurants", "Fast Food"},
			b:        []string{"Food and Drink", "Restaurants"},
			expected: true,
		},
		{
			name:     "case insensitive match",
			a:        []string{"FOOD AND DRINK", "restaurants"},
			b:        []string{"food and drink", "RESTAURANTS"},
			expected: true,
		},
		{
			name:     "partial match returns false",
			a:        []string{"Food and Drink"},
			b:        []string{"Food and Drink", "Restaurants"},
			expected: false,
		},
		{
			name:     "no match",
			a:        []string{"Transportation", "Gas"},
			b:        []string{"Food and Drink"},
			expected: false,
		},
		{
			name:     "single element match",
			a:        []string{"Paycheck"},
			b:        []string{"Paycheck"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ContainsAll(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("ContainsAll(%v, %v) = %v, want %v", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestContainsAny(t *testing.T) {
	tests := []struct {
		name     string
		a        []string
		b        []string
		expected bool
	}{
		{
			name:     "empty a returns false",
			a:        []string{},
			b:        []string{"income", "paycheck"},
			expected: false,
		},
		{
			name:     "empty b returns false",
			a:        []string{"income", "paycheck"},
			b:        []string{},
			expected: false,
		},
		{
			name:     "nil a returns false",
			a:        nil,
			b:        []string{"income"},
			expected: false,
		},
		{
			name:     "nil b returns false",
			a:        []string{"income"},
			b:        nil,
			expected: false,
		},
		{
			name:     "single match",
			a:        []string{"income", "recurring"},
			b:        []string{"income"},
			expected: true,
		},
		{
			name:     "multiple matches",
			a:        []string{"income", "recurring", "paycheck"},
			b:        []string{"income", "paycheck"},
			expected: true,
		},
		{
			name:     "case insensitive match",
			a:        []string{"INCOME", "recurring"},
			b:        []string{"income", "paycheck"},
			expected: true,
		},
		{
			name:     "no match",
			a:        []string{"expense", "shopping"},
			b:        []string{"income", "paycheck"},
			expected: false,
		},
		{
			name:     "first element of a matches last of b",
			a:        []string{"bonus"},
			b:        []string{"income", "paycheck", "bonus"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ContainsAny(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("ContainsAny(%v, %v) = %v, want %v", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestExtractStringValues(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected []string
	}{
		{
			name:     "single string",
			input:    "Food and Drink",
			expected: []string{"Food and Drink"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: []string{""},
		},
		{
			name:     "string slice",
			input:    []string{"Food and Drink", "Restaurants"},
			expected: []string{"Food and Drink", "Restaurants"},
		},
		{
			name:     "empty string slice",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "interface slice with strings",
			input:    []interface{}{"Food and Drink", "Restaurants"},
			expected: []string{"Food and Drink", "Restaurants"},
		},
		{
			name:     "interface slice with mixed types",
			input:    []interface{}{"Food and Drink", 123, "Restaurants", true},
			expected: []string{"Food and Drink", "Restaurants"},
		},
		{
			name:     "interface slice with no strings",
			input:    []interface{}{123, 456, true},
			expected: []string{},
		},
		{
			name:     "nil returns nil",
			input:    nil,
			expected: nil,
		},
		{
			name:     "unsupported type returns nil",
			input:    123,
			expected: nil,
		},
		{
			name:     "map returns nil",
			input:    map[string]string{"key": "value"},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractStringValues(tt.input)

			// Handle nil comparison
			if tt.expected == nil {
				if result != nil {
					t.Errorf("ExtractStringValues(%v) = %v, want nil", tt.input, result)
				}
				return
			}

			// Compare slices
			if len(result) != len(tt.expected) {
				t.Errorf("ExtractStringValues(%v) = %v, want %v", tt.input, result, tt.expected)
				return
			}

			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("ExtractStringValues(%v)[%d] = %v, want %v", tt.input, i, v, tt.expected[i])
				}
			}
		})
	}
}

// Benchmark tests for performance verification
func BenchmarkContainsAll(b *testing.B) {
	a := []string{"Food and Drink", "Restaurants", "Fast Food", "Coffee Shops", "Bakeries"}
	c := []string{"Food and Drink", "Restaurants"}

	for i := 0; i < b.N; i++ {
		ContainsAll(a, c)
	}
}

func BenchmarkContainsAny(b *testing.B) {
	tags := []string{"income", "recurring", "paycheck", "bonus", "salary"}
	search := []string{"vacation", "expense", "income"}

	for i := 0; i < b.N; i++ {
		ContainsAny(tags, search)
	}
}
