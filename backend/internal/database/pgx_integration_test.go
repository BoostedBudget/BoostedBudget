//go:build integration

package database

// Integration proof for NewPool's TimeZone pin (Issue #1461). pgx_test.go's unit tests assert the
// *pgxpool.Config carries RuntimeParams["TimeZone"] = "UTC" without touching the network; this
// test goes one level further and proves a REAL connection pulled from a REAL pool actually reports
// session TimeZone = UTC to Postgres itself — the RuntimeParams entry is inert configuration until
// a live connection's startup packet actually carries it.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/database/... -run TestNewPool_Integration_PinsSessionTimeZone -tags integration -v

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewPool_Integration_PinsSessionTimeZoneToUTC(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	ctx := context.Background()
	pool, err := NewPool(ctx, DefaultConfig(dsn))
	require.NoError(t, err)
	defer pool.Close()

	var tz string
	require.NoError(t, pool.QueryRow(ctx, "SHOW TimeZone").Scan(&tz))
	require.Equal(t, "UTC", tz,
		"a pool built by NewPool must report session TimeZone=UTC — if this fails, the host's own "+
			"Postgres TimeZone default (e.g. America/New_York, confirmed present on this machine) is "+
			"leaking through uncontrolled, exactly the Issue #1461 hazard")
}
