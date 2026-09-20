import type { PrimitiveRegistryEntry } from './index'
import { Button } from './Button'

/**
 * Registry entries for the interactive `Button` primitive — one per variant that renders text on
 * a surface, so Todo 20's contrast suite derives one WCAG pair per entry.
 *
 * ⚠ `Button.destructive` deliberately points at the contrast tokens: `destructive-foreground` on
 * the `bb-danger-strong` FILL, never the raw `destructive` value (which is only 2.77:1 against its
 * near-white foreground in dark mode).
 *
 * `index.ts` concatenates this module into `PRIMITIVE_REGISTRY`.
 */
export const BUTTON_REGISTRY: PrimitiveRegistryEntry[] = [
  {
    component: Button,
    name: 'Button.primary',
    category: 'action',
    interactive: true,
    foregroundToken: 'primary-foreground',
    backgroundToken: 'primary',
    ratioClass: 'body',
  },
  {
    component: Button,
    name: 'Button.secondary',
    category: 'action',
    interactive: true,
    foregroundToken: 'secondary-foreground',
    backgroundToken: 'secondary',
    ratioClass: 'body',
  },
  {
    component: Button,
    name: 'Button.outline',
    category: 'action',
    interactive: true,
    foregroundToken: 'foreground',
    backgroundToken: 'background',
    ratioClass: 'body',
  },
  {
    component: Button,
    name: 'Button.ghost',
    category: 'action',
    interactive: true,
    foregroundToken: 'foreground',
    backgroundToken: 'background',
    ratioClass: 'body',
  },
  {
    component: Button,
    name: 'Button.destructive',
    category: 'action',
    interactive: true,
    foregroundToken: 'destructive-foreground',
    backgroundToken: 'bb-danger-strong',
    ratioClass: 'body',
  },
  {
    component: Button,
    name: 'Button.link',
    category: 'action',
    interactive: true,
    foregroundToken: 'primary',
    backgroundToken: 'background',
    ratioClass: 'body',
  },
]
