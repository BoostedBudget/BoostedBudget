import { act, render } from '@testing-library/react-native'
import { afterEach, beforeEach } from '@jest/globals'
import { createElement, ReactNode } from 'react'
import { Text } from 'react-native'
import { router } from 'expo-router'
import { mockClerk, resetMockClerkAuth, setMockClerkAuth } from '../../../jest.setup'
import { AuthProviders } from '../../auth/AuthProviders'
import { MobileAuthProvider, reportTokenError } from '../../auth/mobile-auth'
import {
  recover,
  reportApiError,
  setApiErrorReporter,
  setAuthRecoveryHandler,
} from '../../bootstrap/api-runtime'
import { initializeMobileRuntime } from '../../bootstrap/runtime'

/**
 * Todo 11 (#1657): 401 recovery through the module-level handler.
 *
 * The shared API client calls `onUnauthorized` only for a non-public path once a request has
 * carried a token (`hasEverAuthenticated`). That flag is only reset by `configureApiClient`, which
 * the mobile app calls exactly once, so it cannot be used to stop post-sign-out recovery. The
 * one-shot `recover` (clear-before-invoke) plus the `isSignedIn`-gated registration in
 * `AuthProviders` is the mechanism that prevents a stale handler from firing after sign-out.
 *
 * This file is `.ts` (no JSX); the provider tree is built with `createElement`.
 */

jest.mock('@boostedbudget/api-client/client', () => ({
  setAuthTokenProvider: jest.fn(),
  configureApiClient: jest.fn(),
}))

const clientMock = jest.requireMock('@boostedbudget/api-client/client') as {
  setAuthTokenProvider: jest.Mock
  configureApiClient: jest.Mock
}

jest.mock('../../theme/ThemeProvider', () => ({
  ThemeProvider: ({ children }: { children: ReactNode }) => children,
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

function tree() {
  return createElement(MobileAuthProvider, {
    authAvailable: true,
    publishableKey: 'pk_test_abc123',
    children: createElement(AuthProviders, {
      children: createElement(Text, null, 'probe-child'),
    }),
  })
}

beforeEach(() => {
  jest.clearAllMocks()
  resetMockClerkAuth()
  setAuthRecoveryHandler(null)
  setApiErrorReporter(null)
})

afterEach(() => {
  setAuthRecoveryHandler(null)
  setApiErrorReporter(null)
})

describe('api-runtime recovery handler', () => {
  it('passes the recovery handler and error reporter into a single configureApiClient call', () => {
    initializeMobileRuntime({ EXPO_PUBLIC_API_URL: 'https://api.example.com/' })

    expect(clientMock.configureApiClient).toHaveBeenCalledTimes(1)
    const config = clientMock.configureApiClient.mock.calls[0][0] as {
      onUnauthorized?: unknown
      reportError?: unknown
    }
    expect(config.onUnauthorized).toBe(recover)
    expect(config.reportError).toBe(reportApiError)
  })

  it('invokes the registered handler once, clearing it before the call', () => {
    const handler = jest.fn()
    setAuthRecoveryHandler(handler)

    recover({ status: 401, url: '/api/v1/budgets' })
    recover({ status: 401, url: '/api/v1/budgets' })

    expect(handler).toHaveBeenCalledTimes(1)
    expect(handler).toHaveBeenCalledWith({ status: 401, url: '/api/v1/budgets' })
  })

  it('is a no-op when no handler is registered', () => {
    expect(() => recover({ status: 401 })).not.toThrow()
  })

  it('forwards API error details to the registered reporter', () => {
    const reporter = jest.fn()
    setApiErrorReporter(reporter)

    const details = { status: 500, url: '/api/v1/budgets' }
    reportApiError(details)

    expect(reporter).toHaveBeenCalledTimes(1)
    expect(reporter).toHaveBeenCalledWith(details)
  })

  it('is a no-op when no API error reporter is registered', () => {
    expect(() => reportApiError({ status: 500 })).not.toThrow()
  })
})

describe('AuthProviders 401 recovery lifecycle', () => {
  it('signs out and navigates to the auth group while signed in', async () => {
    setMockClerkAuth({ isSignedIn: true, userId: 'user_1' })

    render(tree())
    await act(async () => {})

    recover({ status: 401 })

    expect(mockClerk().clerk.signOut).toHaveBeenCalledTimes(1)
    expect(router.replace).toHaveBeenCalledWith('/(auth)/sign-in')
  })

  it('does not register a handler while signed out', async () => {
    setMockClerkAuth({ isSignedIn: false, userId: null })

    render(tree())
    await act(async () => {})

    recover({ status: 401 })

    expect(mockClerk().clerk.signOut).not.toHaveBeenCalled()
    expect(router.replace).not.toHaveBeenCalled()
  })

  it('clears the handler when the session signs out, so a later 401 does nothing', async () => {
    setMockClerkAuth({ isSignedIn: true, userId: 'user_1' })
    const { rerender } = render(tree())
    await act(async () => {})

    setMockClerkAuth({ isSignedIn: false, userId: null })
    rerender(tree())
    await act(async () => {})

    recover({ status: 401 })

    expect(mockClerk().clerk.signOut).not.toHaveBeenCalled()
    expect(router.replace).not.toHaveBeenCalled()
  })

  it('clears the handler on unmount', async () => {
    setMockClerkAuth({ isSignedIn: true, userId: 'user_1' })
    const { unmount } = render(tree())
    await act(async () => {})

    unmount()
    await act(async () => {})

    recover({ status: 401 })

    expect(mockClerk().clerk.signOut).not.toHaveBeenCalled()
  })

  it('routes the adapter token-error sink into the API error reporter', async () => {
    const reporter = jest.fn()
    setApiErrorReporter(reporter)
    setMockClerkAuth({ isSignedIn: true, userId: 'user_1' })

    render(tree())
    await act(async () => {})

    const failure = new Error('token expired')
    reportTokenError(failure)

    expect(reporter).toHaveBeenCalledWith(failure)
  })

  it('never configures the API client from the provider tree', async () => {
    setMockClerkAuth({ isSignedIn: true, userId: 'user_1' })

    render(tree())
    await act(async () => {})

    expect(clientMock.configureApiClient).not.toHaveBeenCalled()
  })
})

describe('AuthProviders concurrent 401 recovery', () => {
  it('dispatches a single recovery when two 401s arrive together', async () => {
    setMockClerkAuth({ isSignedIn: true, userId: 'user_1' })

    render(tree())
    await act(async () => {})

    await Promise.all([
      Promise.resolve().then(() => recover({ status: 401 })),
      Promise.resolve().then(() => recover({ status: 401 })),
    ])

    expect(mockClerk().clerk.signOut).toHaveBeenCalledTimes(1)
    expect(router.replace).toHaveBeenCalledTimes(1)
    expect(router.replace).toHaveBeenCalledWith('/(auth)/sign-in')
  })
})
