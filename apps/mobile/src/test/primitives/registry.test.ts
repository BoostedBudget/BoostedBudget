import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import * as UI from '../../components/ui'
import {
  PRIMITIVE_REGISTRY,
  type ContrastRatioClass,
  type PrimitiveCategory,
  type TextTone,
  type SurfaceVariant,
} from '../../components/ui'
import { BUTTON_REGISTRY } from '../../components/ui/button.registry'
import { CARD_REGISTRY } from '../../components/ui/card.registry'
import { DIALOG_REGISTRY } from '../../components/ui/dialog.registry'
import { FEEDBACK_REGISTRY } from '../../components/ui/feedback.registry'
import { FIELDS_REGISTRY } from '../../components/ui/fields.registry'
import { LIST_REGISTRY } from '../../components/ui/list.registry'
import { NAVIGATION_REGISTRY } from '../../components/ui/navigation.registry'
import { SHEET_REGISTRY } from '../../components/ui/sheet.registry'
import { STATES_REGISTRY } from '../../components/ui/states.registry'
import { THEME_REGISTRY } from '../../components/ui/theme.registry'
import { TOAST_REGISTRY } from '../../components/ui/toast.registry'
import { COLOR_SCHEMES } from './helpers'
import {
  NON_TEXT_BEARING_FILES,
  UI_DIR,
  findUncoveredComponentFiles,
  uiFiles,
} from './registry-coverage'

const UI_FILES = uiFiles()

const TEXT_TONES: readonly TextTone[] = ['default', 'muted', 'subtle', 'primary', 'success', 'warning', 'danger', 'info']
const SURFACE_VARIANTS: readonly SurfaceVariant[] = ['canvas', 'card', 'raised', 'sunken', 'overlay']
const CATEGORIES: readonly PrimitiveCategory[] = [
  'typography',
  'layout',
  'surface',
  'action',
  'field',
  'feedback',
  'overlay',
  'navigation',
  'list',
  'theme',
  'state',
]
const RATIO_CLASSES: readonly ContrastRatioClass[] = ['body', 'large', 'ui']

const REGISTRY_MODULES = [
  BUTTON_REGISTRY,
  CARD_REGISTRY,
  DIALOG_REGISTRY,
  FEEDBACK_REGISTRY,
  FIELDS_REGISTRY,
  LIST_REGISTRY,
  NAVIGATION_REGISTRY,
  SHEET_REGISTRY,
  STATES_REGISTRY,
  THEME_REGISTRY,
  TOAST_REGISTRY,
]

/**
 * The entries that live in `index.ts` (the typography, surface and layout primitives that predate
 * the per-file registry modules). They are not in a `*.registry.ts` module, so the count assertion
 * adds them to the module total explicitly.
 */
const BASE_ENTRY_NAMES = [
  ...TEXT_TONES.map((tone) => `Text.${tone}`),
  ...SURFACE_VARIANTS.map((variant) => `Surface.${variant}`),
  'Box',
  'Stack',
  'Inline',
]

/**
 * Component files that render no text on a surface, so there is no WCAG contrast pair to derive and
 * no registry entry to point at. Every OTHER file that exports a component MUST be referenced by at
 * least one registry entry: adding a new component file without an entry (and without deliberately
 * adding it to `NON_TEXT_BEARING_FILES`) fails the coverage test below. The list lives in
 * `./registry-coverage` so the Todo 20 accessibility suite reads the same source.
 */

describe('components/ui source bans', () => {
  it('contains the primitive files (the ban scan is not vacuous)', () => {
    expect(UI_FILES).toEqual(
      expect.arrayContaining(['Text.tsx', 'Box.tsx', 'Stack.tsx', 'Inline.tsx', 'Surface.tsx', 'index.ts']),
    )
  })

  it('never uses a `dark:` utility', () => {
    const offenders: string[] = []
    for (const file of UI_FILES) {
      const source = readFileSync(resolve(UI_DIR, file), 'utf8')
      if (source.includes('dark:')) offenders.push(file)
    }
    expect(offenders).toEqual([])
  })

  it('never disables font scaling', () => {
    const offenders: string[] = []
    for (const file of UI_FILES) {
      const source = readFileSync(resolve(UI_DIR, file), 'utf8')
      if (/allowFontScaling\s*[:=]\s*\{?\s*false/.test(source)) offenders.push(file)
    }
    expect(offenders).toEqual([])
  })
})

describe('PRIMITIVE_REGISTRY', () => {
  it('carries every required field with a valid value', () => {
    expect(PRIMITIVE_REGISTRY.length).toBeGreaterThan(0)
    for (const entry of PRIMITIVE_REGISTRY) {
      expect(entry.name.length).toBeGreaterThan(0)
      expect(CATEGORIES).toContain(entry.category)
      expect(typeof entry.interactive).toBe('boolean')
      expect(RATIO_CLASSES).toContain(entry.ratioClass)
      expect(entry.component).toBeDefined()
    }
  })

  it('resolves every foreground and background token from the native adapter in both themes', () => {
    for (const entry of PRIMITIVE_REGISTRY) {
      for (const scheme of COLOR_SCHEMES) {
        const section = { ...nativeTokens[scheme].color, ...nativeTokens[scheme].contrast } as Record<string, string>
        expect({ entry: entry.name, scheme, token: entry.foregroundToken }).toEqual({
          entry: entry.name,
          scheme,
          token: expect.any(String),
        })
        expect(section[entry.foregroundToken]).toBeDefined()
        expect(section[entry.backgroundToken]).toBeDefined()
      }
    }
  })

  it('has one entry per Text tone and one per Surface variant, plus each layout primitive', () => {
    const names = PRIMITIVE_REGISTRY.map((entry) => entry.name)
    for (const tone of TEXT_TONES) expect(names).toContain(`Text.${tone}`)
    for (const variant of SURFACE_VARIANTS) expect(names).toContain(`Surface.${variant}`)
    for (const name of ['Box', 'Stack', 'Inline']) expect(names).toContain(name)
  })

  it('references only the primitives the barrel exports', () => {
    const exported = new Set<unknown>(Object.values(UI))
    for (const entry of PRIMITIVE_REGISTRY) {
      expect(exported.has(entry.component)).toBe(true)
    }
    expect(PRIMITIVE_REGISTRY.find((entry) => entry.name === 'Box')?.component).toBe(UI.Box)
    expect(PRIMITIVE_REGISTRY.find((entry) => entry.name === 'Surface.card')?.component).toBe(UI.Surface)
  })

  it('concatenates every registry module exactly once and keeps every name unique', () => {
    const moduleTotal = REGISTRY_MODULES.reduce((sum, module) => sum + module.length, 0)
    expect(PRIMITIVE_REGISTRY.length).toBe(moduleTotal + BASE_ENTRY_NAMES.length)

    const names = PRIMITIVE_REGISTRY.map((entry) => entry.name)
    expect(new Set(names).size).toBe(names.length)

    for (const module of REGISTRY_MODULES) {
      for (const entry of module) expect(PRIMITIVE_REGISTRY).toContain(entry)
    }
  })

  it('covers every text-bearing component file with at least one registry entry', () => {
    const registered = PRIMITIVE_REGISTRY.map((entry) => entry.component)
    expect(findUncoveredComponentFiles(registered)).toEqual([])
    for (const file of NON_TEXT_BEARING_FILES) expect(UI_FILES).toContain(file)
  })

  it('pins the contrast remediation tokens to the info/danger body-text tones', () => {
    const info = PRIMITIVE_REGISTRY.find((entry) => entry.name === 'Text.info')
    const danger = PRIMITIVE_REGISTRY.find((entry) => entry.name === 'Text.danger')
    expect(info).toMatchObject({ foregroundToken: 'bb-info-text', backgroundToken: 'background', ratioClass: 'body' })
    expect(danger).toMatchObject({ foregroundToken: 'bb-danger-text', backgroundToken: 'background', ratioClass: 'body' })
  })
})
