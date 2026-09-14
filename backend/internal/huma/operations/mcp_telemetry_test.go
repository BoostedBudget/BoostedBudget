package operations

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingMCPProductAnalytics struct {
	userID string
	event  phclient.TypedEvent
}

type mcpTelemetryAuthority struct {
	granted bool
	err     error
}

func (a mcpTelemetryAuthority) WithConsentedSubject(_ context.Context, _ string, emit func(string)) (bool, error) {
	if a.err != nil {
		return false, a.err
	}
	if a.granted {
		emit("canonical-clerk-subject")
	}
	return a.granted, nil
}

type mcpTelemetryTransport struct {
	captures int
}

func (t *mcpTelemetryTransport) Capture(_ string, _ string, _ map[string]interface{}) { t.captures++ }
func (*mcpTelemetryTransport) Identify(_ string, _ map[string]interface{})            {}
func (*mcpTelemetryTransport) Close()                                                 {}

func (r *recordingMCPProductAnalytics) CaptureEvent(_ context.Context, userID string, event phclient.TypedEvent) {
	r.userID = userID
	r.event = event
}

func TestMCPToolTelemetryUsesAuthenticatedInternalUserAndTypedEvent(t *testing.T) {
	analytics := &recordingMCPProductAnalytics{}
	handler := NewMCPToolTelemetryHandler(analytics, "worker-secret")
	input := &RecordMCPToolTelemetryInput{
		MCPWorkerSecret: "worker-secret",
		Body: RecordMCPToolTelemetryBody{
			ToolSurface:    "read_execute",
			ToolFamily:     "transactions",
			Outcome:        "succeeded",
			DurationBucket: "250ms_to_1s",
		},
	}
	input.UserID = "internal-user-uuid"

	output, err := handler.Record(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "internal-user-uuid", analytics.userID)
	require.NotNil(t, analytics.event)
	assert.Equal(t, phclient.EventName("mcp_tool_completed"), analytics.event.EventName())
	assert.Equal(t, map[string]interface{}{
		"tool_surface":    phclient.MCPToolCompletedToolSurface("read_execute"),
		"tool_family":     phclient.MCPToolCompletedToolFamily("transactions"),
		"outcome":         phclient.MCPToolCompletedOutcome("succeeded"),
		"duration_bucket": phclient.MCPToolCompletedDurationBucket("250ms_to_1s"),
	}, analytics.event.Properties())
}

func TestMCPToolTelemetryIsNoOpSafeWhenAnalyticsUnavailable(t *testing.T) {
	handler := NewMCPToolTelemetryHandler(nil, "worker-secret")
	input := &RecordMCPToolTelemetryInput{Body: RecordMCPToolTelemetryBody{
		ToolSurface: "search", ToolFamily: "discovery", Outcome: "failed", DurationBucket: "under_250ms",
	}}
	input.MCPWorkerSecret = "worker-secret"
	input.UserID = "internal-user-uuid"

	output, err := handler.Record(context.Background(), input)

	require.NoError(t, err)
	assert.NotNil(t, output)
}

func TestMCPToolTelemetryResponseDoesNotRevealConsentOrAuthorityState(t *testing.T) {
	for _, test := range []struct {
		name         string
		authority    mcpTelemetryAuthority
		wantCaptures int
	}{
		{name: "consented active user", authority: mcpTelemetryAuthority{granted: true}, wantCaptures: 1},
		{name: "consent denied", authority: mcpTelemetryAuthority{}, wantCaptures: 0},
		{name: "deleted user", authority: mcpTelemetryAuthority{}, wantCaptures: 0},
		{name: "authority unavailable", authority: mcpTelemetryAuthority{err: errors.New("database unavailable")}, wantCaptures: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := &mcpTelemetryTransport{}
			gateway := phclient.NewGateway(
				transport,
				test.authority,
				slog.New(slog.NewTextHandler(io.Discard, nil)),
			)
			handler := NewMCPToolTelemetryHandler(gateway, "worker-secret")
			input := &RecordMCPToolTelemetryInput{Body: RecordMCPToolTelemetryBody{
				ToolSurface: "search", ToolFamily: "discovery", Outcome: "succeeded", DurationBucket: "under_250ms",
			}}
			input.MCPWorkerSecret = "worker-secret"
			input.UserID = "internal-user-uuid"

			output, err := handler.Record(context.Background(), input)

			require.NoError(t, err)
			assert.NotNil(t, output)
			assert.Equal(t, test.wantCaptures, transport.captures)
		})
	}
}

func TestMCPToolTelemetryBodyCannotCarryIdentityOrFreeFormPayload(t *testing.T) {
	typ := reflect.TypeOf(RecordMCPToolTelemetryBody{})
	got := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		got = append(got, typ.Field(i).Tag.Get("json"))
	}
	assert.ElementsMatch(t, []string{"tool_surface", "tool_family", "outcome", "duration_bucket"}, got)
}

func TestMCPToolTelemetryRouteRequiresAuthentication(t *testing.T) {
	_, api := humatest.New(t)
	RegisterMCPToolTelemetryRoutes(api, NewMCPToolTelemetryHandler(nil, "worker-secret"))

	resp := api.Post("/api/v1/mcp/telemetry", map[string]interface{}{
		"tool_surface": "search", "tool_family": "discovery", "outcome": "succeeded", "duration_bucket": "under_250ms",
	})

	assert.Equal(t, http.StatusUnauthorized, resp.Code)
}

func TestMCPToolTelemetryRouteReturnsNoContentForAuthenticatedRequests(t *testing.T) {
	_, api := humatest.New(t)
	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		next(huma.WithValue(ctx, inputs.UserIDKey, "authenticated-internal-user"))
	})
	RegisterMCPToolTelemetryRoutes(api, NewMCPToolTelemetryHandler(nil, "worker-secret"))

	resp := api.Post("/api/v1/mcp/telemetry", "X-MCP-Worker-Secret: worker-secret", map[string]interface{}{
		"tool_surface": "search", "tool_family": "discovery", "outcome": "succeeded", "duration_bucket": "under_250ms",
	})

	assert.Equal(t, http.StatusNoContent, resp.Code)
	assert.Empty(t, resp.Body.String())
}

func TestMCPToolTelemetryRouteRejectsMissingOrInvalidWorkerSecret(t *testing.T) {
	for _, header := range []string{"", "X-MCP-Worker-Secret: wrong-secret"} {
		_, api := humatest.New(t)
		api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
			next(huma.WithValue(ctx, inputs.UserIDKey, "authenticated-internal-user"))
		})
		RegisterMCPToolTelemetryRoutes(api, NewMCPToolTelemetryHandler(nil, "worker-secret"))
		args := []interface{}{map[string]interface{}{
			"tool_surface": "search", "tool_family": "discovery", "outcome": "succeeded", "duration_bucket": "under_250ms",
		}}
		if header != "" {
			args = append([]interface{}{header}, args...)
		}

		resp := api.Post("/api/v1/mcp/telemetry", args...)

		assert.Equal(t, http.StatusForbidden, resp.Code)
	}
}

func TestMCPToolTelemetryRouteRejectsUnknownFieldsAndUnboundedValues(t *testing.T) {
	for _, test := range []struct {
		name string
		body map[string]interface{}
	}{
		{name: "client supplied identity", body: map[string]interface{}{
			"tool_surface": "search", "tool_family": "discovery", "outcome": "succeeded", "duration_bucket": "under_250ms",
			"distinct_id": "attacker-selected-subject",
		}},
		{name: "arbitrary tool surface", body: map[string]interface{}{
			"tool_surface": "user-supplied-operation-id", "tool_family": "discovery", "outcome": "succeeded", "duration_bucket": "under_250ms",
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, api := humatest.New(t)
			api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
				next(huma.WithValue(ctx, inputs.UserIDKey, "authenticated-internal-user"))
			})
			RegisterMCPToolTelemetryRoutes(api, NewMCPToolTelemetryHandler(nil, "worker-secret"))

			resp := api.Post("/api/v1/mcp/telemetry", "X-MCP-Worker-Secret: worker-secret", test.body)

			assert.Equal(t, http.StatusUnprocessableEntity, resp.Code, "body: %s", resp.Body.String())
		})
	}
}

func TestMCPToolTelemetryPublishedSchemaIsClosedAndBounded(t *testing.T) {
	_, api := humatest.New(t)
	RegisterAllRoutesForSpec(api)

	document, err := json.Marshal(api.OpenAPI())
	require.NoError(t, err)
	var spec map[string]interface{}
	require.NoError(t, json.Unmarshal(document, &spec))
	paths := spec["paths"].(map[string]interface{})
	path, ok := paths["/api/v1/mcp/telemetry"].(map[string]interface{})
	require.True(t, ok, "telemetry route must be in the generated OpenAPI route table")
	post := path["post"].(map[string]interface{})
	requestBody := post["requestBody"].(map[string]interface{})
	content := requestBody["content"].(map[string]interface{})
	schema := content["application/json"].(map[string]interface{})["schema"].(map[string]interface{})
	ref := schema["$ref"].(string)
	components := spec["components"].(map[string]interface{})["schemas"].(map[string]interface{})
	bodySchema := components[ref[len("#/components/schemas/"):]].(map[string]interface{})
	properties := bodySchema["properties"].(map[string]interface{})

	assert.ElementsMatch(t,
		[]string{"tool_surface", "tool_family", "outcome", "duration_bucket"},
		mapKeys(properties),
	)
	assert.Equal(t, false, bodySchema["additionalProperties"])
	assert.ElementsMatch(t,
		[]interface{}{"search", "read_execute", "write_execute", "recipe_list", "recipe_save", "recipe_run", "recipe_templates"},
		properties["tool_surface"].(map[string]interface{})["enum"],
	)
}

func mapKeys(values map[string]interface{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
