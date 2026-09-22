import { act, render } from '@testing-library/react-native'
import { afterEach, beforeEach } from '@jest/globals'
import { ReactNode } from 'react'
import { Text } from 'react-native'
import { resetMockClerkAuth, setMockClerkAuth } from '../../../jest.setup'
import { AuthProviders } from '../../auth/AuthProviders'
import { MobileAuthProvider, setTokenErrorReporter } from '../../auth/mobile-auth'

/**
 * Todo 10 (#1657): the token provider registered with the shared API client.
 *
 * `AuthProviders` must register a wrapper that always calls the LATEST adapter `getToken` — a
 * ref, not a captured closure — and must report a token rejection while resolving `null`. The
 * API client is mocked so the registered wrapper can be captured and invoked directly; the
 * adapter's own `reportTokenError` sink is driven through `setTokenErrorReporter`.
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

function Probe() {
  return <Text>probe-child</Text>
}

function tree() {
  return (
    <MobileAuthProvider authAvailable publishableKey="pk_test_abc123">
      <AuthProviders>
        <Probe />
      </AuthProviders>
    </MobileAuthProvider>
  )
}

/** The most recently registered provider, as handed to `setAuthTokenProvider`. */
function registeredProvider(): () => Promise<string | null> {
  const calls = clientMock.setAuthTokenProvider.mock.calls
  expect(calls.length).toBeGreaterThan(0)
  return calls[calls.length - 1][0] as () => Promise<string | null>
}

beforeEach(() => {
  jest.clearAllMocks()
  resetMockClerkAuth()
})

afterEach(() => {
  setTokenErrorReporter(null)
})

describe('AuthProviders token provider registration', () => {
  it('registers a wrapper that returns the adapter token', async () => {
    setMockClerkAuth({ getToken: jest.fn(async () => 'token-1') })

    render(tree())
    await act(async () => {})

    expect(clientMock.setAuthTokenProvider).toHaveBeenCalledTimes(1)
    await expect(registeredProvider()()).resolves.toBe('token-1')
  })

  it('always calls the latest getToken after the adapter swaps it', async () => {
    const first = jest.fn(async () => 'token-1')
    setMockClerkAuth({ getToken: first })

    const { rerender } = render(tree())
    await act(async () => {})

    const provider = registeredProvider()
    await expect(provider()).resolves.toBe('token-1')

    // Swap the adapter's getToken and force a re-render. The registered wrapper must NOT be
    // re-registered (it is the same function) but must observe the new getToken through the ref.
    const second = jest.fn(async () => 'token-2')
    setMockClerkAuth({ getToken: second })
    rerender(tree())
    await act(async () => {})

    expect(clientMock.setAuthTokenProvider).toHaveBeenCalledTimes(1)
    await expect(provider()).resolves.toBe('token-2')
    expect(second).toHaveBeenCalledTimes(1)
  })

  it('reports a token rejection and resolves null', async () => {
    const failure = new Error('token unavailable')
    const reporter = jest.fn()
    setTokenErrorReporter(reporter)
    setMockClerkAuth({
      getToken: jest.fn(async () => {
        throw failure
      }),
    })

    render(tree())
    await act(async () => {})

    await expect(registeredProvider()()).resolves.toBeNull()
    expect(reporter).toHaveBeenCalledWith(failure)
  })

  it('registers once per isLoaded transition, not per getToken change', async () => {
    setMockClerkAuth({ isLoaded: true })

    const { rerender } = render(tree())
    await act(async () => {})
    expect(clientMock.setAuthTokenProvider).toHaveBeenCalledTimes(1)

    // A getToken identity change must not re-register the provider.
    setMockClerkAuth({ getToken: jest.fn(async () => 'token-2') })
    rerender(tree())
    await act(async () => {})
    expect(clientMock.setAuthTokenProvider).toHaveBeenCalledTimes(1)

    // An isLoaded transition does re-register.
    setMockClerkAuth({ isLoaded: false })
    rerender(tree())
    await act(async () => {})
    expect(clientMock.setAuthTokenProvider).toHaveBeenCalledTimes(1)

    setMockClerkAuth({ isLoaded: true })
    rerender(tree())
    await act(async () => {})
    expect(clientMock.setAuthTokenProvider).toHaveBeenCalledTimes(2)
  })

  it('never calls configureApiClient', async () => {
    render(tree())
    await act(async () => {})

    expect(clientMock.configureApiClient).not.toHaveBeenCalled()
  })
})