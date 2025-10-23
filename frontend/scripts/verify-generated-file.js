#!/usr/bin/env node

/**
 * Verify that generated subscription tiers file exists and is valid
 *
 * Used in CI and npm scripts to ensure generation succeeded.
 * This is a lightweight check that can be run quickly in pipelines.
 *
 * Exit codes:
 *   0 - File exists and is valid
 *   1 - File missing, invalid, or verification failed
 */

import { readFileSync, existsSync } from 'fs'
import { join, dirname } from 'path'
import { fileURLToPath } from 'url'

const __filename = fileURLToPath(import.meta.url)
const __dirname = dirname(__filename)

const OUTPUT_PATH = join(__dirname, '../src/types/generated/subscriptionTiers.ts')

try {
  // Check 1: File exists
  if (!existsSync(OUTPUT_PATH)) {
    console.error('❌ Generated file not found:', OUTPUT_PATH)
    console.error('')
    console.error('   To fix: Run npm run generate:types')
    console.error('')
    process.exit(1)
  }

  // Check 2: File is readable
  const content = readFileSync(OUTPUT_PATH, 'utf-8')

  // Check 3: Contains expected export
  if (!content.includes('SUBSCRIPTION_TIERS')) {
    console.error('❌ Generated file invalid: missing SUBSCRIPTION_TIERS export')
    console.error('   File:', OUTPUT_PATH)
    console.error('')
    console.error('   This may indicate the generation script failed silently.')
    console.error('   Try running: npm run generate:types')
    console.error('')
    process.exit(1)
  }

  // Check 4: Contains expected tier names
  const expectedTiers = ['free', 'premium_lite', 'premium', 'premium_plus']
  const missingTiers = expectedTiers.filter(tier => !content.includes(tier))

  if (missingTiers.length > 0) {
    console.error('❌ Generated file invalid: missing expected tiers')
    console.error('   File:', OUTPUT_PATH)
    console.error('   Missing tiers:', missingTiers.join(', '))
    console.error('')
    console.error('   The YAML file may have been modified incorrectly.')
    console.error('   Check: config/subscription_tiers.yaml')
    console.error('')
    process.exit(1)
  }

  // Check 5: File size is reasonable
  if (content.length < 1000) {
    console.error('❌ Generated file too small:', content.length, 'bytes')
    console.error('   File:', OUTPUT_PATH)
    console.error('   Expected: >1000 bytes')
    console.error('')
    console.error('   This may indicate truncated output or generation failure.')
    console.error('   Try running: npm run generate:types')
    console.error('')
    process.exit(1)
  }

  if (content.length > 50000) {
    console.error('❌ Generated file too large:', content.length, 'bytes')
    console.error('   File:', OUTPUT_PATH)
    console.error('   Expected: <50000 bytes')
    console.error('')
    console.error('   This may indicate a generation bug or runaway output.')
    console.error('   Check the generation script.')
    console.error('')
    process.exit(1)
  }

  // Success!
  console.log('✅ Generated file verified:', OUTPUT_PATH)
  console.log(`   Size: ${content.length} bytes`)
  console.log(`   Tiers: ${expectedTiers.join(', ')}`)

} catch (error) {
  console.error('❌ Verification failed:', error.message)
  console.error('')
  console.error('   File:', OUTPUT_PATH)
  console.error('   Error:', error)
  console.error('')
  console.error('   Try running: npm run generate:types')
  console.error('')
  process.exit(1)
}
