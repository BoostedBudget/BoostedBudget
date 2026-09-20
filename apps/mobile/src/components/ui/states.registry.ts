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
 * `category` uses the existing `'surface'` member because these entries describe text painted on
 * a surface; `index.ts` is owned by the orchestrator and is deliberately not edited here.
 */
export const STATES_REGISTRY: PrimitiveRegistryEntry[] = [
  { component: asComponent(EmptyState), name: 'EmptyState.title', category: 'surface', interactive: false, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: asComponent(EmptyState), name: 'EmptyState.description', category: 'surface', interactive: false, foregroundToken: 'muted-foreground', backgroundToken: 'background', ratioClass: 'body' },

  { component: asComponent(ErrorState), name: 'ErrorState.title', category: 'surface', interactive: false, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: asComponent(ErrorState), name: 'ErrorState.description', category: 'surface', interactive: false, foregroundToken: 'muted-foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: asComponent(ErrorState), name: 'ErrorState.retry', category: 'surface', interactive: true, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: asComponent(ErrorState), name: 'ErrorState.detailToggle', category: 'surface', interactive: true, foregroundToken: 'primary', backgroundToken: 'background', ratioClass: 'body' },
  { component: asComponent(ErrorState), name: 'ErrorState.detail', category: 'surface', interactive: false, foregroundToken: 'muted-foreground', backgroundToken: 'background', ratioClass: 'body' },
]