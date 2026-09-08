import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { AccountTable } from './AccountTable'
import type { Account } from '@boostedbudget/api-client/models'
import { mockUseUser, mockClerkUser } from '@/test/mocks/clerk'

// Mock account factory for reusable test data
function createMockAccount(overrides?: Partial<Account>): Account {
  return {
    id: 'acc-1',
    user_id: 'user-1',
    linked_item_id: 'item-1',
    plaid_account_id: 'plaid-1',
    name: 'Primary Checking',
    official_name: 'Chase Total Checking',
    type: 'depository',
    subtype: 'checking',
    mask: '1234',
    current_balance: 2500.5,
    available_balance: 2400.0,
    currency_code: 'USD',
    institution_name: 'Chase',
    is_hidden: false,
    exclude_investment_sync: false,
    status: 'active',
    display_order: 0,
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    ...overrides,
  }
}

const mockAccounts: Account[] = [
  createMockAccount({
    nickname: 'My Checking',
    custom_color: '#3B82F6',
    custom_emoji: '💰',
  }),
  createMockAccount({
    id: 'acc-2',
    name: 'Savings Account',
    plaid_account_id: 'plaid-2',
    subtype: 'savings',
    mask: '5678',
    current_balance: 15000.0,
    available_balance: undefined,
    display_order: 1,
    created_at: '2024-01-02T00:00:00Z',
    updated_at: '2024-01-02T00:00:00Z',
  }),
  createMockAccount({
    id: 'acc-3',
    linked_item_id: 'item-2',
    name: 'Credit Card',
    plaid_account_id: 'plaid-3',
    type: 'credit',
    subtype: 'credit card',
    mask: '9012',
    current_balance: -1250.75,
    available_balance: undefined,
    institution_name: 'American Express',
    is_hidden: true,
    display_order: 2,
    created_at: '2024-01-03T00:00:00Z',
    updated_at: '2024-01-03T00:00:00Z',
  }),
]

describe('AccountTable', () => {
  describe('Rendering', () => {
    it('should render a table', () => {
      render(<AccountTable accounts={mockAccounts} />)

      expect(screen.getByRole('table')).toBeInTheDocument()
    })

    it('should render table headers', () => {
      render(<AccountTable accounts={mockAccounts} />)

      expect(screen.getByText('Institution')).toBeInTheDocument()
      expect(screen.getByText('Account')).toBeInTheDocument()
      expect(screen.getByText('Type')).toBeInTheDocument()
      expect(screen.getByText('Acct #')).toBeInTheDocument()
      expect(screen.getByText('Balance')).toBeInTheDocument()
    })

    it('should render all accounts', () => {
      render(<AccountTable accounts={mockAccounts} />)

      const chaseLabels = screen.getAllByText('Chase')
      expect(chaseLabels.length).toBe(2)
      expect(screen.getByText('American Express')).toBeInTheDocument()
    })

    it('should display nickname when available, otherwise account name', () => {
      render(<AccountTable accounts={mockAccounts} />)

      expect(screen.getByText('My Checking')).toBeInTheDocument()
      expect(screen.getByText('Savings Account')).toBeInTheDocument()
    })

    it('should display account type with human-readable label', () => {
      render(<AccountTable accounts={mockAccounts} />)

      expect(screen.getByText('Checking')).toBeInTheDocument()
      expect(screen.getByText('Savings')).toBeInTheDocument()
      expect(screen.getAllByText('Credit Card').length).toBeGreaterThan(0)
    })

    it('should display masked account number', () => {
      render(<AccountTable accounts={mockAccounts} />)

      expect(screen.getByText('••••1234')).toBeInTheDocument()
      expect(screen.getByText('••••5678')).toBeInTheDocument()
      expect(screen.getByText('••••9012')).toBeInTheDocument()
    })

    it('should display formatted balance', () => {
      render(<AccountTable accounts={mockAccounts} />)

      expect(screen.getByText('$2,500.50')).toBeInTheDocument()
      expect(screen.getByText('$15,000.00')).toBeInTheDocument()
    })

    it('should show hidden badge for hidden accounts', () => {
      render(<AccountTable accounts={mockAccounts} />)

      expect(screen.getAllByText('Hidden').length).toBe(1)
    })

    it('should display custom emoji when available', () => {
      render(<AccountTable accounts={mockAccounts} />)

      expect(screen.getByText('💰')).toBeInTheDocument()
    })

    it('should show empty state when no accounts', () => {
      render(<AccountTable accounts={[]} />)

      expect(screen.getByText(/no accounts found/i)).toBeInTheDocument()
    })

    it('should apply loading class when loading', () => {
      render(<AccountTable accounts={mockAccounts} loading />)

      expect(screen.getByRole('table')).toHaveClass('account-table--loading')
    })
  })

  describe('Balance Styling', () => {
    it('should apply positive class to positive balances', () => {
      render(<AccountTable accounts={mockAccounts} />)
      expect(screen.getByText('$2,500.50')).toHaveClass('account-table__balance--positive')
    })

    it('should apply negative class to negative balances', () => {
      render(<AccountTable accounts={mockAccounts} />)
      expect(screen.getByText('-$1,250.75')).toHaveClass('account-table__balance--negative')
    })
  })

  describe('Disconnected badge copy (issue #1495: generalised, cause-agnostic wording)', () => {
    it('shows "This connection is no longer active" rather than a subscription-specific claim', () => {
      const accountsWithDisconnected = [
        createMockAccount({ linked_item_status: 'disconnected' }),
        ...mockAccounts.slice(1),
      ]

      render(<AccountTable accounts={accountsWithDisconnected} />)

      const badge = screen.getByText('Disconnected')
      expect(badge.closest('[title]')).toHaveAttribute('title', 'This connection is no longer active')
    })
  })

  describe('Closed accounts (Issue #1569: retained but no longer active)', () => {
    // fix round 1, item 1: T7 deleted the "as of <date>" caption because the BALANCE it labelled
    // was never sent — but the date itself was real, and it was the only place in the app the
    // closure date appeared (`grep -rn "removed_at\|replaced_at" src/` matched test files only).
    // A user who closed an account in March and looks in August must still be told when.
    it('says WHEN the account closed, not merely that it did', () => {
      const accountsWithClosed = [
        createMockAccount({
          status: 'removed',
          removed_at: '2026-03-01T00:00:00Z',
          current_balance: undefined,
          available_balance: undefined,
        }),
      ]

      render(<AccountTable accounts={accountsWithClosed} />)

      expect(screen.getByText('Closed Mar 1, 2026')).toBeInTheDocument()
    })

    // #1569 T8.5 — NEGATIVE assertion is the point. `isInactive` covers `replaced`, `expired` and
    // `removed`, and `removed` has three causes (user_disconnect / provider_absent / link_removed);
    // accounts.removed_reason is not on the wire, so this table cannot know which. The old tooltip
    // said "This account was disconnected" for all of them. Asserting only the new wording would
    // let the false attribution be restored with the test still green.
    it('does not tell the user they disconnected an account whose cause it cannot know', () => {
      render(<AccountTable accounts={[createMockAccount({ status: 'removed', removed_at: '2026-03-01T00:00:00Z' })]} />)

      const title = screen.getByText('Closed Mar 1, 2026').closest('[title]')?.getAttribute('title') ?? ''

      expect(title).not.toMatch(/was disconnected/i)
      expect(title).toMatch(/no longer active/i)
      expect(title).toMatch(/history is retained/i)
    })

    it('uses replaced_at for a replaced account, not the edit timestamp', () => {
      const accountsWithClosed = [
        createMockAccount({
          status: 'replaced',
          replaced_at: '2026-02-01T00:00:00Z',
          updated_at: '2026-07-14T00:00:00Z',
          current_balance: undefined,
        }),
      ]

      render(<AccountTable accounts={accountsWithClosed} />)

      expect(screen.getByText('Closed Feb 1, 2026')).toBeInTheDocument()
      expect(screen.queryByText(/Jul 14/)).toBeNull()
    })

    it('shows a "Closed" badge for a removed account', () => {
      const accountsWithClosed = [
        createMockAccount({ status: 'removed', removed_at: '2026-03-01T00:00:00Z' }),
        ...mockAccounts.slice(1),
      ]

      render(<AccountTable accounts={accountsWithClosed} />)

      // The badge now carries the closure date (fix round 1, item 1), so match its prefix.
      expect(screen.getByText(/^Closed /)).toBeInTheDocument()
    })

    // Issue #1580. The old assertion here read `getByText(/as of/i)` against a fixture where a
    // REMOVED account still carried current_balance: 2500.50 — a row production cannot produce,
    // since accountBalanceProjection nulls both balance columns for every terminal status. So it
    // pinned a caption ("as of Mar 1, 2026") that in production only ever appeared underneath an
    // em dash. The caption is gone; the cell now says why the figure is absent.
    it('never renders a stranded "as of" caption for a closed account', () => {
      const accountsWithClosed = [
        createMockAccount({
          status: 'removed',
          removed_at: '2026-03-01T00:00:00Z',
          current_balance: undefined,
          available_balance: undefined,
        }),
        ...mockAccounts.slice(1),
      ]

      render(<AccountTable accounts={accountsWithClosed} />)

      expect(screen.queryByText(/as of/i)).toBeNull()
    })

    it('says the closed account has no current balance instead of leaving a bare em dash', () => {
      const accountsWithClosed = [
        createMockAccount({
          status: 'removed',
          removed_at: '2026-03-01T00:00:00Z',
          current_balance: undefined,
          available_balance: undefined,
        }),
      ]

      render(<AccountTable accounts={accountsWithClosed} />)

      expect(screen.getByText('No current balance')).toBeInTheDocument()
    })

    // Mutation-driven: dropping the isClosed guard on the amount survived every other test in
    // this block, because a production closed row carries no balance and formatAccountBalance
    // renders '—' anyway. The guard is defence in depth against a stale cache or a hand-built
    // payload restating a frozen figure as a current one, and this is the only test that sees it.
    it('refuses to render a balance even if a closed row somehow carries one', () => {
      const accountsWithClosed = [
        createMockAccount({
          status: 'removed',
          removed_at: '2026-03-01T00:00:00Z',
          current_balance: 2500.5,
          available_balance: 2500.5,
        }),
      ]

      render(<AccountTable accounts={accountsWithClosed} />)

      expect(screen.queryByText(/\$2,500/)).toBeNull()
      expect(screen.getByText('—')).toBeInTheDocument()
      expect(screen.getByText('No current balance')).toBeInTheDocument()
    })

    it('does not claim "no current balance" for an active account', () => {
      render(<AccountTable accounts={mockAccounts} />)
      expect(screen.queryByText('No current balance')).toBeNull()
    })

    // Issue #1569 task 21, item D — the table path's half of the same ruling. AccountTable
    // formats with `{ empty: '—' }` rather than 'N/A', so the two views need separate assertions;
    // one passing does not imply the other.
    it('renders an em dash, not $0.00, for a closed account with no balance', () => {
      const accountsWithClosed = [
        createMockAccount({
          status: 'removed',
          removed_at: '2026-03-01T00:00:00Z',
          current_balance: undefined,
          available_balance: undefined,
        }),
      ]

      render(<AccountTable accounts={accountsWithClosed} />)

      expect(screen.getByText('—')).toBeInTheDocument()
      expect(screen.queryByText('$0.00')).toBeNull()
      expect(screen.queryByText(/\$2,500/)).toBeNull()
    })

  })

  describe('Row Actions', () => {
    it('should call onEdit when edit button is clicked', () => {
      const onEdit = mock(() => {})
      render(<AccountTable accounts={mockAccounts} onEdit={onEdit} />)

      const editButtons = screen.getAllByRole('button', { name: /edit/i })
      fireEvent.click(editButtons[0])

      expect(onEdit).toHaveBeenCalledWith(mockAccounts[0])
    })

    it('should call onDisconnect when disconnect button is clicked', () => {
      const onDisconnect = mock(() => {})
      render(<AccountTable accounts={mockAccounts} onDisconnect={onDisconnect} />)

      const disconnectButtons = screen.getAllByRole('button', { name: /disconnect/i })
      fireEvent.click(disconnectButtons[0])

      expect(onDisconnect).toHaveBeenCalledWith(mockAccounts[0])
    })

    it('should call onPurge when purge button is clicked for a closed account', () => {
      const onPurge = mock(() => {})
      const closedAccounts = [createMockAccount({ status: 'removed', removed_at: '2026-03-01T00:00:00Z' })]
      render(<AccountTable accounts={closedAccounts} onPurge={onPurge} />)

      const purgeButtons = screen.getAllByRole('button', { name: /permanently delete/i })
      fireEvent.click(purgeButtons[0])

      expect(onPurge).toHaveBeenCalledWith(closedAccounts[0])
    })

    // ── Issue #1591: the last-known balance in the Balance cell. ─────────────────────────────
    //
    // The cell's primary line stays an em dash — a closed account has no current balance and never
    // will. What changes is the sub-line: "No current balance" is what we say when there is
    // nothing else to say, and it is REPLACED by a dated figure when one exists. Fixtures keep
    // current_balance undefined throughout, because that is the only shape production emits for a
    // terminal account (an earlier version of this file asserted against a `removed` account still
    // carrying 2500.50 — a row the backend cannot produce).
    it('shows a dated last-known balance for a closed account that has one', () => {
      const closed = [createMockAccount({
        status: 'removed',
        removed_at: '2026-03-01T00:00:00Z',
        current_balance: undefined,
        available_balance: undefined,
        last_known_balance: 4321,
        last_known_balance_at: '2026-02-24T00:00:00Z',
      })]
      render(<AccountTable accounts={closed} />)

      expect(screen.getByText(/last known \$4,321\.00/i)).toBeInTheDocument()
      expect(screen.getByText(/as of feb 24, 2026/i)).toBeInTheDocument()
      // The primary figure is still absent; the historical one does not take its place.
      expect(screen.queryByText('$4,321.00')).toBeNull()
      expect(screen.queryByText(/no current balance/i)).toBeNull()
    })

    it('renders a recorded ZERO as a figure, not as an absence', () => {
      const closed = [createMockAccount({
        status: 'removed',
        removed_at: '2026-03-01T00:00:00Z',
        current_balance: undefined,
        available_balance: undefined,
        last_known_balance: 0,
        last_known_balance_at: '2026-02-24T00:00:00Z',
      })]
      render(<AccountTable accounts={closed} />)

      expect(screen.getByText(/last known \$0\.00/i)).toBeInTheDocument()
      expect(screen.queryByText(/no current balance/i)).toBeNull()
    })

    it('falls back to "No current balance" when no snapshot is on record', () => {
      const closed = [createMockAccount({
        status: 'removed',
        removed_at: '2026-03-01T00:00:00Z',
        current_balance: undefined,
        available_balance: undefined,
      })]
      render(<AccountTable accounts={closed} />)

      expect(screen.getByText(/no current balance/i)).toBeInTheDocument()
      expect(screen.queryByText(/last known/i)).toBeNull()
      expect(screen.queryByText(/as of/i)).toBeNull()
    })

    it('shows no last-known balance on an ACTIVE account, whatever the payload carries', () => {
      const live = [createMockAccount({
        last_known_balance: 4321,
        last_known_balance_at: '2026-02-24T00:00:00Z',
      })]
      render(<AccountTable accounts={live} />)

      expect(screen.queryByText(/last known/i)).toBeNull()
      expect(screen.getByText('$2,500.50')).toBeInTheDocument()
    })

    it('should not render Edit or Disconnect for a closed account', () => {
      const closedAccounts = [createMockAccount({ status: 'removed', removed_at: '2026-03-01T00:00:00Z' })]
      render(
        <AccountTable
          accounts={closedAccounts}
          onEdit={mock(() => {})}
          onDisconnect={mock(() => {})}
          onPurge={mock(() => {})}
        />
      )

      expect(screen.queryByRole('button', { name: /^edit account$/i })).toBeNull()
      expect(screen.queryByRole('button', { name: /^disconnect account$/i })).toBeNull()
    })

    it('should not show actions column when no handlers provided', () => {
      render(<AccountTable accounts={mockAccounts} />)

      expect(screen.queryByText('Actions')).not.toBeInTheDocument()
    })

    it('should show actions column when handlers provided', () => {
      render(<AccountTable accounts={mockAccounts} onEdit={() => {}} />)

      expect(screen.getByText('Actions')).toBeInTheDocument()
    })

    it('should disable actions when loading', () => {
      render(
        <AccountTable
          accounts={mockAccounts}
          onEdit={() => {}}
          onDisconnect={() => {}}
          loading
        />
      )

      const editButtons = screen.getAllByRole('button', { name: /edit/i })
      const disconnectButtons = screen.getAllByRole('button', { name: /disconnect/i })

      editButtons.forEach((btn) => expect(btn).toBeDisabled())
      disconnectButtons.forEach((btn) => expect(btn).toBeDisabled())
    })
  })

  describe('Row Click', () => {
    it('should call onRowClick when row is clicked', () => {
      const onRowClick = mock(() => {})
      render(<AccountTable accounts={mockAccounts} onRowClick={onRowClick} />)

      const rows = screen.getAllByRole('row')
      fireEvent.click(rows[1])

      expect(onRowClick).toHaveBeenCalledWith(mockAccounts[0])
    })

    it('should make rows clickable when onRowClick provided', () => {
      render(<AccountTable accounts={mockAccounts} onRowClick={() => {}} />)

      const rows = screen.getAllByRole('row')
      expect(rows[1]).toHaveClass('account-table__row--clickable')
    })
  })

  describe('Sorting', () => {
    it('makes name column sortable when onSortChange is provided', () => {
      render(<AccountTable accounts={mockAccounts} onSortChange={mock(() => {})} />)

      const nameHeader = screen.getByText('Account')
      expect(nameHeader.closest('button')).toBeInTheDocument()
    })

    it('calls onSortChange with asc on first click', () => {
      const onSortChange = mock(() => {})
      render(<AccountTable accounts={mockAccounts} onSortChange={onSortChange} />)

      fireEvent.click(screen.getByText('Account').closest('button')!)
      expect(onSortChange).toHaveBeenCalledWith({ columnId: 'name', direction: 'asc' })
    })

    it('calls onSortChange with desc when sorted asc and clicked', () => {
      const onSortChange = mock(() => {})
      render(
        <AccountTable
          accounts={mockAccounts}
          onSortChange={onSortChange}
          sort={{ columnId: 'name', direction: 'asc' }}
        />
      )

      fireEvent.click(screen.getByText('Account').closest('button')!)
      expect(onSortChange).toHaveBeenCalledWith({ columnId: 'name', direction: 'desc' })
    })

    it('calls onSortChange(null) on third click — clears sort', () => {
      const onSortChange = mock(() => {})
      render(
        <AccountTable
          accounts={mockAccounts}
          onSortChange={onSortChange}
          sort={{ columnId: 'name', direction: 'desc' }}
        />
      )

      fireEvent.click(screen.getByText('Account').closest('button')!)
      expect(onSortChange).toHaveBeenCalledWith(null)
    })

    it('shows sort indicator on sorted column', () => {
      render(
        <AccountTable
          accounts={mockAccounts}
          onSortChange={mock(() => {})}
          sort={{ columnId: 'name', direction: 'asc' }}
        />
      )

      const nameTh = screen.getByRole('columnheader', { name: /account/i })
      expect(nameTh).toHaveAttribute('aria-sort', 'ascending')
    })
  })

  describe('Accessibility', () => {
    it('should have accessible table structure', () => {
      render(<AccountTable accounts={mockAccounts} />)

      expect(screen.getByRole('table')).toBeInTheDocument()
      expect(screen.getAllByRole('rowgroup').length).toBe(2)
      expect(screen.getAllByRole('row').length).toBe(4)
    })

    it('should have accessible column headers', () => {
      render(<AccountTable accounts={mockAccounts} />)

      const headers = screen.getAllByRole('columnheader')
      expect(headers.length).toBeGreaterThan(0)
    })

    it('should have aria-label on action buttons', () => {
      render(
        <AccountTable
          accounts={mockAccounts}
          onEdit={() => {}}
          onDisconnect={() => {}}
        />
      )

      const editButtons = screen.getAllByRole('button', { name: /edit account/i })
      const disconnectButtons = screen.getAllByRole('button', { name: /disconnect account/i })

      expect(editButtons.length).toBe(3)
      expect(disconnectButtons.length).toBe(3)
    })
  })

  describe('Phase 5 – DataTable opt-in features', () => {
    beforeEach(() => {
      mockUseUser.mockReturnValue({ user: mockClerkUser, isLoaded: true, isSignedIn: true })
    })

    it('renders data-ph-mask on institution, name, mask, and balance columns', () => {
      const { container } = render(<AccountTable accounts={mockAccounts} />)
      const maskedTds = container.querySelectorAll('td[data-ph-mask]')
      // institution + name + mask + balance = 4 masked cells per row
      expect(maskedTds.length).toBe(mockAccounts.length * 4)
    })

    it('renders resize handles when resizable is true', () => {
      const { container } = render(<AccountTable accounts={mockAccounts} />)
      expect(container.querySelectorAll('[data-testid="resize-handle"]').length).toBeGreaterThan(0)
    })

    it('renders drag handles when reorderable is true', () => {
      const { container } = render(<AccountTable accounts={mockAccounts} />)
      expect(container.querySelectorAll('[data-testid="drag-handle"]').length).toBeGreaterThan(0)
    })

    it('balance column th has data-pinned="right"', () => {
      render(<AccountTable accounts={mockAccounts} />)
      const balanceTh = screen.getByRole('columnheader', { name: /balance/i })
      expect(balanceTh).toHaveAttribute('data-pinned', 'right')
    })

    it('mask column appears in visibility menu', () => {
      render(<AccountTable accounts={mockAccounts} />)
      const columnsButton = screen.getByRole('button', { name: /columns/i })
      fireEvent.click(columnsButton)
      expect(screen.getByRole('menuitemcheckbox', { name: /acct #/i })).toBeInTheDocument()
    })

    it('thead has sticky class and 4rem top offset for global header clearance', () => {
      const { container } = render(<AccountTable accounts={mockAccounts} />)
      const thead = container.querySelector('thead')
      expect(thead).not.toBeNull()
      expect(thead!.className).toContain('sticky')
      expect(thead!.style.top).toBe('4rem')
    })
  })

  describe('Action Button Tooltips', () => {
    it('should have tooltip on edit button', () => {
      render(<AccountTable accounts={mockAccounts} onEdit={mock(() => {})} />)
      screen.getAllByRole('button', { name: /edit account/i }).forEach((btn) => {
        expect(btn).toHaveAttribute('title', 'Edit account')
      })
    })

    it('should have tooltip on disconnect button', () => {
      render(<AccountTable accounts={mockAccounts} onDisconnect={mock(() => {})} />)
      screen.getAllByRole('button', { name: /disconnect account/i }).forEach((btn) => {
        expect(btn).toHaveAttribute('title', 'Stop syncing this account, retaining its history')
      })
    })

    it('should have tooltip on purge button for a closed account', () => {
      const closedAccounts = [createMockAccount({ status: 'removed', removed_at: '2026-03-01T00:00:00Z' })]
      render(<AccountTable accounts={closedAccounts} onPurge={mock(() => {})} />)
      screen.getAllByRole('button', { name: /permanently delete account/i }).forEach((btn) => {
        expect(btn).toHaveAttribute('title', 'Permanently delete this account and its history')
      })
    })
  })
})
