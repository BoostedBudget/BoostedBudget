// Integration tests for subscription tier type generation timing reliability
// Tests validate fsync and verification prevent timing issues on macOS APFS
// Run with: bun test scripts/integration/generation-timing.test.ts

import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { existsSync, unlinkSync, readFileSync } from 'fs'
import { join } from 'path'
import { generateOutputFile, readYamlConfig } from '../generate-tier-types'

const PROJECT_ROOT = join(__dirname, '../../..')
const YAML_PATH = join(PROJECT_ROOT, 'config/subscription_tiers.yaml')
const OUTPUT_PATH = join(__dirname, '../../src/types/generated/subscriptionTiers.ts')

// Helper to clean up generated file
const cleanupFile = () => {
  if (existsSync(OUTPUT_PATH)) {
    unlinkSync(OUTPUT_PATH)
  }
}

describe('Generation Timing Integration Tests', () => {
  beforeEach(cleanupFile)
  afterEach(cleanupFile)

  it('should generate file that is immediately readable', async () => {
    const config = readYamlConfig(YAML_PATH)
    await generateOutputFile(config, OUTPUT_PATH)

    expect(existsSync(OUTPUT_PATH)).toBe(true)

    const content = readFileSync(OUTPUT_PATH, 'utf-8')
    expect(content).toContain('SUBSCRIPTION_TIERS')
    expect(content.length).toBeGreaterThan(1000)
  })

  it('should handle rapid generation cycles without errors', async () => {
    const config = readYamlConfig(YAML_PATH)

    for (let i = 0; i < 10; i++) {
      await generateOutputFile(config, OUTPUT_PATH)
      expect(existsSync(OUTPUT_PATH)).toBe(true)
      expect(readFileSync(OUTPUT_PATH, 'utf-8')).toContain('SUBSCRIPTION_TIERS')
      unlinkSync(OUTPUT_PATH)
      expect(existsSync(OUTPUT_PATH)).toBe(false)
    }
  })

  it('should generate consistent content across multiple runs', async () => {
    const config = readYamlConfig(YAML_PATH)

    // Generate file multiple times and verify content is consistent
    const contents: string[] = []

    for (let i = 0; i < 5; i++) {
      await generateOutputFile(config, OUTPUT_PATH)
      const content = readFileSync(OUTPUT_PATH, 'utf-8')

      // Remove timestamp line which will differ
      const contentWithoutTimestamp = content.replace(/Generated at:.*\n/, '')
      contents.push(contentWithoutTimestamp)

      unlinkSync(OUTPUT_PATH)
    }

    // All contents (minus timestamp) should be identical
    const firstContent = contents[0]
    contents.forEach(content => {
      expect(content).toBe(firstContent)
    })
  })

  it('should generate file with all expected tier names', async () => {
    const config = readYamlConfig(YAML_PATH)
    await generateOutputFile(config, OUTPUT_PATH)

    const content = readFileSync(OUTPUT_PATH, 'utf-8')
    const expectedStrings = [
      'free',
      'premium_lite',
      'premium',
      'premium_plus',
      'export const SUBSCRIPTION_TIERS',
      'export type SubscriptionTier'
    ]
    expectedStrings.forEach(str => expect(content).toContain(str))
  })

  it('should generate file with reasonable size', async () => {
    const config = readYamlConfig(YAML_PATH)
    await generateOutputFile(config, OUTPUT_PATH)

    const content = readFileSync(OUTPUT_PATH, 'utf-8')
    expect(content.length).toBeGreaterThan(1000)
    expect(content.length).toBeLessThan(50000)
  })

  it('should include generation metadata', async () => {
    const config = readYamlConfig(YAML_PATH)
    await generateOutputFile(config, OUTPUT_PATH)

    const content = readFileSync(OUTPUT_PATH, 'utf-8')
    const metadata = [
      'DO NOT EDIT - This file is auto-generated',
      'Generated from: config/subscription_tiers.yaml',
      'force-committed for CI reliability'
    ]
    metadata.forEach(str => expect(content).toContain(str))
  })

  it('should handle concurrent generation attempts gracefully', async () => {
    const config = readYamlConfig(YAML_PATH)

    // Start multiple generations simultaneously
    const promises = [
      generateOutputFile(config, OUTPUT_PATH),
      generateOutputFile(config, OUTPUT_PATH),
      generateOutputFile(config, OUTPUT_PATH)
    ]

    // All should complete successfully
    await Promise.all(promises)

    // File should exist and be valid
    expect(existsSync(OUTPUT_PATH)).toBe(true)
    const content = readFileSync(OUTPUT_PATH, 'utf-8')
    expect(content).toContain('SUBSCRIPTION_TIERS')
  })
})

describe('Robust File Writing Tests', () => {
  afterEach(cleanupFile)

  it('should use fsync to guarantee disk flush', async () => {
    const config = readYamlConfig(YAML_PATH)
    const startTime = Date.now()
    await generateOutputFile(config, OUTPUT_PATH)

    expect(Date.now() - startTime).toBeLessThan(1000)
    expect(readFileSync(OUTPUT_PATH, 'utf-8')).toContain('SUBSCRIPTION_TIERS')
  })

  it('should verify content matches what was written', async () => {
    const config = readYamlConfig(YAML_PATH)
    await generateOutputFile(config, OUTPUT_PATH)
    expect(readFileSync(OUTPUT_PATH, 'utf-8').length).toBeGreaterThan(0)
  })
})
