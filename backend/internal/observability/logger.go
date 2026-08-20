package observability

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	slogsentry "github.com/samber/slog-sentry/v2"
	"go.opentelemetry.io/contrib/bridges/otelslog"
)

// LogConfig holds configuration for the unified logger.
type LogConfig struct {
	Level        string // "debug", "info", "warn", "error" — default "info"
	Environment  string
	ServiceName  string
	Version      string
	SentryDSN    string // if non-empty, route Error+ to Sentry
	OTELEndpoint string // if non-empty, bridge logs to OTEL
}

// NewLogger creates a unified slog.Logger.
// - Base handler: JSON to stdout
// - If SentryDSN set: tee Error/Fatal to Sentry via slogsentry
// - If OTELEndpoint set: bridge to OTEL logs via otelslog
// - Always adds service/version/env attributes
func NewLogger(cfg LogConfig) *slog.Logger {
	level := parseLogLevel(cfg.Level)

	var handler slog.Handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	handler = NewTracingHandler(handler)

	// Tee to Sentry for error+ if DSN configured
	if cfg.SentryDSN != "" {
		sentryHandler := slogsentry.Option{Level: slog.LevelError}.NewSentryHandler()
		handler = &teeHandler{primary: handler, secondary: sentryHandler}
	}

	// Bridge to OTEL logs if endpoint configured
	if cfg.OTELEndpoint != "" {
		otelHandler := otelslog.NewHandler(cfg.ServiceName)
		handler = &teeHandler{primary: handler, secondary: otelHandler}
	}

	return slog.New(handler).With(
		"service", cfg.ServiceName,
		"version", cfg.Version,
		"environment", cfg.Environment,
	)
}

// parseLogLevel converts a string level to slog.Level. Defaults to Info.
func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// teeHandler sends log records to two handlers.
type teeHandler struct {
	primary   slog.Handler
	secondary slog.Handler
}

func (t *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return t.primary.Enabled(ctx, level) || t.secondary.Enabled(ctx, level)
}

// Handle fans the record out to both handlers and reports every failure.
//
// Two things it deliberately does, both of which it used to get wrong (#1537 round 4, found as two
// errcheck findings — the unchecked returns were the symptom, silent failure was the defect):
//
//   - It JOINS the errors instead of discarding them. Returning nil unconditionally made a secondary
//     handler that was failing every write indistinguishable from success. Joining rather than
//     returning the first also means one broken transport cannot hide another.
//   - It gates each handler on its OWN Enabled. Enabled below is an OR across members, so reaching
//     Handle proves only that SOMETHING wanted the record — a member that declined the level was
//     still being handed it. slog's "Handle is only called when Enabled is true" guarantee applies to
//     the tee, not to what the tee wraps.
func (t *teeHandler) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	if t.primary.Enabled(ctx, r.Level) {
		if err := t.primary.Handle(ctx, r); err != nil {
			errs = append(errs, fmt.Errorf("primary log handler: %w", err))
		}
	}
	if t.secondary.Enabled(ctx, r.Level) {
		if err := t.secondary.Handle(ctx, r); err != nil {
			errs = append(errs, fmt.Errorf("secondary log handler: %w", err))
		}
	}
	return errors.Join(errs...)
}

func (t *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &teeHandler{
		primary:   t.primary.WithAttrs(attrs),
		secondary: t.secondary.WithAttrs(attrs),
	}
}

func (t *teeHandler) WithGroup(name string) slog.Handler {
	return &teeHandler{
		primary:   t.primary.WithGroup(name),
		secondary: t.secondary.WithGroup(name),
	}
}
