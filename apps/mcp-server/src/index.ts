/**
 * BoostedBudget MCP Server — Cloudflare Worker entry point.
 *
 * Phases:
 *   0   — Scaffold: env validation + /health
 *   3   — OAuth resource-server metadata + Clerk token verification
 *   4   — MCP agent (search tool, sandboxed)
 *   5   — Execute reads via host-proxy
 *   6b  — Writes (confirmation-gated)
 *   7   — Read-only recipes
 *   8   — PostHog + hardening
 *
 * The Worker NEVER touches the database. It calls the Go backend
 * (api.boostedbudget.com) via typed Orval client. The backend is
 * the single gatekeeper for auth, scoping, and validation.
 */

import { type Env, parseEnv } from './env'
import { handleHealth } from './health'
import {
  buildProtectedResourceMetadata,
  unauthorizedResponse,
} from './oauth/resource-server'
import { ClerkTokenVerifier, verifyClerkToken } from './oauth/clerk-verifier'
import { assertNotRevoked } from './oauth/revocation'
import { BoostedBudgetMcpAgent } from './mcp/agent'

export { BoostedBudgetMcpAgent }

const CORS_HEADERS = {
  'Access-Control-Allow-Origin': '*',
  'Access-Control-Expose-Headers': 'WWW-Authenticate',
}

export default {
  async fetch(request: Request, rawEnv: Env): Promise<Response> {
    const env = parseEnv(rawEnv)
    const url = new URL(request.url)

    // ── CORS preflight ─────────────────────────────────────────────────────
    if (request.method === 'OPTIONS') {
      return new Response(null, { status: 204, headers: CORS_HEADERS })
    }

    // ── Health check (no auth) ─────────────────────────────────────────────
    if (url.pathname === '/health' && request.method === 'GET') {
      return handleHealth()
    }

    // ── OAuth protected-resource metadata (no auth required, RFC 9728) ────
    if (
      url.pathname === '/.well-known/oauth-protected-resource' ||
      url.pathname === '/.well-known/oauth-protected-resource/mcp'
    ) {
      return new Response(JSON.stringify(buildProtectedResourceMetadata(env)), {
        status: 200,
        headers: { ...CORS_HEADERS, 'Content-Type': 'application/json' },
      })
    }

    // ── All other routes require a valid Clerk OAuth token ─────────────────
    let authCtx
    try {
      const token = ClerkTokenVerifier.extractBearerToken(
        request.headers.get('Authorization') ?? undefined,
      )
      authCtx = await verifyClerkToken(token, env)
      await assertNotRevoked(authCtx, env)
    } catch {
      return unauthorizedResponse(env.MCP_RESOURCE_URL, 'invalid_token')
    }

    // ── MCP endpoint — route to BoostedBudgetMcpAgent Durable Object ──────────
    if (url.pathname === '/mcp' || url.pathname.startsWith('/mcp/')) {
      // Inject auth context as trusted internal headers before routing to the DO.
      // The DO is only reachable through this Worker (never directly exposed),
      // so these headers are trustworthy.
      const authedRequest = new Request(request, {
        headers: new Headers({
          ...Object.fromEntries(request.headers.entries()),
          'X-MCP-Clerk-User-Id': authCtx.clerkUserId,
          'X-MCP-Grant-Id': authCtx.grantId,
        }),
      })

      const mcpHandler = BoostedBudgetMcpAgent.serve('/mcp', {
        binding: 'MCP_AGENT',
      })
      return mcpHandler.fetch(authedRequest, env as Parameters<typeof mcpHandler.fetch>[1], {} as ExecutionContext)
    }

    // TODO Phase 8: Sentry/tracing error boundary

    return new Response(JSON.stringify({ error: 'not_found' }), {
      status: 404,
      headers: { 'Content-Type': 'application/json' },
    })
  },
}

export type { Env }
