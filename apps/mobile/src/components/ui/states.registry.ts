import type { ComponentType } from 'react'
import type { PrimitiveRegistryEntry } from './index'
import { EmptyState } from './EmptyState'
import { ErrorState } from './ErrorState'

// Required-prop components cannot satisfy `ComponentType<Record<string, unknown>>` structurally.
const asComponent = (component: unknown): ComponentType<Record<string, unknown>> =>
  component as ComponentType<Record<string, unknown>>

/**
 * One entry per text-bearing role in the empty/error states. Todo 20 derives one WCAG pair per
 * entry from `foregroundToken`/`backgroundToken` and checks it against `ratioClass`.
 *
 * `index.ts` concatenates this module into `PRIMITIVE_REGISTRY`.
 */
export const STATES_REGISTRY: PrimitiveRegistryEntry[] = [
  { component: asComponent(EmptyState), name: 'EmptyState.title', category: 'state', interactive: false, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: asComponent(EmptyState), name: 'EmptyState.description', category: 'state', interactive: false, foregroundToken: 'muted-foreground', backgroundToken: 'background', ratioClass: 'body' },

  { component: asComponent(ErrorState), name: 'ErrorState.title', category: 'state', interactive: false, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: asComponent(ErrorState), name: 'ErrorState.description', category: 'state', interactive: false, foregroundToken: 'muted-foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: asComponent(ErrorState), name: 'ErrorState.retry', category: 'state', interactive: true, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: asComponent(ErrorState), name: 'ErrorState.detailToggle', category: 'state', interactive: true, foregroundToken: 'primary', backgroundToken: 'background', ratioClass: 'body' },
  { component: asComponent(ErrorState), name: 'ErrorState.detail', category: 'state', interactive: false, foregroundToken: 'muted-foreground', backgroundToken: 'background', ratioClass: 'body' },
]