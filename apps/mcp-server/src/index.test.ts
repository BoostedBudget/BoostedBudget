import { describe, expect, it } from 'bun:test'

// Basic smoke tests for the Worker entry point.
// NOTE: No full Worker-runtime integration test exists yet (GAP-R3-20).
// The full request chain (index.ts → DO agent → tool → host-proxy) is untested
// at the transport level — all other tests are pure-unit with mocked neighbors.
// See the plan for the @cloudflare/vitest-pool-workers integration test work item.
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
