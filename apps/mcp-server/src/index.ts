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

export default {
  async fetch(request: Request, rawEnv: Env): Promise<Response> {
    // Validate environment bindings eagerly so misconfigured deployments
    // fail loudly rather than producing mysterious runtime errors.
    parseEnv(rawEnv)

    const url = new URL(request.url)

    // ── Health check (no auth) ─────────────────────────────────────────────
    if (url.pathname === '/health' && request.method === 'GET') {
      return handleHealth()
    }

    // TODO Phase 3: OAuth protected-resource metadata
    // TODO Phase 3: OAuth authorization-server metadata
    // TODO Phase 4: McpAgent dispatch (Durable Object)
    // TODO Phase 8: Sentry/tracing error boundary

    // Catch-all for unimplemented routes
    return new Response(JSON.stringify({ error: 'not_found' }), {
      status: 404,
      headers: { 'Content-Type': 'application/json' },
    })
  },
}

export type { Env }
