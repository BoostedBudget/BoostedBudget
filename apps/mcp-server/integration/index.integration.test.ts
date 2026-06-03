import { describe, expect, it } from 'vitest'
import { exports } from 'cloudflare:workers'

// Boot smoke test — verifies that the Worker boots successfully with the test
// env bindings defined in vitest.config.ts and serves the /health endpoint.
// If any required env var is missing, parseEnv() returns 503 before reaching /health.

describe('MCP server boot smoke', () => {
  it('GET /health returns 200', async () => {
    const worker = exports.default as { fetch: (req: Request) => Promise<Response> }
    const response = await worker.fetch(new Request('http://mcp.test/health'))
    expect(response.status).toBe(200)
    const body = await response.json()
    expect(body).toHaveProperty('status')
  })
})
