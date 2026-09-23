jest.unmock('expo-router')

import { afterEach, beforeEach, describe, expect, it } from '@jest/globals'
import { act } from '@testing-library/react-native'
import { cleanup, renderRouter, screen } from 'expo-router/testing-library'
import { Stack } from 'expo-router'
import MockAdapter from 'axios-mock-adapter'
import type { ReactNode } from 'react'
import { AXIOS_INSTANCE, configureApiClient } from '@boostedbudget/api-client/client'
import { getHealth } from '@boostedbudget/api-client/endpoints/health/health'
import { mockClerk, resetMockClerkAuth, setMockClerkAuth } from '../../../jest.setup'
import AppLayout from '../../../app/(app)/_layout'
import DashboardScreen from '../../../app/(app)/dashboard'
import AuthLayout from '../../../app/(auth)/_layout'
import SignInScreen from '../../../app/(auth)/sign-in'
import { AuthProviders } from '../../auth/AuthProviders'
import { MobileAuthProvider } from '../../auth/mobile-auth'
import { recover, setAuthRecoveryHandler } from '../../bootstrap/api-runtime'

/**
 * Contract-integration tier — routing leg (todo 21, #1657).
 *
 * This file mounts the REAL `(app)` and `(auth)` layouts through `expo-router`'s own navigation
 * (`expo-router/testing-library` + `jest.unmock('expo-router')`, the pattern `guard-states.test.tsx`
 * established) and wraps them in the REAL `MobileAuthProvider` → `AuthProviders` tree. A request is
 * then driven through the REAL `@boostedbudget/api-client` against an `axios-mock-adapter`, so the
 * full chain is exercised end-to-end: token attachment → request interceptor → 401 response
 * interceptor → the `recover` bridge → the adapter's sign-out → a real pathname change.
 *
 * Clerk is mocked at the seam (`jest.setup.ts`); `mobile-auth.clerk.tsx` reads `useSSO` from the
 * experimental subpath, which the global setup does not cover, so it is mocked below.
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

const BASE_URL = 'https://api.example.com'
const HEALTH_URL = `${BASE_URL}/health`

let mock: MockAdapter
const originalAdapter = AXIOS_INSTANCE.defaults.adapter

function RootLayout() {
  return <Stack screenOptions={{ headerShown: false }} />
}

const ROUTES = {
  _layout: RootLayout,
  index: () => null,
  '(app)/_layout': AppLayout,
  '(app)/dashboard': DashboardScreen,
  '(auth)/_layout': AuthLayout,
  '(auth)/sign-in': SignInScreen,
}

function AuthWrapper({ children }: { children: ReactNode }) {
  return (
    <MobileAuthProvider authAvailable publishableKey="pk_test_abc123">
      <AuthProviders>{children}</AuthProviders>
    </MobileAuthProvider>
  )
}

function renderSignedIn() {
  setMockClerkAuth({ isLoaded: true, isSignedIn: true, userId: 'user_1' })
  mockClerk().auth.getToken.mockResolvedValue('tok_abc')
  return renderRouter(ROUTES, { initialUrl: '/dashboard', wrapper: AuthWrapper })
}

beforeEach(() => {
  jest.clearAllMocks()
  resetMockClerkAuth()
  setAuthRecoveryHandler(null)
  configureApiClient({ baseUrl: BASE_URL, onUnauthorized: recover })
  mock = new MockAdapter(AXIOS_INSTANCE)
})

afterEach(() => {
  cleanup()
  setAuthRecoveryHandler(null)
  mock.restore()
  AXIOS_INSTANCE.defaults.adapter = originalAdapter
  configureApiClient()
})

describe('contract-integration: real router over the real client', () => {
  it('renders the guarded dashboard for a signed-in session', async () => {
    const router = renderSignedIn()
    await act(async () => {})

    expect(router.getPathname()).toBe('/dashboard')
    expect(screen.getByTestId('dashboard-screen')).toBeOnTheScreen()
  })

  it('lands on (auth) when a real 401 flows through the client and the recovery handler', async () => {
    const router = renderSignedIn()
    await act(async () => {})

    mock.onGet(HEALTH_URL).reply(401)

    await act(async () => {
      await expect(getHealth()).rejects.toThrow('Request failed with status code 401')
    })
    await act(async () => {})

    expect(router.getPathname()).toBe('/sign-in')
    expect(screen.getByTestId('sign-in-screen')).toBeOnTheScreen()
    expect(screen.queryByTestId('dashboard-screen')).not.toBeOnTheScreen()
  })
})
