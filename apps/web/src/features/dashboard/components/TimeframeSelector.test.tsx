/**
 * TimeframeSelector Component Tests
 * Issue #832: Updated for ToggleGroup design system integration
 * Issue #953: Updated for calendar-aligned presets via shared DateRangePicker
 */

import { describe, it, expect, mock, beforeEach, afterEach, setSystemTime } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { TimeframeSelector } from './TimeframeSelector'

// Issue #1323: Toggle-group mock retained here — with the real Radix ToggleGroup,
// clicking the default-selected value ('this_month') doesn't re-fire onValueChange
// (Radix prevents re-selection in single mode). Removing this mock breaks the
// 'This Month' test because the component initializes with that preset selected.
// To clean this up, the test should click a different preset first, then 'This Month'.
//
// Keep the container's role/aria-label in sync with src/test/mocks/toggle-group.tsx.
// bun's mock.module is process-global, so this inline mock overrides the shared one for
// every test file that runs after this one in the same process (the pollution #1323 exists
// to prevent). When it omitted role and aria-label, PortfolioTimeRange's accessible-name
// assertion could not hold.
mock.module('@/components/ui/toggle-group', () => ({
  ToggleGroup: ({ children, onValueChange, type, 'aria-label': ariaLabel }: { children: React.ReactNode; onValueChange: (v: string) => void; type?: 'single' | 'multiple'; 'aria-label'?: string }) => (
    <div role={type === 'multiple' ? 'toolbar' : 'radiogroup'} aria-label={ariaLabel} data-testid="toggle-group" onClick={(e) => {
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

describe('TimeframeSelector', () => {
  const mockOnRangeChange = mock(() => {})
  // 2025-01-15 is a Wednesday
  const mockDate = new Date('2025-01-15T12:00:00.000Z')

  beforeEach(() => {
    mockOnRangeChange.mockClear()
    setSystemTime(mockDate)
  })

  afterEach(() => {
    setSystemTime()
  })

  describe('Rendering', () => {
    it('should render all preset buttons', () => {
      render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      expect(screen.getByText('This Week')).toBeTruthy()
      expect(screen.getByText('This Month')).toBeTruthy()
      expect(screen.getByText('Last Month')).toBeTruthy()
      expect(screen.getByText('Last 3 Months')).toBeTruthy()
      expect(screen.getByText('Last 6 Months')).toBeTruthy()
      expect(screen.getByText('Year to Date')).toBeTruthy()
      expect(screen.getByText('All Time')).toBeTruthy()
      expect(screen.getByText('Custom')).toBeTruthy()
    })

    it('should render inside timeframe-selector wrapper', () => {
      render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)
      expect(screen.getByTestId('timeframe-selector')).toBeTruthy()
    })

    it('should not show custom date inputs by default', () => {
      render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      expect(screen.queryByLabelText(/Start Date/i)).toBeNull()
      expect(screen.queryByLabelText(/End Date/i)).toBeNull()
    })
  })

  describe('Preset Period Selection', () => {
    // 2025-01-15 is Wednesday; Monday of that week = Jan 13
    const testCases: [string, string, string][] = [
      ['This Week', '2025-01-13', '2025-01-15'],
      // Ends on the frozen "today", not on Jan 31: this_month no longer emits a future end
      // date (#1626 Phase 3, Defect 1).
      ['This Month', '2025-01-01', '2025-01-15'],
      ['Last Month', '2024-12-01', '2024-12-31'],
      ['Last 3 Months', '2024-10-15', '2025-01-15'],
      ['Last 6 Months', '2024-07-15', '2025-01-15'],
      ['Year to Date', '2025-01-01', '2025-01-15'],
    ]

    testCases.forEach(([label, startDate, endDate]) => {
      it(`should call onRangeChange with correct dates for ${label}`, () => {
        render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)
        fireEvent.click(screen.getByText(label))
        expect(mockOnRangeChange).toHaveBeenCalledWith(startDate, endDate)
      })
    })

    it('should call onRangeChange with empty strings for All Time', () => {
      render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)
      fireEvent.click(screen.getByText('All Time'))
      expect(mockOnRangeChange).toHaveBeenCalledWith('', '')
    })

    it('should not call onRangeChange when Custom is selected', () => {
      render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)
      fireEvent.click(screen.getByText('Custom'))
      expect(mockOnRangeChange).not.toHaveBeenCalled()
    })
  })

  describe('Custom Date Range', () => {
    it('should show date inputs when Custom is selected', () => {
      render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      fireEvent.click(screen.getByText('Custom'))

      expect(screen.getByLabelText(/Start Date/i)).toBeTruthy()
      expect(screen.getByLabelText(/End Date/i)).toBeTruthy()
      expect(screen.getByText('Apply Custom Range')).toBeTruthy()
    })

    it('should have Apply button disabled when no dates entered', () => {
      render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      fireEvent.click(screen.getByText('Custom'))

      const applyButton = screen.getByText('Apply Custom Range').closest('button') as HTMLButtonElement
      expect(applyButton.disabled).toBe(true)
    })

    it('should call onRangeChange with custom dates when Apply clicked', () => {
      render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      fireEvent.click(screen.getByText('Custom'))

      const startInput = screen.getByLabelText(/Start Date/i) as HTMLInputElement
      const endInput = screen.getByLabelText(/End Date/i) as HTMLInputElement

      fireEvent.change(startInput, { target: { value: '2025-01-01' } })
      fireEvent.change(endInput, { target: { value: '2025-01-15' } })

      fireEvent.click(screen.getByText('Apply Custom Range'))

      expect(mockOnRangeChange).toHaveBeenCalledWith('2025-01-01', '2025-01-15')
    })

    it('should hide custom inputs when switching to preset', () => {
      render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      fireEvent.click(screen.getByText('Custom'))
      expect(screen.getByLabelText(/Start Date/i)).toBeTruthy()

      fireEvent.click(screen.getByText('This Week'))
      expect(screen.queryByLabelText(/Start Date/i)).toBeNull()
    })
  })
})
