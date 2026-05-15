import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'
import fs from 'fs'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
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
    rollupOptions: {
      output: {
        entryFileNames: 'assets/entry-[hash].js',
        chunkFileNames: 'assets/[name]-[hash].js',
        assetFileNames: 'assets/[name]-[hash][extname]',
        manualChunks(id: string) {
          // TanStack table/virtual isolated from vendor so its budget is tracked separately
          if (/@tanstack\/react-table|@tanstack\/react-virtual/.test(id)) return 'tanstack';

          // Stable budgetable vendor bucket — prevents Rollup from emitting
          // auto-named common chunks that slip past size-limit
          if (id.includes('node_modules')) return 'vendor';

          if (!id.includes('/apps/web/src/')) return;
          if (id.includes('/legal/')) return 'legal';
          if (id.includes('/features/admin/')) return 'admin';
          if (id.includes('/features/billing/') || id.includes('/features/pricing/') || id.includes('/features/subscription/')) return 'billing';
          if (id.includes('/features/transactions/')) return 'transactions';
          if (id.includes('/features/accounts/')) return 'accounts';
          if (id.includes('/features/budgets/')) return 'budgets';
          if (id.includes('/features/rules/')) return 'rules';
          if (id.includes('/features/dashboard/')) return 'dashboard';
          if (id.includes('/features/reports/')) return 'reports';
          if (id.includes('/features/net-worth/')) return 'net-worth';
          if (id.includes('/features/savings/')) return 'savings';
          if (id.includes('/features/auto-categorization/')) return 'auto-categorization';
          if (id.includes('/features/profile/') || id.includes('/features/deletion/')) return 'profile';
        },
      },
    },
  },
})
