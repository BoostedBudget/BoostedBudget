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
    sourcemap: true,
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
              name: 'charts',
              test: /node_modules\/(echarts|zrender|echarts-for-react)\//,
              priority: 15, // echarts (~500 KB raw) isolated from vendor
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
          ],
        },
      },
    },
  },
})
