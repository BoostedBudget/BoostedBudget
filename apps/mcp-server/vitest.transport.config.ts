/**
 * Vitest config for the MCP transport integration tests.
 *
 * Transport tests use DO-backed WebSocket sessions (Streamable HTTP and SSE),
 * which require --no-isolate --max-workers=1 to share the same workerd isolate
 * and avoid DO state-isolation errors between requests in the same test.
 *
 * Run via:  bun run test:integration:transport
 * CI step:  "Run MCP transport integration tests (serial, --no-isolate)"
 *
 * Non-transport integration tests use vitest.config.ts (normal isolated mode).
 *
 * ── Live smoke mode (#1699 todo 18) ──────────────────────────────────────────
 * Set MCP_SMOKE_API_URL to point the Worker at a REAL backend instead of the
 * intercepted `http://backend.test` host:
 *
 *   MCP_SMOKE_API_URL=http://localhost:8080 \
 *     bun run vitest run --config vitest.transport.config.ts --no-isolate --max-workers=1
 *
 * In that mode only the live smoke file is collected (the transport tests below
 * assert against the `backend.test` host and would not survive the repoint), and
 * BOOSTEDBUDGET_API_URL is set from the env var. With MCP_SMOKE_API_URL unset the
 * config is byte-for-byte behaviourally identical to before: `backend.test` + the
 * transport test only.
 */
import { cloudflareTest } from '@cloudflare/vitest-pool-workers'
import { defineConfig } from 'vitest/config'

const smokeApiUrl = process.env.MCP_SMOKE_API_URL

export default defineConfig({
  plugins: [
    cloudflareTest({
      wrangler: { configPath: './wrangler.toml' },
      miniflare: {
        // Same bindings as vitest.config.ts — keep in sync.
        bindings: {
          ENVIRONMENT: 'development',
          CLERK_SECRET_KEY: 'sk_test_xxx',
          CLERK_PUBLISHABLE_KEY: 'pk_test_xxx',
          CLERK_JWKS_URL: 'http://clerk.test/.well-known/jwks.json',
          BOOSTEDBUDGET_API_URL: smokeApiUrl ?? 'http://backend.test',
          BOOSTEDBUDGET_ORIGIN_SECRET: 'test-origin-secret',
          MCP_WORKER_SECRET: 'test-worker-secret',
          MCP_RESOURCE_URL: 'http://mcp.test',
          CLERK_ISSUER_URL: 'http://clerk.test',
          MCP_OAUTH_AUDIENCE: 'test-oauth-audience',
          CLERK_OAUTH_CLIENT_SECRET: 'test-oauth-client-secret',
        },
        kvNamespaces: ['OAUTH_KV'],
      },
    }),
  ],
  test: {
    include: smokeApiUrl
      ? ['integration/mcp/smoke-live.integration.test.ts']
      : ['integration/mcp/transport.integration.test.ts'],
    // Match the non-transport config timeouts — cold-start and token-minting can be slow on CI.
    testTimeout: 30000,
    hookTimeout: 60000,
    coverage: {
      provider: 'istanbul',
    },
  },
})
