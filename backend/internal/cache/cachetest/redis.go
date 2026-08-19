// Package cachetest provides test-only helpers for internal/cache's integration tests. It has NO
// build tag itself — it is a plain library package — so it compiles under a normal `go build
// ./...`; only the *_test.go files that import it carry `//go:build integration`.
package cachetest

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

// CacheRedisTestURLEnv is the environment variable an integration test reads to find a real Redis
// instance to test against. It is DELIBERATELY separate from any of this application's own
// CACHE_REDIS_URL / RATE_LIMIT_REDIS_URL variables — a test run must never accidentally point at a
// real deployment's Redis.
const CacheRedisTestURLEnv = "CACHE_REDIS_TEST_URL"

// ConnectTestRedis returns a redisclient.Client against CACHE_REDIS_TEST_URL, skipping the test
// (via t.Skip) when that variable is unset. It registers t.Cleanup to close the client.
func ConnectTestRedis(t *testing.T) redisclient.Client {
	t.Helper()

	url := os.Getenv(CacheRedisTestURLEnv)
	if url == "" {
		t.Skipf("%s not set; skipping integration test", CacheRedisTestURLEnv)
	}

	client, err := redisclient.New(redisclient.Options{URL: url, Name: "cache-test"})
	if err != nil {
		t.Fatalf("cachetest.ConnectTestRedis: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// NewIsolatedCache returns a ready-to-use, redis-backed cache.Cache connected to
// CACHE_REDIS_TEST_URL (skipping the test when unset, via ConnectTestRedis), plus a
// scope-kind/scope-ID pair unique to this test — t.Name() plus a random suffix — so concurrent
// test runs sharing one Redis instance never collide on keys. This is precisely why FLUSHDB is
// banned in this package's tests: isolation comes from unique key scoping, not from wiping the
// database.
func NewIsolatedCache(t *testing.T) (c cache.Cache, scopeKind cache.ScopeKind, scopeID string) {
	t.Helper()
	client := ConnectTestRedis(t)
	return cache.NewCache(cache.Options{Client: client}), cache.ScopeUser, uniqueScopeID(t)
}

// uniqueScopeID returns a scope ID scoped by the running test's name plus a random suffix.
func uniqueScopeID(t *testing.T) string {
	t.Helper()
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("cachetest.uniqueScopeID: %v", err)
	}
	return t.Name() + "-" + hex.EncodeToString(b[:])
}
