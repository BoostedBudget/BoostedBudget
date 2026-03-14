package observability

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	dbPoolAcquired = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "db_pool_acquired_connections",
		Help: "Current number of acquired (in-use) database connections.",
	})
	dbPoolIdle = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "db_pool_idle_connections",
		Help: "Current number of idle database connections.",
	})
	dbPoolMax = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "db_pool_max_connections",
		Help: "Maximum number of database connections allowed.",
	})
	dbPoolTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "db_pool_total_connections",
		Help: "Total number of database connections (acquired + idle + constructing).",
	})
)

// StartDBMetricsCollector starts a goroutine that periodically updates
// database pool metrics from pgxpool.Stat(). Call from main() when pool is available.
// The goroutine stops when ctx is cancelled.
func StartDBMetricsCollector(ctx context.Context, pool *pgxpool.Pool) {
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				stat := pool.Stat()
				dbPoolAcquired.Set(float64(stat.AcquiredConns()))
				dbPoolIdle.Set(float64(stat.IdleConns()))
				dbPoolMax.Set(float64(stat.MaxConns()))
				dbPoolTotal.Set(float64(stat.TotalConns()))
			}
		}
	}()
}
