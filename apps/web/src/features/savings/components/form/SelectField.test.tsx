import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { SelectField } from './SelectField'

describe('SelectField', () => {
  it('renders label with correct htmlFor', () => {
    render(
      <SelectField name="category" label="Category" value="" onChange={() => {}}>
        <option value="a">Option A</option>
      </SelectField>
    )
    const select = screen.getByLabelText('Category')
    expect(select).toBeDefined()
    expect(select.getAttribute('id')).toBe('category')
  })

  it('renders select with correct name and value', () => {
    render(
      <SelectField name="category" label="Category" value="a" onChange={() => {}}>
        <option value="a">Option A</option>
        <option value="b">Option B</option>
      </SelectField>
    )
    const select = screen.getByLabelText('Category') as HTMLSelectElement
    expect(select.getAttribute('name')).toBe('category')
    expect(select.value).toBe('a')
  })

  it('renders children as options', () => {
    render(
      <SelectField name="category" label="Category" value="" onChange={() => {}}>
        <option value="a">Option A</option>
        <option value="b">Option B</option>
      </SelectField>
    )
    expect(screen.getByText('Option A')).toBeDefined()
    expect(screen.getByText('Option B')).toBeDefined()
  })

  it('shows error text when error prop provided', () => {
    render(
      <SelectField name="category" label="Category" value="" onChange={() => {}} error="Please select">
        <option value="">--</option>
      </SelectField>
    )
    expect(screen.getByText('Please select')).toBeDefined()
  })

  it('does not show error element when no error prop', () => {
    render(
      <SelectField name="category" label="Category" value="" onChange={() => {}}>
        <option value="">--</option>
      </SelectField>
    )
    expect(screen.queryByText('Please select')).toBeNull()
  })

  it('is disabled when disabled prop is true', () => {
    render(
      <SelectField name="category" label="Category" value="" onChange={() => {}} disabled>
        <option value="">--</option>
      </SelectField>
    )
    const select = screen.getByLabelText('Category') as HTMLSelectElement
    expect(select.disabled).toBe(true)
  })
})
