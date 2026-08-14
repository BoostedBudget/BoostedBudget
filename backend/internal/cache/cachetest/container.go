// This file has NO build tag (same convention as redis.go): it is a plain library so it compiles
// under a normal `go build ./...`. Only the *_test.go files that actually spin up a container
// (which needs a working Docker daemon and takes real wall-clock time) carry `//go:build
// integration`.
package cachetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	redis "github.com/redis/go-redis/v9"
)

// Container is a disposable, single-test redis:7-alpine instance started by StartRedisContainer.
// Unlike ConnectTestRedis/NewIsolatedCache above (which point at the SHARED cache/limiter Redis
// instances and forbid FLUSHDB and forced eviction because those are destructive to other tests),
// a Container is provisioned fresh per test and torn down unconditionally, so both operations are
// safe here — that is the entire reason this type exists.
type Container struct {
	// URL is a redis://127.0.0.1:<port>/0 connection string for the container's random host port.
	URL string
	// Client is a real go-redis client connected to the container. It is a *redis.Client (not
	// redisclient.Client) deliberately: redisclient.Client's interface intentionally does not
	// expose FLUSHDB or CONFIG SET (production code must never issue either against the shared
	// instances — see redisclient's package doc comment), but the fail-safe suite this container
	// exists for needs exactly those two commands.
	Client *redis.Client

	name string
}

// dockerCheckTimeout bounds how long StartRedisContainer waits to confirm a usable Docker daemon
// (and small housekeeping docker calls like `docker rm`/`docker port`) before deciding to t.Skip
// or fail rather than hang.
const dockerCheckTimeout = 5 * time.Second

// dockerRunTimeout bounds `docker run` itself, which — unlike the daemon check — may need to pull
// the redis:7-alpine image on a cold cache. Deliberately much larger than dockerCheckTimeout so a
// healthy-but-cold Docker install doesn't spuriously fail on first use.
const dockerRunTimeout = 60 * time.Second

// containerReadyTimeout bounds how long StartRedisContainer waits for `redis-cli ping` to succeed
// inside the freshly started container before failing the test outright (Docker was available, so
// a failure here is a real problem, not an environment gap worth skipping over).
const containerReadyTimeout = 15 * time.Second

// containerNamePattern strips everything Docker container names disallow (only
// `[a-zA-Z0-9][a-zA-Z0-9_.-]` is legal) — t.Name() for a subtest contains "/", which is not.
var containerNamePattern = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)

// StartRedisContainer docker-runs a disposable redis:7-alpine container, binding container port
// 6379 to a host port Docker itself assigns (never one this process picked and released first —
// that would be a TOCTOU race against any other process, including a concurrent test run, doing
// the same thing). It waits for the container to answer `redis-cli ping` with PONG, and returns a
// Container wrapping it. t.Cleanup removes the container unconditionally — including when the
// test fails or panics — so a killed test run never leaves it behind; cleanup is registered
// BEFORE `docker run` even starts, keyed only on the container's name, so a container that gets
// created on the daemon but whose `docker run` client call then times out (e.g. mid image pull)
// is still reaped.
//
// serverArgs, if given, are appended verbatim to the container's `redis-server` command line
// (e.g. "--maxmemory", "10mb", "--maxmemory-policy", "allkeys-lru" for an eviction test).
//
// StartRedisContainer calls t.Skip (not t.Fatal) when Docker itself is unavailable, so CI/dev
// environments without Docker skip cleanly instead of hard-failing every test that needs one.
func StartRedisContainer(t *testing.T, serverArgs ...string) *Container {
	t.Helper()

	requireDocker(t)

	name := containerName(t)

	// Registered before docker run is even attempted — see the doc comment above.
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), dockerCheckTimeout)
		defer cancel()
		_ = exec.CommandContext(cleanupCtx, "docker", "rm", "-f", name).Run()
	})

	args := append([]string{
		"run", "-d", "--rm",
		"--name", name,
		// "127.0.0.1::6379" (empty host port) tells Docker to pick the host port itself, bound to
		// loopback only — never 0.0.0.0, which would publish this disposable Redis on every host
		// interface.
		"-p", "127.0.0.1::6379",
		"redis:7-alpine",
		"redis-server",
	}, serverArgs...)

	runCtx, cancel := context.WithTimeout(context.Background(), dockerRunTimeout)
	defer cancel()
	if out, err := exec.CommandContext(runCtx, "docker", args...).CombinedOutput(); err != nil {
		t.Fatalf("cachetest.StartRedisContainer: docker run failed: %v\n%s", err, out)
	}

	port := hostPortFor(t, name)

	waitForRedisReady(t, name)

	url := fmt.Sprintf("redis://127.0.0.1:%d/0", port)
	client := redis.NewClient(&redis.Options{Addr: fmt.Sprintf("127.0.0.1:%d", port)})
	t.Cleanup(func() { _ = client.Close() })

	return &Container{URL: url, Client: client, name: name}
}

// hostPortFor asks Docker which host port it assigned to name's published 6379/tcp, via
// `docker port` — the container's own inspection, not a value this process guessed beforehand.
func hostPortFor(t *testing.T, name string) int {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), dockerCheckTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "port", name, "6379/tcp").CombinedOutput()
	if err != nil {
		t.Fatalf("cachetest.StartRedisContainer: docker port failed: %v\n%s", err, out)
	}

	// Output looks like "127.0.0.1:54321" (one line per published binding on this address).
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	idx := strings.LastIndex(line, ":")
	if idx < 0 {
		t.Fatalf("cachetest.StartRedisContainer: unrecognized `docker port` output: %q", out)
	}
	port, err := strconv.Atoi(line[idx+1:])
	if err != nil {
		t.Fatalf("cachetest.StartRedisContainer: unrecognized `docker port` output: %q", out)
	}
	return port
}

// requireDocker t.Skips the calling test unless a Docker daemon is reachable within
// dockerCheckTimeout.
func requireDocker(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("cachetest.StartRedisContainer: docker not found on PATH; skipping")
	}

	ctx, cancel := context.WithTimeout(context.Background(), dockerCheckTimeout)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "docker", "info").CombinedOutput(); err != nil {
		t.Skipf("cachetest.StartRedisContainer: docker daemon unreachable; skipping: %v\n%s", err, out)
	}
}

// waitForRedisReady polls `docker exec <name> redis-cli ping` until it reports PONG or
// containerReadyTimeout elapses, at which point it fails the test (Docker itself was confirmed
// reachable by requireDocker, so a container that never becomes ready is a genuine failure, not
// an environment gap).
func waitForRedisReady(t *testing.T, name string) {
	t.Helper()

	deadline := time.Now().Add(containerReadyTimeout)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		out, err := exec.CommandContext(ctx, "docker", "exec", name, "redis-cli", "ping").CombinedOutput()
		cancel()
		if err == nil && strings.TrimSpace(string(out)) == "PONG" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("cachetest.StartRedisContainer: container %q never answered redis-cli ping within %s", name, containerReadyTimeout)
}

// containerName returns a Docker container name unique to this test run: a "cachetest-" prefix,
// t.Name() with every Docker-illegal character collapsed to "-", and an 8-hex-digit random
// suffix. The random suffix is what protects parallel runs (and leftovers from a previously killed
// run reusing the same test name) from colliding on name — t.Name() alone is not enough since two
// separate `go test` invocations of the identical test share it.
func containerName(t *testing.T) string {
	t.Helper()

	sanitized := containerNamePattern.ReplaceAllString(t.Name(), "-")

	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("cachetest.containerName: crypto/rand unavailable: %v", err)
	}

	return "cachetest-" + sanitized + "-" + hex.EncodeToString(b[:])
}
