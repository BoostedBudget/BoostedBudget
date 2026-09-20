import type { ComponentType } from 'react'
import type { PrimitiveRegistryEntry } from './index'
import { ConfirmDialog } from './ConfirmDialog'
import { Dialog } from './Dialog'

// Required-prop components cannot satisfy `ComponentType<Record<string, unknown>>` structurally.
const asComponent = (component: unknown): ComponentType<Record<string, unknown>> =>
  component as ComponentType<Record<string, unknown>>

/**
 * One entry per (dialog surface x text role) that renders text on a surface. Todo 20 derives one
 * WCAG pair per entry from `foregroundToken`/`backgroundToken` and checks it against `ratioClass`.
 *
 * `category` uses the existing `'surface'` member because these entries describe text painted on a
 * surface; `index.ts` is owned by the orchestrator and is deliberately not edited here.
 */
export const DIALOG_REGISTRY: PrimitiveRegistryEntry[] = [
  { component: asComponent(Dialog), name: 'Dialog.title', category: 'surface', interactive: false, foregroundToken: 'popover-foreground', backgroundToken: 'popover', ratioClass: 'body' },
  { component: asComponent(Dialog), name: 'Dialog.description', category: 'surface', interactive: false, foregroundToken: 'muted-foreground', backgroundToken: 'popover', ratioClass: 'body' },

  { component: asComponent(ConfirmDialog), name: 'ConfirmDialog.title', category: 'surface', interactive: false, foregroundToken: 'popover-foreground', backgroundToken: 'popover', ratioClass: 'body' },
  { component: asComponent(ConfirmDialog), name: 'ConfirmDialog.message', category: 'surface', interactive: false, foregroundToken: 'muted-foreground', backgroundToken: 'popover', ratioClass: 'body' },
  { component: asComponent(ConfirmDialog), name: 'ConfirmDialog.confirm', category: 'surface', interactive: true, foregroundToken: 'primary-foreground', backgroundToken: 'primary', ratioClass: 'body' },
  { component: asComponent(ConfirmDialog), name: 'ConfirmDialog.confirmDestructive', category: 'surface', interactive: true, foregroundToken: 'destructive-foreground', backgroundToken: 'bb-danger-strong', ratioClass: 'body' },
  { component: asComponent(ConfirmDialog), name: 'ConfirmDialog.cancel', category: 'surface', interactive: true, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'body' },
]