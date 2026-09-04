package operations

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// DataExportHandler serves the GDPR Article 20 export (#1587).
type DataExportHandler struct {
	service services.DataExportService
}

// NewDataExportHandler creates a DataExportHandler.
func NewDataExportHandler(service services.DataExportService) *DataExportHandler {
	return &DataExportHandler{service: service}
}

// GetDataExportInput is the request.
//
// ⚠ IT CARRIES NO PARAMETER, AND THAT IS THE TENANCY DESIGN. The only user this endpoint can export
// is the one the Clerk middleware authenticated, because there is nothing in the request that could
// name a different one. An `?userId=` filter would have to be authorised; an absent one cannot be
// forgotten.
type GetDataExportInput struct {
	inputs.AuthenticatedInput
}

// GetDataExportOutput is the response.
type GetDataExportOutput struct {
	// ContentDisposition makes a browser navigation save the document instead of rendering it, with
	// a name the user can recognise a year later.
	ContentDisposition string `header:"Content-Disposition"`

	Body *domain.DataExport
}

// GetDataExport handles GET /api/v1/user/data-export.
//
// JSON, and only JSON. GDPR asks for a "structured, commonly used, machine-readable format", and one
// JSON document satisfies all three for a dataset that is fundamentally a graph: transactions
// reference accounts, budget categories reference budgets, contributions reference goals. A CSV
// bundle would have to be ~49 files in a zip, would flatten every foreign key into an opaque UUID
// column anyway, and would need its own archive-assembly and streaming surface — extra surface that
// buys the user nothing JSON does not already give them. If a CSV request ever arrives, the honest
// place for it is a converter over this payload, not a second export path that can disagree with it.
func (h *DataExportHandler) GetDataExport(ctx context.Context, input *GetDataExportInput) (*GetDataExportOutput, error) {
	export, err := h.service.ExportUserData(ctx, input.UserID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to generate data export")
	}

	return &GetDataExportOutput{
		ContentDisposition: fmt.Sprintf("attachment; filename=\"boostedbudget-export-%s.json\"",
			export.GeneratedAt.Format("2006-01-02")),
		Body: export,
	}, nil
}

// RegisterDataExportRoutes registers the export route with the Huma API.
//
// ⚠ REGISTERED IN BOTH PLACES, DELIBERATELY. `make generate-openapi` runs
// RegisterAllRoutesForSpec in register.go and never reads cmd/server/main.go, so a route wired only
// in main.go produces a zero-byte spec diff and an endpoint the generated client cannot see. This
// function is called from both.
//
// The path is hyphenated (`data-export`, not `export`) so that it cannot be confused with a
// `{param}` sibling under /api/v1/user/ by any router, present or future. Reachability is pinned by
// a router-level humatest (TestDataExportRoute_IsReachable), not by a handler test: a handler test
// passes just as happily when nothing routes to it.
func RegisterDataExportRoutes(api huma.API, handler *DataExportHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "getDataExport",
		Method:      http.MethodGet,
		Path:        "/api/v1/user/data-export",
		Summary:     "Export my data",
		Description: "Returns a machine-readable JSON export of the authenticated user's own data " +
			"(GDPR Article 20, right to data portability). Scoped strictly to the caller: rows " +
			"belonging to household peers are never included, and credentials, encrypted-at-rest " +
			"secrets and third-party authentication identifiers are excluded.",
		Tags:     []string{"Privacy"},
		Security: []map[string][]string{{"clerk": {}}},
	}, handler.GetDataExport)
}
