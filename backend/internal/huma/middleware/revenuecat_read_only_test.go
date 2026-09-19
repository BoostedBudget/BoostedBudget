package middleware_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/require"
)

func TestRevenueCatRefreshRemainsAvailableDuringAuthorityOutage(t *testing.T) {
	server, api := setupReadOnlyTestAPI(t)
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		huma.Register(api, huma.Operation{OperationID: "revenuecat-refresh-" + method, Method: method, Path: "/api/v1/subscription/authority/refresh", Security: []map[string][]string{{"clerk": {}}}}, func(context.Context, *struct{}) (*struct{ Body string }, error) {
			return &struct{ Body string }{Body: "handled"}, nil
		})
	}
	resp := doRequestUnresolved(t, server, http.MethodPost, "/api/v1/subscription/authority/refresh")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp = doRequestUnresolved(t, server, http.MethodDelete, "/api/v1/subscription/authority/refresh")
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode, "the recovery allowance is method-scoped")
}

func TestRevenueCatCheckoutCoordinationRemainsAvailableWithoutAccess(t *testing.T) {
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/subscription/checkout-intents"},
		{http.MethodDelete, "/api/v1/subscription/checkout-intents/{intent_id}"},
	} {
		t.Run(tc.method, func(t *testing.T) {
			server, api := setupReadOnlyTestAPI(t)
			huma.Register(api, huma.Operation{OperationID: "checkout-" + tc.method, Method: tc.method, Path: tc.path, Security: []map[string][]string{{"clerk": {}}}}, func(context.Context, *struct{}) (*struct{ Body string }, error) {
				return &struct{ Body string }{Body: "handled"}, nil
			})
			path := strings.ReplaceAll(tc.path, "{intent_id}", "id")
			resp := doRequestUnresolved(t, server, tc.method, path)
			require.Equal(t, http.StatusOK, resp.StatusCode)
		})
	}
}
