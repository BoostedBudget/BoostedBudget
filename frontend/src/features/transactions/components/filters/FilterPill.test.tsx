/**
 * FilterPill Component Tests
 * Issue #529: Transaction page filtering
 */

import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { FilterPill } from './FilterPill'

describe('FilterPill', () => {
  it('should render label and value', () => {
    render(<FilterPill label="Category" value="Food" onRemove={() => {}} />)

    expect(screen.getByText('Category:')).toBeInTheDocument()
    expect(screen.getByText('Food')).toBeInTheDocument()
  })

  it('should call onRemove when X button is clicked', async () => {
    const user = userEvent.setup()
    const onRemove = vi.fn()

    render(<FilterPill label="Category" value="Food" onRemove={onRemove} />)

    const removeButton = screen.getByRole('button', { name: /remove category filter/i })
    await user.click(removeButton)

    expect(onRemove).toHaveBeenCalledTimes(1)
  })

  it('should truncate long values', () => {
    const longValue = 'This is a very long category name that should be truncated'
    render(<FilterPill label="Category" value={longValue} onRemove={() => {}} />)

    const valueElement = screen.getByText(longValue)
    expect(valueElement).toHaveClass('truncate')
  })

  it('should have accessible remove button', () => {
    render(<FilterPill label="Date" value="Last 7 days" onRemove={() => {}} />)

    const removeButton = screen.getByRole('button', { name: /remove date filter/i })
    expect(removeButton).toBeInTheDocument()
  })
})
