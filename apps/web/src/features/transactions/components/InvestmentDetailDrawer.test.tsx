import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { InvestmentDetailDrawer } from './InvestmentDetailDrawer'
import type { InvestmentActivity } from '../types'

function createWrapper() {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        {children}
      </QueryClientProvider>
    )
  }
}

const baseActivity: InvestmentActivity = {
  id: 'inv-1',
  user_id: 'user-1',
  account_id: 'account-inv',
  source: 'investment' as const,
  amount: -1500.0,
  date: '2025-10-25',
  name: 'Buy AAPL',
  ticker_symbol: 'AAPL',
  security_name: 'Apple Inc.',
  activity_type: 'buy',
  investment_quantity: 10,
  investment_price: 150.0,
  investment_fees: 0.0,
  created_at: '2025-10-25T10:00:00Z',
  updated_at: '2025-10-25T10:00:00Z',
}

type AccountsMap = Record<string, { nickname?: string | null; name?: string | null }>

function renderDrawer(
  transaction: InvestmentActivity | null,
  open: boolean,
  onOpenChange: (open: boolean) => void = () => {},
  accountsMap?: AccountsMap,
) {
  const Wrapper = createWrapper()
  return render(
    <Wrapper>
      <InvestmentDetailDrawer
        transaction={transaction}
        open={open}
        onOpenChange={onOpenChange}
        accountsMap={accountsMap}
      />
    </Wrapper>,
  )
}

describe('InvestmentDetailDrawer', () => {
  it('renders nothing when transaction is null', () => {
    renderDrawer(null, false)
    expect(screen.queryByText('Investment Activity')).toBeNull()
  })

  it('renders the sheet title "Investment Activity" when open', () => {
    renderDrawer(baseActivity, true)
    expect(screen.getByText('Investment Activity')).toBeInTheDocument()
  })

  it('renders security name and ticker symbol when open', () => {
    renderDrawer(baseActivity, true)
    expect(screen.getByTestId('security-name-value').textContent).toContain('Apple Inc.')
    expect(screen.getByTestId('ticker-symbol-value').textContent).toContain('AAPL')
  })

  it('renders ActivityTypeChip with the activity type label', () => {
    renderDrawer(baseActivity, true)
    // ActivityTypeChip renders "Buy" label for 'buy' activity type
    expect(screen.getByText('Buy')).toBeInTheDocument()
  })

  it('renders quantity with "shares" label', () => {
    renderDrawer(baseActivity, true)
    const qtyEl = screen.getByTestId('quantity-value')
    expect(qtyEl.textContent).toContain('10')
    expect(qtyEl.textContent).toContain('shares')
  })

  it('renders price per share formatted as USD', () => {
    renderDrawer(baseActivity, true)
    const priceEl = screen.getByTestId('price-value')
    expect(priceEl.textContent).toContain('$150.00')
  })

  it('renders account nickname when accountsMap is provided', () => {
    const accountsMap = { 'account-inv': { nickname: 'Brokerage ••1234', name: 'Brokerage Account' } }
    renderDrawer(baseActivity, true, undefined, accountsMap)
    const accountEl = screen.getByTestId('account-value')
    expect(accountEl.textContent).toContain('Brokerage ••1234')
  })

  it('falls back to account name when nickname is absent', () => {
    const accountsMap = { 'account-inv': { nickname: null, name: 'Brokerage Account' } }
    renderDrawer(baseActivity, true, undefined, accountsMap)
    const accountEl = screen.getByTestId('account-value')
    expect(accountEl.textContent).toContain('Brokerage Account')
  })

  it('falls back to raw account_id when not found in accountsMap', () => {
    renderDrawer(baseActivity, true, undefined, {})
    const accountEl = screen.getByTestId('account-value')
    expect(accountEl.textContent).toContain('account-inv')
  })

  it('falls back to raw account_id when no accountsMap provided', () => {
    renderDrawer(baseActivity, true)
    const accountEl = screen.getByTestId('account-value')
    expect(accountEl.textContent).toContain('account-inv')
  })

  it('renders date in human-readable format', () => {
    renderDrawer(baseActivity, true)
    // "Oct 25, 2025" — via Intl.DateTimeFormat
    expect(screen.getByTestId('date-value').textContent).toContain('Oct 25, 2025')
  })

  it('shows "—" for fees when investment_fees is 0', () => {
    renderDrawer({ ...baseActivity, investment_fees: 0 }, true)
    const feesEl = screen.getByTestId('fees-value')
    expect(feesEl.textContent).toBe('—')
  })

  it('shows "—" for fees when investment_fees is null', () => {
    renderDrawer({ ...baseActivity, investment_fees: null }, true)
    const feesEl = screen.getByTestId('fees-value')
    expect(feesEl.textContent).toBe('—')
  })

  it('shows formatted fees when non-zero', () => {
    const activity = { ...baseActivity, investment_fees: 4.99 }
    renderDrawer(activity, true)
    const feesEl = screen.getByTestId('fees-value')
    expect(feesEl.textContent).toContain('$4.99')
  })

  it('renders inflow amount with + prefix (PFCv2: negative = inflow)', () => {
    // baseActivity.amount = -1500 → inflow → +$1,500.00
    renderDrawer(baseActivity, true)
    const amountEl = screen.getByTestId('amount-value')
    expect(amountEl.textContent).toBe('+$1,500.00')
  })

  it('renders outflow amount with - prefix (PFCv2: positive = outflow)', () => {
    renderDrawer({ ...baseActivity, amount: 250.0 }, true)
    const amountEl = screen.getByTestId('amount-value')
    expect(amountEl.textContent).toBe('-$250.00')
  })

  it('renders zero amount without sign prefix', () => {
    renderDrawer({ ...baseActivity, amount: 0 }, true)
    const amountEl = screen.getByTestId('amount-value')
    expect(amountEl.textContent).toBe('$0.00')
  })

  it('has NO edit controls — no Edit, Categorize, Save, or Tag buttons', () => {
    renderDrawer(baseActivity, true)
    expect(screen.queryByRole('button', { name: /edit/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /categorize/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /save/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /^tag/i })).toBeNull()
  })

  it('applies data-ph-mask to security name header (SheetDescription)', () => {
    renderDrawer(baseActivity, true)
    expect(screen.getByTestId('security-name-header').hasAttribute('data-ph-mask')).toBe(true)
  })

  it('applies data-ph-mask to security name element', () => {
    renderDrawer(baseActivity, true)
    expect(screen.getByTestId('security-name-value').hasAttribute('data-ph-mask')).toBe(true)
  })

  it('applies data-ph-mask to ticker symbol element', () => {
    renderDrawer(baseActivity, true)
    expect(screen.getByTestId('ticker-symbol-value').hasAttribute('data-ph-mask')).toBe(true)
  })

  it('applies data-ph-mask to quantity element', () => {
    renderDrawer(baseActivity, true)
    expect(screen.getByTestId('quantity-value').hasAttribute('data-ph-mask')).toBe(true)
  })

  it('applies data-ph-mask to price element', () => {
    renderDrawer(baseActivity, true)
    expect(screen.getByTestId('price-value').hasAttribute('data-ph-mask')).toBe(true)
  })

  it('applies data-ph-mask to fees element', () => {
    renderDrawer(baseActivity, true)
    expect(screen.getByTestId('fees-value').hasAttribute('data-ph-mask')).toBe(true)
  })

  it('applies data-ph-mask to amount element', () => {
    renderDrawer(baseActivity, true)
    expect(screen.getByTestId('amount-value').hasAttribute('data-ph-mask')).toBe(true)
  })

  it('applies data-ph-mask to account element', () => {
    renderDrawer(baseActivity, true)
    expect(screen.getByTestId('account-value').hasAttribute('data-ph-mask')).toBe(true)
  })

  it('applies data-ph-mask to date element (trade timing is PII)', () => {
    renderDrawer(baseActivity, true)
    expect(screen.getByTestId('date-value').hasAttribute('data-ph-mask')).toBe(true)
  })

  it('calls onOpenChange(false) when close button is clicked', () => {
    const onOpenChange = mock((v: boolean) => v)

    const Wrapper = createWrapper()
    render(
      <Wrapper>
        <InvestmentDetailDrawer
          transaction={baseActivity}
          open={true}
          onOpenChange={onOpenChange}
        />
      </Wrapper>,
    )

    const closeBtn = screen.getByRole('button', { name: /close/i })
    fireEvent.click(closeBtn)
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })
})
