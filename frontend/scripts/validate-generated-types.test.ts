import { describe, it, expect } from 'vitest'
import { readFileSync } from 'fs'
import { join, dirname } from 'path'
import { fileURLToPath } from 'url'
import { readYamlConfig } from './generate-tier-types.js'

const __filename = fileURLToPath(import.meta.url)
const __dirname = dirname(__filename)

const PROJECT_ROOT = join(__dirname, '..', '..')
const YAML_PATH = join(PROJECT_ROOT, 'config/subscription_tiers.yaml')
const GENERATED_PATH = join(PROJECT_ROOT, 'frontend/src/types/generated/subscriptionTiers.ts')

/**
 * Generates the expected TypeScript content from YAML config
 * This duplicates the generation logic to validate the committed file
 */
function generateExpectedContent(config: any): string {
  const tierNames = Object.keys(config.tiers)
  const tierType = tierNames.map(name => `'${name}'`).join(' | ')

  // Build the expected file content (matches generate-tier-types.js output)
  const header = `/**
 * COMMITTED FILE - Edit YAML, not this file
 *
 * Generated from: config/subscription_tiers.yaml
 * To regenerate: npm run generate:types
 *
 * This file is committed to git and validated by tests.
 * If YAML changes without regeneration, CI will fail.
 */`

  const types = `export type SubscriptionTier = ${tierType}

export interface TierLimits {
  accounts: number
  rules: number
  executions_per_day: number
  min_execution_interval_minutes: number
  api_calls_per_month?: number
  data_export_requests_per_month?: number
}

export interface TierFeatures {
  ads_enabled: boolean
  ai_insights: boolean
  api_access?: boolean
  priority_support?: boolean
  custom_categories?: boolean
  advanced_reports?: boolean
}

export interface TierConfig {
  display_name: string
  description: string
  price_monthly: number
  price_yearly: number
  stripe_price_id_monthly?: string | null
  stripe_price_id_yearly?: string | null
  limits: TierLimits
  features: TierFeatures
}

export interface SubscriptionTiersConfig {
  ${tierNames.map((name) => `${name}: TierConfig`).join('\n  ')}
}`

  const tierEntries = Object.entries(config.tiers).map(([tierName, tierData]: [string, any]) => {
    const limits = tierData.limits
    const features = tierData.features

    // Build features array from limits and features
    const featuresList: string[] = []

    // Add limits as features
    if (limits.accounts === -1) {
      featuresList.push('Unlimited bank accounts')
    } else {
      featuresList.push(`${limits.accounts} bank accounts`)
    }

    if (limits.rules === -1) {
      featuresList.push('Unlimited automation rules')
    } else {
      featuresList.push(`${limits.rules} automation rules`)
    }

    // Add execution frequency
    const execsPerDay = limits.executions_per_day
    if (execsPerDay === 1) {
      featuresList.push('Rules run 1x per day')
    } else if (execsPerDay >= 144) {
      featuresList.push('Rules run every 10 minutes')
    } else {
      const hours = Math.round(24 / execsPerDay)
      featuresList.push(`Rules run every ${hours} hours`)
    }

    // Add feature flags
    if (features.ads_enabled) {
      featuresList.push('Ad-supported')
    } else {
      featuresList.push('No ads')
    }

    if (features.ai_insights) {
      featuresList.push('AI-powered insights')
    }

    if (features.priority_support) {
      featuresList.push('Priority support')
    }

    if (features.advanced_reports) {
      featuresList.push('Advanced reports')
    }

    if (features.api_access) {
      featuresList.push('API access')
    }

    const priceMonthly = tierData.price_monthly === 0 ? '$0' : `$${tierData.price_monthly.toFixed(2)}`
    const priceYearly = tierData.price_yearly === 0 ? '$0' : `$${tierData.price_yearly.toFixed(2)}`

    return `  ${tierName}: {
    name: '${tierData.display_name}',
    description: '${tierData.description}',
    priceMonthly: '${priceMonthly}',
    priceYearly: '${priceYearly}',
    features: [${featuresList.map(f => `'${f}'`).join(', ')}],
    limits: {
      accounts: ${limits.accounts},
      rules: ${limits.rules},
      executionsPerDay: ${limits.executions_per_day},
      minExecutionIntervalMinutes: ${limits.min_execution_interval_minutes},
    },
  }`
  })

  const constants = `export const SUBSCRIPTION_TIERS = {
${tierEntries.join(',\n')}
} as const`

  return `${header}\n\n${types}\n\n${constants}\n`
}

describe('Generated subscription tier types validation', () => {
  it('should match YAML configuration exactly', () => {
    // Read YAML configuration
    const config = readYamlConfig(YAML_PATH)

    // Generate expected content from YAML
    const expectedContent = generateExpectedContent(config)

    // Read the committed file
    const actualContent = readFileSync(GENERATED_PATH, 'utf-8')

    // Compare - this will fail with a clear diff if out of sync
    expect(actualContent).toBe(expectedContent)
  })

  it('should have all tier names from YAML', () => {
    const config = readYamlConfig(YAML_PATH)
    const actualContent = readFileSync(GENERATED_PATH, 'utf-8')

    for (const tierName of Object.keys(config.tiers)) {
      expect(actualContent).toContain(tierName)
    }
  })

  it('should not contain placeholder or TODO comments', () => {
    const actualContent = readFileSync(GENERATED_PATH, 'utf-8')

    expect(actualContent).not.toContain('TODO')
    expect(actualContent).not.toContain('FIXME')
    expect(actualContent).not.toContain('placeholder')
  })
})
