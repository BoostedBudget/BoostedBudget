import { WorkerEntrypoint } from 'cloudflare:workers'
import { BoostedBudgetMcpAgent } from './agent'
import type { Env } from '../env'

/**
 * McpApiHandler — WorkerEntrypoint that bridges OAuthProvider to BoostedBudgetMcpAgent.
 *
 * workers-oauth-provider validates the access token and calls this handler's fetch()
 * with ctx.props = { clerkUserId, accessToken } (stored when completing authorization).
 * We inject those as trusted X-MCP-* headers before forwarding to the Durable Object.
 *
 * The DO only receives requests from this WorkerEntrypoint — it is never directly exposed.
 */
export class McpApiHandler extends WorkerEntrypoint<Env> {
  async fetch(request: Request): Promise<Response> {
    const { clerkUserId, accessToken } = this.ctx.props as {
      clerkUserId: string
      accessToken: string
    }

    // Build a clean set of headers — strip any inbound X-MCP-* headers then set
    // trusted values from the authenticated props. This prevents clients from
    // injecting their own MCP headers.
    const headers = new Headers(request.headers)
    for (const key of [
      'X-MCP-Clerk-User-Id',
      'X-MCP-Grant-Id',
      'X-MCP-Access-Token',
      'X-MCP-Origin',
      'X-MCP-Worker-Secret',
    ]) {
      headers.delete(key)
    }
    headers.set('X-MCP-Clerk-User-Id', clerkUserId)
    headers.set('X-MCP-Access-Token', accessToken)
    headers.set('X-MCP-Worker-Secret', this.env.MCP_WORKER_SECRET)
    headers.set('X-MCP-Origin', '1')

    const authedRequest = new Request(request, { headers })
    const url = new URL(request.url)

    if (url.pathname === '/sse' || url.pathname.startsWith('/sse/')) {
      const sseHandler = BoostedBudgetMcpAgent.serveSSE('/sse', { binding: 'MCP_AGENT' })
      return sseHandler.fetch(authedRequest, this.env as Parameters<typeof sseHandler.fetch>[1], this.ctx)
    }

    const mcpHandler = BoostedBudgetMcpAgent.serve('/mcp', { binding: 'MCP_AGENT' })
    return mcpHandler.fetch(authedRequest, this.env as Parameters<typeof mcpHandler.fetch>[1], this.ctx)
  }
}
