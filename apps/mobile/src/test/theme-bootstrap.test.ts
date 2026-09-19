import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { buildThemeBootstrapScript, resolveBootstrapTheme } from '../theme/bootstrap-script'
import { resolveTheme } from '../theme/preference'

// The pre-paint script is a STRING, so these assertions are the only jest-visible
// contract. The built-HTML half (the script is actually inlined and precedes any
// `<script src=` / modulepreload) lives in Todo 22, which runs after `export:web`.
const script = buildThemeBootstrapScript(nativeTokens)

describe('buildThemeBootstrapScript', () => {
  it('reads the shared persisted preference key', () => {
    expect(script).toContain('bb-theme')
  })

  it('reads the user-scoped preference variant', () => {
    expect(script).toContain('bb-theme:')
  })

  it('resolves the system preference from matchMedia', () => {
    expect(script).toContain("matchMedia('(prefers-color-scheme: dark)')")
  })

  it('falls back to system for a corrupt stored value', () => {
    expect(script).toContain("value === 'light' || value === 'dark' || value === 'system'")
  })

  it('writes a data-bb-theme attribute for debuggability', () => {
    expect(script).toContain('data-bb-theme')
  })

  it('emits a superset of the generated dark map keys as CSS custom properties', () => {
    const darkKeys = [
      ...Object.keys(nativeTokens.dark.color),
      ...Object.keys(nativeTokens.dark.contrast),
    ]

    for (const key of darkKeys) {
      expect(script).toContain(`"${key}":`)
    }
  })

  it('emits the light map too so the resolved theme is always complete', () => {
    const lightKeys = [
      ...Object.keys(nativeTokens.light.color),
      ...Object.keys(nativeTokens.light.contrast),
    ]

    for (const key of lightKeys) {
      expect(script).toContain(`"${key}":`)
    }
  })
})

describe('resolveBootstrapTheme', () => {
  const preferences = ['light', 'dark', 'system'] as const
  const schemes = ['light', 'dark'] as const

  for (const preference of preferences) {
    for (const scheme of schemes) {
      it(`agrees with preference.ts for ${preference} × ${scheme}`, () => {
        expect(resolveBootstrapTheme(preference, scheme)).toBe(resolveTheme(preference, scheme))
      })
    }
  }
})