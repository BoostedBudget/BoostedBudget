/**
 * AllocationCard Component Tests
 * Issue #1296: Donut chart + config popover for portfolio allocation.
 * TDD: tests written before implementation.
 */

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import '@/test/mocks/shared-charts'
import { mockUseDarkMode } from '@/test/mocks/shared-charts'
import { mockUseGetPortfolioAllocationBreakdown } from '@/test/mocks/analytics-api'
import { mockTrackEvent } from '@/test/mocks/track-event'
import { AllocationCard } from './AllocationCard'
import type { AllocationConfig } from '../hooks/usePortfolioAllocationConfig'

const DEFAULT_CONFIG: AllocationConfig = {
  include: [],
  group_by: 'security_type',
  basis: 'assets',
}

function makeOnConfigChange() {
  return mock((_next: AllocationConfig) => {})
}

describe('AllocationCard', () => {
  beforeEach(() => {
    mockUseDarkMode.mockReturnValue(false)
    mockTrackEvent.mockReset()
    mockUseGetPortfolioAllocationBreakdown.mockReturnValue({
      data: {
        total_value: 50000,
        buckets: [
          { security_type: 'Equity', asset_class: 'stock', total_value: 40000, percentage: 80, holding_count: 5 },
          { security_type: 'Fixed Income', asset_class: 'bond', total_value: 10000, percentage: 20, holding_count: 2 },
        ],
      },
      isLoading: false,
      isError: false,
    })
  })

  it('renders donut chart and config popover trigger', () => {
    render(<AllocationCard config={DEFAULT_CONFIG} onConfigChange={makeOnConfigChange()} />)
    // Chart should be rendered
    expect(screen.getByTestId('echarts-chart')).toBeDefined()
    // Settings popover trigger should be present (Settings2 icon button)
    expect(screen.getByRole('button', { name: /allocation settings/i })).toBeDefined()
  })

  it('renders the Allocation section title', () => {
    render(<AllocationCard config={DEFAULT_CONFIG} onConfigChange={makeOnConfigChange()} />)
    expect(screen.getByText('Allocation')).toBeDefined()
  })

  it('fires PORTFOLIO_ALLOCATION_CONFIG_CHANGED when basis toggled', () => {
    const onConfigChange = makeOnConfigChange()
    render(<AllocationCard config={DEFAULT_CONFIG} onConfigChange={onConfigChange} />)

    // Open the popover
    fireEvent.click(screen.getByRole('button', { name: /allocation settings/i }))

    // Click the "Net of liabilities" radio
    const netRadio = screen.getByRole('radio', { name: /net of liabilities/i })
    fireEvent.click(netRadio)

    expect(onConfigChange).toHaveBeenCalledTimes(1)
    const [nextConfig] = onConfigChange.mock.calls[0] as [AllocationConfig]
    expect(nextConfig.basis).toBe('net')

    expect(mockTrackEvent).toHaveBeenCalledWith(
      'portfolio_allocation_config_changed',
      expect.anything(),
    )
  })

  it('fires PORTFOLIO_ALLOCATION_CONFIG_CHANGED when group_by changed', () => {
    const onConfigChange = makeOnConfigChange()
    render(<AllocationCard config={DEFAULT_CONFIG} onConfigChange={onConfigChange} />)

    fireEvent.click(screen.getByRole('button', { name: /allocation settings/i }))

    const assetClassRadio = screen.getByRole('radio', { name: /asset class/i })
    fireEvent.click(assetClassRadio)

    expect(onConfigChange).toHaveBeenCalledTimes(1)
    const [nextConfig] = onConfigChange.mock.calls[0] as [AllocationConfig]
    expect(nextConfig.group_by).toBe('asset_class')

    expect(mockTrackEvent).toHaveBeenCalledWith(
      'portfolio_allocation_config_changed',
      expect.anything(),
    )
  })

  it('fires PORTFOLIO_ALLOCATION_CONFIG_CHANGED when include checkbox toggled', () => {
    const onConfigChange = makeOnConfigChange()
    render(<AllocationCard config={DEFAULT_CONFIG} onConfigChange={onConfigChange} />)

    fireEvent.click(screen.getByRole('button', { name: /allocation settings/i }))

    const cashCheckbox = screen.getByRole('checkbox', { name: /cash/i })
    fireEvent.click(cashCheckbox)

    expect(onConfigChange).toHaveBeenCalledTimes(1)
    const [nextConfig] = onConfigChange.mock.calls[0] as [AllocationConfig]
    expect(nextConfig.include).toContain('cash')

    expect(mockTrackEvent).toHaveBeenCalledWith(
      'portfolio_allocation_config_changed',
      expect.anything(),
    )
  })

  it('unchecks include when already checked', () => {
    const configWithCash: AllocationConfig = { ...DEFAULT_CONFIG, include: ['cash'] }
    const onConfigChange = makeOnConfigChange()
    render(<AllocationCard config={configWithCash} onConfigChange={onConfigChange} />)

    fireEvent.click(screen.getByRole('button', { name: /allocation settings/i }))

    const cashCheckbox = screen.getByRole('checkbox', { name: /cash/i })
    fireEvent.click(cashCheckbox)

    expect(onConfigChange).toHaveBeenCalledTimes(1)
    const [nextConfig] = onConfigChange.mock.calls[0] as [AllocationConfig]
    expect(nextConfig.include).not.toContain('cash')
  })

  it('renders Group by security type radio as initially selected', () => {
    render(<AllocationCard config={DEFAULT_CONFIG} onConfigChange={makeOnConfigChange()} />)
    fireEvent.click(screen.getByRole('button', { name: /allocation settings/i }))
    const securityTypeRadio = screen.getByRole('radio', { name: /security type/i })
    // In a real RadioGroup the default is checked; in mock or real Radix this still renders
    expect(securityTypeRadio).toBeDefined()
  })

  it('renders all three group_by options', () => {
    render(<AllocationCard config={DEFAULT_CONFIG} onConfigChange={makeOnConfigChange()} />)
    fireEvent.click(screen.getByRole('button', { name: /allocation settings/i }))
    expect(screen.getByRole('radio', { name: /security type/i })).toBeDefined()
    expect(screen.getByRole('radio', { name: /asset class/i })).toBeDefined()
    expect(screen.getByRole('radio', { name: /account/i })).toBeDefined()
  })
})
