/**
 * Subscription Tier Type Generator
 *
 * Generates TypeScript types and constants from config/subscription_tiers.yaml
 * This is a build-time code generation tool.
 */

import { readFileSync, writeFileSync, existsSync, mkdirSync } from 'fs'
import { dirname } from 'path'
import yaml from 'js-yaml'

export interface TierConfig {
  tiers: Record<string, any>
}

/**
 * Read and parse YAML configuration file
 */
export function readYamlConfig(path: string): TierConfig {
  if (!existsSync(path)) {
    throw new Error(`File not found: ${path}`)
  }

  try {
    const fileContents = readFileSync(path, 'utf-8')
    const config = yaml.load(fileContents) as TierConfig
    return config
  } catch (error) {
    throw new Error(`Failed to parse YAML: ${error}`)
  }
}

/**
 * Extract tier names from config, ensuring underscore naming convention
 */
export function parseSubscriptionTiers(config: TierConfig): string[] {
  return Object.keys(config.tiers)
}

/**
 * Generate TypeScript type definitions from tier config
 */
export function generateTypeScriptTypes(config: TierConfig): string {
  const tierNames = parseSubscriptionTiers(config)
  const tierLiterals = tierNames.map((name) => `'${name}'`).join(' | ')

  return `
export type SubscriptionTier = ${tierLiterals}

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
}
`
}

/**
 * Build human-readable feature list from tier limits and features
 */
function buildFeatureList(limits: any, featureFlags: any): string[] {
  const features: string[] = []

  // Add limits as features
  if (limits.accounts === -1) {
    features.push('Unlimited bank accounts')
  } else {
    features.push(`${limits.accounts} bank accounts`)
  }

  if (limits.rules === -1) {
    features.push('Unlimited automation rules')
  } else {
    features.push(`${limits.rules} automation rules`)
  }

  // Add execution frequency
  const execsPerDay = limits.executions_per_day
  if (execsPerDay === 1) {
    features.push('Rules run 1x per day')
  } else if (execsPerDay >= 144) {
    features.push('Rules run every 10 minutes')
  } else {
    const hours = Math.round(24 / execsPerDay)
    features.push(`Rules run every ${hours} hours`)
  }

  // Add feature flags
  if (featureFlags.ads_enabled) {
    features.push('Ad-supported')
  } else {
    features.push('No ads')
  }

  if (featureFlags.ai_insights) {
    features.push('AI-powered insights')
  }

  if (featureFlags.priority_support) {
    features.push('Priority support')
  }

  if (featureFlags.advanced_reports) {
    features.push('Advanced reports')
  }

  if (featureFlags.api_access) {
    features.push('API access')
  }

  return features
}

/**
 * Format price value for display
 */
function formatPrice(price: number): string {
  return price === 0 ? '$0' : `$${price.toFixed(2)}`
}

/**
 * Generate TypeScript constants with tier data
 */
export function generateTypeScriptConstants(config: TierConfig): string {
  const tierEntries = Object.entries(config.tiers).map(([tierName, tierData]) => {
    const features = buildFeatureList(tierData.limits, tierData.features)
    const featuresStr = features.map((f) => `'${f}'`).join(', ')

    return `  ${tierName}: {
    name: '${tierData.display_name}',
    description: '${tierData.description}',
    priceMonthly: '${formatPrice(tierData.price_monthly)}',
    priceYearly: '${formatPrice(tierData.price_yearly)}',
    features: [${featuresStr}],
    limits: {
      accounts: ${tierData.limits.accounts},
      rules: ${tierData.limits.rules},
      executionsPerDay: ${tierData.limits.executions_per_day},
      minExecutionIntervalMinutes: ${tierData.limits.min_execution_interval_minutes},
    },
  }`
  })

  return `
export const SUBSCRIPTION_TIERS = {
${tierEntries.join(',\n')}
} as const
`
}

/**
 * Validate that tier config has all required fields
 */
export function validateTierConfig(config: TierConfig): void {
  if (!config.tiers || typeof config.tiers !== 'object') {
    throw new Error('Missing required field: tiers')
  }

  for (const [tierName, tierData] of Object.entries(config.tiers)) {
    if (!tierData.limits) {
      throw new Error(`Missing required field: tiers.${tierName}.limits`)
    }
    if (!tierData.features) {
      throw new Error(`Missing required field: tiers.${tierName}.features`)
    }
  }
}

/**
 * Generate complete output file with header and all type definitions
 */
export function generateOutputFile(config: TierConfig, outputPath: string): void {
  validateTierConfig(config)

  const header = `/**
 * DO NOT EDIT - This file is auto-generated
 *
 * Generated from: config/subscription_tiers.yaml
 *
 * To modify subscription tiers, edit the YAML file and run:
 *   npm run generate:types
 */
`

  const types = generateTypeScriptTypes(config)
  const constants = generateTypeScriptConstants(config)

  const output = header + types + constants

  // Ensure output directory exists
  const outputDir = dirname(outputPath)
  if (!existsSync(outputDir)) {
    mkdirSync(outputDir, { recursive: true })
  }

  writeFileSync(outputPath, output, 'utf-8')
}
