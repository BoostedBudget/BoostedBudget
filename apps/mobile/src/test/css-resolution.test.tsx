import { act, render, screen } from '@testing-library/react-native'
import { StyleSheet, View } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { setColorScheme } from './color-scheme'

// The runtime resolves `oklch()` through lightningcss, which serialises `#ffffff` as `#fff`,
// while the adapter emits the same colour as six-digit hex. Expand shorthand before comparing.
function normalizeHex(color: string): string {
  const hex = color.toLowerCase()
  return /^#[0-9a-f]{3}$/.test(hex)
    ? `#${hex[1]}${hex[1]}${hex[2]}${hex[2]}${hex[3]}${hex[3]}`
    : hex
}

function resolvedStyle(testID: string): Record<string, unknown> {
  return StyleSheet.flatten(screen.getByTestId(testID).props.style) ?? {}
}

function CssProbe({ testID, className }: { testID: string; className: string }) {
  return <View testID={testID} className={className} />
}

describe('nativewind css resolution in jest', () => {
  it('resolves semantic colour utilities to the light token, then the dark token', () => {
    setColorScheme('light')
    render(<CssProbe testID="color-probe" className="bg-background text-foreground" />)

    const light = resolvedStyle('color-probe')
    expect(normalizeHex(String(light.backgroundColor))).toBe(
      normalizeHex(nativeTokens.light.color.background),
    )
    expect(light.color).toBe(nativeTokens.light.color.foreground)

    act(() => setColorScheme('dark'))

    const dark = resolvedStyle('color-probe')
    expect(normalizeHex(String(dark.backgroundColor))).toBe(
      normalizeHex(nativeTokens.dark.color.background),
    )
    expect(dark.color).toBe(nativeTokens.dark.color.foreground)
  })

  it('resolves spacing scale utilities to the token value', () => {
    render(<CssProbe testID="spacing-probe" className="p-bb-4" />)

    expect(resolvedStyle('spacing-probe').padding).toBe(nativeTokens.light.spacing['spacing-bb-4'])
  })
})
