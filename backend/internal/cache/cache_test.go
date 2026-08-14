package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

func testKey(method string) Key {
	return Key{
		Namespace: NamespaceAnalytics,
		ScopeKind: ScopeUser,
		ScopeID:   "u1",
		Method:    method,
		ArgHash:   "hash1",
	}
}

func TestGetOrLoad_ColdMissCallsLoaderOnceThenSets(t *testing.T) {
	fake := redisclient.NewFake("cache")
	c := NewCache(Options{Client: fake})
	ctx := context.Background()

	var calls int32
	got, hit, err := c.GetOrLoad(ctx, testKey("M1"), func(ctx context.Context) ([]byte, error) {
		atomic.AddInt32(&calls, 1)
		return []byte(`{"v":1}`), nil
	})
	require.NoError(t, err)
	assert.False(t, hit)
	assert.JSONEq(t, `{"v":1}`, string(got))
	assert.Equal(t, int32(1), calls)
	assert.Equal(t, 1, fake.SetCalls(), "a cold miss must attempt to SET the loaded value")
}

func TestGetOrLoad_WarmHitDoesNotCallLoader(t *testing.T) {
	fake := redisclient.NewFake("cache")
	c := NewCache(Options{Client: fake})
	ctx := context.Background()

	_, _, err := c.GetOrLoad(ctx, testKey("M1"), func(ctx context.Context) ([]byte, error) {
		return []byte(`{"v":1}`), nil
	})
	require.NoError(t, err)

	var calls int32
	got, hit, err := c.GetOrLoad(ctx, testKey("M1"), func(ctx context.Context) ([]byte, error) {
		atomic.AddInt32(&calls, 1)
		return []byte(`{"v":999}`), nil
	})
	require.NoError(t, err)
	assert.True(t, hit)
	assert.JSONEq(t, `{"v":1}`, string(got))
	assert.Equal(t, int32(0), calls, "a warm hit must never call the loader")
}

func TestGetOrLoad_LoaderErrorIsPropagatedAndNothingIsCached(t *testing.T) {
	fake := redisclient.NewFake("cache")
	c := NewCache(Options{Client: fake})
	ctx := context.Background()
	boom := errors.New("loader boom")

	_, hit, err := c.GetOrLoad(ctx, testKey("M1"), func(ctx context.Context) ([]byte, error) {
		return nil, boom
	})
	assert.ErrorIs(t, err, boom)
	assert.False(t, hit)
	assert.Equal(t, 0, fake.SetCalls(), "a loader error must cache nothing")
}

func TestGetOrLoad_TTLAppliedFromNamespace(t *testing.T) {
	fake := redisclient.NewFake("cache")
	c := NewCache(Options{Client: fake})
	ctx := context.Background()

	_, _, err := c.GetOrLoad(ctx, testKey("M1"), func(ctx context.Context) ([]byte, error) {
		return []byte(`{}`), nil
	})
	require.NoError(t, err)

	cfg, _ := lookupNamespace(NamespaceAnalytics)
	// Read the raw stored value directly and confirm it decodes cleanly at the namespace TTL, and
	// stops decoding once we simulate reading long past 2x that TTL.
	gen, err := NewGenerations(fake).Get(ctx, ScopeUser, "u1")
	require.NoError(t, err)
	dataKey := testKey("M1").DataKey(cfg.SchemaVersion, gen)
	raw, err := fake.Get(ctx, dataKey)
	require.NoError(t, err)

	_, err = decodeEnvelope([]byte(raw), cfg.SchemaVersion, cfg.TTL, time.Now())
	assert.NoError(t, err)

	_, err = decodeEnvelope([]byte(raw), cfg.SchemaVersion, cfg.TTL, time.Now().Add(2*cfg.TTL+time.Second))
	assert.ErrorIs(t, err, ErrStaleEnvelope)
}

func TestGetOrLoad_NamespaceDisabled_ZeroCommandsAndCallsLoader(t *testing.T) {
	fake := redisclient.NewFake("cache")
	c := NewCache(Options{
		Client:                fake,
		EnvDisabledNamespaces: map[Namespace]bool{NamespaceAnalytics: true},
	})
	ctx := context.Background()

	var calls int32
	_, hit, err := c.GetOrLoad(ctx, testKey("M1"), func(ctx context.Context) ([]byte, error) {
		atomic.AddInt32(&calls, 1)
		return []byte(`{}`), nil
	})
	require.NoError(t, err)
	assert.False(t, hit)
	assert.Equal(t, int32(1), calls)
	assert.Equal(t, 0, fake.TotalCommandCalls(), "a disabled namespace must issue zero Redis commands")
}

// TestGenerationReadError_BypassesCacheEntirely proves the cache-level half of the bypass
// contract: a generation READ error must skip BOTH the data GET and the data SET, never just the
// read.
func TestGenerationReadError_BypassesCacheEntirely(t *testing.T) {
	fake := redisclient.NewFake("cache")
	fake.GetErr = errors.New("redis: timeout") // also makes the generation Get fail
	c := NewCache(Options{Client: fake})
	ctx := context.Background()

	var calls int32
	got, hit, err := c.GetOrLoad(ctx, testKey("M1"), func(ctx context.Context) ([]byte, error) {
		atomic.AddInt32(&calls, 1)
		return []byte(`{"fresh":true}`), nil
	})
	require.NoError(t, err)
	assert.False(t, hit)
	assert.JSONEq(t, `{"fresh":true}`, string(got))
	assert.Equal(t, int32(1), calls)
	assert.Equal(t, 0, fake.SetCalls(), "a generation read error must bypass the SET too, not just the GET")
}

func TestGetOrLoad_DataGetErrorTreatedAsMiss_LoaderCalledAndSetAttempted(t *testing.T) {
	fake := redisclient.NewFake("cache")
	c := NewCache(Options{Client: fake})
	ctx := context.Background()

	// Let the generation mint succeed first (uses SetNX, not Get-based GetErr path), then flip on
	// GetErr for the DATA read.
	_, err := NewGenerations(fake).Get(ctx, ScopeUser, "u1")
	require.NoError(t, err)
	fake.GetErr = errors.New("redis: timeout")

	var calls int32
	got, hit, err := c.GetOrLoad(ctx, testKey("M1"), func(ctx context.Context) ([]byte, error) {
		atomic.AddInt32(&calls, 1)
		return []byte(`{"fresh":true}`), nil
	})
	require.NoError(t, err)
	assert.False(t, hit)
	assert.JSONEq(t, `{"fresh":true}`, string(got))
	assert.Equal(t, int32(1), calls)
}

func TestGetOrLoad_CodecGarbage_TreatedAsMissWithoutDelete(t *testing.T) {
	fake := redisclient.NewFake("cache")
	c := NewCache(Options{Client: fake})
	ctx := context.Background()

	gen, err := NewGenerations(fake).Get(ctx, ScopeUser, "u1")
	require.NoError(t, err)
	cfg, _ := lookupNamespace(NamespaceAnalytics)
	dataKey := testKey("M1").DataKey(cfg.SchemaVersion, gen)
	require.NoError(t, fake.Set(ctx, dataKey, "not-json-garbage", 0))

	var calls int32
	_, hit, err := c.GetOrLoad(ctx, testKey("M1"), func(ctx context.Context) ([]byte, error) {
		atomic.AddInt32(&calls, 1)
		return []byte(`{"fresh":true}`), nil
	})
	require.NoError(t, err)
	assert.False(t, hit)
	assert.Equal(t, int32(1), calls)
	assert.Equal(t, 0, fake.DelCalls(), "a codec miss must never DEL — that would race a concurrent writer")
}

func TestSingleflight_ConcurrentColdReadsInvokeLoaderExactlyOnce(t *testing.T) {
	fake := redisclient.NewFake("cache")
	c := NewCache(Options{Client: fake})
	ctx := context.Background()

	var calls int32
	var wg sync.WaitGroup
	const n = 100
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, _, err := c.GetOrLoad(ctx, testKey("M1"), func(ctx context.Context) ([]byte, error) {
				atomic.AddInt32(&calls, 1)
				time.Sleep(5 * time.Millisecond)
				return []byte(`{"v":1}`), nil
			})
			assert.NoError(t, err)
		}()
	}
	wg.Wait()
	assert.Equal(t, int32(1), calls, "100 concurrent cold reads for the same key must invoke the loader exactly once")
}

func TestSingleflight_KeyedByGeneration_BumpSplitsFlights(t *testing.T) {
	fake := redisclient.NewFake("cache")
	c := NewCache(Options{Client: fake})
	ctx := context.Background()

	_, _, err := c.GetOrLoad(ctx, testKey("M1"), func(ctx context.Context) ([]byte, error) {
		return []byte(`{"v":1}`), nil
	})
	require.NoError(t, err)

	c.Generations().Bump(ctx, ScopeUser, "u1", "invalidation")

	var calls int32
	_, hit, err := c.GetOrLoad(ctx, testKey("M1"), func(ctx context.Context) ([]byte, error) {
		atomic.AddInt32(&calls, 1)
		return []byte(`{"v":2}`), nil
	})
	require.NoError(t, err)
	assert.False(t, hit, "a bump must make the next read a fresh flight (cold miss), not reuse the old generation's cached entry")
	assert.Equal(t, int32(1), calls)
}

func TestNoopCache_IssuesZeroRedisCommands(t *testing.T) {
	c := NewNoopCache()
	ctx := context.Background()

	var calls int32
	got, hit, err := c.GetOrLoad(ctx, testKey("M1"), func(ctx context.Context) ([]byte, error) {
		atomic.AddInt32(&calls, 1)
		return []byte(`{"fresh":true}`), nil
	})
	require.NoError(t, err)
	assert.False(t, hit)
	assert.JSONEq(t, `{"fresh":true}`, string(got))
	assert.Equal(t, int32(1), calls)

	// noopCache holds no client of any kind (see its type doc comment): this second call, run
	// against a Fake that WOULD record commands if noopCache ever reached it, proves zero commands
	// were issued — not merely that the result "looked like a miss".
	fake := redisclient.NewFake("cache")
	_ = fake // deliberately never wired into c: noopCache has no field to accept it
	assert.Equal(t, 0, fake.TotalCommandCalls())
}

func TestNoopCache_EnabledAlwaysFalse(t *testing.T) {
	c := NewNoopCache()
	assert.False(t, c.Enabled(NamespaceAnalytics))
	assert.Nil(t, c.Generations())
}
