package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/labstack/echo/v4"
)

// Issue #1569 Task 19 — which router actually serves these routes, measured rather than argued.
//
// WHY THIS EXISTS. Task 14 moved GET /api/v1/boosts/executions to the hyphenated sibling
// /api/v1/boost-executions after observing the literal segment being swallowed by the
// /api/v1/boosts/{id} wildcard, and recorded the cause as "huma v2's humaflow router". Production
// does not use that router: cmd/server/main.go builds the API with humaecho.NewV4, i.e. Echo v4's
// radix tree, which resolves static before param. The observation was real; it happened inside
// humatest.
//
// WHAT THIS MEASURES, and the numbers are from this test, not from reasoning:
//
//	humatest, wildcard registered FIRST   -> the WILDCARD wins; the literal route is unreachable
//	humatest, literal  registered FIRST   -> the literal wins
//	humaecho (Echo v4), either order      -> the LITERAL always wins
//
// So humatest is registration-order sensitive and Echo v4 is not. Two consequences, and the second
// is the one that costs time:
//
//  1. A literal route registered after a {param} sibling is NOT dead in production. Task 14's
//     endpoint would have worked; the hyphenated sibling is a fine convention but it was not a fix.
//  2. A router-level test written with humatest can raise a FALSE dead-route alarm — and, in the
//     other registration order, can give a false ALL-CLEAR that production would not have needed
//     anyway. Any future guard over {param} siblings has to be built on the production adapter, or
//     it is measuring the harness.
//
// This test is the citation for both claims. If either router changes its precedence, it goes red
// here rather than in a comment nobody re-derives.

type routerProbeOutput struct {
	Body struct {
		Who string `json:"who"`
	}
}

func registerRouterProbeWildcard(api huma.API, hit *string) {
	huma.Register(api, huma.Operation{
		OperationID: "routerProbeByID", Method: http.MethodGet, Path: "/api/v1/things/{id}",
	}, func(_ context.Context, in *struct {
		ID string `path:"id"`
	}) (*routerProbeOutput, error) {
		*hit = "wildcard:" + in.ID
		out := &routerProbeOutput{}
		out.Body.Who = "wildcard"
		return out, nil
	})
}

func registerRouterProbeLiteral(api huma.API, hit *string) {
	huma.Register(api, huma.Operation{
		OperationID: "routerProbeLiteral", Method: http.MethodGet, Path: "/api/v1/things/executions",
	}, func(_ context.Context, _ *struct{}) (*routerProbeOutput, error) {
		*hit = "literal"
		out := &routerProbeOutput{}
		out.Body.Who = "literal"
		return out, nil
	})
}

// echoProbe dispatches one GET through the SAME adapter main.go uses.
func echoProbe(t *testing.T, register func(huma.API, *string)) string {
	t.Helper()
	var hit string
	e := echo.New()
	api := humaecho.NewV4(e, huma.DefaultConfig("router-precedence-probe", "1.0.0"))
	register(api, &hit)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/things/executions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("probe request failed with %d: %s — the probe must reach A handler, or 'which one' "+
			"is not what is being measured", rec.Code, rec.Body.String())
	}
	return hit
}

func humatestProbe(t *testing.T, register func(huma.API, *string)) string {
	t.Helper()
	var hit string
	_, api := humatest.New(t)
	register(api, &hit)

	resp := api.Get("/api/v1/things/executions")
	if resp.Code != http.StatusOK {
		t.Fatalf("probe request failed with %d: %s", resp.Code, resp.Body.String())
	}
	return hit
}

// TestRouterPrecedence_ProductionEchoAlwaysPrefersTheLiteralSegment is the claim that matters:
// nothing registered against the real adapter is shadowed by a {param} sibling, whichever order the
// two are registered in.
func TestRouterPrecedence_ProductionEchoAlwaysPrefersTheLiteralSegment(t *testing.T) {
	wildcardFirst := echoProbe(t, func(api huma.API, hit *string) {
		registerRouterProbeWildcard(api, hit)
		registerRouterProbeLiteral(api, hit)
	})
	if wildcardFirst != "literal" {
		t.Errorf("Echo v4 dispatched /things/executions to %q with the wildcard registered first; "+
			"static-before-param no longer holds and every literal/{param} sibling in the tree needs "+
			"re-checking", wildcardFirst)
	}

	literalFirst := echoProbe(t, func(api huma.API, hit *string) {
		registerRouterProbeLiteral(api, hit)
		registerRouterProbeWildcard(api, hit)
	})
	if literalFirst != "literal" {
		t.Errorf("Echo v4 dispatched to %q with the literal registered first", literalFirst)
	}
}

// TestRouterPrecedence_HumatestIsRegistrationOrderSensitive pins the divergence itself. Without it,
// the next person writing a router-level test with humatest reads its verdict as production's.
func TestRouterPrecedence_HumatestIsRegistrationOrderSensitive(t *testing.T) {
	wildcardFirst := humatestProbe(t, func(api huma.API, hit *string) {
		registerRouterProbeWildcard(api, hit)
		registerRouterProbeLiteral(api, hit)
	})
	if wildcardFirst != "wildcard:executions" {
		t.Errorf("humatest dispatched to %q with the wildcard registered first; it used to swallow the "+
			"literal, and Task 14's comment plus this file's header are written against that "+
			"behaviour. If humatest now matches Echo, say so in both places", wildcardFirst)
	}

	literalFirst := humatestProbe(t, func(api huma.API, hit *string) {
		registerRouterProbeLiteral(api, hit)
		registerRouterProbeWildcard(api, hit)
	})
	if literalFirst != "literal" {
		t.Errorf("humatest dispatched to %q with the literal registered first", literalFirst)
	}
}
