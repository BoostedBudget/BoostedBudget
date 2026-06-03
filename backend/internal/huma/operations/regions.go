package operations

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
)

// GetRegionsInput represents the input for the regions endpoint (no parameters needed).
type GetRegionsInput struct{}

// GetRegionsOutput represents the regions response.
type GetRegionsOutput struct {
	// CacheControl header for CDN caching (24 hours - regions rarely change)
	CacheControl string `header:"Cache-Control"`
	Body         RegionsResponseBody
}

// RegionResponse is the public-facing region data returned by the API.
// Infrastructure details (env var names) are intentionally excluded.
type RegionResponse struct {
	Code        string `json:"code" doc:"Fly.io IATA region code"`
	DisplayName string `json:"display_name" doc:"Human-readable region name"`
	Country     string `json:"country" doc:"Country name"`
	City        string `json:"city" doc:"City name"`
	Flag        string `json:"flag" doc:"Country flag emoji"`
}

// RegionsResponseBody is the response body for GET /api/v1/regions.
type RegionsResponseBody struct {
	Regions       map[string]RegionResponse `json:"regions" doc:"Available regions keyed by Fly.io IATA code"`
	DefaultRegion string                    `json:"default_region" doc:"Default region for new users"`
	ServerRegion  string                    `json:"server_region" doc:"Region this server instance runs in"`
}

// RegionsHandler handles region configuration requests.
type RegionsHandler struct {
	regionConfig *config.RegionConfig
}

// NewRegionsHandler creates a new RegionsHandler.
// Panics if regionConfig is nil (required dependency).
func NewRegionsHandler(regionConfig *config.RegionConfig) *RegionsHandler {
	if regionConfig == nil {
		panic("regionConfig is required for RegionsHandler")
	}
	return &RegionsHandler{regionConfig: regionConfig}
}

// GetRegions handles GET /api/v1/regions requests.
// Returns all available data residency regions with display info.
// Infrastructure details are excluded from the response.
// This is a public endpoint (no authentication required).
func (h *RegionsHandler) GetRegions(_ context.Context, _ *GetRegionsInput) (*GetRegionsOutput, error) {
	codes := h.regionConfig.GetValidRegionCodes()
	regions := make(map[string]RegionResponse, len(codes))
	for _, code := range codes {
		def, err := h.regionConfig.GetRegion(code)
		if err != nil {
			continue
		}
		regions[code] = RegionResponse{
			Code:        code,
			DisplayName: def.DisplayName,
			Country:     def.Country,
			City:        def.City,
			Flag:        def.Flag,
		}
	}
	return &GetRegionsOutput{
		CacheControl: "public, max-age=86400",
		Body: RegionsResponseBody{
			Regions:       regions,
			DefaultRegion: h.regionConfig.GetDefaultRegion(),
			ServerRegion:  h.regionConfig.GetServerRegion(),
		},
	}, nil
}

// RegisterRegionRoutes registers the regions route with the Huma API.
func RegisterRegionRoutes(api huma.API, handler *RegionsHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "getRegions",
		Method:      http.MethodGet,
		Path:        "/api/v1/regions",
		Summary:     "Get available regions",
		Description: "Returns all available data residency regions. Use this to populate region selectors. This is a public endpoint.",
		Tags:        []string{"Regions"},
	}, handler.GetRegions)
}
