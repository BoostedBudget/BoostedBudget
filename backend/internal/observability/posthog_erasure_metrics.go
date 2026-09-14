package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	PostHogErasureAttempts = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "posthog_erasure_attempts_total",
		Help: "PostHog erasure attempts by bounded phase, outcome, and error class; never labeled by identity or provider body.",
	}, []string{"phase", "outcome", "error_class"})
	PostHogErasureRows = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "posthog_erasure_rows",
		Help: "Current durable PostHog erasure outbox rows by state. Alert on manual_pending or dead_letter and on sustained pending/retrying.",
	}, []string{"state"})
	PostHogErasureOldestOutstandingSeconds = UnmeasuredGauge(prometheus.GaugeOpts{
		Name: "posthog_erasure_oldest_outstanding_seconds",
		Help: "Age of the oldest non-completed PostHog erasure obligation; alert on sustained growth or absence of this metric.",
	})
)

var postHogErasureStates = []string{"pending", "retrying", "manual_pending", "dead_letter", "completed"}

func RecordPostHogErasureAttempt(phase, outcome, errorClass string) {
	if phase != "person_resolve" && phase != "person_baseline" && phase != "person_delete" && phase != "person_poll" && phase != "support_manual" {
		phase = "unknown"
	}
	if outcome != "accepted" && outcome != "retry" && outcome != "dead_letter" {
		outcome = "failure"
	}
	if errorClass != "none" && errorClass != "configuration" && errorClass != "rate_limited" && errorClass != "provider_unavailable" && errorClass != "auth" && errorClass != "invalid_response" && errorClass != "transport" {
		errorClass = "unknown"
	}
	PostHogErasureAttempts.WithLabelValues(phase, outcome, errorClass).Inc()
}

func PublishPostHogErasureBacklog(counts map[string]int64, oldestSeconds float64) {
	for _, state := range postHogErasureStates {
		PostHogErasureRows.WithLabelValues(state).Set(float64(counts[state]))
	}
	PostHogErasureOldestOutstandingSeconds.WithLabelValues().Set(max(oldestSeconds, 0))
}
