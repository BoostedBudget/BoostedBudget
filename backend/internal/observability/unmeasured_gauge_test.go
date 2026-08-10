package observability

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// UnmeasuredGauge exists for one property, and this is it: BEFORE anything has measured, the metric
// is ABSENT from the scrape — not present carrying a confident 0.
//
// The distinction is the whole point. A registered plain Gauge exports 0 from process start, so
// "nobody has queried the database yet" and "measured, and it is zero" are the same scrape, and no
// PromQL can separate them. Every gauge in this file fed by a periodic collector spent its startup
// window making the second claim while only the first was true; for DeletionResidueRecords, whose
// rule is `increase(...) > 0`, the eventual 0 → N step then read as N new incidents and a restart
// paged somebody about a GDPR failure that never happened.
func TestUnmeasuredGauge_IsAbsentUntilSomethingHasMeasured(t *testing.T) {
	g := UnmeasuredGauge(prometheus.GaugeOpts{
		Name: "unmeasured_gauge_test_metric",
		Help: "test",
	})
	t.Cleanup(func() { g.DeleteLabelValues() })

	assert.Equal(t, 0, testutil.CollectAndCount(g),
		"an unmeasured gauge must collect NOTHING, so absent() can distinguish 'not looked' from 'looked, and it is zero'")

	// ⚠ THE MANDATORY SIBLING: a measured ZERO must still publish. A design that achieved "never a
	// startup zero" by never publishing zero at all would be worse than the defect — the alert could
	// fire but never clear.
	g.WithLabelValues().Set(0)
	require.Equal(t, 1, testutil.CollectAndCount(g),
		"a measured zero is a real reading and must appear")
	assert.Equal(t, float64(0), testutil.ToFloat64(g))

	g.WithLabelValues().Set(42)
	assert.Equal(t, float64(42), testutil.ToFloat64(g))
}

// And the published sample must be shaped EXACTLY like a plain Gauge's — one series, same metric
// name, NO labels. If UnmeasuredGauge introduced a label, every existing dashboard query and every
// recording rule against these metric names would silently stop matching, which would be a far
// larger regression than the startup zero it fixes.
func TestUnmeasuredGauge_PublishesAnUnlabelledSeriesUnderItsOwnName(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "unmeasured_gauge_shape_test", Help: "test"}, nil)
	require.NoError(t, reg.Register(g))

	g.WithLabelValues().Set(5)

	families, err := reg.Gather()
	require.NoError(t, err)
	require.Len(t, families, 1)
	assert.Equal(t, "unmeasured_gauge_shape_test", families[0].GetName())
	require.Len(t, families[0].GetMetric(), 1)
	assert.Empty(t, families[0].GetMetric()[0].GetLabel(),
		"the published series must carry no labels — anything else changes the scrape shape these metric names already have")
	assert.Equal(t, float64(5), families[0].GetMetric()[0].GetGauge().GetValue())
}
