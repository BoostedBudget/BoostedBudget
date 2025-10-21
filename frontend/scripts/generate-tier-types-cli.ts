#!/usr/bin/env tsx

/**
 * CLI entry point for subscription tier type generator
 *
 * Reads config/subscription_tiers.yaml and generates
 * frontend/src/types/generated/subscriptionTiers.ts
 */

import { join, dirname } from 'path'
import { fileURLToPath } from 'url'
import { readYamlConfig, generateOutputFile } from './generate-tier-types.js'

const __filename = fileURLToPath(import.meta.url)
const __dirname = dirname(__filename)

const PROJECT_ROOT = join(__dirname, '../..')
const YAML_PATH = join(PROJECT_ROOT, 'config/subscription_tiers.yaml')
const OUTPUT_PATH = join(__dirname, '../src/types/generated/subscriptionTiers.ts')

try {
  console.log('🔄 Generating subscription tier types...')
  console.log(`📖 Reading: ${YAML_PATH}`)

  const config = readYamlConfig(YAML_PATH)

  console.log(`✍️  Writing: ${OUTPUT_PATH}`)
  generateOutputFile(config, OUTPUT_PATH)

  console.log('✅ Successfully generated subscription tier types!')
} catch (error) {
  console.error('❌ Failed to generate subscription tier types:', error)
  process.exit(1)
}
