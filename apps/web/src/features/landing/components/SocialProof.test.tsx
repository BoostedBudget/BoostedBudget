/**
 * SocialProof Tests
 * Issue #972: Savings Transfer Automation — landing page stat
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { screen } from '@testing-library/react'
import React from 'react'
import { SocialProof } from './SocialProof'
import { mockUseGetSavingsImpactAggregate } from '@/test/mocks/savings-impact-api'
import { renderWithQuery } from '@/test/utils/queryTestUtils'

describe('SocialProof', () => {
  beforeEach(() => {
    mockUseGetSavingsImpactAggregate.mockClear()
    mockUseGetSavingsImpactAggregate.mockReturnValue({
      data: null,
      isLoading: false,
      error: null,
    })
  })

  it('renders trust badges', () => {
    renderWithQuery(<SocialProof />)
    expect(screen.getByText(/bank-level security/i)).toBeTruthy()
  })

  it('renders savings stat when published=true', () => {
    mockUseGetSavingsImpactAggregate.mockReturnValue({
      data: {
        published: true,
        total_amount: 125000,
        user_count: 342,
        completed_count: 1200,
        computed_at: new Date().toISOString(),
      },
      isLoading: false,
      error: null,
    })
    renderWithQuery(<SocialProof />)
    expect(screen.getByText(/125,000/)).toBeTruthy()
    expect(screen.getByText(/342/)).toBeTruthy()
  })

  it('hides savings stat when published=false', () => {
    mockUseGetSavingsImpactAggregate.mockReturnValue({
      data: {
        published: false,
        total_amount: 125000,
        user_count: 342,
        completed_count: 1200,
        computed_at: new Date().toISOString(),
      },
      isLoading: false,
      error: null,
    })
    renderWithQuery(<SocialProof />)
    expect(screen.queryByText(/125,000/)).toBeNull()
  })

  it('hides savings stat when loading', () => {
    mockUseGetSavingsImpactAggregate.mockReturnValue({
      data: null,
      isLoading: true,
      error: null,
    })
    renderWithQuery(<SocialProof />)
    expect(screen.queryByText(/moved to savings/i)).toBeNull()
  })

  it('hides savings stat when data is null', () => {
    mockUseGetSavingsImpactAggregate.mockReturnValue({
      data: null,
      isLoading: false,
      error: null,
    })
    renderWithQuery(<SocialProof />)
    expect(screen.queryByText(/moved to savings/i)).toBeNull()
  })

  // Issue #972: PII masking — both the total_amount and user_count in the savings
  // stat badge are individually wrapped in data-ph-mask spans (SocialProof.tsx:101/103)
  // so PostHog session replay never captures raw financial figures on the landing page.
  it('applies data-ph-mask to savings total_amount and user_count spans', () => {
    mockUseGetSavingsImpactAggregate.mockReturnValue({
      data: {
        published: true,
        total_amount: 200000,
        user_count: 500,
        completed_count: 1800,
        computed_at: new Date().toISOString(),
      },
      isLoading: false,
      error: null,
    })
    const { container } = renderWithQuery(<SocialProof />)
    const maskedSpans = container.querySelectorAll('[data-ph-mask]')
    // Expect at least two: one for total_amount, one for user_count
    expect(maskedSpans.length).toBeGreaterThanOrEqual(2)
    const maskedTexts = Array.from(maskedSpans).map((el) => el.textContent ?? '')
    expect(maskedTexts.some((t) => t.includes('200,000'))).toBe(true)
    expect(maskedTexts.some((t) => t.includes('500'))).toBe(true)
  })
})
