package database

// Unit tests for NewPool's connection configuration. These deliberately avoid a live database —
// buildPoolConfig only parses the DSN and decorates the resulting *pgxpool.Config, so it can be
// asserted against without a running Postgres instance.
//
// Issue #1461: retention_expires_at (a GDPR deletion deadline) is computed in Postgres via
// clock_timestamp() + make_interval(months => N), which performs the calendar addition in the
// SESSION time zone. NewPool never pinned that session parameter, so the same code could compute
// a different absolute deadline depending on the host's local Postgres configuration (confirmed
// locally: this machine's Postgres session TimeZone defaults to America/New_York, not UTC).
// Pinning every pooled connection's TimeZone to UTC at connection-startup time (via
// ConnConfig.RuntimeParams, the same startup-packet mechanism used for search_path/application_name)
// makes the arithmetic deterministic regardless of host or deployment environment.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPoolConfig_PinsSessionTimeZoneToUTC(t *testing.T) {
	poolConfig, err := buildPoolConfig(DefaultConfig("postgres://user:pass@localhost:5432/db?sslmode=disable"))
	require.NoError(t, err)
	require.NotNil(t, poolConfig.ConnConfig.RuntimeParams, "RuntimeParams must be initialized")
	assert.Equal(t, "UTC", poolConfig.ConnConfig.RuntimeParams["TimeZone"],
		"every pooled connection must start its session with TimeZone=UTC, so calendar-interval "+
			"arithmetic (make_interval months/years) is deterministic regardless of the host's own "+
			"Postgres TimeZone default")
}

func TestBuildPoolConfig_PreservesExistingRuntimeParams(t *testing.T) {
	// application_name (or any other DSN-supplied runtime parameter) must survive the TimeZone
	// pin being added — the pin must merge into RuntimeParams, not replace the map wholesale.
	poolConfig, err := buildPoolConfig(DefaultConfig(
		"postgres://user:pass@localhost:5432/db?sslmode=disable&application_name=boostedbudget-test",
	))
	require.NoError(t, err)
	assert.Equal(t, "boostedbudget-test", poolConfig.ConnConfig.RuntimeParams["application_name"])
	assert.Equal(t, "UTC", poolConfig.ConnConfig.RuntimeParams["TimeZone"])
}

func TestBuildPoolConfig_RejectsEmptyDatabaseURL(t *testing.T) {
	_, err := buildPoolConfig(DefaultConfig(""))
	require.Error(t, err)
}
