package observability

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

// TestRecordJobSuccess_SetsGaugeToNow proves the generic heartbeat helper records the current
// Unix timestamp for the given job kind. This is the mechanism a staleness alert
// ("override-expiry has not succeeded in > 48h") reads from: time() - job_last_success_timestamp
// _seconds{kind="..."}.
func TestRecordJobSuccess_SetsGaugeToNow(t *testing.T) {
	const kind = "test_job_heartbeat_success"

	before := time.Now().Unix()
	RecordJobSuccess(kind)
	after := time.Now().Unix()

	got := testutil.ToFloat64(JobLastSuccessTimestamp.WithLabelValues(kind))
	assert.GreaterOrEqual(t, got, float64(before))
	assert.LessOrEqual(t, got, float64(after))
}

// TestRecordJobSuccess_LabelsAreIndependent proves each job kind gets its own label value —
// recording a heartbeat for one kind must never advance another kind's gauge. Without this, a
// healthy job's heartbeat could mask a different job's silent staleness.
func TestRecordJobSuccess_LabelsAreIndependent(t *testing.T) {
	const kindA = "test_job_heartbeat_a"
	const kindB = "test_job_heartbeat_b"

	// Seed kindB with an old, deterministic value; kindA must never touch it.
	JobLastSuccessTimestamp.WithLabelValues(kindB).Set(1)

	RecordJobSuccess(kindA)

	assert.Equal(t, float64(1), testutil.ToFloat64(JobLastSuccessTimestamp.WithLabelValues(kindB)),
		"recording a heartbeat for kindA must not affect kindB's gauge value")
	assert.NotEqual(t, float64(1), testutil.ToFloat64(JobLastSuccessTimestamp.WithLabelValues(kindA)))
}

// TestRecordJobSuccess_NotAdvancedOnFailure_CallerContract documents the contract every periodic
// worker's deferred metrics func relies on: RecordJobSuccess must ONLY be called on the success
// path. This test proves the negative directly at the helper level — if the caller simply never
// invokes it (as on a failed run), the gauge for that kind stays at its previous value (zero, if
// never recorded) rather than advancing on its own.
func TestRecordJobSuccess_NotAdvancedOnFailure_CallerContract(t *testing.T) {
	const kind = "test_job_heartbeat_never_called"

	// Never call RecordJobSuccess for this kind — simulates a job that has never succeeded
	// (or a run whose caller correctly skipped the call because err != nil).
	got := testutil.ToFloat64(JobLastSuccessTimestamp.WithLabelValues(kind))
	assert.Equal(t, float64(0), got, "a kind whose success path was never reached must show a zero-value gauge, not silently accrue a value")
}
