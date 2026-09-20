import type { PrimitiveRegistryEntry } from './index'
import { Alert } from './Alert'

/**
 * Feedback primitives' registry entries. Kept in a separate module so parallel primitive workers
 * never touch `index.ts`; the orchestrator merges this array into `PRIMITIVE_REGISTRY` in one later
 * step. One entry per (Alert × variant) that renders text on a surface.
 *
 * `Alert.info` / `Alert.danger` deliberately point at the `*-strong` contrast fills with the
 * matching `*-foreground`, not the raw `info` / `destructive` values (Todo 1's `contrastTokens`).
 */
export const FEEDBACK_REGISTRY: PrimitiveRegistryEntry[] = [
  {
    component: Alert,
    name: 'Alert.info',
    category: 'surface',
    interactive: false,
    foregroundToken: 'info-foreground',
    backgroundToken: 'bb-info-strong',
    ratioClass: 'body',
  },
  {
    component: Alert,
    name: 'Alert.success',
    category: 'surface',
    interactive: false,
    foregroundToken: 'success-foreground',
    backgroundToken: 'success',
    ratioClass: 'body',
  },
  {
    component: Alert,
    name: 'Alert.warning',
    category: 'surface',
    interactive: false,
    foregroundToken: 'warning-foreground',
    backgroundToken: 'warning',
    ratioClass: 'body',
  },
  {
    component: Alert,
    name: 'Alert.danger',
    category: 'surface',
    interactive: false,
    foregroundToken: 'destructive-foreground',
    backgroundToken: 'bb-danger-strong',
    ratioClass: 'body',
  },
]