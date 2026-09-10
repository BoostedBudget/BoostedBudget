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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode, ReactElement } from 'react'
import { AccountCustomizationModal } from './AccountCustomizationModal'
import type { Account } from '../types'
import type { GetMyHouseholdResponseBody, UserAccountShareView } from '@boostedbudget/api-client/models'
import {
  mockUseGetMyHousehold,
  mockUseGetHouseholdEntitlement,
  mockRefetchMyHousehold,
  mockRefetchHouseholdEntitlement,
  mockUseListMyAccountShares,
  mockRefetchMyAccountShares,
  mockUseSetHouseholdShareVisibility,
  mockSetHouseholdShareVisibilityMutateAsync,
} from '@/test/mocks/household-api'
import { mockToastError } from '@/test/mocks/sonner'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { ANALYTICS_EVENTS } from '@/shared/analytics'

// Issue #1370 Task 8: AccountCustomizationModal now calls useHousehold()/
// useHouseholdVisibility() unconditionally (Rules of Hooks — the household
// share row only *renders* conditionally, but the hooks always run). Both
// hooks call useQueryClient() internally, so every render needs a
// QueryClientProvider ancestor (mirrors ScopeSegmentedControl.test.tsx /
// useHousehold.test.tsx's own wrapper pattern).
function createWrapper() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

function renderModal(ui: ReactElement) {
  return render(ui, { wrapper: createWrapper() })
}

// #1618/P4-T5: the exclude-investment switch now lives behind a collapsed "Advanced"
// disclosure (see AccountCustomizationModal.tsx) and is not in the document at all until this
// is clicked open. Every test that queries that switch must call this first.
async function openAdvancedSection(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: 'Advanced' }))
}

const mockHouseholdResponse: GetMyHouseholdResponseBody = {
  household: {
    id: 'household-1',
    name: 'The Smiths',
    owner_user_id: 'user-1',
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  },
  summary: {
    is_owner: true,
    seats_active: 2,
    seats_pending: 0,
    source: 'household',
    tier: 'household_3',
    users_included: 3,
  },
}

function mockInHousehold() {
  mockUseGetMyHousehold.mockReturnValue({
    data: mockHouseholdResponse,
    isLoading: false,
    error: null,
    refetch: mockRefetchMyHousehold,
  })
  mockUseGetHouseholdEntitlement.mockReturnValue({
    data: mockHouseholdResponse.summary,
    isLoading: false,
    error: null,
    refetch: mockRefetchHouseholdEntitlement,
  })
}

function mockNotInHousehold() {
  mockUseGetMyHousehold.mockReturnValue({
    data: undefined,
    isLoading: false,
    error: null,
    refetch: mockRefetchMyHousehold,
  })
  mockUseGetHouseholdEntitlement.mockReturnValue({
    data: undefined,
    isLoading: false,
    error: null,
    refetch: mockRefetchHouseholdEntitlement,
  })
}

function mockShares(shares: UserAccountShareView[]) {
  mockUseListMyAccountShares.mockReturnValue({
    data: shares,
    isLoading: false,
    error: null,
    refetch: mockRefetchMyAccountShares,
  })
}

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
    is_hidden: false,
    exclude_investment_sync: false,
    created_at: '2025-10-18T14:30:00Z',
    updated_at: '2025-10-20T10:15:00Z',
  }

  beforeEach(() => {
    mockOnOpenChange.mockClear()
    mockOnSave.mockClear()

    // Household mocks are process-global (shared across test files via
    // mock.module()) — reset to a deterministic "not in household, no shares"
    // baseline before each test so unrelated tests aren't affected by state
    // left over from the "Household Sharing" describe block below.
    mockUseGetMyHousehold.mockClear()
    mockUseGetHouseholdEntitlement.mockClear()
    mockRefetchMyHousehold.mockClear()
    mockRefetchHouseholdEntitlement.mockClear()
    mockUseListMyAccountShares.mockClear()
    mockRefetchMyAccountShares.mockClear()
    mockUseSetHouseholdShareVisibility.mockClear()
    mockSetHouseholdShareVisibilityMutateAsync.mockClear()
    mockToastError.mockClear()
    mockTrackEvent.mockClear()
    mockNotInHousehold()
    mockShares([])
    mockUseSetHouseholdShareVisibility.mockReturnValue({
      mutateAsync: mockSetHouseholdShareVisibilityMutateAsync,
      isPending: false,
    })
  })

  describe('Modal Open/Close', () => {
    it('renders dialog when open=true', () => {
      renderModal(
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
      renderModal(
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
      renderModal(
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
      renderModal(
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

      renderModal(
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
      renderModal(
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

      renderModal(
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

      renderModal(
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

      renderModal(
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

      renderModal(
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
      renderModal(
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

      renderModal(
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

      renderModal(
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

    // #1609: the user-visible symptom, encoded.
    //
    // The modal already decides correctly — it closes only when nothing failed — but
    // useAccountCustomization's `catch { return null }` turned a rejection into a resolved value
    // one layer down, so `Promise.allSettled(...).status` was never 'rejected' and the modal
    // closed on a failed save exactly as it does on a successful one. The user was told their
    // edit had saved, and found out on the next refresh that it had not. This case fails if that
    // swallowing ever comes back at any layer between the button and the mutation.
    it('stays open and does not close when the save rejects', async () => {
      const user = userEvent.setup()
      const rejectingSave = mock(() => Promise.reject(new Error('Save failed')))

      renderModal(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={rejectingSave}
        />
      )

      const nicknameInput = screen.getByLabelText('Nickname')
      await user.type(nicknameInput, 'New Name')
      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(rejectingSave).toHaveBeenCalled()
      })
      // The save is settled (the button is interactive again), and the modal did NOT close.
      await waitFor(() => {
        expect(screen.getByRole('button', { name: 'Save Changes' })).toBeEnabled()
      })
      expect(mockOnOpenChange).not.toHaveBeenCalled()
      expect(screen.getByText('Customize Account')).toBeInTheDocument()
    })

    it('closes modal after successful save', async () => {
      const user = userEvent.setup()

      renderModal(
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
      renderModal(
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
      renderModal(
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
      renderModal(
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

      renderModal(
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

      renderModal(
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
      renderModal(
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

      renderModal(
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

      renderModal(
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
      renderModal(
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

      renderModal(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      const toggle = screen.getByRole('switch', { name: /hide account|show account/i })
      expect(toggle).toHaveAttribute('aria-checked', 'false')

      await user.click(toggle)
      // Dialog opens; confirm to proceed
      const dialog = screen.getByRole('alertdialog')
      await user.click(within(dialog).getByRole('button', { name: 'Hide Account' }))

      expect(toggle).toHaveAttribute('aria-checked', 'true')
    })

    it('shows hidden badge after confirming hide', async () => {
      const user = userEvent.setup()

      renderModal(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      const toggle = screen.getByRole('switch', { name: /hide account|show account/i })
      await user.click(toggle)
      const dialog = screen.getByRole('alertdialog')
      await user.click(within(dialog).getByRole('button', { name: 'Hide Account' }))

      expect(screen.getByText('Hidden')).toBeInTheDocument()
    })
  })

  describe('Tab Switching', () => {
    it('switches between icon and emoji tabs', async () => {
      const user = userEvent.setup()

      renderModal(
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

      renderModal(
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
      renderModal(
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
    it('shows generic placeholder and has maxLength/helper text', () => {
      renderModal(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      const nicknameInput = screen.getByLabelText('Nickname')
      // R9.3: placeholder changed to generic string so account name isn't recorded
      // in PostHog session replay (attributes are scanned verbatim, cannot be masked)
      expect(nicknameInput).toHaveAttribute('placeholder', 'Account name')
      expect(nicknameInput).toHaveAttribute('maxLength', '255')
      expect(
        screen.getByText('A custom name to help you identify this account.')
      ).toBeInTheDocument()
    })
  })

  describe('Dialog Content', () => {
    it('renders dialog description and institution info with PII protection', () => {
      renderModal(
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

      // R9.2: institution_name is now wrapped in a data-ph-mask span for PII protection;
      // combined text still reads "Chase - checking" — verify via function matcher since
      // RTL getByText does not match text split across child elements with a plain string
      const institutionEl = screen.getByText((_, element) => {
        return (
          element?.tagName === 'SPAN' &&
          element.textContent?.replace(/\s+/g, ' ').trim() === 'Chase - checking'
        )
      })
      expect(institutionEl).toBeInTheDocument()
    })

    it('renders color picker', () => {
      renderModal(
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

      renderModal(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await user.click(screen.getByRole('switch', { name: /hide account|show account/i }))

      expect(screen.getByRole('alertdialog')).toBeInTheDocument()
      expect(screen.getByText('Hide Account?')).toBeInTheDocument()
    })

    it('does not open confirmation dialog when unhiding a hidden account', async () => {
      const user = userEvent.setup()
      const hiddenAccount: Account = { ...mockAccount, is_hidden: true }

      renderModal(
        <AccountCustomizationModal
          account={hiddenAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await user.click(screen.getByRole('switch', { name: /hide account|show account/i }))

      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    })

    it('cancelling the dialog leaves the account visible', async () => {
      const user = userEvent.setup()

      renderModal(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      const toggle = screen.getByRole('switch', { name: /hide account|show account/i })
      await user.click(toggle)

      const dialog = screen.getByRole('alertdialog')
      await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))

      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
      expect(toggle).toHaveAttribute('aria-checked', 'false')
      expect(screen.queryByText('Hidden')).not.toBeInTheDocument()
    })

    it('confirming the dialog marks the account as hidden', async () => {
      const user = userEvent.setup()

      renderModal(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      const toggle = screen.getByRole('switch', { name: /hide account|show account/i })
      await user.click(toggle)

      const dialog = screen.getByRole('alertdialog')
      await user.click(within(dialog).getByRole('button', { name: 'Hide Account' }))

      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
      expect(toggle).toHaveAttribute('aria-checked', 'true')
      expect(screen.getByText('Hidden')).toBeInTheDocument()
    })

    it('saving after confirming hide calls onSave with is_hidden: true', async () => {
      const user = userEvent.setup()

      renderModal(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await user.click(screen.getByRole('switch', { name: /hide account|show account/i }))
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

  describe('Exclude Investment Activity Toggle (Issue #1609 Item 5 / #1618b)', () => {
    it('reflects the stored value when the account already excludes investment sync', async () => {
      const user = userEvent.setup()
      const excludedAccount: Account = {
        ...mockAccount,
        user_account_id: 'ua-chained-1618b',
        exclude_investment_sync: true,
      }

      renderModal(
        <AccountCustomizationModal
          account={excludedAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await openAdvancedSection(user)
      const toggle = screen.getByRole('switch', { name: /investment activity/i })
      expect(toggle).toHaveAttribute('aria-checked', 'true')
    })

    it('reflects the stored value when the account does not exclude investment sync', async () => {
      const user = userEvent.setup()
      renderModal(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await openAdvancedSection(user)
      const toggle = screen.getByRole('switch', { name: /investment activity/i })
      expect(toggle).toHaveAttribute('aria-checked', 'false')
    })

    // #1631 M4 — THE COPY IS A CONDITION OF THE AUTHORISATION, SO IT IS PINNED HERE.
    //
    // The retention ADR permits this flag to suppress INGESTION (rather than merely filter reads)
    // only under three scope tests, and one of them is that the user is told the cost in the
    // control itself before they commit — see
    // docs/decisions/2026-08-24-financial-history-retained-by-default.md
    // § "AMENDMENT (#1631): a fourth category — never ingesting a row". The cost is that clearing
    // the flag backfills at most 2 years (Plaid's limit on /investments/transactions/get), so an
    // account excluded for longer permanently loses its earliest excluded period.
    //
    // Without this test, "shortening the copy is a defect" was an assertion in a code comment that
    // nothing could check, and a future copy pass tidying the sentence away would silently revoke
    // the ADR's authorisation for the whole feature. These assertions are deliberately loose about
    // wording and strict about the two facts that carry the warning: that we stop saving, and the
    // 2-year bound.
    it('warns that exclusion stops saving activity and that the backfill reaches only 2 years', async () => {
      const user = userEvent.setup()
      const chained: Account = { ...mockAccount, user_account_id: 'ua-chained-1631' }

      renderModal(
        <AccountCustomizationModal
          account={chained}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await openAdvancedSection(user)

      const toggle = screen.getByRole('switch', { name: /investment activity/i })
      const description = toggle.closest('div.flex')?.querySelector('p')
      expect(description).not.toBeNull()
      const copy = description?.textContent ?? ''

      expect(copy).toMatch(/2 years/)
      expect(copy).toMatch(/stop saving/i)
      // The de-duplication promise must survive too — it is the REASON the user reaches for the
      // toggle, and a warning with no stated benefit is not a usable control.
      expect(copy).toMatch(/twice/i)
    })

    // ⚠ #1618b review finding: these two MUST use a CHAINED account. exclude_investment_sync is a
    // column on `user_accounts`, so an account with no user_account_id has nowhere to store it and
    // the toggle is deliberately disabled for it. Running these against the unchained `mockAccount`
    // proved the value reached onSave for precisely the account class where the flow is invalid.
    const chainedAccount: Account = { ...mockAccount, user_account_id: 'ua-chained-1618b' }

    it('sends exclude_investment_sync: true in the PATCH body when flipped on', async () => {
      const user = userEvent.setup()

      renderModal(
        <AccountCustomizationModal
          account={chainedAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await openAdvancedSection(user)
      await user.click(screen.getByRole('switch', { name: /investment activity/i }))
      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(mockOnSave).toHaveBeenCalledWith(
          chainedAccount,
          expect.objectContaining({ exclude_investment_sync: true }),
        )
      })
    })

    it('sends exclude_investment_sync: false in the PATCH body when flipped off', async () => {
      const user = userEvent.setup()
      const excludedAccount: Account = {
        ...mockAccount,
        user_account_id: 'ua-chained-1618b',
        exclude_investment_sync: true,
      }

      renderModal(
        <AccountCustomizationModal
          account={excludedAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await openAdvancedSection(user)
      await user.click(screen.getByRole('switch', { name: /investment activity/i }))
      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(mockOnSave).toHaveBeenCalledWith(
          excludedAccount,
          expect.objectContaining({ exclude_investment_sync: false }),
        )
      })
    })

    it('does not include exclude_investment_sync in the PATCH body when left unchanged', async () => {
      const user = userEvent.setup()

      renderModal(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      // Change something unrelated so a save actually fires, without touching the toggle.
      await user.type(screen.getByLabelText('Nickname'), 'x')
      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(mockOnSave).toHaveBeenCalledWith(
          mockAccount,
          expect.not.objectContaining({ exclude_investment_sync: expect.anything() }),
        )
      })
    })
  })

  describe('Household Sharing (Issue #1370 Task 8)', () => {
    const householdAccount: Account = {
      ...mockAccount,
      user_account_id: 'ua-123',
    }

    it('does not show the household share switch when not in a household', async () => {
      const user = userEvent.setup()
      mockNotInHousehold()
      mockShares([{ user_account_id: 'ua-123', visibility: 'shared' }])

      renderModal(
        <AccountCustomizationModal
          account={householdAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      // Only the pre-existing "Hide Account" and "Exclude Investment Activity" switches
      // should be present (household share switch absent). The investment switch is behind
      // the collapsed Advanced disclosure (#1618/P4-T5), so it must be opened first.
      await openAdvancedSection(user)
      expect(screen.getAllByRole('switch')).toHaveLength(2)
      expect(screen.queryByText('Share with household')).not.toBeInTheDocument()
      expect(screen.queryByText('Hidden from household')).not.toBeInTheDocument()
    })

    it('does not show the household share switch when the account has no user_account_id', async () => {
      const user = userEvent.setup()
      mockInHousehold()
      mockShares([])

      renderModal(
        <AccountCustomizationModal
          account={mockAccount} // no user_account_id
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await openAdvancedSection(user)
      expect(screen.getAllByRole('switch')).toHaveLength(2)
      expect(screen.queryByText('Share with household')).not.toBeInTheDocument()
      expect(screen.queryByText('Hidden from household')).not.toBeInTheDocument()
    })

    it('shows the household share switch when in a household with a usable user_account_id', async () => {
      const user = userEvent.setup()
      mockInHousehold()
      mockShares([{ user_account_id: 'ua-123', visibility: 'hidden' }])

      renderModal(
        <AccountCustomizationModal
          account={householdAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await openAdvancedSection(user)
      expect(screen.getAllByRole('switch')).toHaveLength(3)
    })

    it('initializes checked when the account visibility is "shared"', () => {
      mockInHousehold()
      mockShares([{ user_account_id: 'ua-123', visibility: 'shared' }])

      renderModal(
        <AccountCustomizationModal
          account={householdAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      expect(screen.getByText('Share with household')).toBeInTheDocument()
      const householdSwitch = screen.getByRole('switch', { name: /household/i })
      expect(householdSwitch).toHaveAttribute('aria-checked', 'true')
    })

    it('initializes unchecked when the account visibility is "hidden"', () => {
      mockInHousehold()
      mockShares([{ user_account_id: 'ua-123', visibility: 'hidden' }])

      renderModal(
        <AccountCustomizationModal
          account={householdAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      expect(screen.getByText('Hidden from household')).toBeInTheDocument()
      const householdSwitch = screen.getByRole('switch', { name: /household/i })
      expect(householdSwitch).toHaveAttribute('aria-checked', 'false')
    })

    it('initializes unchecked when the account has no share grant (null visibility)', () => {
      mockInHousehold()
      mockShares([]) // no grant at all — verified backend semantics: null === not-shared

      renderModal(
        <AccountCustomizationModal
          account={householdAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      const householdSwitch = screen.getByRole('switch', { name: /household/i })
      expect(householdSwitch).toHaveAttribute('aria-checked', 'false')
    })

    it('toggling off then saving calls setVisibility with "hidden", keyed by user_account_id', async () => {
      const user = userEvent.setup()
      mockInHousehold()
      mockShares([{ user_account_id: 'ua-123', visibility: 'shared' }])

      renderModal(
        <AccountCustomizationModal
          account={householdAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      const householdSwitch = screen.getByRole('switch', { name: /household/i })
      expect(householdSwitch).toHaveAttribute('aria-checked', 'true')

      await user.click(householdSwitch)
      expect(householdSwitch).toHaveAttribute('aria-checked', 'false')

      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(mockSetHouseholdShareVisibilityMutateAsync).toHaveBeenCalledWith({
          id: 'ua-123',
          data: { visibility: 'hidden' },
        })
      })

      // Regression: the household-share write must never leak into the
      // onSave/changes payload — no appearance fields changed, so onSave
      // should not be called at all.
      expect(mockOnSave).not.toHaveBeenCalled()
    })

    it('toggling on then saving calls setVisibility with "shared"', async () => {
      const user = userEvent.setup()
      mockInHousehold()
      mockShares([{ user_account_id: 'ua-123', visibility: 'hidden' }])

      renderModal(
        <AccountCustomizationModal
          account={householdAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      const householdSwitch = screen.getByRole('switch', { name: /household/i })
      await user.click(householdSwitch)

      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(mockSetHouseholdShareVisibilityMutateAsync).toHaveBeenCalledWith({
          id: 'ua-123',
          data: { visibility: 'shared' },
        })
      })
      expect(mockOnSave).not.toHaveBeenCalled()
    })

    it('does not call setVisibility when the switch is left untouched', async () => {
      const user = userEvent.setup()
      mockInHousehold()
      mockShares([{ user_account_id: 'ua-123', visibility: 'shared' }])

      renderModal(
        <AccountCustomizationModal
          account={householdAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(mockOnOpenChange).toHaveBeenCalledWith(false)
      })
      expect(mockSetHouseholdShareVisibilityMutateAsync).not.toHaveBeenCalled()
      expect(mockOnSave).not.toHaveBeenCalled()
    })

    it('closes the modal after a share-only save (no appearance changes)', async () => {
      const user = userEvent.setup()
      mockInHousehold()
      mockShares([{ user_account_id: 'ua-123', visibility: 'shared' }])

      renderModal(
        <AccountCustomizationModal
          account={householdAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await user.click(screen.getByRole('switch', { name: /household/i }))
      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(mockOnOpenChange).toHaveBeenCalledWith(false)
      })
    })

    it('fires both onSave and setVisibility when appearance and share visibility both change', async () => {
      const user = userEvent.setup()
      mockInHousehold()
      mockShares([{ user_account_id: 'ua-123', visibility: 'shared' }])

      renderModal(
        <AccountCustomizationModal
          account={householdAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await user.type(screen.getByLabelText('Nickname'), 'New Name')
      await user.click(screen.getByRole('switch', { name: /household/i }))
      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(mockOnSave).toHaveBeenCalledWith(
          householdAccount,
          expect.objectContaining({ nickname: 'New Name' })
        )
        expect(mockSetHouseholdShareVisibilityMutateAsync).toHaveBeenCalledWith({
          id: 'ua-123',
          data: { visibility: 'hidden' },
        })
      })
    })

    it('does not call setVisibility when the account has no user_account_id, even in a household', async () => {
      const user = userEvent.setup()
      mockInHousehold()
      mockShares([])

      renderModal(
        <AccountCustomizationModal
          account={mockAccount} // no user_account_id
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await user.type(screen.getByLabelText('Nickname'), 'New Name')
      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(mockOnSave).toHaveBeenCalled()
      })
      expect(mockSetHouseholdShareVisibilityMutateAsync).not.toHaveBeenCalled()
    })

    it('does not call setVisibility when the switch is toggled and then toggled back before saving', async () => {
      const user = userEvent.setup()
      mockInHousehold()
      mockShares([{ user_account_id: 'ua-123', visibility: 'shared' }])

      renderModal(
        <AccountCustomizationModal
          account={householdAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      const householdSwitch = screen.getByRole('switch', { name: /household/i })
      await user.click(householdSwitch) // shared -> hidden
      await user.click(householdSwitch) // hidden -> back to shared (original)

      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(mockOnOpenChange).toHaveBeenCalledWith(false)
      })
      expect(mockSetHouseholdShareVisibilityMutateAsync).not.toHaveBeenCalled()
    })

    it('re-syncs the switch once the shares query resolves after mounting while still loading', async () => {
      const user = userEvent.setup()
      mockInHousehold()
      // Shares query is still in flight when the modal first mounts.
      mockUseListMyAccountShares.mockReturnValue({
        data: undefined,
        isLoading: true,
        error: null,
        refetch: mockRefetchMyAccountShares,
      })

      const { rerender } = renderModal(
        <AccountCustomizationModal
          account={householdAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      expect(screen.getByRole('switch', { name: /household/i })).toHaveAttribute('aria-checked', 'false')

      // Query resolves with a "shared" grant for this account.
      mockUseListMyAccountShares.mockReturnValue({
        data: [{ user_account_id: 'ua-123', visibility: 'shared' }],
        isLoading: false,
        error: null,
        refetch: mockRefetchMyAccountShares,
      })
      rerender(
        <AccountCustomizationModal
          account={householdAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await waitFor(() => {
        expect(screen.getByRole('switch', { name: /household/i })).toHaveAttribute('aria-checked', 'true')
      })

      // The re-synced value is now the baseline — saving without touching
      // the switch must not treat it as a change.
      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(mockOnOpenChange).toHaveBeenCalledWith(false)
      })
      expect(mockSetHouseholdShareVisibilityMutateAsync).not.toHaveBeenCalled()
    })

    it('disables the switch while the shares query is loading, closing the window for the user to act on a provisional value', async () => {
      const user = userEvent.setup()
      mockInHousehold()
      mockUseListMyAccountShares.mockReturnValue({
        data: undefined,
        isLoading: true,
        error: null,
        refetch: mockRefetchMyAccountShares,
      })

      const { rerender } = renderModal(
        <AccountCustomizationModal
          account={householdAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      const householdSwitch = screen.getByRole('switch', { name: /household/i })
      expect(householdSwitch).toBeDisabled()

      // Clicking a disabled switch must not toggle it — the real value
      // ('hidden' here) is still unknown while the query is loading.
      await user.click(householdSwitch)
      expect(householdSwitch).toHaveAttribute('aria-checked', 'false')

      // Query resolves with 'hidden' — matches the untouched provisional
      // state, so the switch stays unchecked, is re-enabled, and saving
      // does not treat this as a change.
      mockUseListMyAccountShares.mockReturnValue({
        data: [{ user_account_id: 'ua-123', visibility: 'hidden' }],
        isLoading: false,
        error: null,
        refetch: mockRefetchMyAccountShares,
      })
      rerender(
        <AccountCustomizationModal
          account={householdAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await waitFor(() => {
        expect(screen.getByRole('switch', { name: /household/i })).not.toBeDisabled()
      })
      expect(screen.getByRole('switch', { name: /household/i })).toHaveAttribute('aria-checked', 'false')

      // Now that it's enabled, the user can toggle it normally and the
      // change is saved as expected.
      await user.click(screen.getByRole('switch', { name: /household/i }))
      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(mockSetHouseholdShareVisibilityMutateAsync).toHaveBeenCalledWith({
          id: 'ua-123',
          data: { visibility: 'shared' },
        })
      })
    })

    it('leaves the modal open and shows a still-shared warning when un-sharing fails (share-only save)', async () => {
      const user = userEvent.setup()
      mockInHousehold()
      mockShares([{ user_account_id: 'ua-123', visibility: 'shared' }])
      mockSetHouseholdShareVisibilityMutateAsync.mockRejectedValueOnce(new Error('network error'))

      renderModal(
        <AccountCustomizationModal
          account={householdAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      // Starting visibility is 'shared'; toggling off attempts to un-share.
      await user.click(screen.getByRole('switch', { name: /household/i }))
      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(mockSetHouseholdShareVisibilityMutateAsync).toHaveBeenCalled()
      })
      await waitFor(() => {
        // Directionally accurate message: the write failed while trying to
        // REVOKE sharing, so the account is still shared — that's the
        // privacy-consequential case the message must call out explicitly,
        // not a generic failure message.
        expect(mockToastError).toHaveBeenCalledWith(
          'Failed to stop sharing with your household — this account is still visible to household members. Please try again.'
        )
      })

      // A failed share-only save must not silently close the modal.
      expect(mockOnOpenChange).not.toHaveBeenCalledWith(false)
      // No analytics event on failure.
      expect(mockTrackEvent).not.toHaveBeenCalled()
    })

    it('shows a generic failure message when SHARING (not un-sharing) fails', async () => {
      const user = userEvent.setup()
      mockInHousehold()
      // Starting visibility is 'hidden' (no share grant); toggling on attempts to share.
      mockShares([{ user_account_id: 'ua-123', visibility: 'hidden' }])
      mockSetHouseholdShareVisibilityMutateAsync.mockRejectedValueOnce(new Error('network error'))

      renderModal(
        <AccountCustomizationModal
          account={householdAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await user.click(screen.getByRole('switch', { name: /household/i }))
      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(mockToastError).toHaveBeenCalledWith('Failed to update household sharing. Please try again.')
      })

      // Lower-stakes failure direction — safe-by-default — but still must
      // not silently close the modal.
      expect(mockOnOpenChange).not.toHaveBeenCalledWith(false)
      expect(mockTrackEvent).not.toHaveBeenCalled()
    })

    it('disables Save/Cancel while a share-only save is in flight, to prevent double-submission', async () => {
      const user = userEvent.setup()
      mockInHousehold()
      mockShares([{ user_account_id: 'ua-123', visibility: 'shared' }])

      let resolveSetVisibility: () => void = () => {}
      mockSetHouseholdShareVisibilityMutateAsync.mockImplementationOnce(
        () => new Promise<undefined>((resolve) => {
          resolveSetVisibility = () => resolve(undefined)
        })
      )

      renderModal(
        <AccountCustomizationModal
          account={householdAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await user.click(screen.getByRole('switch', { name: /household/i }))
      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /Save Changes/i })).toBeDisabled()
      })
      expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled()

      resolveSetVisibility()

      await waitFor(() => {
        expect(mockOnOpenChange).toHaveBeenCalledWith(false)
      })
    })

    it('stays open and surfaces a still-shared warning when setVisibility fails even though the appearance save succeeded (Finding 1 regression)', async () => {
      const user = userEvent.setup()
      mockInHousehold()
      mockShares([{ user_account_id: 'ua-123', visibility: 'shared' }])
      mockSetHouseholdShareVisibilityMutateAsync.mockRejectedValueOnce(new Error('network error'))

      renderModal(
        <AccountCustomizationModal
          account={householdAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await user.type(screen.getByLabelText('Nickname'), 'New Name')
      await user.click(screen.getByRole('switch', { name: /household/i }))
      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(mockOnSave).toHaveBeenCalledWith(
          householdAccount,
          expect.objectContaining({ nickname: 'New Name' })
        )
        expect(mockToastError).toHaveBeenCalledWith(
          'Failed to stop sharing with your household — this account is still visible to household members. Please try again.'
        )
      })

      // The core Finding 1 bug: the appearance save succeeded, but the modal
      // must NOT close, because the parallel share-visibility write failed.
      // Closing here would let the user walk away believing the account was
      // made private when it is still shared with the household.
      expect(mockOnOpenChange).not.toHaveBeenCalledWith(false)
      // No analytics event on a failed share-visibility write.
      expect(mockTrackEvent).not.toHaveBeenCalled()
    })

    it('closes the modal when both appearance and share-visibility saves succeed', async () => {
      const user = userEvent.setup()
      mockInHousehold()
      mockShares([{ user_account_id: 'ua-123', visibility: 'shared' }])

      renderModal(
        <AccountCustomizationModal
          account={householdAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await user.type(screen.getByLabelText('Nickname'), 'New Name')
      await user.click(screen.getByRole('switch', { name: /household/i }))
      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(mockOnOpenChange).toHaveBeenCalledWith(false)
      })
      expect(mockToastError).not.toHaveBeenCalled()
    })

    it('fires HOUSEHOLD_ACCOUNT_SHARE_CHANGED with the new visibility once the share-visibility write succeeds', async () => {
      const user = userEvent.setup()
      mockInHousehold()
      mockShares([{ user_account_id: 'ua-123', visibility: 'shared' }])

      renderModal(
        <AccountCustomizationModal
          account={householdAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      // shared -> hidden
      await user.click(screen.getByRole('switch', { name: /household/i }))
      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(mockTrackEvent).toHaveBeenCalledWith(
          ANALYTICS_EVENTS.HOUSEHOLD_ACCOUNT_SHARE_CHANGED,
          { visibility: 'hidden' }
        )
      })
    })

    it('does not fire HOUSEHOLD_ACCOUNT_SHARE_CHANGED when the share-visibility write fails', async () => {
      const user = userEvent.setup()
      mockInHousehold()
      mockShares([{ user_account_id: 'ua-123', visibility: 'shared' }])
      mockSetHouseholdShareVisibilityMutateAsync.mockRejectedValueOnce(new Error('network error'))

      renderModal(
        <AccountCustomizationModal
          account={householdAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await user.click(screen.getByRole('switch', { name: /household/i }))
      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(mockToastError).toHaveBeenCalled()
      })
      expect(mockTrackEvent).not.toHaveBeenCalled()
    })

    it('does not fire HOUSEHOLD_ACCOUNT_SHARE_CHANGED when the switch is left untouched (appearance-only save)', async () => {
      const user = userEvent.setup()
      mockInHousehold()
      mockShares([{ user_account_id: 'ua-123', visibility: 'shared' }])

      renderModal(
        <AccountCustomizationModal
          account={householdAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await user.type(screen.getByLabelText('Nickname'), 'New Name')
      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(mockOnOpenChange).toHaveBeenCalledWith(false)
      })
      expect(mockSetHouseholdShareVisibilityMutateAsync).not.toHaveBeenCalled()
      expect(mockTrackEvent).not.toHaveBeenCalled()
    })
  })

  // #1618b review finding: exclude_investment_sync is a column on `user_accounts`. An account with
  // no user_account_id has no chain row to store it on, and the legacy PATCH /accounts/{id} schema
  // does not declare the field at all. A toggle the user can flip that cannot persist is worse than
  // no toggle — it closes as if it saved. So it must be visibly inert for those accounts.
  describe('exclude investment activity — unchained accounts (#1618b)', () => {
    it('disables the toggle when the account has no user_account_id', async () => {
      const user = userEvent.setup()
      renderModal(
        <AccountCustomizationModal
          account={mockAccount}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await openAdvancedSection(user)
      expect(screen.getByRole('switch', { name: /investment activity/i })).toBeDisabled()
    })

    it('leaves the toggle enabled when the account IS chained', async () => {
      const user = userEvent.setup()
      renderModal(
        <AccountCustomizationModal
          account={{ ...mockAccount, user_account_id: 'ua-chained-1618b' }}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      await openAdvancedSection(user)
      expect(screen.getByRole('switch', { name: /investment activity/i })).not.toBeDisabled()
    })
  })

  describe('Advanced disclosure (#1618/P4-T5)', () => {
    it('starts collapsed, keeping the investment-activity switch out of the document', () => {
      renderModal(
        <AccountCustomizationModal
          account={{ ...mockAccount, user_account_id: 'ua-chained-1618b' }}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      const advancedButton = screen.getByRole('button', { name: 'Advanced' })
      expect(advancedButton).toHaveAttribute('aria-expanded', 'false')
      expect(screen.queryByRole('switch', { name: /investment activity/i })).not.toBeInTheDocument()
    })

    it('reveals the investment-activity switch once opened, and hides it again on collapse', async () => {
      const user = userEvent.setup()
      renderModal(
        <AccountCustomizationModal
          account={{ ...mockAccount, user_account_id: 'ua-chained-1618b' }}
          open={true}
          onOpenChange={mockOnOpenChange}
          onSave={mockOnSave}
        />
      )

      const advancedButton = screen.getByRole('button', { name: 'Advanced' })
      await user.click(advancedButton)

      expect(advancedButton).toHaveAttribute('aria-expanded', 'true')
      expect(screen.getByRole('switch', { name: /investment activity/i })).toBeInTheDocument()

      await user.click(advancedButton)

      expect(advancedButton).toHaveAttribute('aria-expanded', 'false')
      expect(screen.queryByRole('switch', { name: /investment activity/i })).not.toBeInTheDocument()
    })
  })

})
