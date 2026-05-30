import { describe, expect, it } from 'bun:test'

// Basic smoke tests for the Worker entry point.
// Full Worker integration is verified via `wrangler dev` in CI.
// We test request routing logic in isolation here.

describe('health endpoint', () => {
  it('returns 200 with JSON body for GET /health', async () => {
    // Dynamically import so we can test without a full Cloudflare runtime
    const { handleHealth } = await import('./health')
    const response = handleHealth()
    expect(response.status).toBe(200)
    const body = await response.json()
    expect(body).toMatchObject({ status: 'ok', service: 'mcp-server' })
  })
})
