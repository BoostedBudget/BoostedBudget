import { cloudflareTest } from '@cloudflare/vitest-pool-workers'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [
    cloudflareTest({
      wrangler: { configPath: './wrangler.toml' },
      miniflare: {
        // Supply all vars that parseEnv() requires so the Worker boots (not 503).
        // ENVIRONMENT=development allows http:// URLs in Zod validation.
        bindings: {
          ENVIRONMENT: 'development',
          CLERK_SECRET_KEY: 'sk_test_xxx',
          CLERK_PUBLISHABLE_KEY: 'pk_test_xxx',
          CLERK_JWKS_URL: 'http://clerk.test/.well-known/jwks.json',
          BOOSTEDBUDGET_API_URL: 'http://backend.test',
          BOOSTEDBUDGET_ORIGIN_SECRET: 'test-origin-secret',
          MCP_WORKER_SECRET: 'test-worker-secret',
          MCP_RESOURCE_URL: 'http://mcp.test',
          CLERK_ISSUER_URL: 'http://clerk.test',
          MCP_OAUTH_AUDIENCE: 'test-oauth-audience',
          CLERK_OAUTH_CLIENT_SECRET: 'test-oauth-client-secret',
        },
        // KV namespace for OAuth token/state storage (workers-oauth-provider)
        kvNamespaces: ['OAUTH_KV'],
      },
    }),
  ],
  test: {
    // All integration tests run in a SINGLE workerd process (--no-isolate --max-workers=1
    // in the test:integration script). Running multiple workerd processes concurrently on
    // the CI runner causes "Failed to start cloudflare-pool worker" / TCP.onStreamRead
    // failures due to resource exhaustion — the macOS runner can't start 3+ workerd
    // JIT-compilation sessions simultaneously.
    //
    // State safety: verified across all test files — revocationCache cleared by Scenario 6,
    // OAUTH_KV creates fresh random IDs per run, fetch spy is beforeEach/afterEach scoped,
    // OAuthProvider is stateless read-only config. No cross-test contamination.
    include: ['integration/**/*.integration.test.ts'],
    // #1699: the live smoke (integration/mcp/smoke-live.integration.test.ts) needs a real
    // backend via MCP_SMOKE_API_URL and is run through vitest.transport.config.ts, not here.
    // It matches the include glob above, so CI's `test:integration` (this config) collected it
    // with BOOSTEDBUDGET_API_URL=http://backend.test and failed at DNS. Excluding it keeps this
    // config a self-contained, no-live-infra suite; the transport config still collects it.
    exclude: ['integration/mcp/smoke-live.integration.test.ts'],
    // Generous timeouts for Workers runtime cold-start on CI runners.
    testTimeout: 30000,
    hookTimeout: 60000,
    coverage: {
      provider: 'istanbul',
    },
  },
})
