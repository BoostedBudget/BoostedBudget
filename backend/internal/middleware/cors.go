package middleware

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	echomiddleware "github.com/labstack/echo/v4/middleware"
)

// ValidateCORSConfig checks that the CORS configuration is safe for the given environment.
// Returns an error if wildcard origins are used in production.
// Logs a warning if wildcard origins are used in staging.
// For non-wildcard origins, validates each is a valid http(s) URL.
func ValidateCORSConfig(allowedOrigins, environment string) error {
	isWildcard := allowedOrigins == "" || allowedOrigins == "*"

	if isWildcard {
		switch environment {
		case "production":
			return fmt.Errorf("CORS_ALLOWED_ORIGINS cannot be '*' or empty in production: set explicit allowed origins")
		case "staging":
			log.Println("[WARNING] CORS_ALLOWED_ORIGINS is '*' in staging — consider setting explicit origins")
		}
		return nil
	}

	// Validate each non-wildcard origin is a valid URL
	for _, origin := range strings.Split(allowedOrigins, ",") {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
			return fmt.Errorf("invalid CORS origin %q: must start with http:// or https://", trimmed)
		}
	}
	return nil
}

// GetCORSConfig returns an Echo CORS middleware configuration based on allowed origins
// If allowedOrigins is empty or "*", it allows all origins (dev/test only)
// For production, should be a comma-separated list of allowed origins
func GetCORSConfig(allowedOrigins string) echomiddleware.CORSConfig {
	var origins []string
	if allowedOrigins == "" || allowedOrigins == "*" {
		origins = []string{"*"}
	} else {
		// Split comma-separated origins and trim whitespace
		for _, origin := range strings.Split(allowedOrigins, ",") {
			if trimmed := strings.TrimSpace(origin); trimmed != "" {
				origins = append(origins, trimmed)
			}
		}
	}

	return echomiddleware.CORSConfig{
		AllowOrigins:     origins,
		AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowHeaders:     []string{"Authorization", "Content-Type", "X-Requested-With"},
		AllowCredentials: true,
		MaxAge:           86400,
	}
}
