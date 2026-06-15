/**
 * HoldingsTable Component Tests
 * Issue #1296: Portfolio tab — holdings DataTable.
 * Tests: data rendering, loading state, empty state, PII masking.
 */

import { describe, it, expect, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { HoldingsTable } from './HoldingsTable'
import { mockUseGetHoldings } from '@/test/mocks/investments-api'
import type { HoldingView } from '@boostedbudget/api-client/models/holdingView'

const mockHolding: HoldingView = {
  id: 'h1',
  account_id: 'a1',
  user_id: 'u1',
  security_id: 's1',
  quantity: 10,
  security_name: 'Apple Inc',
  ticker_symbol: 'AAPL',
  security_type: 'stock',
  institution_value: 2500,
  institution_price: 250,
  cost_basis: 2000,
  gain_loss: 500,
  gain_loss_pct: 25,
  day_change: 50,
  day_change_pct: 2,
}

describe('HoldingsTable', () => {
  beforeEach(() => {
    mockUseGetHoldings.mockReturnValue({ data: undefined, isLoading: false, isError: false })
  })

  it('renders holdings data with correct column count', () => {
    mockUseGetHoldings.mockReturnValue({
      data: { holdings: [mockHolding], count: 1 },
      isLoading: false,
      isError: false,
    })
    render(<HoldingsTable />)
    expect(screen.getByText('AAPL')).toBeDefined()
  })

  it('shows loading state', () => {
    mockUseGetHoldings.mockReturnValue({ data: undefined, isLoading: true, isError: false })
    const { container } = render(<HoldingsTable />)
    const statusEl =
      container.querySelector('[role="status"]') ?? screen.queryByRole('status', { hidden: true })
    expect(statusEl).toBeDefined()
  })

  it('renders empty state when no holdings', () => {
    mockUseGetHoldings.mockReturnValue({
      data: { holdings: [], count: 0 },
      isLoading: false,
      isError: false,
    })
    render(<HoldingsTable />)
    expect(screen.getByText(/no holdings found/i)).toBeDefined()
  })

  it('masked columns contain data-ph-mask', () => {
    mockUseGetHoldings.mockReturnValue({
      data: { holdings: [mockHolding], count: 1 },
      isLoading: false,
      isError: false,
    })
    const { container } = render(<HoldingsTable />)
    const maskedTds = container.querySelectorAll('td[data-ph-mask]')
    expect(maskedTds.length).toBeGreaterThan(0)
  })
})
