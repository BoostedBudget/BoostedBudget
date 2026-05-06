import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { TextField } from './TextField'

describe('TextField', () => {
  it('renders label with correct htmlFor', () => {
    render(<TextField name="myField" label="My Label" value="" onChange={() => {}} />)
    const input = screen.getByLabelText('My Label')
    expect(input).toBeDefined()
    expect(input.getAttribute('id')).toBe('myField')
  })

  it('renders input with correct name and type', () => {
    render(<TextField name="myField" label="My Label" value="hello" onChange={() => {}} />)
    const input = screen.getByLabelText('My Label') as HTMLInputElement
    expect(input.getAttribute('name')).toBe('myField')
    expect(input.getAttribute('type')).toBe('text')
    expect(input.value).toBe('hello')
  })

  it('shows error text when error prop provided', () => {
    render(<TextField name="myField" label="My Label" value="" onChange={() => {}} error="Required field" />)
    expect(screen.getByText('Required field')).toBeDefined()
  })

  it('does not show error element when no error prop', () => {
    render(<TextField name="myField" label="My Label" value="" onChange={() => {}} />)
    expect(screen.queryByText('Required field')).toBeNull()
  })

  it('renders placeholder when provided', () => {
    render(<TextField name="myField" label="My Label" value="" onChange={() => {}} placeholder="Enter text" />)
    const input = screen.getByPlaceholderText('Enter text')
    expect(input).toBeDefined()
  })
})
