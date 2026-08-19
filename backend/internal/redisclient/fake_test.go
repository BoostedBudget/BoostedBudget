package redisclient

import (
	"context"
	"errors"
	"testing"

	redis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFake_SatisfiesClientAndRecordsInvocations is a smoke test for the shared test double: Tasks
// 4/5/9 depend on it behaving like a real Client for their own unit tests, so its basic recording
// behavior is verified here rather than only implicitly, the first time a later task uses it.
func TestFake_SatisfiesClientAndRecordsInvocations(t *testing.T) {
	f := NewFake("cache")

	assert.Equal(t, "cache", f.Name())
	assert.NotEmpty(t, f.RedactedAddr())

	require.NoError(t, f.Ping(context.Background()))
	assert.Equal(t, 1, f.PingCalls())

	f.PingErr = errors.New("boom")
	assert.ErrorIs(t, f.Ping(context.Background()), f.PingErr)
	assert.Equal(t, 2, f.PingCalls())

	f.RunScriptResult = "ok"
	script := redis.NewScript("return 1")
	result, err := f.RunScript(context.Background(), script, []string{"k1"}, "a1")
	require.NoError(t, err)
	assert.Equal(t, "ok", result)
	assert.Equal(t, 1, f.RunScriptCalls())

	calls := f.ScriptCalls()
	require.Len(t, calls, 1)
	assert.Same(t, script, calls[0].Script)
	assert.Equal(t, []string{"k1"}, calls[0].Keys)
	assert.Equal(t, []any{"a1"}, calls[0].Args)

	f.Stats = PoolStats{TotalConns: 5}
	assert.Equal(t, PoolStats{TotalConns: 5}, f.PoolStats())

	require.NoError(t, f.Close())
	assert.Equal(t, 1, f.CloseCalls())
}

// TestFake_GetSetDelRoundTrip exercises the Task 9 in-memory data-command additions: Get on a
// missing key returns ErrNotFound (never a zero-value string with a nil error, which callers could
// mistake for a real empty cached value), Set makes it visible, and Del removes it again.
func TestFake_GetSetDelRoundTrip(t *testing.T) {
	f := NewFake("cache")
	ctx := context.Background()

	_, err := f.Get(ctx, "missing")
	assert.ErrorIs(t, err, ErrNotFound)

	require.NoError(t, f.Set(ctx, "k1", "v1", 0))
	val, err := f.Get(ctx, "k1")
	require.NoError(t, err)
	assert.Equal(t, "v1", val)

	require.NoError(t, f.Del(ctx, "k1"))
	_, err = f.Get(ctx, "k1")
	assert.ErrorIs(t, err, ErrNotFound)
}

// TestFake_SetNX proves the fake's SetNX honours "set only if absent" semantics: the first caller
// wins and gets ok=true; a second caller against the same key gets ok=false and the value is left
// untouched (this is the primitive generation-minting under a race depends on).
func TestFake_SetNX(t *testing.T) {
	f := NewFake("cache")
	ctx := context.Background()

	ok, err := f.SetNX(ctx, "gen", "token-a", 0)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = f.SetNX(ctx, "gen", "token-b", 0)
	require.NoError(t, err)
	assert.False(t, ok)

	val, err := f.Get(ctx, "gen")
	require.NoError(t, err)
	assert.Equal(t, "token-a", val, "SetNX must not overwrite an existing value")
}

// TestFake_MGet proves the fake reports per-key presence rather than folding a missing key into a
// zero-value string: MGET against Redis returns a nil entry for a missing key, distinct from a
// key holding an actual empty string.
func TestFake_MGet(t *testing.T) {
	f := NewFake("cache")
	ctx := context.Background()

	require.NoError(t, f.Set(ctx, "a", "va", 0))
	require.NoError(t, f.Set(ctx, "b", "", 0))

	vals, found, err := f.MGet(ctx, "a", "b", "c")
	require.NoError(t, err)
	require.Len(t, vals, 3)
	require.Len(t, found, 3)

	assert.True(t, found[0])
	assert.Equal(t, "va", vals[0])
	assert.True(t, found[1])
	assert.Equal(t, "", vals[1])
	assert.False(t, found[2])
}

// TestFake_ErrorInjectionForNewDataCommands lets consumers (Task 9) drive the "Redis errored"
// paths for Get/Set/SetNX/MGet/Del without a real server.
func TestFake_ErrorInjectionForNewDataCommands(t *testing.T) {
	f := NewFake("cache")
	ctx := context.Background()
	boom := errors.New("boom")

	f.GetErr = boom
	_, err := f.Get(ctx, "k")
	assert.ErrorIs(t, err, boom)

	f.SetErr = boom
	assert.ErrorIs(t, f.Set(ctx, "k", "v", 0), boom)

	f.SetNXErr = boom
	_, err = f.SetNX(ctx, "k", "v", 0)
	assert.ErrorIs(t, err, boom)

	f.MGetErr = boom
	_, _, err = f.MGet(ctx, "k")
	assert.ErrorIs(t, err, boom)

	f.DelErr = boom
	assert.ErrorIs(t, f.Del(ctx, "k"), boom)
}

// TestFake_ScanMatchesGlobPattern is the RED test for Issue #1531 Task 10b's GDPR purge: the
// purge path needs to enumerate every data+generation key under a scope tag (e.g.
// "bb:c:{u:<uid>}:*") to delete them outright, which Get/Set/MGet/Del cannot do since none of
// them enumerate keys by pattern.
func TestFake_ScanMatchesGlobPattern(t *testing.T) {
	f := NewFake("cache")
	ctx := context.Background()

	require.NoError(t, f.Set(ctx, "bb:c:{u:1}:analytics:v1:gABC:Method:hash1", "v", 0))
	require.NoError(t, f.Set(ctx, "bb:c:{u:1}:analytics:v1:gABC:Method:hash2", "v", 0))
	require.NoError(t, f.Set(ctx, "bb:c:{u:1}:gen", "ABC", 0))
	require.NoError(t, f.Set(ctx, "bb:c:{u:2}:analytics:v1:gABC:Method:hash1", "v", 0))

	keys, err := f.Scan(ctx, "bb:c:{u:1}:*")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"bb:c:{u:1}:analytics:v1:gABC:Method:hash1",
		"bb:c:{u:1}:analytics:v1:gABC:Method:hash2",
		"bb:c:{u:1}:gen",
	}, keys)
}

func TestFake_ScanErr(t *testing.T) {
	f := NewFake("cache")
	f.ScanErr = errors.New("boom")
	_, err := f.Scan(context.Background(), "*")
	assert.ErrorIs(t, err, f.ScanErr)
}
