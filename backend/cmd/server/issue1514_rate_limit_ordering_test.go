package main

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
)

// Issue #1514: the rate limiter's identity model (clientIdentity, see
// internal/middleware/rate_limit.go) only trusts the Cf-Connecting-Ip header
// when the request's echo.Context already carries GateStatusPassed, which
// CloudflareOnly stamps onto the context after verifying the immediate peer
// against Cloudflare's published CIDR ranges plus a shared X-Origin-Secret.
// That precondition — CloudflareOnly running BEFORE IPRateLimiter in the
// middleware chain — was previously enforced only by two adjacent e.Use(...)
// calls in main(), and every existing identity test
// (internal/middleware/rate_limit_test.go) injects the GateStatus directly
// via middleware.SetGateStatus rather than exercising the real CloudflareOnly
// middleware, so nothing failed if the two calls were silently swapped.
//
// This test builds the REAL chain — real CloudflareOnly, real IPRateLimiter,
// wired via the exact registerCloudflareAndRateLimitMiddleware helper main()
// calls — and proves the security property that ordering exists to provide:
// a peer that does NOT clear CloudflareOnly (wrong Fly-Client-Ip / bad or
// missing origin secret) can spoof any Cf-Connecting-Ip it likes without
// being able to consume, share, or exhaust another (genuine) client's
// rate-limit bucket.

// testCFCIDR is a documentation/test-only range (TEST-NET-3, RFC 5737) used
// here in place of Cloudflare's real published ranges so the test doesn't
// depend on network access or Cloudflare's actual CIDR list.
const testCFCIDR = "203.0.113.0/24"

// cloudflareEdgeIP is inside testCFCIDR: a genuine Cloudflare-edge peer.
const cloudflareEdgeIP = "203.0.113.5"

// attackerEdgeIP is outside testCFCIDR: a peer directly hitting the Fly app,
// bypassing Cloudflare entirely.
const attackerEdgeIP = "198.51.100.9"

// flyProxyRemoteAddr simulates what r.RemoteAddr is in the REAL Fly.io
// deployment: Fly's own internal proxy address, identical across virtually
// every inbound request regardless of which real end-client is behind it
// (documented in rate_limit.go's clientIdentity comment). Both the genuine
// request and the attacker's request below use this SAME value, because that
// is what actually happens on Fly — it is not a contrived coincidence.
const flyProxyRemoteAddr = "10.0.0.1:443"

const testOriginSecret = "issue-1514-test-origin-secret" //nolint:gosec // test fixture, not a real secret

func newOrderingTestCFCIDRRef() *atomic.Pointer[[]netip.Prefix] {
	ref := new(atomic.Pointer[[]netip.Prefix])
	prefixes := []netip.Prefix{netip.MustParsePrefix(testCFCIDR)}
	ref.Store(&prefixes)
	return ref
}

func newOrderingTestConfig() *config.Config {
	return &config.Config{
		Environment:         "production", // must NOT be in CloudflareOnly's bypassEnvironments
		CFOriginSecret:      testOriginSecret,
		RateLimitIP:         2,
		RateLimitAuth:       2,
		RateLimitMaxEntries: 100,
	}
}

// buildOrderedChain wires CloudflareOnly then IPRateLimiter via the shared
// production helper — the exact order main() uses.
func buildOrderedChain(t *testing.T) *echo.Echo {
	t.Helper()
	e := echo.New()
	registerCloudflareAndRateLimitMiddleware(e, newOrderingTestCFCIDRRef(), newOrderingTestConfig())
	e.GET("/", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })
	return e
}

type orderingRequest struct {
	remoteAddr   string
	flyClientIP  string
	originSecret string
	cfConnectIP  string
}

func doOrderingRequest(e *echo.Echo, r orderingRequest) int {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = r.remoteAddr
	if r.flyClientIP != "" {
		req.Header.Set("Fly-Client-Ip", r.flyClientIP)
	}
	if r.originSecret != "" {
		req.Header.Set("X-Origin-Secret", r.originSecret)
	}
	if r.cfConnectIP != "" {
		req.Header.Set("Cf-Connecting-Ip", r.cfConnectIP)
	}
	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, req)
	return rr.Code
}

// TestRateLimitOrdering_SpoofedCfConnectingIpFromNonCloudflarePeerCannotShareOrExhaustBucket
// is the behavioural regression pinned by Issue #1514. It exercises the REAL
// CloudflareOnly + IPRateLimiter chain (via registerCloudflareAndRateLimitMiddleware,
// the same helper main() calls) rather than a synthetic GateStatus injection.
//
// Scenario: an attacker who does NOT arrive via a genuine Cloudflare edge
// (invalid Fly-Client-Ip, so CloudflareOnly's Layer-1 check fails) spoofs the
// SAME Cf-Connecting-Ip value ("198.51.100.1") that a genuine, Cloudflare-verified
// victim client presents, and shares the SAME RemoteAddr as the victim
// (realistic on Fly, since RemoteAddr is Fly's fixed internal proxy address for
// both). RateLimitIP is set to 2 so a small flood exhausts a bucket quickly.
//
// With CloudflareOnly correctly registered BEFORE IPRateLimiter (today's
// order, and what this test pins): every attacker request is rejected 403 by
// CloudflareOnly BEFORE IPRateLimiter ever runs, so no token is ever consumed
// under any bucket on the attacker's behalf. The genuine victim, who DOES
// clear CloudflareOnly, is keyed on Cf-Connecting-Ip and gets a bucket
// untouched by the attacker's flood.
func TestRateLimitOrdering_SpoofedCfConnectingIpFromNonCloudflarePeerCannotShareOrExhaustBucket(t *testing.T) {
	e := buildOrderedChain(t)

	victimIdentity := "198.51.100.1" // the genuine end-client's real IP, per Cf-Connecting-Ip

	// Attacker floods well past RateLimitIP (2), spoofing the victim's identity
	// and sharing the victim's RemoteAddr. None of this should touch a real
	// bucket because CloudflareOnly rejects every one of these requests first.
	for i := 0; i < 5; i++ {
		code := doOrderingRequest(e, orderingRequest{
			remoteAddr:   flyProxyRemoteAddr,
			flyClientIP:  attackerEdgeIP, // NOT in testCFCIDR -> CloudflareOnly Layer 1 fails
			originSecret: testOriginSecret,
			cfConnectIP:  victimIdentity, // spoofed: claims to be the victim
		})
		if code != http.StatusForbidden {
			t.Fatalf("attacker request %d: got %d, want 403 (CloudflareOnly must reject a non-Cloudflare peer before the rate limiter ever runs)", i, code)
		}
	}

	// The genuine victim, who actually clears CloudflareOnly, must still be
	// able to make requests — the attacker's flood must not have exhausted (or
	// even touched) a bucket the victim shares.
	for i := 0; i < 2; i++ {
		code := doOrderingRequest(e, orderingRequest{
			remoteAddr:   flyProxyRemoteAddr, // same RemoteAddr as the attacker, on purpose
			flyClientIP:  cloudflareEdgeIP,   // genuine Cloudflare edge, in testCFCIDR
			originSecret: testOriginSecret,
			cfConnectIP:  victimIdentity,
		})
		if code != http.StatusOK {
			t.Fatalf("genuine victim request %d: got %d, want 200 — the attacker's spoofed-identity flood must not have exhausted this client's bucket", i, code)
		}
	}
}

// TestRateLimitOrdering_HelperRegistersCloudflareBeforeRateLimiter guards the
// registration order itself using the real echo.Echo router's middleware
// stack, independent of the exploit scenario above: it confirms that a
// request which would be rejected by CloudflareOnly never reaches a
// rate-limited response of any other shape (e.g. 429) — i.e. CloudflareOnly's
// decision is always the FIRST word on such a request, which is only
// possible if it runs first.
func TestRateLimitOrdering_HelperRegistersCloudflareBeforeRateLimiter(t *testing.T) {
	e := buildOrderedChain(t)

	// Drastically exceed the rate limit with valid-looking but non-Cloudflare
	// requests. If IPRateLimiter ran first, at least one of these would
	// eventually see a 429 (rate limiter engaging on the RemoteAddr fallback)
	// before/instead of CloudflareOnly's 403. If CloudflareOnly runs first (as
	// required), every single one is rejected 403, never reaching the limiter.
	for i := 0; i < 10; i++ {
		code := doOrderingRequest(e, orderingRequest{
			remoteAddr:   flyProxyRemoteAddr,
			flyClientIP:  attackerEdgeIP,
			originSecret: testOriginSecret,
			cfConnectIP:  "198.51.100.1",
		})
		if code != http.StatusForbidden {
			t.Fatalf("request %d: got %d, want 403 for every non-Cloudflare-sourced request regardless of volume", i, code)
		}
	}
}

// sanity check that the constants above stay well-formed if edited.
func TestOrderingConstants_AreValid(t *testing.T) {
	if _, err := netip.ParsePrefix(testCFCIDR); err != nil {
		t.Fatalf("testCFCIDR invalid: %v", err)
	}
	if _, err := netip.ParseAddr(cloudflareEdgeIP); err != nil {
		t.Fatalf("cloudflareEdgeIP invalid: %v", err)
	}
	if _, err := netip.ParseAddr(attackerEdgeIP); err != nil {
		t.Fatalf("attackerEdgeIP invalid: %v", err)
	}
	prefix := netip.MustParsePrefix(testCFCIDR)
	if !prefix.Contains(netip.MustParseAddr(cloudflareEdgeIP)) {
		t.Fatalf("cloudflareEdgeIP %s must be inside testCFCIDR %s", cloudflareEdgeIP, testCFCIDR)
	}
	if prefix.Contains(netip.MustParseAddr(attackerEdgeIP)) {
		t.Fatalf("attackerEdgeIP %s must be OUTSIDE testCFCIDR %s", attackerEdgeIP, testCFCIDR)
	}
}
