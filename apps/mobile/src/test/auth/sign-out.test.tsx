import { afterEach, beforeEach, describe, expect, it } from '@jest/globals'
import { act, fireEvent, render, screen } from '@testing-library/react-native'
import { router } from 'expo-router'
import { ReactNode } from 'react'
import {
  AXIOS_INSTANCE,
  configureApiClient,
  customInstance,
} from '@boostedbudget/api-client/client'
import { mockClerk, resetMockClerkAuth, setMockClerkAuth } from '../../../jest.setup'
import DashboardScreen from '../../../app/(app)/dashboard'
import { AuthProviders } from '../../auth/AuthProviders'
import { MobileAuthProvider } from '../../auth/mobile-auth'
import {
  recover,
  setApiErrorReporter,
  setAuthRecoveryHandler,
} from '../../bootstrap/api-runtime'

/**
 * Todo 20 (#1657): the dashboard sign-out control.
 *
 * The control is driven through the REAL provider tree (`MobileAuthProvider` → `AuthProviders` →
 * `DashboardScreen`) and the Clerk mock, so the `signOut` the button calls is the adapter's own
 * `clerk.signOut()` and the token provider is the one `AuthProviders` registered on the shared
 * client. Three properties are pinned:
 *
 *  1. the click signs out through the adapter, clears the recovery handler, and navigates to
 *     `(auth)`;
 *  2. a 401 that lands AFTER sign-out dispatches nothing (the handler is cleared, so `recover` is
 *     a no-op) — this is the mechanism, because the shared client's `hasEverAuthenticated` is only
 *     reset by `configureApiClient`, which the app calls exactly once;
 *  3. no stale token is attached to a request issued after sign-out.
 *
 * `mobile-auth.clerk.tsx` imports `useSSO` from the experimental subpath; the global setup mock
 * only covers `@clerk/expo`, so the experimental hook is mocked here (as in auth-providers.test).
 */
jest.mock('../../theme/ThemeProvider', () => ({
  ThemeProvider: ({ children }: { children: ReactNode }) => children,
  useTheme: () => ({
    preference: 'system',
    resolvedTheme: 'light',
    setPreference: jest.fn(),
    hydrated: true,
  }),
}))

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

const originalAdapter = AXIOS_INSTANCE.defaults.adapter
let lastAuthorization: unknown

/** A recording adapter: it captures the Authorization header and never touches the network. */
function recordRequests() {
  AXIOS_INSTANCE.defaults.adapter = async (config) => {
    lastAuthorization = (config.headers as unknown as { Authorization?: string } | undefined)
      ?.Authorization
    return { data: null, status: 200, statusText: 'OK', headers: {}, config }
  }
}

function tree() {
  return (
    <MobileAuthProvider authAvailable publishableKey="pk_test_abc123">
      <AuthProviders>
        <DashboardScreen />
      </AuthProviders>
    </MobileAuthProvider>
  )
}

/**
 * A signed-in Clerk session whose `signOut` also clears the in-memory auth state — the real
 * Clerk `signOut` drops the session, so `getToken` resolves null afterwards. The mock mutation is
 * what a real Clerk state change would deliver on the next render.
 */
function signInWithToken(token: string) {
  setMockClerkAuth({ isLoaded: true, isSignedIn: true, userId: 'user_1' })
  mockClerk().auth.getToken.mockResolvedValue(token)
  mockClerk().clerk.signOut.mockImplementation(async () => {
    setMockClerkAuth({ isSignedIn: false, userId: null })
    mockClerk().auth.getToken.mockResolvedValue(null)
  })
}

async function renderSignedIn(token = 'stale-token') {
  signInWithToken(token)
  const view = render(tree())
  // Flush the `isLoaded` effect that registers the token provider on the shared client.
  await act(async () => {})
  return view
}

async function pressSignOut() {
  await act(async () => {
    fireEvent.press(screen.getByTestId('dashboard-sign-out'))
  })
}

beforeEach(() => {
  jest.clearAllMocks()
  resetMockClerkAuth()
  setAuthRecoveryHandler(null)
  setApiErrorReporter(null)
  lastAuthorization = undefined
  recordRequests()
})

afterEach(() => {
  setAuthRecoveryHandler(null)
  setApiErrorReporter(null)
  AXIOS_INSTANCE.defaults.adapter = originalAdapter
  // Reset the shared client's runtime state (a test-only reset; the app calls this once).
  configureApiClient()
})

describe('dashboard sign-out', () => {
  it('signs out through the adapter, clears the recovery handler, and navigates to (auth)', async () => {
    await renderSignedIn()

    await pressSignOut()

    expect(mockClerk().clerk.signOut).toHaveBeenCalledTimes(1)
    expect(router.replace).toHaveBeenCalledWith('/(auth)/sign-in')

    // The explicit clear makes a 401 arriving after sign-out a no-op. Without it the handler
    // `AuthProviders` registered while signed in would sign out a second time and re-navigate.
    recover({ status: 401, url: '/api/v1/accounts' })

    expect(mockClerk().clerk.signOut).toHaveBeenCalledTimes(1)
    expect(router.replace).toHaveBeenCalledTimes(1)
  })

  it('attaches no stale token to a request issued after sign-out', async () => {
    await renderSignedIn('stale-token')

    await customInstance({ url: '/api/v1/accounts' })
    expect(lastAuthorization).toBe('Bearer stale-token')

    await pressSignOut()
    lastAuthorization = undefined

    await customInstance({ url: '/api/v1/accounts' })
    expect(lastAuthorization).toBeUndefined()
  })

  it('clears the in-memory user identity after sign-out', async () => {
    const view = await renderSignedIn()
    expect(screen.getByTestId('dashboard-identity')).toHaveTextContent('user_1')

    await pressSignOut()
    view.rerender(tree())
    await act(async () => {})

    expect(screen.getByTestId('dashboard-identity')).toHaveTextContent('Signed in')
  })
})
