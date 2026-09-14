package observability

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestRecordPostHogErasureAttemptBoundsLabels(t *testing.T) {
	before := testutil.ToFloat64(PostHogErasureAttempts.WithLabelValues("unknown", "failure", "unknown"))
	RecordPostHogErasureAttempt("identity-containing-phase", "identity-containing-outcome", "raw provider body")
	after := testutil.ToFloat64(PostHogErasureAttempts.WithLabelValues("unknown", "failure", "unknown"))
	assert.Equal(t, before+1, after)
}

func TestRecordPostHogErasureAttemptRecognizesBaselineCheckpointPhase(t *testing.T) {
	before := testutil.ToFloat64(PostHogErasureAttempts.WithLabelValues("person_baseline", "accepted", "none"))
	RecordPostHogErasureAttempt("person_baseline", "accepted", "none")
	after := testutil.ToFloat64(PostHogErasureAttempts.WithLabelValues("person_baseline", "accepted", "none"))
	assert.Equal(t, before+1, after)
}

func TestRecordPostHogErasureAttemptRecognizesClerkFencePhase(t *testing.T) {
	before := testutil.ToFloat64(PostHogErasureAttempts.WithLabelValues("clerk_delete", "accepted", "none"))
	RecordPostHogErasureAttempt("clerk_delete", "accepted", "none")
	after := testutil.ToFloat64(PostHogErasureAttempts.WithLabelValues("clerk_delete", "accepted", "none"))
	assert.Equal(t, before+1, after)
}

func TestPublishPostHogErasureBacklogSetsEveryStateAndAge(t *testing.T) {
	PublishPostHogErasureBacklog(map[string]int64{"pending": 2, "manual_pending": 1}, 75)
	assert.Equal(t, float64(2), testutil.ToFloat64(PostHogErasureRows.WithLabelValues("pending")))
	assert.Equal(t, float64(0), testutil.ToFloat64(PostHogErasureRows.WithLabelValues("retrying")))
	assert.Equal(t, float64(1), testutil.ToFloat64(PostHogErasureRows.WithLabelValues("manual_pending")))
	assert.Equal(t, float64(75), testutil.ToFloat64(PostHogErasureOldestOutstandingSeconds))
}
