import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { BillingDetails } from './BillingDetails'

describe('BillingDetails', () => {
  const defaultProps = {
    nextBillingDate: '2024-03-15',
    amount: '19.99',
    currency: 'USD',
    billingInterval: 'month',
    portalUrl: 'https://billing.paddle.com/portal',
  }

  it('should render heading', () => {
    render(<BillingDetails {...defaultProps} />)

    expect(screen.getByText(/Billing Details/i)).toBeDefined()
  })

  it('should display next billing date', () => {
    render(<BillingDetails {...defaultProps} />)

    expect(screen.getByText(/2024-03-15/i)).toBeDefined()
    expect(screen.getByText(/Next Billing Date/i)).toBeDefined()
  })

  it('should display billing amount', () => {
    render(<BillingDetails {...defaultProps} />)

    expect(screen.getByText(/19\.99/i)).toBeDefined()
    expect(screen.getByText(/USD/i)).toBeDefined()
  })

  it('should display monthly billing interval', () => {
    render(<BillingDetails {...defaultProps} />)

    expect(screen.getByText(/Monthly/i)).toBeDefined()
  })

  it('should display yearly billing interval', () => {
    render(<BillingDetails {...defaultProps} billingInterval="year" />)

    expect(screen.getByText(/Yearly/i)).toBeDefined()
  })

  it('should have link to billing portal', () => {
    render(<BillingDetails {...defaultProps} />)

    const link = screen.getByRole('link', { name: /Manage Payment Methods/i })
    expect(link).toBeDefined()
    expect(link.getAttribute('href')).toBe('https://billing.paddle.com/portal')
  })

  it('should open portal link in new tab', () => {
    render(<BillingDetails {...defaultProps} />)

    const link = screen.getByRole('link', { name: /Manage Payment Methods/i })
    expect(link.getAttribute('target')).toBe('_blank')
    expect(link.getAttribute('rel')).toContain('noopener')
    expect(link.getAttribute('rel')).toContain('noreferrer')
  })

  it('should not render portal link when url is not provided', () => {
    render(<BillingDetails {...defaultProps} portalUrl={undefined} />)

    expect(screen.queryByRole('link', { name: /Manage Payment Methods/i })).toBeNull()
  })

  it('should display billing cycle information', () => {
    render(<BillingDetails {...defaultProps} />)

    expect(screen.getByText(/Billing Cycle/i)).toBeDefined()
  })

  it('should format next billing date correctly', () => {
    render(<BillingDetails {...defaultProps} nextBillingDate="2024-12-31" />)

    expect(screen.getByText(/2024-12-31/i)).toBeDefined()
  })

  it('should handle missing optional fields gracefully', () => {
    render(
      <BillingDetails
        nextBillingDate="2024-03-15"
        amount="0.00"
        currency="USD"
        billingInterval="month"
      />
    )

    expect(screen.getByText(/2024-03-15/i)).toBeDefined()
    expect(screen.getByText(/0\.00/i)).toBeDefined()
  })

  it('should display payment method section', () => {
    render(<BillingDetails {...defaultProps} paymentMethod="•••• 4242" />)

    expect(screen.getByText(/Payment Method:/i)).toBeDefined()
    expect(screen.getByText(/•••• 4242/i)).toBeDefined()
  })

  it('should not display payment method when not provided', () => {
    render(<BillingDetails {...defaultProps} />)

    expect(screen.queryByText(/Payment Method:/i)).toBeNull()
  })

  it('should have proper card structure', () => {
    const { container } = render(<BillingDetails {...defaultProps} />)

    const card = container.querySelector('.billing-details-card')
    expect(card).toBeDefined()
  })
})
