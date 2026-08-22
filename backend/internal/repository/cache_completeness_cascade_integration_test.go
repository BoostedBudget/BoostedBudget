//go:build integration

package repository

// Issue #1537 — keeps deleteCascadeReachesCachedTables honest against the real schema.
//
// The unit-level derivation is hermetic by design (no database), so the cascade map is a pinned
// literal. A pinned literal rots: adding a foreign key, or changing an ON DELETE action from
// NO ACTION to CASCADE, silently widens what a DELETE writes. Because an under-reported write set
// looks DISJOINT, that rot converts straight into a false dispositionDerivedNoCacheImpact — the
// dangerous direction. This test recomputes the map from pg_constraint and fails on any drift.

import (
	"context"
	"os"
	"sort"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestDeleteCascadeMapMatchesLiveSchema(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err, "connect to test database")
	defer pool.Close()

	// Every FK whose ON DELETE action writes the child: 'c' CASCADE, 'n' SET NULL, 'd' SET DEFAULT.
	rows, err := pool.Query(ctx, `
		SELECT c.confrelid::regclass::text AS parent,
		       c.conrelid::regclass::text  AS child,
		       c.confdeltype
		FROM pg_constraint c
		WHERE c.contype = 'f' AND c.confdeltype IN ('c','n','d')`)
	require.NoError(t, err)
	defer rows.Close()

	type edge struct {
		child, action string
	}
	edges := map[string][]edge{}
	for rows.Next() {
		var parent, child, action string
		require.NoError(t, rows.Scan(&parent, &child, &action))
		edges[parent] = append(edges[parent], edge{child, action})
	}
	require.NoError(t, rows.Err())
	require.NotEmpty(t, edges, "no cascading foreign keys found — the schema is not migrated, and an "+
		"empty edge set would make this test pass while proving nothing")

	read := map[string]bool{}
	for _, r := range expectedCachedReadTables {
		read[r] = true
	}

	// Recurse through CASCADE only: SET NULL / SET DEFAULT update the child rather than deleting it,
	// so the child is written but its own children are untouched.
	var writtenBy func(parent string, seen map[string]bool) map[string]bool
	writtenBy = func(parent string, seen map[string]bool) map[string]bool {
		out := map[string]bool{}
		for _, e := range edges[parent] {
			out[e.child] = true
			if e.action == "c" && !seen[e.child] {
				seen[e.child] = true
				for k := range writtenBy(e.child, seen) {
					out[k] = true
				}
			}
		}
		return out
	}

	live := map[string][]string{}
	for parent := range edges {
		var hits []string
		for tbl := range writtenBy(parent, map[string]bool{parent: true}) {
			if read[tbl] {
				hits = append(hits, tbl)
			}
		}
		if len(hits) > 0 {
			sort.Strings(hits)
			live[parent] = hits
		}
	}

	// Compare against the pinned map.
	for parent, want := range deleteCascadeReachesCachedTables {
		got, ok := live[parent]
		if !ok {
			t.Errorf("pinned cascade map claims DELETE on %q reaches cached tables %v, but the live "+
				"schema has no such cascade. Stale entries make the derivation OVER-report, forcing "+
				"methods into human review unnecessarily — regenerate the map.", parent, want)
			continue
		}
		require.Equal(t, want, got, "cascade targets for DELETE on %q drifted", parent)
	}
	for parent, got := range live {
		if _, ok := deleteCascadeReachesCachedTables[parent]; !ok {
			t.Errorf("the live schema cascades DELETE on %q into cached-read tables %v, but the pinned "+
				"map does not list it. This is the DANGEROUS direction: any dispositionDerivedNoCacheImpact "+
				"entry that deletes from %q is currently under-derived and may be a false NoCacheImpact. "+
				"Regenerate deleteCascadeReachesCachedTables and re-check those entries.",
				parent, got, parent)
		}
	}
}
