package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validCloudflareIPv4 is an IP in Cloudflare's published range 173.245.48.0/20.
const validCloudflareIPv4 = "173.245.48.1"

// validCloudflareIPv4Mapped is the same IP in IPv4-mapped IPv6 notation.
const validCloudflareIPv4Mapped = "::ffff:173.245.48.1"

// nonCloudflareIP is a well-known IP not in any Cloudflare CIDR.
const nonCloudflareIP = "8.8.8.8"

const testSecret = "a3f9e2c1b0d84726f5e3a1c0b9d87264a3f9e2c1b0d84726f5e3a1c0b9d87264"

// newCIDRRef builds an atomic.Pointer holding the given CIDR prefixes for use in tests.
func newCIDRRef(prefixes []netip.Prefix) *atomic.Pointer[[]netip.Prefix] {
	ref := &atomic.Pointer[[]netip.Prefix]{}
	ref.Store(&prefixes)
	return ref
}

// mustParsePrefix parses a CIDR string and panics on failure.
func mustParsePrefix(s string) netip.Prefix {
	p, err := netip.ParsePrefix(s)
	if err != nil {
		panic("mustParsePrefix: " + err.Error())
	}
	return p
}

// testCIDRs is a minimal set of Cloudflare CIDRs containing validCloudflareIPv4.
var testCIDRs = []netip.Prefix{
	mustParsePrefix("173.245.48.0/20"),
	mustParsePrefix("104.16.0.0/13"),
}

// setupCloudflareEcho creates an Echo instance with the CloudflareOnly middleware and
// a test handler at GET /test, GET /health, GET /health/, GET /metrics, GET /metrics/.
func setupCloudflareEcho(cidrs []netip.Prefix, secret, env string) *echo.Echo {
	e := echo.New()
	e.Use(middleware.CloudflareOnly(newCIDRRef(cidrs), secret, env))
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	}
	e.GET("/test", handler)
	e.GET("/health", handler)
	e.GET("/health/", handler)   // trailing-slash variant (Fly.io may send this)
	e.GET("/metrics", handler)
	e.GET("/metrics/", handler)  // trailing-slash variant (monitoring probes)
	return e
}

// doRequest fires a request against the Echo instance and returns the response code.
func doRequest(e *echo.Echo, path string, headers map[string]string) int {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	e.ServeHTTP(rr, req)
	return rr.Code
}

// ---------------------------------------------------------------------------
// CloudflareOnly — blocking behavior
// ---------------------------------------------------------------------------

func TestCloudflareOnly_BlocksWhenNoFlyClientIP(t *testing.T) {
	e := setupCloudflareEcho(testCIDRs, testSecret, "staging")
	// No Fly-Client-IP header at all — must return 403.
	code := doRequest(e, "/test", map[string]string{
		"X-Origin-Secret": testSecret,
	})
	assert.Equal(t, http.StatusForbidden, code)
}

func TestCloudflareOnly_BlocksWhenIPNotInCFRanges(t *testing.T) {
	e := setupCloudflareEcho(testCIDRs, testSecret, "staging")
	// Fly-Client-IP is a real IP but NOT in Cloudflare's ranges.
	code := doRequest(e, "/test", map[string]string{
		"Fly-Client-Ip": nonCloudflareIP,
		"X-Origin-Secret":   testSecret,
	})
	assert.Equal(t, http.StatusForbidden, code)
}

func TestCloudflareOnly_BlocksWhenSecretMissing(t *testing.T) {
	e := setupCloudflareEcho(testCIDRs, testSecret, "staging")
	// Valid Cloudflare IP, but no X-Origin-Secret header.
	code := doRequest(e, "/test", map[string]string{
		"Fly-Client-Ip": validCloudflareIPv4,
		// X-Origin-Secret intentionally omitted
	})
	assert.Equal(t, http.StatusForbidden, code)
}

func TestCloudflareOnly_BlocksWhenSecretWrong(t *testing.T) {
	e := setupCloudflareEcho(testCIDRs, testSecret, "staging")
	code := doRequest(e, "/test", map[string]string{
		"Fly-Client-Ip": validCloudflareIPv4,
		"X-Origin-Secret":   "wrong-secret-value",
	})
	assert.Equal(t, http.StatusForbidden, code)
}

func TestCloudflareOnly_BlocksWhenMultipleSecretHeaders(t *testing.T) {
	e := setupCloudflareEcho(testCIDRs, testSecret, "staging")
	// Multiple X-Origin-Secret headers: one correct, one attacker-injected.
	// Must be rejected — never accept multi-value secret headers.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Fly-Client-Ip", validCloudflareIPv4)
	req.Header.Add("X-Origin-Secret", testSecret) // correct
	req.Header.Add("X-Origin-Secret", "injected") // attacker addition
	e.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusForbidden, rr.Code)
}

func TestCloudflareOnly_BlocksWhenEmptyFlyClientIP(t *testing.T) {
	e := setupCloudflareEcho(testCIDRs, testSecret, "staging")
	// Empty string Fly-Client-IP header — must reject.
	code := doRequest(e, "/test", map[string]string{
		"Fly-Client-Ip": "",
		"X-Origin-Secret":   testSecret,
	})
	assert.Equal(t, http.StatusForbidden, code)
}

func TestCloudflareOnly_BlocksWhenMalformedFlyClientIP(t *testing.T) {
	e := setupCloudflareEcho(testCIDRs, testSecret, "staging")
	code := doRequest(e, "/test", map[string]string{
		"Fly-Client-Ip": "not-an-ip-address",
		"X-Origin-Secret":   testSecret,
	})
	assert.Equal(t, http.StatusForbidden, code)
}

// ---------------------------------------------------------------------------
// CloudflareOnly — allow behavior
// ---------------------------------------------------------------------------

func TestCloudflareOnly_AllowsValidIPAndSecret(t *testing.T) {
	e := setupCloudflareEcho(testCIDRs, testSecret, "staging")
	code := doRequest(e, "/test", map[string]string{
		"Fly-Client-Ip": validCloudflareIPv4,
		"X-Origin-Secret":   testSecret,
	})
	assert.Equal(t, http.StatusOK, code)
}

func TestCloudflareOnly_AllowsValidIPAndSecretInProduction(t *testing.T) {
	e := setupCloudflareEcho(testCIDRs, testSecret, "production")
	code := doRequest(e, "/test", map[string]string{
		"Fly-Client-Ip": validCloudflareIPv4,
		"X-Origin-Secret":   testSecret,
	})
	assert.Equal(t, http.StatusOK, code)
}

// ---------------------------------------------------------------------------
// CloudflareOnly — environment bypass
// ---------------------------------------------------------------------------

func TestCloudflareOnly_SkipsCheckInDevelopment(t *testing.T) {
	e := setupCloudflareEcho(testCIDRs, testSecret, "development")
	// No CF headers at all — should still get 200 in development.
	code := doRequest(e, "/test", nil)
	assert.Equal(t, http.StatusOK, code)
}

func TestCloudflareOnly_SkipsCheckInLocal(t *testing.T) {
	e := setupCloudflareEcho(testCIDRs, testSecret, "local")
	code := doRequest(e, "/test", nil)
	assert.Equal(t, http.StatusOK, code)
}

func TestCloudflareOnly_SkipsCheckInTest(t *testing.T) {
	e := setupCloudflareEcho(testCIDRs, testSecret, "test")
	code := doRequest(e, "/test", nil)
	assert.Equal(t, http.StatusOK, code)
}

func TestCloudflareOnly_SkipsCheckInQA(t *testing.T) {
	// QA environment is used by Playwright E2E tests (playwright.config.ts sets ENVIRONMENT=qa).
	// Direct localhost requests have no Cloudflare headers, so the middleware must bypass in qa.
	e := setupCloudflareEcho(testCIDRs, testSecret, "qa")
	code := doRequest(e, "/test", nil)
	assert.Equal(t, http.StatusOK, code)
}

// ---------------------------------------------------------------------------
// CloudflareOnly — exempted paths
// ---------------------------------------------------------------------------

func TestCloudflareOnly_HealthCheckBypassed(t *testing.T) {
	e := setupCloudflareEcho(testCIDRs, testSecret, "staging")
	// /health must be accessible without CF headers (Fly.io internal health check).
	code := doRequest(e, "/health", nil)
	assert.Equal(t, http.StatusOK, code)
}

func TestCloudflareOnly_MetricsBypassed(t *testing.T) {
	e := setupCloudflareEcho(testCIDRs, testSecret, "staging")
	// /metrics must be accessible without CF headers (Fly.io metrics scraper).
	code := doRequest(e, "/metrics", nil)
	assert.Equal(t, http.StatusOK, code)
}

// ---------------------------------------------------------------------------
// CloudflareOnly — IPv4-mapped IPv6
// ---------------------------------------------------------------------------

func TestCloudflareOnly_IPv4MappedIPv6Handled(t *testing.T) {
	e := setupCloudflareEcho(testCIDRs, testSecret, "staging")
	// Fly.io may present the client IP in IPv4-mapped IPv6 form.
	// ::ffff:173.245.48.1 must match the 173.245.48.0/20 IPv4 CIDR.
	code := doRequest(e, "/test", map[string]string{
		"Fly-Client-Ip": validCloudflareIPv4Mapped,
		"X-Origin-Secret":   testSecret,
	})
	assert.Equal(t, http.StatusOK, code)
}

// ---------------------------------------------------------------------------
// LoadCloudflareCIDRsFrom — CIDR loading strategy
// ---------------------------------------------------------------------------

func TestLoadCloudflareCIDRs_FallbackOnFetchFailure(t *testing.T) {
	// Point to an unreachable server — must fall back to hardcoded list.
	ctx := context.Background()
	cidrs := middleware.LoadCloudflareCIDRsFrom(ctx, "http://127.0.0.1:0/ips-v4", "http://127.0.0.1:0/ips-v6")

	require.NotEmpty(t, cidrs, "should return hardcoded fallback list when fetch fails")
	// The fallback must include the Cloudflare CIDRs we test against.
	found := false
	target, _ := netip.ParsePrefix("173.245.48.0/20")
	for _, p := range cidrs {
		if p == target {
			found = true
			break
		}
	}
	assert.True(t, found, "fallback list should contain 173.245.48.0/20")
}

func TestLoadCloudflareCIDRs_UsesFetchedWhenAvailable(t *testing.T) {
	// Serve a custom IP list — should use it instead of the hardcoded fallback.
	v4Body := "10.0.0.0/8\n192.168.0.0/16\n"
	v6Body := "fc00::/7\n"

	v4Srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(v4Body))
	}))
	defer v4Srv.Close()

	v6Srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(v6Body))
	}))
	defer v6Srv.Close()

	ctx := context.Background()
	cidrs := middleware.LoadCloudflareCIDRsFrom(ctx, v4Srv.URL, v6Srv.URL)

	require.Len(t, cidrs, 3, "should return exactly the 3 CIDRs served by mock")

	expected := map[string]bool{
		"10.0.0.0/8":     false,
		"192.168.0.0/16": false,
		"fc00::/7":       false,
	}
	for _, p := range cidrs {
		expected[p.Masked().String()] = true
	}
	for cidr, found := range expected {
		assert.True(t, found, "expected CIDR %s in result", cidr)
	}
}

func TestLoadCloudflareCIDRs_ValidatesNonEmpty(t *testing.T) {
	// Server returns empty body for IPv4 — must fall back to hardcoded list.
	emptySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// empty body
	}))
	defer emptySrv.Close()

	ctx := context.Background()
	cidrs := middleware.LoadCloudflareCIDRsFrom(ctx, emptySrv.URL, emptySrv.URL)

	require.NotEmpty(t, cidrs, "should fall back to hardcoded list when fetched list is empty")
}

func TestLoadCloudflareCIDRs_ValidatesAllParse(t *testing.T) {
	// Server returns an unparseable line mixed with valid ones — must fall back.
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-a-valid-cidr\n10.0.0.0/8\n"))
	}))
	defer badSrv.Close()

	ctx := context.Background()
	cidrs := middleware.LoadCloudflareCIDRsFrom(ctx, badSrv.URL, badSrv.URL)

	require.NotEmpty(t, cidrs, "should fall back to hardcoded list when fetched list contains unparseable entries")
	// The result must NOT contain the bad entry — it must be the known hardcoded list.
	for _, p := range cidrs {
		assert.NotEqual(t, "not-a-valid-cidr", p.String(),
			"fetched CIDRs with bad entries should not be used")
	}
}

// ---------------------------------------------------------------------------
// Hardcoded fallback CIDR list integrity
// ---------------------------------------------------------------------------

func TestCloudflareFallbackCIDRs_NonEmpty(t *testing.T) {
	cidrs := middleware.CloudflareFallbackCIDRs()
	assert.NotEmpty(t, cidrs, "hardcoded Cloudflare CIDR list must not be empty")
}

func TestCloudflareFallbackCIDRs_AllParseable(t *testing.T) {
	cidrs := middleware.CloudflareFallbackCIDRs()
	// If any entry failed to parse, it would not be in the list — so just verify non-empty
	// and that every entry has a valid prefix length.
	for _, p := range cidrs {
		assert.True(t, p.IsValid(), "all fallback CIDRs must be valid prefixes")
		assert.Greater(t, p.Bits(), 0, "all fallback CIDRs must have prefix length > 0")
	}
}

// ---------------------------------------------------------------------------
// Constant-time comparison edge cases
// ---------------------------------------------------------------------------

func TestCloudflareOnly_RejectsEmptySecretEvenWithValidIP(t *testing.T) {
	// When the configured originSecret is empty in prod/staging, block all.
	// (In practice config.validate() catches this before server starts,
	// but the middleware itself must also be safe.)
	e := setupCloudflareEcho(testCIDRs, "", "staging")
	// Attacker sends valid CF IP + empty secret header — must block.
	code := doRequest(e, "/test", map[string]string{
		"Fly-Client-Ip": validCloudflareIPv4,
		"X-Origin-Secret":   "",
	})
	assert.Equal(t, http.StatusForbidden, code)
}

func TestCloudflareOnly_RejectsDifferentLengthSecret(t *testing.T) {
	// Verifies constant-time compare handles length-mismatch without panic.
	e := setupCloudflareEcho(testCIDRs, testSecret, "staging")
	code := doRequest(e, "/test", map[string]string{
		"Fly-Client-Ip":   validCloudflareIPv4,
		"X-Origin-Secret": "short",
	})
	assert.Equal(t, http.StatusForbidden, code)
}

// ---------------------------------------------------------------------------
// CloudflareOnly — exempted paths (trailing-slash variants)
// ---------------------------------------------------------------------------

func TestCloudflareOnly_HealthCheckTrailingSlashBypassed(t *testing.T) {
	e := setupCloudflareEcho(testCIDRs, testSecret, "staging")
	code := doRequest(e, "/health/", nil)
	assert.Equal(t, http.StatusOK, code)
}

func TestCloudflareOnly_MetricsTrailingSlashBypassed(t *testing.T) {
	e := setupCloudflareEcho(testCIDRs, testSecret, "staging")
	code := doRequest(e, "/metrics/", nil)
	assert.Equal(t, http.StatusOK, code)
}

// ---------------------------------------------------------------------------
// CloudflareOnly — sentinel: production/staging must NOT be in bypass list
// ---------------------------------------------------------------------------

func TestCloudflareOnly_ProductionAndStagingRequireCFHeaders(t *testing.T) {
	// Sentinel: Verify "production" and "staging" are never in bypassEnvironments.
	for _, env := range []string{"production", "staging"} {
		env := env
		t.Run(env, func(t *testing.T) {
			e := setupCloudflareEcho(testCIDRs, testSecret, env)
			code := doRequest(e, "/test", nil)
			assert.Equal(t, http.StatusForbidden, code,
				"env %q must enforce CF checks", env)
		})
	}
}

// ---------------------------------------------------------------------------
// StartCIDRRefresher — goroutine behavior
// ---------------------------------------------------------------------------

func TestStartCIDRRefresher_SwapsAtomicPointerOnTick(t *testing.T) {
	// Serve distinct v4 and v6 CIDRs — refresher must atomically swap the pointer to them.
	v4Srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("10.255.0.0/16\n"))
	}))
	defer v4Srv.Close()

	v6Srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fc00::/7\n"))
	}))
	defer v6Srv.Close()

	// Start with the standard test CIDRs.
	ref := newCIDRRef(testCIDRs)
	initialPtr := ref.Load()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go middleware.StartCIDRRefresherFrom(ctx, 50*time.Millisecond, ref, v4Srv.URL, v6Srv.URL)

	// Wait up to 500ms for the first tick to fire and swap the pointer.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if ref.Load() != initialPtr {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	require.NotEqual(t, initialPtr, ref.Load(), "atomic pointer should have been swapped after tick")

	loaded := *ref.Load()
	require.Len(t, loaded, 2, "refreshed list should contain exactly the 2 CIDRs served (1 v4, 1 v6)")

	expectedV4, _ := netip.ParsePrefix("10.255.0.0/16")
	expectedV6, _ := netip.ParsePrefix("fc00::/7")
	assert.Contains(t, loaded, expectedV4, "refreshed list should contain the served v4 CIDR")
	assert.Contains(t, loaded, expectedV6, "refreshed list should contain the served v6 CIDR")
}

func TestStartCIDRRefresher_FallsBackToHardcodedListOnFetchFailure(t *testing.T) {
	// When the remote URL returns 500, the refresher must not set an empty list.
	// LoadCloudflareCIDRsFrom falls back to CloudflareFallbackCIDRs() — verify non-empty.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ref := newCIDRRef(testCIDRs)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go middleware.StartCIDRRefresherFrom(ctx, 50*time.Millisecond, ref, srv.URL, srv.URL)

	// Wait long enough for at least one tick to fire and attempt a (failing) fetch.
	time.Sleep(200 * time.Millisecond)

	loaded := *ref.Load()
	assert.NotEmpty(t, loaded, "atomic pointer must never be set to empty — hardcoded fallback should apply")
}
