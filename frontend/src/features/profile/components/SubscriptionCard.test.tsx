import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { User } from '../types'
import { mockSubscriptionTiersResponse } from '@/test/fixtures/subscriptionFixtures'
import {
  mockUseGetSubscriptionTiers,
  mockRefetchSubscriptionTiers,
} from '@/test/mocks/subscription-api'

import { SubscriptionCard } from './SubscriptionCard'

function renderWithRouter(ui: React.ReactElement) {
  return render(
    <QueryClientProvider client={new QueryClient({
      defaultOptions: {
        queries: { retry: false, gcTime: 0 },
      },
    })}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>
  )
}

describe('SubscriptionCard', () => {
  beforeEach(() => {
    mockUseGetSubscriptionTiers.mockClear()
    mockRefetchSubscriptionTiers.mockClear()

    // Default successful response - configure the API mock
    mockUseGetSubscriptionTiers.mockReturnValue({
      data: mockSubscriptionTiersResponse,
      isLoading: false,
      error: null,
      refetch: mockRefetchSubscriptionTiers,
    })
  })

  afterEach(() => {
    mockUseGetSubscriptionTiers.mockClear()
    mockRefetchSubscriptionTiers.mockClear()
  })

  describe('Tier display', () => {
    it('should display free tier with upgrade button', () => {
      renderWithRouter(<SubscriptionCard tier="free" status="active" />)

      expect(screen.getByText(/free/i)).toBeInTheDocument()
      expect(screen.getByText(/\$0/i)).toBeInTheDocument()
      expect(screen.getByText(/1 automation rules/i)).toBeInTheDocument()
      expect(screen.getByText(/3 bank accounts/i)).toBeInTheDocument()
      expect(screen.getByText(/ad-supported/i)).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /upgrade/i })).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /manage/i })).not.toBeInTheDocument()
    })

    it('should display premium lite tier with manage button', () => {
      renderWithRouter(<SubscriptionCard tier="premium_lite" status="active" />)

      expect(screen.getByText(/premium lite/i)).toBeInTheDocument()
      expect(screen.getByText(/\$4\.99/i)).toBeInTheDocument()
      expect(screen.getByText(/5 automation rules/i)).toBeInTheDocument()
      expect(screen.getByText(/15 bank accounts/i)).toBeInTheDocument()
      expect(screen.getByText(/no ads/i)).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /manage/i })).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /upgrade/i })).not.toBeInTheDocument()
    })

    it('should display premium tier with manage button', () => {
      renderWithRouter(<SubscriptionCard tier="premium" status="active" />)

      expect(screen.getByText(/premium/i)).toBeInTheDocument()
      expect(screen.getByText(/\$9\.99/i)).toBeInTheDocument()
      expect(screen.getByText(/15 automation rules/i)).toBeInTheDocument()
      expect(screen.getByText(/30 bank accounts/i)).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /manage/i })).toBeInTheDocument()
    })

    it('should display premium+ tier with unlimited features', () => {
      renderWithRouter(<SubscriptionCard tier="premium_plus" status="active" />)

      expect(screen.getByText(/premium\+/i)).toBeInTheDocument()
      expect(screen.getByText(/\$19\.99/i)).toBeInTheDocument()
      expect(screen.getByText(/unlimited bank accounts/i)).toBeInTheDocument()
      expect(screen.getByText(/unlimited automation rules/i)).toBeInTheDocument()
      expect(screen.getByText(/ai-powered insights/i)).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /manage/i })).toBeInTheDocument()
    })
  })

  describe('Status badges', () => {
    it('should display correct status badges', () => {
      const { rerender } = renderWithRouter(<SubscriptionCard tier="premium" status="active" />)
      expect(screen.getByText(/active/i)).toBeInTheDocument()

      rerender(
        <MemoryRouter>
          <SubscriptionCard tier="premium" status="inactive" />
        </MemoryRouter>
      )
      expect(screen.getByText(/inactive/i)).toBeInTheDocument()

      rerender(
        <MemoryRouter>
          <SubscriptionCard tier="free" status="cancelled" />
        </MemoryRouter>
      )
      expect(screen.getByText(/cancelled/i)).toBeInTheDocument()
    })
  })

  describe('Visual styling', () => {
    const tierClasses = [
      { tier: 'free', class: 'tier-free' },
      { tier: 'premium_lite', class: 'tier-premium_lite' },
      { tier: 'premium', class: 'tier-premium' },
      { tier: 'premium_plus', class: 'tier-premium_plus' },
    ]

    tierClasses.forEach(({ tier, class: tierClass }) => {
      it(`should apply ${tierClass} styling`, () => {
        const { container } = renderWithRouter(<SubscriptionCard tier={tier as User['subscription_tier']} status="active" />)

        const card = container.querySelector('.subscription-card')
        expect(card).toHaveClass(tierClass)
      })
    })
  })

  describe('Null safety and error handling', () => {
    it('should handle missing tier data gracefully', () => {
      expect(() => {
        renderWithRouter(<SubscriptionCard tier={undefined as unknown as User['subscription_tier']} status="active" />)
      }).not.toThrow()

      expect(screen.getByText(/loading/i)).toBeInTheDocument()
    })

    it('should handle missing status gracefully', () => {
      expect(() => {
        renderWithRouter(<SubscriptionCard tier="free" status={undefined as unknown as User['subscription_status']} />)
      }).not.toThrow()
    })

    it('should handle null tier and undefined status', () => {
      expect(() => {
        renderWithRouter(<SubscriptionCard tier={null as unknown as User['subscription_tier']} status={undefined as unknown as User['subscription_status']} />)
      }).not.toThrow()

      expect(screen.getByText(/loading/i)).toBeInTheDocument()
    })
  })

  describe('Hook states', () => {
    it('should show loading state when hook is loading', () => {
      mockUseGetSubscriptionTiers.mockReturnValue({
        data: undefined,
        isLoading: true,
        error: null,
        refetch: mockRefetchSubscriptionTiers,
      })

      renderWithRouter(<SubscriptionCard tier="free" status="active" />)

      expect(screen.getByText(/loading subscription details/i)).toBeInTheDocument()
    })

    it('should show error state when hook has error', () => {
      const error = new Error('Failed to fetch tiers')
      mockUseGetSubscriptionTiers.mockReturnValue({
        data: undefined,
        isLoading: false,
        error,
        refetch: mockRefetchSubscriptionTiers,
      })

      renderWithRouter(<SubscriptionCard tier="free" status="active" />)

      expect(screen.getByText(/error loading subscription/i)).toBeInTheDocument()
      expect(screen.getByText(/failed to fetch tiers/i)).toBeInTheDocument()
    })

    it('should render normally when hook succeeds', () => {
      renderWithRouter(<SubscriptionCard tier="free" status="active" />)

      expect(screen.getByText(/free/i)).toBeInTheDocument()
      expect(screen.queryByText(/loading/i)).not.toBeInTheDocument()
      expect(screen.queryByText(/error/i)).not.toBeInTheDocument()
    })
  })
})
