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
          // POSTHOG_API_KEY intentionally omitted — keeps analytics no-op in tests
        },
      },
    }),
  ],
  test: {
    include: ['**/*.integration.test.ts'],
    coverage: {
      provider: 'istanbul',
    },
  },
})
