import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { NumberField } from './NumberField'

describe('NumberField', () => {
  it('renders label with correct htmlFor', () => {
    render(<NumberField name="amount" label="Amount" value="" onChange={() => {}} />)
    const input = screen.getByLabelText('Amount')
    expect(input).toBeDefined()
    expect(input.getAttribute('id')).toBe('amount')
  })

  it('renders input with type number', () => {
    render(<NumberField name="amount" label="Amount" value="100" onChange={() => {}} />)
    const input = screen.getByLabelText('Amount') as HTMLInputElement
    expect(input.getAttribute('type')).toBe('number')
    expect(input.value).toBe('100')
    expect(input.getAttribute('name')).toBe('amount')
  })

  it('shows error text when error prop provided', () => {
    render(<NumberField name="amount" label="Amount" value="" onChange={() => {}} error="Must be positive" />)
    expect(screen.getByText('Must be positive')).toBeDefined()
  })

  it('does not show error element when no error prop', () => {
    render(<NumberField name="amount" label="Amount" value="" onChange={() => {}} />)
    expect(screen.queryByText('Must be positive')).toBeNull()
  })

  it('renders prefix character when prefix prop provided', () => {
    render(<NumberField name="amount" label="Amount" value="" onChange={() => {}} prefix="$" />)
    expect(screen.getByText('$')).toBeDefined()
  })

  it('adds pl-7 class to input when prefix is present', () => {
    render(<NumberField name="amount" label="Amount" value="" onChange={() => {}} prefix="$" />)
    const input = screen.getByLabelText('Amount')
    expect(input.className).toContain('pl-7')
  })

  it('does not add pl-7 class when no prefix', () => {
    render(<NumberField name="amount" label="Amount" value="" onChange={() => {}} />)
    const input = screen.getByLabelText('Amount')
    expect(input.className).not.toContain('pl-7')
  })

  it('passes min and step attributes', () => {
    render(<NumberField name="amount" label="Amount" value="" onChange={() => {}} min="0" step="0.01" />)
    const input = screen.getByLabelText('Amount')
    expect(input.getAttribute('min')).toBe('0')
    expect(input.getAttribute('step')).toBe('0.01')
  })
})
