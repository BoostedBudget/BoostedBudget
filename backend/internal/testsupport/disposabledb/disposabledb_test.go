package disposabledb

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestParseURLAllowsOnlyKnownDisposableLoopbackDatabases(t *testing.T) {
	for _, raw := range []string{
		"postgres://devuser:devpass@localhost:5432/freshcheck?sslmode=disable",
		"postgres://test:test@127.0.0.1:49152/test?sslmode=disable",
		"postgresql://test:test@[::1]:5432/test?sslmode=disable&connect_timeout=5",
	} {
		_, err := ParseURL(raw)
		require.NoError(t, err)
	}
	for _, raw := range []string{
		"postgres://devuser:devpass@localhost/boostedbudget?sslmode=disable",
		"postgres://test:test@localhost/freshcheck",
		"postgres://devuser:devpass@localhost/test",
		"postgres://postgres:postgres@localhost/test",
		"postgres://test:test@production.example/test",
		"postgres://test:test@10.0.0.1/test",
		"postgres://test:test@127.0.0.1,production.example/test",
		"postgres://test:test@localhost/test?host=production.example",
		"postgres://test:test@localhost/test?host=127.0.0.1",
		"postgres://test:test@localhost/test?hostaddr=203.0.113.1",
		"postgres://test:test@localhost/test?%68ost=production.example",
		"postgres://test:test@localhost/test?service=production",
		"postgres://test:test@localhost/test?options=-c%20host%3Dproduction.example",
		"postgres://test:test@localhost/test?port=5433",
		"postgres://test:test@localhost/test?dbname=boostedbudget",
		"postgres://test:test@localhost/test?user=devuser",
		"postgres://test:test@/test",
		"host=localhost user=test dbname=test",
		"postgres://test:test@localhost/test#ignored",
		"",
	} {
		_, err := ParseURL(raw)
		require.Error(t, err, "unsafe or ambiguous test database URL must be refused")
	}
}

func TestParsedConnectionCannotFallbackToRemoteDatabase(t *testing.T) {
	cfg, err := ParseURL("postgres://test:test@localhost/test?sslmode=disable")
	require.NoError(t, err)
	cfg.ConnConfig.Fallbacks = []*pgconn.FallbackConfig{{Host: "production.example", Port: 5432}}
	require.Error(t, validateConnection(cfg.ConnConfig))
	cfg.ConnConfig.Fallbacks = []*pgconn.FallbackConfig{{Host: "127.0.0.1", Port: 5432}}
	require.NoError(t, validateConnection(cfg.ConnConfig))
}
