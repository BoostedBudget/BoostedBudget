package observability

import (
	"context"
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

func (t *teeHandler) Handle(ctx context.Context, r slog.Record) error {
	t.primary.Handle(ctx, r)
	t.secondary.Handle(ctx, r)
	return nil
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
