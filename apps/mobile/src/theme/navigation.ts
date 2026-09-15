import { nativeNavigationTokens } from '@boostedbudget/design-tokens'
import { DarkTheme, DefaultTheme, type Theme } from 'expo-router'
import type { ColorSchemeName } from 'react-native'

type StatusBarStyle = 'dark' | 'light'

export type NavigationAppearance = Readonly<{
  theme: Theme
  statusBarStyle: StatusBarStyle
}>

export function getNavigationAppearance(colorScheme: ColorSchemeName): NavigationAppearance {
  const isDark = colorScheme === 'dark'
  const colors = isDark ? nativeNavigationTokens.dark : nativeNavigationTokens.light
  const baseTheme = isDark ? DarkTheme : DefaultTheme

  return {
    theme: {
      ...baseTheme,
      colors: {
        ...colors,
      },
    },
    statusBarStyle: isDark ? 'light' : 'dark',
  }
}
