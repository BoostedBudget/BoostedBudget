import * as React from 'react'
import { cva } from 'class-variance-authority'
import { AlertTriangle, Info, Loader2 } from 'lucide-react'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

const iconContainerVariants = cva(
  'flex items-center justify-center rounded-full size-10 shrink-0',
  {
    variants: {
      variant: {
        destructive: 'bg-destructive/10 text-destructive',
        warning: 'bg-warning/10 text-warning',
        info: 'bg-info/10 text-info',
      },
    },
    defaultVariants: { variant: 'destructive' },
  }
)

const VARIANT_ICONS = {
  destructive: AlertTriangle,
  warning: AlertTriangle,
  info: Info,
} as const

const VARIANT_BUTTON_STYLE = {
  destructive: 'destructive',
  warning: 'destructive',
  info: 'default',
} as const

const DEFAULT_CONFIRM_TEXT = {
  destructive: 'Delete',
  warning: 'Confirm',
  info: 'Confirm',
} as const

export interface ConfirmationDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onConfirm: () => void
  title: string
  description: React.ReactNode
  variant?: 'destructive' | 'warning' | 'info'
  confirmButtonText?: string
  cancelButtonText?: string
  /** When provided, enables type-to-confirm mode. User must type this text (case-insensitive) to enable confirm button. */
  confirmText?: string
  /** Label for the type-to-confirm input. Defaults to `Type "{confirmText}" to confirm`. */
  confirmLabel?: string
  loading?: boolean
}

export function ConfirmationDialog({
  open,
  onOpenChange,
  onConfirm,
  title,
  description,
  variant = 'destructive',
  confirmButtonText,
  cancelButtonText = 'Cancel',
  confirmText,
  confirmLabel,
  loading = false,
}: ConfirmationDialogProps) {
  const [typed, setTyped] = React.useState('')
  const inputId = React.useId()

  React.useEffect(() => {
    if (!open) setTyped('')
  }, [open])

  const hasTypeToConfirm = confirmText !== undefined
  const isConfirmed =
    !hasTypeToConfirm || typed.toLowerCase() === confirmText.toLowerCase()

  const buttonText = confirmButtonText ?? DEFAULT_CONFIRM_TEXT[variant]
  const label = confirmLabel ?? `Type "${confirmText}" to confirm`
  const Icon = VARIANT_ICONS[variant]
  const buttonStyle = VARIANT_BUTTON_STYLE[variant]
  // Destructive and warning variants prevent accidental overlay-click dismissal
  const blockInteractOutside = variant !== 'info'

  function handleOpenChange(nextOpen: boolean) {
    if (loading) return
    onOpenChange(nextOpen)
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent
        role="alertdialog"
        showCloseButton={false}
        onEscapeKeyDown={loading ? (e) => e.preventDefault() : undefined}
        onInteractOutside={
          blockInteractOutside ? (e) => e.preventDefault() : undefined
        }
        className="sm:max-w-md"
      >
        <div className="flex items-start gap-4">
          <div
            className={cn(iconContainerVariants({ variant }))}
            aria-hidden="true"
          >
            <Icon className="size-5" />
          </div>
          <div className="flex-1 space-y-1 pt-0.5">
            <DialogTitle>{title}</DialogTitle>
            <DialogDescription>{description}</DialogDescription>
          </div>
        </div>

        {hasTypeToConfirm && (
          <div>
            <label htmlFor={inputId} className="text-sm font-medium">
              {label}
            </label>
            <input
              id={inputId}
              type="text"
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && isConfirmed && !loading) {
                  e.preventDefault()
                  onConfirm()
                }
              }}
              disabled={loading}
              autoComplete="off"
              aria-required="true"
              className="border-input bg-background placeholder:text-muted-foreground focus-visible:ring-ring mt-2 flex h-9 w-full rounded-md border px-3 py-2 text-sm focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50"
            />
          </div>
        )}

        <DialogFooter>
          <Button
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={loading}
          >
            {cancelButtonText}
          </Button>
          <Button
            variant={buttonStyle}
            onClick={onConfirm}
            disabled={!isConfirmed || loading}
          >
            {loading && <Loader2 className="animate-spin" />}
            {buttonText}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
