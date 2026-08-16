package main

// Issue #1531 Blocker 4 — REDIS_TIMEOUT_MS / REDIS_POOL_SIZE were loaded, validated, and
// documented (internal/config/config.go ~:89-92,249-250 — the comment even claims they are "read
// by redisclient.Options construction") but neither main() call site
// (redisclient.New(redisclient.Options{...})) actually passed them through: both sent only
// URL/Name, so the knobs validated at boot were dead — every deploy silently ran on
// redisclient's hardcoded defaults (200ms read/write timeout, pool size 10) regardless of what an
// operator configured.
//
// newRedisClientOptions (main.go) is the fix: both call sites now build their Options through it.
// This test proves the mapping itself is correct — the one part of the fix a real-Redis
// integration test (which only sees the Client interface, with no Options() accessor) cannot
// observe directly. redisclient's own TestNew_ExplicitOptionsOverrideDefaults already proves,
// against the real go-redis client's rdb.Options(), that whatever an Options struct carries DOES
// reach the wire — so once this test pins that cfg.RedisTimeoutMs/RedisPoolSize land correctly in
// the Options struct handed to redisclient.New, the two tests together close the whole chain:
// config -> Options -> real client.
//
// ADVERSARIAL VALIDATION: temporarily reverted newRedisClientOptions to the pre-fix shape (only
// URL/Name set, ReadTimeout/WriteTimeout/PoolSize left zero) — TestNewRedisClientOptions_
// WiresConfiguredTimeoutAndPoolSize FAILED (zero != configured values). Restored the fix — test
// PASSED again.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
)

func TestNewRedisClientOptions_WiresConfiguredTimeoutAndPoolSize(t *testing.T) {
	cfg := &config.Config{
		RedisTimeoutMs: 350,
		RedisPoolSize:  17,
	}

	opts := newRedisClientOptions(cfg, "redis://example.invalid:6379", "cache")

	assert.Equal(t, "redis://example.invalid:6379", opts.URL)
	assert.Equal(t, "cache", opts.Name)
	assert.Equal(t, 350*time.Millisecond, opts.ReadTimeout,
		"REDIS_TIMEOUT_MS must reach Options.ReadTimeout")
	assert.Equal(t, 350*time.Millisecond, opts.WriteTimeout,
		"REDIS_TIMEOUT_MS must reach Options.WriteTimeout")
	assert.Equal(t, 17, opts.PoolSize, "REDIS_POOL_SIZE must reach Options.PoolSize")
}

// redisClientCallSite is one `redisclient.New(newRedisClientOptions(cfg, <urlExpr>, <nameExpr>))`
// call site found in main.go, with urlExpr/nameExpr rendered back to source text so the test can
// compare against what main.go actually wrote, not a value this test invented.
type redisClientCallSite struct {
	urlExpr, nameExpr string
}

// scanRedisClientNewCallSites walks main.go's AST (this package's own file, found via go/build's
// caller-independent approach of just parsing "main.go" in the current directory — this test
// package's directory IS cmd/server) and returns one entry per
// `redisclient.New(newRedisClientOptions(cfg, url, name))` call site found, in source order.
func scanRedisClientNewCallSites(t *testing.T) []redisClientCallSite {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", nil, 0)
	require.NoError(t, err, "parsing main.go")

	var sites []redisClientCallSite
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "New" {
			return true
		}
		pkgIdent, ok := sel.X.(*ast.Ident)
		if !ok || pkgIdent.Name != "redisclient" {
			return true
		}
		if len(call.Args) != 1 {
			return true
		}
		inner, ok := call.Args[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		innerIdent, ok := inner.Fun.(*ast.Ident)
		if !ok || innerIdent.Name != "newRedisClientOptions" || len(inner.Args) != 3 {
			return true
		}
		sites = append(sites, redisClientCallSite{
			urlExpr:  exprString(inner.Args[1]),
			nameExpr: exprString(inner.Args[2]),
		})
		return true
	})
	return sites
}

// exprString renders simple expressions (identifiers, selectors, string literals) back to their
// source form, which is all the shapes newRedisClientOptions' url/name arguments actually take in
// main.go today (cfg.RateLimitRedisURL, "cache", cache.LimiterClientName).
func exprString(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return exprString(v.X) + "." + v.Sel.Name
	case *ast.BasicLit:
		return v.Value
	default:
		return "<unrecognized expr>"
	}
}

// TestMainGo_WiresDistinctRedisClientOptionsForLimiterAndCache is the real-wiring counterpart D9
// asked for: TestNewRedisClientOptions_WiresConfiguredTimeoutAndPoolSize above proves the
// config->Options mapping is correct in isolation, but nothing previously verified that main.go's
// OWN two call sites actually invoke newRedisClientOptions with the limiter/cache config fields —
// as opposed to, say, both call sites accidentally passing cfg.RateLimitRedisURL (a copy-paste
// that would silently point the cache client at the rate-limiter's Redis instance, or vice versa).
// Calling newRedisClientOptions twice with values this test invents (the old shape) cannot catch
// that: it would pass identically whether or not main.go's real call sites are correct or swapped.
func TestMainGo_WiresDistinctRedisClientOptionsForLimiterAndCache(t *testing.T) {
	sites := scanRedisClientNewCallSites(t)
	require.Len(t, sites, 2, "expected exactly 2 redisclient.New(newRedisClientOptions(...)) call "+
		"sites in main.go (limiter client, cache client) — found a different count; main.go's Redis "+
		"client wiring shape has changed and this guard needs to be updated to match")

	byURL := map[string]redisClientCallSite{}
	for _, s := range sites {
		byURL[s.urlExpr] = s
	}

	limiterSite, ok := byURL["cfg.RateLimitRedisURL"]
	require.True(t, ok, "no redisclient.New(newRedisClientOptions(cfg, cfg.RateLimitRedisURL, ...)) "+
		"call site found in main.go — found URL expressions: %v", sites)
	assert.Equal(t, "cache.LimiterClientName", limiterSite.nameExpr,
		"the limiter client's redisclient.New call must name it via cache.LimiterClientName "+
			"(== \"limiter\"), not a literal that could drift from cache.NewControl's own check")

	cacheSite, ok := byURL["cfg.CacheRedisURL"]
	require.True(t, ok, "no redisclient.New(newRedisClientOptions(cfg, cfg.CacheRedisURL, ...)) "+
		"call site found in main.go — found URL expressions: %v", sites)
	assert.Equal(t, `"cache"`, cacheSite.nameExpr,
		"the cache client's redisclient.New call must be named \"cache\"")

	assert.NotEqual(t, limiterSite.urlExpr, cacheSite.urlExpr,
		"the limiter and cache clients must be wired to DIFFERENT config URL fields — if both "+
			"call sites used the same field, one client would silently point at the wrong Redis "+
			"instance")
}
