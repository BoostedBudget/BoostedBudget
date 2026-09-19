import { beforeAll, beforeEach } from '@jest/globals'
import { dirname, resolve } from 'node:path'
import { readFileSync } from 'node:fs'
import tailwindcss from '@tailwindcss/postcss'
import postcss from 'postcss'
import { registerCSS } from 'react-native-css/jest'
import { setColorScheme } from './src/test/color-scheme'

jest.mock('expo-router', () => {
  const Stack = ({ children }: { children?: React.ReactNode }) => children ?? null
  Stack.Screen = () => null
  const fonts = {
    regular: { fontFamily: 'System', fontWeight: '400' },
    medium: { fontFamily: 'System', fontWeight: '500' },
    bold: { fontFamily: 'System', fontWeight: '600' },
    heavy: { fontFamily: 'System', fontWeight: '700' },
  }
  return {
    DarkTheme: { dark: true, colors: {}, fonts },
    DefaultTheme: { dark: false, colors: {}, fonts },
    Link: ({ children }: { children: React.ReactNode }) => children,
    Stack,
    ThemeProvider: jest.fn(({ children }: { children: React.ReactNode }) => children),
  }
})

jest.mock('expo-status-bar', () => ({
  StatusBar: jest.fn(() => null),
}))

// The native AsyncStorage build throws at import time when no native module is
// registered, which is always the case under jest. Use the package's own
// in-memory mock so the theme provider can be exercised deterministically.
jest.mock('@react-native-async-storage/async-storage', () =>
  require('@react-native-async-storage/async-storage/jest/async-storage-mock'),
)

const MOBILE_ROOT = dirname(require.resolve('./package.json'))
const GLOBAL_CSS_PATH = resolve(MOBILE_ROOT, 'global.css')

// Probe utilities that no production source references yet. Jest-only: the native build picks
// these up from real component usage, while the probe test must resolve them regardless of what
// Tailwind's content scan happens to see. `base` pins the scan to `apps/mobile` so the compiled
// stylesheet does not depend on the directory jest was launched from.
const PROBE_UTILITIES = '@source inline("bg-background text-foreground p-bb-4");'

let compiledCss: string | undefined

beforeAll(async () => {
  // `registerCSS` compiles plain CSS into `StyleCollection`; it does not expand Tailwind
  // directives (`@import 'tailwindcss'`, `@source`) or package `@import`s, so `global.css` goes
  // through the real Tailwind/PostCSS pipeline first. The Tailwind pass is the expensive half,
  // so it runs once per test file; the react-native-css compile in `beforeEach` is ~17ms.
  const source = `${readFileSync(GLOBAL_CSS_PATH, 'utf8')}\n${PROBE_UTILITIES}\n`
  const { css } = await postcss([tailwindcss({ base: MOBILE_ROOT })]).process(source, {
    from: GLOBAL_CSS_PATH,
  })
  compiledCss = css
})

beforeEach(() => {
  // Resolving `className` in jest needs BOTH halves:
  //   1. the `react-native` -> `react-native-css/components` import rewrite, which only the
  //      jest-scoped `nativewind/babel` preset in `babel.config.js` performs (Metro does it in
  //      its resolver for the native build), and
  //   2. `registerCSS`, which compiles the plain CSS into `StyleCollection` for the rewritten
  //      components to read. `registerCSS` alone is not enough — without (1) the plain
  //      `react-native` components ignore `className` entirely.
  // `react-native-css/jest` clears `StyleCollection` in its own `beforeEach`, registered by the
  // import above and therefore running before this one, so re-register per test.
  if (compiledCss) {
    registerCSS(compiledCss)
  }
  // `react-native-css/jest` resets the source build's `colorScheme` observable, which the runtime
  // the rewritten components use does not read, so a `dark` test leaks into the next one. Reset
  // explicitly; a test renders in dark by calling `setColorScheme('dark')` itself.
  setColorScheme('light')
})
