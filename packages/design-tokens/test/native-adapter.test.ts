import { describe, expect, it } from 'bun:test'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import Color from 'colorjs.io'
import { nativeTokens } from '../generated/native'
import {
  contrastTokens,
  darkSemanticTokens,
  elevationTokens,
  focusTokens,
  lightSemanticTokens,
  motionTokens,
  nativeNavigationTokens,
  spacingTokens,
  touchTargetTokens,
  typographyTokens,
} from '../src/tokens'

const packageRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const nativePath = resolve(packageRoot, 'generated/native.ts')

const THEMES = ['light', 'dark'] as const
type Theme = (typeof THEMES)[number]

const semanticByTheme = { light: lightSemanticTokens, dark: darkSemanticTokens }
const contrastByTheme = { light: contrastTokens.light, dark: contrastTokens.dark }

// The generated module is `as const`; widen it so the drift loops can index by name.
const native = nativeTokens as unknown as Record<Theme, Record<string, Record<string, string | number>>>
const nav = nativeNavigationTokens as unknown as Record<Theme, Record<string, string>>

function colorEntries(theme: Theme): Array<{ key: string; css: string; native: string }> {
  return [
    ...Object.entries(semanticByTheme[theme]).map(([key, css]) => ({
      key: `color.${key}`,
      css,
      native: String(native[theme].color[key]),
    })),
    ...Object.entries(contrastByTheme[theme]).map(([key, css]) => ({
      key: `contrast.${key}`,
      css,
      native: String(native[theme].contrast[key]),
    })),
  ]
}

describe('native/StyleSheet token adapter', () => {
  it('covers every semantic colour key with no missing or orphan key', () => {
    for (const theme of THEMES) {
      expect(Object.keys(native[theme].color).sort()).toEqual(
        Object.keys(semanticByTheme[theme]).sort(),
      )
    }
  })

  it('covers every scale key with no missing or orphan key', () => {
    const sharedScales: Record<string, Record<string, unknown>> = {
      spacing: spacingTokens,
      typography: typographyTokens,
      elevation: elevationTokens,
      motion: motionTokens,
      touchTarget: touchTargetTokens,
      focus: focusTokens,
    }

    for (const theme of THEMES) {
      expect(Object.keys(native[theme]).sort()).toEqual(
        ['color', 'contrast', ...Object.keys(sharedScales)].sort(),
      )
      expect(Object.keys(native[theme].contrast).sort()).toEqual(
        Object.keys(contrastByTheme[theme]).sort(),
      )
      for (const [section, source] of Object.entries(sharedScales)) {
        expect(Object.keys(native[theme][section]).sort()).toEqual(Object.keys(source).sort())
      }
    }
  })

  it('contains no CSS-only syntax', () => {
    const source = readFileSync(nativePath, 'utf8')
    for (const banned of ['oklch(', 'var(--', 'color-mix(', 'prefers-color-scheme']) {
      expect(source).not.toContain(banned)
    }
  })

  it('round-trips every colour to within ΔE00 2 of the source (or the closest sRGB)', () => {
    for (const theme of THEMES) {
      for (const { key, css, native: nativeValue } of colorEntries(theme)) {
        const source = new Color(css)
        const generated = new Color(nativeValue)

        if (source.inGamut('srgb')) {
          expect(source.deltaE2000(generated), `${theme}.${key}`).toBeLessThanOrEqual(2)
        } else {
          // sRGB cannot represent this colour within ΔE00 2 — the closest achievable
          // sRGB is the gamut-mapped source (up to ΔE00 ~5.1 away). Assert the
          // generated value is that projection (≤ 2) and within the empirical floor.
          const closest = source.clone().toGamut({ space: 'srgb', method: 'lch.c' })
          expect(closest.deltaE2000(generated), `${theme}.${key} (closest)`).toBeLessThanOrEqual(2)
          expect(source.deltaE2000(generated), `${theme}.${key} (floor)`).toBeLessThanOrEqual(6)
        }
      }
    }
  })

  it('preserves alpha on the three translucent dark tokens', () => {
    const alphaKeys = ['border', 'sidebar-border', 'input'] as const
    for (const key of alphaKeys) {
      const source = new Color(darkSemanticTokens[key])
      const generated = new Color(String(native.dark.color[key]))
      expect(source.alpha, key).toBeLessThan(1)
      expect(Math.abs(generated.alpha - source.alpha), key).toBeLessThanOrEqual(1 / 255)
    }
  })

  it('converts numeric scales to numbers and keeps font families as strings', () => {
    expect(native.light.spacing['spacing-bb-0']).toBe(0)
    expect(native.light.spacing['spacing-bb-4']).toBe(16)
    expect(native.light.spacing['spacing-bb-16']).toBe(64)

    expect(native.light.typography['text-bb-xs']).toBe(12)
    expect(native.light.typography['text-bb-body']).toBe(16)
    expect(native.light.typography['text-bb-4xl']).toBe(36)
    expect(native.light.typography['leading-bb-normal']).toBe(1.5)
    expect(native.light.typography['font-weight-bb-bold']).toBe(700)
    expect(native.light.typography['tracking-bb-tight']).toBe(-0.025)
    expect(native.light.typography['tracking-bb-normal']).toBe(0)
    expect(typeof native.light.typography['font-bb-sans']).toBe('string')
    expect(String(native.light.typography['font-bb-sans'])).toContain('ui-sans-serif')

    expect(native.light.motion['transition-duration-bb-fast']).toBe(150)
    expect(native.light.touchTarget.min).toBe(44)
    expect(native.light.touchTarget.comfortable).toBe(48)
    expect(native.light.focus['bb-focus-ring-width']).toBe(2)
    expect(native.light.focus['bb-focus-ring-offset']).toBe(2)
    expect(typeof native.light.focus['bb-focus-ring-color']).toBe('string')
  })

  it('keeps nativeNavigationTokens: seven keys equal their conversions, five named exceptions', () => {
    const navToSemantic = {
      primary: 'primary',
      background: 'background',
      card: 'card',
      text: 'foreground',
      border: 'border',
      notification: 'destructive',
    } as const
    const exceptions = new Set([
      'light.primary',
      'light.text',
      'dark.background',
      'dark.card',
      'dark.notification',
    ])

    let equalCount = 0
    let exceptionCount = 0
    for (const theme of THEMES) {
      for (const [navKey, semanticKey] of Object.entries(navToSemantic)) {
        const navValue = nav[theme][navKey]
        const converted = String(native[theme].color[semanticKey])
        if (exceptions.has(`${theme}.${navKey}`)) {
          expect(navValue, `${theme}.${navKey}`).not.toBe(converted)
          exceptionCount += 1
        } else {
          expect(navValue, `${theme}.${navKey}`).toBe(converted)
          equalCount += 1
        }
      }
    }

    expect(equalCount).toBe(7)
    expect(exceptionCount).toBe(5)
  })

  it('pins the literal nativeNavigationTokens values', () => {
    expect(nativeNavigationTokens).toEqual({
      light: {
        primary: '#262626',
        background: '#ffffff',
        card: '#ffffff',
        text: '#171717',
        border: '#e5e5e5',
        notification: '#e7000b',
      },
      dark: {
        primary: '#e5e5e5',
        background: '#171717',
        card: '#262626',
        text: '#fafafa',
        border: '#ffffff1a',
        notification: '#fb2c36',
      },
    })
  })
})