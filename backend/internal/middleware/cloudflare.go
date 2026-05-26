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

// exemptedPaths bypass CloudflareOnly (/health for Fly.io health check, /metrics for Prometheus).
var exemptedPaths = map[string]bool{
	"/health":  true,
	"/metrics": true,
}

// bypassEnvironments skip Cloudflare checks (development/local/test).
var bypassEnvironments = map[string]bool{
	"development": true,
	"local":       true,
	"test":        true,
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

// StartCIDRRefresher periodically refreshes Cloudflare CIDR ranges until ctx is cancelled.
func StartCIDRRefresher(ctx context.Context, interval time.Duration, ref *atomic.Pointer[[]netip.Prefix]) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fresh := LoadCloudflareCIDRs(ctx)
			ref.Store(&fresh)
			slog.InfoContext(ctx, "refreshed Cloudflare IP ranges", "count", len(fresh))
		}
	}
}

// CloudflareOnly enforces dual-layer protection: (1) Fly-Client-IP must be in Cloudflare ranges,
// (2) X-CF-Secret header must match originSecret (constant-time).
// Bypassed in development/local/test and for /health, /metrics paths.
func CloudflareOnly(ref *atomic.Pointer[[]netip.Prefix], originSecret string, env string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Bypass for local/development/test environments.
			if bypassEnvironments[env] {
				return next(c)
			}

			// Bypass exempted paths (/health, /metrics).
			if exemptedPaths[c.Request().URL.Path] {
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

			// Layer 2: Validate X-CF-Secret header (fail-safe if empty)
			if originSecret == "" {
				slog.WarnContext(r.Context(), "cloudflare-only: blocked — CF_ORIGIN_SECRET not configured",
					"path", r.URL.Path)
				return c.String(http.StatusForbidden, "Forbidden")
			}

			// Reject if zero or multiple X-CF-Secret headers are present.
			secretValues := r.Header["X-Cf-Secret"]
			if len(secretValues) != 1 {
				slog.WarnContext(r.Context(), "cloudflare-only: blocked — unexpected X-CF-Secret header count",
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
