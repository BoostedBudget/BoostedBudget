import { defineConfig, Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'
import { existsSync } from 'fs'
import { execSync } from 'child_process'

/**
 * Vite plugin to generate subscription tier types before build
 *
 * Ensures generated types are available before module resolution starts.
 * This prevents timing issues where imports fail because the file doesn't
 * exist yet.
 *
 * The plugin checks if the generated file exists and generates it if missing.
 * Generation only happens once per Vite server lifetime.
 */
function tierTypesGeneratorPlugin(): Plugin {
  const GENERATED_FILE = path.join(__dirname, 'src/types/generated/subscriptionTiers.ts')
  let hasGenerated = false

  const ensureGenerated = () => {
    if (hasGenerated) {
      return
    }

    console.log('🔍 [tier-types] Checking generated types...')

    if (!existsSync(GENERATED_FILE)) {
      console.log('🔄 [tier-types] File not found, generating types...')
      try {
        execSync('npm run generate:types', {
          cwd: __dirname,
          stdio: 'inherit'
        })
        console.log('✅ [tier-types] Types generated successfully')
      } catch (error) {
        console.error('❌ [tier-types] Generation failed:', error)
        throw new Error('Failed to generate subscription tier types')
      }
    } else {
      console.log('✅ [tier-types] Types already exist')
    }

    hasGenerated = true
  }

  return {
    name: 'tier-types-generator',
    enforce: 'pre', // Run before other plugins

    // Run when Vite config is resolved (earliest possible hook)
    configResolved() {
      ensureGenerated()
    },

    // Also run when build starts (redundant but ensures coverage)
    buildStart() {
      ensureGenerated()
    }
  }
}

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    tierTypesGeneratorPlugin(), // BEFORE React plugin
    react()
  ],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 3000,
    host: true,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/health': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: true,
  },
})
