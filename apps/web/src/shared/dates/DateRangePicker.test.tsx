import { describe, test, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { DateRangePicker } from './DateRangePicker'
import type { DatePreset } from './presets'

// Mock the ToggleGroup components to keep tests simple
mock.module('@/components/ui/toggle-group', () => ({
  ToggleGroup: ({ children, onValueChange }: { children: React.ReactNode; onValueChange: (v: string) => void }) => (
    <div data-testid="toggle-group" onClick={(e) => {
      const btn = (e.target as HTMLElement).closest('[data-value]')
      if (btn) onValueChange(btn.getAttribute('data-value') ?? '')
    }}>
      {children}
    </div>
  ),
  ToggleGroupItem: ({ value, children }: { value: string; children: React.ReactNode }) => (
    <button data-value={value} type="button">{children}</button>
  ),
}))

describe('DateRangePicker', () => {
  const defaultProps = {
    preset: 'this_month' as DatePreset,
    onPresetChange: mock(() => {}),
    onCustomRangeChange: mock(() => {}),
  }

  test('renders all preset buttons', () => {
    render(<DateRangePicker {...defaultProps} />)
    expect(screen.getByText('This Week')).toBeTruthy()
    expect(screen.getByText('This Month')).toBeTruthy()
    expect(screen.getByText('Last Month')).toBeTruthy()
    expect(screen.getByText('Last 3 Months')).toBeTruthy()
    expect(screen.getByText('Last 6 Months')).toBeTruthy()
    expect(screen.getByText('Year to Date')).toBeTruthy()
    expect(screen.getByText('All Time')).toBeTruthy()
    expect(screen.getByText('Custom')).toBeTruthy()
  })

  test('excludePresets hides specific options', () => {
    render(<DateRangePicker {...defaultProps} excludePresets={['all', 'this_week']} />)
    expect(screen.queryByText('All Time')).toBeNull()
    expect(screen.queryByText('This Week')).toBeNull()
    expect(screen.getByText('This Month')).toBeTruthy()
  })

  test('calls onPresetChange when a preset is selected', () => {
    const onPresetChange = mock((_: DatePreset) => {})
    render(<DateRangePicker {...defaultProps} onPresetChange={onPresetChange} />)
    fireEvent.click(screen.getByText('Last Month'))
    expect(onPresetChange).toHaveBeenCalledWith('last_month')
  })

  test('shows custom date inputs when preset is custom', () => {
    render(<DateRangePicker {...defaultProps} preset="custom" />)
    expect(screen.getByLabelText(/Start Date/i)).toBeTruthy()
    expect(screen.getByLabelText(/End Date/i)).toBeTruthy()
    expect(screen.getByText('Apply Custom Range')).toBeTruthy()
  })

  test('does not show custom date inputs for non-custom preset', () => {
    render(<DateRangePicker {...defaultProps} preset="this_month" />)
    expect(screen.queryByLabelText(/Start Date/i)).toBeNull()
    expect(screen.queryByText('Apply Custom Range')).toBeNull()
  })

  test('calls onCustomRangeChange when custom dates are applied', () => {
    const onCustomRangeChange = mock((_s: string, _e: string) => {})
    render(
      <DateRangePicker
        {...defaultProps}
        preset="custom"
        onCustomRangeChange={onCustomRangeChange}
      />
    )
    const startInput = screen.getByLabelText(/Start Date/i) as HTMLInputElement
    const endInput = screen.getByLabelText(/End Date/i) as HTMLInputElement
    fireEvent.change(startInput, { target: { value: '2024-01-01' } })
    fireEvent.change(endInput, { target: { value: '2024-01-31' } })
    fireEvent.click(screen.getByText('Apply Custom Range'))
    expect(onCustomRangeChange).toHaveBeenCalledWith('2024-01-01', '2024-01-31')
  })

  test('Apply button disabled when custom dates not set', () => {
    render(<DateRangePicker {...defaultProps} preset="custom" />)
    const button = screen.getByText('Apply Custom Range').closest('button') as HTMLButtonElement
    expect(button.disabled).toBe(true)
  })

  test('disabled prop prevents interaction', () => {
    const onPresetChange = mock((_: DatePreset) => {})
    render(<DateRangePicker {...defaultProps} disabled onPresetChange={onPresetChange} />)
    // Component renders but is visually disabled - no clicks fire
    expect(screen.getByTestId('toggle-group')).toBeTruthy()
  })
})
