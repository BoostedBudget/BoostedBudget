import AsyncStorage from '@react-native-async-storage/async-storage'
import { afterEach, beforeEach } from '@jest/globals'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react-native'
import { readFileSync, readdirSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import type { ReactNode } from 'react'
import { Appearance, Platform, Pressable } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { VariableContextProvider } from 'nativewind'
import RootLayout, { ThemedApp } from '../../app/_layout'
import { ThemeProvider as ThemePreferenceProvider, useTheme } from '../theme/ThemeProvider'

jest.mock('nativewind', () => ({
  VariableContextProvider: jest.fn(({ children }: { children?: ReactNode }) => children ?? null),
}))

const VariableContextProviderMock = VariableContextProvider as unknown as jest.Mock

const MOBILE_ROOT = dirname(require.resolve('../../package.json'))
const GREPPED_DIRS = [resolve(MOBILE_ROOT, 'app'), resolve(MOBILE_ROOT, 'src/components')]

function collectSourceFiles(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const full = resolve(dir, entry.name)
    if (entry.isDirectory()) return collectSourceFiles(full)
    return /\.(ts|tsx)$/.test(entry.name) ? [full] : []
  })
}

function lastVariableContextValue(): Record<string, string> {
  const calls = VariableContextProviderMock.mock.calls
  return calls[calls.length - 1]?.[0].value as Record<string, string>
}

describe('app-wide theme wiring', () => {
  beforeEach(() => {
    VariableContextProviderMock.mockClear()
  })

  afterEach(() => {
    jest.restoreAllMocks()
  })

  it('passes the resolved theme variables to VariableContextProvider on native', async () => {
    render(<RootLayout />)
    await act(async () => {})

    expect(VariableContextProviderMock).toHaveBeenCalled()
    const value = lastVariableContextValue()
    expect(value['--background']).toBe(nativeTokens.light.color.background)
    expect(value['--bb-info-text']).toBe(nativeTokens.light.contrast['bb-info-text'])
  })

  it('calls Appearance.setColorScheme with the resolved theme on native', async () => {
    const spy = jest.spyOn(Appearance, 'setColorScheme')

    render(<RootLayout />)
    await act(async () => {})

    expect(spy).toHaveBeenCalledWith('light')
  })

  it('writes the resolved theme onto document.documentElement and skips VariableContextProvider on web', async () => {
    const setProperty = jest.fn()
    const setAttribute = jest.fn()
    ;(globalThis as unknown as { document: unknown }).document = {
      documentElement: { style: { setProperty }, setAttribute },
    }
    const platform = jest.replaceProperty(Platform, 'OS', 'web')
    const appearanceSpy = jest.spyOn(Appearance, 'setColorScheme')

    try {
      render(<RootLayout />)
      await act(async () => {})

      expect(setProperty).toHaveBeenCalledWith('--background', nativeTokens.light.color.background)
      expect(setAttribute).toHaveBeenCalledWith('data-bb-theme', 'light')
      expect(VariableContextProviderMock).not.toHaveBeenCalled()
      expect(appearanceSpy).not.toHaveBeenCalled()
    } finally {
      platform.restore()
      delete (globalThis as unknown as { document?: unknown }).document
    }
  })

  it('updates document.documentElement when the preference changes on web', async () => {
    const setProperty = jest.fn()
    const setAttribute = jest.fn()
    ;(globalThis as unknown as { document: unknown }).document = {
      documentElement: { style: { setProperty }, setAttribute },
    }
    const platform = jest.replaceProperty(Platform, 'OS', 'web')

    function PreferenceSwitcher() {
      const { setPreference } = useTheme()
      return <Pressable testID="set-dark" onPress={() => setPreference('dark')} />
    }

    try {
      render(
        <ThemePreferenceProvider>
          <ThemedApp>
            <PreferenceSwitcher />
          </ThemedApp>
        </ThemePreferenceProvider>,
      )
      await act(async () => {})
      setProperty.mockClear()
      setAttribute.mockClear()

      await act(async () => {
        fireEvent.press(screen.getByTestId('set-dark'))
      })

      expect(setProperty).toHaveBeenCalledWith('--background', nativeTokens.dark.color.background)
      expect(setAttribute).toHaveBeenCalledWith('data-bb-theme', 'dark')
    } finally {
      platform.restore()
      delete (globalThis as unknown as { document?: unknown }).document
    }
  })

  it('never calls useColorScheme in a screen or a primitive', () => {
    const offenders = GREPPED_DIRS.flatMap(collectSourceFiles).filter((file) =>
      readFileSync(file, 'utf8').includes('useColorScheme'),
    )

    expect(offenders).toEqual([])
  })

  it('does not write a light theme before hydration when the persisted preference is dark', async () => {
    // The pre-paint bootstrap already applied the PERSISTED preference (dark) before first paint.
    // The runtime must not clobber it with the still-`system` resolution (light) while AsyncStorage
    // is unresolved, or the user sees a dark -> light -> dark flash.
    await AsyncStorage.setItem('bb-theme', 'dark')
    const setProperty = jest.fn()
    const setAttribute = jest.fn()
    ;(globalThis as unknown as { document: unknown }).document = {
      documentElement: { style: { setProperty }, setAttribute },
    }
    const platform = jest.replaceProperty(Platform, 'OS', 'web')

    try {
      render(<RootLayout />)
      await waitFor(() => expect(setAttribute).toHaveBeenCalledWith('data-bb-theme', 'dark'))

      const backgroundWrites = setProperty.mock.calls.filter(([name]) => name === '--background')
      expect(backgroundWrites.length).toBeGreaterThan(0)
      // The FIRST background write must already be the dark token: no light write may precede it.
      expect(backgroundWrites[0][1]).toBe(nativeTokens.dark.color.background)
      expect(backgroundWrites.map(([, value]) => value)).not.toContain(
        nativeTokens.light.color.background,
      )
    } finally {
      platform.restore()
      delete (globalThis as unknown as { document?: unknown }).document
      await AsyncStorage.clear()
    }
  })

  it('wires the generated exclude list into metro inlineVariables', () => {
    const metroSource = readFileSync(resolve(MOBILE_ROOT, 'metro.config.js'), 'utf8')
    expect(metroSource).toContain('nativewind-exclude.json')
    expect(metroSource).toContain('inlineVariables')
    expect(metroSource).toContain('exclude')

    const exclude = require('@boostedbudget/design-tokens/nativewind-exclude.json') as string[]
    const normalise = (name: string) => name.replace(/^--/, '')
    const adapterKeys = [
      ...Object.keys(nativeTokens.light.color),
      ...Object.keys(nativeTokens.light.contrast),
    ]
    expect(exclude.map(normalise).sort()).toEqual(adapterKeys.map(normalise).sort())
  })
})