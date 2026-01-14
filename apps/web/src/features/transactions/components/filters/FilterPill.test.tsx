import { describe, it, expect, mock } from 'bun:test'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { FilterPill } from './FilterPill'

describe('FilterPill', () => {
  it('should render label and value', () => {
    render(<FilterPill label="Category" value="Food" onRemove={mock(() => {})} />)

    expect(screen.getByText('Category:')).toBeInTheDocument()
    expect(screen.getByText('Food')).toBeInTheDocument()
  })

  it('should call onRemove when X button is clicked', async () => {
    const user = userEvent.setup()
    const onRemove = mock(() => {})

    render(<FilterPill label="Category" value="Food" onRemove={onRemove} />)

    const removeButton = screen.getByRole('button', { name: /remove category filter/i })
    await user.click(removeButton)

    expect(onRemove).toHaveBeenCalledTimes(1)
  })

  it('should truncate long values', () => {
    const longValue = 'This is a very long category name that should be truncated'
    render(<FilterPill label="Category" value={longValue} onRemove={mock(() => {})} />)

    const valueElement = screen.getByText(longValue)
    expect(valueElement).toHaveClass('truncate')
  })
})
