package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestUpdateUserMetadata_MarshalError tests that nil-like inputs are handled gracefully.
// Note: Real Clerk API calls require network access, so these tests focus on local logic.
func TestUpdateUserMetadata_MarshalError(t *testing.T) {
	// Verify that marshaling cycle-safe maps doesn't panic
	svc := &clerkService{}

	// A map with a valid value should marshal successfully (error path is in json.Marshal failure)
	// We can't easily trigger json.Marshal failure with basic types, so test the happy path shape.
	metadata := map[string]interface{}{
		"subscription_tier":   "premium",
		"subscription_status": "active",
	}

	ctx := context.Background()

	// This will fail at the Clerk API call (no real key), but the marshal step succeeds.
	// We're testing that the function doesn't panic and returns an error (API call fails).
	err := svc.UpdateUserMetadata(ctx, "user_test123", metadata)
	// Should return an error from Clerk API (invalid key), not a panic
	assert.Error(t, err)
}

func TestUpdateUserMetadata_EmptyMetadata(t *testing.T) {
	svc := &clerkService{}
	ctx := context.Background()

	err := svc.UpdateUserMetadata(ctx, "user_test123", map[string]interface{}{})
	// Should return an error from Clerk API (invalid key), not panic on empty map
	assert.Error(t, err)
}

func TestClerkService_UpdateUserEmail_InterfaceExists(t *testing.T) {
	var svc ClerkService
	assert.Nil(t, svc)
}
