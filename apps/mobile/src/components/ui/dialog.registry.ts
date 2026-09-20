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
 * `index.ts` concatenates this module into `PRIMITIVE_REGISTRY`.
 */
export const DIALOG_REGISTRY: PrimitiveRegistryEntry[] = [
  { component: asComponent(Dialog), name: 'Dialog.title', category: 'overlay', interactive: false, foregroundToken: 'popover-foreground', backgroundToken: 'popover', ratioClass: 'body' },
  { component: asComponent(Dialog), name: 'Dialog.description', category: 'overlay', interactive: false, foregroundToken: 'muted-foreground', backgroundToken: 'popover', ratioClass: 'body' },

  { component: asComponent(ConfirmDialog), name: 'ConfirmDialog.title', category: 'overlay', interactive: false, foregroundToken: 'popover-foreground', backgroundToken: 'popover', ratioClass: 'body' },
  { component: asComponent(ConfirmDialog), name: 'ConfirmDialog.message', category: 'overlay', interactive: false, foregroundToken: 'muted-foreground', backgroundToken: 'popover', ratioClass: 'body' },
  { component: asComponent(ConfirmDialog), name: 'ConfirmDialog.confirm', category: 'overlay', interactive: true, foregroundToken: 'primary-foreground', backgroundToken: 'primary', ratioClass: 'body' },
  { component: asComponent(ConfirmDialog), name: 'ConfirmDialog.confirmDestructive', category: 'overlay', interactive: true, foregroundToken: 'destructive-foreground', backgroundToken: 'bb-danger-strong', ratioClass: 'body' },
  { component: asComponent(ConfirmDialog), name: 'ConfirmDialog.cancel', category: 'overlay', interactive: true, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'body' },
]