import { afterEach } from '@jest/globals'
import { act, fireEvent, render, screen } from '@testing-library/react-native'
import { readFileSync, readdirSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import type { ReactElement } from 'react'
import * as RN from 'react-native'
import { SafeAreaProvider } from 'react-native-safe-area-context'
import { nativeNavigationTokens } from '@boostedbudget/design-tokens'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { AppBar } from '../../components/ui/AppBar'
import { BackButton } from '../../components/ui/BackButton'
import { NAVIGATION_REGISTRY } from '../../components/ui/navigation.registry'
import { Screen } from '../../components/ui/Screen'
import { ScreenHeader } from '../../components/ui/ScreenHeader'
import { TabBar, type TabBarItem } from '../../components/ui/TabBar'
import { Text } from '../../components/ui/Text'
import { ThemeProvider } from '../../theme/ThemeProvider'
import { COLOR_SCHEMES, allowedColors, collectHexColors, resolvedColor, resolvedStyle } from './helpers'

const TOUCH_MIN = nativeTokens.light.touchTarget.min
const INSETS = { top: 47, right: 0, bottom: 34, left: 0 }
const FRAME = { x: 0, y: 0, width: 390, height: 844 }

const MOBILE_ROOT = dirname(require.resolve('../../../package.json'))
const UI_DIR = resolve(MOBILE_ROOT, 'src/components/ui')
const UI_FILES = readdirSync(UI_DIR).filter((file) => file.endsWith('.ts') || file.endsWith('.tsx'))

afterEach(() => {
  jest.restoreAllMocks()
})

/**
 * The navigation primitives resolve their chrome colours from `nativeNavigationTokens` for the
 * app's resolved theme. `ThemeProvider` derives that from `useColorScheme()`, which jest-expo
 * leaves at `null` regardless of `Appearance.setColorScheme`, so the scheme is injected here.
 * The async AsyncStorage hydration is flushed inside `act` to keep the output warning-free.
 */
async function renderWithTheme(node: ReactElement, scheme: 'light' | 'dark') {
  jest.spyOn(RN, 'useColorScheme').mockReturnValue(scheme)
  const result = render(<ThemeProvider>{node}</ThemeProvider>)
  await act(async () => {})
  return result
}

function renderWithInsets(node: ReactElement, scheme: 'light' | 'dark' = 'light') {
  jest.spyOn(RN, 'useColorScheme').mockReturnValue(scheme)
  return render(
    <ThemeProvider>
      <SafeAreaProvider initialMetrics={{ frame: FRAME, insets: INSETS }}>{node}</SafeAreaProvider>
    </ThemeProvider>,
  )
}

const TAB_ITEMS: TabBarItem[] = [
  { key: 'home', label: 'Home', selected: true },
  { key: 'budgets', label: 'Budgets' },
  { key: 'settings', label: 'Settings', disabled: true },
]

describe('ScreenHeader', () => {
  it('renders its title as a header role', async () => {
    await renderWithTheme(<ScreenHeader title="Home" />, 'light')

    const title = screen.getByTestId('screen-header-title')
    expect(title.props.accessibilityRole).toBe('header')
    expect(screen.getByText('Home')).toBeTruthy()
  })

  it('renders a back button only when onBack is provided', async () => {
    const { rerender } = await renderWithTheme(<ScreenHeader title="Home" />, 'light')
    expect(screen.queryByTestId('screen-header-back')).toBeNull()

    rerender(
      <ThemeProvider>
        <ScreenHeader title="Home" onBack={() => {}} />
      </ThemeProvider>,
    )
    expect(screen.getByTestId('screen-header-back')).toBeTruthy()
  })

  it('invokes onBack through the back button', async () => {
    const onBack = jest.fn()
    await renderWithTheme(<ScreenHeader title="Home" onBack={onBack} />, 'light')

    fireEvent.press(screen.getByTestId('screen-header-back'))
    expect(onBack).toHaveBeenCalledTimes(1)
  })

  it('renders the actions slot', async () => {
    await renderWithTheme(
      <ScreenHeader title="Home" actions={<Text testID="header-action">Edit</Text>} />,
      'light',
    )

    expect(screen.getByTestId('screen-header-actions')).toBeTruthy()
    expect(screen.getByTestId('header-action')).toBeTruthy()
  })

  it('renders a larger title in the large variant', async () => {
    const { unmount } = await renderWithTheme(<ScreenHeader title="Home" />, 'light')
    const regular = resolvedStyle('screen-header-title').fontSize as number

    unmount()
    await renderWithTheme(<ScreenHeader title="Home" large />, 'light')
    const large = resolvedStyle('screen-header-title').fontSize as number

    expect(large).toBeGreaterThan(regular)
  })

  it('exposes AppBar as the same header primitive', async () => {
    await renderWithTheme(<AppBar title="Home" />, 'light')

    expect(screen.getByTestId('screen-header-title').props.accessibilityRole).toBe('header')
  })
})

describe('BackButton', () => {
  it('has the accessible name "Go back" by default', async () => {
    await renderWithTheme(<BackButton onPress={() => {}} />, 'light')

    const button = screen.getByTestId('back-button')
    expect(button.props.accessibilityRole).toBe('button')
    expect(button.props.accessibilityLabel).toBe('Go back')
  })

  it('accepts a custom accessible name', async () => {
    await renderWithTheme(<BackButton onPress={() => {}} label="Back to budgets" />, 'light')

    expect(screen.getByTestId('back-button').props.accessibilityLabel).toBe('Back to budgets')
  })

  it('invokes the injected callback', async () => {
    const onPress = jest.fn()
    await renderWithTheme(<BackButton onPress={onPress} />, 'light')

    fireEvent.press(screen.getByTestId('back-button'))
    expect(onPress).toHaveBeenCalledTimes(1)
  })

  it('meets the touch floor', async () => {
    await renderWithTheme(<BackButton onPress={() => {}} />, 'light')

    const style = resolvedStyle('back-button')
    expect(style.minHeight as number).toBeGreaterThanOrEqual(TOUCH_MIN)
    expect(style.minWidth as number).toBeGreaterThanOrEqual(TOUCH_MIN)
  })

  it('shows a visible focus indicator when focused', async () => {
    await renderWithTheme(<BackButton onPress={() => {}} />, 'light')

    expect(resolvedStyle('back-button').outlineWidth).toBeUndefined()

    fireEvent(screen.getByTestId('back-button'), 'focus')
    expect(resolvedStyle('back-button').outlineWidth).toBe(
      nativeTokens.light.focus['bb-focus-ring-width'],
    )
    expect(resolvedColor('back-button', 'outlineColor')).toBe(
      nativeTokens.light.focus['bb-focus-ring-color'],
    )
  })
})

describe('TabBar', () => {
  it('exposes tab role and selected state on every item', async () => {
    await renderWithTheme(<TabBar items={TAB_ITEMS} />, 'light')

    const home = screen.getByTestId('tab-bar-item-home')
    expect(home.props.accessibilityRole).toBe('tab')
    expect(home.props.accessibilityState).toMatchObject({ selected: true })
    expect(home.props.accessibilityLabel).toBe('Home')

    const budgets = screen.getByTestId('tab-bar-item-budgets')
    expect(budgets.props.accessibilityRole).toBe('tab')
    expect(budgets.props.accessibilityState).toMatchObject({ selected: false })
  })

  it('meets the touch floor on every item', async () => {
    await renderWithTheme(<TabBar items={TAB_ITEMS} />, 'light')

    for (const key of ['home', 'budgets', 'settings']) {
      const style = resolvedStyle(`tab-bar-item-${key}`)
      expect(style.minHeight as number).toBeGreaterThanOrEqual(TOUCH_MIN)
      expect(style.minWidth as number).toBeGreaterThanOrEqual(TOUCH_MIN)
    }
  })

  it('invokes onSelect and the item handler when pressed', async () => {
    const onSelect = jest.fn()
    const onPress = jest.fn()
    await renderWithTheme(
      <TabBar items={[{ key: 'home', label: 'Home', onPress }]} onSelect={onSelect} />,
      'light',
    )

    fireEvent.press(screen.getByTestId('tab-bar-item-home'))
    expect(onPress).toHaveBeenCalledTimes(1)
    expect(onSelect).toHaveBeenCalledWith('home')
  })

  it('does not invoke a disabled item', async () => {
    const onSelect = jest.fn()
    await renderWithTheme(<TabBar items={TAB_ITEMS} onSelect={onSelect} />, 'light')

    fireEvent.press(screen.getByTestId('tab-bar-item-settings'))
    expect(onSelect).not.toHaveBeenCalled()
  })

  it('shows a visible focus indicator when focused', async () => {
    await renderWithTheme(<TabBar items={TAB_ITEMS} />, 'light')

    fireEvent(screen.getByTestId('tab-bar-item-home'), 'focus')
    expect(resolvedStyle('tab-bar-item-home').outlineWidth).toBe(
      nativeTokens.light.focus['bb-focus-ring-width'],
    )
  })
})

describe('Screen', () => {
  it('renders the header and its children', async () => {
    await renderWithTheme(
      <Screen header={<ScreenHeader title="Home" />}>
        <Text testID="screen-body">Body</Text>
      </Screen>,
      'light',
    )

    expect(screen.getByTestId('screen-header-title')).toBeTruthy()
    expect(screen.getByTestId('screen-body')).toBeTruthy()
  })

  it('renders a ScrollView only when scrollable', async () => {
    const { rerender } = await renderWithTheme(
      <Screen>
        <Text>Body</Text>
      </Screen>,
      'light',
    )
    expect(screen.queryByTestId('screen-scroll')).toBeNull()
    expect(screen.getByTestId('screen-content')).toBeTruthy()

    rerender(
      <ThemeProvider>
        <Screen scrollable>
          <Text>Body</Text>
        </Screen>
      </ThemeProvider>,
    )
    expect(screen.getByTestId('screen-scroll')).toBeTruthy()
  })

  it('applies the safe-area insets', () => {
    renderWithInsets(
      <Screen header={<ScreenHeader title="Home" />}>
        <Text>Body</Text>
      </Screen>,
    )

    expect(resolvedStyle('screen').paddingTop).toBe(INSETS.top)
    expect(resolvedStyle('screen-content').paddingBottom).toBe(INSETS.bottom)
  })
})

describe('navigation chrome colours', () => {
  it.each(COLOR_SCHEMES)('derives every chrome colour from nativeNavigationTokens (%s)', async (scheme) => {
    const nav = nativeNavigationTokens[scheme]
    await renderWithTheme(
      <Screen header={<ScreenHeader title="Home" onBack={() => {}} />}>
        <TabBar items={TAB_ITEMS} />
      </Screen>,
      scheme,
    )

    expect(resolvedColor('screen', 'backgroundColor')).toBe(nav.background)
    expect(resolvedColor('screen-header', 'backgroundColor')).toBe(nav.card)
    expect(resolvedColor('screen-header', 'borderBottomColor')).toBe(nav.border)
    expect(resolvedColor('screen-header-title', 'color')).toBe(nav.text)
    expect(resolvedColor('screen-header-back-glyph', 'color')).toBe(nav.text)
    expect(resolvedColor('tab-bar', 'backgroundColor')).toBe(nav.card)
    expect(resolvedColor('tab-bar', 'borderTopColor')).toBe(nav.border)
    expect(resolvedColor('tab-bar-item-home-label', 'color')).toBe(nav.primary)
    expect(resolvedColor('tab-bar-item-budgets-label', 'color')).toBe(nav.text)
  })

  it.each(COLOR_SCHEMES)('renders only token colours (%s)', async (scheme) => {
    await renderWithTheme(
      <Screen header={<ScreenHeader title="Home" onBack={() => {}} />}>
        <TabBar items={TAB_ITEMS} />
      </Screen>,
      scheme,
    )

    const rendered = [
      'screen',
      'screen-header',
      'screen-header-title',
      'screen-header-back',
      'screen-header-back-glyph',
      'tab-bar',
      'tab-bar-item-home',
      'tab-bar-item-home-label',
      'tab-bar-item-budgets-label',
    ].flatMap((id) => collectHexColors(resolvedStyle(id)))

    // `allowedColors()` renders its own token probe, so it must run AFTER the styles are read.
    const allowed = allowedColors()

    expect(rendered.length).toBeGreaterThan(0)
    for (const color of rendered) expect(allowed.has(color)).toBe(true)
  })
})

describe('components/ui source bans', () => {
  it('contains the navigation primitive files (the ban scan is not vacuous)', () => {
    expect(UI_FILES).toEqual(
      expect.arrayContaining([
        'Screen.tsx',
        'ScreenHeader.tsx',
        'AppBar.tsx',
        'BackButton.tsx',
        'TabBar.tsx',
        'navigation.registry.ts',
      ]),
    )
  })

  it('never imports expo-router inside a primitive', () => {
    const offenders: string[] = []
    for (const file of UI_FILES) {
      const source = readFileSync(resolve(UI_DIR, file), 'utf8')
      if (source.includes('expo-router')) offenders.push(file)
    }
    expect(offenders).toEqual([])
  })

  it('never uses a `dark:` utility', () => {
    const offenders: string[] = []
    for (const file of UI_FILES) {
      const source = readFileSync(resolve(UI_DIR, file), 'utf8')
      if (source.includes('dark:')) offenders.push(file)
    }
    expect(offenders).toEqual([])
  })
})

describe('NAVIGATION_REGISTRY', () => {
  it('has one entry per text-bearing navigation role', () => {
    const names = NAVIGATION_REGISTRY.map((entry) => entry.name)
    for (const name of ['ScreenHeader.title', 'TabBar.label', 'BackButton.label']) {
      expect(names).toContain(name)
    }
  })

  it('resolves every foreground/background token in both themes', () => {
    for (const entry of NAVIGATION_REGISTRY) {
      for (const scheme of COLOR_SCHEMES) {
        const section = { ...nativeTokens[scheme].color, ...nativeTokens[scheme].contrast } as Record<
          string,
          string
        >
        expect(section[entry.foregroundToken]).toBeDefined()
        expect(section[entry.backgroundToken]).toBeDefined()
      }
    }
  })
})

describe('TabBar types', () => {
  it('requires a text label on every tab item', () => {
    // @ts-expect-error a tab item without a text label must not compile
    const item: TabBarItem = { key: 'home' }
    expect(item).toBeDefined()
  })
})