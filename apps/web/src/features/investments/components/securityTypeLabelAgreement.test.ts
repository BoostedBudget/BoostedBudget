import { describe, expect, it } from 'bun:test'
import { securityTypeLabel } from '@/features/transactions/investmentSubcategory'
import { humanizeBucketLabel } from './allocationBucketLabel'

/**
 * #1631 C2: the plan's Verification asked to assert the Holdings Type column and the allocation
 * donut render the SAME label for the SAME `security_type`. No such test existed — both surfaces
 * asserted "ETF" independently, which would pass even if they diverged for every other value.
 *
 * Both call sites are exercised through the exact functions the production render call sites use:
 * - HoldingsTable's Type column: `securityTypeLabel(r.security_type, 'Unknown')`
 * - AllocationDonutChart (group_by=security_type): `humanizeBucketLabel(value, 'security_type')`
 */
describe('Holdings Type column vs allocation donut label agreement (#1631 C2)', () => {
  it('renders the same label for every KNOWN security_type value', () => {
    const knownValues = ['etf', 'reit', 'mbs', 'abs', 'mutual fund', 'stock', 'bond', 'cash']

    for (const value of knownValues) {
      const holdingsLabel = securityTypeLabel(value, 'Unknown')
      const donutLabel = humanizeBucketLabel(value, 'security_type')
      expect(donutLabel).toBe(holdingsLabel)
    }
  })

  // ⚠ They DELIBERATELY diverge for the empty/null/"unknown" case, and this pins that divergence
  // rather than "fixing" it into agreement. Holdings shows "Unknown" — a security-type label,
  // matching HoldingView.SecurityType's documented nil -> "unknown" contract. The donut shows
  // "Other" — an aggregation BUCKET name, not a security-type label; it groups every
  // unclassifiable holding into one slice rather than labeling the slice "Unknown". This is why
  // `securityTypeLabel(value, fallback)` takes the fallback per caller instead of hard-coding one.
  it('deliberately diverges on the unknown/null/empty case: Holdings says "Unknown", the donut says "Other"', () => {
    for (const value of ['', 'unknown', 'UNKNOWN'] as const) {
      expect(securityTypeLabel(value, 'Unknown')).toBe('Unknown')
      expect(humanizeBucketLabel(value, 'security_type')).toBe('Other')
    }
  })
})
