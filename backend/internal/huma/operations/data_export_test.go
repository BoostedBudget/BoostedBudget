package operations

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDataExportService is a stub services.DataExportService that records the user id it was asked
// about, so a test can prove the handler exports the AUTHENTICATED user rather than anyone else.
type mockDataExportService struct {
	export      *domain.DataExport
	err         error
	askedUserID string
	calls       int
}

func (m *mockDataExportService) ExportUserData(_ context.Context, userID string) (*domain.DataExport, error) {
	m.calls++
	m.askedUserID = userID
	if m.err != nil {
		return nil, m.err
	}
	return m.export, nil
}

func (m *mockDataExportService) SetAnalytics(_ phclient.Analytics) {}

func sampleExport() *domain.DataExport {
	return &domain.DataExport{
		ExportVersion: domain.DataExportVersion,
		GeneratedAt:   time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC),
		UserID:        "user-123",
		Sections: map[string][]map[string]any{
			"transactions": {{"id": "txn-1", "amount": json.Number("12.34")}},
			"budgets":      {},
		},
		ExcludedTables: map[string]string{"audit_logs": "security audit trail"},
		RowCount:       1,
	}
}

func TestGetDataExport_ExportsTheAuthenticatedUser(t *testing.T) {
	service := &mockDataExportService{export: sampleExport()}
	handler := NewDataExportHandler(service)

	output, err := handler.GetDataExport(context.Background(), &GetDataExportInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	})

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "user-123", service.askedUserID,
		"the handler must export the id the middleware authenticated, not any other")
	assert.Equal(t, "user-123", output.Body.UserID)
	assert.Equal(t, 1, output.Body.RowCount)
}

// TestGetDataExport_SetsADownloadFilename pins the Content-Disposition, because it is the whole
// difference between the browser saving the user's financial history and rendering ~50 sections of
// JSON into a tab.
func TestGetDataExport_SetsADownloadFilename(t *testing.T) {
	service := &mockDataExportService{export: sampleExport()}
	handler := NewDataExportHandler(service)

	output, err := handler.GetDataExport(context.Background(), &GetDataExportInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	})

	require.NoError(t, err)
	assert.Equal(t, `attachment; filename="boostedbudget-export-2026-08-25.json"`, output.ContentDisposition)
}

func TestGetDataExport_ServiceFailure_Returns500(t *testing.T) {
	service := &mockDataExportService{err: assert.AnError}
	handler := NewDataExportHandler(service)

	output, err := handler.GetDataExport(context.Background(), &GetDataExportInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	})

	assert.Nil(t, output)
	require.Error(t, err)
	var status huma.StatusError
	require.ErrorAs(t, err, &status)
	assert.Equal(t, http.StatusInternalServerError, status.GetStatus())
}

// TestDataExportRoute_IsReachable is the routing evidence. The handler tests above pass just as
// happily when nothing routes to the handler at all; only a router says whether the path resolves.
//
// 401 rather than 404 is the assertion that matters: the route exists and the auth resolver ran.
func TestDataExportRoute_IsReachable(t *testing.T) {
	_, api := humatest.New(t)
	RegisterDataExportRoutes(api, NewDataExportHandler(&mockDataExportService{export: sampleExport()}))

	resp := api.Get("/api/v1/user/data-export")

	assert.Equal(t, http.StatusUnauthorized, resp.Code,
		"expected the route to exist and reject the unauthenticated call; 404 would mean it is not "+
			"registered at all")
	assert.Contains(t, resp.Body.String(), "User not authenticated")
}

// TestDataExportRoute_HasNoParamSiblingToLoseTo guards the routing hazard rather than the router.
//
// The hazard is real and this project has already been bitten by it: a literal segment registered
// alongside a `{param}` sibling resolves differently in humatest (registration-order sensitive) and
// in production Echo v4 (always prefers the literal), so a route can look dead in tests and work in
// production, or the reverse. `data-export` is hyphenated so no plausible sibling collides — but
// "no plausible sibling" is a claim about the route table, not about the handler, so it is checked
// against the route table.
//
// ⚠ A humatest-based "the literal wins" test was tried first and REMOVED: humatest resolves
// /api/v1/user/data-export to a `{section}` sibling registered before it, which says nothing about
// production and would have been a permanently red test asserting a false thing. What is checkable
// is that the ambiguity does not exist in the first place.
func TestDataExportRoute_HasNoParamSiblingToLoseTo(t *testing.T) {
	_, api := humatest.New(t)
	RegisterAllRoutesForSpec(api)

	const exportPath = "/api/v1/user/data-export"
	require.Contains(t, api.OpenAPI().Paths, exportPath,
		"the export route is missing from RegisterAllRoutesForSpec — `make generate-openapi` reads "+
			"that function and NOT cmd/server/main.go, so a route wired only in main.go produces a "+
			"zero-byte spec diff and an endpoint the generated client cannot see")

	var siblings []string
	for path := range api.OpenAPI().Paths {
		rest, isUserPath := strings.CutPrefix(path, "/api/v1/user/")
		if !isUserPath || path == exportPath {
			continue
		}
		// A single templated segment directly under /api/v1/user/ is the shape that could swallow
		// /api/v1/user/data-export.
		if !strings.Contains(rest, "/") && strings.HasPrefix(rest, "{") {
			siblings = append(siblings, path)
		}
	}
	assert.Empty(t, siblings,
		"a {param} sibling now shares /api/v1/user/ with the literal %s. humatest and Echo v4 "+
			"disagree about which one wins, so this must be resolved deliberately: register the "+
			"literal first AND pin the resolution with a router-level test that runs against the "+
			"production router, or move the parameterised route under its own prefix", exportPath)
}
