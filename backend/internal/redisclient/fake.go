package redisclient

// This file has NO build tag: Fake is imported by OTHER packages' ordinary unit tests (Tasks 4, 5,
// 9), not just by tests inside this package.

import (
	"context"
	"sync"
	"time"

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

	// GetErr, when non-nil, is returned by every Get call (instead of consulting the in-memory
	// store).
	GetErr error
	// SetErr, when non-nil, is returned by every Set call (the store is left unmodified).
	SetErr error
	// SetNXErr, when non-nil, is returned by every SetNX call.
	SetNXErr error
	// MGetErr, when non-nil, is returned by every MGet call.
	MGetErr error
	// DelErr, when non-nil, is returned by every Del call.
	DelErr error

	pingCalls      int
	runScriptCalls int
	closeCalls     int
	getCalls       int
	setCalls       int
	setNXCalls     int
	mgetCalls      int
	delCalls       int
	scriptCalls    []ScriptCall

	store map[string]string
}

// NewFake returns a Fake Client with the given name and a "host:port"-shaped RedactedAddr, so
// tests that assert on it don't need to special-case the fake.
func NewFake(name string) *Fake {
	return &Fake{name: name, redactedAddr: "fake:0", store: make(map[string]string)}
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

func (f *Fake) Get(_ context.Context, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalls++
	if f.GetErr != nil {
		return "", f.GetErr
	}
	v, ok := f.store[key]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (f *Fake) Set(_ context.Context, key, value string, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setCalls++
	if f.SetErr != nil {
		return f.SetErr
	}
	f.store[key] = value
	return nil
}

func (f *Fake) SetNX(_ context.Context, key, value string, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setNXCalls++
	if f.SetNXErr != nil {
		return false, f.SetNXErr
	}
	if _, exists := f.store[key]; exists {
		return false, nil
	}
	f.store[key] = value
	return true, nil
}

func (f *Fake) MGet(_ context.Context, keys ...string) ([]string, []bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mgetCalls++
	if f.MGetErr != nil {
		return nil, nil, f.MGetErr
	}
	vals := make([]string, len(keys))
	found := make([]bool, len(keys))
	for i, k := range keys {
		if v, ok := f.store[k]; ok {
			vals[i] = v
			found[i] = true
		}
	}
	return vals, found, nil
}

func (f *Fake) Del(_ context.Context, keys ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delCalls++
	if f.DelErr != nil {
		return f.DelErr
	}
	for _, k := range keys {
		delete(f.store, k)
	}
	return nil
}

// GetCalls returns how many times Get has been invoked.
func (f *Fake) GetCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.getCalls
}

// SetCalls returns how many times Set has been invoked.
func (f *Fake) SetCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.setCalls
}

// SetNXCalls returns how many times SetNX has been invoked.
func (f *Fake) SetNXCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.setNXCalls
}

// MGetCalls returns how many times MGet has been invoked.
func (f *Fake) MGetCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mgetCalls
}

// DelCalls returns how many times Del has been invoked.
func (f *Fake) DelCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.delCalls
}

// TotalCommandCalls sums every recorded invocation across all commands (Ping, RunScript, Get, Set,
// SetNX, MGet, Del). It exists so a consumer's "zero Redis commands" test (e.g. Task 9's
// TestNoopCache_IssuesZeroRedisCommands) has one number to assert is 0, instead of enumerating
// every method and risking a silent gap the next time a command is added.
func (f *Fake) TotalCommandCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pingCalls + f.runScriptCalls + f.getCalls + f.setCalls + f.setNXCalls + f.mgetCalls + f.delCalls
}

var _ Client = (*Fake)(nil)
