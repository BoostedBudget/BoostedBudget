/**
 * Region configuration for BoostedBudget
 * Uses Fly.io IATA airport codes for region identification
 * Aligned with backend region codes and Fly.io deployment regions
 */

export interface RegionInfo {
  code: string // Fly.io IATA code (e.g., 'iad')
  displayName: string // Human-readable name (e.g., 'USA (Virginia)')
  country: string // Country name
  city: string // City/region name
}

/**
 * Supported regions for data residency
 * Matches backend domain/regions.go
 */
export const REGIONS = {
  iad: {
    code: 'iad',
    displayName: 'USA (Virginia)',
    country: 'USA',
    city: 'Virginia',
  },
  lhr: {
    code: 'lhr',
    displayName: 'UK (London)',
    country: 'UK',
    city: 'London',
  },
  syd: {
    code: 'syd',
    displayName: 'Australia (Sydney)',
    country: 'Australia',
    city: 'Sydney',
  },
} as const

/**
 * Type-safe region code type
 */
export type RegionCode = keyof typeof REGIONS

/**
 * Array of valid region codes for iteration
 */
export const REGION_CODES: RegionCode[] = Object.keys(REGIONS) as RegionCode[]

/**
 * Default region for new users (MVP single region)
 */
export const DEFAULT_REGION: RegionCode = 'iad'

/**
 * Get the display name for a region code
 * Returns the code itself if not found (graceful degradation)
 *
 * @param code - Region code (e.g., 'iad')
 * @returns Display name (e.g., 'USA (Virginia)') or code if not found
 */
export function getRegionDisplay(code: string): string {
  const region = REGIONS[code as RegionCode]
  return region?.displayName || code
}

/**
 * Get full region information for a region code
 * Returns undefined if region code is invalid
 *
 * @param code - Region code to look up
 * @returns RegionInfo object or undefined
 */
export function getRegionInfo(code: string): RegionInfo | undefined {
  return REGIONS[code as RegionCode]
}

/**
 * Check if a region code is valid
 *
 * @param code - Region code to validate
 * @returns true if valid, false otherwise
 */
export function isValidRegion(code: string): code is RegionCode {
  return code in REGIONS
}
