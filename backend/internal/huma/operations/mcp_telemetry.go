package operations

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
)

// MCPToolTelemetryHandler accepts only the bounded completion dimensions published in the
// generated PostHog contract. Authentication middleware supplies the internal user UUID.
type MCPToolTelemetryHandler struct {
	analytics       phclient.ProductAnalytics
	mcpWorkerSecret string
}

func NewMCPToolTelemetryHandler(analytics phclient.ProductAnalytics, mcpWorkerSecret string) *MCPToolTelemetryHandler {
	return &MCPToolTelemetryHandler{analytics: analytics, mcpWorkerSecret: mcpWorkerSecret}
}

type RecordMCPToolTelemetryBody struct {
	ToolSurface    string `json:"tool_surface" enum:"search,read_execute,write_execute,recipe_list,recipe_save,recipe_run,recipe_templates"`
	ToolFamily     string `json:"tool_family" enum:"accounts,analytics,budgets,boosts,savings,transactions,discovery,workflows,cross_domain,other"`
	Outcome        string `json:"outcome" enum:"succeeded,failed"`
	DurationBucket string `json:"duration_bucket" enum:"under_250ms,250ms_to_1s,1s_to_5s,over_5s"`
}

type RecordMCPToolTelemetryInput struct {
	inputs.AuthenticatedInput
	MCPWorkerSecret string `header:"X-MCP-Worker-Secret" required:"true" doc:"Worker-only shared secret; constant-time validated before accepting telemetry"`
	Body            RecordMCPToolTelemetryBody
}

type RecordMCPToolTelemetryOutput struct{}

func (h *MCPToolTelemetryHandler) Record(ctx context.Context, input *RecordMCPToolTelemetryInput) (*RecordMCPToolTelemetryOutput, error) {
	if h == nil || h.mcpWorkerSecret == "" || !validMCPWorkerSecret(input.MCPWorkerSecret, h.mcpWorkerSecret) {
		return nil, huma.Error403Forbidden("invalid or missing Worker secret")
	}
	if h.analytics != nil {
		h.analytics.CaptureEvent(ctx, input.UserID, phclient.MCPToolCompletedPayload{
			ToolSurface:    phclient.MCPToolCompletedToolSurface(input.Body.ToolSurface),
			ToolFamily:     phclient.MCPToolCompletedToolFamily(input.Body.ToolFamily),
			Outcome:        phclient.MCPToolCompletedOutcome(input.Body.Outcome),
			DurationBucket: phclient.MCPToolCompletedDurationBucket(input.Body.DurationBucket),
		})
	}
	return &RecordMCPToolTelemetryOutput{}, nil
}

func validMCPWorkerSecret(provided, expected string) bool {
	providedDigest := sha256.Sum256([]byte(provided))
	expectedDigest := sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(providedDigest[:], expectedDigest[:]) == 1
}

func RegisterMCPToolTelemetryRoutes(api huma.API, handler *MCPToolTelemetryHandler) {
	huma.Register(api, huma.Operation{
		OperationID:   "recordMCPToolTelemetry",
		Method:        http.MethodPost,
		Path:          "/api/v1/mcp/telemetry",
		Summary:       "Record an MCP tool completion",
		Description:   "Records one bounded, best-effort MCP tool completion through the current-consent analytics gateway. Identity is derived from the authenticated OAuth bearer token, and the Worker secret is required.",
		Tags:          []string{"MCP Telemetry"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusNoContent,
	}, handler.Record)
}
