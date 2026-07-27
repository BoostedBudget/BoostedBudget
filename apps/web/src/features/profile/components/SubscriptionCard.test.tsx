import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { mockSubscriptionTiersResponse } from '@/test/fixtures/subscriptionFixtures'
import {
  mockUseGetSubscriptionTiers,
  mockRefetchSubscriptionTiers,
} from '@/test/mocks/subscription-api'
import { getSubscriptionPresentation } from '@/features/subscription/lib/subscriptionPresentation'
import type { SubscriptionPresentation } from '@/features/subscription/lib/subscriptionPresentation'

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
      renderWithRouter(
        <SubscriptionCard tier="premium" presentation={getSubscriptionPresentation({ status: 'active' })} />
      )

      expect(screen.getByText(/premium/i)).toBeInTheDocument()
      expect(screen.getByText(/\$8\.99/i)).toBeInTheDocument()
      expect(screen.getByText(/unlimited automation rules/i)).toBeInTheDocument()
      expect(screen.getByText(/unlimited bank accounts/i)).toBeInTheDocument()
      expect(screen.getByText(/for 1 person/i)).toBeInTheDocument()
      expect(screen.getByRole('link', { name: /manage subscription/i })).toBeInTheDocument()
    })

    it('should display household_2 tier with manage button', () => {
      renderWithRouter(
        <SubscriptionCard tier="household_2" presentation={getSubscriptionPresentation({ status: 'active' })} />
      )

      expect(screen.getByText(/household \(2 members\)/i)).toBeInTheDocument()
      expect(screen.getByText(/\$12\.99/i)).toBeInTheDocument()
      expect(screen.getByText(/unlimited automation rules/i)).toBeInTheDocument()
      expect(screen.getByText(/for 2 household members/i)).toBeInTheDocument()
      expect(screen.getByRole('link', { name: /manage subscription/i })).toBeInTheDocument()
    })

    it('should display household_5 tier with unlimited features', () => {
      renderWithRouter(
        <SubscriptionCard tier="household_5" presentation={getSubscriptionPresentation({ status: 'active' })} />
      )

      expect(screen.getByText(/household \(5 members\)/i)).toBeInTheDocument()
      expect(screen.getByText(/\$18\.99/i)).toBeInTheDocument()
      expect(screen.getByText(/unlimited bank accounts/i)).toBeInTheDocument()
      expect(screen.getByText(/unlimited automation rules/i)).toBeInTheDocument()
      expect(screen.getByText(/for 5 household members/i)).toBeInTheDocument()
      expect(screen.getByRole('link', { name: /manage subscription/i })).toBeInTheDocument()
    })
  })

  describe('Status badges', () => {
    it('should display correct status badges', () => {
      const queryClient = createQueryClient()
      const { rerender } = renderWithRouter(
        <SubscriptionCard tier="premium" presentation={getSubscriptionPresentation({ status: 'active' })} />,
        queryClient
      )
      expect(screen.getByText(/active/i)).toBeInTheDocument()

      rerender(
        <QueryClientProvider client={queryClient}>
          <MemoryRouter>
            <SubscriptionCard tier="premium" presentation={getSubscriptionPresentation({ status: 'inactive' })} />
          </MemoryRouter>
        </QueryClientProvider>
      )
      expect(screen.getByText(/inactive/i)).toBeInTheDocument()

      rerender(
        <QueryClientProvider client={queryClient}>
          <MemoryRouter>
            <SubscriptionCard tier="premium" presentation={getSubscriptionPresentation({ status: 'cancelled' })} />
          </MemoryRouter>
        </QueryClientProvider>
      )
      expect(screen.getByText(/cancelled/i)).toBeInTheDocument()
    })

    it('should show trial days remaining in badge when status is trial', () => {
      renderWithRouter(
        <SubscriptionCard
          tier="premium"
          presentation={getSubscriptionPresentation({ status: 'trial', trialDaysRemaining: 42 })}
        />
      )

      expect(screen.getByText(/trial — 42 days remaining/i)).toBeInTheDocument()
    })

    it('should show just "Trial" when status is trial but no days remaining provided', () => {
      renderWithRouter(
        <SubscriptionCard tier="premium" presentation={getSubscriptionPresentation({ status: 'trial' })} />
      )

      expect(screen.getByText('Trial')).toBeInTheDocument()
    })

    it('should use singular "day" when 1 day remaining', () => {
      renderWithRouter(
        <SubscriptionCard
          tier="premium"
          presentation={getSubscriptionPresentation({ status: 'trial', trialDaysRemaining: 1 })}
        />
      )

      expect(screen.getByText(/trial — 1 day remaining/i)).toBeInTheDocument()
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
        const { container } = renderWithRouter(
          <SubscriptionCard tier={tier} presentation={getSubscriptionPresentation({ status: 'active' })} />
        )

        const card = container.querySelector('.subscription-card')
        expect(card).toHaveClass(tierClass)
      })
    })
  })

  describe('Null safety and error handling', () => {
    it('should handle missing tier data gracefully', () => {
      expect(() => {
        renderWithRouter(
          <SubscriptionCard
            tier={undefined as unknown as string}
            presentation={getSubscriptionPresentation({ status: 'active' })}
          />
        )
      }).not.toThrow()

      expect(screen.getByText(/loading/i)).toBeInTheDocument()
    })

    it('should handle missing presentation gracefully', () => {
      expect(() => {
        renderWithRouter(
          <SubscriptionCard tier="premium" presentation={undefined as unknown as SubscriptionPresentation} />
        )
      }).not.toThrow()

      expect(screen.getByText(/loading/i)).toBeInTheDocument()
    })

    it('should handle null tier and undefined presentation', () => {
      expect(() => {
        renderWithRouter(
          <SubscriptionCard
            tier={null as unknown as string}
            presentation={undefined as unknown as SubscriptionPresentation}
          />
        )
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

      renderWithRouter(
        <SubscriptionCard tier="premium" presentation={getSubscriptionPresentation({ status: 'active' })} />
      )

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

      renderWithRouter(
        <SubscriptionCard tier="premium" presentation={getSubscriptionPresentation({ status: 'active' })} />
      )

      expect(screen.getByText(/error loading subscription/i)).toBeInTheDocument()
      expect(screen.getByText(/failed to fetch tiers/i)).toBeInTheDocument()
    })

    it('should render normally when hook succeeds', () => {
      renderWithRouter(
        <SubscriptionCard tier="premium" presentation={getSubscriptionPresentation({ status: 'active' })} />
      )

      expect(screen.getByText(/premium/i)).toBeInTheDocument()
      expect(screen.queryByText(/loading/i)).not.toBeInTheDocument()
      expect(screen.queryByText(/error/i)).not.toBeInTheDocument()
    })
  })

  describe('Pricing display', () => {
    it('should display correct pricing for all tiers', () => {
      const queryClient = createQueryClient()
      // Premium: $8.99
      const { rerender } = renderWithRouter(
        <SubscriptionCard tier="premium" presentation={getSubscriptionPresentation({ status: 'active' })} />,
        queryClient
      )
      expect(screen.getByText(/\$8\.99/i)).toBeInTheDocument()

      // Household 2: $12.99
      rerender(
        <QueryClientProvider client={queryClient}>
          <MemoryRouter>
            <SubscriptionCard tier="household_2" presentation={getSubscriptionPresentation({ status: 'active' })} />
          </MemoryRouter>
        </QueryClientProvider>
      )
      expect(screen.getByText(/\$12\.99/i)).toBeInTheDocument()

      // Household 5: $18.99
      rerender(
        <QueryClientProvider client={queryClient}>
          <MemoryRouter>
            <SubscriptionCard tier="household_5" presentation={getSubscriptionPresentation({ status: 'active' })} />
          </MemoryRouter>
        </QueryClientProvider>
      )
      expect(screen.getByText(/\$18\.99/i)).toBeInTheDocument()
    })

    it('should display /month suffix for paid tiers', () => {
      renderWithRouter(
        <SubscriptionCard tier="premium" presentation={getSubscriptionPresentation({ status: 'active' })} />
      )
      expect(screen.getByText(/\/month/i)).toBeInTheDocument()
    })
  })

  describe('Feature display based on tier', () => {
    it('should not render an AI feature claim (BoostedBudget has no AI)', () => {
      renderWithRouter(
        <SubscriptionCard tier="premium" presentation={getSubscriptionPresentation({ status: 'active' })} />
      )
      expect(screen.queryByText(/ai-powered insights/i)).not.toBeInTheDocument()
    })

    it('should show unlimited bank accounts for all tiers', () => {
      renderWithRouter(
        <SubscriptionCard tier="premium" presentation={getSubscriptionPresentation({ status: 'active' })} />
      )
      expect(screen.getByText(/unlimited bank accounts/i)).toBeInTheDocument()
    })

    it('should show users_included as differentiator per tier', () => {
      const queryClient = createQueryClient()
      // Premium: 1 person
      const { rerender } = renderWithRouter(
        <SubscriptionCard tier="premium" presentation={getSubscriptionPresentation({ status: 'active' })} />,
        queryClient
      )
      expect(screen.getByText(/for 1 person/i)).toBeInTheDocument()

      // Household 2: 2 household members
      rerender(
        <QueryClientProvider client={queryClient}>
          <MemoryRouter>
            <SubscriptionCard tier="household_2" presentation={getSubscriptionPresentation({ status: 'active' })} />
          </MemoryRouter>
        </QueryClientProvider>
      )
      expect(screen.getByText(/for 2 household members/i)).toBeInTheDocument()

      // Household 5: 5 household members
      rerender(
        <QueryClientProvider client={queryClient}>
          <MemoryRouter>
            <SubscriptionCard tier="household_5" presentation={getSubscriptionPresentation({ status: 'active' })} />
          </MemoryRouter>
        </QueryClientProvider>
      )
      expect(screen.getByText(/for 5 household members/i)).toBeInTheDocument()
    })

  })

  describe('Seat usage line', () => {
    it('should show seat usage when seatUsage is passed with total > 1', () => {
      renderWithRouter(
        <SubscriptionCard
          tier="household_5"
          presentation={getSubscriptionPresentation({ status: 'active' })}
          seatUsage={{ used: 2, total: 5 }}
        />
      )

      expect(screen.getByText(/2 of 5 seats used/i)).toBeInTheDocument()
    })

    it('should omit seat usage line when seatUsage is undefined', () => {
      renderWithRouter(
        <SubscriptionCard tier="premium" presentation={getSubscriptionPresentation({ status: 'active' })} />
      )

      expect(screen.queryByText(/seats used/i)).not.toBeInTheDocument()
    })

    it('should omit seat usage line when seatUsage.total is 1', () => {
      renderWithRouter(
        <SubscriptionCard
          tier="premium"
          presentation={getSubscriptionPresentation({ status: 'active' })}
          seatUsage={{ used: 1, total: 1 }}
        />
      )

      expect(screen.queryByText(/seats used/i)).not.toBeInTheDocument()
    })
  })

  // Issue #1413: comped users get an info "Complimentary" badge (never a destructive "inactive"
  // one), no monthly price, and the comp end date (or a permanent-access note) surfaced in copy.
  describe('Comped presentation (#1413)', () => {
    it('shows an info-variant Complimentary badge instead of a destructive inactive one', () => {
      renderWithRouter(
        <SubscriptionCard
          tier="premium"
          presentation={getSubscriptionPresentation({ status: 'inactive', isComped: true })}
        />
      )
      const badge = screen.getByText('Complimentary')
      expect(badge).toHaveClass('bg-info')
      expect(badge).not.toHaveClass('bg-destructive')
    })

    it('shows a Gift icon alongside the badge label (not conveyed by colour alone)', () => {
      renderWithRouter(
        <SubscriptionCard
          tier="premium"
          presentation={getSubscriptionPresentation({ status: 'inactive', isComped: true })}
        />
      )
      const badge = screen.getByText('Complimentary')
      const icon = badge.querySelector('svg')
      expect(icon).toHaveClass('lucide-gift')
    })

    it('does not show a price for a comped user', () => {
      renderWithRouter(
        <SubscriptionCard
          tier="premium"
          presentation={getSubscriptionPresentation({ status: 'inactive', isComped: true })}
        />
      )
      expect(screen.queryByText('$8.99')).not.toBeInTheDocument()
      expect(screen.queryByText('/month')).not.toBeInTheDocument()
    })

    it('shows the comp end date when present', () => {
      renderWithRouter(
        <SubscriptionCard
          tier="premium"
          presentation={getSubscriptionPresentation({
            status: 'inactive',
            isComped: true,
            compEndsAt: '2026-12-31T00:00:00Z',
          })}
        />
      )
      expect(screen.getByText(/Complimentary access ends 2026-12-31/i)).toBeInTheDocument()
    })

    it('shows provided-by copy for a permanent comp (no end date)', () => {
      renderWithRouter(
        <SubscriptionCard
          tier="premium"
          presentation={getSubscriptionPresentation({ status: 'inactive', isComped: true })}
        />
      )
      expect(screen.getByText(/Complimentary Premium, provided by BoostedBudget/i)).toBeInTheDocument()
    })

    it('never uses the word "expired" or an exclamation mark in comped copy', () => {
      renderWithRouter(
        <SubscriptionCard
          tier="premium"
          presentation={getSubscriptionPresentation({ status: 'inactive', isComped: true })}
        />
      )
      expect(screen.queryByText(/expired/i)).not.toBeInTheDocument()
      expect(document.body.textContent).not.toContain('!')
    })
  })

  // Regression: non-comped badges keep their existing colour and label.
  describe('Regression: non-comped badges unchanged (#1413)', () => {
    it('renders the Active badge for an active user', () => {
      renderWithRouter(
        <SubscriptionCard tier="premium" presentation={getSubscriptionPresentation({ status: 'active' })} />
      )
      expect(screen.getByText('Active')).toHaveClass('bg-success')
    })

    it('renders the Trial badge for a trial user', () => {
      renderWithRouter(
        <SubscriptionCard tier="premium" presentation={getSubscriptionPresentation({ status: 'trial' })} />
      )
      expect(screen.getByText('Trial')).toHaveClass('bg-info')
    })

    it('renders the Cancelled badge for a cancelled user', () => {
      renderWithRouter(
        <SubscriptionCard tier="premium" presentation={getSubscriptionPresentation({ status: 'cancelled' })} />
      )
      expect(screen.getByText('Cancelled')).toHaveClass('bg-secondary')
    })
  })
})
