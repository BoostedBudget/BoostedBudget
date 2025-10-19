import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { SubscriptionCard } from './SubscriptionCard'
import type { User } from '../types'

describe('SubscriptionCard', () => {
  describe('Free tier', () => {
    it('should display free tier name and features', () => {
      render(<SubscriptionCard tier="free" status="active" />)

      expect(screen.getByText(/free/i)).toBeInTheDocument()
      expect(screen.getByText(/\$0/i)).toBeInTheDocument()
      expect(screen.getByText(/3 automation rules/i)).toBeInTheDocument()
      expect(screen.getByText(/2 linked accounts/i)).toBeInTheDocument()
      expect(screen.getByText(/ad-supported/i)).toBeInTheDocument()
    })

    it('should show upgrade button for free tier', () => {
      render(<SubscriptionCard tier="free" status="active" />)

      const upgradeButton = screen.getByRole('button', { name: /upgrade/i })
      expect(upgradeButton).toBeInTheDocument()
    })

    it('should not show manage button for free tier', () => {
      render(<SubscriptionCard tier="free" status="active" />)

      expect(screen.queryByRole('button', { name: /manage/i })).not.toBeInTheDocument()
    })
  })

  describe('Premium tier', () => {
    it('should display premium tier name and features', () => {
      render(<SubscriptionCard tier="premium" status="active" />)

      expect(screen.getByText(/premium/i)).toBeInTheDocument()
      expect(screen.getByText(/\$9\.99/i)).toBeInTheDocument()
      expect(screen.getByText(/unlimited rules/i)).toBeInTheDocument()
      expect(screen.getByText(/unlimited accounts/i)).toBeInTheDocument()
      expect(screen.getByText(/no ads/i)).toBeInTheDocument()
    })

    it('should show manage button for premium tier', () => {
      render(<SubscriptionCard tier="premium" status="active" />)

      const manageButton = screen.getByRole('button', { name: /manage/i })
      expect(manageButton).toBeInTheDocument()
    })

    it('should not show upgrade button for premium tier', () => {
      render(<SubscriptionCard tier="premium" status="active" />)

      expect(screen.queryByRole('button', { name: /upgrade/i })).not.toBeInTheDocument()
    })
  })

  describe('Premium+ tier', () => {
    it('should display premium+ tier name and features', () => {
      render(<SubscriptionCard tier="premium+" status="active" />)

      expect(screen.getByText(/premium\+/i)).toBeInTheDocument()
      expect(screen.getByText(/\$19\.99/i)).toBeInTheDocument()
      expect(screen.getByText(/everything in premium/i)).toBeInTheDocument()
      expect(screen.getByText(/ai-powered insights/i)).toBeInTheDocument()
      expect(screen.getByText(/priority support/i)).toBeInTheDocument()
    })

    it('should show manage button for premium+ tier', () => {
      render(<SubscriptionCard tier="premium+" status="active" />)

      const manageButton = screen.getByRole('button', { name: /manage/i })
      expect(manageButton).toBeInTheDocument()
    })
  })

  describe('Status badges', () => {
    it('should show active status', () => {
      render(<SubscriptionCard tier="premium" status="active" />)

      expect(screen.getByText(/active/i)).toBeInTheDocument()
    })

    it('should show inactive status', () => {
      render(<SubscriptionCard tier="premium" status="inactive" />)

      expect(screen.getByText(/inactive/i)).toBeInTheDocument()
    })

    it('should show cancelled status', () => {
      render(<SubscriptionCard tier="free" status="cancelled" />)

      expect(screen.getByText(/cancelled/i)).toBeInTheDocument()
    })
  })

  describe('Visual styling', () => {
    it('should have distinct styling for free tier', () => {
      const { container } = render(<SubscriptionCard tier="free" status="active" />)

      const card = container.querySelector('.subscription-card')
      expect(card).toHaveClass('tier-free')
    })

    it('should have distinct styling for premium tier', () => {
      const { container } = render(<SubscriptionCard tier="premium" status="active" />)

      const card = container.querySelector('.subscription-card')
      expect(card).toHaveClass('tier-premium')
    })

    it('should have distinct styling for premium+ tier', () => {
      const { container } = render(<SubscriptionCard tier="premium+" status="active" />)

      const card = container.querySelector('.subscription-card')
      expect(card).toHaveClass('tier-premium-plus')
    })
  })

  describe('Null safety and error handling', () => {
    it('should handle undefined tier gracefully without crashing', () => {
      // This test will FAIL with current implementation
      // Current code tries to call .replace() on undefined at line 15
      expect(() => {
        render(<SubscriptionCard tier={undefined as unknown as User['subscription_tier']} status="active" />)
      }).not.toThrow()
    })

    it('should show loading state when tier is undefined', () => {
      // This test will FAIL - current implementation crashes
      render(<SubscriptionCard tier={undefined as unknown as User['subscription_tier']} status="active" />)

      expect(screen.getByText(/loading/i)).toBeInTheDocument()
    })

    it('should handle undefined status gracefully', () => {
      // Should not crash when status is undefined
      expect(() => {
        render(<SubscriptionCard tier="free" status={undefined as unknown as User['subscription_status']} />)
      }).not.toThrow()
    })

    it('should handle null tier gracefully', () => {
      // Should not crash when tier is null
      expect(() => {
        render(<SubscriptionCard tier={null as unknown as User['subscription_tier']} status="active" />)
      }).not.toThrow()
    })

    it('should handle both tier and status being undefined', () => {
      // Should not crash when both are undefined
      expect(() => {
        render(<SubscriptionCard tier={undefined as unknown as User['subscription_tier']} status={undefined as unknown as User['subscription_status']} />)
      }).not.toThrow()

      expect(screen.getByText(/loading/i)).toBeInTheDocument()
    })

    it('should handle empty string tier', () => {
      // Should handle empty string gracefully
      expect(() => {
        render(<SubscriptionCard tier={'' as unknown as User['subscription_tier']} status="active" />)
      }).not.toThrow()
    })
  })
})
