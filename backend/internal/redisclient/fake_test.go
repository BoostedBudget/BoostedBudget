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
