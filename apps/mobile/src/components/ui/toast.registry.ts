import type { ComponentType } from 'react'
import type { PrimitiveRegistryEntry } from './index'
import { Toast } from './Toast'

/**
 * Registry entries for the `Toast` primitive — one per variant that renders text on a surface, so
 * Todo 20's contrast suite derives one WCAG pair per entry.
 *
 * `Toast.info` / `Toast.danger` deliberately point at the `*-strong` contrast fills with the
 * matching `*-foreground`, never the raw `info` / `destructive` values.
 *
 * `Toast` requires a `message` prop, so it is not structurally assignable to
 * `ComponentType<Record<string, unknown>>`; the same local widening cast used by
 * `fields.registry.ts` is required. This file is intentionally NOT merged into
 * `PRIMITIVE_REGISTRY` here — parallel primitive workers each append their own registry module and
 * the orchestrator wires the barrel in one later step.
 */
const asComponent = (component: unknown) => component as ComponentType<Record<string, unknown>>

export const TOAST_REGISTRY: PrimitiveRegistryEntry[] = [
  {
    component: asComponent(Toast),
    name: 'Toast.info',
    category: 'surface',
    interactive: false,
    foregroundToken: 'info-foreground',
    backgroundToken: 'bb-info-strong',
    ratioClass: 'body',
  },
  {
    component: asComponent(Toast),
    name: 'Toast.success',
    category: 'surface',
    interactive: false,
    foregroundToken: 'success-foreground',
    backgroundToken: 'success',
    ratioClass: 'body',
  },
  {
    component: asComponent(Toast),
    name: 'Toast.warning',
    category: 'surface',
    interactive: false,
    foregroundToken: 'warning-foreground',
    backgroundToken: 'warning',
    ratioClass: 'body',
  },
  {
    component: asComponent(Toast),
    name: 'Toast.danger',
    category: 'surface',
    interactive: false,
    foregroundToken: 'destructive-foreground',
    backgroundToken: 'bb-danger-strong',
    ratioClass: 'body',
  },
]
