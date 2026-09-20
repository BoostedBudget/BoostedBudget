import type { ComponentType } from 'react'
import type { PrimitiveRegistryEntry } from './index'
import { ThemeSelector } from './ThemeSelector'

// Required-prop components cannot satisfy `ComponentType<Record<string, unknown>>` structurally.
const asComponent = (component: unknown): ComponentType<Record<string, unknown>> =>
  component as ComponentType<Record<string, unknown>>

/**
 * One entry per text-bearing theme-selector state. Todo 20 derives one WCAG pair per entry from
 * `foregroundToken`/`backgroundToken` and checks it against `ratioClass`. `category` uses the
 * existing `'surface'` member because these entries describe text painted on a surface; `index.ts`
 * is owned by the orchestrator and is deliberately not edited here.
 */
export const THEME_REGISTRY: PrimitiveRegistryEntry[] = [
  { component: asComponent(ThemeSelector), name: 'ThemeSelector.option', category: 'surface', interactive: true, foregroundToken: 'muted-foreground', backgroundToken: 'card', ratioClass: 'body' },
  { component: asComponent(ThemeSelector), name: 'ThemeSelector.optionSelected', category: 'surface', interactive: true, foregroundToken: 'primary-foreground', backgroundToken: 'primary', ratioClass: 'body' },
]