#!/usr/bin/env tsx

/**
 * CLI entry point for subscription tier type generator
 *
 * Reads config/subscription_tiers.yaml and generates
 * frontend/src/types/generated/subscriptionTiers.ts
 *
 * Uses robust file writing with fsync and verification to prevent
 * timing issues on macOS APFS and other delayed-write filesystems.
 */

import { join, dirname } from 'path'
import { fileURLToPath } from 'url'
import { readYamlConfig, generateOutputFile } from './generate-tier-types.js'

const __filename = fileURLToPath(import.meta.url)
const __dirname = dirname(__filename)

const PROJECT_ROOT = join(__dirname, '../..')
const YAML_PATH = join(PROJECT_ROOT, 'config/subscription_tiers.yaml')
const OUTPUT_PATH = join(__dirname, '../src/types/generated/subscriptionTiers.ts')

async function main() {
  const startTime = Date.now()

  try {
    console.log('🔄 Generating subscription tier types...')
    console.log(`📖 Reading: ${YAML_PATH}`)

    const config = readYamlConfig(YAML_PATH)
    const tierCount = Object.keys(config.tiers).length
    console.log(`   Found ${tierCount} tiers: ${Object.keys(config.tiers).join(', ')}`)

    console.log(`✍️  Writing: ${OUTPUT_PATH}`)
    console.log('   Using robust write with fsync and verification...')

    await generateOutputFile(config, OUTPUT_PATH)

    const duration = Date.now() - startTime
    console.log(`✅ Successfully generated and verified subscription tier types (${duration}ms)`)

  } catch (error) {
    const duration = Date.now() - startTime
    console.error(`❌ Failed to generate subscription tier types after ${duration}ms:`)
    if (error instanceof Error) {
      console.error(`   Error: ${error.message}`)
      if (error.stack) {
        console.error(`   Stack: ${error.stack}`)
      }
    } else {
      console.error(error)
    }
    process.exit(1)
  }
}

main()
