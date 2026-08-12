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

  it('has accessible group label for screen readers', () => {
    const onPresetChange = mock(() => {})
    render(<PortfolioTimeRange activePreset="1Y" onPresetChange={onPresetChange} />)
    // Assert the accessible NAME, not just that a container exists. This previously
    // fell back to queryByTestId, which passes even if aria-label is deleted from
    // PortfolioTimeRange.tsx — the test could not fail for the reason it claims to check.
    // The shared ToggleGroup mock now forwards aria-label, so the role+name query works.
    // radiogroup, not group: this is <ToggleGroup type="single"> (see toggle-group mock).
    expect(screen.getByRole('radiogroup', { name: 'Portfolio time range' })).toBeDefined()
  })
})
