import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    // Support dynamic port allocation via PORT or E2E_FRONTEND_PORT env var
    port: parseInt(process.env.PORT || process.env.E2E_FRONTEND_PORT || '3000'),
    host: true,
    proxy: {
      '/api': {
        // Support dynamic backend port via E2E_BACKEND_PORT env var
        target: `http://localhost:${process.env.E2E_BACKEND_PORT || '8080'}`,
        changeOrigin: true,
      },
      '/health': {
        // Support dynamic backend port via E2E_BACKEND_PORT env var
        target: `http://localhost:${process.env.E2E_BACKEND_PORT || '8080'}`,
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: true,
  },
})
