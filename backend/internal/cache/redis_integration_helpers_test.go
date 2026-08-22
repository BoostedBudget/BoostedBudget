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
