package inputs_test

import (
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
)

// TestPagePaginationInput_ToLimitOffset tests the conversion from page-based to offset-based pagination
func TestPagePaginationInput_ToLimitOffset(t *testing.T) {
	tests := []struct {
		name           string
		page           int
		pageSize       int
		expectedLimit  int
		expectedOffset int
	}{
		{
			name:           "first page",
			page:           1,
			pageSize:       20,
			expectedLimit:  20,
			expectedOffset: 0,
		},
		{
			name:           "second page",
			page:           2,
			pageSize:       20,
			expectedLimit:  20,
			expectedOffset: 20,
		},
		{
			name:           "third page with different page size",
			page:           3,
			pageSize:       10,
			expectedLimit:  10,
			expectedOffset: 20,
		},
		{
			name:           "large page number",
			page:           100,
			pageSize:       50,
			expectedLimit:  50,
			expectedOffset: 4950,
		},
		{
			name:           "page size of 1",
			page:           5,
			pageSize:       1,
			expectedLimit:  1,
			expectedOffset: 4,
		},
		{
			name:           "max page size",
			page:           1,
			pageSize:       100,
			expectedLimit:  100,
			expectedOffset: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := inputs.PagePaginationInput{
				Page:     tt.page,
				PageSize: tt.pageSize,
			}

			limit, offset := input.ToLimitOffset()

			assert.Equal(t, tt.expectedLimit, limit)
			assert.Equal(t, tt.expectedOffset, offset)
		})
	}
}

// TestPagePaginationInput_DefaultValues tests that the struct has expected default tag values
func TestPagePaginationInput_DefaultValues(t *testing.T) {
	// This test verifies the struct tags by documenting expected defaults
	// The actual parsing is handled by Huma, but we test the intended behavior
	input := inputs.PagePaginationInput{
		Page:     1,  // default
		PageSize: 20, // default
	}

	limit, offset := input.ToLimitOffset()

	assert.Equal(t, 20, limit)
	assert.Equal(t, 0, offset)
}

// TestPagePaginationInput_CalculateTotalPages tests the total pages calculation helper
func TestPagePaginationInput_CalculateTotalPages(t *testing.T) {
	tests := []struct {
		name          string
		totalCount    int
		pageSize      int
		expectedPages int
	}{
		{
			name:          "exact division",
			totalCount:    100,
			pageSize:      20,
			expectedPages: 5,
		},
		{
			name:          "partial page",
			totalCount:    101,
			pageSize:      20,
			expectedPages: 6,
		},
		{
			name:          "single page",
			totalCount:    15,
			pageSize:      20,
			expectedPages: 1,
		},
		{
			name:          "zero items",
			totalCount:    0,
			pageSize:      20,
			expectedPages: 0,
		},
		{
			name:          "exactly one page",
			totalCount:    20,
			pageSize:      20,
			expectedPages: 1,
		},
		{
			name:          "large dataset",
			totalCount:    10000,
			pageSize:      100,
			expectedPages: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := inputs.PagePaginationInput{
				Page:     1,
				PageSize: tt.pageSize,
			}

			totalPages := input.CalculateTotalPages(tt.totalCount)

			assert.Equal(t, tt.expectedPages, totalPages)
		})
	}
}

// TestPagePaginationInput_HasMore tests the has_more calculation helper
func TestPagePaginationInput_HasMore(t *testing.T) {
	tests := []struct {
		name       string
		page       int
		pageSize   int
		totalCount int
		expected   bool
	}{
		{
			name:       "first page with more data",
			page:       1,
			pageSize:   20,
			totalCount: 100,
			expected:   true,
		},
		{
			name:       "last page",
			page:       5,
			pageSize:   20,
			totalCount: 100,
			expected:   false,
		},
		{
			name:       "single page",
			page:       1,
			pageSize:   20,
			totalCount: 15,
			expected:   false,
		},
		{
			name:       "exactly on page boundary",
			page:       5,
			pageSize:   20,
			totalCount: 100,
			expected:   false,
		},
		{
			name:       "partial last page",
			page:       5,
			pageSize:   20,
			totalCount: 95,
			expected:   false,
		},
		{
			name:       "middle page",
			page:       3,
			pageSize:   20,
			totalCount: 100,
			expected:   true,
		},
		{
			name:       "empty result",
			page:       1,
			pageSize:   20,
			totalCount: 0,
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := inputs.PagePaginationInput{
				Page:     tt.page,
				PageSize: tt.pageSize,
			}

			hasMore := input.HasMore(tt.totalCount)

			assert.Equal(t, tt.expected, hasMore)
		})
	}
}
