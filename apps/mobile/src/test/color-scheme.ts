import { Appearance } from 'react-native'
import { colorScheme } from 'react-native-css/native'

/**
 * Pins the colour scheme for a resolved-style test.
 *
 * Two resolution paths must agree for `@media (prefers-color-scheme: dark)` rules to apply:
 * React Native's `Appearance` API and `react-native-css`'s own observable, which its media-query
 * evaluator reads (`react-native-css/dist/module/native/conditions/media-query.js`).
 * `react-native-css/jest` resets both before every test, so call this inside a test — or in a
 * `beforeEach` registered after the `react-native-css/jest` import in `jest.setup.ts`.
 */
export function setColorScheme(scheme: 'light' | 'dark'): void {
  Appearance.setColorScheme(scheme)
  colorScheme.set(scheme)
}
