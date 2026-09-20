import type { ComponentType } from 'react'
import type { PrimitiveRegistryEntry } from './index'
import { ActionSheet } from './ActionSheet'

// Required-prop components cannot satisfy `ComponentType<Record<string, unknown>>` structurally.
const asComponent = (component: unknown): ComponentType<Record<string, unknown>> =>
  component as ComponentType<Record<string, unknown>>

/**
 * One entry per text-bearing sheet role. Todo 20 derives one WCAG pair per entry from
 * `foregroundToken`/`backgroundToken` and checks it against `ratioClass`.
 *
 * `index.ts` concatenates this module into `PRIMITIVE_REGISTRY`.
 *
 * `ActionSheet.actionDestructive` deliberately points at the contrast tokens:
 * `destructive-foreground` on the `bb-danger-strong` FILL, never the raw `destructive` value.
 */
export const SHEET_REGISTRY: PrimitiveRegistryEntry[] = [
  { component: asComponent(ActionSheet), name: 'ActionSheet.title', category: 'overlay', interactive: false, foregroundToken: 'popover-foreground', backgroundToken: 'popover', ratioClass: 'body' },
  { component: asComponent(ActionSheet), name: 'ActionSheet.action', category: 'overlay', interactive: true, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: asComponent(ActionSheet), name: 'ActionSheet.actionDestructive', category: 'overlay', interactive: true, foregroundToken: 'destructive-foreground', backgroundToken: 'bb-danger-strong', ratioClass: 'body' },
  { component: asComponent(ActionSheet), name: 'ActionSheet.cancel', category: 'overlay', interactive: true, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'body' },
]