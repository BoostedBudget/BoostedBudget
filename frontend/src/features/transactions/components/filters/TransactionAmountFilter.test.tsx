import { describe, it, expect, mock } from 'bun:test'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { TransactionAmountFilter } from './TransactionAmountFilter'

describe('TransactionAmountFilter', () => {
  it('should render min/max inputs with $ prefix', () => {
    render(<TransactionAmountFilter onChange={() => {}} />)

    expect(screen.getByLabelText('Minimum amount')).toBeInTheDocument()
    expect(screen.getByLabelText('Maximum amount')).toBeInTheDocument()
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
    const onChange = mock(() => {})

    render(<TransactionAmountFilter onChange={onChange} />)

    const minInput = screen.getByLabelText('Minimum amount')
    await user.type(minInput, '25.50')
    minInput.blur()

    expect(onChange).toHaveBeenCalledWith(25.5, undefined)
  })

  it('should handle Enter key to commit value', async () => {
    const user = userEvent.setup()
    const onChange = mock(() => {})

    render(<TransactionAmountFilter onChange={onChange} />)

    const minInput = screen.getByLabelText('Minimum amount')
    await user.type(minInput, '50')
    await user.keyboard('{Enter}')

    expect(onChange).toHaveBeenCalledWith(50, undefined)
  })

  it('should strip non-numeric characters', async () => {
    const user = userEvent.setup()
    const onChange = mock(() => {})

    render(<TransactionAmountFilter onChange={onChange} />)

    const minInput = screen.getByLabelText('Minimum amount')
    await user.type(minInput, '$1,234.56')
    minInput.blur()

    expect(onChange).toHaveBeenCalledWith(1234.56, undefined)
  })

  it('should preserve max amount when min changes', async () => {
    const user = userEvent.setup()
    const onChange = mock(() => {})

    render(<TransactionAmountFilter maxAmount={100} onChange={onChange} />)

    const minInput = screen.getByLabelText('Minimum amount')
    await user.type(minInput, '10')
    minInput.blur()

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

  const SHORTCUTS = ['<$25', '$50+', '$100+', '$500+']

  it('should render all shortcut buttons', () => {
    render(<TransactionAmountFilter onChange={() => {}} />)

    SHORTCUTS.forEach((name) => {
      expect(screen.getByRole('button', { name })).toBeInTheDocument()
    })
  })

  it('should apply shortcut on click', async () => {
    const user = userEvent.setup()
    const onChange = mock(() => {})

    render(<TransactionAmountFilter onChange={onChange} />)
    await user.click(screen.getByRole('button', { name: '$50+' }))

    expect(onChange).toHaveBeenCalledWith(50, undefined)
  })

  it('should apply max-only shortcut on click', async () => {
    const user = userEvent.setup()
    const onChange = mock(() => {})

    render(<TransactionAmountFilter onChange={onChange} />)
    await user.click(screen.getByRole('button', { name: '<$25' }))

    expect(onChange).toHaveBeenCalledWith(undefined, 25)
  })

  it('should highlight active shortcut', () => {
    render(
      <TransactionAmountFilter
        minAmount={100}
        onChange={() => {}}
      />
    )

    expect(screen.getByRole('button', { name: '$100+' })).toHaveClass('bg-blue-600')
  })

  it('should clear shortcut when clicking active one', async () => {
    const user = userEvent.setup()
    const onChange = mock(() => {})

    render(
      <TransactionAmountFilter
        minAmount={50}
        onChange={onChange}
      />
    )

    await user.click(screen.getByRole('button', { name: '$50+' }))

    expect(onChange).toHaveBeenCalledWith(undefined, undefined)
  })

  it('should disable shortcuts when disabled prop is true', () => {
    render(<TransactionAmountFilter onChange={() => {}} disabled />)

    SHORTCUTS.forEach((name) => {
      expect(screen.getByRole('button', { name })).toBeDisabled()
    })
  })
})
