package observability

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// JobLastSuccessTimestamp records the Unix timestamp of each periodic job's most recent
// successful completion, labeled by kind (matches jobs_completed_total/job_duration_seconds'
// "kind" label — the job's river.Job.Kind / *Args.Kind() value).
//
// Why this exists alongside jobs_completed_total{status="failed"} (Issue #1413 P4): that counter
// tracks individual failed ATTEMPTS, but a job that stops running entirely — crashes before
// Work() is ever invoked, is dropped from the periodic schedule, or gets stuck retrying into the
// dead-letter queue with no automatic replay — also stops incrementing jobs_completed_total in
// EITHER status. Nothing about that counter distinguishes "quiet because healthy, nothing to do"
// from "quiet because it has not run in three weeks". This gauge closes that gap: alert on
// `time() - job_last_success_timestamp_seconds{kind="..."} > threshold` (e.g. 48h) to catch
// silent staleness a failure counter alone cannot see.
//
// manual_tier_override_expiry_check is the motivating case: Issue #1413 Critical finding C1
// made the retention-notice/hard-delete queries exclude a user while ANY manual_tier_override
// survives, live or lapsed — clearing a lapsed one is this job's job alone. If it silently stops,
// affected users are never warned and never deleted, with nothing else to notice. This gauge
// (paired with the oldest-uncleared-expired-override gauge in the repository package) is what
// makes that silence observable.
var JobLastSuccessTimestamp = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "job_last_success_timestamp_seconds",
	Help: "Unix timestamp of each periodic job's most recent successful completion, labeled by kind.",
}, []string{"kind"})

// RecordJobSuccess records "now" as kind's last successful completion. This is the one generic
// helper every periodic worker's existing deferred metrics func calls — ONLY on the success path
// (err == nil in Work()) — rather than each worker hand-rolling its own timestamp gauge. Never
// call this on a failed run: doing so would defeat the entire point of the metric.
func RecordJobSuccess(kind string) {
	JobLastSuccessTimestamp.WithLabelValues(kind).Set(float64(time.Now().Unix()))
}
