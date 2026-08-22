package observability

import (
	"errors"
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingHandler is a minimal slog.Handler that records every Handle call and WithGroup call.
type recordingHandler struct {
	mu       sync.Mutex
	records  []slog.Record
	groups   []string
	enabled  bool
}

func newRecordingHandler() *recordingHandler { return &recordingHandler{enabled: true} }

func (r *recordingHandler) Enabled(_ context.Context, _ slog.Level) bool { return r.enabled }

func (r *recordingHandler) Handle(_ context.Context, rec slog.Record) error {
	r.mu.Lock()
	r.records = append(r.records, rec)
	r.mu.Unlock()
	return nil
}

func (r *recordingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return r }

func (r *recordingHandler) WithGroup(name string) slog.Handler {
	r.mu.Lock()
	r.groups = append(r.groups, name)
	r.mu.Unlock()
	return r
}

func (r *recordingHandler) recordCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.records)
}

func TestNewLogger_NoExternalSinks(t *testing.T) {
	cfg := LogConfig{
		Level:        "info",
		Environment:  "test",
		ServiceName:  "test-service",
		Version:      "0.0.1",
		SentryDSN:    "",
		OTELEndpoint: "",
	}
	logger := NewLogger(cfg)
	require.NotNil(t, logger, "NewLogger should not return nil")
	// Should not panic when logging
	logger.Info("test message")
}

func TestNewLogger_WithSentryDSN(t *testing.T) {
	cfg := LogConfig{
		Level:       "info",
		Environment: "test",
		ServiceName: "test-service",
		Version:     "0.0.1",
		SentryDSN:   "https://fake@sentry.io/123",
	}
	// Should not panic even with a fake DSN (Sentry handler is initialized lazily)
	logger := NewLogger(cfg)
	require.NotNil(t, logger)
}

func TestNewLogger_WithOTELEndpoint(t *testing.T) {
	cfg := LogConfig{
		Level:        "debug",
		Environment:  "test",
		ServiceName:  "test-service",
		Version:      "0.0.1",
		OTELEndpoint: "localhost:4318",
	}
	logger := NewLogger(cfg)
	require.NotNil(t, logger)
}

func TestTeeHandler_Enabled(t *testing.T) {
	tee := &teeHandler{
		primary:   slog.NewJSONHandler(nil, &slog.HandlerOptions{Level: slog.LevelInfo}),
		secondary: slog.NewJSONHandler(nil, &slog.HandlerOptions{Level: slog.LevelInfo}),
	}

	ctx := t.Context()
	assert.False(t, tee.Enabled(ctx, slog.LevelDebug))
	assert.True(t, tee.Enabled(ctx, slog.LevelInfo))
	assert.True(t, tee.Enabled(ctx, slog.LevelWarn))
}

func TestTeeHandler_Enabled_PrimaryDisabledSecondaryEnabled(t *testing.T) {
	tee := &teeHandler{
		primary:   slog.NewJSONHandler(nil, &slog.HandlerOptions{Level: slog.LevelError}),
		secondary: slog.NewJSONHandler(nil, &slog.HandlerOptions{Level: slog.LevelDebug}),
	}

	assert.True(t, tee.Enabled(t.Context(), slog.LevelInfo))
}

func TestTeeHandler_Handle(t *testing.T) {
	primary := newRecordingHandler()
	secondary := newRecordingHandler()
	tee := &teeHandler{primary: primary, secondary: secondary}

	err := tee.Handle(t.Context(), slog.Record{})
	assert.NoError(t, err)
	assert.Equal(t, 1, primary.recordCount(), "primary handler must receive the record")
	assert.Equal(t, 1, secondary.recordCount(), "secondary handler must receive the record")
}

func TestTeeHandler_WithGroup(t *testing.T) {
	primary := newRecordingHandler()
	secondary := newRecordingHandler()
	tee := &teeHandler{primary: primary, secondary: secondary}

	_ = tee.WithGroup("mygroup")
	assert.Equal(t, []string{"mygroup"}, primary.groups, "primary must receive WithGroup call")
	assert.Equal(t, []string{"mygroup"}, secondary.groups, "secondary must receive WithGroup call")
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"info", slog.LevelInfo},
		{"", slog.LevelInfo},
		{"unknown", slog.LevelInfo},
		{"INFO", slog.LevelInfo}, // case-sensitive — defaults to info
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseLogLevel(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// stubHandler records what it was asked to do and can fail on demand.
type stubHandler struct {
	enabled bool
	err     error
	handled int
}

func (s *stubHandler) Enabled(context.Context, slog.Level) bool { return s.enabled }
func (s *stubHandler) Handle(context.Context, slog.Record) error {
	s.handled++
	return s.err
}
func (s *stubHandler) WithAttrs([]slog.Attr) slog.Handler { return s }
func (s *stubHandler) WithGroup(string) slog.Handler      { return s }

// TestTeeHandler_ReportsErrorsAndRespectsEnabled pins two properties the tee handler did not have.
//
// It discarded both sub-handler errors and returned nil unconditionally, so a secondary handler that
// was failing every write — a broken Sentry transport, say — was invisible. golangci-lint flagged
// this as two errcheck findings; the errcheck was the symptom, the silent failure was the defect.
//
// It also called Handle on BOTH handlers whenever EITHER was enabled, because Enabled is an OR. slog
// guarantees Handle is only reached when Enabled returned true, but for a fan-out that guarantee is
// about the TEE, not its members: a secondary handler that declined a level still received the
// record. Each member must be gated on its own Enabled.
func TestTeeHandler_ReportsErrorsAndRespectsEnabled(t *testing.T) {
	rec := slog.Record{Level: slog.LevelInfo}

	t.Run("a failing sub-handler surfaces instead of being swallowed", func(t *testing.T) {
		boom := errors.New("secondary transport down")
		primary := &stubHandler{enabled: true}
		secondary := &stubHandler{enabled: true, err: boom}
		tee := &teeHandler{primary: primary, secondary: secondary}

		err := tee.Handle(context.Background(), rec)
		require.Error(t, err, "a sub-handler error must not be discarded — a broken secondary "+
			"transport was previously indistinguishable from success")
		assert.ErrorIs(t, err, boom)
		assert.Equal(t, 1, primary.handled, "the primary must still receive the record")
	})

	t.Run("both errors are reported, not just the first", func(t *testing.T) {
		e1, e2 := errors.New("primary down"), errors.New("secondary down")
		tee := &teeHandler{
			primary:   &stubHandler{enabled: true, err: e1},
			secondary: &stubHandler{enabled: true, err: e2},
		}
		err := tee.Handle(context.Background(), rec)
		require.Error(t, err)
		assert.ErrorIs(t, err, e1)
		assert.ErrorIs(t, err, e2, "returning only the first error hides the second")
	})

	t.Run("a disabled sub-handler is not called", func(t *testing.T) {
		primary := &stubHandler{enabled: true}
		secondary := &stubHandler{enabled: false}
		tee := &teeHandler{primary: primary, secondary: secondary}

		require.True(t, tee.Enabled(context.Background(), slog.LevelInfo))
		require.NoError(t, tee.Handle(context.Background(), rec))
		assert.Equal(t, 1, primary.handled)
		assert.Equal(t, 0, secondary.handled,
			"Enabled is an OR across members, so reaching Handle says nothing about whether THIS "+
				"member wanted the record; it must be gated individually")
	})
}
