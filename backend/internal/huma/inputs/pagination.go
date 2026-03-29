package inputs

// PaginationInput provides standard pagination parameters for list endpoints.
// Embed this in your input structs to add limit and offset query parameters.
//
// Example:
//
//	type GetTransactionsInput struct {
//	    inputs.AuthenticatedInput
//	    inputs.PaginationInput
//	    // ... other fields
//	}
type PaginationInput struct {
	// Limit is the maximum number of items to return.
	// Default: 50, Min: 1, Max: 100
	Limit int `query:"limit" default:"50" minimum:"1" maximum:"100" doc:"Number of items to return (1-100)"`

	// Offset is the number of items to skip before starting to return results.
	// Default: 0, Min: 0
	Offset int `query:"offset" default:"0" minimum:"0" doc:"Number of items to skip for pagination"`
}
