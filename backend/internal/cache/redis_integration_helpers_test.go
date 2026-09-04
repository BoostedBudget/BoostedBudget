//go:build integration

package cache_test

import (
	"context"
	"os"
	"testing"
	"time"

	redis "github.com/redis/go-redis/v9"

	"github.com/maxnussbaum/BoostedBudget/internal/cache/cachetest"
)

// pttl reads a key's remaining TTL directly via go-redis (bypassing redisclient.Client, which
// deliberately does not expose PTTL — Task 9's Client interface only needed GET/SET/SETNX/MGET/
// DEL). This is a read-only inspection query used ONLY by hygiene tests
// (TestGenerationKeys_HaveNoTTL_DataKeysDo); it never mutates state and is never FLUSHDB.
func pttl(ctx context.Context, t *testing.T, key string) (time.Duration, error) {
	t.Helper()
	opts, err := redis.ParseURL(os.Getenv(cachetest.CacheRedisTestURLEnv))
	if err != nil {
		t.Fatalf("pttl: parse test URL: %v", err)
	}
	rdb := redis.NewClient(opts)
	defer func() { _ = rdb.Close() }()
	return rdb.PTTL(ctx, key).Result()
}

// findExactlyOneKey resolves pattern (a Redis KEYS glob) against the real test instance and
// t.Fatals unless it matches exactly one key — never zero (which would make a caller's assertion
// pass vacuously on a key that was never written), and never more than one (which would mean
// pattern was too loose to identify a single data key). It exists so
// TestGenerationKeys_HaveNoTTL_DataKeysDo can discover the ACTUAL key GetOrLoad wrote rather than
// reconstructing one by hand with a schema version that can (and did) drift out of sync with
// namespace.go's registry — see that test's comment for the full incident.
func findExactlyOneKey(ctx context.Context, t *testing.T, pattern string) string {
	t.Helper()
	opts, err := redis.ParseURL(os.Getenv(cachetest.CacheRedisTestURLEnv))
	if err != nil {
		t.Fatalf("findExactlyOneKey: parse test URL: %v", err)
	}
	rdb := redis.NewClient(opts)
	defer func() { _ = rdb.Close() }()

	keys, err := rdb.Keys(ctx, pattern).Result()
	if err != nil {
		t.Fatalf("findExactlyOneKey: KEYS %q: %v", pattern, err)
	}
	if len(keys) != 1 {
		t.Fatalf("findExactlyOneKey: pattern %q matched %d keys (want exactly 1): %v", pattern, len(keys), keys)
	}
	return keys[0]
}
