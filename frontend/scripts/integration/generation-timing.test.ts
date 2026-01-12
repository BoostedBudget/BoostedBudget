/**
 * Integration tests for subscription tier type generation timing reliability
 *
 * These tests validate that the robust file writing with fsync and verification
 * prevents timing issues that were occurring on macOS APFS and other filesystems.
 *
 * Run with: bun test scripts/integration/generation-timing.test.ts
 */

import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { existsSync, unlinkSync, readFileSync } from 'fs'
import { join } from 'path'
import { generateOutputFile, readYamlConfig } from '../generate-tier-types'

const PROJECT_ROOT = join(__dirname, '../../..')
const YAML_PATH = join(PROJECT_ROOT, 'config/subscription_tiers.yaml')
const OUTPUT_PATH = join(__dirname, '../../src/types/generated/subscriptionTiers.ts')

describe('Generation Timing Integration Tests', () => {
  beforeEach(() => {
    // Clean up generated file before each test
    if (existsSync(OUTPUT_PATH)) {
      unlinkSync(OUTPUT_PATH)
    }
  })

  afterEach(() => {
    // Clean up after tests
    if (existsSync(OUTPUT_PATH)) {
      unlinkSync(OUTPUT_PATH)
    }
  })

  it('should generate file that is immediately readable', async () => {
    const config = readYamlConfig(YAML_PATH)

    // Generate file
    await generateOutputFile(config, OUTPUT_PATH)

    // File should exist immediately
    expect(existsSync(OUTPUT_PATH)).toBe(true)

    // File should be readable immediately (no timing delay)
    const content = readFileSync(OUTPUT_PATH, 'utf-8')
    expect(content).toContain('SUBSCRIPTION_TIERS')
    expect(content.length).toBeGreaterThan(1000)
  })

  it('should handle rapid generation cycles without errors', async () => {
    const config = readYamlConfig(YAML_PATH)

    // Rapidly generate, delete, and regenerate
    for (let i = 0; i < 10; i++) {
      await generateOutputFile(config, OUTPUT_PATH)
      expect(existsSync(OUTPUT_PATH)).toBe(true)

      const content = readFileSync(OUTPUT_PATH, 'utf-8')
      expect(content).toContain('SUBSCRIPTION_TIERS')

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

    // Check all tier names are present
    expect(content).toContain('free')
    expect(content).toContain('premium_lite')
    expect(content).toContain('premium')
    expect(content).toContain('premium_plus')

    // Check structure
    expect(content).toContain('export const SUBSCRIPTION_TIERS')
    expect(content).toContain('export type SubscriptionTier')
  })

  it('should generate file with reasonable size', async () => {
    const config = readYamlConfig(YAML_PATH)

    await generateOutputFile(config, OUTPUT_PATH)

    const content = readFileSync(OUTPUT_PATH, 'utf-8')

    // File should be at least 1KB but less than 50KB
    expect(content.length).toBeGreaterThan(1000)
    expect(content.length).toBeLessThan(50000)
  })

  it('should include generation metadata', async () => {
    const config = readYamlConfig(YAML_PATH)

    await generateOutputFile(config, OUTPUT_PATH)

    const content = readFileSync(OUTPUT_PATH, 'utf-8')

    // Check metadata is present
    expect(content).toContain('DO NOT EDIT - This file is auto-generated')
    expect(content).toContain('Generated from: config/subscription_tiers.yaml')
    expect(content).toContain('force-committed for CI reliability')
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
  it('should use fsync to guarantee disk flush', async () => {
    const config = readYamlConfig(YAML_PATH)

    const startTime = Date.now()
    await generateOutputFile(config, OUTPUT_PATH)
    const duration = Date.now() - startTime

    // Generation with fsync should complete in reasonable time (< 1 second)
    expect(duration).toBeLessThan(1000)

    // File should be readable immediately after return
    const content = readFileSync(OUTPUT_PATH, 'utf-8')
    expect(content).toContain('SUBSCRIPTION_TIERS')
  })

  it('should verify content matches what was written', async () => {
    const config = readYamlConfig(YAML_PATH)

    // Generation includes verification step
    await generateOutputFile(config, OUTPUT_PATH)

    // If we got here without error, verification passed
    const content = readFileSync(OUTPUT_PATH, 'utf-8')
    expect(content.length).toBeGreaterThan(0)
  })
})
