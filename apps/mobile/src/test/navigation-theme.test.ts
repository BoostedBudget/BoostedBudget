import { nativeNavigationTokens } from '@boostedbudget/design-tokens'
import { getNavigationAppearance } from '../theme/navigation'

describe('navigation appearance', () => {
  it('uses shared light tokens for navigation chrome and a dark status bar', () => {
    const appearance = getNavigationAppearance('light')

    expect(appearance.theme.dark).toBe(false)
    expect(appearance.theme.colors.background).toBe(nativeNavigationTokens.light.background)
    expect(appearance.theme.colors.card).toBe(nativeNavigationTokens.light.card)
    expect(appearance.theme.colors.text).toBe(nativeNavigationTokens.light.text)
    expect(appearance.statusBarStyle).toBe('dark')
  })

  it('uses shared dark tokens for navigation chrome and a light status bar', () => {
    const appearance = getNavigationAppearance('dark')

    expect(appearance.theme.dark).toBe(true)
    expect(appearance.theme.colors.background).toBe(nativeNavigationTokens.dark.background)
    expect(appearance.theme.colors.card).toBe(nativeNavigationTokens.dark.card)
    expect(appearance.theme.colors.text).toBe(nativeNavigationTokens.dark.text)
    expect(appearance.theme.colors.card).not.toBe(nativeNavigationTokens.light.card)
    expect(appearance.statusBarStyle).toBe('light')
  })

  it('falls back to the light appearance when the system preference is unavailable', () => {
    expect(getNavigationAppearance('unspecified')).toEqual(getNavigationAppearance('light'))
  })
})
