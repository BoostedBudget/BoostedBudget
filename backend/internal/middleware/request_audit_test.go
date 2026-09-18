package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
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

// TestRequestAudit_ValidRequestIdIsPropagated: a well-formed client header is preserved for
// cross-service tracing. RequestID is wired FIRST (production order), since RequestAudit reads the
// ID RequestID established rather than the raw request header.
func TestRequestAudit_ValidRequestIdIsPropagated(t *testing.T) {
	id := uuid.New()
	e := echo.New()
	var got uuid.UUID
	e.Use(middleware.RequestID())
	e.Use(middleware.RequestAudit())
	e.GET("/audit-probe", func(c echo.Context) error {
		_, _, got = middleware.RequestAuditFrom(c.Request().Context())
		return c.NoContent(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/audit-probe", nil)
	req.Header.Set("X-Request-ID", id.String())
	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	assert.Equal(t, id, got)
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

// TestRequestAudit_CorrelatesWithRequestIDMiddleware is the correlation guard: the pre-existing
// RequestID middleware (installed FIRST, main.go:300) establishes the request ID that every
// http_request log line and the response X-Request-ID header carry. RequestAudit must record THAT
// ID, not a second independent one, or an operator grepping audit_logs.request_id by the response's
// ID finds nothing. Here the request carries no X-Request-ID header, which is exactly the case
// where the old code diverged.
func TestRequestAudit_CorrelatesWithRequestIDMiddleware(t *testing.T) {
	e := echo.New()
	var auditID uuid.UUID
	var obsID uuid.UUID
	e.Use(middleware.RequestID())
	e.Use(middleware.RequestAudit())
	e.GET("/audit-probe", func(c echo.Context) error {
		_, _, auditID = middleware.RequestAuditFrom(c.Request().Context())
		obsID, _ = uuid.Parse(observability.RequestIDFromContext(c.Request().Context()))
		return c.NoContent(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/audit-probe", nil)
	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	require.NotEqual(t, uuid.Nil, obsID, "RequestID middleware must have generated a UUID")
	assert.Equal(t, obsID, auditID,
		"audit request_id must equal the request's established ID; a second independent UUID defeats "+
			"log/audit correlation")
}

// TestRequestAudit_MalformedHeaderWithRequestIDStillYieldsServerUuid keeps the never-propagate rule
// intact when RequestID is wired: RequestID stores the raw malformed header, and RequestAudit must
// refuse it rather than copy a non-UUID into the UUID column.
func TestRequestAudit_MalformedHeaderWithRequestIDStillYieldsServerUuid(t *testing.T) {
	e := echo.New()
	var auditID uuid.UUID
	var obsRaw string
	e.Use(middleware.RequestID())
	e.Use(middleware.RequestAudit())
	e.GET("/audit-probe", func(c echo.Context) error {
		_, _, auditID = middleware.RequestAuditFrom(c.Request().Context())
		obsRaw = observability.RequestIDFromContext(c.Request().Context())
		return c.NoContent(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/audit-probe", nil)
	req.Header.Set("X-Request-ID", "not-a-uuid")
	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "not-a-uuid", obsRaw, "fixture: RequestID must have stored the raw header")
	assert.NotEqual(t, uuid.Nil, auditID, "a server-generated UUID is required")
	assert.NotEqual(t, "not-a-uuid", auditID.String(), "malformed input must never be propagated")
}
