/**
 * SavingsGoalSelect Tests
 */

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

const mockUseGetSavingsGoals = mock(() => ({
  data: undefined,
  isLoading: false,
  isError: false,
}))

mock.module('@boostedbudget/api-client/endpoints/savings-goals/savings-goals', () => ({
  useGetSavingsGoals: mockUseGetSavingsGoals,
}))

import { SavingsGoalSelect } from './SavingsGoalSelect'

describe('SavingsGoalSelect', () => {
  const mockOnChange = mock(() => {})

  const mockGoals = [
    { id: 'goal-1', name: 'House Down Payment', target_amount: 50000, current_amount: 12000 },
    { id: 'goal-2', name: 'Emergency Fund', target_amount: 10000, current_amount: 5000 },
  ]

  beforeEach(() => {
    mockOnChange.mockClear()
    mockUseGetSavingsGoals.mockReturnValue({ data: mockGoals, isLoading: false, isError: false })
  })

  it('renders a select with placeholder option', () => {
    render(<SavingsGoalSelect value="" onChange={mockOnChange} />)
    expect(screen.getByRole('combobox')).toBeTruthy()
    expect(screen.getByText(/select a savings goal/i)).toBeTruthy()
  })

  it('renders available goals as options', () => {
    render(<SavingsGoalSelect value="" onChange={mockOnChange} />)
    expect(screen.getByText('House Down Payment')).toBeTruthy()
    expect(screen.getByText('Emergency Fund')).toBeTruthy()
  })

  it('calls onChange with the selected goal id', async () => {
    const user = userEvent.setup()
    render(<SavingsGoalSelect value="" onChange={mockOnChange} />)
    const select = screen.getByRole('combobox')
    await user.selectOptions(select, 'goal-1')
    expect(mockOnChange).toHaveBeenCalledWith('goal-1')
  })

  it('shows loading state when goals are loading', () => {
    mockUseGetSavingsGoals.mockReturnValue({ data: undefined, isLoading: true })
    render(<SavingsGoalSelect value="" onChange={mockOnChange} />)
    const select = screen.getByRole('combobox')
    expect(select.hasAttribute('disabled')).toBe(true)
  })

  it('respects external disabled prop', () => {
    render(<SavingsGoalSelect value="" onChange={mockOnChange} disabled />)
    const select = screen.getByRole('combobox')
    expect(select.hasAttribute('disabled')).toBe(true)
  })

  it('reflects controlled value', () => {
    render(<SavingsGoalSelect value="goal-2" onChange={mockOnChange} />)
    const select = screen.getByRole('combobox') as HTMLSelectElement
    expect(select.value).toBe('goal-2')
  })

  it('shows error message when goals fetch fails', () => {
    mockUseGetSavingsGoals.mockReturnValue({ data: undefined, isLoading: false, isError: true })
    render(<SavingsGoalSelect value="" onChange={mockOnChange} />)
    expect(screen.getByText(/couldn.*load savings goals/i)).toBeTruthy()
    // select is not rendered in error state
    expect(screen.queryByRole('combobox')).toBeFalsy()
  })
})
