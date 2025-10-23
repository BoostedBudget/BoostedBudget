import { defineConfig, Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'
import { existsSync } from 'fs'
import { readYamlConfig, generateOutputFile } from './scripts/generate-tier-types.js'

/**
 * Vite plugin to generate subscription tier types before build
 *
 * Ensures generated types are available before module resolution starts.
 * This prevents timing issues where imports fail because the file doesn't
 * exist yet.
 *
 * The plugin directly imports and calls generation functions synchronously,
 * ensuring each Vite worker (in parallel test execution) has the file
 * available before any module resolution happens.
 *
 * Generation only happens once per Vite server lifetime.
 */
function tierTypesGeneratorPlugin(): Plugin {
  const GENERATED_FILE = path.join(__dirname, 'src/types/generated/subscriptionTiers.ts')
  const YAML_PATH = path.join(__dirname, '../config/subscription_tiers.yaml')
  let hasGenerated = false

  const ensureGenerated = async () => {
    if (hasGenerated) {
      return
    }

    console.log('🔍 [tier-types] Checking generated types...')

    if (!existsSync(GENERATED_FILE)) {
      console.log('🔄 [tier-types] File not found, generating inline...')
      try {
        const startTime = Date.now()
        const config = readYamlConfig(YAML_PATH)
        await generateOutputFile(config, GENERATED_FILE)
        const duration = Date.now() - startTime
        console.log(`✅ [tier-types] Types generated successfully (${duration}ms)`)
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
    async configResolved() {
      await ensureGenerated()
    },

    // Also run when build starts (redundant but ensures coverage)
    async buildStart() {
      await ensureGenerated()
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
