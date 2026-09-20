import type { ComponentType } from 'react'
import type { PrimitiveRegistryEntry } from './index'
import { Card } from './Card'
import { StatCard } from './StatCard'

/**
 * `PrimitiveRegistryEntry.component` is `ComponentType<Record<string, unknown>>`; a component with
 * REQUIRED props (`StatCard.label`/`value`) is not structurally assignable, so the entries widen
 * through this local cast. `Card`'s props are all optional and would assign directly, but both use
 * the same helper so the reason is uniform.
 */
const asComponent = (component: unknown) => component as ComponentType<Record<string, unknown>>

/**
 * Registry entries for the `Card` and `StatCard` primitives — one per (Card variant × text role)
 * and one per StatCard text role, so Todo 20's contrast suite derives one WCAG pair per entry.
 *
 * ⚠ `Card.sunken.description` deliberately points at the full-strength `foreground` token:
 * `muted-foreground` on `muted` is only 4.35:1 in light mode and would fail the body-text floor.
 * `CardDescription` reads the variant from context and applies the same fallback.
 *
 * `index.ts` concatenates this module into `PRIMITIVE_REGISTRY`. The entries stay `category:
 * 'surface'` because Card/StatCard are surface primitives — this is the real category, not the
 * placeholder the other parallel modules used before the union was widened.
 */
export const CARD_REGISTRY: PrimitiveRegistryEntry[] = [
  {
    component: asComponent(Card),
    name: 'Card.default.title',
    category: 'surface',
    interactive: false,
    foregroundToken: 'foreground',
    backgroundToken: 'card',
    ratioClass: 'body',
  },
  {
    component: asComponent(Card),
    name: 'Card.default.description',
    category: 'surface',
    interactive: false,
    foregroundToken: 'muted-foreground',
    backgroundToken: 'card',
    ratioClass: 'body',
  },
  {
    component: asComponent(Card),
    name: 'Card.outlined.title',
    category: 'surface',
    interactive: false,
    foregroundToken: 'foreground',
    backgroundToken: 'card',
    ratioClass: 'body',
  },
  {
    component: asComponent(Card),
    name: 'Card.outlined.description',
    category: 'surface',
    interactive: false,
    foregroundToken: 'muted-foreground',
    backgroundToken: 'card',
    ratioClass: 'body',
  },
  {
    component: asComponent(Card),
    name: 'Card.elevated.title',
    category: 'surface',
    interactive: false,
    foregroundToken: 'foreground',
    backgroundToken: 'card',
    ratioClass: 'body',
  },
  {
    component: asComponent(Card),
    name: 'Card.elevated.description',
    category: 'surface',
    interactive: false,
    foregroundToken: 'muted-foreground',
    backgroundToken: 'card',
    ratioClass: 'body',
  },
  {
    component: asComponent(Card),
    name: 'Card.sunken.title',
    category: 'surface',
    interactive: false,
    foregroundToken: 'foreground',
    backgroundToken: 'muted',
    ratioClass: 'body',
  },
  {
    component: asComponent(Card),
    name: 'Card.sunken.description',
    category: 'surface',
    interactive: false,
    foregroundToken: 'foreground',
    backgroundToken: 'muted',
    ratioClass: 'body',
  },
  {
    component: asComponent(StatCard),
    name: 'StatCard.label',
    category: 'surface',
    interactive: false,
    foregroundToken: 'muted-foreground',
    backgroundToken: 'card',
    ratioClass: 'body',
  },
  {
    component: asComponent(StatCard),
    name: 'StatCard.value',
    category: 'surface',
    interactive: false,
    foregroundToken: 'foreground',
    backgroundToken: 'card',
    ratioClass: 'large',
  },
  {
    component: asComponent(StatCard),
    name: 'StatCard.delta.up',
    category: 'surface',
    interactive: false,
    foregroundToken: 'success',
    backgroundToken: 'card',
    ratioClass: 'body',
  },
  {
    component: asComponent(StatCard),
    name: 'StatCard.delta.down',
    category: 'surface',
    interactive: false,
    foregroundToken: 'bb-danger-text',
    backgroundToken: 'card',
    ratioClass: 'body',
  },
  {
    component: asComponent(StatCard),
    name: 'StatCard.delta.flat',
    category: 'surface',
    interactive: false,
    foregroundToken: 'muted-foreground',
    backgroundToken: 'card',
    ratioClass: 'body',
  },
]