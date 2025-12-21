/**
 * AccountCustomizationModal Component Tests
 *
 * Tests for the modal dialog that allows users to customize
 * their account appearance including nickname, color, icon/emoji, and visibility.
 *
 * Issue #307: Account Customization
 * Issue #544: Add unit tests for account customization components
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AccountCustomizationModal } from './AccountCustomizationModal'
import type { Account } from '../types'

// Mock child components to simplify testing
vi.mock('./ColorPicker', () => ({
  ColorPicker: ({
    value,
    onChange,
  }: {
    value: string | null
    onChange: (color: string | null) => void
  }) => (
    <div data-testid="color-picker">
      <button onClick={() => onChange('#FF0000')}>Select Red</button>
      <button onClick={() => onChange(null)}>Clear Color</button>
      <span data-testid="color-value">{value || 'none'}</span>
    </div>
  ),
}))

vi.mock('./IconSelector', () => ({
  IconSelector: ({
    value,
    onChange,
  }: {
    value: string | null
    onChange: (icon: string | null) => void
  }) => (
    <div data-testid="icon-selector">
      <button onClick={() => onChange('wallet')}>Select Wallet</button>
      <button onClick={() => onChange(null)}>Clear Icon</button>
      <span data-testid="icon-value">{value || 'none'}</span>
    </div>
  ),
}))

vi.mock('./EmojiInput', () => ({
  EmojiInput: ({
    value,
    onChange,
  }: {
    value: string | null
    onChange: (emoji: string | null) => void
  }) => (
    <div data-testid="emoji-input">
      <button onClick={() => onChange('💰')}>Select Money</button>
      <button onClick={() => onChange(null)}>Clear Emoji</button>
      <span data-testid="emoji-value">{value || 'none'}</span>
    </div>
  ),
}))

vi.mock('./AccountIconDisplay', () => ({
  AccountIconDisplay: ({ icon, className }: { icon: string; className?: string }) => (
    <span data-testid="account-icon" data-icon={icon} className={className}>
      [{icon}]
    </span>
  ),
}))

describe('AccountCustomizationModal', () => {
  const mockOnOpenChange = vi.fn()
  const mockOnSave = vi.fn()

  const mockAccount: Account = {
    id: '550e8400-e29b-41d4-a716-446655440000',
    linked_item_id: '770e9511-f3ac-52e5-b827-557766551112',
    user_id: 'auth0|507f1f77bcf86cd799439011',
    plaid_account_id: 'plaid-acc-1',
    name: 'Chase Checking',
    official_name: 'Chase Total Checking',
    type: 'depository',
    subtype: 'checking',
    mask: '0000',
    current_balance: 2500.50,
    available_balance: 2300.50,
    currency_code: 'USD',
    institution_name: 'Chase',
    created_at: '2025-10-18T14:30:00Z',
    updated_at: '2025-10-20T10:15:00Z',
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Modal Open/Close', () => {
    it('renders dialog when open=true', () => {
      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      expect(screen.getByRole('dialog')).toBeInTheDocument()
      expect(screen.getByText('Customize Account')).toBeInTheDocument()
    })

    it('does not render dialog content when open=false', () => {
      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={false}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      expect(screen.queryByText('Customize Account')).not.toBeInTheDocument()
    })

    it('does not render ModalContent when account is null', () => {
      render(
        <AccountCustomizationModal
          account={null}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      // Dialog exists but content is not rendered
      expect(screen.queryByText('Customize Account')).not.toBeInTheDocument()
      expect(screen.queryByLabelText('Nickname')).not.toBeInTheDocument()
    })

    it('calls onOpenChange when cancel is clicked', async () => {
      const user = userEvent.setup()
      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await user.click(screen.getByRole('button', { name: 'Cancel' }))

      expect(mockOnOpenChange).toHaveBeenCalledWith(false)
    })
  })

  describe('Display Name Fallback', () => {
    it('uses nickname when available', () => {
      const accountWithNickname = {
        ...mockAccount,
        nickname: 'My Checking',
      }

      render(
        <AccountCustomizationModal
          account={accountWithNickname}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      // Preview should show nickname
      expect(screen.getByText('My Checking')).toBeInTheDocument()
    })

    it('falls back to official_name when no nickname', () => {
      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      // Should show official_name in preview
      expect(screen.getByText('Chase Total Checking')).toBeInTheDocument()
    })

    it('falls back to name when no official_name', () => {
      const accountWithoutOfficialName = {
        ...mockAccount,
        official_name: undefined,
      }

      render(
        <AccountCustomizationModal
          account={accountWithoutOfficialName}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      expect(screen.getByText('Chase Checking')).toBeInTheDocument()
    })

    it('falls back to "Account" when no names', () => {
      const accountWithNoNames = {
        ...mockAccount,
        name: '',
        official_name: undefined,
        nickname: undefined,
      }

      render(
        <AccountCustomizationModal
          account={accountWithNoNames}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      expect(screen.getByText('Account')).toBeInTheDocument()
    })
  })

  describe('Form Initialization', () => {
    it('initializes nickname from account', () => {
      const accountWithNickname = {
        ...mockAccount,
        nickname: 'My Savings',
      }

      render(
        <AccountCustomizationModal
          account={accountWithNickname}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      const nicknameInput = screen.getByLabelText('Nickname')
      expect(nicknameInput).toHaveValue('My Savings')
    })

    it('initializes on emoji tab when account has emoji', () => {
      const accountWithEmoji = {
        ...mockAccount,
        custom_emoji: '💰',
      }

      render(
        <AccountCustomizationModal
          account={accountWithEmoji}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      // Emoji tab should be active
      const emojiTab = screen.getByRole('tab', { name: 'Emoji' })
      expect(emojiTab).toHaveAttribute('data-state', 'active')
    })

    it('initializes on icon tab when account has no emoji', () => {
      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      // Icon tab should be active by default
      const iconTab = screen.getByRole('tab', { name: 'Icon' })
      expect(iconTab).toHaveAttribute('data-state', 'active')
    })
  })

  describe('Save Logic', () => {
    it('calls onSave with changed fields', async () => {
      const user = userEvent.setup()
      mockOnSave.mockResolvedValue(undefined)

      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      // Change nickname
      const nicknameInput = screen.getByLabelText('Nickname')
      await user.type(nicknameInput, 'New Name')

      // Save
      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(mockOnSave).toHaveBeenCalledWith(mockAccount.id, expect.objectContaining({
          nickname: 'New Name',
        }))
      })
    })

    it('calls onCancel when no changes made', async () => {
      const user = userEvent.setup()

      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      // Click save without making changes
      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      // Should close modal without calling onSave
      expect(mockOnSave).not.toHaveBeenCalled()
      expect(mockOnOpenChange).toHaveBeenCalledWith(false)
    })

    it('closes modal after successful save', async () => {
      const user = userEvent.setup()
      mockOnSave.mockResolvedValue(undefined)

      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      // Change nickname and save
      const nicknameInput = screen.getByLabelText('Nickname')
      await user.type(nicknameInput, 'New Name')
      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(mockOnOpenChange).toHaveBeenCalledWith(false)
      })
    })
  })

  describe('Loading State', () => {
    it('shows loading spinner when isSaving=true', () => {
      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
          isSaving={true}
        />
      )

      // Should have Loader2 icon (animate-spin class)
      const saveButton = screen.getByRole('button', { name: /Save Changes/i })
      expect(saveButton.querySelector('.animate-spin')).toBeInTheDocument()
    })

    it('disables buttons when isSaving=true', () => {
      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
          isSaving={true}
        />
      )

      expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled()
      expect(screen.getByRole('button', { name: /Save Changes/i })).toBeDisabled()
    })

    it('enables buttons when isSaving=false', () => {
      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
          isSaving={false}
        />
      )

      expect(screen.getByRole('button', { name: 'Cancel' })).not.toBeDisabled()
      expect(screen.getByRole('button', { name: 'Save Changes' })).not.toBeDisabled()
    })
  })

  describe('Preview', () => {
    it('shows emoji in preview when emoji is selected', () => {
      const accountWithEmoji = {
        ...mockAccount,
        custom_emoji: '💰',
      }

      render(
        <AccountCustomizationModal
          account={accountWithEmoji}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      // Preview should contain the emoji (multiple matches expected due to mock)
      const emojis = screen.getAllByText('💰')
      expect(emojis.length).toBeGreaterThan(0)
    })

    it('shows icon in preview when icon is selected', () => {
      const accountWithIcon = {
        ...mockAccount,
        custom_icon: 'wallet' as const,
      }

      render(
        <AccountCustomizationModal
          account={accountWithIcon}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      // Should render AccountIconDisplay
      expect(screen.getByTestId('account-icon')).toHaveAttribute('data-icon', 'wallet')
    })

    it('shows first letter when no icon or emoji', () => {
      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      // Should show first letter of display name (C for Chase Total Checking)
      expect(screen.getByText('C')).toBeInTheDocument()
    })

    it('renders with custom color applied', () => {
      const accountWithColor = {
        ...mockAccount,
        custom_color: '#EF4444',
      }

      // Verify the component renders without errors when color is set
      // The color is applied via inline styles to the preview container
      render(
        <AccountCustomizationModal
          account={accountWithColor}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      // Verify dialog renders with account having custom color
      expect(screen.getByRole('dialog')).toBeInTheDocument()
      // Account name should be visible (this exercises the color branch)
      expect(screen.getByText('Chase Total Checking')).toBeInTheDocument()
    })
  })

  describe('Hidden Toggle', () => {
    it('shows hidden badge when is_hidden is true', () => {
      const hiddenAccount = {
        ...mockAccount,
        is_hidden: true,
      }

      render(
        <AccountCustomizationModal
          account={hiddenAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      expect(screen.getByText('Hidden')).toBeInTheDocument()
    })

    it('does not show hidden badge when is_hidden is false', () => {
      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      expect(screen.queryByText('Hidden')).not.toBeInTheDocument()
    })

    it('toggles hidden state when clicked', async () => {
      const user = userEvent.setup()

      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      const toggle = screen.getByRole('switch')
      expect(toggle).toHaveAttribute('aria-checked', 'false')

      await user.click(toggle)

      expect(toggle).toHaveAttribute('aria-checked', 'true')
    })

    it('shows hidden badge after toggle', async () => {
      const user = userEvent.setup()

      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      const toggle = screen.getByRole('switch')
      await user.click(toggle)

      // Hidden badge should appear
      expect(screen.getByText('Hidden')).toBeInTheDocument()
    })
  })

  describe('Tab Switching', () => {
    it('switches between icon and emoji tabs', async () => {
      const user = userEvent.setup()

      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      const emojiTab = screen.getByRole('tab', { name: 'Emoji' })
      await user.click(emojiTab)

      expect(emojiTab).toHaveAttribute('data-state', 'active')
      expect(screen.getByRole('tab', { name: 'Icon' })).toHaveAttribute('data-state', 'inactive')
    })

    it('shows emoji input when emoji tab is active', async () => {
      const user = userEvent.setup()

      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      const emojiTab = screen.getByRole('tab', { name: 'Emoji' })
      await user.click(emojiTab)

      expect(screen.getByTestId('emoji-input')).toBeInTheDocument()
    })

    it('shows icon selector when icon tab is active', () => {
      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      expect(screen.getByTestId('icon-selector')).toBeInTheDocument()
    })
  })

  describe('Nickname Input', () => {
    it('shows placeholder with account name', () => {
      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      const nicknameInput = screen.getByLabelText('Nickname')
      expect(nicknameInput).toHaveAttribute('placeholder', 'Chase Total Checking')
    })

    it('has maxLength of 255', () => {
      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      const nicknameInput = screen.getByLabelText('Nickname')
      expect(nicknameInput).toHaveAttribute('maxLength', '255')
    })

    it('shows helper text', () => {
      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      expect(
        screen.getByText('A custom name to help you identify this account.')
      ).toBeInTheDocument()
    })
  })

  describe('Dialog Content', () => {
    it('renders dialog description', () => {
      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      expect(
        screen.getByText('Personalize how this account appears in your dashboard.')
      ).toBeInTheDocument()
    })

    it('renders account institution info in preview', () => {
      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      expect(screen.getByText('Chase - checking')).toBeInTheDocument()
    })

    it('renders color picker', () => {
      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      expect(screen.getByTestId('color-picker')).toBeInTheDocument()
    })
  })
})
