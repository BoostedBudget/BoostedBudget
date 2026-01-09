package validators

import (
	"context"
	"fmt"
	"os"
	"time"
)

// SyncInitialValidator tests initial sync (empty cursor) flow.
// Requires PLAID_TEST_ACCESS_TOKEN environment variable for sandbox testing.
type SyncInitialValidator struct{}

func init() {
	Register(&SyncInitialValidator{})
	Register(&SyncIncrementalValidator{})
	Register(&SyncPaginationValidator{})
}

// Name returns the validator identifier.
func (v *SyncInitialValidator) Name() string {
	return "sync-initial"
}

// Description returns a human-readable description.
func (v *SyncInitialValidator) Description() string {
	return "Test initial transaction sync with empty cursor"
}

// Validate tests the initial sync flow.
func (v *SyncInitialValidator) Validate(ctx context.Context, opts *ValidationOptions) *ValidationResult {
	start := time.Now()
	result := &ValidationResult{
		Name:    v.Name(),
		Details: make(map[string]interface{}),
	}

	// Get test access token from environment
	accessToken := os.Getenv("PLAID_TEST_ACCESS_TOKEN")
	if accessToken == "" {
		result.Status = StatusSkip
		result.Message = "PLAID_TEST_ACCESS_TOKEN not set - skipping sync test"
		result.Details["reason"] = "No test access token available"
		result.Duration = time.Since(start)
		return result
	}

	if opts.PlaidService == nil {
		result.Status = StatusFail
		result.Message = "PlaidService not initialized"
		result.Duration = time.Since(start)
		return result
	}

	// Call TransactionsSync with empty cursor (initial sync)
	resp, err := opts.PlaidService.TransactionsSync(ctx, accessToken, "", 30)
	if err != nil {
		result.Status = StatusFail
		result.Message = fmt.Sprintf("Initial sync failed: %v", err)
		result.Error = err
		result.Duration = time.Since(start)
		return result
	}

	// Verify response structure
	cursor := resp.GetNextCursor()
	hasMore := resp.GetHasMore()
	addedCount := len(resp.GetAdded())
	modifiedCount := len(resp.GetModified())
	removedCount := len(resp.GetRemoved())

	result.Details["cursor_received"] = cursor != ""
	result.Details["has_more"] = hasMore
	result.Details["added_count"] = addedCount
	result.Details["modified_count"] = modifiedCount
	result.Details["removed_count"] = removedCount

	if cursor == "" {
		result.Status = StatusFail
		result.Message = "No cursor returned from initial sync"
		result.Duration = time.Since(start)
		return result
	}

	result.Status = StatusPass
	result.Message = fmt.Sprintf("Initial sync returned %d transactions with cursor", addedCount+modifiedCount)
	result.Duration = time.Since(start)
	return result
}

// SyncIncrementalValidator tests incremental sync (with existing cursor).
type SyncIncrementalValidator struct{}

// Name returns the validator identifier.
func (v *SyncIncrementalValidator) Name() string {
	return "sync-incremental"
}

// Description returns a human-readable description.
func (v *SyncIncrementalValidator) Description() string {
	return "Test incremental sync with existing cursor"
}

// Validate tests the incremental sync flow.
func (v *SyncIncrementalValidator) Validate(ctx context.Context, opts *ValidationOptions) *ValidationResult {
	start := time.Now()
	result := &ValidationResult{
		Name:    v.Name(),
		Details: make(map[string]interface{}),
	}

	accessToken := os.Getenv("PLAID_TEST_ACCESS_TOKEN")
	if accessToken == "" {
		result.Status = StatusSkip
		result.Message = "PLAID_TEST_ACCESS_TOKEN not set - skipping sync test"
		result.Duration = time.Since(start)
		return result
	}

	if opts.PlaidService == nil {
		result.Status = StatusFail
		result.Message = "PlaidService not initialized"
		result.Duration = time.Since(start)
		return result
	}

	// First, get initial cursor
	initialResp, err := opts.PlaidService.TransactionsSync(ctx, accessToken, "", 30)
	if err != nil {
		result.Status = StatusFail
		result.Message = fmt.Sprintf("Failed to get initial cursor: %v", err)
		result.Error = err
		result.Duration = time.Since(start)
		return result
	}

	cursor := initialResp.GetNextCursor()
	if cursor == "" {
		result.Status = StatusFail
		result.Message = "No initial cursor received"
		result.Duration = time.Since(start)
		return result
	}

	result.Details["initial_cursor"] = cursor[:min(20, len(cursor))] + "..."

	// Now test incremental sync with the cursor
	incrResp, err := opts.PlaidService.TransactionsSync(ctx, accessToken, cursor, 0)
	if err != nil {
		result.Status = StatusFail
		result.Message = fmt.Sprintf("Incremental sync failed: %v", err)
		result.Error = err
		result.Duration = time.Since(start)
		return result
	}

	newCursor := incrResp.GetNextCursor()
	hasMore := incrResp.GetHasMore()

	result.Details["new_cursor_received"] = newCursor != ""
	result.Details["has_more"] = hasMore
	result.Details["added_count"] = len(incrResp.GetAdded())
	result.Details["modified_count"] = len(incrResp.GetModified())
	result.Details["removed_count"] = len(incrResp.GetRemoved())

	if newCursor == "" {
		result.Status = StatusFail
		result.Message = "No cursor returned from incremental sync"
		result.Duration = time.Since(start)
		return result
	}

	result.Status = StatusPass
	result.Message = "Incremental sync completed successfully"
	result.Duration = time.Since(start)
	return result
}

// SyncPaginationValidator tests multi-page sync handling.
type SyncPaginationValidator struct{}

// Name returns the validator identifier.
func (v *SyncPaginationValidator) Name() string {
	return "sync-pagination"
}

// Description returns a human-readable description.
func (v *SyncPaginationValidator) Description() string {
	return "Test multi-page transaction sync pagination"
}

// Validate tests the pagination flow.
func (v *SyncPaginationValidator) Validate(ctx context.Context, opts *ValidationOptions) *ValidationResult {
	start := time.Now()
	result := &ValidationResult{
		Name:    v.Name(),
		Details: make(map[string]interface{}),
	}

	accessToken := os.Getenv("PLAID_TEST_ACCESS_TOKEN")
	if accessToken == "" {
		result.Status = StatusSkip
		result.Message = "PLAID_TEST_ACCESS_TOKEN not set - skipping sync test"
		result.Duration = time.Since(start)
		return result
	}

	if opts.PlaidService == nil {
		result.Status = StatusFail
		result.Message = "PlaidService not initialized"
		result.Duration = time.Since(start)
		return result
	}

	// Paginate through all available data
	cursor := ""
	pageCount := 0
	totalAdded := 0
	totalModified := 0
	totalRemoved := 0
	maxPages := 20 // Safety limit

	for {
		pageCount++
		if pageCount > maxPages {
			result.Status = StatusWarn
			result.Message = fmt.Sprintf("Stopped at %d pages (safety limit)", maxPages)
			result.Details["page_count"] = pageCount
			result.Details["total_added"] = totalAdded
			result.Details["total_modified"] = totalModified
			result.Details["total_removed"] = totalRemoved
			result.Duration = time.Since(start)
			return result
		}

		// Use 30 days on first page, 0 on subsequent (cursor-based)
		daysRequested := 0
		if cursor == "" {
			daysRequested = 30
		}

		resp, err := opts.PlaidService.TransactionsSync(ctx, accessToken, cursor, daysRequested)
		if err != nil {
			result.Status = StatusFail
			result.Message = fmt.Sprintf("Pagination failed on page %d: %v", pageCount, err)
			result.Error = err
			result.Duration = time.Since(start)
			return result
		}

		totalAdded += len(resp.GetAdded())
		totalModified += len(resp.GetModified())
		totalRemoved += len(resp.GetRemoved())
		cursor = resp.GetNextCursor()

		if !resp.GetHasMore() {
			break
		}
	}

	result.Details["page_count"] = pageCount
	result.Details["total_added"] = totalAdded
	result.Details["total_modified"] = totalModified
	result.Details["total_removed"] = totalRemoved
	result.Details["final_cursor_length"] = len(cursor)

	if pageCount == 1 {
		result.Status = StatusPass
		result.Message = fmt.Sprintf("Sync completed in 1 page (%d transactions)", totalAdded+totalModified)
	} else {
		result.Status = StatusPass
		result.Message = fmt.Sprintf("Pagination verified: %d pages, %d total transactions", pageCount, totalAdded+totalModified)
	}

	result.Duration = time.Since(start)
	return result
}
