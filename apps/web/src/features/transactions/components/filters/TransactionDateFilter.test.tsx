/**
 * TransactionDateFilter Component Tests
 * Issue #529: Transaction page filtering
 * Issue #953: Updated for shared DateRangePicker with calendar-aligned presets
 */

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { TransactionDateFilter } from './TransactionDateFilter'

// Mock ToggleGroup so tests don't depend on Radix internals
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

describe('TransactionDateFilter', () => {
  const mockOnPresetChange = mock(() => {})
  const mockOnCustomRangeChange = mock(() => {})

  beforeEach(() => {
    mockOnPresetChange.mockClear()
    mockOnCustomRangeChange.mockClear()
  })

  const defaultProps = {
    preset: 'all' as const,
    onPresetChange: mockOnPresetChange,
    onCustomRangeChange: mockOnCustomRangeChange,
  }

  it('should render all preset buttons', () => {
    render(<TransactionDateFilter {...defaultProps} />)

    expect(screen.getByRole('button', { name: 'This Week' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'This Month' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Last Month' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Last 3 Months' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Last 6 Months' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Year to Date' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'All Time' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Custom' })).toBeInTheDocument()
  })

  it('should call onPresetChange when preset clicked', async () => {
    const user = userEvent.setup()
    const onPresetChange = mock(() => {})

    render(<TransactionDateFilter {...defaultProps} onPresetChange={onPresetChange} />)

    await user.click(screen.getByRole('button', { name: 'This Week' }))

    expect(onPresetChange).toHaveBeenCalledWith('this_week')
  })

  it('should show custom date inputs when preset is custom', () => {
    render(<TransactionDateFilter {...defaultProps} preset="custom" />)

    expect(screen.getByLabelText(/Start Date/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/End Date/i)).toBeInTheDocument()
  })

  it('should not show custom inputs for non-custom preset', () => {
    render(<TransactionDateFilter {...defaultProps} preset="this_month" />)

    expect(screen.queryByLabelText(/Start Date/i)).not.toBeInTheDocument()
  })

  it('should call onCustomRangeChange when Apply button clicked', async () => {
    const user = userEvent.setup()
    const onCustomRangeChange = mock(() => {})

    render(
      <TransactionDateFilter
        {...defaultProps}
        preset="custom"
        onCustomRangeChange={onCustomRangeChange}
      />
    )

    const startInput = screen.getByLabelText(/Start Date/i) as HTMLInputElement
    const endInput = screen.getByLabelText(/End Date/i) as HTMLInputElement

    fireEvent.change(startInput, { target: { value: '2025-01-01' } })
    fireEvent.change(endInput, { target: { value: '2025-06-30' } })

    await user.click(screen.getByRole('button', { name: 'Apply Custom Range' }))

    expect(onCustomRangeChange).toHaveBeenCalledWith('2025-01-01', '2025-06-30')
  })

  it('should disable Apply button when dates are incomplete', () => {
    render(<TransactionDateFilter {...defaultProps} preset="custom" />)

    const applyButton = screen.getByRole('button', { name: 'Apply Custom Range' })
    expect(applyButton).toBeDisabled()
  })

  it('should display initial date values', () => {
    render(
      <TransactionDateFilter
        {...defaultProps}
        preset="custom"
        startDate="2025-01-01"
        endDate="2025-06-30"
      />
    )

    const startInput = screen.getByLabelText(/Start Date/i) as HTMLInputElement
    const endInput = screen.getByLabelText(/End Date/i) as HTMLInputElement

    expect(startInput.value).toBe('2025-01-01')
    expect(endInput.value).toBe('2025-06-30')
  })

  it('should render the Date Range label', () => {
    render(<TransactionDateFilter {...defaultProps} />)

    expect(screen.getByText('Date Range')).toBeInTheDocument()
  })
})
