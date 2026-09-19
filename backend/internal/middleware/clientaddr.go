package middleware

import (
	"net/http"
	"net/netip"
	"strings"
)

// SingleValuedHeaderAddr returns the canonicalized address carried by a header that is only
// trustworthy when it appears exactly once and parses as a valid IP. Zero occurrences, more than
// one occurrence (possible header-injection / smuggling), and unparseable values all report
// ok=false rather than handing back an opaque string to be used as a bucket key or an audit value.
//
// Moved here (and exported) from rate_limit.go by #1683 Task 4 so the request-audit middleware can
// share the exact same validation the rate limiter relies on. Behaviour is unchanged: the address
// is unmapped (IPv4-mapped IPv6 collapses to IPv4) before being returned.
func SingleValuedHeaderAddr(r *http.Request, canonicalHeader string) (netip.Addr, bool) {
	values := r.Header[canonicalHeader]
	if len(values) != 1 {
		return netip.Addr{}, false
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(values[0]))
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}
