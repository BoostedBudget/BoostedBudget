import { readFileSync, readdirSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import {
  Box,
  Inline,
  PRIMITIVE_REGISTRY,
  Stack,
  Surface,
  Text,
  type ContrastRatioClass,
  type PrimitiveCategory,
  type TextTone,
  type SurfaceVariant,
} from '../../components/ui'
import { COLOR_SCHEMES } from './helpers'

const MOBILE_ROOT = dirname(require.resolve('../../../package.json'))
const UI_DIR = resolve(MOBILE_ROOT, 'src/components/ui')

const UI_FILES = readdirSync(UI_DIR).filter((file) => file.endsWith('.ts') || file.endsWith('.tsx'))

const TEXT_TONES: readonly TextTone[] = ['default', 'muted', 'subtle', 'primary', 'success', 'warning', 'danger', 'info']
const SURFACE_VARIANTS: readonly SurfaceVariant[] = ['canvas', 'card', 'raised', 'sunken', 'overlay']
const CATEGORIES: readonly PrimitiveCategory[] = ['typography', 'layout', 'surface']
const RATIO_CLASSES: readonly ContrastRatioClass[] = ['body', 'large', 'ui']

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
    const components = [Text, Box, Stack, Inline, Surface]
    for (const entry of PRIMITIVE_REGISTRY) {
      expect(components).toContain(entry.component)
    }
    expect(PRIMITIVE_REGISTRY.find((entry) => entry.name === 'Box')?.component).toBe(Box)
    expect(PRIMITIVE_REGISTRY.find((entry) => entry.name === 'Surface.card')?.component).toBe(Surface)
  })

  it('pins the contrast remediation tokens to the info/danger body-text tones', () => {
    const info = PRIMITIVE_REGISTRY.find((entry) => entry.name === 'Text.info')
    const danger = PRIMITIVE_REGISTRY.find((entry) => entry.name === 'Text.danger')
    expect(info).toMatchObject({ foregroundToken: 'bb-info-text', backgroundToken: 'background', ratioClass: 'body' })
    expect(danger).toMatchObject({ foregroundToken: 'bb-danger-text', backgroundToken: 'background', ratioClass: 'body' })
  })
})
