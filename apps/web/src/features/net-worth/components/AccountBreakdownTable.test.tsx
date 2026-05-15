import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { AccountBreakdownTable } from './AccountBreakdownTable'
import type { AccountBreakdownRow } from '../types'
import type { SortColumn, SortDirection } from '../hooks/useNetWorthBreakdownFilters'

const asset: AccountBreakdownRow = {
  id: 'acc-1',
  name: 'Checking',
  type: 'depository',
  institutionName: 'Chase',
  currencyCode: 'USD',
  currentBalance: 5000,
  percentOfSide: 50,
  side: 'assets',
}

const asset2: AccountBreakdownRow = {
  id: 'acc-2',
  name: 'Savings',
  type: 'depository',
  institutionName: 'Citibank',
  currencyCode: 'USD',
  currentBalance: 5000,
  percentOfSide: 50,
  side: 'assets',
}

const liability: AccountBreakdownRow = {
  id: 'acc-3',
  name: 'Visa Card',
  type: 'credit',
  institutionName: 'Chase',
  currencyCode: 'USD',
  currentBalance: -2000,
  percentOfSide: 20,
  side: 'liabilities',
}

const defaultSortState = {
  sortColumn: null as SortColumn | null,
  sortDirection: 'asc' as SortDirection,
  onSort: mock(() => {}),
}

describe('AccountBreakdownTable', () => {
  it('renders Assets section caption', () => {
    const { container } = render(
      <AccountBreakdownTable
        assets={[asset]}
        liabilities={[]}
        assetsTotal={5000}
        liabilitiesTotal={0}
        loading={false}
        error={null}
        {...defaultSortState}
      />
    )
    const caption = container.querySelector('caption')
    expect(caption?.textContent).toMatch(/Assets/)
  })

  it('renders Liabilities section caption', () => {
    const { container } = render(
      <AccountBreakdownTable
        assets={[]}
        liabilities={[liability]}
        assetsTotal={0}
        liabilitiesTotal={2000}
        loading={false}
        error={null}
        {...defaultSortState}
      />
    )
    const caption = container.querySelector('caption')
    expect(caption?.textContent).toMatch(/Liabilities/)
  })

  it('renders account name in each row', () => {
    render(
      <AccountBreakdownTable
        assets={[asset]}
        liabilities={[liability]}
        assetsTotal={5000}
        liabilitiesTotal={2000}
        loading={false}
        error={null}
        {...defaultSortState}
      />
    )
    expect(screen.getByText('Checking')).toBeDefined()
    expect(screen.getByText('Visa Card')).toBeDefined()
  })

  it('renders Institution column header', () => {
    render(
      <AccountBreakdownTable
        assets={[asset]}
        liabilities={[]}
        assetsTotal={5000}
        liabilitiesTotal={0}
        loading={false}
        error={null}
        {...defaultSortState}
      />
    )
    // TableHeaderCell renders the label text as-is; CSS uppercase is visual only
    expect(screen.getByText('Institution')).toBeDefined()
  })

  it('renders institutionName for each row', () => {
    render(
      <AccountBreakdownTable
        assets={[asset, asset2]}
        liabilities={[]}
        assetsTotal={10000}
        liabilitiesTotal={0}
        loading={false}
        error={null}
        {...defaultSortState}
      />
    )
    expect(screen.getByText('Chase')).toBeDefined()
    expect(screen.getByText('Citibank')).toBeDefined()
  })

  it('does NOT render ••••mask anywhere in the table', () => {
    const rowWithMask = { ...asset, name: 'Plaid Checking' }
    render(
      <AccountBreakdownTable
        assets={[rowWithMask]}
        liabilities={[]}
        assetsTotal={5000}
        liabilitiesTotal={0}
        loading={false}
        error={null}
        {...defaultSortState}
      />
    )
    expect(screen.queryByText(/••••/)).toBeNull()
  })

  it('renders subtotal formatted currency in assets caption', () => {
    render(
      <AccountBreakdownTable
        assets={[asset]}
        liabilities={[]}
        assetsTotal={154014}
        liabilitiesTotal={0}
        loading={false}
        error={null}
        {...defaultSortState}
      />
    )
    // Caption should show the total
    expect(screen.getByText(/154,014/)).toBeDefined()
  })

  it('shows explicit empty-filter message when assets zeroed by filter', () => {
    render(
      <AccountBreakdownTable
        assets={[]}
        liabilities={[liability]}
        assetsTotal={5000}  // non-zero total means assets exist but were filtered out
        liabilitiesTotal={2000}
        loading={false}
        error={null}
        {...defaultSortState}
      />
    )
    expect(screen.getByText(/No asset accounts match the current filters/i)).toBeDefined()
  })

  it('omits assets section entirely when assetsTotal is 0', () => {
    render(
      <AccountBreakdownTable
        assets={[]}
        liabilities={[liability]}
        assetsTotal={0}
        liabilitiesTotal={2000}
        loading={false}
        error={null}
        {...defaultSortState}
      />
    )
    expect(screen.queryByText(/Assets/)).toBeNull()
  })

  it('shows explicit empty-filter message when liabilities zeroed by filter', () => {
    render(
      <AccountBreakdownTable
        assets={[asset]}
        liabilities={[]}
        assetsTotal={5000}
        liabilitiesTotal={2000}  // non-zero means some exist but were filtered
        loading={false}
        error={null}
        {...defaultSortState}
      />
    )
    expect(screen.getByText(/No liability accounts match the current filters/i)).toBeDefined()
  })

  it('renders sort buttons for all columns', () => {
    render(
      <AccountBreakdownTable
        assets={[asset]}
        liabilities={[]}
        assetsTotal={5000}
        liabilitiesTotal={0}
        loading={false}
        error={null}
        {...defaultSortState}
      />
    )
    // TableHeaderCell renders buttons for sortable columns
    const buttons = screen.getAllByRole('button')
    expect(buttons.length).toBeGreaterThanOrEqual(4) // at least Institution, Account, Balance, %
  })

  it('calls onSort when a sortable header is clicked', () => {
    const onSort = mock(() => {})
    render(
      <AccountBreakdownTable
        assets={[asset]}
        liabilities={[]}
        assetsTotal={5000}
        liabilitiesTotal={0}
        loading={false}
        error={null}
        sortColumn={null}
        sortDirection="asc"
        onSort={onSort}
      />
    )
    // Click the Institution header button
    const institutionBtn = screen.getAllByRole('button').find(
      (b) => b.textContent?.includes('Institution') || b.closest('th')?.textContent?.includes('Institution')
    )
    if (institutionBtn) fireEvent.click(institutionBtn)
    expect(onSort).toHaveBeenCalled()
  })

  it('applies data-ph-mask to balance cells but NOT percent cells', () => {
    const { container } = render(
      <AccountBreakdownTable
        assets={[asset]}
        liabilities={[]}
        assetsTotal={5000}
        liabilitiesTotal={0}
        loading={false}
        error={null}
        {...defaultSortState}
      />
    )
    const maskedCells = container.querySelectorAll('[data-ph-mask]')
    expect(maskedCells.length).toBeGreaterThan(0)
    // Masked cells should be balance cells (show currency), not percent cells (show %)
    for (const cell of maskedCells) {
      expect(cell.textContent).not.toMatch(/\d+\.\d+%/)
    }
  })

  it('shows loading state', () => {
    render(
      <AccountBreakdownTable
        assets={[]}
        liabilities={[]}
        assetsTotal={0}
        liabilitiesTotal={0}
        loading={true}
        error={null}
        {...defaultSortState}
      />
    )
    expect(screen.getByRole('status')).toBeDefined()
  })

  it('shows error state', () => {
    render(
      <AccountBreakdownTable
        assets={[]}
        liabilities={[]}
        assetsTotal={0}
        liabilitiesTotal={0}
        loading={false}
        error={new Error('oops')}
        {...defaultSortState}
      />
    )
    expect(screen.getByText(/oops/)).toBeDefined()
  })

  it('shows empty state when both totals are 0 and no rows', () => {
    render(
      <AccountBreakdownTable
        assets={[]}
        liabilities={[]}
        assetsTotal={0}
        liabilitiesTotal={0}
        loading={false}
        error={null}
        {...defaultSortState}
      />
    )
    expect(screen.getByText(/No accounts found/i)).toBeDefined()
  })

  it('uses overflow-x-auto wrapper for mobile', () => {
    const { container } = render(
      <AccountBreakdownTable
        assets={[asset]}
        liabilities={[]}
        assetsTotal={5000}
        liabilitiesTotal={0}
        loading={false}
        error={null}
        {...defaultSortState}
      />
    )
    const wrapper = container.querySelector('.overflow-x-auto')
    expect(wrapper).not.toBeNull()
  })

  it('does NOT show empty state when assetsTotal is 0 but rows exist (zero-balance accounts)', () => {
    const zeroBalanceAsset: AccountBreakdownRow = { ...asset, currentBalance: 0, percentOfSide: 0 }
    render(
      <AccountBreakdownTable
        assets={[zeroBalanceAsset]}
        liabilities={[]}
        assetsTotal={0}
        liabilitiesTotal={0}
        loading={false}
        error={null}
        {...defaultSortState}
      />
    )
    expect(screen.queryByText(/No accounts found/i)).toBeNull()
    expect(screen.getByText('Chase')).toBeDefined()
  })

  it('renders th with aria-sort="ascending" on active sort column', () => {
    const { container } = render(
      <AccountBreakdownTable
        assets={[asset]}
        liabilities={[]}
        assetsTotal={5000}
        liabilitiesTotal={0}
        loading={false}
        error={null}
        sortColumn="balance"
        sortDirection="asc"
        onSort={mock(() => {})}
      />
    )
    const sortedTh = container.querySelector('th[aria-sort="ascending"]')
    expect(sortedTh).not.toBeNull()
  })
})
