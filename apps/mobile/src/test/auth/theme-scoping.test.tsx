import AsyncStorage from '@react-native-async-storage/async-storage'
import { beforeEach } from '@jest/globals'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react-native'
import { runInNewContext } from 'node:vm'
import { Text } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { resetMockClerkAuth, setMockClerkAuth } from '../../../jest.setup'
import { AuthProviders } from '../../auth/AuthProviders'
import { MobileAuthProvider } from '../../auth/mobile-auth'
import { buildThemeBootstrapScript } from '../../theme/bootstrap-script'
import { useTheme } from '../../theme/ThemeProvider'

/**
 * Todo 19: the theme preference is keyed by the signed-in Clerk user.
 *
 * These tests drive the REAL `AuthProviders` + `ThemeProvider` through the Clerk mock, so the
 * `userId` that reaches `themeStorageKey` is the one the adapter actually reports. They pin the
 * two key namespaces (`bb-theme` and `bb-theme:<userId>`) against cross-writing, the re-read when
 * the user changes, and the pre-paint bootstrap's continued read of the bare key.
 *
 * `mobile-auth.clerk.tsx` imports `useSSO` from the experimental subpath; the global setup mock
 * only covers `@clerk/expo`, so the experimental hook is mocked here (as in auth-providers.test).
 */
jest.mock('@clerk/expo/experimental', () => ({
  useSSO: () => ({
    startSSOFlow: jest.fn(async () => ({
      createdSessionId: null,
      authSessionResult: null,
      signIn: null,
      signUp: null,
    })),
  }),
}))

const BARE_KEY = 'bb-theme'
const USER_KEY = 'bb-theme:user_1'

function ThemeProbe() {
  const { preference, hydrated, setPreference } = useTheme()

  return (
    <>
      <Text testID="preference">{preference}</Text>
      <Text testID="hydrated">{String(hydrated)}</Text>
      <Text testID="set-dark" onPress={() => setPreference('dark')}>
        set dark
      </Text>
      <Text testID="set-light" onPress={() => setPreference('light')}>
        set light
      </Text>
    </>
  )
}

function tree() {
  return (
    <MobileAuthProvider authAvailable publishableKey="pk_test_abc123">
      <AuthProviders>
        <ThemeProbe />
      </AuthProviders>
    </MobileAuthProvider>
  )
}

function renderSignedIn(userId: string | null) {
  setMockClerkAuth({ isLoaded: true, isSignedIn: userId !== null, userId })
  return render(tree())
}

async function waitForHydration() {
  await waitFor(() => expect(screen.getByTestId('hydrated')).toHaveTextContent('true'))
}

async function press(testID: 'set-dark' | 'set-light') {
  await act(async () => {
    fireEvent.press(screen.getByTestId(testID))
  })
}

beforeEach(async () => {
  await AsyncStorage.clear()
  jest.clearAllMocks()
  resetMockClerkAuth()
})

describe('theme preference scoping', () => {
  it('stores a signed-in preference under the user-scoped key only', async () => {
    await AsyncStorage.setItem(BARE_KEY, 'light')

    renderSignedIn('user_1')
    await waitForHydration()
    await press('set-dark')

    await waitFor(() => expect(AsyncStorage.setItem).toHaveBeenCalledWith(USER_KEY, 'dark'))
    expect(AsyncStorage.setItem).not.toHaveBeenCalledWith(BARE_KEY, 'dark')
    await expect(AsyncStorage.getItem(USER_KEY)).resolves.toBe('dark')
    // The signed-out namespace is untouched by a signed-in write.
    await expect(AsyncStorage.getItem(BARE_KEY)).resolves.toBe('light')
  })

  it('stores a signed-out preference under the bare key only', async () => {
    renderSignedIn(null)
    await waitForHydration()
    await press('set-dark')

    await waitFor(() => expect(AsyncStorage.setItem).toHaveBeenCalledWith(BARE_KEY, 'dark'))
    expect(AsyncStorage.setItem).not.toHaveBeenCalledWith(USER_KEY, 'dark')
    await expect(AsyncStorage.getItem(BARE_KEY)).resolves.toBe('dark')
    await expect(AsyncStorage.getItem(USER_KEY)).resolves.toBeNull()
  })

  it('stores an inert preference under the bare key only', async () => {
    render(
      <MobileAuthProvider authAvailable={false}>
        <AuthProviders>
          <ThemeProbe />
        </AuthProviders>
      </MobileAuthProvider>,
    )
    await waitForHydration()
    await press('set-dark')

    await waitFor(() => expect(AsyncStorage.setItem).toHaveBeenCalledWith(BARE_KEY, 'dark'))
    expect(AsyncStorage.setItem).not.toHaveBeenCalledWith(USER_KEY, 'dark')
    await expect(AsyncStorage.getItem(USER_KEY)).resolves.toBeNull()
  })

  it('never cross-writes between the two key namespaces', async () => {
    // Signed-in write: only the user-scoped key may be written.
    const view = renderSignedIn('user_1')
    await waitForHydration()
    await press('set-dark')
    await waitFor(() => expect(AsyncStorage.setItem).toHaveBeenCalledWith(USER_KEY, 'dark'))
    expect(AsyncStorage.setItem).not.toHaveBeenCalledWith(BARE_KEY, 'dark')

    // Signed-out write: only the bare key may be written. The rerender is required — the
    // `setPreference` callback closes over the storage key of the render that produced it.
    jest.clearAllMocks()
    setMockClerkAuth({ isLoaded: true, isSignedIn: false, userId: null })
    view.rerender(tree())
    await press('set-light')
    await waitFor(() => expect(AsyncStorage.setItem).toHaveBeenCalledWith(BARE_KEY, 'light'))
    expect(AsyncStorage.setItem).not.toHaveBeenCalledWith(USER_KEY, 'light')
  })

  it('re-reads the user-scoped preference when the user signs in', async () => {
    await AsyncStorage.setItem(BARE_KEY, 'light')
    await AsyncStorage.setItem(USER_KEY, 'dark')

    const view = renderSignedIn(null)
    await waitForHydration()
    expect(screen.getByTestId('preference')).toHaveTextContent('light')

    setMockClerkAuth({ isLoaded: true, isSignedIn: true, userId: 'user_1' })
    view.rerender(tree())

    await waitFor(() => expect(screen.getByTestId('preference')).toHaveTextContent('dark'))
  })

  it('re-reads the bare preference when the user signs out', async () => {
    await AsyncStorage.setItem(BARE_KEY, 'light')
    await AsyncStorage.setItem(USER_KEY, 'dark')

    const view = renderSignedIn('user_1')
    await waitForHydration()
    expect(screen.getByTestId('preference')).toHaveTextContent('dark')

    setMockClerkAuth({ isLoaded: true, isSignedIn: false, userId: null })
    view.rerender(tree())

    await waitFor(() => expect(screen.getByTestId('preference')).toHaveTextContent('light'))
  })

  it('keeps the pre-paint bootstrap reading the bare key so a signed-out reload does not flash', () => {
    const script = buildThemeBootstrapScript(nativeTokens)
    const store = new Map<string, string>([[BARE_KEY, 'dark']])
    const localStorage = {
      getItem: (key: string) => store.get(key) ?? null,
      key: (index: number) => Array.from(store.keys())[index] ?? null,
      get length() {
        return store.size
      },
    }
    const setProperty = jest.fn()
    const setAttribute = jest.fn()

    runInNewContext(script, {
      window: { localStorage, matchMedia: () => ({ matches: false }) },
      document: { documentElement: { style: { setProperty }, setAttribute } },
    })

    // The bare key alone drives the pre-paint theme: no scoped key is present.
    expect(setAttribute).toHaveBeenCalledWith('data-bb-theme', 'dark')
    expect(setProperty).toHaveBeenCalledWith('--background', nativeTokens.dark.color.background)
  })
})