import type { ComponentType } from 'react'
import type { PrimitiveRegistryEntry } from './index'
import { ThemeSelector } from './ThemeSelector'

// Required-prop components cannot satisfy `ComponentType<Record<string, unknown>>` structurally.
const asComponent = (component: unknown): ComponentType<Record<string, unknown>> =>
  component as ComponentType<Record<string, unknown>>

/**
 * One entry per text-bearing theme-selector state. Todo 20 derives one WCAG pair per entry from
 * `foregroundToken`/`backgroundToken` and checks it against `ratioClass`. `index.ts` concatenates
 * this module into `PRIMITIVE_REGISTRY`.
 */
export const THEME_REGISTRY: PrimitiveRegistryEntry[] = [
  { component: asComponent(ThemeSelector), name: 'ThemeSelector.option', category: 'theme', interactive: true, foregroundToken: 'muted-foreground', backgroundToken: 'card', ratioClass: 'body' },
  { component: asComponent(ThemeSelector), name: 'ThemeSelector.optionSelected', category: 'theme', interactive: true, foregroundToken: 'primary-foreground', backgroundToken: 'primary', ratioClass: 'body' },
]