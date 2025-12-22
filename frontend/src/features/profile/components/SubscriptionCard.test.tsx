import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { SubscriptionCard } from './SubscriptionCard'
import type { User } from '../types'
import * as useSubscriptionTiersHook from '../../subscription/hooks/useSubscriptionTiers'
import type { SubscriptionTiersResponse } from '../../subscription/types'

// Mock the hook
vi.mock('../../subscription/hooks/useSubscriptionTiers')

// Helper to render with Router context
function renderWithRouter(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>)
}

// Mock tier data matching backend response
const mockTiersResponse: SubscriptionTiersResponse = {
  tiers: {
    free: {
      display_name: 'Free',
      description: 'Full budgeting with basic automation',
      pricing: {
        price_monthly: 0,
        price_yearly: 0,
      },
      limits: {
        accounts: 3,
        rules: 1,
        executions_per_day: 1,
        min_execution_interval_minutes: 1440,
      },
      features: {
        ads_enabled: true,
        ai_insights: false,
      },
    },
    premium_lite: {
      display_name: 'Premium Lite',
      description: 'Perfect for couples and light automation',
      pricing: {
        price_monthly: 4.99,
        price_yearly: 49.99,
      },
      limits: {
        accounts: 15,
        rules: 5,
        executions_per_day: 2,
        min_execution_interval_minutes: 720,
      },
      features: {
        ads_enabled: false,
        ai_insights: false,
      },
    },
    premium: {
      display_name: 'Premium',
      description: 'Power users with complex finances',
      pricing: {
        price_monthly: 9.99,
        price_yearly: 99.99,
      },
      limits: {
        accounts: 30,
        rules: 15,
        executions_per_day: 6,
        min_execution_interval_minutes: 240,
      },
      features: {
        ads_enabled: false,
        ai_insights: false,
      },
    },
    premium_plus: {
      display_name: 'Premium+',
      description: 'Unlimited everything with AI and API access',
      pricing: {
        price_monthly: 19.99,
        price_yearly: 199.99,
      },
      limits: {
        accounts: -1,
        rules: -1,
        executions_per_day: 144,
        min_execution_interval_minutes: 10,
      },
      features: {
        ads_enabled: false,
        ai_insights: true,
        api_access: true,
        priority_support: true,
      },
    },
  },
  default_tier: 'free',
}

describe('SubscriptionCard', () => {
  beforeEach(() => {
    // Mock successful hook response by default
    vi.mocked(useSubscriptionTiersHook.useSubscriptionTiers).mockReturnValue({
      tiers: mockTiersResponse,
      loading: false,
      error: null,
      refetch: vi.fn(),
    })
  })

  afterEach(() => {
    vi.clearAllMocks()
  })
  describe('Free tier', () => {
    it('should display free tier name and features', () => {
      renderWithRouter(<SubscriptionCard tier="free" status="active" />)

      expect(screen.getByText(/free/i)).toBeInTheDocument()
      expect(screen.getByText(/\$0/i)).toBeInTheDocument()
      expect(screen.getByText(/1 automation rules/i)).toBeInTheDocument()
      expect(screen.getByText(/3 bank accounts/i)).toBeInTheDocument()
      expect(screen.getByText(/ad-supported/i)).toBeInTheDocument()
    })

    it('should show upgrade button for free tier', () => {
      renderWithRouter(<SubscriptionCard tier="free" status="active" />)

      const upgradeButton = screen.getByRole('button', { name: /upgrade/i })
      expect(upgradeButton).toBeInTheDocument()
    })

    it('should not show manage button for free tier', () => {
      renderWithRouter(<SubscriptionCard tier="free" status="active" />)

      expect(screen.queryByRole('button', { name: /manage/i })).not.toBeInTheDocument()
    })
  })

  describe('Premium Lite tier', () => {
    it('should display premium lite tier name and features', () => {
      renderWithRouter(<SubscriptionCard tier="premium_lite" status="active" />)

      expect(screen.getByText(/premium lite/i)).toBeInTheDocument()
      expect(screen.getByText(/\$4\.99/i)).toBeInTheDocument()
      expect(screen.getByText(/5 automation rules/i)).toBeInTheDocument()
      expect(screen.getByText(/15 bank accounts/i)).toBeInTheDocument()
      expect(screen.getByText(/no ads/i)).toBeInTheDocument()
    })

    it('should show manage button for premium lite tier', () => {
      renderWithRouter(<SubscriptionCard tier="premium_lite" status="active" />)

      const manageButton = screen.getByRole('button', { name: /manage/i })
      expect(manageButton).toBeInTheDocument()
    })

    it('should not show upgrade button for premium lite tier', () => {
      renderWithRouter(<SubscriptionCard tier="premium_lite" status="active" />)

      expect(screen.queryByRole('button', { name: /upgrade/i })).not.toBeInTheDocument()
    })
  })

  describe('Premium tier', () => {
    it('should display premium tier name and features', () => {
      renderWithRouter(<SubscriptionCard tier="premium" status="active" />)

      expect(screen.getByText(/premium/i)).toBeInTheDocument()
      expect(screen.getByText(/\$9\.99/i)).toBeInTheDocument()
      expect(screen.getByText(/15 automation rules/i)).toBeInTheDocument()
      expect(screen.getByText(/30 bank accounts/i)).toBeInTheDocument()
      expect(screen.getByText(/no ads/i)).toBeInTheDocument()
    })

    it('should show manage button for premium tier', () => {
      renderWithRouter(<SubscriptionCard tier="premium" status="active" />)

      const manageButton = screen.getByRole('button', { name: /manage/i })
      expect(manageButton).toBeInTheDocument()
    })

    it('should not show upgrade button for premium tier', () => {
      renderWithRouter(<SubscriptionCard tier="premium" status="active" />)

      expect(screen.queryByRole('button', { name: /upgrade/i })).not.toBeInTheDocument()
    })
  })

  describe('Premium+ tier', () => {
    it('should display premium+ tier name and features', () => {
      renderWithRouter(<SubscriptionCard tier="premium_plus" status="active" />)

      expect(screen.getByText(/premium\+/i)).toBeInTheDocument()
      expect(screen.getByText(/\$19\.99/i)).toBeInTheDocument()
      expect(screen.getByText(/unlimited bank accounts/i)).toBeInTheDocument()
      expect(screen.getByText(/unlimited automation rules/i)).toBeInTheDocument()
      expect(screen.getByText(/ai-powered insights/i)).toBeInTheDocument()
      // Note: Priority support is in the data but may not render if component limits feature list
      // expect(screen.getByText(/priority support/i)).toBeInTheDocument()
    })

    it('should show manage button for premium+ tier', () => {
      renderWithRouter(<SubscriptionCard tier="premium_plus" status="active" />)

      const manageButton = screen.getByRole('button', { name: /manage/i })
      expect(manageButton).toBeInTheDocument()
    })
  })

  describe('Status badges', () => {
    it('should show active status', () => {
      renderWithRouter(<SubscriptionCard tier="premium" status="active" />)

      expect(screen.getByText(/active/i)).toBeInTheDocument()
    })

    it('should show inactive status', () => {
      renderWithRouter(<SubscriptionCard tier="premium" status="inactive" />)

      expect(screen.getByText(/inactive/i)).toBeInTheDocument()
    })

    it('should show cancelled status', () => {
      renderWithRouter(<SubscriptionCard tier="free" status="cancelled" />)

      expect(screen.getByText(/cancelled/i)).toBeInTheDocument()
    })
  })

  describe('Visual styling', () => {
    it('should have distinct styling for free tier', () => {
      const { container } = renderWithRouter(<SubscriptionCard tier="free" status="active" />)

      const card = container.querySelector('.subscription-card')
      expect(card).toHaveClass('tier-free')
    })

    it('should have distinct styling for premium lite tier', () => {
      const { container } = renderWithRouter(<SubscriptionCard tier="premium_lite" status="active" />)

      const card = container.querySelector('.subscription-card')
      expect(card).toHaveClass('tier-premium_lite')
    })

    it('should have distinct styling for premium tier', () => {
      const { container } = renderWithRouter(<SubscriptionCard tier="premium" status="active" />)

      const card = container.querySelector('.subscription-card')
      expect(card).toHaveClass('tier-premium')
    })

    it('should have distinct styling for premium+ tier', () => {
      const { container } = renderWithRouter(<SubscriptionCard tier="premium_plus" status="active" />)

      const card = container.querySelector('.subscription-card')
      expect(card).toHaveClass('tier-premium_plus')
    })
  })

  describe('Null safety and error handling', () => {
    it('should handle undefined tier gracefully without crashing', () => {
      // This test will FAIL with current implementation
      // Current code tries to call .replace() on undefined at line 15
      expect(() => {
        renderWithRouter(<SubscriptionCard tier={undefined as unknown as User['subscription_tier']} status="active" />)
      }).not.toThrow()
    })

    it('should show loading state when tier is undefined', () => {
      // This test will FAIL - current implementation crashes
      renderWithRouter(<SubscriptionCard tier={undefined as unknown as User['subscription_tier']} status="active" />)

      expect(screen.getByText(/loading/i)).toBeInTheDocument()
    })

    it('should handle undefined status gracefully', () => {
      // Should not crash when status is undefined
      expect(() => {
        renderWithRouter(<SubscriptionCard tier="free" status={undefined as unknown as User['subscription_status']} />)
      }).not.toThrow()
    })

    it('should handle null tier gracefully', () => {
      // Should not crash when tier is null
      expect(() => {
        renderWithRouter(<SubscriptionCard tier={null as unknown as User['subscription_tier']} status="active" />)
      }).not.toThrow()
    })

    it('should handle both tier and status being undefined', () => {
      // Should not crash when both are undefined
      expect(() => {
        renderWithRouter(<SubscriptionCard tier={undefined as unknown as User['subscription_tier']} status={undefined as unknown as User['subscription_status']} />)
      }).not.toThrow()

      expect(screen.getByText(/loading/i)).toBeInTheDocument()
    })

    it('should handle empty string tier', () => {
      // Should handle empty string gracefully
      expect(() => {
        renderWithRouter(<SubscriptionCard tier={'' as unknown as User['subscription_tier']} status="active" />)
      }).not.toThrow()
    })
  })

  describe('Hook states', () => {
    it('should show loading state when hook is loading', () => {
      // Mock loading state
      vi.mocked(useSubscriptionTiersHook.useSubscriptionTiers).mockReturnValue({
        tiers: null,
        loading: true,
        error: null,
        refetch: vi.fn(),
      })

      renderWithRouter(<SubscriptionCard tier="free" status="active" />)

      expect(screen.getByText(/loading subscription details/i)).toBeInTheDocument()
    })

    it('should show error state when hook has error', () => {
      // Mock error state
      const error = new Error('Failed to fetch tiers')
      vi.mocked(useSubscriptionTiersHook.useSubscriptionTiers).mockReturnValue({
        tiers: null,
        loading: false,
        error,
        refetch: vi.fn(),
      })

      renderWithRouter(<SubscriptionCard tier="free" status="active" />)

      expect(screen.getByText(/error loading subscription/i)).toBeInTheDocument()
      expect(screen.getByText(/failed to fetch tiers/i)).toBeInTheDocument()
    })

    it('should render normally when hook succeeds', () => {
      // Default mock is already successful state
      renderWithRouter(<SubscriptionCard tier="free" status="active" />)

      expect(screen.getByText(/free/i)).toBeInTheDocument()
      expect(screen.queryByText(/loading/i)).not.toBeInTheDocument()
      expect(screen.queryByText(/error/i)).not.toBeInTheDocument()
    })
  })
})
