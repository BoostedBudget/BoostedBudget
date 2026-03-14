package observability

import (
	"time"

	"github.com/getsentry/sentry-go"
)

// SentryConfig holds Sentry SDK configuration.
type SentryConfig struct {
	DSN         string
	Environment string
	Release     string
	SampleRate  float64
	Debug       bool
}

// InitSentry initializes the Sentry SDK. Returns nil if DSN is empty (no-op).
func InitSentry(cfg SentryConfig) error {
	if cfg.DSN == "" {
		return nil
	}
	sampleRate := cfg.SampleRate
	if sampleRate == 0 {
		sampleRate = 1.0
	}
	return sentry.Init(sentry.ClientOptions{
		Dsn:         cfg.DSN,
		Environment: cfg.Environment,
		Release:     cfg.Release,
		SampleRate:  sampleRate,
		Debug:       cfg.Debug,
	})
}

// FlushSentry flushes buffered events to Sentry with a timeout.
func FlushSentry(timeout time.Duration) {
	sentry.Flush(timeout)
}
