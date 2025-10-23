import { defineConfig, Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'
import { readYamlConfig, generateOutputFile } from './scripts/generate-tier-types.js'
import { verifyFile } from './scripts/generate-tier-types-robust.js'

/**
 * Vite plugin to generate subscription tier types before build
 *
 * Ensures generated types are available AND VALID before module resolution starts.
 * This prevents timing issues where imports fail because the file doesn't
 * exist yet or isn't fully synced on macOS APFS.
 *
 * Key improvements:
 * - Uses verifyFile() instead of existsSync() to check file validity
 * - Each Vite worker independently verifies the file (no shared state)
 * - Always regenerates if file is missing or invalid
 *
 * This addresses macOS APFS timing issues where a file may exist but not
 * be fully visible to all processes yet.
 */
function tierTypesGeneratorPlugin(): Plugin {
  const GENERATED_FILE = path.join(__dirname, 'src/types/generated/subscriptionTiers.ts')
  const YAML_PATH = path.join(__dirname, '../config/subscription_tiers.yaml')

  const ensureValidFile = async () => {
    console.log('🔍 [tier-types] Verifying generated types...')

    // Check if file is valid (not just exists)
    const isValid = verifyFile(GENERATED_FILE, {
      verificationPattern: 'SUBSCRIPTION_TIERS',
      minSize: 1000,
      maxSize: 50000
    })

    if (!isValid) {
      console.log('🔄 [tier-types] File invalid or missing, regenerating...')
      try {
        const startTime = Date.now()
        const config = readYamlConfig(YAML_PATH)
        await generateOutputFile(config, GENERATED_FILE)
        const duration = Date.now() - startTime
        console.log(`✅ [tier-types] Types generated and verified (${duration}ms)`)
      } catch (error) {
        console.error('❌ [tier-types] Generation failed:', error)
        throw new Error('Failed to generate subscription tier types')
      }
    } else {
      console.log('✅ [tier-types] Types valid and ready')
    }
  }

  return {
    name: 'tier-types-generator',
    enforce: 'pre', // Run before other plugins

    // Run when Vite config is resolved (earliest possible hook)
    async configResolved() {
      await ensureValidFile()
    },

    // Also run when build starts (redundant but ensures coverage)
    async buildStart() {
      await ensureValidFile()
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
