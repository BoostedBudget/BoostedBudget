import type { ComponentType } from 'react'
import type { PrimitiveRegistryEntry } from './index'
import { BackButton } from './BackButton'
import { ScreenHeader } from './ScreenHeader'
import { TabBar } from './TabBar'

// Required-prop components cannot satisfy `ComponentType<Record<string, unknown>>` structurally.
const asComponent = (component: unknown): ComponentType<Record<string, unknown>> =>
  component as ComponentType<Record<string, unknown>>

/**
 * One entry per text-bearing navigation role. Todo 20 derives one WCAG pair per entry from
 * `foregroundToken`/`backgroundToken` and checks it against `ratioClass`.
 *
 * The chrome paints `nativeNavigationTokens` (a native-safe projection of the same palette), so the
 * registry names the closest semantic tokens — `foreground` on `card` — which is what the contrast
 * suite resolves against `nativeTokens`. `index.ts` concatenates this module into
 * `PRIMITIVE_REGISTRY`.
 */
export const NAVIGATION_REGISTRY: PrimitiveRegistryEntry[] = [
  { component: asComponent(ScreenHeader), name: 'ScreenHeader.title', category: 'navigation', interactive: false, foregroundToken: 'foreground', backgroundToken: 'card', ratioClass: 'body' },
  { component: asComponent(TabBar), name: 'TabBar.label', category: 'navigation', interactive: true, foregroundToken: 'foreground', backgroundToken: 'card', ratioClass: 'body' },
  { component: asComponent(BackButton), name: 'BackButton.label', category: 'navigation', interactive: true, foregroundToken: 'foreground', backgroundToken: 'card', ratioClass: 'body' },
]