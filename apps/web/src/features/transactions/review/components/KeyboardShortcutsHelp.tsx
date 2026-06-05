/**
 * KeyboardShortcutsHelp — modal dialog listing all review keyboard shortcuts.
 * Triggered by pressing '?' or clicking the help button.
 * All shortcuts also have visible button equivalents per WCAG requirement.
 */

import { Keyboard } from 'lucide-react'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { TRIAGE_SHORTCUTS } from '../hooks/useTriageKeyboard'

interface KeyboardShortcutsHelpProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function KeyboardShortcutsHelp({ open, onOpenChange }: KeyboardShortcutsHelpProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-sm" aria-describedby={undefined}>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Keyboard className="size-4" aria-hidden="true" />
            Keyboard shortcuts
          </DialogTitle>
        </DialogHeader>
        <dl className="space-y-2">
          {TRIAGE_SHORTCUTS.map(({ key, description }) => (
            <div key={key} className="flex items-center justify-between gap-4">
              <dt>
                <kbd className="inline-flex h-6 items-center rounded border border-border bg-muted px-1.5 font-mono text-xs text-muted-foreground">
                  {key}
                </kbd>
              </dt>
              <dd className="text-sm text-foreground">{description}</dd>
            </div>
          ))}
        </dl>
      </DialogContent>
    </Dialog>
  )
}
