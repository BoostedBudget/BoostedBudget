package inputs

// PagePaginationInput provides page-based pagination parameters for admin list endpoints.
// Embed this in your input structs to add page and page_size query parameters.
//
// Example:
//
//	type ListUsersInput struct {
//	    inputs.AdminAuthenticatedInput
//	    inputs.PagePaginationInput
//	    // ... other fields
//	}
type PagePaginationInput struct {
	// Page is the page number (1-indexed).
	// Default: 1, Min: 1
	Page int `query:"page" default:"1" minimum:"1" doc:"Page number (1-indexed)"`

	// PageSize is the number of items per page.
	// Default: 20, Min: 1, Max: 100
	PageSize int `query:"page_size" default:"20" minimum:"1" maximum:"100" doc:"Number of items per page (1-100)"`
}

// ToLimitOffset converts page-based pagination to limit/offset for database queries.
// Returns limit (same as PageSize) and offset (calculated from page and page size).
func (p *PagePaginationInput) ToLimitOffset() (limit, offset int) {
	limit = p.PageSize
	offset = (p.Page - 1) * p.PageSize
	return
}

// CalculateTotalPages returns the total number of pages for a given total count.
// Returns 0 if totalCount is 0.
func (p *PagePaginationInput) CalculateTotalPages(totalCount int) int {
	if totalCount == 0 {
		return 0
	}
	// Integer ceiling division: (a + b - 1) / b
	return (totalCount + p.PageSize - 1) / p.PageSize
}

// HasMore returns true if there are more pages after the current page.
func (p *PagePaginationInput) HasMore(totalCount int) bool {
	return p.Page*p.PageSize < totalCount
}
