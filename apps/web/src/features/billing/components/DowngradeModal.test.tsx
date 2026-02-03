import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { DowngradeModal } from './DowngradeModal'

describe('DowngradeModal', () => {
  const mockOnDowngrade = mock(() => {})
  const mockOnClose = mock(() => {})

  const currentTier = {
    id: 'premium_plus',
    name: 'Premium+',
    price_monthly: '$19.99',
    features: ['Unlimited rules', 'Unlimited accounts', 'AI insights', 'API access'],
  }

  const downgradeTier = {
    id: 'premium',
    name: 'Premium',
    price_monthly: '$9.99',
    features: ['15 rules', '30 accounts'],
  }

  const nextBillingDate = '2024-03-15'

  beforeEach(() => {
    mockOnDowngrade.mockClear()
    mockOnClose.mockClear()
  })

  it('should not render when open is false', () => {
    render(
      <DowngradeModal
        open={false}
        currentTier={currentTier}
        downgradeTier={downgradeTier}
        nextBillingDate={nextBillingDate}
        onDowngrade={mockOnDowngrade}
        onClose={mockOnClose}
      />
    )

    expect(screen.queryByText(/Downgrade Your Plan/i)).toBeNull()
  })

  it('should render when open is true', () => {
    render(
      <DowngradeModal
        open={true}
        currentTier={currentTier}
        downgradeTier={downgradeTier}
        nextBillingDate={nextBillingDate}
        onDowngrade={mockOnDowngrade}
        onClose={mockOnClose}
      />
    )

    expect(screen.getByText(/Downgrade Your Plan/i)).toBeDefined()
  })

  it('should display warning about feature loss', () => {
    render(
      <DowngradeModal
        open={true}
        currentTier={currentTier}
        downgradeTier={downgradeTier}
        nextBillingDate={nextBillingDate}
        onDowngrade={mockOnDowngrade}
        onClose={mockOnClose}
      />
    )

    const warningText = screen.getAllByText(/you will lose access/i)
    expect(warningText.length).toBeGreaterThan(0)
  })

  it('should display current tier features', () => {
    render(
      <DowngradeModal
        open={true}
        currentTier={currentTier}
        downgradeTier={downgradeTier}
        nextBillingDate={nextBillingDate}
        onDowngrade={mockOnDowngrade}
        onClose={mockOnClose}
      />
    )

    const unlimitedRulesElements = screen.getAllByText(/Unlimited rules/i)
    expect(unlimitedRulesElements.length).toBeGreaterThan(0)
    const aiInsightsElements = screen.getAllByText(/AI insights/i)
    expect(aiInsightsElements.length).toBeGreaterThan(0)
    const apiAccessElements = screen.getAllByText(/API access/i)
    expect(apiAccessElements.length).toBeGreaterThan(0)
  })

  it('should display downgrade tier features', () => {
    render(
      <DowngradeModal
        open={true}
        currentTier={currentTier}
        downgradeTier={downgradeTier}
        nextBillingDate={nextBillingDate}
        onDowngrade={mockOnDowngrade}
        onClose={mockOnClose}
      />
    )

    expect(screen.getByText(/15 rules/i)).toBeDefined()
    expect(screen.getByText(/30 accounts/i)).toBeDefined()
  })

  it('should display effective date notice', () => {
    render(
      <DowngradeModal
        open={true}
        currentTier={currentTier}
        downgradeTier={downgradeTier}
        nextBillingDate={nextBillingDate}
        onDowngrade={mockOnDowngrade}
        onClose={mockOnClose}
      />
    )

    expect(screen.getByText(/2024-03-15/i)).toBeDefined()
    expect(screen.getByText(/end of your current billing period/i)).toBeDefined()
  })

  it('should have confirmation checkbox', () => {
    render(
      <DowngradeModal
        open={true}
        currentTier={currentTier}
        downgradeTier={downgradeTier}
        nextBillingDate={nextBillingDate}
        onDowngrade={mockOnDowngrade}
        onClose={mockOnClose}
      />
    )

    const checkbox = screen.getByRole('checkbox')
    expect(checkbox).toBeDefined()
    expect(checkbox).not.toBeChecked()
  })

  it('should disable confirm button until checkbox is checked', () => {
    render(
      <DowngradeModal
        open={true}
        currentTier={currentTier}
        downgradeTier={downgradeTier}
        nextBillingDate={nextBillingDate}
        onDowngrade={mockOnDowngrade}
        onClose={mockOnClose}
      />
    )

    const confirmButton = screen.getByRole('button', { name: /Confirm Downgrade/i })
    expect(confirmButton).toBeDisabled()
  })

  it('should enable confirm button when checkbox is checked', () => {
    render(
      <DowngradeModal
        open={true}
        currentTier={currentTier}
        downgradeTier={downgradeTier}
        nextBillingDate={nextBillingDate}
        onDowngrade={mockOnDowngrade}
        onClose={mockOnClose}
      />
    )

    const checkbox = screen.getByRole('checkbox')
    const confirmButton = screen.getByRole('button', { name: /Confirm Downgrade/i })

    fireEvent.click(checkbox)

    expect(confirmButton).not.toBeDisabled()
  })

  it('should call onDowngrade with tier id when confirm button clicked', () => {
    render(
      <DowngradeModal
        open={true}
        currentTier={currentTier}
        downgradeTier={downgradeTier}
        nextBillingDate={nextBillingDate}
        onDowngrade={mockOnDowngrade}
        onClose={mockOnClose}
      />
    )

    const checkbox = screen.getByRole('checkbox')
    fireEvent.click(checkbox)

    const confirmButton = screen.getByRole('button', { name: /Confirm Downgrade/i })
    fireEvent.click(confirmButton)

    expect(mockOnDowngrade).toHaveBeenCalledWith('premium')
  })

  it('should call onClose when cancel button clicked', () => {
    render(
      <DowngradeModal
        open={true}
        currentTier={currentTier}
        downgradeTier={downgradeTier}
        nextBillingDate={nextBillingDate}
        onDowngrade={mockOnDowngrade}
        onClose={mockOnClose}
      />
    )

    const cancelButton = screen.getByRole('button', { name: /Cancel/i })
    fireEvent.click(cancelButton)

    expect(mockOnClose).toHaveBeenCalledTimes(1)
  })

  it('should show features that will be lost', () => {
    render(
      <DowngradeModal
        open={true}
        currentTier={currentTier}
        downgradeTier={downgradeTier}
        nextBillingDate={nextBillingDate}
        onDowngrade={mockOnDowngrade}
        onClose={mockOnClose}
      />
    )

    // AI insights and API access are in current tier but not in downgrade tier
    const aiInsightsElements = screen.getAllByText(/AI insights/i)
    expect(aiInsightsElements.length).toBeGreaterThan(0)
    const apiAccessElements = screen.getAllByText(/API access/i)
    expect(apiAccessElements.length).toBeGreaterThan(0)
  })

  it('should display tier names in comparison', () => {
    render(
      <DowngradeModal
        open={true}
        currentTier={currentTier}
        downgradeTier={downgradeTier}
        nextBillingDate={nextBillingDate}
        onDowngrade={mockOnDowngrade}
        onClose={mockOnClose}
      />
    )

    const premiumPlusElements = screen.getAllByText(/Premium\+/i)
    expect(premiumPlusElements.length).toBeGreaterThan(0)

    const premiumElements = screen.getAllByText(/^Premium$/i)
    expect(premiumElements.length).toBeGreaterThan(0)
  })
})
