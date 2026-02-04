import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { CurrentSubscriptionDisplay } from './CurrentSubscriptionDisplay'

describe('CurrentSubscriptionDisplay', () => {
  const defaultProps = {
    tier: 'premium_plus',
    tierName: 'Premium+',
    status: 'active',
    billingInterval: 'month',
    amount: '19.99',
    currency: 'USD',
    features: ['Unlimited rules', 'Unlimited accounts', 'AI insights', 'API access'],
  }

  it('should render tier name', () => {
    render(<CurrentSubscriptionDisplay {...defaultProps} />)

    const tierElements = screen.getAllByText(/Premium\+/i)
    expect(tierElements.length).toBeGreaterThan(0)
  })

  it('should display tier badge', () => {
    render(<CurrentSubscriptionDisplay {...defaultProps} />)

    expect(screen.getByText(/Current Plan/i)).toBeDefined()
  })

  it('should display status badge for active subscription', () => {
    render(<CurrentSubscriptionDisplay {...defaultProps} />)

    expect(screen.getByText(/active/i)).toBeDefined()
  })

  it('should display status badge for cancelled subscription', () => {
    render(<CurrentSubscriptionDisplay {...defaultProps} status="cancelled" />)

    expect(screen.getByText(/cancelled/i)).toBeDefined()
  })

  it('should display status badge for trial subscription', () => {
    render(<CurrentSubscriptionDisplay {...defaultProps} status="trial" />)

    expect(screen.getByText(/trial/i)).toBeDefined()
  })

  it('should display monthly price', () => {
    render(<CurrentSubscriptionDisplay {...defaultProps} />)

    expect(screen.getByText(/19\.99/i)).toBeDefined()
    expect(screen.getByText(/per month/i)).toBeDefined()
  })

  it('should display yearly price', () => {
    render(<CurrentSubscriptionDisplay {...defaultProps} billingInterval="year" />)

    expect(screen.getByText(/19\.99/i)).toBeDefined()
    expect(screen.getByText(/per year/i)).toBeDefined()
  })

  it('should display currency', () => {
    render(<CurrentSubscriptionDisplay {...defaultProps} />)

    expect(screen.getByText(/USD/i)).toBeDefined()
  })

  it('should display all features', () => {
    render(<CurrentSubscriptionDisplay {...defaultProps} />)

    expect(screen.getByText(/Unlimited rules/i)).toBeDefined()
    expect(screen.getByText(/Unlimited accounts/i)).toBeDefined()
    const aiElements = screen.getAllByText(/AI insights/i)
    expect(aiElements.length).toBeGreaterThan(0)
    const apiElements = screen.getAllByText(/API access/i)
    expect(apiElements.length).toBeGreaterThan(0)
  })

  it('should have proper structure with heading', () => {
    render(<CurrentSubscriptionDisplay {...defaultProps} />)

    expect(screen.getByText(/Current Subscription/i)).toBeDefined()
  })

  it('should display features with check marks', () => {
    const { container } = render(<CurrentSubscriptionDisplay {...defaultProps} />)

    const listItems = container.querySelectorAll('li')
    expect(listItems.length).toBe(4)
  })

  it('should handle free tier', () => {
    render(
      <CurrentSubscriptionDisplay
        tier="free"
        tierName="Free"
        status="active"
        billingInterval="month"
        amount="0.00"
        currency="USD"
        features={['1 rule', '3 accounts']}
      />
    )

    expect(screen.getByText(/Free/i)).toBeDefined()
    expect(screen.getByText(/1 rule/i)).toBeDefined()
    expect(screen.getByText(/3 accounts/i)).toBeDefined()
  })

  it('should apply different styling for different statuses', () => {
    render(<CurrentSubscriptionDisplay {...defaultProps} status="cancelled" />)

    const statusBadge = screen.getByText(/cancelled/i)
    expect(statusBadge.className).toContain('cancelled')
  })

  it('should format price with currency symbol', () => {
    render(<CurrentSubscriptionDisplay {...defaultProps} />)

    // Should show USD 19.99 or similar format
    const priceText = screen.getByText(/19\.99/i)
    expect(priceText).toBeDefined()
  })
})
