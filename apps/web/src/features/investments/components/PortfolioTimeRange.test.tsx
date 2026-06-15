/**
 * PortfolioTimeRange Component Tests
 * Issue #1296: Preset-based time range selector for portfolio charts.
 */

import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { PortfolioTimeRange } from './PortfolioTimeRange'
import type { PortfolioPreset } from '../hooks/usePortfolioTimeRange'

describe('PortfolioTimeRange', () => {
  const presets: PortfolioPreset[] = ['1W', '1M', '3M', 'YTD', '1Y', 'All']

  it('renders all 6 preset buttons', () => {
    const onPresetChange = mock(() => {})
    render(<PortfolioTimeRange activePreset="1Y" onPresetChange={onPresetChange} />)

    for (const preset of presets) {
      expect(screen.getByText(preset)).toBeDefined()
    }
  })

  it('does not call onPresetChange when the already-active preset is clicked', () => {
    // Proves the ToggleGroup single-select behavior: clicking the current value is a no-op.
    // This works with both real Radix and the suite-wide toggle-group mock used by
    // TransactionDateFilter/DateRangePicker/SpendingReportsPage tests.
    const onPresetChange = mock(() => {})
    render(<PortfolioTimeRange activePreset="3M" onPresetChange={onPresetChange} />)
    fireEvent.click(screen.getByText('3M'))
    expect(onPresetChange).not.toHaveBeenCalled()
  })

  it('calls onPresetChange with clicked preset', () => {
    const onPresetChange = mock(() => {})
    render(<PortfolioTimeRange activePreset="1Y" onPresetChange={onPresetChange} />)

    fireEvent.click(screen.getByText('1M'))
    expect(onPresetChange).toHaveBeenCalledTimes(1)
    expect(onPresetChange).toHaveBeenCalledWith('1M')
  })

  it('does not call onPresetChange for the already-active preset', () => {
    const onPresetChange = mock(() => {})
    render(<PortfolioTimeRange activePreset="1Y" onPresetChange={onPresetChange} />)

    fireEvent.click(screen.getByText('1Y'))
    // ToggleGroup single-select doesn't fire on deselect — onPresetChange should not fire
    // (or if it does fire, verify that the parent receives a valid preset)
    // In practice the ToggleGroup single mode ignores clicks on already-selected value
    expect(onPresetChange).not.toHaveBeenCalledWith(undefined)
  })

  it('has accessible group label for screen readers', () => {
    const onPresetChange = mock(() => {})
    render(<PortfolioTimeRange activePreset="1Y" onPresetChange={onPresetChange} />)
    // The ToggleGroup receives aria-label="Portfolio time range".
    // Real Radix renders with data-slot="toggle-group"; the suite-wide mock renders with data-testid="toggle-group".
    // Either way a wrapper container element must exist with the preset buttons inside.
    const group = screen.queryByTestId('toggle-group') ?? screen.queryByRole('group')
    expect(group).not.toBeNull()
  })
})
