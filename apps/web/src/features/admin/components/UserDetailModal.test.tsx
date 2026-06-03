/**
 * UserDetailModal Component Tests
 * Issue #644: Admin User Management UI
 *
 * Tests for user detail modal dialog.
 */

import { describe, it, expect, vi, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { UserDetailModal } from './UserDetailModal'

// Mock user data
const mockUser = {
  id: 'user-1',
  email: 'admin@example.com',
  subscription_tier: 'premium',
  subscription_status: 'active',
  primary_region: 'iad',
  created_at: '2024-01-15T10:00:00Z',
  last_login_at: '2024-03-15T14:30:00Z',
  is_active: true,
  linked_accounts_count: 5,
  automation_rules_count: 10,
  transactions_count: 1500,
  budgets_count: 4,
}

describe('UserDetailModal', () => {
  const mockOnClose = vi.fn()

  beforeEach(() => {
    mockOnClose.mockClear()
  })

  it('should not render when open is false', () => {
    render(
      <UserDetailModal
        user={mockUser}
        loading={false}
        open={false}
        onClose={mockOnClose}
      />
    )

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('should render when open is true', () => {
    render(
      <UserDetailModal
        user={mockUser}
        loading={false}
        open={true}
        onClose={mockOnClose}
      />
    )

    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('should show loading state when loading', () => {
    render(
      <UserDetailModal
        user={undefined}
        loading={true}
        open={true}
        onClose={mockOnClose}
      />
    )

    expect(screen.getByText(/loading/i)).toBeInTheDocument()
  })

  it('should display user email in title', () => {
    render(
      <UserDetailModal
        user={mockUser}
        loading={false}
        open={true}
        onClose={mockOnClose}
      />
    )

    expect(screen.getByText('admin@example.com')).toBeInTheDocument()
  })

  it('should display user profile information', () => {
    render(
      <UserDetailModal
        user={mockUser}
        loading={false}
        open={true}
        onClose={mockOnClose}
      />
    )

    // Subscription tier and status are displayed together as "Premium (active)"
    expect(screen.getByText(/premium/i)).toBeInTheDocument()
    expect(screen.getByText(/active/i)).toBeInTheDocument()
    expect(screen.getByText(/USA \(Virginia\)/i)).toBeInTheDocument()
  })

  it('should display user statistics', () => {
    render(
      <UserDetailModal
        user={mockUser}
        loading={false}
        open={true}
        onClose={mockOnClose}
      />
    )

    expect(screen.getByText('5')).toBeInTheDocument() // accounts
    expect(screen.getByText('10')).toBeInTheDocument() // rules
    expect(screen.getByText('1,500')).toBeInTheDocument() // transactions
    expect(screen.getByText('4')).toBeInTheDocument() // budgets
  })

  it('should display created at date', () => {
    render(
      <UserDetailModal
        user={mockUser}
        loading={false}
        open={true}
        onClose={mockOnClose}
      />
    )

    // Date is formatted - just check it's there
    expect(screen.getByText(/jan/i)).toBeInTheDocument()
  })

  it('should call onClose when close button is clicked', async () => {
    render(
      <UserDetailModal
        user={mockUser}
        loading={false}
        open={true}
        onClose={mockOnClose}
      />
    )

    // Find the footer close button (not the dialog X button)
    const closeButtons = screen.getAllByRole('button', { name: /close/i })
    // Click the last one (footer button)
    await userEvent.click(closeButtons[closeButtons.length - 1])

    expect(mockOnClose).toHaveBeenCalledTimes(1)
  })
})
