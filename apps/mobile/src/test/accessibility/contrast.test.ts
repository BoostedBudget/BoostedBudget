import { afterEach } from '@jest/globals'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { PRIMITIVE_REGISTRY } from '../../components/ui'
import {
  CONTRAST_FLOORS,
  DECORATIVE_ALLOWLIST,
  MANDATORY_REMEDIATED_PAIRS,
  derivedContrastPairs,
  resolveToken,
  type ColorScheme,
} from './helpers'

afterEach(() => {
  jest.restoreAllMocks()
})

const THEMES: readonly ColorScheme[] = ['light', 'dark']

/**
 * Criterion 6 (#1656): contrast, computed in pure JS from the generated adapter.
 *
 * The pair set is DERIVED mechanically from `PRIMITIVE_REGISTRY` (one pair per entry, per theme),
 * never hand-listed and never measured against a hand-typed colour. The four remediated pairs are a
 * MANDATORY enumerated assertion set on top of the derived set.
 */
describe('accessibility: contrast (criterion 6)', () => {
  it('derives exactly one pair per registry entry, per theme', () => {
    const pairs = derivedContrastPairs()
    expect(pairs.length).toBe(PRIMITIVE_REGISTRY.length * THEMES.length)
    for (const scheme of THEMES) {
      expect(pairs.filter((pair) => pair.scheme === scheme)).toHaveLength(PRIMITIVE_REGISTRY.length)
    }
  })

  it('resolves every pair from the generated adapter, never a hand-typed colour', () => {
    for (const entry of PRIMITIVE_REGISTRY) {
      for (const scheme of THEMES) {
        expect(() => resolveToken(scheme, entry.foregroundToken)).not.toThrow()
        expect(() => resolveToken(scheme, entry.backgroundToken)).not.toThrow()
        expect(resolveToken(scheme, entry.foregroundToken)).toMatch(/^#[0-9a-f]{3,8}$/i)
        expect(resolveToken(scheme, entry.backgroundToken)).toMatch(/^#[0-9a-f]{3,8}$/i)
      }
    }
  })

  it('clears the WCAG floor for every derived pair in both themes', () => {
    const failures = derivedContrastPairs()
      .filter((pair) => pair.ratio < pair.floor)
      .map((pair) => `${pair.scheme} ${pair.entry}: ${pair.foregroundToken} on ${pair.backgroundToken} = ${pair.ratio.toFixed(3)} (need ${pair.floor})`)
    expect(failures).toEqual([])
  })

  it('enforces >= 4.5:1 for body text and >= 3:1 for large text / UI boundaries', () => {
    expect(CONTRAST_FLOORS).toEqual({ body: 4.5, large: 3, ui: 3 })
    for (const pair of derivedContrastPairs()) {
      expect(pair.floor).toBe(CONTRAST_FLOORS[pair.ratioClass])
    }
  })

  it('asserts the four remediated pairs at >= 4.5:1 against their new tokens', () => {
    for (const pair of MANDATORY_REMEDIATED_PAIRS) {
      const ratio = derivedPairRatio(pair.scheme, pair.foregroundToken, pair.backgroundToken)
      expect({ pair: pair.name, pass: ratio >= 4.5, ratio: ratio.toFixed(3) }).toEqual({
        pair: pair.name,
        pass: true,
        ratio: expect.any(String),
      })
    }
  })

  it('never allowlists a pair that renders body text', () => {
    const bodyPairs = derivedContrastPairs().filter((pair) => pair.ratioClass === 'body')
    for (const item of DECORATIVE_ALLOWLIST) {
      expect({ entry: item.entry, reason: item.reason.trim().length > 0 }).toEqual({
        entry: item.entry,
        reason: true,
      })
      const bodyMatch = bodyPairs.find((pair) => pair.entry === item.entry && pair.scheme === item.scheme)
      expect({ entry: item.entry, bodyText: bodyMatch !== undefined }).toEqual({
        entry: item.entry,
        bodyText: false,
      })
    }
  })

  it('never allowlists one of the four remediated pairs', () => {
    for (const pair of MANDATORY_REMEDIATED_PAIRS) {
      expect(DECORATIVE_ALLOWLIST.some((item) => item.entry === pair.name)).toBe(false)
    }
  })

  it('detects a token that drops below its floor (mutant)', () => {
    const section = nativeTokens.light.color as unknown as Record<string, string>
    const original = section['muted-foreground']
    try {
      section['muted-foreground'] = '#999999'
      const pair = derivedContrastPairs().find(
        (item) => item.entry === 'Text.muted' && item.scheme === 'light',
      )
      expect(pair?.ratio).toBeLessThan(CONTRAST_FLOORS.body)
    } finally {
      section['muted-foreground'] = original
    }
    // The mutation is reverted: the derived set is clean again.
    expect(derivedContrastPairs().filter((pair) => pair.ratio < pair.floor)).toEqual([])
  })
})

function derivedPairRatio(scheme: ColorScheme, foregroundToken: string, backgroundToken: string): number {
  const pair = derivedContrastPairs().find(
    (item) =>
      item.scheme === scheme &&
      item.foregroundToken === foregroundToken &&
      item.backgroundToken === backgroundToken,
  )
  if (!pair) throw new Error(`no derived pair for ${scheme} ${foregroundToken} on ${backgroundToken}`)
  return pair.ratio
}
