import type { ComponentType } from 'react'
import { Box } from './Box'
import { Inline } from './Inline'
import { Stack } from './Stack'
import { Surface } from './Surface'
import { Text } from './Text'

export { Box, Inline, Stack, Surface, Text }
export type { BoxProps, BoxBgTone, BoxBorderTone, BoxRadiusTone } from './Box'
export type { InlineProps, InlineAlign, InlineJustify } from './Inline'
export type { StackProps, StackAlign, StackJustify } from './Stack'
export type { SurfaceProps, SurfaceVariant } from './Surface'
export type { TextProps, TextVariant, TextTone, TextAlign } from './Text'
export type { SpacingStep, SpacingProp, FlexAlign, FlexJustify } from './spacing'

export type PrimitiveCategory = 'typography' | 'layout' | 'surface'
export type ContrastRatioClass = 'body' | 'large' | 'ui'

export interface PrimitiveRegistryEntry {
  component: ComponentType<Record<string, unknown>>
  name: string
  category: PrimitiveCategory
  interactive: boolean
  foregroundToken: string
  backgroundToken: string
  ratioClass: ContrastRatioClass
}

/**
 * One entry per (primitive x tone/variant) that renders text on a surface, plus one entry per
 * layout primitive. Todo 20 derives one WCAG pair per entry from `foregroundToken`/
 * `backgroundToken` (resolved against `nativeTokens[theme].color` or `.contrast`) and checks it
 * against `ratioClass`. Todos 9-18 append their own entries below; keep the array append-only.
 */
export const PRIMITIVE_REGISTRY: PrimitiveRegistryEntry[] = [
  { component: Text, name: 'Text.default', category: 'typography', interactive: false, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: Text, name: 'Text.muted', category: 'typography', interactive: false, foregroundToken: 'muted-foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: Text, name: 'Text.subtle', category: 'typography', interactive: false, foregroundToken: 'muted-foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: Text, name: 'Text.primary', category: 'typography', interactive: false, foregroundToken: 'primary', backgroundToken: 'background', ratioClass: 'body' },
  { component: Text, name: 'Text.success', category: 'typography', interactive: false, foregroundToken: 'success', backgroundToken: 'background', ratioClass: 'body' },
  { component: Text, name: 'Text.warning', category: 'typography', interactive: false, foregroundToken: 'warning-foreground', backgroundToken: 'warning', ratioClass: 'body' },
  { component: Text, name: 'Text.danger', category: 'typography', interactive: false, foregroundToken: 'bb-danger-text', backgroundToken: 'background', ratioClass: 'body' },
  { component: Text, name: 'Text.info', category: 'typography', interactive: false, foregroundToken: 'bb-info-text', backgroundToken: 'background', ratioClass: 'body' },

  { component: Surface, name: 'Surface.canvas', category: 'surface', interactive: false, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: Surface, name: 'Surface.card', category: 'surface', interactive: false, foregroundToken: 'card-foreground', backgroundToken: 'card', ratioClass: 'body' },
  { component: Surface, name: 'Surface.raised', category: 'surface', interactive: false, foregroundToken: 'card-foreground', backgroundToken: 'card', ratioClass: 'body' },
  { component: Surface, name: 'Surface.sunken', category: 'surface', interactive: false, foregroundToken: 'foreground', backgroundToken: 'muted', ratioClass: 'body' },
  { component: Surface, name: 'Surface.overlay', category: 'surface', interactive: false, foregroundToken: 'popover-foreground', backgroundToken: 'popover', ratioClass: 'body' },

  { component: Box, name: 'Box', category: 'layout', interactive: false, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'ui' },
  { component: Stack, name: 'Stack', category: 'layout', interactive: false, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'ui' },
  { component: Inline, name: 'Inline', category: 'layout', interactive: false, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'ui' },
]
