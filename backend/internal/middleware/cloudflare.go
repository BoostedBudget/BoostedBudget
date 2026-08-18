package middleware

import (
	"bufio"
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
	"time"

	"github.com/labstack/echo/v4"
)

const (
	cloudflareIPv4URL = "https://www.cloudflare.com/ips-v4"
	cloudflareIPv6URL = "https://www.cloudflare.com/ips-v6"
	cidrFetchTimeout  = 5 * time.Second
	cidrRefreshEvery  = 24 * time.Hour
)

// GateStatus records how a request cleared (or bypassed) CloudflareOnly, so that
// downstream middleware — specifically the rate limiter — can decide which client
// identity is trustworthy for THIS request. CloudflareOnly is the only place that
// knows which of these paths a request took; it must record that before calling
// next(), because everything downstream sees the same echo.Context.
type GateStatus int

const (
	// GateStatusUnknown means CloudflareOnly did not run (e.g. not registered in
	// this middleware chain, such as a test that wires IPRateLimiter alone).
	// Callers MUST treat this the same as GateStatusBypassEnv: trust nothing but
	// the raw transport peer address.
	GateStatusUnknown GateStatus = iota
	// GateStatusBypassEnv means the request skipped CloudflareOnly entirely because
	// bypassEnvironments[env] is true (development/local/test/qa).
	GateStatusBypassEnv
	// GateStatusExemptPath means the request hit an exemptedPaths route (/health and
	// /health/ — /metrics was removed from exemptedPaths in #1531; see that map's own
	// doc comment for why) and skipped CloudflareOnly's IP/secret checks. Unlike
	// GateStatusPassed, no header on this request has been validated.
	GateStatusExemptPath
	// GateStatusPassed means the request's immediate peer was verified to be in
	// Cloudflare's published CIDR ranges AND presented the correct X-Origin-Secret.
	GateStatusPassed
)

// gateStatusContextKey is the echo.Context key CloudflareOnly uses to publish
// GateStatus for downstream middleware.
const gateStatusContextKey = "cf_gate_status"

// SetGateStatus records how a request cleared CloudflareOnly. Exported so tests in
// other files (e.g. rate_limit_test.go) can simulate a given gate outcome without
// wiring the full CloudflareOnly middleware.
func SetGateStatus(c echo.Context, s GateStatus) {
	c.Set(gateStatusContextKey, s)
}

// GateStatusFrom reads the GateStatus set by CloudflareOnly for this request. If
// CloudflareOnly never ran, this returns GateStatusUnknown.
func GateStatusFrom(c echo.Context) GateStatus {
	s, _ := c.Get(gateStatusContextKey).(GateStatus)
	return s
}

// exemptedPaths bypass BOTH CloudflareOnly's origin checks and (see IPRateLimiter in
// rate_limit.go) the rate limiter's primary backend. It is a single shared list, read only
// through isExemptPath, so the two call sites cannot drift apart — issue #1531 asked for this
// explicitly. Includes trailing-slash variants because some health checkers and monitoring probes
// append a trailing slash.
//
// ⚠ /metrics and /metrics/ are deliberately NOT here (they were before #1531). Since #1514,
// /metrics is served only on the internal listener (cmd/server/internal_metrics.go) and is not
// registered on the public Echo instance at all — leaving it "exempt" here would be a latent
// fail-open trap if anyone ever re-registers that route publicly: it would bypass both the
// Cloudflare gate and (transitively) the primary rate-limit backend with no code change to this
// file at all.
var exemptedPaths = map[string]bool{
	"/health":  true,
	"/health/": true,
}

// isExemptPath reports whether path bypasses CloudflareOnly's origin checks and the rate
// limiter's primary backend. See the exemptedPaths doc comment for why this is the one place both
// callers read from.
func isExemptPath(path string) bool {
	return exemptedPaths[path]
}

// bypassEnvironments skip Cloudflare checks (development/local/test/qa).
// qa is used by Playwright E2E tests which make direct localhost calls without Cloudflare headers.
var bypassEnvironments = map[string]bool{
	"development": true,
	"local":       true,
	"test":        true,
	"qa":          true,
}

// LoadCloudflareCIDRs fetches Cloudflare IP ranges at startup, falling back to hardcoded list on error.
func LoadCloudflareCIDRs(ctx context.Context) []netip.Prefix {
	return LoadCloudflareCIDRsFrom(ctx, cloudflareIPv4URL, cloudflareIPv6URL)
}

// LoadCloudflareCIDRsFrom fetches Cloudflare IP ranges (exported for testing).
func LoadCloudflareCIDRsFrom(ctx context.Context, ipv4URL, ipv6URL string) []netip.Prefix {
	fetched, err := fetchCIDRs(ctx, ipv4URL, ipv6URL)
	if err != nil {
		slog.WarnContext(ctx, "cloudflare IP fetch failed — using hardcoded fallback",
			"error", err)
		return CloudflareFallbackCIDRs()
	}
	slog.InfoContext(ctx, "loaded Cloudflare IP ranges", "count", len(fetched))
	return fetched
}

// StartCIDRRefresher periodically refreshes Cloudflare CIDR ranges using official URLs.
func StartCIDRRefresher(ctx context.Context, interval time.Duration, ref *atomic.Pointer[[]netip.Prefix]) {
	StartCIDRRefresherFrom(ctx, interval, ref, cloudflareIPv4URL, cloudflareIPv6URL)
}

// StartCIDRRefresherFrom is like StartCIDRRefresher but accepts custom URLs (for test injection).
func StartCIDRRefresherFrom(ctx context.Context, interval time.Duration, ref *atomic.Pointer[[]netip.Prefix], ipv4URL, ipv6URL string) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fresh := LoadCloudflareCIDRsFrom(ctx, ipv4URL, ipv6URL)
			ref.Store(&fresh)
			slog.InfoContext(ctx, "refreshed Cloudflare IP ranges", "count", len(fresh))
		}
	}
}

// CloudflareOnly enforces dual-layer protection: (1) Fly-Client-IP must be in Cloudflare ranges,
// (2) X-Origin-Secret header must match originSecret (constant-time).
// Bypassed in development/local/test/qa environments (see bypassEnvironments) and for /health,
// /health/ paths (see exemptedPaths). /metrics was removed from exemptedPaths in #1531 — see that
// var's doc comment for why.
func CloudflareOnly(ref *atomic.Pointer[[]netip.Prefix], originSecret string, env string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Bypass for local/development/test environments.
			if bypassEnvironments[env] {
				SetGateStatus(c, GateStatusBypassEnv)
				return next(c)
			}

			// Bypass exempted paths (/health).
			if isExemptPath(c.Request().URL.Path) {
				SetGateStatus(c, GateStatusExemptPath)
				return next(c)
			}

			r := c.Request()

			// Layer 1: Validate Fly-Client-IP is in Cloudflare ranges
			flyClientIP := r.Header.Get("Fly-Client-Ip")
			if flyClientIP == "" {
				slog.WarnContext(r.Context(), "cloudflare-only: blocked — missing Fly-Client-IP",
					"path", r.URL.Path,
					"method", r.Method)
				return c.String(http.StatusForbidden, "Forbidden")
			}

			addr, err := netip.ParseAddr(flyClientIP)
			if err != nil {
				slog.WarnContext(r.Context(), "cloudflare-only: blocked — malformed Fly-Client-IP",
					"path", r.URL.Path,
					"fly_client_ip", flyClientIP)
				return c.String(http.StatusForbidden, "Forbidden")
			}

			// Normalize IPv4-mapped IPv6 addresses for CIDR matching
			addr = addr.Unmap()

			cidrs := *ref.Load()
			if !isCloudflareIP(addr, cidrs) {
				slog.WarnContext(r.Context(), "cloudflare-only: blocked — IP not in Cloudflare ranges",
					"path", r.URL.Path,
					"fly_client_ip", flyClientIP)
				return c.String(http.StatusForbidden, "Forbidden")
			}

			// Layer 2: Validate X-Origin-Secret header (fail-safe if empty)
			if originSecret == "" {
				slog.WarnContext(r.Context(), "cloudflare-only: blocked — CF_ORIGIN_SECRET not configured",
					"path", r.URL.Path)
				return c.String(http.StatusForbidden, "Forbidden")
			}

			// Reject if zero or multiple X-Origin-Secret headers are present.
			secretValues := r.Header["X-Origin-Secret"]
			if len(secretValues) != 1 {
				slog.WarnContext(r.Context(), "cloudflare-only: blocked — unexpected X-Origin-Secret header count",
					"path", r.URL.Path,
					"count", len(secretValues))
				return c.String(http.StatusForbidden, "Forbidden")
			}

			// Constant-time comparison to prevent timing attacks
			if subtle.ConstantTimeCompare([]byte(secretValues[0]), []byte(originSecret)) != 1 {
				slog.WarnContext(r.Context(), "cloudflare-only: blocked — invalid origin secret",
					"path", r.URL.Path,
					"fly_client_ip", flyClientIP)
				// Do NOT log the secret value — not even the received one.
				return c.String(http.StatusForbidden, "Forbidden")
			}

			SetGateStatus(c, GateStatusPassed)
			return next(c)
		}
	}
}

// isCloudflareIP reports whether addr is contained in any of the given CIDR prefixes.
func isCloudflareIP(addr netip.Addr, cidrs []netip.Prefix) bool {
	for _, cidr := range cidrs {
		if cidr.Contains(addr) {
			return true
		}
	}
	return false
}

// fetchCIDRs fetches and validates IPv4/IPv6 CIDR lists from the given URLs.
func fetchCIDRs(ctx context.Context, ipv4URL, ipv6URL string) ([]netip.Prefix, error) {
	client := &http.Client{Timeout: cidrFetchTimeout}

	v4Lines, err := fetchLines(ctx, client, ipv4URL)
	if err != nil {
		return nil, err
	}

	v6Lines, err := fetchLines(ctx, client, ipv6URL)
	if err != nil {
		return nil, err
	}

	all := append(v4Lines, v6Lines...)
	parsed := parseCIDRStrings(all)

	if len(parsed) == 0 {
		return nil, &fetchValidationError{}
	}

	return parsed, nil
}

// fetchLines fetches a URL and returns its non-empty, non-comment lines.
func fetchLines(ctx context.Context, client *http.Client, url string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &fetchHTTPError{url: url, status: resp.StatusCode}
	}

	var lines []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines, scanner.Err()
}

type fetchValidationError struct{}

func (*fetchValidationError) Error() string {
	return "fetched CIDR list was empty or contained unparseable entries"
}

type fetchHTTPError struct {
	url    string
	status int
}

func (*fetchHTTPError) Error() string {
	return "unexpected HTTP status fetching Cloudflare IPs"
}
