package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var appPanicsTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "app_panics_total",
	Help: "Total number of panics recovered by the application.",
})

// PanicRecovery returns an Echo middleware that recovers from panics,
// logs them via slog, increments the panic counter, and returns an RFC 9457 response.
// This runs BEFORE sentryecho so it formats the response; sentryecho reports to Sentry.
func PanicRecovery(logger *slog.Logger, env string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) (err error) {
			defer func() {
				if r := recover(); r != nil {
					appPanicsTotal.Inc()
					stack := debug.Stack()
					logger.ErrorContext(c.Request().Context(), "panic recovered",
						"panic", fmt.Sprintf("%v", r),
						"stack", string(stack),
					)

					body := map[string]any{
						"status": http.StatusInternalServerError,
						"title":  "Internal Server Error",
						"detail": "An unexpected error occurred",
					}
					if env != "production" {
						body["panic"] = fmt.Sprintf("%v", r)
					}

					err = c.JSON(http.StatusInternalServerError, body)
				}
			}()
			return next(c)
		}
	}
}
