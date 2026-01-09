/**
 * TimeframeSelector Component Tests
 * Issue #564: Frontend Test Coverage Improvement
 */

import { describe, it, expect, mock, beforeEach, afterEach, setSystemTime } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { TimeframeSelector } from './TimeframeSelector'

describe('TimeframeSelector', () => {
  const mockOnRangeChange = mock(() => {})
  const mockDate = new Date('2025-01-15T12:00:00.000Z')

  beforeEach(() => {
    mockOnRangeChange.mockClear()
    setSystemTime(mockDate)
  })

  afterEach(() => {
    setSystemTime()
  })

  describe('Rendering', () => {
    it('should render the component with title', () => {
      render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      expect(screen.getByText('Timeframe')).toBeInTheDocument()
    })

    it('should render all preset buttons', () => {
      render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      expect(screen.getByText('7 Days')).toBeInTheDocument()
      expect(screen.getByText('30 Days')).toBeInTheDocument()
      expect(screen.getByText('90 Days')).toBeInTheDocument()
      expect(screen.getByText('1 Year')).toBeInTheDocument()
      expect(screen.getByText('All Time')).toBeInTheDocument()
      expect(screen.getByText('Custom')).toBeInTheDocument()
    })

    it('should have 30 Days selected by default', () => {
      render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      const button30d = screen.getByText('30 Days')
      expect(button30d.className).toContain('bg-blue-600')
    })

    it('should not show custom date inputs by default', () => {
      render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      expect(screen.queryByText('Start Date')).not.toBeInTheDocument()
      expect(screen.queryByText('End Date')).not.toBeInTheDocument()
    })
  })

  describe('Preset Period Selection', () => {
    it('should call onRangeChange with correct dates for 7 Days', () => {
      render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      fireEvent.click(screen.getByText('7 Days'))

      expect(mockOnRangeChange).toHaveBeenCalledWith('2025-01-08', '2025-01-15')
    })

    it('should call onRangeChange with correct dates for 30 Days', () => {
      render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      fireEvent.click(screen.getByText('30 Days'))

      expect(mockOnRangeChange).toHaveBeenCalledWith('2024-12-16', '2025-01-15')
    })

    it('should call onRangeChange with correct dates for 90 Days', () => {
      render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      fireEvent.click(screen.getByText('90 Days'))

      expect(mockOnRangeChange).toHaveBeenCalledWith('2024-10-17', '2025-01-15')
    })

    it('should call onRangeChange with correct dates for 1 Year', () => {
      render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      fireEvent.click(screen.getByText('1 Year'))

      expect(mockOnRangeChange).toHaveBeenCalledWith('2024-01-15', '2025-01-15')
    })

    it('should call onRangeChange with correct dates for All Time', () => {
      render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      fireEvent.click(screen.getByText('All Time'))

      expect(mockOnRangeChange).toHaveBeenCalledWith('2015-01-15', '2025-01-15')
    })

    it('should not call onRangeChange when Custom is selected', () => {
      render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      fireEvent.click(screen.getByText('Custom'))

      expect(mockOnRangeChange).not.toHaveBeenCalled()
    })
  })

  describe('Button Selection State', () => {
    it('should update selected button styling when clicked', () => {
      render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      const button7d = screen.getByText('7 Days')
      const button30d = screen.getByText('30 Days')

      // Initially 30d is selected
      expect(button30d.className).toContain('bg-blue-600')
      expect(button7d.className).not.toContain('bg-blue-600')

      // Click 7d
      fireEvent.click(button7d)

      // Now 7d should be selected
      expect(button7d.className).toContain('bg-blue-600')
      expect(button30d.className).not.toContain('bg-blue-600')
    })

    it('should show unselected button with gray styling', () => {
      render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      const button7d = screen.getByText('7 Days')
      expect(button7d.className).toContain('bg-gray-100')
    })
  })

  describe('Custom Date Range', () => {
    it('should show date inputs when Custom is selected', () => {
      render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      fireEvent.click(screen.getByText('Custom'))

      expect(screen.getByText('Start Date')).toBeInTheDocument()
      expect(screen.getByText('End Date')).toBeInTheDocument()
      expect(screen.getByText('Apply Custom Range')).toBeInTheDocument()
    })

    it('should have Apply button disabled when no dates entered', () => {
      render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      fireEvent.click(screen.getByText('Custom'))

      const applyButton = screen.getByText('Apply Custom Range')
      expect(applyButton).toBeDisabled()
    })

    it('should have Apply button disabled when only start date entered', () => {
      const { container } = render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      fireEvent.click(screen.getByText('Custom'))

      // Get date inputs by their type attribute
      const dateInputs = Array.from(container.querySelectorAll('input[type="date"]')) as HTMLInputElement[]
      fireEvent.change(dateInputs[0], { target: { value: '2025-01-01' } })

      const applyButton = screen.getByText('Apply Custom Range')
      expect(applyButton).toBeDisabled()
    })

    it('should have Apply button disabled when only end date entered', () => {
      const { container } = render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      fireEvent.click(screen.getByText('Custom'))

      const dateInputs = Array.from(container.querySelectorAll('input[type="date"]')) as HTMLInputElement[]
      fireEvent.change(dateInputs[1], { target: { value: '2025-01-15' } })

      const applyButton = screen.getByText('Apply Custom Range')
      expect(applyButton).toBeDisabled()
    })

    it('should enable Apply button when both dates entered', () => {
      const { container } = render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      fireEvent.click(screen.getByText('Custom'))

      const dateInputs = Array.from(container.querySelectorAll('input[type="date"]')) as HTMLInputElement[]

      fireEvent.change(dateInputs[0], { target: { value: '2025-01-01' } })
      fireEvent.change(dateInputs[1], { target: { value: '2025-01-15' } })

      const applyButton = screen.getByText('Apply Custom Range')
      expect(applyButton).not.toBeDisabled()
    })

    it('should call onRangeChange with custom dates when Apply clicked', () => {
      const { container } = render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      fireEvent.click(screen.getByText('Custom'))

      const dateInputs = Array.from(container.querySelectorAll('input[type="date"]')) as HTMLInputElement[]

      fireEvent.change(dateInputs[0], { target: { value: '2025-01-01' } })
      fireEvent.change(dateInputs[1], { target: { value: '2025-01-15' } })

      fireEvent.click(screen.getByText('Apply Custom Range'))

      expect(mockOnRangeChange).toHaveBeenCalledWith('2025-01-01', '2025-01-15')
    })

    it('should hide custom inputs when switching to preset', () => {
      render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      // Select Custom
      fireEvent.click(screen.getByText('Custom'))
      expect(screen.getByText('Start Date')).toBeInTheDocument()

      // Select 7 Days
      fireEvent.click(screen.getByText('7 Days'))
      expect(screen.queryByText('Start Date')).not.toBeInTheDocument()
    })
  })

  describe('Date Input Constraints', () => {
    it('should set max date on start input to end date when set', () => {
      const { container } = render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      fireEvent.click(screen.getByText('Custom'))

      const dateInputs = Array.from(container.querySelectorAll('input[type="date"]')) as HTMLInputElement[]
      const startInput = dateInputs[0]
      const endInput = dateInputs[1]

      fireEvent.change(endInput, { target: { value: '2025-01-10' } })

      expect(startInput.max).toBe('2025-01-10')
    })

    it('should set min date on end input to start date when set', () => {
      const { container } = render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      fireEvent.click(screen.getByText('Custom'))

      const dateInputs = Array.from(container.querySelectorAll('input[type="date"]')) as HTMLInputElement[]
      const startInput = dateInputs[0]
      const endInput = dateInputs[1]

      fireEvent.change(startInput, { target: { value: '2025-01-05' } })

      expect(endInput.min).toBe('2025-01-05')
    })

    it('should set max date on end input to today', () => {
      const { container } = render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      fireEvent.click(screen.getByText('Custom'))

      const dateInputs = Array.from(container.querySelectorAll('input[type="date"]')) as HTMLInputElement[]
      const endInput = dateInputs[1]

      expect(endInput.max).toBe('2025-01-15')
    })
  })

  describe('Edge Cases', () => {
    it('should handle multiple preset selections', () => {
      render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      fireEvent.click(screen.getByText('7 Days'))
      fireEvent.click(screen.getByText('90 Days'))
      fireEvent.click(screen.getByText('1 Year'))

      expect(mockOnRangeChange).toHaveBeenCalledTimes(3)
      expect(mockOnRangeChange).toHaveBeenLastCalledWith('2024-01-15', '2025-01-15')
    })

    it('should not call onRangeChange when Apply clicked with empty dates', () => {
      render(<TimeframeSelector onRangeChange={mockOnRangeChange} />)

      fireEvent.click(screen.getByText('Custom'))

      // Force click on disabled button (should not trigger callback)
      const applyButton = screen.getByText('Apply Custom Range')
      fireEvent.click(applyButton)

      expect(mockOnRangeChange).not.toHaveBeenCalled()
    })
  })
})
