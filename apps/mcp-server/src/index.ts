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

import { type Env, type ParsedEnv, parseEnv } from './env'
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
  async fetch(request: Request, rawEnv: Env, ctx: ExecutionContext): Promise<Response> {
    let env: ParsedEnv
    try {
      env = parseEnv(rawEnv)
    } catch (err) {
      const message = err instanceof Error ? err.message : 'env validation failed'
      console.error('[mcp-startup] env validation error:', message)
      return new Response(JSON.stringify({ error: 'misconfigured' }), {
        status: 503,
        headers: { 'Content-Type': 'application/json' },
      })
    }
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
    let rawAccessToken: string
    try {
      rawAccessToken = ClerkTokenVerifier.extractBearerToken(
        request.headers.get('Authorization') ?? undefined,
      )
      authCtx = await verifyClerkToken(rawAccessToken, env)
      await assertNotRevoked(authCtx, env)
    } catch (err) {
      // Log auth failures for operational visibility (Workers Logs enabled in wrangler.toml).
      // Do NOT include the raw token or sensitive payload in the log — just the error type.
      const errName = err instanceof Error ? err.constructor.name : typeof err
      console.error(`[mcp-auth] token verification failed: ${errName}`, {
        path: url.pathname,
        method: request.method,
      })
      return unauthorizedResponse(env.MCP_RESOURCE_URL, 'invalid_token')
    }

    // ── Inject trusted auth headers — shared by both MCP transports ─────────
    // Build headers from scratch — delete then set to prevent injection/duplicates.
    // The DO is only reachable through this Worker (never directly exposed).
    const forwardHeaders = new Headers(request.headers)
    // Clear any existing MCP headers
    for (const key of ['X-MCP-Clerk-User-Id', 'X-MCP-Grant-Id', 'X-MCP-Access-Token', 'X-MCP-Origin', 'X-MCP-Worker-Secret']) {
      forwardHeaders.delete(key)
    }
    // Set trusted headers from verified auth context
    forwardHeaders.set('X-MCP-Clerk-User-Id', authCtx.clerkUserId)
    forwardHeaders.set('X-MCP-Grant-Id', authCtx.grantId)
    forwardHeaders.set('X-MCP-Access-Token', rawAccessToken!)
    forwardHeaders.set('X-MCP-Origin', '1')
    forwardHeaders.set('X-MCP-Worker-Secret', env.MCP_WORKER_SECRET)
    const authedRequest = new Request(request, { headers: forwardHeaders })

    // ── MCP Streamable HTTP transport (primary) ────────────────────────────
    if (url.pathname === '/mcp' || url.pathname.startsWith('/mcp/')) {
      const mcpHandler = BoostedBudgetMcpAgent.serve('/mcp', {
        binding: 'MCP_AGENT',
      })
      return mcpHandler.fetch(authedRequest, env as Parameters<typeof mcpHandler.fetch>[1], ctx)
    }

    // ── MCP SSE transport (legacy clients that don't support Streamable HTTP) ─
    // Served at /sse; the DO session is the same BoostedBudgetMcpAgent.
    if (url.pathname === '/sse' || url.pathname.startsWith('/sse/')) {
      const sseHandler = BoostedBudgetMcpAgent.serveSSE('/sse', {
        binding: 'MCP_AGENT',
      })
      return sseHandler.fetch(authedRequest, env as Parameters<typeof sseHandler.fetch>[1], ctx)
    }

    return new Response(JSON.stringify({ error: 'not_found' }), {
      status: 404,
      headers: { 'Content-Type': 'application/json' },
    })
  },
}

export type { Env }
