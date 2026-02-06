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

function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
    },
  })
}

function renderWithRouter(ui: React.ReactElement, queryClient = createQueryClient()) {
  return render(
    <QueryClientProvider client={queryClient}>
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
    it('should display premium tier with manage button', () => {
      renderWithRouter(<SubscriptionCard tier="premium" status="active" />)

      expect(screen.getByText(/premium/i)).toBeInTheDocument()
      expect(screen.getByText(/\$8\.99/i)).toBeInTheDocument()
      expect(screen.getByText(/unlimited automation rules/i)).toBeInTheDocument()
      expect(screen.getByText(/30 bank accounts/i)).toBeInTheDocument()
      expect(screen.getByText(/no ads/i)).toBeInTheDocument()
      expect(screen.getByRole('link', { name: /manage subscription/i })).toBeInTheDocument()
    })

    it('should display household_2 tier with manage button', () => {
      renderWithRouter(<SubscriptionCard tier="household_2" status="active" />)

      expect(screen.getByText(/household \(2 members\)/i)).toBeInTheDocument()
      expect(screen.getByText(/\$12\.99/i)).toBeInTheDocument()
      expect(screen.getByText(/unlimited automation rules/i)).toBeInTheDocument()
      expect(screen.getByText(/60 bank accounts/i)).toBeInTheDocument()
      expect(screen.getByRole('link', { name: /manage subscription/i })).toBeInTheDocument()
    })

    it('should display household_5 tier with unlimited features', () => {
      renderWithRouter(<SubscriptionCard tier="household_5" status="active" />)

      expect(screen.getByText(/household \(5 members\)/i)).toBeInTheDocument()
      expect(screen.getByText(/\$18\.99/i)).toBeInTheDocument()
      expect(screen.getByText(/150 bank accounts/i)).toBeInTheDocument()
      expect(screen.getByText(/unlimited automation rules/i)).toBeInTheDocument()
      expect(screen.getByText(/ai-powered insights/i)).toBeInTheDocument()
      expect(screen.getByRole('link', { name: /manage subscription/i })).toBeInTheDocument()
    })
  })

  describe('Status badges', () => {
    it('should display correct status badges', () => {
      const queryClient = createQueryClient()
      const { rerender } = renderWithRouter(<SubscriptionCard tier="premium" status="active" />, queryClient)
      expect(screen.getByText(/active/i)).toBeInTheDocument()

      rerender(
        <QueryClientProvider client={queryClient}>
          <MemoryRouter>
            <SubscriptionCard tier="premium" status="inactive" />
          </MemoryRouter>
        </QueryClientProvider>
      )
      expect(screen.getByText(/inactive/i)).toBeInTheDocument()

      rerender(
        <QueryClientProvider client={queryClient}>
          <MemoryRouter>
            <SubscriptionCard tier="premium" status="cancelled" />
          </MemoryRouter>
        </QueryClientProvider>
      )
      expect(screen.getByText(/cancelled/i)).toBeInTheDocument()
    })
  })

  describe('Visual styling', () => {
    const tierClasses = [
      { tier: 'premium', class: 'tier-premium' },
      { tier: 'household_2', class: 'tier-household_2' },
      { tier: 'household_3', class: 'tier-household_3' },
      { tier: 'household_4', class: 'tier-household_4' },
      { tier: 'household_5', class: 'tier-household_5' },
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
        renderWithRouter(<SubscriptionCard tier="premium" status={undefined as unknown as User['subscription_status']} />)
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

      renderWithRouter(<SubscriptionCard tier="premium" status="active" />)

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

      renderWithRouter(<SubscriptionCard tier="premium" status="active" />)

      expect(screen.getByText(/error loading subscription/i)).toBeInTheDocument()
      expect(screen.getByText(/failed to fetch tiers/i)).toBeInTheDocument()
    })

    it('should render normally when hook succeeds', () => {
      renderWithRouter(<SubscriptionCard tier="premium" status="active" />)

      expect(screen.getByText(/premium/i)).toBeInTheDocument()
      expect(screen.queryByText(/loading/i)).not.toBeInTheDocument()
      expect(screen.queryByText(/error/i)).not.toBeInTheDocument()
    })
  })

  describe('Pricing display', () => {
    it('should display correct pricing for all tiers', () => {
      const queryClient = createQueryClient()
      // Premium: $8.99
      const { rerender } = renderWithRouter(<SubscriptionCard tier="premium" status="active" />, queryClient)
      expect(screen.getByText(/\$8\.99/i)).toBeInTheDocument()

      // Household 2: $12.99
      rerender(
        <QueryClientProvider client={queryClient}>
          <MemoryRouter>
            <SubscriptionCard tier="household_2" status="active" />
          </MemoryRouter>
        </QueryClientProvider>
      )
      expect(screen.getByText(/\$12\.99/i)).toBeInTheDocument()

      // Household 5: $18.99
      rerender(
        <QueryClientProvider client={queryClient}>
          <MemoryRouter>
            <SubscriptionCard tier="household_5" status="active" />
          </MemoryRouter>
        </QueryClientProvider>
      )
      expect(screen.getByText(/\$18\.99/i)).toBeInTheDocument()
    })

    it('should display /month suffix for paid tiers', () => {
      renderWithRouter(<SubscriptionCard tier="premium" status="active" />)
      expect(screen.getByText(/\/month/i)).toBeInTheDocument()
    })
  })

  describe('Feature display based on tier', () => {
    it('should show AI insights for all premium tiers', () => {
      renderWithRouter(<SubscriptionCard tier="premium" status="active" />)
      expect(screen.getByText(/ai-powered insights/i)).toBeInTheDocument()
    })

    it('should show correct account limits per tier', () => {
      const queryClient = createQueryClient()
      // Premium: 30 accounts
      const { rerender } = renderWithRouter(<SubscriptionCard tier="premium" status="active" />, queryClient)
      expect(screen.getByText(/30 bank accounts/i)).toBeInTheDocument()

      // Household 2: 60 accounts
      rerender(
        <QueryClientProvider client={queryClient}>
          <MemoryRouter>
            <SubscriptionCard tier="household_2" status="active" />
          </MemoryRouter>
        </QueryClientProvider>
      )
      expect(screen.getByText(/60 bank accounts/i)).toBeInTheDocument()

      // Household 5: 150 accounts
      rerender(
        <QueryClientProvider client={queryClient}>
          <MemoryRouter>
            <SubscriptionCard tier="household_5" status="active" />
          </MemoryRouter>
        </QueryClientProvider>
      )
      expect(screen.getByText(/150 bank accounts/i)).toBeInTheDocument()
    })

    it('should show "no ads" for all paid tiers', () => {
      renderWithRouter(<SubscriptionCard tier="premium" status="active" />)
      expect(screen.getByText(/no ads/i)).toBeInTheDocument()
    })
  })
})
