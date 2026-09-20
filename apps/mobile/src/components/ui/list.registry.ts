import type { ComponentType } from 'react'
import type { PrimitiveRegistryEntry } from './index'
import { ListItem } from './ListItem'
import { SectionHeader } from './SectionHeader'

/**
 * `PrimitiveRegistryEntry.component` is `ComponentType<Record<string, unknown>>`; a component with
 * REQUIRED props (`ListItem.title`, `SectionHeader.title`) is not structurally assignable, so the
 * entries widen through this local cast (same reason as `fields.registry.ts`).
 */
const asComponent = (component: unknown) => component as ComponentType<Record<string, unknown>>

/**
 * Registry entries for the list primitives — one per text-bearing role, so Todo 20's contrast
 * suite derives one WCAG pair per entry.
 *
 * `ListItem.title.selected` pins the selected row to the `accent` surface with `accent-foreground`
 * text; `ListItem` applies `bg-accent` when `selected` is set.
 *
 * This file is intentionally NOT merged into `PRIMITIVE_REGISTRY` here — parallel primitive workers
 * each append their own registry module and the orchestrator wires the barrel in one later step.
 */
export const LIST_REGISTRY: PrimitiveRegistryEntry[] = [
  {
    component: asComponent(ListItem),
    name: 'ListItem.title',
    category: 'surface',
    interactive: true,
    foregroundToken: 'foreground',
    backgroundToken: 'card',
    ratioClass: 'body',
  },
  {
    component: asComponent(ListItem),
    name: 'ListItem.subtitle',
    category: 'surface',
    interactive: true,
    foregroundToken: 'muted-foreground',
    backgroundToken: 'card',
    ratioClass: 'body',
  },
  {
    component: asComponent(ListItem),
    name: 'ListItem.title.selected',
    category: 'surface',
    interactive: true,
    foregroundToken: 'accent-foreground',
    backgroundToken: 'accent',
    ratioClass: 'body',
  },
  {
    component: asComponent(SectionHeader),
    name: 'SectionHeader.title',
    category: 'surface',
    interactive: false,
    foregroundToken: 'muted-foreground',
    backgroundToken: 'background',
    ratioClass: 'body',
  },
]