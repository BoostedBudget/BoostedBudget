/**
 * TransactionAmountFilter Component Tests
 * Issue #529: Transaction page filtering
 */

import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { TransactionAmountFilter } from './TransactionAmountFilter'

describe('TransactionAmountFilter', () => {
  it('should render min/max inputs with $ prefix', () => {
    render(<TransactionAmountFilter onChange={() => {}} />)

    expect(screen.getByLabelText('Minimum amount')).toBeInTheDocument()
    expect(screen.getByLabelText('Maximum amount')).toBeInTheDocument()
    // Check for $ symbols
    expect(screen.getAllByText('$')).toHaveLength(2)
  })

  it('should display initial values', () => {
    render(
      <TransactionAmountFilter
        minAmount={10.5}
        maxAmount={100}
        onChange={() => {}}
      />
    )

    const minInput = screen.getByLabelText('Minimum amount') as HTMLInputElement
    const maxInput = screen.getByLabelText('Maximum amount') as HTMLInputElement

    expect(minInput.value).toBe('10.50')
    expect(maxInput.value).toBe('100.00')
  })

  it('should call onChange on blur with parsed values', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()

    render(<TransactionAmountFilter onChange={onChange} />)

    const minInput = screen.getByLabelText('Minimum amount')
    await user.type(minInput, '25.50')
    fireEvent.blur(minInput)

    expect(onChange).toHaveBeenCalledWith(25.5, undefined)
  })

  it('should handle Enter key to commit value', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()

    render(<TransactionAmountFilter onChange={onChange} />)

    const minInput = screen.getByLabelText('Minimum amount')
    await user.type(minInput, '50')
    await user.keyboard('{Enter}')

    expect(onChange).toHaveBeenCalledWith(50, undefined)
  })

  it('should strip non-numeric characters', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()

    render(<TransactionAmountFilter onChange={onChange} />)

    const minInput = screen.getByLabelText('Minimum amount')
    await user.type(minInput, '$1,234.56')
    fireEvent.blur(minInput)

    expect(onChange).toHaveBeenCalledWith(1234.56, undefined)
  })

  it('should preserve max amount when min changes', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()

    render(<TransactionAmountFilter maxAmount={100} onChange={onChange} />)

    const minInput = screen.getByLabelText('Minimum amount')
    await user.type(minInput, '10')
    fireEvent.blur(minInput)

    expect(onChange).toHaveBeenCalledWith(10, 100)
  })

  it('should disable inputs when disabled prop is true', () => {
    render(<TransactionAmountFilter onChange={() => {}} disabled />)

    expect(screen.getByLabelText('Minimum amount')).toBeDisabled()
    expect(screen.getByLabelText('Maximum amount')).toBeDisabled()
  })

  it('should render label', () => {
    render(<TransactionAmountFilter onChange={() => {}} />)

    expect(screen.getByText('Amount Range')).toBeInTheDocument()
  })

  describe('amount shortcuts', () => {
    it('should render all shortcut buttons', () => {
      render(<TransactionAmountFilter onChange={() => {}} />)

      expect(screen.getByRole('button', { name: '<$25' })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: '$50+' })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: '$100+' })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: '$500+' })).toBeInTheDocument()
    })

    it('should apply shortcut on click', async () => {
      const user = userEvent.setup()
      const onChange = vi.fn()

      render(<TransactionAmountFilter onChange={onChange} />)

      await user.click(screen.getByRole('button', { name: '$50+' }))

      expect(onChange).toHaveBeenCalledWith(50, undefined)
    })

    it('should apply max-only shortcut on click', async () => {
      const user = userEvent.setup()
      const onChange = vi.fn()

      render(<TransactionAmountFilter onChange={onChange} />)

      await user.click(screen.getByRole('button', { name: '<$25' }))

      expect(onChange).toHaveBeenCalledWith(undefined, 25)
    })

    it('should highlight active shortcut', () => {
      render(
        <TransactionAmountFilter
          minAmount={100}
          maxAmount={undefined}
          onChange={() => {}}
        />
      )

      const activeButton = screen.getByRole('button', { name: '$100+' })
      expect(activeButton).toHaveClass('bg-blue-600')
    })

    it('should clear shortcut when clicking active one', async () => {
      const user = userEvent.setup()
      const onChange = vi.fn()

      render(
        <TransactionAmountFilter
          minAmount={50}
          maxAmount={undefined}
          onChange={onChange}
        />
      )

      await user.click(screen.getByRole('button', { name: '$50+' }))

      expect(onChange).toHaveBeenCalledWith(undefined, undefined)
    })

    it('should disable shortcuts when disabled prop is true', () => {
      render(<TransactionAmountFilter onChange={() => {}} disabled />)

      expect(screen.getByRole('button', { name: '<$25' })).toBeDisabled()
      expect(screen.getByRole('button', { name: '$50+' })).toBeDisabled()
      expect(screen.getByRole('button', { name: '$100+' })).toBeDisabled()
      expect(screen.getByRole('button', { name: '$500+' })).toBeDisabled()
    })
  })
})
