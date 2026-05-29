/**
 * IconPicker tests (Issue #1203).
 */
import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { IconPicker } from './IconPicker'

describe('IconPicker', () => {
  it('opens the grid and selecting an icon calls onChange with the key', async () => {
    const onChange = mock(() => {})
    render(<IconPicker value={null} onChange={onChange} />)

    fireEvent.click(screen.getByRole('button', { name: /choose an icon/i }))

    // The grid renders icon buttons labeled by their key.
    const utensils = await screen.findByRole('gridcell', { name: 'utensils' })
    fireEvent.click(utensils)

    expect(onChange).toHaveBeenCalledTimes(1)
    expect(onChange).toHaveBeenCalledWith('utensils')
  })

  it('marks the current value as pressed', async () => {
    render(<IconPicker value="coffee" onChange={() => {}} />)
    fireEvent.click(screen.getByRole('button', { name: /choose an icon/i }))

    const coffee = await screen.findByRole('gridcell', { name: 'coffee' })
    expect(coffee.getAttribute('aria-pressed')).toBe('true')
  })

  it('filters icons by search query', async () => {
    render(<IconPicker value={null} onChange={() => {}} />)
    fireEvent.click(screen.getByRole('button', { name: /choose an icon/i }))

    const search = await screen.findByLabelText('Search icons')
    fireEvent.change(search, { target: { value: 'coffee' } })

    expect(screen.queryByRole('gridcell', { name: 'coffee' })).toBeTruthy()
    expect(screen.queryByRole('gridcell', { name: 'utensils' })).toBeNull()
  })
})
