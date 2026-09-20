import { render, screen } from '@testing-library/react-native'
import { StyleSheet, View } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { setColorScheme } from '../color-scheme'

export const COLOR_SCHEMES = ['light', 'dark'] as const
export type ColorScheme = (typeof COLOR_SCHEMES)[number]

export const TOKEN_COLOR_KEYS = Object.keys(nativeTokens.light.color)
export const TOKEN_CONTRAST_KEYS = Object.keys(nativeTokens.light.contrast)
export const ALL_TOKEN_KEYS = [...TOKEN_COLOR_KEYS, ...TOKEN_CONTRAST_KEYS]

// Tailwind's content scanner is textual: it emits a `bg-*` utility only when the literal appears
// somewhere under `@source './src'`. The probe renders these literal classes directly, so every
// adapter colour is resolved from its own token utility rather than a hand-typed hex.
const TOKEN_BG_LITERALS =
  'bg-background bg-foreground bg-card bg-card-foreground bg-popover bg-popover-foreground ' +
  'bg-primary bg-primary-foreground bg-secondary bg-secondary-foreground bg-muted bg-muted-foreground ' +
  'bg-accent bg-accent-foreground bg-destructive bg-destructive-foreground bg-success bg-success-foreground ' +
  'bg-warning bg-warning-foreground bg-info bg-info-foreground bg-border bg-input bg-ring ' +
  'bg-chart-1 bg-chart-2 bg-chart-3 bg-chart-4 bg-chart-5 bg-sidebar bg-sidebar-foreground ' +
  'bg-sidebar-primary bg-sidebar-primary-foreground bg-sidebar-accent bg-sidebar-accent-foreground ' +
  'bg-sidebar-border bg-sidebar-ring bg-skeleton bg-bb-info-text bg-bb-danger-text bg-bb-info-strong bg-bb-danger-strong'

const TOKEN_BG_CLASSES = TOKEN_BG_LITERALS.split(' ')

/** The runtime serialises `#ffffff` as `#fff`; expand shorthand so both forms compare equal. */
export function normalizeHex(color: string): string {
  const hex = color.trim().toLowerCase()
  return /^#[0-9a-f]{3}$/.test(hex) ? `#${hex[1]}${hex[1]}${hex[2]}${hex[2]}${hex[3]}${hex[3]}` : hex
}

export function resolvedStyle(testID: string): Record<string, unknown> {
  return (StyleSheet.flatten(screen.getByTestId(testID).props.style) ?? {}) as Record<string, unknown>
}

export function resolvedColor(testID: string, prop: string): string | undefined {
  const value = resolvedStyle(testID)[prop]
  return typeof value === 'string' ? normalizeHex(value) : undefined
}

const ELEVATION_PROPS = new Set(['boxShadow', 'shadowColor', 'textShadowColor', 'textShadowOffset'])

export function collectHexColors(style: Record<string, unknown>): string[] {
  return Object.entries(style)
    .filter(
      ([prop, value]) =>
        !ELEVATION_PROPS.has(prop) && typeof value === 'string' && /^#[0-9a-f]{3,8}$/i.test(value),
    )
    .map(([, value]) => normalizeHex(value as string))
}

export function adapterColors(scheme: ColorScheme): string[] {
  const merged = { ...nativeTokens[scheme].color, ...nativeTokens[scheme].contrast } as Record<string, string>
  return Object.values(merged).map(normalizeHex)
}

function buildRuntimeTokenColors(): Record<ColorScheme, Record<string, string>> {
  const result = { light: {}, dark: {} } as Record<ColorScheme, Record<string, string>>
  for (const scheme of COLOR_SCHEMES) {
    setColorScheme(scheme)
    render(
      <View>
        {TOKEN_BG_CLASSES.map((className) => (
          <View key={className} testID={`token-${scheme}-${className.slice(3)}`} className={className} />
        ))}
      </View>,
    )
    for (const key of ALL_TOKEN_KEYS) {
      const background = resolvedStyle(`token-${scheme}-${key}`).backgroundColor
      if (typeof background === 'string') result[scheme][key] = normalizeHex(background)
    }
  }
  return result
}

let runtimeCache: Record<ColorScheme, Record<string, string>> | undefined

/**
 * Every token's colour as the CSS runtime actually resolves it, keyed by adapter token name.
 *
 * The runtime maps out-of-gamut `oklch()` through lightningcss, which can differ from the
 * adapter's `colorjs.io` conversion by up to 19/255 per channel (e.g. `chart-1`). Assertions
 * compare against this map rather than the adapter's hex so they pin the token *name* — the
 * primitive cannot pass with a raw `info` where `bb-info-text` is required.
 */
export function runtimeTokenColors(): Record<ColorScheme, Record<string, string>> {
  if (!runtimeCache) runtimeCache = buildRuntimeTokenColors()
  return runtimeCache
}

/**
 * Colours a primitive is allowed to render: the adapter's values plus the runtime-resolved
 * values of every token utility. A hard-coded hex such as `#ff0000` is in neither set.
 */
export function allowedColors(): Set<string> {
  const runtime = runtimeTokenColors()
  const allowed = new Set<string>()
  for (const scheme of COLOR_SCHEMES) {
    for (const color of adapterColors(scheme)) allowed.add(color)
    for (const color of Object.values(runtime[scheme])) allowed.add(color)
  }
  return allowed
}
