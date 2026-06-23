// Issue #1323: Centralized ConfirmationDialog mock to prevent mock.module() pollution.
// This file is preloaded via bunfig.toml before all tests.
//
// The stub renders just enough for component tests (open/close, title, description,
// confirm/cancel buttons). ConfirmationDialog.test.tsx tests the REAL implementation
// and must run separately via test-conflict-files.sh.

import React from 'react'
import { mock } from 'bun:test'

export const mockOnConfirm = mock(() => {})
export const mockOnOpenChange = mock(() => {})

// Minimal stub — component tests only need to verify open state, titles, and button clicks.
// Not registered globally because ConfirmationDialog.test.tsx needs the real implementation.
// Each test file that needs this stub must call mock.module('@/components/ui/confirmation-dialog', ...)
// with the STUB_CONFIRMATION_DIALOG — never create new stubs inline.
export const STUB_CONFIRMATION_DIALOG = {
  ConfirmationDialog: ({
    open,
    title,
    description,
    confirmText,
    confirmButtonText,
    cancelButtonText = 'Cancel',
    variant,
    onConfirm,
    onOpenChange,
  }: {
    open: boolean
    title: string
    description?: React.ReactNode
    confirmText?: string
    confirmButtonText?: string
    cancelButtonText?: string
    variant?: string
    onConfirm?: () => void
    onOpenChange?: (open: boolean) => void
    [key: string]: unknown
  }) =>
    open ? (
      <div
        role="alertdialog"
        data-testid="confirm-delete-dialog"
        data-title={title}
        data-confirm-text={confirmText}
      >
        <div data-testid="dialog-title">{title}</div>
        {description && <div data-testid="dialog-description">{description}</div>}
        <button onClick={onConfirm}>
          {confirmButtonText ?? (variant === 'destructive' ? 'Delete' : 'Confirm')}
        </button>
        <button onClick={() => onOpenChange?.(false)}>{cancelButtonText}</button>
      </div>
    ) : null,
}

// NOTE: No global mock.module() here. ConfirmationDialog.test.tsx needs the real implementation.
// Each consuming test file MUST call:
//   import { STUB_CONFIRMATION_DIALOG } from '@/test/mocks/confirmation-dialog'
//   mock.module('@/components/ui/confirmation-dialog', () => STUB_CONFIRMATION_DIALOG)
