package operations

import (
	"context"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputePostHogIdentityHashKnownVector(t *testing.T) {
	got := computePostHogIdentityHash("The quick brown fox jumps over the lazy dog", "key")
	assert.Equal(t, "f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8", got)
}

func TestGetPostHogSupportIdentityUsesAuthenticatedSubject(t *testing.T) {
	handler := NewPostHogSupportIdentityHandler("phc_public", "server-only-secret")
	output, err := handler.Get(context.Background(), &GetPostHogSupportIdentityInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user_clerk_123"},
	})

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "user_clerk_123", output.Body.DistinctID)
	assert.Equal(t, computePostHogIdentityHash("user_clerk_123", "server-only-secret"), output.Body.IdentityHash)
	assert.Equal(t, "no-store", output.CacheControl)
}

func TestGetPostHogSupportIdentityFailsClosedWhenUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name      string
		publicKey string
		secret    string
	}{
		{name: "missing public project token", secret: "secret"},
		{name: "missing secret token", publicKey: "phc_public"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := NewPostHogSupportIdentityHandler(tc.publicKey, tc.secret)
			output, err := handler.Get(context.Background(), &GetPostHogSupportIdentityInput{
				AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user_clerk_123"},
			})
			assert.Nil(t, output)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "Support is unavailable")
			assert.NotContains(t, err.Error(), "secret")
		})
	}
}

func TestPostHogSupportIdentityRouteRequiresAuthentication(t *testing.T) {
	_, api := humatest.New(t)
	RegisterPostHogSupportIdentityRoutes(api, NewPostHogSupportIdentityHandler("phc_public", "secret"))

	resp := api.Get("/api/v1/user/posthog-support-identity")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)
}
