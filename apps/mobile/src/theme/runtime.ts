import { Platform } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'

export type ResolvedTheme = 'light' | 'dark'

export type ThemeVariables = Record<`--${string}`, string>

export interface ThemeVariableRoot {
  style: { setProperty(name: string, value: string): void }
  setAttribute(name: string, value: string): void
}

/**
 * The resolved theme's CSS custom properties, `--`-prefixed. This is the same variable set the
 * pre-paint bootstrap script writes (Todo 5), so the runtime switch and the first paint cannot
 * disagree about which variables a theme owns.
 */
export function themeVariables(resolvedTheme: ResolvedTheme): ThemeVariables {
  const theme = nativeTokens[resolvedTheme]
  const merged = { ...theme.color, ...theme.contrast }

  return Object.fromEntries(
    Object.entries(merged).map(([name, value]) => [`--${name}`, value]),
  ) as ThemeVariables
}

/**
 * Web-only runtime switch. Writes the resolved theme's variables onto the document root — the
 * SAME mechanism the pre-paint script uses — so the page background behind the app flips too.
 * `VariableContextProvider` is the native mechanism and is deliberately not rendered on web.
 */
export function applyWebThemeVariables(
  resolvedTheme: ResolvedTheme,
  variables: ThemeVariables,
  root: ThemeVariableRoot,
): void {
  for (const [name, value] of Object.entries(variables)) {
    root.style.setProperty(name, value)
  }
  root.setAttribute('data-bb-theme', resolvedTheme)
}

export function isWebPlatform(): boolean {
  return Platform.OS === 'web'
}