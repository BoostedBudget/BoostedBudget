package posthog

import (
	"errors"
	"io"
	"log/slog"
	"testing"

	posthoggo "github.com/posthog/posthog-go"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeEnqueuer records the messages a Client enqueues.
type fakeEnqueuer struct {
	messages []posthoggo.Message
	err      error
}

func (f *fakeEnqueuer) Enqueue(message posthoggo.Message) error {
	f.messages = append(f.messages, message)
	return f.err
}

func (f *fakeEnqueuer) Close() error { return nil }

// TestEmitPersonPropertyUnset_EnqueuesSetWithUnset pins the wire shape: a $set capture carrying a
// top-level $unset array naming the person properties to remove.
func TestEmitPersonPropertyUnset_EnqueuesSetWithUnset(t *testing.T) {
	enqueuer := &fakeEnqueuer{}
	client := &Client{inner: enqueuer, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	client.EmitPersonPropertyUnset("user_clerk_123", []string{"name"})

	require.Len(t, enqueuer.messages, 1)
	capture, ok := enqueuer.messages[0].(posthoggo.Capture)
	require.True(t, ok)
	assert.Equal(t, "user_clerk_123", capture.DistinctId)
	assert.Equal(t, "$set", capture.Event)
	assert.Equal(t, map[string]interface{}{"$unset": []string{"name"}}, map[string]interface{}(capture.Properties))
}

// TestEmitPersonPropertyUnset_NilClient_NoOp pins the typed-nil guard: NewClient returns a nil
// *Client when no API key is configured, and it is still wired as a non-nil interface value.
func TestEmitPersonPropertyUnset_NilClient_NoOp(t *testing.T) {
	var client *Client
	assert.NotPanics(t, func() {
		client.EmitPersonPropertyUnset("user_clerk_123", []string{"name"})
	})
}

// TestEmitPersonPropertyUnset_EnqueueFailureRecordsMetric pins the failure signal: an enqueue
// failure increments the bounded enqueue counter rather than failing silently.
func TestEmitPersonPropertyUnset_EnqueueFailureRecordsMetric(t *testing.T) {
	before := testutil.ToFloat64(analyticsDeliveryFailuresTotal.WithLabelValues("enqueue"))
	client := &Client{
		inner:  &fakeEnqueuer{err: errors.New("transport down")},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	client.EmitPersonPropertyUnset("user_clerk_123", []string{"name"})

	after := testutil.ToFloat64(analyticsDeliveryFailuresTotal.WithLabelValues("enqueue"))
	assert.Equal(t, before+1, after)
}
