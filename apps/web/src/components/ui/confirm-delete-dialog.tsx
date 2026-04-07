/**
 * ConfirmDeleteDialog
 * Issue #978: Bank Account Deletion
 *
 * Reusable type-to-confirm deletion dialog built on Radix Dialog primitives.
 */
import * as React from 'react'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'

interface ConfirmDeleteDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onConfirm: () => void
  title: string
  description: React.ReactNode
  confirmText: string
  confirmLabel?: string
  loading?: boolean
  destructiveButtonText?: string
}

export function ConfirmDeleteDialog({
  open,
  onOpenChange,
  onConfirm,
  title,
  description,
  confirmText,
  confirmLabel,
  loading = false,
  destructiveButtonText = 'Delete',
}: ConfirmDeleteDialogProps) {
  const [typed, setTyped] = React.useState('')
  const id = React.useId()

  // Clear typed input when dialog closes
  React.useEffect(() => {
    if (!open) {
      setTyped('')
    }
  }, [open])

  const isConfirmed = typed.toLocaleLowerCase() === confirmText.toLocaleLowerCase()
  const label = confirmLabel ?? `Type "${confirmText}" to confirm`

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>

        <div className="mt-2">
          <label htmlFor={id} className="text-sm font-medium">
            {label}
          </label>
          <input
            id={id}
            type="text"
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            disabled={loading}
            aria-label="Confirmation input"
            aria-required="true"
            autoComplete="off"
            className="border-input bg-background ring-offset-background placeholder:text-muted-foreground focus-visible:ring-ring mt-2 flex h-9 w-full rounded-md border px-3 py-2 text-sm focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50"
          />
        </div>

        <DialogFooter>
          <Button
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={loading}
          >
            Cancel
          </Button>
          <Button
            variant="destructive"
            onClick={onConfirm}
            disabled={!isConfirmed || loading}
          >
            {destructiveButtonText}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
