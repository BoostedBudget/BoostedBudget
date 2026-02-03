import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { CancelConfirmationModal } from './CancelConfirmationModal'

describe('CancelConfirmationModal', () => {
  const mockOnConfirm = mock(() => {})
  const mockOnClose = mock(() => {})

  const subscription = {
    tier: 'Premium+',
    status: 'active',
    billing_interval: 'month',
    amount: '19.99',
    currency: 'USD',
    next_billing_date: '2024-03-15',
    features: ['Unlimited rules', 'Unlimited accounts', 'AI insights', 'API access'],
  }

  beforeEach(() => {
    mockOnConfirm.mockClear()
    mockOnClose.mockClear()
  })

  it('should not render when open is false', () => {
    render(
      <CancelConfirmationModal
        open={false}
        subscription={subscription}
        onConfirm={mockOnConfirm}
        onClose={mockOnClose}
      />
    )

    expect(screen.queryByText(/Cancel Subscription/i)).toBeNull()
  })

  it('should render first confirmation dialog when open is true', () => {
    render(
      <CancelConfirmationModal
        open={true}
        subscription={subscription}
        onConfirm={mockOnConfirm}
        onClose={mockOnClose}
      />
    )

    expect(screen.getByText(/Cancel Subscription/i)).toBeDefined()
    expect(screen.getByText(/Are you sure/i)).toBeDefined()
  })

  it('should display final billing date', () => {
    render(
      <CancelConfirmationModal
        open={true}
        subscription={subscription}
        onConfirm={mockOnConfirm}
        onClose={mockOnClose}
      />
    )

    expect(screen.getByText(/2024-03-15/i)).toBeDefined()
  })

  it('should display subscription tier', () => {
    render(
      <CancelConfirmationModal
        open={true}
        subscription={subscription}
        onConfirm={mockOnConfirm}
        onClose={mockOnClose}
      />
    )

    const tierElements = screen.getAllByText(/Premium\+/i)
    expect(tierElements.length).toBeGreaterThan(0)
  })

  it('should display features that will be lost', () => {
    render(
      <CancelConfirmationModal
        open={true}
        subscription={subscription}
        onConfirm={mockOnConfirm}
        onClose={mockOnClose}
      />
    )

    const aiElements = screen.getAllByText(/AI insights/i)
    expect(aiElements.length).toBeGreaterThan(0)
    const apiElements = screen.getAllByText(/API access/i)
    expect(apiElements.length).toBeGreaterThan(0)
  })

  it('should have cancel button in first dialog', () => {
    render(
      <CancelConfirmationModal
        open={true}
        subscription={subscription}
        onConfirm={mockOnConfirm}
        onClose={mockOnClose}
      />
    )

    expect(screen.getByRole('button', { name: /Keep Subscription/i })).toBeDefined()
  })

  it('should have proceed button in first dialog', () => {
    render(
      <CancelConfirmationModal
        open={true}
        subscription={subscription}
        onConfirm={mockOnConfirm}
        onClose={mockOnClose}
      />
    )

    expect(screen.getByRole('button', { name: /Yes, Cancel/i })).toBeDefined()
  })

  it('should call onClose when Keep Subscription clicked', () => {
    render(
      <CancelConfirmationModal
        open={true}
        subscription={subscription}
        onConfirm={mockOnConfirm}
        onClose={mockOnClose}
      />
    )

    const keepButton = screen.getByRole('button', { name: /Keep Subscription/i })
    fireEvent.click(keepButton)

    expect(mockOnClose).toHaveBeenCalledTimes(1)
  })

  it('should show second confirmation dialog when Yes Cancel clicked', () => {
    render(
      <CancelConfirmationModal
        open={true}
        subscription={subscription}
        onConfirm={mockOnConfirm}
        onClose={mockOnClose}
      />
    )

    const yesCancelButton = screen.getByRole('button', { name: /Yes, Cancel/i })
    fireEvent.click(yesCancelButton)

    expect(screen.getByText(/This action is permanent/i)).toBeDefined()
  })

  it('should have text input for typing CANCEL in second dialog', () => {
    render(
      <CancelConfirmationModal
        open={true}
        subscription={subscription}
        onConfirm={mockOnConfirm}
        onClose={mockOnClose}
      />
    )

    const yesCancelButton = screen.getByRole('button', { name: /Yes, Cancel/i })
    fireEvent.click(yesCancelButton)

    const input = screen.getByPlaceholderText(/Type CANCEL/i)
    expect(input).toBeDefined()
  })

  it('should disable confirm button until CANCEL is typed correctly', () => {
    render(
      <CancelConfirmationModal
        open={true}
        subscription={subscription}
        onConfirm={mockOnConfirm}
        onClose={mockOnClose}
      />
    )

    const yesCancelButton = screen.getByRole('button', { name: /Yes, Cancel/i })
    fireEvent.click(yesCancelButton)

    const confirmButton = screen.getByRole('button', { name: /Confirm Cancellation/i })
    expect(confirmButton).toBeDisabled()
  })

  it('should enable confirm button when CANCEL is typed', () => {
    render(
      <CancelConfirmationModal
        open={true}
        subscription={subscription}
        onConfirm={mockOnConfirm}
        onClose={mockOnClose}
      />
    )

    const yesCancelButton = screen.getByRole('button', { name: /Yes, Cancel/i })
    fireEvent.click(yesCancelButton)

    const input = screen.getByPlaceholderText(/Type CANCEL/i)
    fireEvent.change(input, { target: { value: 'CANCEL' } })

    const confirmButton = screen.getByRole('button', { name: /Confirm Cancellation/i })
    expect(confirmButton).not.toBeDisabled()
  })

  it('should call onConfirm when confirm button clicked with correct text', () => {
    render(
      <CancelConfirmationModal
        open={true}
        subscription={subscription}
        onConfirm={mockOnConfirm}
        onClose={mockOnClose}
      />
    )

    const yesCancelButton = screen.getByRole('button', { name: /Yes, Cancel/i })
    fireEvent.click(yesCancelButton)

    const input = screen.getByPlaceholderText(/Type CANCEL/i)
    fireEvent.change(input, { target: { value: 'CANCEL' } })

    const confirmButton = screen.getByRole('button', { name: /Confirm Cancellation/i })
    fireEvent.click(confirmButton)

    expect(mockOnConfirm).toHaveBeenCalledTimes(1)
  })

  it('should have go back button in second dialog', () => {
    render(
      <CancelConfirmationModal
        open={true}
        subscription={subscription}
        onConfirm={mockOnConfirm}
        onClose={mockOnClose}
      />
    )

    const yesCancelButton = screen.getByRole('button', { name: /Yes, Cancel/i })
    fireEvent.click(yesCancelButton)

    expect(screen.getByRole('button', { name: /Go Back/i })).toBeDefined()
  })

  it('should go back to first dialog when Go Back clicked', () => {
    render(
      <CancelConfirmationModal
        open={true}
        subscription={subscription}
        onConfirm={mockOnConfirm}
        onClose={mockOnClose}
      />
    )

    const yesCancelButton = screen.getByRole('button', { name: /Yes, Cancel/i })
    fireEvent.click(yesCancelButton)

    const goBackButton = screen.getByRole('button', { name: /Go Back/i })
    fireEvent.click(goBackButton)

    expect(screen.getByText(/Are you sure/i)).toBeDefined()
    expect(screen.queryByText(/This action is permanent/i)).toBeNull()
  })
})
