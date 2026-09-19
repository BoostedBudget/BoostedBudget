import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'
import fs from 'fs'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  optimizeDeps: {
    // Include echarts ESM modules to ensure esbuild pre-bundles them correctly.
    // Note: We use echarts-for-react/esm/core (not /lib/core) to avoid Vite 8 CJS→ESM
    // interop issues where esbuild 0.25 no longer auto-unwraps exports.__esModule = true.
    include: ['echarts-for-react/esm/core', 'echarts/core'],
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
      // Workspace packages - bypass package.json exports resolution for Vite/esbuild compatibility
      // (esbuild has limited support for wildcard exports like "./endpoints/*/*")
      '@boostedbudget/api-client': path.resolve(__dirname, '../../packages/api-client/src'),
      '@boostedbudget/shared-types': path.resolve(__dirname, '../../packages/shared-types/src'),
    },
  },
  server: {
    // Support dynamic port allocation via PORT or E2E_FRONTEND_PORT env var
    port: parseInt(process.env.PORT || process.env.E2E_FRONTEND_PORT || '3000'),
    host: '0.0.0.0', // Explicitly bind to all interfaces for Tailscale access
    // HTTPS configuration for Tailscale access (self-signed cert)
    https: fs.existsSync(path.resolve(__dirname, './cert.pem')) ? {
      key: fs.readFileSync(path.resolve(__dirname, './key.pem')),
      cert: fs.readFileSync(path.resolve(__dirname, './cert.pem')),
    } : undefined,
    // Disable host check to allow access from any hostname
    strictPort: false,
    cors: true, // Enable CORS for WebSocket connections
    // HMR configuration for Tailscale and remote access
    hmr: {
      // Let Vite auto-detect the WebSocket host from browser location
      // This allows HMR to work whether accessing via localhost or Tailscale
      clientPort: parseInt(process.env.PORT || process.env.E2E_FRONTEND_PORT || '3000'),
    },
    proxy: {
      // Proxy backend routes (use Docker service name for container communication)
      // Note: Use separate keys instead of regex pattern for Vite compatibility
      '/api': {
        target: process.env.DOCKER_ENV ? 'http://backend:8080' : `http://localhost:${process.env.E2E_BACKEND_PORT || '8080'}`,
        changeOrigin: true,
        secure: false, // Allow self-signed certificates
      },
      '/health': {
        target: process.env.DOCKER_ENV ? 'http://backend:8080' : `http://localhost:${process.env.E2E_BACKEND_PORT || '8080'}`,
        changeOrigin: true,
        secure: false, // Allow self-signed certificates
      },
    },
  },
  build: {
    outDir: 'dist',
    // Issue #1514 (G6): 'hidden' still emits .map files (so Sentry can symbolicate) but
    // omits the `//# sourceMappingURL=` comment from the built .js bundles, so browsers
    // never fetch the maps. The CI deploy workflows upload the maps to Sentry via
    // sentry-cli, then unconditionally delete dist/**/*.map before `wrangler pages deploy`
    // so they're never published to Cloudflare Pages regardless of upload outcome.
    sourcemap: 'hidden',
    // Vite 8 uses Rolldown internally; rolldownOptions is the preferred API.
    rolldownOptions: {
      output: {
        entryFileNames: 'assets/entry-[hash].js',
        chunkFileNames: 'assets/[name]-[hash].js',
        assetFileNames: 'assets/[name]-[hash][extname]',
        // Use codeSplitting.groups instead of manualChunks (non-deprecated API).
        // NOTE: @tanstack/* kept in vendor group (not isolated) to avoid cross-chunk
        // init failures. Regression guard: e2e/prod-bundle-smoke.noauth.spec.ts.
        codeSplitting: {
          includeDependenciesRecursively: false,
          groups: [
            // ── node_modules splits ──────────────────────────────────────────
            // Higher priority groups prevent lower-priority (vendor) catch-all from
            // vacuuming them. Each is budgeted independently in .size-limit.json.
            {
              // Preserve the checkout runtime's dynamic SDK import. Without this
              // group, vendor absorbs the SDK and every landing visit downloads it.
              name: 'revenuecat',
              test: /node_modules\/@revenuecat\/purchases-js\//,
              priority: 15,
            },
            {
              name: 'charts',
              test: /node_modules\/(echarts|zrender|echarts-for-react)\//,
              priority: 15, // echarts (~500 KB raw) isolated from vendor
            },
            // #1650. The consent-gated posthog-js SDK is ~92 kB gzip and is loaded
            // lazily by src/shared/lib/posthog.ts, so it must not sit in `vendor`
            // (which is eagerly imported). Isolating it here gives the lazy import
            // a stable chunk name and a budget of its own.
            //
            // The two source modules move with it, and that half is load-bearing:
            // groups assign chunks by PATH, so leaving them in `shared` (priority 7)
            // would keep a static `shared -> posthog` edge and the SDK would still be
            // modulepreloaded on `/`. They are reachable only through App.tsx's
            // React.lazy boundary, so the whole chunk stays lazy. routes.ts is
            // deliberately NOT here — App.tsx imports it eagerly and it must stay
            // SDK-free in `shared`.
            {
              name: 'posthog',
              test: /\/apps\/web\/src\/shared\/posthog\/(AuthenticatedPostHogExperiences|engagement)\./,
              priority: 11,
            },
            {
              name: 'posthog',
              test: /node_modules\/(posthog-js|@posthog)\//,
              priority: 11,
            },
            // #1601. Isolating echarts from `vendor` was NOT enough to keep it off the landing
            // route, because our own wrappers around it live under src/shared/charts/ — which the
            // `shared` group (priority 7) swept into the eagerly-imported shared chunk. The
            // shared chunk therefore statically imported echarts, and `/` downloaded 216 KiB gzip
            // of charting to render a page with no chart on it. Confirmed from the built output's
            // own import graph, which showed a literal `shared -> charts` edge.
            //
            // Only the four modules with a RUNTIME echarts dependency move, and the list is
            // exact rather than a directory glob:
            //   echarts-setup  — imports echarts/core, charts, components, renderers
            //   themes         — imports echarts-setup for its registerTheme() side effects
            //   EChartsChart   — imports echarts-setup and echarts-for-react
            //   index          — re-exports EChartsChart, so leaving it behind would preserve
            //                    the exact edge this group exists to cut
            //
            // formatters/colors/escapeHtml/useDarkMode/types and options/* deliberately STAY in
            // `shared`: options/* import only `type { EChartsOption }`, which is erased at build
            // and creates no runtime edge, and formatters is imported directly by chunks that are
            // themselves on the eager path — moving it would drag this chunk straight back onto
            // the critical path, which is the failure this comment exists to prevent.
            {
              name: 'charts',
              test: /\/apps\/web\/src\/shared\/charts\/(echarts-setup|themes|EChartsChart|index)\./,
              priority: 15,
            },
            {
              name: 'vendor',
              test: /node_modules\//,
              priority: 10, // Stable catch-all for all node_modules (incl. @tanstack/* + @dnd-kit/*)
            },

            // ── shared source splits ─────────────────────────────────────────
            // Consolidate cross-module code to avoid orphan chunks.
            {
              name: 'api-client',
              test: /\/packages\/(api-client|shared-types)\//,
              priority: 8, // Generated React Query hooks + types
            },
            // #1601. MUST stay above `shared` (7), and the file list is deliberately explicit.
            //
            // These four are the authenticated top-nav chrome. They live under src/components/,
            // so `shared` below would sweep them into the eagerly-imported shared chunk — and
            // Header statically imports @/features/notifications and
            // @/features/transactions/hooks/useReviewSummary, so `shared` then reached
            // transactions -> accounts -> investments/net-worth/household/profile/admin ->
            // dashboard -> echarts. That one edge was most of the landing route's 847 KiB, for
            // chrome that `!isLandingPage` stops from rendering on / at all.
            //
            // Groups assign chunks by PATH, not by import graph, so lazy()-ing Header in App.tsx
            // is not sufficient on its own — without this group the module stays in an eager
            // chunk and its static imports come with it. Both halves are required.
            //
            // AppFooter is pointedly NOT here: it renders on the landing page, so it belongs in
            // the eager `shared` chunk. Adding it would put this whole chunk back on the
            // critical path and silently undo the split. Enforced by
            // scripts/check-route-weight.sh, which fails if this chunk reappears in /'s eager set.
            {
              name: 'layout-chrome',
              test: /\/apps\/web\/src\/components\/layout\/(Header|Navigation|UserDropdown|MobileNav)\./,
              priority: 9,
            },
            // #1601. The KEYSTONE of the landing route's cascade — ONE group, deliberately.
            //
            // These are all light modules that eager code needs but which lived in heavy feature
            // chunks, so importing one dragged its whole feature along.
            //
            // ⚠ RETRACTION. An earlier version of this comment said they were consolidated
            // because "rolldown honours only the FIRST group matching a given name". THAT IS
            // FALSE, and the #1601 review caught it. Rolldown's own docs say the opposite — "It's
            // ok to have the same name for different groups. Rolldown will deduplicate the chunk
            // names if necessary", with higher priority chosen first and group index breaking
            // ties only among equal priorities. Both groups always applied; the build proves it
            // by emitting two distinct `shared-*.js` chunks.
            //
            // I inferred the wrong rule from a measurement that did not move (797 -> 798 KiB) for
            // an entirely different reason: the admin chunk was being held on the landing route
            // by rolldown's unresolved-optional-peer stub for date-fns-tz, which `vendor`
            // imported. Relocating these modules WAS working the whole time; the stub was masking
            // it. One group is still the clearer way to express one intent, so the shape stays —
            // but not for the reason originally given.
            //
            // ⚠ Known consequence, accepted: the two shared chunks import each other
            // (shared-A <-> shared-B is a genuine chunk-level cycle in the current build). ES
            // modules handle cyclic chunk imports, the build succeeds and the app and E2E suite
            // pass, so this is a maintenance smell rather than a defect — but if chunking here is
            // reworked, breaking the cycle is the thing to aim for.
            //
            //   profile/hooks/*        — useProfile is a thin api-client wrapper, but the profile
            //                            chunk also holds ProfilePage, which imports the admin
            //                            and household BARRELS. Two eager consumers reached it:
            //                            PremiumRoute (src/auth/ -> `entry`) and
            //                            usePostHogIdentify (src/shared/hooks/).
            //   admin chrome           — AdminRoute/AdminLayout are the `element` of the /admin
            //                            route branch, so App.tsx needs them statically; a guard
            //                            cannot be lazy without a Suspense boundary above what it
            //                            guards. AdminSidebar/useIsAdmin/useAdminMode/constants
            //                            come with them.
            //   billing fallbacks      — a Suspense fallback and an error-boundary fallback, which
            //                            must resolve synchronously by definition.
            //
            // Together these held profile -> household -> accounts -> {investments, net-worth} ->
            // dashboard -> charts on a page that renders none of it. Runtime guards do not help:
            // usePostHogIdentify already passes { enabled: !!user }, but chunk membership is
            // STATIC — the import exists whether or not the query runs.
            //
            // Every module matched here was checked to import nothing heavier than react-router,
            // lucide icons, cn(), a constant, or an api-client endpoint. The heavy pages
            // (ProfilePage, UsersPage, HealthDashboardPage, ...) stay in their feature chunks and
            // remain lazily loaded by their own routes.
            {
              name: 'shared',
              test: /\/apps\/web\/src\/features\/(profile\/hooks\/|admin\/(components\/(AdminRoute|AdminLayout|AdminSidebar)|hooks\/(useIsAdmin|useAdminMode)|constants)\.|billing\/(components\/(SubscriptionManagementSkeleton|SubscriptionErrorState)|hooks\/useRevenueCatCheckout|revenueCatCheckout|revenueCatRuntime)\.)/,
              priority: 8,
            },
            {
              name: 'shared',
              test: /\/apps\/web\/src\/(shared|components|lib)\/|\/apps\/web\/src\/features\/(categories\/utils|tags)\//,
              priority: 7, // App-wide utilities, hooks, UI primitives (incl. tags — used across features)
            },

            // ── per-feature route splits ─────────────────────────────────────
            // Lazy-loaded features get dedicated chunks; all at priority 5.
            {
              name: 'legal',
              test: /\/apps\/web\/src\/legal\//,
              priority: 5,
            },
            {
              name: 'admin',
              test: /\/apps\/web\/src\/features\/admin\//,
              priority: 5,
            },
            {
              name: 'billing',
              test: /\/apps\/web\/src\/features\/(billing|pricing|subscription)\//,
              priority: 5,
            },
            {
              name: 'transactions',
              test: /\/apps\/web\/src\/features\/transactions\//,
              priority: 5,
            },
            {
              name: 'accounts',
              test: /\/apps\/web\/src\/features\/accounts\//,
              priority: 5,
            },
            {
              // Keep the detail-only activity panel behind its React.lazy boundary;
              // the feature-wide budgets group would otherwise absorb it.
              name: 'budget-plan',
              test: /\/apps\/web\/src\/features\/budgets\/components\/(BudgetPlanActivityPanel|BudgetPlanFlowChart|buildBudgetFlow)\./,
              priority: 6,
            },
            {
              name: 'budgets',
              test: /\/apps\/web\/src\/features\/budgets\//,
              priority: 5,
            },
            {
              name: 'boosts',
              test: /\/apps\/web\/src\/features\/boosts\//,
              priority: 5,
            },
            {
              name: 'dashboard',
              test: /\/apps\/web\/src\/features\/dashboard\//,
              priority: 5,
            },
            {
              name: 'reports',
              test: /\/apps\/web\/src\/features\/reports\//,
              priority: 5,
            },
            {
              name: 'net-worth',
              test: /\/apps\/web\/src\/features\/net-worth\//,
              priority: 5,
            },
            {
              name: 'savings',
              test: /\/apps\/web\/src\/features\/savings\//,
              priority: 5,
            },
            {
              name: 'auto-categorization',
              test: /\/apps\/web\/src\/features\/auto-categorization\//,
              priority: 5,
            },
            {
              name: 'profile',
              test: /\/apps\/web\/src\/features\/(profile|deletion)\//,
              priority: 5,
            },
            // Issue #1300: consolidated Planning + Insights tabs
            {
              name: 'planning',
              test: /\/apps\/web\/src\/features\/planning\//,
              priority: 5,
            },
            {
              name: 'insights',
              test: /\/apps\/web\/src\/features\/insights\//,
              priority: 5,
            },
            // Issue #1296: Portfolio tab — investments feature code
            {
              name: 'investments',
              test: /\/apps\/web\/src\/features\/investments\//,
              priority: 5,
            },
            // Issue #1370: Household frontend (Phase 6) — invite accept page,
            // household hooks/components
            {
              name: 'household',
              test: /\/apps\/web\/src\/features\/household\//,
              priority: 5,
            },
            // Issue #1411: public /mcp marketing + docs page
            {
              name: 'mcp',
              test: /\/apps\/web\/src\/features\/mcp\//,
              priority: 5,
            },
          ],
        },
      },
    },
  },
})
