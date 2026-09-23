import { act, render, screen } from '@testing-library/react-native'
import { beforeEach } from '@jest/globals'
import { ReactNode } from 'react'
import { Text } from 'react-native'
import { resetMockClerkAuth, setMockClerkAuth } from '../../../jest.setup'
import { AuthProviders } from '../../auth/AuthProviders'
import { MobileAuthProvider, inertMobileAuthContextValue } from '../../auth/mobile-auth'
import { useToast } from '../../components/ui/ToastProvider'

/**
 * `AuthProviders` is the bridge between the auth adapter and the rest of the provider tree. These
 * tests pin its four responsibilities: user-scoped theme preference, the `isLoaded` gate, the
 * toast provider, and the two inert registration seams (todos 10 and 11).
 *
 * The theme provider is mocked so the `userId` prop can be asserted directly; the real provider's
 * storage-key behaviour is covered by `theme-provider.test.tsx` and `theme-wiring.test.tsx`.
 */
jest.mock('../../theme/ThemeProvider', () => ({
  ThemeProvider: jest.fn(({ children }: { children: ReactNode }) => children),
}))

// `mobile-auth.clerk.tsx` imports `useSSO` from the experimental subpath; the global setup mock
// only covers `@clerk/expo`, so the experimental hook is mocked here.
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

const ThemeProviderMock = jest.requireMock('../../theme/ThemeProvider').ThemeProvider as jest.Mock

function lastThemeProviderProps(): { userId?: string } {
  const calls = ThemeProviderMock.mock.calls
  return calls[calls.length - 1]?.[0]
}

function Probe() {
  return <Text>probe-child</Text>
}

function ToastProbe() {
  const { show } = useToast()
  return <Text>{typeof show === 'function' ? 'toast-ready' : 'toast-missing'}</Text>
}

function renderWithClerk(children: ReactNode) {
  return render(
    <MobileAuthProvider authAvailable publishableKey="pk_test_abc123">
      <AuthProviders>{children}</AuthProviders>
    </MobileAuthProvider>,
  )
}

beforeEach(() => {
  jest.clearAllMocks()
  resetMockClerkAuth()
})

describe('AuthProviders', () => {
  it('passes the signed-in userId to the theme provider', async () => {
    setMockClerkAuth({ isSignedIn: true, userId: 'user_1' })

    renderWithClerk(<Probe />)
    await act(async () => {})

    expect(lastThemeProviderProps().userId).toBe('user_1')
  })

  it('passes undefined to the theme provider when signed out', async () => {
    setMockClerkAuth({ isSignedIn: false, userId: null })

    renderWithClerk(<Probe />)
    await act(async () => {})

    expect(lastThemeProviderProps().userId).toBeUndefined()
  })

  it('passes undefined to the theme provider in inert mode', async () => {
    render(
      <MobileAuthProvider authAvailable={false}>
        <AuthProviders>
          <Probe />
        </AuthProviders>
      </MobileAuthProvider>,
    )
    await act(async () => {})

    expect(lastThemeProviderProps().userId).toBeUndefined()
  })

  it('does not render children before the adapter is loaded', async () => {
    setMockClerkAuth({ isLoaded: false })

    renderWithClerk(<Probe />)
    await act(async () => {})

    expect(screen.queryByText('probe-child')).not.toBeOnTheScreen()
  })

  it('renders children once the adapter is loaded', async () => {
    setMockClerkAuth({ isLoaded: true })

    renderWithClerk(<Probe />)
    await act(async () => {})

    expect(screen.getByText('probe-child')).toBeOnTheScreen()
  })

  it('renders ToastProvider around the children', async () => {
    render(
      <MobileAuthProvider authAvailable={false}>
        <AuthProviders>
          <ToastProbe />
        </AuthProviders>
      </MobileAuthProvider>,
    )
    await act(async () => {})

    expect(screen.getByText('toast-ready')).toBeOnTheScreen()
  })

  it('exposes the token-provider seam with the adapter getToken', async () => {
    const onTokenProviderReady = jest.fn()

    render(
      <MobileAuthProvider authAvailable={false}>
        <AuthProviders onTokenProviderReady={onTokenProviderReady}>
          <Probe />
        </AuthProviders>
      </MobileAuthProvider>,
    )
    await act(async () => {})

    expect(onTokenProviderReady).toHaveBeenCalledWith(inertMobileAuthContextValue.auth.getToken)
  })

  it('exposes the recovery seam with the adapter sign-out surface', async () => {
    const onRecoveryReady = jest.fn()

    render(
      <MobileAuthProvider authAvailable={false}>
        <AuthProviders onRecoveryReady={onRecoveryReady}>
          <Probe />
        </AuthProviders>
      </MobileAuthProvider>,
    )
    await act(async () => {})

    expect(onRecoveryReady).toHaveBeenCalledWith(
      expect.objectContaining({
        isSignedIn: false,
        signOut: inertMobileAuthContextValue.auth.signOut,
        reportTokenError: inertMobileAuthContextValue.auth.reportTokenError,
      }),
    )
  })
})