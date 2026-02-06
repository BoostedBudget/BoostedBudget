import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { UpgradeModal } from './UpgradeModal'

describe('UpgradeModal', () => {
  const mockOnUpgrade = mock(() => {})
  const mockOnClose = mock(() => {})

  const currentTier = {
    id: 'premium',
    name: 'Premium',
    price_monthly: '$8.99',
    price_yearly: '$79.00',
    features: ['Unlimited rules', '30 accounts'],
  }

  const availableTiers = [
    {
      id: 'household_5',
      name: 'Household (5+)',
      price_monthly: '$18.99',
      price_yearly: '$179.00',
      features: ['Unlimited rules', '150 accounts', 'AI insights', 'API access'],
    },
  ]

  beforeEach(() => {
    mockOnUpgrade.mockClear()
    mockOnClose.mockClear()
  })

  it('should not render when open is false', () => {
    render(
      <UpgradeModal
        open={false}
        currentTier={currentTier}
        availableTiers={availableTiers}
        onUpgrade={mockOnUpgrade}
        onClose={mockOnClose}
      />
    )

    expect(screen.queryByText(/Upgrade Your Plan/i)).toBeNull()
  })

  it('should render when open is true', () => {
    render(
      <UpgradeModal
        open={true}
        currentTier={currentTier}
        availableTiers={availableTiers}
        onUpgrade={mockOnUpgrade}
        onClose={mockOnClose}
      />
    )

    expect(screen.getByText(/Upgrade Your Plan/i)).toBeDefined()
  })

  it('should display current tier information', () => {
    render(
      <UpgradeModal
        open={true}
        currentTier={currentTier}
        availableTiers={availableTiers}
        onUpgrade={mockOnUpgrade}
        onClose={mockOnClose}
      />
    )

    const premiumElements = screen.getAllByText(/Premium/i)
    expect(premiumElements.length).toBeGreaterThan(0)
    // Check for current tier features
    expect(screen.getByText(/Unlimited rules/i)).toBeDefined()
    expect(screen.getByText(/30 accounts/i)).toBeDefined()
  })

  it('should display available upgrade tiers', () => {
    render(
      <UpgradeModal
        open={true}
        currentTier={currentTier}
        availableTiers={availableTiers}
        onUpgrade={mockOnUpgrade}
        onClose={mockOnClose}
      />
    )

    const premiumPlusElements = screen.getAllByText(/Premium\+/i)
    expect(premiumPlusElements.length).toBeGreaterThan(0)
    expect(screen.getByText(/Unlimited rules/i)).toBeDefined()
    expect(screen.getByText(/AI insights/i)).toBeDefined()
  })

  it('should display monthly pricing by default', () => {
    render(
      <UpgradeModal
        open={true}
        currentTier={currentTier}
        availableTiers={availableTiers}
        onUpgrade={mockOnUpgrade}
        onClose={mockOnClose}
      />
    )

    expect(screen.getByText(/\$19\.99/i)).toBeDefined()
  })

  it('should toggle to yearly pricing when billing interval changed', () => {
    render(
      <UpgradeModal
        open={true}
        currentTier={currentTier}
        availableTiers={availableTiers}
        onUpgrade={mockOnUpgrade}
        onClose={mockOnClose}
      />
    )

    const yearlyToggle = screen.getByLabelText(/Yearly/i)
    fireEvent.click(yearlyToggle)

    expect(screen.getByText(/\$199\.99/i)).toBeDefined()
  })

  it('should call onUpgrade with correct parameters when upgrade button clicked', () => {
    render(
      <UpgradeModal
        open={true}
        currentTier={currentTier}
        availableTiers={availableTiers}
        onUpgrade={mockOnUpgrade}
        onClose={mockOnClose}
      />
    )

    const upgradeButton = screen.getByRole('button', { name: /Upgrade to Premium\+/i })
    fireEvent.click(upgradeButton)

    expect(mockOnUpgrade).toHaveBeenCalledWith('premium_plus', 'month')
  })

  it('should call onUpgrade with yearly interval when yearly selected', () => {
    render(
      <UpgradeModal
        open={true}
        currentTier={currentTier}
        availableTiers={availableTiers}
        onUpgrade={mockOnUpgrade}
        onClose={mockOnClose}
      />
    )

    const yearlyToggle = screen.getByLabelText(/Yearly/i)
    fireEvent.click(yearlyToggle)

    const upgradeButton = screen.getByRole('button', { name: /Upgrade to Premium\+/i })
    fireEvent.click(upgradeButton)

    expect(mockOnUpgrade).toHaveBeenCalledWith('premium_plus', 'year')
  })

  it('should call onClose when cancel button clicked', () => {
    render(
      <UpgradeModal
        open={true}
        currentTier={currentTier}
        availableTiers={availableTiers}
        onUpgrade={mockOnUpgrade}
        onClose={mockOnClose}
      />
    )

    const cancelButton = screen.getByRole('button', { name: /Cancel/i })
    fireEvent.click(cancelButton)

    expect(mockOnClose).toHaveBeenCalledTimes(1)
  })

  it('should display proration notice', () => {
    render(
      <UpgradeModal
        open={true}
        currentTier={currentTier}
        availableTiers={availableTiers}
        onUpgrade={mockOnUpgrade}
        onClose={mockOnClose}
      />
    )

    const prorationText = screen.getAllByText(/prorated immediately/i)
    expect(prorationText.length).toBeGreaterThan(0)
  })

  it('should show tier comparison with feature differences', () => {
    render(
      <UpgradeModal
        open={true}
        currentTier={currentTier}
        availableTiers={availableTiers}
        onUpgrade={mockOnUpgrade}
        onClose={mockOnClose}
      />
    )

    // Current tier features
    expect(screen.getByText(/Unlimited rules/i)).toBeDefined()
    expect(screen.getByText(/30 accounts/i)).toBeDefined()

    // New tier features
    expect(screen.getByText(/Unlimited rules/i)).toBeDefined()
    expect(screen.getByText(/Unlimited accounts/i)).toBeDefined()
  })
})
