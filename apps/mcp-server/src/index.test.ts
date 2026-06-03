import { describe, expect, it } from 'bun:test'

// Basic smoke tests for the Worker entry point (bun:test, no Workers runtime needed).
// Worker-runtime integration tests live in apps/mcp-server/integration/ and are run
// via `bun run test:integration` / `bun run test:integration:transport` (vitest-pool-workers).
// The unit tests here verify health endpoint routing in isolation.

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
