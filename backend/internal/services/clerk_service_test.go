package services

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"testing"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/clerktest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpdateUserMetadata_MarshalError (#1413 G7: strengthened) actually exercises the
// json.Marshal failure path in clerk_service.go's UpdateUserMetadata, rather than relying on a
// live Clerk API call failing for an unrelated reason (invalid key / no network). The previous
// version of this test built a perfectly marshalable map and asserted only assert.Error(t, err)
// — that would pass identically whether the error came from a marshal failure, an HTTP error, a
// 4xx from Clerk, or any other failure whatsoever, so it never actually proved the marshal-error
// branch ran. math.Inf(1) is a value encoding/json's Marshal rejects
// (*json.UnsupportedValueError: "unsupported value: +Inf") BEFORE any network call is attempted,
// so asserting the specific wrapped error text pins this test to the marshal path specifically.
func TestUpdateUserMetadata_MarshalError(t *testing.T) {
	svc := &clerkService{}

	metadata := map[string]interface{}{
		"subscription_tier": math.Inf(1), // json.Marshal fails on NaN/Inf floats
	}

	ctx := context.Background()

	err := svc.UpdateUserMetadata(ctx, "user_test123", metadata)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to marshal Clerk metadata",
		"must fail at the marshal step specifically, not merely fail for any reason")
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

// TestUpdateUserMetadata_UsesMergeEndpoint_NotFullUserReplace (#1413 G7: new — nothing
// previously enforced this). domain.User.ClerkPublicMetadata()'s doc comment
// (internal/domain/user.go) explains that its explicit "tier": nil relies on Clerk's PATCH
// /v1/users/{id}/metadata endpoint performing a shallow MERGE into existing publicMetadata —
// deleting only the "tier" key and leaving every other existing key untouched. clerk_service.go's
// own doc comment claims "Uses UpdateMetadata which merges with existing metadata rather than
// replacing" but nothing ran that claim through an assertion: swap the call to user.Update (PATCH
// /v1/users/{id}, a full-record replace) and every previous test here would still pass, since
// they only ever look at the returned error, never the outgoing request.
//
// This test intercepts the Clerk SDK's global Backend with clerktest.RoundTripper (the same
// mechanism the SDK's own client tests use) and asserts on the actual outgoing request path,
// which is the one observable difference between the two SDK calls for an otherwise-identical
// payload (user.UpdateMetadata: PATCH .../metadata; user.Update: PATCH just .../{id}) — so a
// future swap of the transport fails this test loudly instead of silently.
func TestUpdateUserMetadata_UsesMergeEndpoint_NotFullUserReplace(t *testing.T) {
	clerkUserID := "user_merge_check"
	metadata := map[string]interface{}{
		"subscription_tier":   "premium",
		"subscription_status": "active",
		"tier":                nil,
	}
	metadataJSON, err := json.Marshal(metadata)
	require.NoError(t, err)
	expectedBody, err := json.Marshal(map[string]json.RawMessage{"public_metadata": metadataJSON})
	require.NoError(t, err)

	origBackend := clerk.GetBackend()
	t.Cleanup(func() { clerk.SetBackend(origBackend) })
	clerk.SetBackend(clerk.NewBackend(&clerk.BackendConfig{
		HTTPClient: &http.Client{
			Transport: &clerktest.RoundTripper{
				T:      t,
				Method: http.MethodPatch,
				Path:   "/v1/users/" + clerkUserID + "/metadata", // NOT "/v1/users/{id}" (user.Update)
				In:     json.RawMessage(expectedBody),
				Out:    json.RawMessage(`{"id":"` + clerkUserID + `"}`),
			},
		},
	}))

	svc := &clerkService{}
	err = svc.UpdateUserMetadata(context.Background(), clerkUserID, metadata)
	require.NoError(t, err, "must call the merge endpoint with the exact expected path and body")
}
