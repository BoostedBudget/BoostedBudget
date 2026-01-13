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
      '/(api|health)': {
        target: process.env.DOCKER_ENV ? 'http://backend:8080' : `http://localhost:${process.env.E2E_BACKEND_PORT || '8080'}`,
        changeOrigin: true,
        secure: false, // Allow self-signed certificates
      },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: true,
  },
})
