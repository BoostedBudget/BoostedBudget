package operations

import (
	"bytes"
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
)

// MetricsHandler handles Prometheus metrics requests.
type MetricsHandler struct{}

// NewMetricsHandler creates a new MetricsHandler instance.
func NewMetricsHandler() *MetricsHandler {
	return &MetricsHandler{}
}

// --- Input/Output Types ---

// GetMetricsInput represents the input for getting metrics (no params).
type GetMetricsInput struct{}

// GetMetricsOutput represents the response for metrics.
type GetMetricsOutput struct {
	ContentType string `header:"Content-Type"`
	Body        []byte
}

// --- Handler Methods ---

// GetMetrics handles GET /metrics requests.
// Returns Prometheus metrics in text format.
func (h *MetricsHandler) GetMetrics(ctx context.Context, input *GetMetricsInput) (*GetMetricsOutput, error) {
	// Gather metrics from the default registry
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to gather metrics")
	}

	// Encode metrics in text format
	var buf bytes.Buffer
	for _, mf := range mfs {
		if _, err := expfmt.MetricFamilyToText(&buf, mf); err != nil {
			return nil, huma.Error500InternalServerError("Failed to encode metrics")
		}
	}

	return &GetMetricsOutput{
		ContentType: "text/plain; version=0.0.4; charset=utf-8",
		Body:        buf.Bytes(),
	}, nil
}

// RegisterMetricsRoutes registers metrics routes with the Huma API.
//
// Issue #1514: this Operation has no `Security` annotation, so it carries no Clerk
// authentication regardless of which API instance it's registered on. That is safe ONLY
// because cmd/server/main.go registers it exclusively on the internal-only metrics
// listener (see internal_metrics.go), never on the public-facing humaAPI/e — the internal
// listener is unauthenticated but not reachable from the public internet (see
// bindInternalMetricsListener's "ACCEPTED RESIDUAL RISK" doc comment for what it IS
// reachable from). This function itself has no way to enforce that; it is enforced by
// main.go's wiring, which TestInternalMetricsListener_IsWiredIntoMain in
// cmd/server/issue1514_internal_metrics_listener_test.go guards against drifting.
func RegisterMetricsRoutes(api huma.API, handler *MetricsHandler) {
	// GET /metrics - Prometheus metrics
	huma.Register(api, huma.Operation{
		OperationID: "getMetrics",
		Method:      http.MethodGet,
		Path:        "/metrics",
		Summary:     "Get Prometheus metrics",
		Description: "Returns Prometheus metrics in text format for monitoring.",
		Tags:        []string{"Monitoring"},
	}, handler.GetMetrics)
}
