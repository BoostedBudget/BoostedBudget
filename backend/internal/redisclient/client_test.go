package redisclient

import (
	"errors"
	"strings"
	"testing"
	"time"

	redis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_RejectsUnparseableURL(t *testing.T) {
	_, err := New(Options{URL: "://not-a-valid-url", Name: "test"})
	require.Error(t, err)
}

func TestNew_ErrorNeverContainsTheURLPassword(t *testing.T) {
	// redis.ParseURL delegates to net/url, which for a URL with an invalid control character
	// returns a ParseError whose message echoes the raw input string verbatim. This is the
	// realistic case the doc comment warns about: an unparseable URL that still embeds
	// credentials, which is exactly when leaking them would matter most.
	const password = "sup3rsecret"
	badURL := "rediss://default:" + password + "@h:6379\x7f"

	_, err := New(Options{URL: badURL, Name: "test"})
	require.Error(t, err)

	msg := err.Error()
	assert.NotContains(t, msg, password)
	assert.NotContains(t, msg, badURL)
}

func TestNew_EmptyURLReturnsErrNotConfigured(t *testing.T) {
	c, err := New(Options{URL: "", Name: "test"})

	assert.Nil(t, c)
	assert.True(t, errors.Is(err, ErrNotConfigured))
}

func TestNew_AppliesTightDefaultTimeouts(t *testing.T) {
	c, err := New(Options{URL: "redis://localhost:6379", Name: "test"})
	require.NoError(t, err)
	defer c.Close()

	cl, ok := c.(*client)
	require.True(t, ok)

	assert.LessOrEqual(t, cl.opts.ReadTimeout, 500*time.Millisecond)
	assert.LessOrEqual(t, cl.opts.WriteTimeout, 500*time.Millisecond)
	assert.Equal(t, 0, cl.opts.MaxRetries)
}

func TestNew_EnablesContextTimeout(t *testing.T) {
	c, err := New(Options{URL: "redis://localhost:6379", Name: "test"})
	require.NoError(t, err)
	defer c.Close()

	cl, ok := c.(*client)
	require.True(t, ok)

	assert.True(t, cl.rdb.Options().ContextTimeoutEnabled, "go-redis will not honour context deadlines unless this is explicitly true")
}

func TestNew_ExplicitOptionsOverrideDefaults(t *testing.T) {
	c, err := New(Options{
		URL:          "redis://localhost:6379",
		Name:         "test",
		DialTimeout:  2 * time.Second,
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 75 * time.Millisecond,
		PoolTimeout:  100 * time.Millisecond,
		PoolSize:     42,
		MinIdleConns: 7,
		MaxRetries:   3,
	})
	require.NoError(t, err)
	defer c.Close()

	cl, ok := c.(*client)
	require.True(t, ok)

	assert.Equal(t, 2*time.Second, cl.opts.DialTimeout)
	assert.Equal(t, 50*time.Millisecond, cl.opts.ReadTimeout)
	assert.Equal(t, 75*time.Millisecond, cl.opts.WriteTimeout)
	assert.Equal(t, 100*time.Millisecond, cl.opts.PoolTimeout)
	assert.Equal(t, 42, cl.opts.PoolSize)
	assert.Equal(t, 7, cl.opts.MinIdleConns)
	assert.Equal(t, 3, cl.opts.MaxRetries)
	assert.Equal(t, 3, cl.rdb.Options().MaxRetries, "an explicit non-zero MaxRetries must reach go-redis untranslated")
}

func TestClient_NameReturnsConfiguredName(t *testing.T) {
	c, err := New(Options{URL: "redis://localhost:6379", Name: "cache"})
	require.NoError(t, err)
	defer c.Close()

	assert.Equal(t, "cache", c.Name())
}

func TestClient_RedactedAddrIsHostPortOnly(t *testing.T) {
	c, err := New(Options{URL: "redis://user:hunter2@localhost:6379/0", Name: "test"})
	require.NoError(t, err)
	defer c.Close()

	addr := c.RedactedAddr()

	assert.Equal(t, "localhost:6379", addr)
	assert.False(t, strings.Contains(addr, "user"))
	assert.False(t, strings.Contains(addr, "hunter2"))
}

func TestPoolStats_MapsEveryGoRedisField(t *testing.T) {
	src := &redis.PoolStats{
		Hits:       1,
		Misses:     2,
		Timeouts:   3,
		TotalConns: 4,
		IdleConns:  5,
		StaleConns: 6,
		// Fields go-redis tracks that this package deliberately does not surface. Included here
		// so a future field rename in go-redis (which would break this struct literal at compile
		// time) is caught immediately, rather than being silently ignored by mapPoolStats.
		WaitCount: 99,
		Unusable:  1,
	}

	got := mapPoolStats(src)

	assert.EqualValues(t, 1, got.Hits)
	assert.EqualValues(t, 2, got.Misses)
	assert.EqualValues(t, 3, got.Timeouts)
	assert.EqualValues(t, 4, got.TotalConns)
	assert.EqualValues(t, 5, got.IdleConns)
	assert.EqualValues(t, 6, got.StaleConns)
}
