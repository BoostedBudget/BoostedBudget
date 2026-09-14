//go:build integration

package pgtime

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestStored_Integration_MatchesPGXTimestampRoundTrip(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping PostgreSQL timestamp round-trip test")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close(ctx)) })

	for _, input := range []time.Time{
		time.Date(2026, 9, 13, 23, 52, 41, 538427527, time.UTC),
		time.Date(2026, 9, 13, 19, 52, 41, 999999999, time.FixedZone("test-offset", -4*60*60)),
		time.Date(1969, 12, 31, 23, 59, 59, 999999999, time.UTC),
	} {
		var stored, shifted time.Time
		require.NoError(t, conn.QueryRow(ctx,
			"SELECT $1::timestamptz, $1::timestamptz + interval '1 microsecond'", input,
		).Scan(&stored, &shifted))
		require.Equal(t, Stored(input), stored.UTC(), "Go fixture must match the exact pgx/PostgreSQL round trip")
		require.NotEqual(t, Stored(input), shifted.UTC(), "a one-microsecond database change must still fail identity")
	}
}
