package middleware

import (
	"net/http"
	"strings"

	echomiddleware "github.com/labstack/echo/v4/middleware"
)

// GetCORSConfig returns an Echo CORS middleware configuration based on allowed origins
// If allowedOrigins is empty or "*", it allows all origins (dev/test only)
// For production, should be a comma-separated list of allowed origins
func GetCORSConfig(allowedOrigins string) echomiddleware.CORSConfig {
	// Parse allowed origins
	var origins []string
	if allowedOrigins == "" || allowedOrigins == "*" {
		// Allow all origins (development/testing only)
		origins = []string{"*"}
	} else {
		// Split comma-separated origins and trim whitespace
		originList := strings.Split(allowedOrigins, ",")
		for _, origin := range originList {
			trimmed := strings.TrimSpace(origin)
			if trimmed != "" {
				origins = append(origins, trimmed)
			}
		}
	}

	return echomiddleware.CORSConfig{
		AllowOrigins:     origins,
		AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowHeaders:     []string{"Authorization", "Content-Type", "X-Requested-With"},
		AllowCredentials: true,
		MaxAge:           86400, // 24 hours
	}
}
