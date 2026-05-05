import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { DateField } from './DateField'

describe('DateField', () => {
  it('renders label with correct htmlFor', () => {
    render(<DateField name="deadline" label="Deadline" value="" onChange={() => {}} />)
    const input = screen.getByLabelText('Deadline')
    expect(input).toBeDefined()
    expect(input.getAttribute('id')).toBe('deadline')
  })

  it('renders input with type date', () => {
    render(<DateField name="deadline" label="Deadline" value="2025-12-31" onChange={() => {}} />)
    const input = screen.getByLabelText('Deadline') as HTMLInputElement
    expect(input.getAttribute('type')).toBe('date')
    expect(input.getAttribute('name')).toBe('deadline')
  })

  it('passes min attribute when provided', () => {
    render(<DateField name="deadline" label="Deadline" value="" onChange={() => {}} min="2025-01-01" />)
    const input = screen.getByLabelText('Deadline')
    expect(input.getAttribute('min')).toBe('2025-01-01')
  })

  it('does not render min attribute when not provided', () => {
    render(<DateField name="deadline" label="Deadline" value="" onChange={() => {}} />)
    const input = screen.getByLabelText('Deadline')
    expect(input.getAttribute('min')).toBeNull()
  })
})
