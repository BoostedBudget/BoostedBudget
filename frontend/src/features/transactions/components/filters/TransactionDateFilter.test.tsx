/**
 * TransactionDateFilter Component Tests
 * Issue #529: Transaction page filtering
 */

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { TransactionDateFilter } from './TransactionDateFilter'

const mockOnPresetChange = mock(() => {})
const mockOnCustomRangeChange = mock(() => {})

describe('TransactionDateFilter', () => {
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

    expect(screen.getByRole('button', { name: '7 days' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '30 days' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '90 days' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '1 year' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'All time' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Custom' })).toBeInTheDocument()
  })

  it('should highlight selected preset', () => {
    render(<TransactionDateFilter {...defaultProps} preset="30d" />)

    const selected = screen.getByRole('button', { name: '30 days' })
    expect(selected).toHaveAttribute('aria-pressed', 'true')
  })

  it('should call onPresetChange when preset clicked', async () => {
    const user = userEvent.setup()
    const onPresetChange = mock(() => {})

    render(<TransactionDateFilter {...defaultProps} onPresetChange={onPresetChange} />)

    await user.click(screen.getByRole('button', { name: '7 days' }))

    expect(onPresetChange).toHaveBeenCalledWith('7d')
  })

  it('should show custom date inputs when Custom is selected', async () => {
    const user = userEvent.setup()

    render(<TransactionDateFilter {...defaultProps} />)

    // Initially custom inputs should not be visible
    expect(screen.queryByText('Start date')).not.toBeInTheDocument()

    // Click Custom button
    await user.click(screen.getByRole('button', { name: 'Custom' }))

    // Custom inputs should be visible
    expect(screen.getByText('Start date')).toBeInTheDocument()
    expect(screen.getByText('End date')).toBeInTheDocument()
  })

  it('should show custom inputs when preset is custom', () => {
    render(<TransactionDateFilter {...defaultProps} preset="custom" />)

    expect(screen.getByText('Start date')).toBeInTheDocument()
    expect(screen.getByText('End date')).toBeInTheDocument()
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

    // Find inputs using the label text parent container
    const startInput = screen.getByText('Start date').parentElement?.querySelector('input')
    const endInput = screen.getByText('End date').parentElement?.querySelector('input')

    if (startInput) fireEvent.change(startInput, { target: { value: '2025-01-01' } })
    if (endInput) fireEvent.change(endInput, { target: { value: '2025-06-30' } })

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

    // Find inputs using the label text parent container
    const startInput = screen.getByText('Start date').parentElement?.querySelector('input') as HTMLInputElement
    const endInput = screen.getByText('End date').parentElement?.querySelector('input') as HTMLInputElement

    expect(startInput?.value).toBe('2025-01-01')
    expect(endInput?.value).toBe('2025-06-30')
  })

  it('should disable all buttons when disabled prop is true', () => {
    render(<TransactionDateFilter {...defaultProps} disabled />)

    const buttons = screen.getAllByRole('button')
    buttons.forEach((button) => {
      expect(button).toBeDisabled()
    })
  })

  it('should render label', () => {
    render(<TransactionDateFilter {...defaultProps} />)

    expect(screen.getByText('Date Range')).toBeInTheDocument()
  })
})
