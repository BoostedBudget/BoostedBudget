package redisclient

// This file has NO build tag: Fake is imported by OTHER packages' ordinary unit tests (Tasks 4, 5,
// 9), not just by tests inside this package.

import (
	"context"
	"sync"

	redis "github.com/redis/go-redis/v9"
)

// ScriptCall records a single invocation of Fake.RunScript, so callers can assert on what a
// consumer asked Redis to do without a real server.
type ScriptCall struct {
	Script *redis.Script
	Keys   []string
	Args   []any
}

// Fake is an in-memory Client for use in other packages' unit tests. It never dials a real Redis
// server. Configure PingErr/RunScriptErr to drive failure paths (breaker trips, limiter fallback);
// PingCalls/RunScriptCalls/ScriptCalls record what happened for assertions.
type Fake struct {
	name         string
	redactedAddr string

	mu sync.Mutex

	// PingErr, when non-nil, is returned by every Ping call.
	PingErr error
	// RunScriptErr, when non-nil, is returned by every RunScript call.
	RunScriptErr error
	// RunScriptResult is returned by RunScript when RunScriptErr is nil.
	RunScriptResult any
	// Stats is returned verbatim by PoolStats.
	Stats PoolStats
	// CloseErr, when non-nil, is returned by Close.
	CloseErr error

	pingCalls      int
	runScriptCalls int
	closeCalls     int
	scriptCalls    []ScriptCall
}

// NewFake returns a Fake Client with the given name and a "host:port"-shaped RedactedAddr, so
// tests that assert on it don't need to special-case the fake.
func NewFake(name string) *Fake {
	return &Fake{name: name, redactedAddr: "fake:0"}
}

func (f *Fake) Name() string { return f.name }

func (f *Fake) RedactedAddr() string { return f.redactedAddr }

func (f *Fake) Ping(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pingCalls++
	return f.PingErr
}

// PingCalls returns how many times Ping has been invoked.
func (f *Fake) PingCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pingCalls
}

func (f *Fake) RunScript(_ context.Context, s *redis.Script, keys []string, args ...any) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runScriptCalls++
	f.scriptCalls = append(f.scriptCalls, ScriptCall{Script: s, Keys: keys, Args: args})
	if f.RunScriptErr != nil {
		return nil, f.RunScriptErr
	}
	return f.RunScriptResult, nil
}

// RunScriptCalls returns how many times RunScript has been invoked.
func (f *Fake) RunScriptCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runScriptCalls
}

// ScriptCalls returns a copy of every recorded RunScript invocation, in call order.
func (f *Fake) ScriptCalls() []ScriptCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]ScriptCall, len(f.scriptCalls))
	copy(out, f.scriptCalls)
	return out
}

func (f *Fake) PoolStats() PoolStats {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Stats
}

func (f *Fake) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeCalls++
	return f.CloseErr
}

// CloseCalls returns how many times Close has been invoked.
func (f *Fake) CloseCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closeCalls
}

var _ Client = (*Fake)(nil)
