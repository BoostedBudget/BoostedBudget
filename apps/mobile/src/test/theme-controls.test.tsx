import AsyncStorage from '@react-native-async-storage/async-storage'
import { afterEach, beforeEach } from '@jest/globals'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react-native'
import { StyleSheet, Text } from 'react-native'
import * as RN from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { ThemeSelector } from '../components/ui/ThemeSelector'
import { THEME_REGISTRY } from '../components/ui/theme.registry'
import { ThemeProvider, useTheme } from '../theme/ThemeProvider'

function ResolvedThemeProbe() {
  const { resolvedTheme } = useTheme()
  return <Text testID="resolved-theme">{resolvedTheme}</Text>
}

function renderSelector() {
  return render(
    <ThemeProvider>
      <ThemeSelector />
      <ResolvedThemeProbe />
    </ThemeProvider>,
  )
}

describe('ThemeSelector', () => {
  beforeEach(async () => {
    await AsyncStorage.clear()
    jest.clearAllMocks()
  })

  afterEach(() => {
    jest.restoreAllMocks()
  })

  it('renders three options with accessible names', async () => {
    renderSelector()
    await act(async () => {})

    expect(screen.getByRole('radio', { name: 'Light' })).toBeOnTheScreen()
    expect(screen.getByRole('radio', { name: 'Dark' })).toBeOnTheScreen()
    expect(screen.getByRole('radio', { name: 'System' })).toBeOnTheScreen()
  })

  it('exposes accessibilityState.selected on the selected option only', async () => {
    await AsyncStorage.setItem('bb-theme', 'dark')
    renderSelector()
    await act(async () => {})

    expect(screen.getByTestId('theme-selector-dark').props.accessibilityState).toEqual({
      selected: true,
    })
    expect(screen.getByTestId('theme-selector-light').props.accessibilityState).toEqual({
      selected: false,
    })
    expect(screen.getByTestId('theme-selector-system').props.accessibilityState).toEqual({
      selected: false,
    })
  })

  it('writes each selection through the store and re-renders the resolved theme', async () => {
    jest.spyOn(RN, 'useColorScheme').mockReturnValue('dark')
    renderSelector()
    await act(async () => {})

    expect(screen.getByTestId('resolved-theme')).toHaveTextContent('dark')

    await act(async () => {
      fireEvent.press(screen.getByTestId('theme-selector-light'))
    })
    expect(screen.getByTestId('resolved-theme')).toHaveTextContent('light')
    await waitFor(() => expect(AsyncStorage.setItem).toHaveBeenCalledWith('bb-theme', 'light'))

    await act(async () => {
      fireEvent.press(screen.getByTestId('theme-selector-dark'))
    })
    expect(screen.getByTestId('resolved-theme')).toHaveTextContent('dark')

    await act(async () => {
      fireEvent.press(screen.getByTestId('theme-selector-system'))
    })
    expect(screen.getByTestId('resolved-theme')).toHaveTextContent('dark')
  })

  it('meets the touch floor on every option', async () => {
    renderSelector()
    await act(async () => {})

    for (const value of ['light', 'dark', 'system']) {
      const style = StyleSheet.flatten(
        screen.getByTestId(`theme-selector-${value}`).props.style,
      ) as Record<string, unknown>
      expect(style.minHeight).toBeGreaterThanOrEqual(nativeTokens.light.touchTarget.min)
    }
  })

  it('paints the selected label with the primary-foreground token', async () => {
    await AsyncStorage.setItem('bb-theme', 'dark')
    renderSelector()
    await act(async () => {})

    const style = StyleSheet.flatten(
      screen.getByTestId('theme-selector-dark-label').props.style,
    ) as Record<string, unknown>
    expect(style.color).toBe(nativeTokens.light.color['primary-foreground'])
  })
})

describe('THEME_REGISTRY', () => {
  it('names tokens that resolve in both themes', () => {
    expect(THEME_REGISTRY.length).toBeGreaterThan(0)

    for (const entry of THEME_REGISTRY) {
      for (const theme of ['light', 'dark'] as const) {
        const colors = {
          ...nativeTokens[theme].color,
          ...nativeTokens[theme].contrast,
        } as Record<string, string>
        expect(colors[entry.foregroundToken]).toBeDefined()
        expect(colors[entry.backgroundToken]).toBeDefined()
      }
    }
  })
})