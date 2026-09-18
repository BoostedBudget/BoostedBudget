package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// auditProbe holds the metadata RequestAuditFrom reported for the request under test.
type auditProbe struct {
	ip        netip.Addr
	ua        string
	requestID uuid.UUID
}

// newRequestAuditEcho wires RequestAudit behind a pre-middleware that records the same gate status
// CloudflareOnly would have set, mirroring production ordering. The probe handler reads the
// metadata from the request context, which is where the service consuming it (Task 5) reads it.
func newRequestAuditEcho(gate middleware.GateStatus) (*echo.Echo, *auditProbe) {
	e := echo.New()
	probe := &auditProbe{}
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			middleware.SetGateStatus(c, gate)
			return next(c)
		}
	})
	e.Use(middleware.RequestAudit())
	e.GET("/audit-probe", func(c echo.Context) error {
		probe.ip, probe.ua, probe.requestID = middleware.RequestAuditFrom(c.Request().Context())
		return c.NoContent(http.StatusOK)
	})
	return e, probe
}

func serveAuditProbe(t *testing.T, e *echo.Echo, req *http.Request) {
	t.Helper()
	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, "probe handler must run")
}

// TestRequestAudit_CapturesSingleCfConnectingIpWhenGatePassed is the happy path: a request that
// cleared CloudflareOnly carries exactly one Cf-Connecting-Ip, and that address is recorded.
func TestRequestAudit_CapturesSingleCfConnectingIpWhenGatePassed(t *testing.T) {
	e, probe := newRequestAuditEcho(middleware.GateStatusPassed)
	req := httptest.NewRequest(http.MethodGet, "/audit-probe", nil)
	req.Header.Set("Cf-Connecting-Ip", "203.0.113.9")
	req.Header.Set("User-Agent", "probe/1.0")
	serveAuditProbe(t, e, req)

	assert.Equal(t, netip.MustParseAddr("203.0.113.9"), probe.ip)
	assert.Equal(t, "probe/1.0", probe.ua)
}

// TestRequestAudit_DuplicatedCfConnectingIpIsRefused: a duplicated header is header-injection /
// smuggling territory — the same refusal SingleValuedHeaderAddr exists for — so no IP is recorded.
func TestRequestAudit_DuplicatedCfConnectingIpIsRefused(t *testing.T) {
	e, probe := newRequestAuditEcho(middleware.GateStatusPassed)
	req := httptest.NewRequest(http.MethodGet, "/audit-probe", nil)
	req.Header.Add("Cf-Connecting-Ip", "203.0.113.9")
	req.Header.Add("Cf-Connecting-Ip", "198.51.100.1")
	serveAuditProbe(t, e, req)

	assert.False(t, probe.ip.IsValid(), "duplicated Cf-Connecting-Ip must record no IP, got %v", probe.ip)
}

// TestRequestAudit_NoIpWhenGateDidNotPass: only GateStatusPassed makes Cf-Connecting-Ip
// trustworthy. A bypass-env, exempt-path or never-wired gate must record NO IP rather than a
// plausible-but-unvalidated one.
func TestRequestAudit_NoIpWhenGateDidNotPass(t *testing.T) {
	for _, tc := range []struct {
		name string
		gate middleware.GateStatus
	}{
		{"unknown (gate never ran)", middleware.GateStatusUnknown},
		{"bypass env", middleware.GateStatusBypassEnv},
		{"exempt path", middleware.GateStatusExemptPath},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, probe := newRequestAuditEcho(tc.gate)
			req := httptest.NewRequest(http.MethodGet, "/audit-probe", nil)
			req.Header.Set("Cf-Connecting-Ip", "203.0.113.9")
			serveAuditProbe(t, e, req)

			assert.False(t, probe.ip.IsValid(), "gate %v must not yield an IP, got %v", tc.gate, probe.ip)
		})
	}
}

// TestRequestAudit_NeverUsesRemoteAddrOrFlyClientIp is the negative fixture for the IP source:
// RemoteAddr is the TCP peer (the Fly proxy) and Fly-Client-Ip is CloudflareOnly's own input. If
// the middleware ever read either, this assertion fails.
func TestRequestAudit_NeverUsesRemoteAddrOrFlyClientIp(t *testing.T) {
	e, probe := newRequestAuditEcho(middleware.GateStatusPassed)
	req := httptest.NewRequest(http.MethodGet, "/audit-probe", nil)
	req.RemoteAddr = "198.51.100.44:12345"
	req.Header.Set("Fly-Client-Ip", "198.51.100.55")
	serveAuditProbe(t, e, req)

	assert.False(t, probe.ip.IsValid(),
		"IP must come ONLY from Cf-Connecting-Ip; got %v (RemoteAddr=%q, Fly-Client-Ip=%q)",
		probe.ip, req.RemoteAddr, req.Header.Get("Fly-Client-Ip"))
}

// TestRequestAudit_MalformedRequestIdIsReplacedByServerUuid is the negative fixture for the
// request ID: a non-UUID X-Request-ID must never be propagated into the UUID column.
func TestRequestAudit_MalformedRequestIdIsReplacedByServerUuid(t *testing.T) {
	e, probe := newRequestAuditEcho(middleware.GateStatusPassed)
	req := httptest.NewRequest(http.MethodGet, "/audit-probe", nil)
	req.Header.Set("X-Request-ID", "not-a-uuid")
	serveAuditProbe(t, e, req)

	assert.NotEqual(t, uuid.Nil, probe.requestID, "a server-generated UUID is required")
	assert.NotEqual(t, "not-a-uuid", probe.requestID.String(), "malformed input must never be propagated")
}

// TestRequestAudit_AbsentRequestIdGetsServerUuid: an absent header also yields a server-generated
// UUID (never uuid.Nil, which the INSERT would bind as SQL NULL).
func TestRequestAudit_AbsentRequestIdGetsServerUuid(t *testing.T) {
	e, probe := newRequestAuditEcho(middleware.GateStatusPassed)
	req := httptest.NewRequest(http.MethodGet, "/audit-probe", nil)
	serveAuditProbe(t, e, req)

	assert.NotEqual(t, uuid.Nil, probe.requestID)
}

// TestRequestAudit_ValidRequestIdIsPropagated: a well-formed header is preserved for cross-service
// tracing.
func TestRequestAudit_ValidRequestIdIsPropagated(t *testing.T) {
	id := uuid.New()
	e, probe := newRequestAuditEcho(middleware.GateStatusPassed)
	req := httptest.NewRequest(http.MethodGet, "/audit-probe", nil)
	req.Header.Set("X-Request-ID", id.String())
	serveAuditProbe(t, e, req)

	assert.Equal(t, id, probe.requestID)
}

// TestRequestAuditFrom_WithoutMiddlewareIsZeroSafe pins the "middleware never ran" contract: the
// accessor returns zero values rather than panicking, so a caller that reads it unconditionally
// cannot crash a request.
func TestRequestAuditFrom_WithoutMiddlewareIsZeroSafe(t *testing.T) {
	ip, ua, requestID := middleware.RequestAuditFrom(httptest.NewRequest(http.MethodGet, "/x", nil).Context())
	assert.False(t, ip.IsValid())
	assert.Empty(t, ua)
	assert.Equal(t, uuid.Nil, requestID)
}
