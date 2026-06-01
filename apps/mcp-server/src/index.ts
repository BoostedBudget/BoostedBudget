/**
 * BoostedBudget MCP Server — Cloudflare Worker entry point.
 *
 * Uses @cloudflare/workers-oauth-provider to act as a full OAuth 2.1 AS,
 * supporting Dynamic Client Registration (RFC 7591) so Claude Code and other
 * MCP clients can connect without pre-registration.
 *
 * Auth flow:
 *   1. MCP client does DCR at /oauth/register (handled by OAuthProvider)
 *   2. MCP client redirects user to /oauth/authorize (our handler → Clerk)
 *   3. Clerk authenticates the user, redirects to /oauth/callback
 *   4. /oauth/callback exchanges code with Clerk, completes authorization
 *   5. OAuthProvider issues its own access token to the MCP client
 *   6. Authenticated requests reach McpApiHandler → BoostedBudgetMcpAgent DO
 *
 * The Worker NEVER touches the database. It calls the Go backend
 * (api.boostedbudget.com / staging-api.boostedbudget.com) via Orval client.
 */

import { OAuthProvider, getOAuthApi } from '@cloudflare/workers-oauth-provider'
import type { OAuthProviderOptions } from '@cloudflare/workers-oauth-provider'
import { handleHealth } from './health'
import { handleAuthorize, handleCallback } from './oauth/authorize-handler'
import { McpApiHandler } from './mcp/mcp-api-handler'
import { BoostedBudgetMcpAgent } from './mcp/agent'
import { parseEnv } from './env'
import type { Env } from './env'

export { BoostedBudgetMcpAgent }
export { McpApiHandler }

const oauthOptions: OAuthProviderOptions<Env> = {
  // /mcp and /sse routes are authenticated; all other routes go to defaultHandler
  apiRoute: ['/mcp', '/sse'],
  apiHandler: McpApiHandler,

  defaultHandler: {
    async fetch(request: Request, rawEnv: Env): Promise<Response> {
      try {
        parseEnv(rawEnv)
      } catch (err) {
        console.error('[mcp-startup] env validation error:', err instanceof Error ? err.message : err)
        return new Response(JSON.stringify({ error: 'misconfigured' }), {
          status: 503,
          headers: { 'Content-Type': 'application/json' },
        })
      }

      const url = new URL(request.url)

      if (request.method === 'OPTIONS') {
        return new Response(null, {
          status: 204,
          headers: { 'Access-Control-Allow-Origin': '*', 'Access-Control-Expose-Headers': 'WWW-Authenticate' },
        })
      }

      if (url.pathname === '/health') {
        return handleHealth()
      }

      // OAuth authorization UI — redirects user to Clerk for authentication
      if (url.pathname === '/oauth/authorize') {
        const oauthHelpers = getOAuthApi(oauthOptions, rawEnv as Env)
        return handleAuthorize(request, rawEnv as Env, oauthHelpers)
      }

      // Clerk redirects here after user authenticates
      if (url.pathname === '/oauth/callback') {
        const oauthHelpers = getOAuthApi(oauthOptions, rawEnv as Env)
        return handleCallback(request, rawEnv as Env, oauthHelpers)
      }

      return new Response(JSON.stringify({ error: 'not_found' }), {
        status: 404,
        headers: { 'Content-Type': 'application/json' },
      })
    },
  },

  // OAuth endpoints — all implemented by the library
  authorizeEndpoint: '/oauth/authorize',
  tokenEndpoint: '/oauth/token',
  clientRegistrationEndpoint: '/oauth/register',

  scopesSupported: ['openid', 'email', 'profile'],
  // resource auto-detected from request origin (staging vs prod handled automatically)
}

export default new OAuthProvider<Env>(oauthOptions)
