/**
 * ConfirmationDialog Component Tests
 * Issue #960: Confirmation dialogs for destructive actions
 */

import * as React from 'react'
import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { ConfirmationDialog } from './confirmation-dialog'

const onOpenChange = mock(() => {})
const onConfirm = mock(() => {})

const baseProps = {
  open: true,
  onOpenChange,
  onConfirm,
  title: 'Delete Item',
  description: 'This action cannot be undone.',
}

beforeEach(() => {
  onOpenChange.mockReset()
  onConfirm.mockReset()
})

describe('ConfirmationDialog', () => {
  describe('Rendering', () => {
    it('renders title and description when open', () => {
      render(<ConfirmationDialog {...baseProps} />)
      expect(screen.getByText('Delete Item')).toBeInTheDocument()
      expect(screen.getByText('This action cannot be undone.')).toBeInTheDocument()
    })

    it('has role="alertdialog"', () => {
      render(<ConfirmationDialog {...baseProps} />)
      expect(screen.getByRole('alertdialog')).toBeInTheDocument()
    })

    it('renders Cancel and Delete buttons by default (destructive variant)', () => {
      render(<ConfirmationDialog {...baseProps} />)
      expect(screen.getByRole('button', { name: /cancel/i })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /delete/i })).toBeInTheDocument()
    })

    it('renders "Confirm" as default button text for warning variant', () => {
      render(<ConfirmationDialog {...baseProps} variant="warning" />)
      expect(screen.getByRole('button', { name: /confirm/i })).toBeInTheDocument()
    })

    it('renders "Confirm" as default button text for info variant', () => {
      render(<ConfirmationDialog {...baseProps} variant="info" />)
      expect(screen.getByRole('button', { name: /confirm/i })).toBeInTheDocument()
    })

    it('renders custom confirmButtonText', () => {
      render(<ConfirmationDialog {...baseProps} confirmButtonText="Remove Widget" />)
      expect(screen.getByRole('button', { name: /remove widget/i })).toBeInTheDocument()
    })

    it('renders custom cancelButtonText', () => {
      render(<ConfirmationDialog {...baseProps} cancelButtonText="Keep It" />)
      expect(screen.getByRole('button', { name: /keep it/i })).toBeInTheDocument()
    })

    it('renders description as ReactNode', () => {
      render(
        <ConfirmationDialog
          {...baseProps}
          description={<span data-testid="rich-desc">Rich content</span>}
        />
      )
      expect(screen.getByTestId('rich-desc')).toBeInTheDocument()
    })

    it('does not render when open=false', () => {
      render(<ConfirmationDialog {...baseProps} open={false} />)
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    })
  })

  describe('Simple mode (no confirmText)', () => {
    it('confirm button is enabled immediately', () => {
      render(<ConfirmationDialog {...baseProps} />)
      expect(screen.getByRole('button', { name: /delete/i })).not.toBeDisabled()
    })

    it('calls onConfirm when confirm button is clicked', () => {
      render(<ConfirmationDialog {...baseProps} />)
      fireEvent.click(screen.getByRole('button', { name: /delete/i }))
      expect(onConfirm).toHaveBeenCalledTimes(1)
    })

    it('calls onOpenChange(false) when Cancel is clicked', () => {
      render(<ConfirmationDialog {...baseProps} />)
      fireEvent.click(screen.getByRole('button', { name: /cancel/i }))
      expect(onOpenChange).toHaveBeenCalledWith(false)
    })

    it('does NOT confirm on Enter keydown to prevent reflexive confirms', () => {
      render(<ConfirmationDialog {...baseProps} />)
      fireEvent.keyDown(screen.getByRole('alertdialog'), { key: 'Enter', code: 'Enter' })
      expect(onConfirm).not.toHaveBeenCalled()
    })

    it('does not render a text input', () => {
      render(<ConfirmationDialog {...baseProps} />)
      expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
    })
  })

  describe('Type-to-confirm mode (confirmText provided)', () => {
    const typeProps = { ...baseProps, confirmText: 'Delete' }

    it('renders the type-to-confirm input and label', () => {
      render(<ConfirmationDialog {...typeProps} />)
      expect(screen.getByText('Type "Delete" to confirm')).toBeInTheDocument()
      expect(screen.getByRole('textbox')).toBeInTheDocument()
    })

    it('renders a custom confirmLabel', () => {
      render(<ConfirmationDialog {...typeProps} confirmLabel='Enter "Delete" below' />)
      expect(screen.getByText('Enter "Delete" below')).toBeInTheDocument()
    })

    it('confirm button is disabled when input is empty', () => {
      render(<ConfirmationDialog {...typeProps} />)
      expect(screen.getByRole('button', { name: /delete/i })).toBeDisabled()
    })

    it('confirm button is disabled on partial match', () => {
      render(<ConfirmationDialog {...typeProps} />)
      fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Del' } })
      expect(screen.getByRole('button', { name: /delete/i })).toBeDisabled()
    })

    it('confirm button is enabled on exact case-sensitive match', () => {
      render(<ConfirmationDialog {...typeProps} />)
      fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Delete' } })
      expect(screen.getByRole('button', { name: /delete/i })).not.toBeDisabled()
    })

    it('confirm button is enabled on case-insensitive match', () => {
      render(<ConfirmationDialog {...typeProps} />)
      fireEvent.change(screen.getByRole('textbox'), { target: { value: 'delete' } })
      expect(screen.getByRole('button', { name: /delete/i })).not.toBeDisabled()
    })

    it('calls onConfirm when button is clicked after matching', () => {
      render(<ConfirmationDialog {...typeProps} />)
      fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Delete' } })
      fireEvent.click(screen.getByRole('button', { name: /delete/i }))
      expect(onConfirm).toHaveBeenCalledTimes(1)
    })

    it('calls onConfirm on Enter in input when text matches', () => {
      render(<ConfirmationDialog {...typeProps} />)
      const input = screen.getByRole('textbox')
      fireEvent.change(input, { target: { value: 'Delete' } })
      fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' })
      expect(onConfirm).toHaveBeenCalledTimes(1)
    })

    it('does NOT call onConfirm on Enter when text does not match', () => {
      render(<ConfirmationDialog {...typeProps} />)
      const input = screen.getByRole('textbox')
      fireEvent.change(input, { target: { value: 'Del' } })
      fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' })
      expect(onConfirm).not.toHaveBeenCalled()
    })
  })

  describe('Loading state', () => {
    it('disables both buttons when loading=true', () => {
      render(<ConfirmationDialog {...baseProps} loading={true} />)
      expect(screen.getByRole('button', { name: /cancel/i })).toBeDisabled()
      expect(screen.getByRole('button', { name: /delete/i })).toBeDisabled()
    })

    it('disables the type-to-confirm input when loading=true', () => {
      render(<ConfirmationDialog {...baseProps} confirmText="Delete" loading={true} />)
      expect(screen.getByRole('textbox')).toBeDisabled()
    })

    it('shows a spinner when loading=true', () => {
      render(<ConfirmationDialog {...baseProps} loading={true} />)
      expect(document.querySelector('.animate-spin')).toBeInTheDocument()
    })

    it('does NOT call onConfirm on Enter when loading', () => {
      render(<ConfirmationDialog {...baseProps} confirmText="Delete" loading={true} />)
      const input = screen.getByRole('textbox')
      fireEvent.change(input, { target: { value: 'Delete' } })
      fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' })
      expect(onConfirm).not.toHaveBeenCalled()
    })
  })

  describe('Keyboard — Escape', () => {
    it('calls onOpenChange(false) when Escape is pressed (not loading)', () => {
      render(<ConfirmationDialog {...baseProps} />)
      fireEvent.keyDown(document.body, { key: 'Escape', code: 'Escape' })
      expect(onOpenChange).toHaveBeenCalledWith(false)
    })
  })

  describe('Input state reset', () => {
    it('clears the type-to-confirm input when dialog closes and reopens', () => {
      function Wrapper() {
        const [open, setOpen] = React.useState(true)
        return (
          <>
            <button onClick={() => setOpen(false)}>close</button>
            <button onClick={() => setOpen(true)}>reopen</button>
            <ConfirmationDialog
              open={open}
              onOpenChange={setOpen}
              onConfirm={() => {}}
              title="Test"
              description="desc"
              confirmText="Delete"
            />
          </>
        )
      }

      render(<Wrapper />)

      const input = screen.getByRole('textbox')
      fireEvent.change(input, { target: { value: 'Delete' } })
      expect(input).toHaveValue('Delete')

      fireEvent.click(screen.getByText('close'))
      fireEvent.click(screen.getByText('reopen'))

      expect(screen.getByRole('textbox')).toHaveValue('')
    })
  })
})
