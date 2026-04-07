/**
 * ConfirmDeleteDialog Component Tests
 * Issue #978: Bank Account Deletion
 */

import * as React from 'react'
import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { ConfirmDeleteDialog } from './confirm-delete-dialog'

const defaultProps = {
  open: true,
  onOpenChange: mock(() => {}),
  onConfirm: mock(() => {}),
  title: 'Delete Account',
  description: 'This action cannot be undone.',
  confirmText: 'Delete',
}

describe('ConfirmDeleteDialog', () => {
  describe('Rendering', () => {
    it('renders title, description, and confirmLabel when open', () => {
      render(<ConfirmDeleteDialog {...defaultProps} />)
      expect(screen.getByText('Delete Account')).toBeInTheDocument()
      expect(screen.getByText('This action cannot be undone.')).toBeInTheDocument()
      expect(screen.getByText(`Type "Delete" to confirm`)).toBeInTheDocument()
    })

    it('renders a custom confirmLabel when provided', () => {
      render(
        <ConfirmDeleteDialog
          {...defaultProps}
          confirmLabel='Enter "Delete" below'
        />
      )
      expect(screen.getByText('Enter "Delete" below')).toBeInTheDocument()
    })

    it('renders Cancel and Delete buttons', () => {
      render(<ConfirmDeleteDialog {...defaultProps} />)
      expect(screen.getByRole('button', { name: /cancel/i })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /delete/i })).toBeInTheDocument()
    })

    it('renders a custom destructiveButtonText when provided', () => {
      render(
        <ConfirmDeleteDialog
          {...defaultProps}
          destructiveButtonText="Remove Account"
        />
      )
      expect(screen.getByRole('button', { name: /remove account/i })).toBeInTheDocument()
    })
  })

  describe('Delete button enablement', () => {
    it('Delete button is disabled initially (empty input)', () => {
      render(<ConfirmDeleteDialog {...defaultProps} />)
      const deleteBtn = screen.getByRole('button', { name: /delete/i })
      expect(deleteBtn).toBeDisabled()
    })

    it('Delete button is disabled on partial match', () => {
      render(<ConfirmDeleteDialog {...defaultProps} confirmText="Delete" />)
      const input = screen.getByRole('textbox')
      fireEvent.change(input, { target: { value: 'Del' } })
      expect(screen.getByRole('button', { name: /delete/i })).toBeDisabled()
    })

    it('Delete button is enabled on exact match (different case)', () => {
      render(<ConfirmDeleteDialog {...defaultProps} confirmText="Delete" />)
      const input = screen.getByRole('textbox')
      fireEvent.change(input, { target: { value: 'delete' } })
      expect(screen.getByRole('button', { name: /delete/i })).not.toBeDisabled()
    })

    it('Delete button is enabled on exact match (same case)', () => {
      render(<ConfirmDeleteDialog {...defaultProps} confirmText="Delete" />)
      const input = screen.getByRole('textbox')
      fireEvent.change(input, { target: { value: 'Delete' } })
      expect(screen.getByRole('button', { name: /delete/i })).not.toBeDisabled()
    })
  })

  describe('Confirm action', () => {
    it('calls onConfirm when Delete is clicked after correct input', () => {
      const onConfirm = mock(() => {})
      render(
        <ConfirmDeleteDialog
          {...defaultProps}
          onConfirm={onConfirm}
          confirmText="Delete"
        />
      )
      const input = screen.getByRole('textbox')
      fireEvent.change(input, { target: { value: 'Delete' } })
      fireEvent.click(screen.getByRole('button', { name: /delete/i }))
      expect(onConfirm).toHaveBeenCalledTimes(1)
    })
  })

  describe('Loading state', () => {
    it('disables both Cancel and Delete buttons when loading=true', () => {
      render(<ConfirmDeleteDialog {...defaultProps} loading={true} />)
      expect(screen.getByRole('button', { name: /cancel/i })).toBeDisabled()
      expect(screen.getByRole('button', { name: /delete/i })).toBeDisabled()
    })
  })

  describe('Escape key', () => {
    it('calls onOpenChange with false when Escape is pressed', () => {
      const onOpenChange = mock(() => {})
      render(<ConfirmDeleteDialog {...defaultProps} onOpenChange={onOpenChange} />)
      fireEvent.keyDown(document.body, { key: 'Escape', code: 'Escape' })
      expect(onOpenChange).toHaveBeenCalledWith(false)
    })
  })

  describe('Input clearing on close', () => {
    it('clears the input when dialog is closed and reopened', () => {
      function Wrapper() {
        const [open, setOpen] = React.useState(true)
        return (
          <>
            <button onClick={() => setOpen(false)}>close</button>
            <button onClick={() => setOpen(true)}>reopen</button>
            <ConfirmDeleteDialog
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

      // Type something into the input
      const input = screen.getByRole('textbox')
      fireEvent.change(input, { target: { value: 'Delete' } })
      expect(input).toHaveValue('Delete')

      // Close the dialog
      fireEvent.click(screen.getByText('close'))

      // Reopen the dialog
      fireEvent.click(screen.getByText('reopen'))

      // Input should be cleared
      const reopenedInput = screen.getByRole('textbox')
      expect(reopenedInput).toHaveValue('')
    })
  })
})
