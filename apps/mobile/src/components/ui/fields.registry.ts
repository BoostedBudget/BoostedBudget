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
 * `index.ts` concatenates this module into `PRIMITIVE_REGISTRY`.
 */
export const FIELDS_REGISTRY: PrimitiveRegistryEntry[] = [
  { component: Label, name: 'Label.default', category: 'field', interactive: false, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: Label, name: 'Label.required', category: 'field', interactive: false, foregroundToken: 'bb-danger-text', backgroundToken: 'background', ratioClass: 'body' },
  { component: Label, name: 'Label.disabled', category: 'field', interactive: false, foregroundToken: 'muted-foreground', backgroundToken: 'background', ratioClass: 'body' },

  { component: asComponent(FormField), name: 'FormField.label', category: 'field', interactive: false, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: asComponent(FormField), name: 'FormField.hint', category: 'field', interactive: false, foregroundToken: 'muted-foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: asComponent(FormField), name: 'FormField.error', category: 'field', interactive: false, foregroundToken: 'bb-danger-text', backgroundToken: 'background', ratioClass: 'body' },

  { component: TextField, name: 'TextField.value', category: 'field', interactive: true, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: TextField, name: 'TextField.placeholder', category: 'field', interactive: true, foregroundToken: 'muted-foreground', backgroundToken: 'background', ratioClass: 'body' },

  { component: TextArea, name: 'TextArea.value', category: 'field', interactive: true, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: TextArea, name: 'TextArea.placeholder', category: 'field', interactive: true, foregroundToken: 'muted-foreground', backgroundToken: 'background', ratioClass: 'body' },

  { component: asComponent(Switch), name: 'Switch.label', category: 'field', interactive: true, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'body' },
  { component: asComponent(Checkbox), name: 'Checkbox.label', category: 'field', interactive: true, foregroundToken: 'foreground', backgroundToken: 'background', ratioClass: 'body' },
]