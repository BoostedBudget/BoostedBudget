package handlers

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// SyncStatsHandler handles monthly sync statistics endpoints
// Issue #242: Monthly sync limits - expose usage stats to frontend
type SyncStatsHandler struct {
	syncLimitService services.SyncLimitService
}

// NewSyncStatsHandler creates a new SyncStatsHandler instance
func NewSyncStatsHandler(syncLimitService services.SyncLimitService) *SyncStatsHandler {
	return &SyncStatsHandler{
		syncLimitService: syncLimitService,
	}
}

// GetSyncStats returns monthly sync usage statistics for the authenticated user
// GET /api/v1/sync-stats
//
// Response 200:
//
//	{
//	  "user_id": "uuid",
//	  "month": "2025-11",
//	  "accounts_synced": 2,
//	  "accounts_limit": 3,
//	  "total_transactions": 150,
//	  "can_sync_more": true,
//	  "reset_date": "2025-12-01T00:00:00Z"
//	}
//
// Response 401: User not authenticated
// Response 500: Internal server error
func (h *SyncStatsHandler) GetSyncStats(c echo.Context) error {
	// Step 1: Get authenticated user ID from context (set by Auth0 middleware)
	userID, ok := c.Get("user_id").(string)
	if !ok || userID == "" {
		return echo.NewHTTPError(http.StatusUnauthorized, "user not authenticated")
	}

	// Step 2: Get sync statistics from service layer
	stats, err := h.syncLimitService.GetSyncStats(c.Request().Context(), userID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get sync statistics")
	}

	// Step 3: Return statistics as JSON
	return c.JSON(http.StatusOK, stats)
}
