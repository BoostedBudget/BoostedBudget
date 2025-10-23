/**
 * Vitest Global Setup
 *
 * Runs once before all test suites to ensure generated files exist.
 * This is critical because vitest resolves module imports during test discovery,
 * which happens before any Vite plugins or test setup files run.
 *
 * Without this, tests that import generated types will fail with "Cannot find module"
 * even if the pretest hook successfully generated the file, because vitest's
 * module resolution happens too early in the lifecycle.
 */

import { execSync } from 'child_process'
import { existsSync } from 'fs'
import { join } from 'path'

const GENERATED_FILE = join(__dirname, 'src/types/generated/subscriptionTiers.ts')

export async function setup() {
  // Check if generated file exists
  if (!existsSync(GENERATED_FILE)) {
    console.log('⚠️  [vitest-setup] Generated subscription tier types not found')
    console.log('🔄 [vitest-setup] Generating types before test discovery...')

    try {
      // Run generation synchronously to block vitest until complete
      execSync('npm run generate:types', {
        cwd: __dirname,
        stdio: 'inherit'
      })

      console.log('✅ [vitest-setup] Types generated successfully')
    } catch (error) {
      console.error('❌ [vitest-setup] Failed to generate types:', error)
      throw error
    }
  } else {
    console.log('✅ [vitest-setup] Generated subscription tier types found')
  }
}

export async function teardown() {
  // No cleanup needed - generated file should persist for subsequent runs
}
