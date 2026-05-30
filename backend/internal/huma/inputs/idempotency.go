package inputs

// IdempotencyInput reads the Idempotency-Key header from the request.
// Embed this in write operation input structs that need idempotency guarantees.
//
// The Worker injects this header for MCP write operations. Go backend handlers
// that need idempotency should embed this and call idempotencyRepo.GetOrCreate().
//
// Usage:
//
//	type CreateBudgetInput struct {
//	    inputs.AuthenticatedInput
//	    inputs.IdempotencyInput
//	    Body domain.CreateBudgetRequest
//	}
//
//	// In handler:
//	if input.IdempotencyKey != "" {
//	    return idempotencyRepo.GetOrCreate(ctx, input.IdempotencyKey, input.UserID, "createBudget", fn, &result)
//	}
type IdempotencyInput struct {
	// IdempotencyKey is the value of the Idempotency-Key header.
	// Empty when the header is not present (non-MCP callers).
	IdempotencyKey string `header:"Idempotency-Key" doc:"Idempotency key for MCP write operations (optional)"`
}
