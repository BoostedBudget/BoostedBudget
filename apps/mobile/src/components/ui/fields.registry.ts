import type { ComponentType } from 'react'
import type { PrimitiveRegistryEntry } from './index'
import { Checkbox } from './Checkbox'
import { FormField } from './FormField'
import { Label } from './Label'
import { Switch } from './Switch'
import { TextArea } from './TextArea'
import { TextField } from './TextField'

// Required-prop components cannot satisfy `ComponentType<Record<string, unknown>>` structurally.
const asComponent = (component: unknown): ComponentType<Record<string, unknown>> =>
  component as ComponentType<Record<string, unknown>>

/**
 * One entry per (field control x state) that renders text on a surface. Todo 20 derives one WCAG
 * pair per entry from `foregroundToken`/`backgroundToken` and checks it against `ratioClass`.
 *
 * `category` uses the existing `'surface'` member because these entries describe text painted on
 * a surface; `index.ts` is owned by the orchestrator and is deliberately not edited here.
 */
export const FIELDS_REGISTRY: PrimitiveRegistryEntry[] = [
  { component: Label, name: 'Label.default', category: 'surface', interactive: false, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: Label, name: 'Label.required', category: 'surface', interactive: false, foregroundToken: 'bb-danger-text', backgroundToken: 'background', ratioClass: 'body' },
  { component: Label, name: 'Label.disabled', category: 'surface', interactive: false, foregroundToken: 'muted-foreground', backgroundToken: 'background', ratioClass: 'body' },

  { component: asComponent(FormField), name: 'FormField.label', category: 'surface', interactive: false, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: asComponent(FormField), name: 'FormField.hint', category: 'surface', interactive: false, foregroundToken: 'muted-foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: asComponent(FormField), name: 'FormField.error', category: 'surface', interactive: false, foregroundToken: 'bb-danger-text', backgroundToken: 'background', ratioClass: 'body' },

  { component: TextField, name: 'TextField.value', category: 'surface', interactive: true, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: TextField, name: 'TextField.placeholder', category: 'surface', interactive: true, foregroundToken: 'muted-foreground', backgroundToken: 'background', ratioClass: 'body' },

  { component: TextArea, name: 'TextArea.value', category: 'surface', interactive: true, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: TextArea, name: 'TextArea.placeholder', category: 'surface', interactive: true, foregroundToken: 'muted-foreground', backgroundToken: 'background', ratioClass: 'body' },

  { component: asComponent(Switch), name: 'Switch.label', category: 'surface', interactive: true, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: asComponent(Checkbox), name: 'Checkbox.label', category: 'surface', interactive: true, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'body' },
]