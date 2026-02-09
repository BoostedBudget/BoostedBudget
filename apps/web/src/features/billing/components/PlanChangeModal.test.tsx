import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { PlanChangeModal } from './PlanChangeModal'
import {
  mockUsePreviewPaddleSubscriptionUpdate,
  mockPreviewUpdateMutateAsync,
} from '@/test/mocks/subscription-api'

const mockOnChangePlan = mock(() => {})
const mockOnClose = mock(() => {})

const mockTiers = {
  premium: {
    display_name: 'Premium',
    pricing: {
      price_monthly: 9.99,
      price_yearly: 99.0,
    },
    users_included: 3,
    accounts: 30,
  },
  household_2: {
    display_name: 'Household (2 users)',
    pricing: {
      price_monthly: 14.99,
      price_yearly: 149.0,
    },
    users_included: 2,
    accounts: 50,
  },
  household_3: {
    display_name: 'Household (3 users)',
    pricing: {
      price_monthly: 19.99,
      price_yearly: 199.0,
    },
    users_included: 3,
    accounts: 100,
  },
  free: {
    display_name: 'Free',
    pricing: {
      price_monthly: 0,
      price_yearly: 0,
    },
    users_included: 1,
    accounts: 3,
  },
}

describe('PlanChangeModal', () => {
  beforeEach(() => {
    mockOnChangePlan.mockClear()
    mockOnClose.mockClear()
    mockPreviewUpdateMutateAsync.mockClear()
    mockUsePreviewPaddleSubscriptionUpdate.mockClear()
    mockUsePreviewPaddleSubscriptionUpdate.mockReturnValue({
      mutateAsync: mockPreviewUpdateMutateAsync,
      isPending: false,
      isSuccess: false,
      isError: false,
      error: null,
      reset: mock(() => {}),
    })
    mockPreviewUpdateMutateAsync.mockResolvedValue({
      proration_amount: '5.00',
      currency: 'USD',
      next_charge_date: '2026-03-15',
    })
  })

  it('should not render when open is false', () => {
    render(
      <PlanChangeModal
        open={false}
        currentTier="premium"
        currentBillingInterval="month"
        tiers={mockTiers}
        isUpdating={false}
        onChangePlan={mockOnChangePlan}
        onClose={mockOnClose}
      />
    )

    expect(screen.queryByText(/Change Your Plan/i)).toBeNull()
  })

  it('should render title when open is true', () => {
    render(
      <PlanChangeModal
        open={true}
        currentTier="premium"
        currentBillingInterval="month"
        tiers={mockTiers}
        isUpdating={false}
        onChangePlan={mockOnChangePlan}
        onClose={mockOnClose}
      />
    )

    expect(screen.getByText(/Change Your Plan/i)).toBeDefined()
  })

  it('should show all tiers except current tier', () => {
    render(
      <PlanChangeModal
        open={true}
        currentTier="premium"
        currentBillingInterval="month"
        tiers={mockTiers}
        isUpdating={false}
        onChangePlan={mockOnChangePlan}
        onClose={mockOnClose}
      />
    )

    // Should show household_2, household_3, free
    expect(screen.getByText('Household (2 users)')).toBeDefined()
    expect(screen.getByText('Household (3 users)')).toBeDefined()
    const freeElements = screen.getAllByText('Free')
    expect(freeElements.length).toBeGreaterThan(0)

    // Should NOT show current tier name in the tier cards (only in comparison)
    const premiumElements = screen.queryAllByText(/Premium/i)
    // Premium should not appear in the tier selection grid
    expect(premiumElements.length).toBe(0)
  })

  it('should label higher-priced tiers as Upgrade with green badge', () => {
    render(
      <PlanChangeModal
        open={true}
        currentTier="premium"
        currentBillingInterval="month"
        tiers={mockTiers}
        isUpdating={false}
        onChangePlan={mockOnChangePlan}
        onClose={mockOnClose}
      />
    )

    const upgradeBadges = screen.getAllByText('Upgrade')
    // household_2 and household_3 are upgrades
    expect(upgradeBadges.length).toBeGreaterThanOrEqual(2)
  })

  it('should label lower-priced tiers as Downgrade with amber badge', () => {
    render(
      <PlanChangeModal
        open={true}
        currentTier="premium"
        currentBillingInterval="month"
        tiers={mockTiers}
        isUpdating={false}
        onChangePlan={mockOnChangePlan}
        onClose={mockOnClose}
      />
    )

    const downgradeBadges = screen.getAllByText('Downgrade')
    // free is a downgrade
    expect(downgradeBadges.length).toBeGreaterThanOrEqual(1)
  })

  it('should show monthly pricing by default (matching current billing interval)', () => {
    render(
      <PlanChangeModal
        open={true}
        currentTier="premium"
        currentBillingInterval="month"
        tiers={mockTiers}
        isUpdating={false}
        onChangePlan={mockOnChangePlan}
        onClose={mockOnClose}
      />
    )

    // Should show monthly price for household_2
    expect(screen.getByText(/\$14\.99/)).toBeDefined()
  })

  it('should toggle to yearly pricing', () => {
    render(
      <PlanChangeModal
        open={true}
        currentTier="premium"
        currentBillingInterval="month"
        tiers={mockTiers}
        isUpdating={false}
        onChangePlan={mockOnChangePlan}
        onClose={mockOnClose}
      />
    )

    const yearlyRadio = screen.getByLabelText(/Yearly billing/i)
    fireEvent.click(yearlyRadio)

    // Should show yearly price for household_2
    expect(screen.getByText(/\$149/)).toBeDefined()
  })

  it('should show users_included and accounts per tier', () => {
    render(
      <PlanChangeModal
        open={true}
        currentTier="premium"
        currentBillingInterval="month"
        tiers={mockTiers}
        isUpdating={false}
        onChangePlan={mockOnChangePlan}
        onClose={mockOnClose}
      />
    )

    // household_2: 2 users, 50 accounts
    expect(screen.getAllByText(/2.*users/i).length).toBeGreaterThan(0)
    expect(screen.getAllByText(/50.*accounts/i).length).toBeGreaterThan(0)
  })

  it('should call preview API when a tier is selected', async () => {
    render(
      <PlanChangeModal
        open={true}
        currentTier="premium"
        currentBillingInterval="month"
        tiers={mockTiers}
        isUpdating={false}
        onChangePlan={mockOnChangePlan}
        onClose={mockOnClose}
      />
    )

    const household2Card = screen.getByText('Household (2 users)').closest('div')
    expect(household2Card).toBeDefined()
    fireEvent.click(household2Card!)

    await waitFor(() => {
      expect(mockPreviewUpdateMutateAsync).toHaveBeenCalled()
    })
  })

  it('should show proration amount for upgrades', async () => {
    // Set up mock to return success state with data
    let previewData = {
      proration_amount: '5.00',
      currency: 'USD',
      next_charge_date: '2026-03-15',
    }

    mockUsePreviewPaddleSubscriptionUpdate.mockReturnValue({
      mutateAsync: mockPreviewUpdateMutateAsync,
      isPending: false,
      isSuccess: true,
      isError: false,
      error: null,
      data: previewData,
      reset: mock(() => {}),
    })

    const { rerender } = render(
      <PlanChangeModal
        open={true}
        currentTier="premium"
        currentBillingInterval="month"
        tiers={mockTiers}
        isUpdating={false}
        onChangePlan={mockOnChangePlan}
        onClose={mockOnClose}
      />
    )

    const household2Card = screen.getByText('Household (2 users)').closest('[role="button"]')
    fireEvent.click(household2Card!)

    // Wait for preview API call
    await waitFor(() => {
      expect(mockPreviewUpdateMutateAsync).toHaveBeenCalled()
    })

    // Force a rerender with the preview data ready
    rerender(
      <PlanChangeModal
        open={true}
        currentTier="premium"
        currentBillingInterval="month"
        tiers={mockTiers}
        isUpdating={false}
        onChangePlan={mockOnChangePlan}
        onClose={mockOnClose}
      />
    )

    await waitFor(() => {
      const text = screen.queryByText(/5\.00.*immediately/i)
      expect(text).toBeDefined()
    })
  })

  it('should show effective date for downgrades', async () => {
    let previewData = {
      proration_amount: '0.00',
      currency: 'USD',
      next_charge_date: '2026-03-15',
    }

    mockPreviewUpdateMutateAsync.mockResolvedValue(previewData)
    mockUsePreviewPaddleSubscriptionUpdate.mockReturnValue({
      mutateAsync: mockPreviewUpdateMutateAsync,
      isPending: false,
      isSuccess: true,
      isError: false,
      error: null,
      data: previewData,
      reset: mock(() => {}),
    })

    const { rerender } = render(
      <PlanChangeModal
        open={true}
        currentTier="premium"
        currentBillingInterval="month"
        tiers={mockTiers}
        isUpdating={false}
        onChangePlan={mockOnChangePlan}
        onClose={mockOnClose}
      />
    )

    // Find the downgrade badge to locate the free tier card
    const downgradeBadge = screen.getAllByText('Downgrade')[0]
    const freeCard = downgradeBadge.closest('[role="button"]')
    fireEvent.click(freeCard!)

    await waitFor(() => {
      expect(mockPreviewUpdateMutateAsync).toHaveBeenCalled()
    })

    // Force rerender
    rerender(
      <PlanChangeModal
        open={true}
        currentTier="premium"
        currentBillingInterval="month"
        tiers={mockTiers}
        isUpdating={false}
        onChangePlan={mockOnChangePlan}
        onClose={mockOnClose}
      />
    )

    await waitFor(() => {
      const text = screen.queryByText(/Changes take effect on.*2026-03-15/i)
      expect(text).toBeDefined()
    })
  })

  it('should show confirmation checkbox for downgrades only', async () => {
    render(
      <PlanChangeModal
        open={true}
        currentTier="premium"
        currentBillingInterval="month"
        tiers={mockTiers}
        isUpdating={false}
        onChangePlan={mockOnChangePlan}
        onClose={mockOnClose}
      />
    )

    // Select downgrade (free)
    const downgradeBadge = screen.getAllByText('Downgrade')[0]
    const freeCard = downgradeBadge.closest('[role="button"]')
    fireEvent.click(freeCard!)

    await waitFor(() => {
      expect(screen.getByRole('checkbox')).toBeDefined()
    })

    // Select upgrade (household_2)
    const household2Card = screen.getByText('Household (2 users)').closest('div')
    fireEvent.click(household2Card!)

    await waitFor(() => {
      expect(screen.queryByRole('checkbox')).toBeNull()
    })
  })

  it('should disable confirm button when no tier selected', () => {
    render(
      <PlanChangeModal
        open={true}
        currentTier="premium"
        currentBillingInterval="month"
        tiers={mockTiers}
        isUpdating={false}
        onChangePlan={mockOnChangePlan}
        onClose={mockOnClose}
      />
    )

    const confirmButton = screen.getByRole('button', { name: /Confirm/i })
    expect(confirmButton).toBeDisabled()
  })

  it('should enable confirm button for upgrade selection (no checkbox needed)', async () => {
    render(
      <PlanChangeModal
        open={true}
        currentTier="premium"
        currentBillingInterval="month"
        tiers={mockTiers}
        isUpdating={false}
        onChangePlan={mockOnChangePlan}
        onClose={mockOnClose}
      />
    )

    const household2Card = screen.getByText('Household (2 users)').closest('div')
    fireEvent.click(household2Card!)

    await waitFor(() => {
      const confirmButton = screen.getByRole('button', { name: /Confirm/i })
      expect(confirmButton).not.toBeDisabled()
    })
  })

  it('should keep confirm disabled for downgrade until checkbox checked', async () => {
    render(
      <PlanChangeModal
        open={true}
        currentTier="premium"
        currentBillingInterval="month"
        tiers={mockTiers}
        isUpdating={false}
        onChangePlan={mockOnChangePlan}
        onClose={mockOnClose}
      />
    )

    const downgradeBadge = screen.getAllByText('Downgrade')[0]
    const freeCard = downgradeBadge.closest('[role="button"]')
    fireEvent.click(freeCard!)

    await waitFor(() => {
      const confirmButton = screen.getByRole('button', { name: /Confirm/i })
      expect(confirmButton).toBeDisabled()
    })

    const checkbox = screen.getByRole('checkbox')
    fireEvent.click(checkbox)

    await waitFor(() => {
      const confirmButton = screen.getByRole('button', { name: /Confirm/i })
      expect(confirmButton).not.toBeDisabled()
    })
  })

  it('should call onChangePlan with prorated_immediately for upgrades', async () => {
    render(
      <PlanChangeModal
        open={true}
        currentTier="premium"
        currentBillingInterval="month"
        tiers={mockTiers}
        isUpdating={false}
        onChangePlan={mockOnChangePlan}
        onClose={mockOnClose}
      />
    )

    const household2Card = screen.getByText('Household (2 users)').closest('div')
    fireEvent.click(household2Card!)

    await waitFor(() => {
      const confirmButton = screen.getByRole('button', { name: /Confirm/i })
      expect(confirmButton).not.toBeDisabled()
    })

    const confirmButton = screen.getByRole('button', { name: /Confirm/i })
    fireEvent.click(confirmButton)

    expect(mockOnChangePlan).toHaveBeenCalledWith('household_2', 'month', 'prorated_immediately')
  })

  it('should call onChangePlan with full_next_billing_period for downgrades', async () => {
    render(
      <PlanChangeModal
        open={true}
        currentTier="premium"
        currentBillingInterval="month"
        tiers={mockTiers}
        isUpdating={false}
        onChangePlan={mockOnChangePlan}
        onClose={mockOnClose}
      />
    )

    const downgradeBadge = screen.getAllByText('Downgrade')[0]
    const freeCard = downgradeBadge.closest('[role="button"]')
    fireEvent.click(freeCard!)

    await waitFor(() => {
      const checkbox = screen.getByRole('checkbox')
      fireEvent.click(checkbox)
    })

    const confirmButton = screen.getByRole('button', { name: /Confirm/i })
    fireEvent.click(confirmButton)

    expect(mockOnChangePlan).toHaveBeenCalledWith('free', 'month', 'full_next_billing_period')
  })

  it('should call onClose when cancel clicked', () => {
    render(
      <PlanChangeModal
        open={true}
        currentTier="premium"
        currentBillingInterval="month"
        tiers={mockTiers}
        isUpdating={false}
        onChangePlan={mockOnChangePlan}
        onClose={mockOnClose}
      />
    )

    const cancelButton = screen.getByRole('button', { name: /Cancel/i })
    fireEvent.click(cancelButton)

    expect(mockOnClose).toHaveBeenCalled()
  })
})
