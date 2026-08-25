/**
 * AccountCard Component Tests
 *
 * Tests the account card component that displays individual account
 * details including balance, institution, and account type information.
 *
 * Issue #575: Hybrid test framework migration (Vitest + bun:test)
 */

import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { AccountCard } from './AccountCard'
import type { Account } from './types'
import { scanForUnmaskedPII, type PIISentinel } from '@/test/pii-masking'

function renderWithRouter(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>)
}

describe('AccountCard', () => {
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

  it('should render account information', () => {
    renderWithRouter(<AccountCard account={mockAccount} />)

    // Check institution name
    expect(screen.getByText('Chase')).toBeInTheDocument()

    // Check account name (uses official_name if available)
    expect(screen.getByText('Chase Total Checking')).toBeInTheDocument()

    // Check account type
    expect(screen.getByText('Bank Account - Checking')).toBeInTheDocument()

    // Check mask
    expect(screen.getByText('****0000')).toBeInTheDocument()

    // Check current balance
    expect(screen.getByText('$2,500.50')).toBeInTheDocument()

    // Check available balance (should show since it differs from current)
    expect(screen.getByText('$2,300.50')).toBeInTheDocument()
  })

  it('should fallback to name when official_name is not provided', () => {
    const accountWithoutOfficialName: Account = {
      ...mockAccount,
      official_name: undefined,
    }

    renderWithRouter(<AccountCard account={accountWithoutOfficialName} />)

    expect(screen.getByText('Chase Checking')).toBeInTheDocument()
  })

  it('should display "Unknown Bank" when institution_name is missing', () => {
    const accountWithoutInstitution: Account = {
      ...mockAccount,
      institution_name: undefined,
    }

    renderWithRouter(<AccountCard account={accountWithoutInstitution} />)

    expect(screen.getByText('Unknown Bank')).toBeInTheDocument()
  })

  it('should not show mask when not provided', () => {
    const accountWithoutMask: Account = {
      ...mockAccount,
      mask: undefined,
    }

    const { container } = renderWithRouter(<AccountCard account={accountWithoutMask} />)

    expect(container.textContent).not.toContain('****')
  })

  it('should show N/A for undefined balances', () => {
    const accountWithoutBalances: Account = {
      ...mockAccount,
      current_balance: undefined,
      available_balance: undefined,
    }

    renderWithRouter(<AccountCard account={accountWithoutBalances} />)

    const balanceElements = screen.getAllByText('N/A')
    expect(balanceElements.length).toBeGreaterThan(0)
  })

  it('should not show available balance when it equals current balance', () => {
    const accountWithSameBalances: Account = {
      ...mockAccount,
      current_balance: 2500.50,
      available_balance: 2500.50,
    }

    renderWithRouter(<AccountCard account={accountWithSameBalances} />)

    // Only one instance of $2,500.50 should appear (current balance)
    const balances = screen.getAllByText('$2,500.50')
    expect(balances).toHaveLength(1)
  })

  it('should handle credit card accounts', () => {
    const creditCardAccount: Account = {
      ...mockAccount,
      type: 'credit',
      subtype: 'credit card',
      name: 'Chase Freedom',
    }

    renderWithRouter(<AccountCard account={creditCardAccount} />)

    expect(screen.getByText('Credit Card - Credit card')).toBeInTheDocument()
  })

  it('should handle loan accounts', () => {
    const loanAccount: Account = {
      ...mockAccount,
      type: 'loan',
      subtype: 'mortgage',
      name: 'Home Mortgage',
    }

    renderWithRouter(<AccountCard account={loanAccount} />)

    expect(screen.getByText('Loan - Mortgage')).toBeInTheDocument()
  })

  it('should handle investment accounts', () => {
    const investmentAccount: Account = {
      ...mockAccount,
      type: 'investment',
      subtype: '401k',
      name: 'Retirement Account',
    }

    renderWithRouter(<AccountCard account={investmentAccount} />)

    expect(screen.getByText('Investment - 401k')).toBeInTheDocument()
  })

  it('should handle account without subtype', () => {
    const accountWithoutSubtype: Account = {
      ...mockAccount,
      subtype: undefined,
    }

    renderWithRouter(<AccountCard account={accountWithoutSubtype} />)

    expect(screen.getByText('Bank Account')).toBeInTheDocument()
  })

  it('should format non-USD currencies', () => {
    const eurAccount: Account = {
      ...mockAccount,
      currency_code: 'EUR',
      current_balance: 1500.75,
    }

    renderWithRouter(<AccountCard account={eurAccount} />)

    // EUR symbol should be present
    expect(screen.getByText(/€1,500.75/)).toBeInTheDocument()
  })

  describe('summary region link (Task 6)', () => {
    it('renders a link to /accounts/:id wrapping the summary region', () => {
      renderWithRouter(<AccountCard account={mockAccount} />)

      const link = screen.getByRole('link', { name: /view bank account - checking account ending in 0000 details/i })
      expect(link).toBeInTheDocument()
      expect(link).toHaveAttribute('href', `/accounts/${mockAccount.id}`)
    })

    it('Edit and Disconnect buttons are NOT nested inside the summary link', () => {
      renderWithRouter(
        <AccountCard account={mockAccount} onEdit={mock(() => {})} onDisconnect={mock(() => {})} />
      )

      const link = screen.getByRole('link', { name: /view bank account - checking account ending in 0000 details/i })
      // Buttons should be outside the link element
      expect(link.querySelector('button')).toBeNull()
    })

    it('Edit button is present as a sibling outside the link', () => {
      const onEdit = mock(() => {})
      renderWithRouter(<AccountCard account={mockAccount} onEdit={onEdit} />)

      const link = screen.getByRole('link', { name: /view bank account - checking account ending in 0000 details/i })
      const editButton = screen.getByRole('button', { name: /edit account/i })
      // The edit button should not be a descendant of the link
      expect(link.contains(editButton)).toBe(false)
    })

    it('Disconnect button is present as a sibling outside the link', () => {
      const onDisconnect = mock(() => {})
      renderWithRouter(<AccountCard account={mockAccount} onDisconnect={onDisconnect} />)

      const link = screen.getByRole('link', { name: /view bank account - checking account ending in 0000 details/i })
      const disconnectButton = screen.getByText('Disconnect')
      expect(link.contains(disconnectButton)).toBe(false)
    })
  })

  describe('summary link accessible name excludes PII (issue #1514: aria-label is unmaskable)', () => {
    // Every DOM attribute on the link — not just aria-label — must be free of the PII.
    // This is what actually matters for PostHog session replay: it records attributes
    // verbatim regardless of which attribute name carries the leak.
    function attributeValues(el: Element): string[] {
      return Array.from(el.attributes).map((a) => a.value)
    }

    it('does not include the nickname or institution name in any DOM attribute on the link', () => {
      const accountWithNickname: Account = {
        ...mockAccount,
        nickname: 'My Secret Vacation Fund',
        institution_name: 'Chase',
      }
      const { container } = renderWithRouter(<AccountCard account={accountWithNickname} />)

      const link = container.querySelector('a[href$="/accounts/' + accountWithNickname.id + '"]') as HTMLElement
      expect(link).toBeTruthy()
      const values = attributeValues(link)
      expect(values.some((v) => v.includes('My Secret Vacation Fund'))).toBe(false)
      expect(values.some((v) => v.includes('Chase Total Checking'))).toBe(false)
      expect(values.some((v) => v.includes('Chase'))).toBe(false)

      // Belt-and-suspenders: the project's own PII scanner (used elsewhere for exactly this
      // class of bug) must find zero attribute-borne violations on the whole rendered card.
      const sentinels: PIISentinel[] = [
        { value: 'My Secret Vacation Fund', fieldDescription: 'account.nickname' },
        { value: 'Chase', fieldDescription: 'account.institution_name' },
      ]
      expect(scanForUnmaskedPII(container, sentinels)).toEqual([])
    })

    it('still distinguishes two different accounts from each other via the accessible name', () => {
      const accountA: Account = { ...mockAccount, id: 'acct-a', mask: '1111' }
      const accountB: Account = { ...mockAccount, id: 'acct-b', mask: '2222' }
      const { rerender } = renderWithRouter(<AccountCard account={accountA} />)
      expect(
        screen.getByRole('link', { name: /view bank account - checking account ending in 1111 details/i })
      ).toBeInTheDocument()

      rerender(
        <MemoryRouter>
          <AccountCard account={accountB} />
        </MemoryRouter>
      )
      expect(
        screen.getByRole('link', { name: /view bank account - checking account ending in 2222 details/i })
      ).toBeInTheDocument()
    })

    it('omits the mask clause (no "undefined") when mask is absent', () => {
      const accountWithoutMask: Account = { ...mockAccount, mask: undefined }
      const { container } = renderWithRouter(<AccountCard account={accountWithoutMask} />)

      const link = screen.getByRole('link', { name: /view bank account - checking account details/i })
      expect(container.textContent).not.toContain('undefined')
      expect(attributeValues(link).some((v) => v.includes('undefined'))).toBe(false)
      expect(attributeValues(link).some((v) => v.includes('ending in'))).toBe(false)
    })

    it('does not put account.id in any rendered attribute other than the link href (issue #1514)', () => {
      const { container } = renderWithRouter(<AccountCard account={mockAccount} />)

      const violations: string[] = []
      container.querySelectorAll('*').forEach((el) => {
        Array.from(el.attributes).forEach((attr) => {
          if (attr.name === 'href') return
          if (attr.value.includes(mockAccount.id)) {
            violations.push(`${el.tagName.toLowerCase()}[${attr.name}="${attr.value}"]`)
          }
        })
      })
      expect(violations).toEqual([])

      // The href is the one accepted exception and must still carry the id (routing dependency).
      const link = container.querySelector(`a[href="/accounts/${mockAccount.id}"]`)
      expect(link).toBeTruthy()

      // The accessible name must still compute correctly from the masked sr-only span.
      const accessibleLink = screen.getByRole('link', { name: /view bank account - checking account ending in 0000 details/i })
      expect(accessibleLink).toBe(link)
    })
  })

  describe('disconnected badge copy (issue #1495: generalised, cause-agnostic wording)', () => {
    it('shows "This connection is no longer active" rather than a subscription-specific claim', () => {
      const disconnectedAccount: Account = {
        ...mockAccount,
        linked_item_status: 'disconnected',
      }

      renderWithRouter(<AccountCard account={disconnectedAccount} />)

      const badge = screen.getByText('Disconnected')
      expect(badge.closest('[title]')).toHaveAttribute('title', 'This connection is no longer active')
    })
  })

  describe('closed accounts (Issue #1569: retained but no longer active)', () => {
    it('shows a "Closed" badge for a removed account', () => {
      const removedAccount: Account = {
        ...mockAccount,
        status: 'removed',
        removed_at: '2026-03-01T00:00:00Z',
      }

      renderWithRouter(<AccountCard account={removedAccount} />)

      expect(screen.getByText('Closed')).toBeInTheDocument()
    })

    it('does not show a "Closed" badge for an active account', () => {
      renderWithRouter(<AccountCard account={mockAccount} />)
      expect(screen.queryByText('Closed')).toBeNull()
    })

    // Issue #1580. This test used to assert 'Last known balance' + /as of/ against a fixture
    // where a REMOVED account still carried current_balance: 2500.50 — a shape production cannot
    // produce (accountBalanceProjection nulls both balance columns for every terminal status).
    // The real rendering was "Last known balance: N/A … as of Mar 1, 2026": a label, and a date,
    // with nothing between them.
    it('shows no "last known" label and no as-of date for a removed account', () => {
      const removedAccount: Account = {
        ...mockAccount,
        status: 'removed',
        removed_at: '2026-03-01T00:00:00Z',
        current_balance: undefined,
        available_balance: undefined,
      }

      renderWithRouter(<AccountCard account={removedAccount} />)

      expect(screen.queryByText(/last known/i)).toBeNull()
      expect(screen.queryByText(/as of/i)).toBeNull()
      expect(screen.getByText(/closed accounts do not carry a current balance/i)).toBeInTheDocument()
    })

    // Issue #1569 task 21, item D. The user's ruling: "If account is closed, treat as empty
    // balance, we don't want orphaned balances." The API now OMITS current_balance and
    // available_balance for a terminal-status account, so the card receives `undefined` for both.
    //
    // The thing that must not happen is a rendered "$0.00": that is a positive claim that the
    // account is empty, which is a different and false statement from "we no longer know". The
    // fixtures in the two tests above both still carry a balance, so neither of them would notice
    // if this regressed.
    it('renders no balance at all — and no misleading $0.00 — for a closed account', () => {
      const closedAccount: Account = {
        ...mockAccount,
        status: 'removed',
        removed_at: '2026-03-01T00:00:00Z',
        current_balance: undefined,
        available_balance: undefined,
      }

      renderWithRouter(<AccountCard account={closedAccount} />)

      expect(screen.queryByText('$0.00')).toBeNull()
      expect(screen.queryByText(/\$2,500/)).toBeNull()
      // Issue #1580: and the reason is stated, rather than left as a bare N/A the user has to
      // interpret.
      expect(screen.getByText(/closed accounts do not carry a current balance/i)).toBeInTheDocument()
    })
  })

  describe('onDisconnect behavior (Task 8: remove window.confirm)', () => {
    it('calls onDisconnect with the full account object when Disconnect is clicked', () => {
      const onDisconnect = mock(() => {})
      renderWithRouter(<AccountCard account={mockAccount} onDisconnect={onDisconnect} />)

      fireEvent.click(screen.getByText('Disconnect'))

      expect(onDisconnect).toHaveBeenCalledTimes(1)
      expect(onDisconnect).toHaveBeenCalledWith(mockAccount)
    })
  })

  // Issue #1569, Task 6: a closed account's actions are purge-only — Edit/Disconnect both act on
  // a LIVE account and must not render for one that is already closed (carried-over Task 5
  // reviewer finding: closed rows previously kept working Edit/Delete actions that harmlessly
  // re-ran UpdateStatus(removed)).
  describe('closed account actions (Issue #1569, Task 6: purge-only)', () => {
    const closedAccount: Account = {
      ...mockAccount,
      status: 'removed',
      removed_at: '2026-03-01T00:00:00Z',
    }

    it('does not render Edit or Disconnect for a closed account', () => {
      renderWithRouter(
        <AccountCard
          account={closedAccount}
          onEdit={mock(() => {})}
          onDisconnect={mock(() => {})}
          onPurge={mock(() => {})}
        />
      )

      expect(screen.queryByRole('button', { name: /edit account/i })).toBeNull()
      expect(screen.queryByText('Disconnect')).toBeNull()
    })

    it('renders a "Delete from history" purge action for a closed account', () => {
      const onPurge = mock(() => {})
      renderWithRouter(<AccountCard account={closedAccount} onPurge={onPurge} />)

      fireEvent.click(screen.getByText('Delete from history'))

      expect(onPurge).toHaveBeenCalledTimes(1)
      expect(onPurge).toHaveBeenCalledWith(closedAccount)
    })

    it('does not render the purge action for an active account', () => {
      renderWithRouter(<AccountCard account={mockAccount} onPurge={mock(() => {})} />)
      expect(screen.queryByText('Delete from history')).toBeNull()
    })
  })
})
