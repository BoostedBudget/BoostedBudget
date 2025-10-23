/**
 * Tests for subscription tier type generator
 *
 * TDD RED Phase: These tests define the expected behavior before implementation
 */

import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { readFileSync, writeFileSync, unlinkSync, existsSync, mkdirSync } from 'fs'
import { join } from 'path'
import yaml from 'js-yaml'

// These will be implemented in the generator
import {
  readYamlConfig,
  parseSubscriptionTiers,
  generateTypeScriptTypes,
  generateTypeScriptConstants,
  generateOutputFile,
  validateTierConfig,
} from './generate-tier-types'

const FIXTURES_DIR = join(__dirname, '__fixtures__')
const VALID_YAML_PATH = join(FIXTURES_DIR, 'valid-tiers.yaml')
const INVALID_YAML_PATH = join(FIXTURES_DIR, 'invalid-tiers.yaml')
const MISSING_FIELDS_YAML_PATH = join(FIXTURES_DIR, 'missing-fields.yaml')
const OUTPUT_PATH = join(__dirname, '../src/types/generated/subscriptionTiers.ts')

describe('generate-tier-types', () => {
  beforeEach(() => {
    // Create fixtures directory if it doesn't exist
    if (!existsSync(FIXTURES_DIR)) {
      mkdirSync(FIXTURES_DIR, { recursive: true })
    }

    // Create valid YAML fixture
    const validYaml = `
tiers:
  free:
    display_name: "Free"
    description: "Full budgeting with basic automation"
    price_monthly: 0.00
    price_yearly: 0.00
    limits:
      accounts: 8
      rules: 2
      executions_per_day: 1
      min_execution_interval_minutes: 1440
    features:
      ads_enabled: true
      ai_insights: false
  premium_lite:
    display_name: "Premium Lite"
    description: "Perfect for couples and light automation"
    price_monthly: 4.99
    price_yearly: 49.99
    limits:
      accounts: 15
      rules: 5
      executions_per_day: 2
      min_execution_interval_minutes: 720
    features:
      ads_enabled: false
      ai_insights: false
  premium:
    display_name: "Premium"
    description: "Power users with complex finances"
    price_monthly: 9.99
    price_yearly: 99.99
    limits:
      accounts: 30
      rules: 15
      executions_per_day: 6
      min_execution_interval_minutes: 240
    features:
      ads_enabled: false
      ai_insights: false
  premium_plus:
    display_name: "Premium+"
    description: "Unlimited everything with AI and API access"
    price_monthly: 19.99
    price_yearly: 199.99
    limits:
      accounts: -1
      rules: -1
      executions_per_day: 144
      min_execution_interval_minutes: 10
    features:
      ads_enabled: false
      ai_insights: true
`
    writeFileSync(VALID_YAML_PATH, validYaml, 'utf-8')

    // Create invalid YAML fixture (malformed)
    const invalidYaml = `
tiers:
  free:
    display_name: "Free"
    limits: [this is not valid yaml syntax: {
`
    writeFileSync(INVALID_YAML_PATH, invalidYaml, 'utf-8')

    // Create missing required fields fixture
    const missingFieldsYaml = `
tiers:
  free:
    display_name: "Free"
    # Missing limits and features
`
    writeFileSync(MISSING_FIELDS_YAML_PATH, missingFieldsYaml, 'utf-8')
  })

  afterEach(() => {
    // Clean up fixtures
    if (existsSync(VALID_YAML_PATH)) unlinkSync(VALID_YAML_PATH)
    if (existsSync(INVALID_YAML_PATH)) unlinkSync(INVALID_YAML_PATH)
    if (existsSync(MISSING_FIELDS_YAML_PATH)) unlinkSync(MISSING_FIELDS_YAML_PATH)
  })

  describe('readYamlConfig', () => {
    it('should read and parse valid YAML file correctly', () => {
      const config = readYamlConfig(VALID_YAML_PATH)

      expect(config).toBeDefined()
      expect(config.tiers).toBeDefined()
      expect(Object.keys(config.tiers)).toHaveLength(4)
      expect(config.tiers.free).toBeDefined()
      expect(config.tiers.premium_lite).toBeDefined()
      expect(config.tiers.premium).toBeDefined()
      expect(config.tiers.premium_plus).toBeDefined()
    })

    it('should throw error when YAML file does not exist', () => {
      const nonExistentPath = join(FIXTURES_DIR, 'does-not-exist.yaml')

      expect(() => readYamlConfig(nonExistentPath)).toThrow(/file not found/i)
    })

    it('should throw error when YAML is malformed', () => {
      expect(() => readYamlConfig(INVALID_YAML_PATH)).toThrow(/failed to parse yaml/i)
    })
  })

  describe('parseSubscriptionTiers', () => {
    it('should parse all tier names correctly with underscore naming', () => {
      const config = yaml.load(readFileSync(VALID_YAML_PATH, 'utf-8')) as any
      const tiers = parseSubscriptionTiers(config)

      expect(tiers).toHaveLength(4)
      expect(tiers).toContain('free')
      expect(tiers).toContain('premium_lite')
      expect(tiers).toContain('premium')
      expect(tiers).toContain('premium_plus')

      // Should NOT contain hyphenated or plus-sign versions
      expect(tiers).not.toContain('premium-lite')
      expect(tiers).not.toContain('premium+')
    })
  })

  describe('generateTypeScriptTypes', () => {
    it('should generate correct TypeScript interfaces with proper types', () => {
      const config = yaml.load(readFileSync(VALID_YAML_PATH, 'utf-8')) as any
      const typeDefinitions = generateTypeScriptTypes(config)

      // Should include all tier literal types
      expect(typeDefinitions).toContain("'free'")
      expect(typeDefinitions).toContain("'premium_lite'")
      expect(typeDefinitions).toContain("'premium'")
      expect(typeDefinitions).toContain("'premium_plus'")

      // Should define proper interfaces
      expect(typeDefinitions).toMatch(/export type SubscriptionTier = /i)
      expect(typeDefinitions).toMatch(/export interface TierLimits/i)
      expect(typeDefinitions).toMatch(/export interface TierFeatures/i)
      expect(typeDefinitions).toMatch(/export interface TierConfig/i)

      // Should include proper field types
      expect(typeDefinitions).toMatch(/accounts:\s*number/i)
      expect(typeDefinitions).toMatch(/rules:\s*number/i)
      expect(typeDefinitions).toMatch(/ads_enabled:\s*boolean/i)
      expect(typeDefinitions).toMatch(/ai_insights:\s*boolean/i)
    })
  })

  describe('generateTypeScriptConstants', () => {
    it('should generate correct constants with proper values from YAML', () => {
      const config = yaml.load(readFileSync(VALID_YAML_PATH, 'utf-8')) as any
      const constants = generateTypeScriptConstants(config)

      // Should generate SUBSCRIPTION_TIERS constant
      expect(constants).toMatch(/export const SUBSCRIPTION_TIERS/i)

      // Should have correct values for free tier
      expect(constants).toContain('8') // accounts
      expect(constants).toContain('2') // rules
      expect(constants).toMatch(/free.*\$0/is) // price

      // Should have all four tiers
      expect(constants).toMatch(/free:/i)
      expect(constants).toMatch(/premium_lite:/i)
      expect(constants).toMatch(/premium:/i)
      expect(constants).toMatch(/premium_plus:/i)

      // Should have correct Premium Lite values
      expect(constants).toContain('15') // accounts
      expect(constants).toContain('5') // rules
      expect(constants).toMatch(/premium_lite.*\$4\.99/is)

      // Should handle -1 as unlimited
      expect(constants).toMatch(/premium_plus.*accounts.*-1/is)
    })
  })

  describe('validateTierConfig', () => {
    it('should validate that all required fields are present', () => {
      const config = yaml.load(readFileSync(VALID_YAML_PATH, 'utf-8')) as any

      expect(() => validateTierConfig(config)).not.toThrow()
    })

    it('should throw error when required fields are missing', () => {
      const config = yaml.load(readFileSync(MISSING_FIELDS_YAML_PATH, 'utf-8')) as any

      expect(() => validateTierConfig(config)).toThrow(/missing required field/i)
    })
  })

  describe('generateOutputFile', () => {
    it('should create output file with proper header comment and auto-generation warning', () => {
      const config = yaml.load(readFileSync(VALID_YAML_PATH, 'utf-8')) as any
      generateOutputFile(config, OUTPUT_PATH)

      expect(existsSync(OUTPUT_PATH)).toBe(true)

      const output = readFileSync(OUTPUT_PATH, 'utf-8')

      // Should have warning comment
      expect(output).toMatch(/DO NOT EDIT.*auto-generated/i)
      expect(output).toMatch(/config\/subscription_tiers\.yaml/i)
      expect(output).toMatch(/npm run generate:types/i)

      // Clean up
      if (existsSync(OUTPUT_PATH)) unlinkSync(OUTPUT_PATH)
    })
  })
})
