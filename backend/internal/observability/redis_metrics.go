package observability

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// RedisPoolStats mirrors go-redis's PoolStats so this package never imports a Redis client.
// internal/redisclient (a later task) will want this package for its own command-error counter, so
// this package must NOT import internal/redisclient — see system_health_service.go's PoolStats for
// the same mirroring precedent against pgxpool.
type RedisPoolStats struct {
	Hits, Misses, Timeouts            uint32 // CUMULATIVE since client construction
	TotalConns, IdleConns, StaleConns uint32 // instantaneous
	PoolSize                          int    // configured
}

// RedisPoolStatsSource is implemented by anything that can report its own local connection pool
// state. Deliberately just these two methods: RedisPoolStats() reads local pool state ONLY and
// dispatches no Redis command (this is a direct #1531 acceptance criterion — see
// TestStartRedisMetricsCollector_IssuesZeroRedisCommands), and the collector holds only this
// interface, never a concrete client, so it structurally cannot reach anything else.
type RedisPoolStatsSource interface {
	Name() string // "limiter" | "cache"
	RedisPoolStats() RedisPoolStats
}

// Pool metrics, labelled by client ("limiter" | "cache").
//
// ⚠ Ordinary promauto.NewGaugeVec, NOT UnmeasuredGauge (metrics.go:12-35), and deliberately so.
// UnmeasuredGauge exists to give an UNLABELLED gauge the "absent until measured" property that a
// labelled Vec already has for free: a GaugeVec/CounterVec collects nothing for a label combination
// until WithLabelValues(...) creates that child, so "client" never observed is simply absent from
// the scrape — wrapping it in UnmeasuredGauge would add nothing and would be removed by the next
// person who reads its doc comment and doesn't understand why it's there. Do not "fix" this.
var (
	redisPoolTotalConns = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "redis_pool_total_conns",
		Help: "Total number of connections (idle + in-use) in a Redis client's pool, by client.",
	}, []string{"client"})
	redisPoolIdleConns = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "redis_pool_idle_conns",
		Help: "Number of idle connections in a Redis client's pool, by client.",
	}, []string{"client"})
	redisPoolStaleConns = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "redis_pool_stale_conns",
		Help: "Number of stale connections removed from a Redis client's pool, by client.",
	}, []string{"client"})
	redisPoolSizeMax = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "redis_pool_size_max",
		Help: "Configured maximum pool size for a Redis client, by client.",
	}, []string{"client"})

	// Hits/Misses/Timeouts are CUMULATIVE counters reported by go-redis, so they are exported as
	// real Prometheus Counters (rate()-safe), not Gauges — see StartRedisMetricsCollector's delta
	// conversion below for why a naive Set()/Add() of the raw value would be wrong.
	redisPoolHitsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "redis_pool_hits_total",
		Help: "Cumulative pool hits for a Redis client, by client (converted from go-redis's raw cumulative counter into true deltas).",
	}, []string{"client"})
	redisPoolMissesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "redis_pool_misses_total",
		Help: "Cumulative pool misses for a Redis client, by client (converted from go-redis's raw cumulative counter into true deltas).",
	}, []string{"client"})
	redisPoolTimeoutsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "redis_pool_timeouts_total",
		Help: "Cumulative pool wait timeouts for a Redis client, by client (converted from go-redis's raw cumulative counter into true deltas).",
	}, []string{"client"})
)

// RedisMetricsCollector periodically reads RedisPoolStats() from each configured client and
// publishes it as Prometheus metrics. The collection interval is an unexported field (default 15s,
// set by NewRedisMetricsCollector) so tests can inject a short interval instead of sleeping 15s.
type RedisMetricsCollector struct {
	interval time.Duration
}

// NewRedisMetricsCollector returns a collector using the production 15-second interval.
func NewRedisMetricsCollector() *RedisMetricsCollector {
	return &RedisMetricsCollector{interval: 15 * time.Second}
}

// StartRedisMetricsCollector starts a goroutine that periodically updates Redis pool metrics from
// each client's RedisPoolStats(). Call from main() once Redis clients are constructed (Task 7). The
// goroutine stops when ctx is cancelled.
func StartRedisMetricsCollector(ctx context.Context, clients ...RedisPoolStatsSource) {
	NewRedisMetricsCollector().Start(ctx, clients...)
}

// Start begins the collection loop for the given clients. Unlike db_metrics.go's
// StartDBMetricsCollector (which waits for the first tick), this collects ONCE immediately, before
// entering the ticker loop — otherwise every deploy shows an absent-data window as long as the
// interval, which under absent()-based alerting looks identical to an outage.
func (c *RedisMetricsCollector) Start(ctx context.Context, clients ...RedisPoolStatsSource) {
	prev := make(map[string]RedisPoolStats, len(clients))

	collect := func() {
		for _, client := range clients {
			name := client.Name()
			stats := client.RedisPoolStats()

			redisPoolTotalConns.WithLabelValues(name).Set(float64(stats.TotalConns))
			redisPoolIdleConns.WithLabelValues(name).Set(float64(stats.IdleConns))
			redisPoolStaleConns.WithLabelValues(name).Set(float64(stats.StaleConns))
			redisPoolSizeMax.WithLabelValues(name).Set(float64(stats.PoolSize))

			last := prev[name]
			redisPoolHitsTotal.WithLabelValues(name).Add(counterDelta(last.Hits, stats.Hits))
			redisPoolMissesTotal.WithLabelValues(name).Add(counterDelta(last.Misses, stats.Misses))
			redisPoolTimeoutsTotal.WithLabelValues(name).Add(counterDelta(last.Timeouts, stats.Timeouts))
			prev[name] = stats
		}
	}

	go func() {
		collect() // immediate pre-tick read; see doc comment above

		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				collect()
			}
		}
	}()
}

// counterDelta converts a raw cumulative go-redis counter into the amount to Add() to a Prometheus
// Counter. If curr has moved backwards relative to prev — the client was reconstructed and its
// counter reset to (near) zero — treat curr itself as the delta since the reset, rather than
// computing a negative delta: Counter.Add() panics on a negative argument.
func counterDelta(prev, curr uint32) float64 {
	if curr >= prev {
		return float64(curr - prev)
	}
	return float64(curr)
}
