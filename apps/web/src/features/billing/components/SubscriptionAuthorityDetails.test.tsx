import { describe, expect, it } from 'bun:test'
import { render, screen } from '@testing-library/react'
import type { RevenueCatSummary } from '@boostedbudget/api-client/models'
import { SubscriptionAuthorityDetails } from './SubscriptionAuthorityDetails'

describe('SubscriptionAuthorityDetails', () => {
  it('handles null effective access even if the resolution flag is inconsistent', () => {
    render(<SubscriptionAuthorityDetails summary={{as_of: '2026-09-13T10:00:00Z', access_resolved: true, effective: null, own: null, paid: null, comp: null, trial: null, billing_available: false, billing: [], pending_operations: 0}} />)
    expect(screen.getByText(/Access confirmation is temporarily unavailable/)).toBeInTheDocument()
    expect(screen.queryByText(/Effective access:/)).not.toBeInTheDocument()
  })
  it('separately shows effective, paid and complimentary grants', () => {
    render(<SubscriptionAuthorityDetails summary={{
      access_resolved: true,
      effective: { Tier: 'household_2', Status: 'active', Source: 'household' },
      paid: { tier: 'premium', status: 'active' },
      comp: { tier: 'household_2', status: 'active' },
      trial: null, billing_available: false, pending_operations: 1,
    } as unknown as RevenueCatSummary} />)
    expect(screen.getByText(/Effective access: household_2/)).toBeInTheDocument()
    expect(screen.getByText(/Paid access: premium/)).toBeInTheDocument()
    expect(screen.getByText(/Complimentary access: household_2/)).toBeInTheDocument()
    expect(screen.getByText(/Billing details are temporarily unavailable/)).toBeInTheDocument()
  })
  it('shows unresolved access without claiming expiry or blocking recovery', () => {
    render(<SubscriptionAuthorityDetails summary={{ access_resolved: false, billing_available: false } as RevenueCatSummary} />)
    expect(screen.getByText(/Access confirmation is temporarily unavailable/)).toBeInTheDocument()
    expect(screen.queryByText(/expired/i)).toBeNull()
  })
})
