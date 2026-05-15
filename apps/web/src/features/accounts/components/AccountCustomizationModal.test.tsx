/**
 * AccountCustomizationModal Component Tests
 *
 * Tests for the modal dialog that allows users to customize
 * their account appearance including nickname, color, icon/emoji, and visibility.
 *
 * Issue #307: Account Customization
 * Issue #544: Add unit tests for account customization components
 * Issue #575: Migrated to bun:test
 */

import { describe, it, expect, mock, beforeEach } from 'bun:test'

// Issue #575: DO NOT mock child components (ColorPicker, IconSelector, EmojiInput,
// AccountIconDisplay) - they work fine in tests and mocking them causes pollution
// to their own test files via Bun's global mock.module() persistence.
// - ColorPicker uses react-colorful which is mocked globally via bunfig.toml preload
// - IconSelector and EmojiInput work directly in happy-dom
// - AccountIconDisplay renders Lucide SVGs which work in happy-dom

import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AccountCustomizationModal } from './AccountCustomizationModal'
import type { Account } from '../types'

describe('AccountCustomizationModal', () => {
  const mockOnOpenChange = mock(() => {})
  const mockOnSave = mock(() => Promise.resolve())

  const mockAccount: Account = {
    id: '550e8400-e29b-41d4-a716-446655440000',
    linked_item_id: '770e9511-f3ac-52e5-b827-557766551112',
    user_id: 'user_507f1f77bcf86cd799439011',
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
    status: 'active',
    created_at: '2025-10-18T14:30:00Z',
    updated_at: '2025-10-20T10:15:00Z',
  }

  beforeEach(() => {
    mockOnOpenChange.mockClear()
    mockOnSave.mockClear()
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

      const iconTab = screen.getByRole('tab', { name: 'Icon' })
      expect(iconTab).toHaveAttribute('data-state', 'active')
    })
  })

  describe('Save Logic', () => {
    it('calls onSave with changed fields', async () => {
      const user = userEvent.setup()

      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      const nicknameInput = screen.getByLabelText('Nickname')
      await user.type(nicknameInput, 'New Name')

      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(mockOnSave).toHaveBeenCalledWith(mockAccount, expect.objectContaining({
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

      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      expect(mockOnSave).not.toHaveBeenCalled()
      expect(mockOnOpenChange).toHaveBeenCalledWith(false)
    })

    it('closes modal after successful save', async () => {
      const user = userEvent.setup()

      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

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

      // Dialog renders in a portal, so use document.body to query
      // Real AccountIconDisplay renders SVGs with lucide class names
      const iconSvg = document.body.querySelector('svg.h-5')
      expect(iconSvg).toBeInTheDocument()
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

      expect(screen.getByText('C')).toBeInTheDocument()
    })

    it('renders with custom color applied', () => {
      const accountWithColor = {
        ...mockAccount,
        custom_color: '#EF4444',
      }

      render(
        <AccountCustomizationModal
          account={accountWithColor}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      expect(screen.getByRole('dialog')).toBeInTheDocument()
      expect(screen.getByText('Chase Total Checking')).toBeInTheDocument()
    })
  })

  describe('Hidden Toggle', () => {
    it('shows hidden badge when is_hidden is true', () => {
      const hiddenAccount: Account = {
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

    it('toggles hidden state when clicked (via confirmation)', async () => {
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
      // Dialog opens; confirm to proceed
      const dialog = screen.getByRole('alertdialog')
      await user.click(within(dialog).getByRole('button', { name: 'Hide Account' }))

      expect(toggle).toHaveAttribute('aria-checked', 'true')
    })

    it('shows hidden badge after confirming hide', async () => {
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
      const dialog = screen.getByRole('alertdialog')
      await user.click(within(dialog).getByRole('button', { name: 'Hide Account' }))

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

      // Real EmojiInput renders "Suggestions" label
      expect(screen.getByText('Suggestions')).toBeInTheDocument()
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

      // Real IconSelector renders buttons with aria-label "Select X icon"
      expect(screen.getByLabelText('Select Wallet icon')).toBeInTheDocument()
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

      // Real ColorPicker renders hex-color-picker from mocked react-colorful
      expect(screen.getByTestId('hex-color-picker')).toBeInTheDocument()
    })
  })

  describe('Hide Account Confirmation', () => {
    it('opens confirmation dialog when toggling visible account to hidden', async () => {
      const user = userEvent.setup()

      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await user.click(screen.getByRole('switch'))

      expect(screen.getByRole('alertdialog')).toBeInTheDocument()
      expect(screen.getByText('Hide Account?')).toBeInTheDocument()
    })

    it('does not open confirmation dialog when unhiding a hidden account', async () => {
      const user = userEvent.setup()
      const hiddenAccount: Account = { ...mockAccount, is_hidden: true }

      render(
        <AccountCustomizationModal
          account={hiddenAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await user.click(screen.getByRole('switch'))

      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    })

    it('cancelling the dialog leaves the account visible', async () => {
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

      const dialog = screen.getByRole('alertdialog')
      await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))

      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
      expect(toggle).toHaveAttribute('aria-checked', 'false')
      expect(screen.queryByText('Hidden')).not.toBeInTheDocument()
    })

    it('confirming the dialog marks the account as hidden', async () => {
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

      const dialog = screen.getByRole('alertdialog')
      await user.click(within(dialog).getByRole('button', { name: 'Hide Account' }))

      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
      expect(toggle).toHaveAttribute('aria-checked', 'true')
      expect(screen.getByText('Hidden')).toBeInTheDocument()
    })

    it('saving after confirming hide calls onSave with is_hidden: true', async () => {
      const user = userEvent.setup()

      render(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await user.click(screen.getByRole('switch'))
      const dialog = screen.getByRole('alertdialog')
      await user.click(within(dialog).getByRole('button', { name: 'Hide Account' }))

      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(mockOnSave).toHaveBeenCalledWith(
          mockAccount,
          expect.objectContaining({ is_hidden: true }),
        )
      })
    })
  })
})
