/**
 * CategoryIcon tests (Issue #1203).
 */
import { describe, it, expect } from 'bun:test'
import { render } from '@testing-library/react'
import { CategoryIcon } from './CategoryIcon'

describe('CategoryIcon', () => {
  it('renders an icon for a known key', () => {
    const { getByTestId } = render(<CategoryIcon name="utensils" />)
    expect(getByTestId('category-icon')).toBeTruthy()
  })

  it('falls back to an icon for an unknown key', () => {
    const { getByTestId } = render(<CategoryIcon name="not-a-real-icon" />)
    expect(getByTestId('category-icon')).toBeTruthy()
  })

  it('falls back to an icon for a null/empty key', () => {
    const { getByTestId } = render(<CategoryIcon name={null} />)
    expect(getByTestId('category-icon')).toBeTruthy()
  })

  it('applies the provided className', () => {
    const { getByTestId } = render(<CategoryIcon name="coffee" className="h-4 w-4 text-blue-600" />)
    expect(getByTestId('category-icon').getAttribute('class')).toContain('text-blue-600')
  })
})
