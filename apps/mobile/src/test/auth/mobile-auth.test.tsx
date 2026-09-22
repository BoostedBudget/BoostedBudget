import { act, renderHook } from '@testing-library/react-native'
import { beforeEach } from '@jest/globals'
import { ReactNode } from 'react'
import {
  MobileAuthProvider,
  MobileAuthContextValue,
  inertMobileAuthContextValue,
  setTokenErrorReporter,
  useMobileAuth,
  useMobileSignIn,
  useMobileSignUp,
  useMobileSSO,
} from '../../auth/mobile-auth'
import { useClerkMobileAuthContext } from '../../auth/mobile-auth.clerk'

/**
 * The unit tier cannot prove Clerk is never executed: this file installs a local `@clerk/expo`
 * mock, which masks it. Non-execution is proven by the static-export tier plus the source guard
 * (todo 7). Do not read these tests as evidence of that property.
 */

// `var` (not `const`) so the declaration is hoisted above the `jest.mock` factories. The factories
// only close over this object; they read it lazily when the mocked hooks are called during render,
// by which point the assignment below has run.
type MockSetActiveParams = {
  session: string
  navigate: (params: {
    session: { currentTask?: { key: string } }
    decorateUrl: (url: string) => string
  }) => void
}

var mockClerkState = {
  auth: {
    isLoaded: true,
    isSignedIn: false,
    userId: null as string | null,
    getToken: jest.fn(async () => null as string | null),
  },
  clerk: {
    setActive: jest.fn<Promise<void>, [MockSetActiveParams]>(async () => {}),
    signOut: jest.fn(async () => {}),
    session: null as { currentTask?: { key: string } } | null,
  },
  session: {
    isLoaded: true,
    isSignedIn: false,
    session: null as { currentTask?: { key: string } } | null,
  },
  signIn: { signIn: null as unknown },
  signUp: { signUp: null as unknown },
  sso: {
    startSSOFlow: jest.fn(async () => ({
      createdSessionId: null as string | null,
      authSessionResult: null as { type: string } | null,
      signIn: null,
      signUp: null,
    })),
  },
}

jest.mock('@clerk/expo', () => ({
  ClerkProvider: ({ children }: { children: ReactNode }) => children,
  useAuth: () => mockClerkState.auth,
  useClerk: () => mockClerkState.clerk,
  useSession: () => mockClerkState.session,
  useSignIn: () => mockClerkState.signIn,
  useSignUp: () => mockClerkState.signUp,
}))

jest.mock('@clerk/expo/experimental', () => ({
  useSSO: () => mockClerkState.sso,
}))

type ExpectTrue<T extends true> = T

export type _InertSatisfiesInterface = ExpectTrue<
  typeof inertMobileAuthContextValue extends MobileAuthContextValue ? true : false
>

export type _ClerkSatisfiesInterface = ExpectTrue<
  ReturnType<typeof useClerkMobileAuthContext> extends MobileAuthContextValue ? true : false
>

const inertWrapper = ({ children }: { children: ReactNode }) => (
  <MobileAuthProvider authAvailable={false}>{children}</MobileAuthProvider>
)

const clerkWrapper = ({ children }: { children: ReactNode }) => (
  <MobileAuthProvider authAvailable publishableKey="pk_test_abc123">
    {children}
  </MobileAuthProvider>
)

beforeEach(() => {
  jest.clearAllMocks()
  mockClerkState.auth.isLoaded = true
  mockClerkState.auth.isSignedIn = false
  mockClerkState.auth.userId = null
  mockClerkState.auth.getToken.mockResolvedValue(null)
  mockClerkState.clerk.session = null
  mockClerkState.clerk.setActive.mockResolvedValue(undefined)
  mockClerkState.clerk.signOut.mockResolvedValue(undefined)
  mockClerkState.session.session = null
  mockClerkState.signIn.signIn = null
  mockClerkState.signUp.signUp = null
  mockClerkState.sso.startSSOFlow.mockResolvedValue({
    createdSessionId: null,
    authSessionResult: null,
    signIn: null,
    signUp: null,
  })
})

describe('inert mode', () => {
  it('is loaded, signed out, and resolves a null token without throwing', async () => {
    const { result } = renderHook(() => useMobileAuth(), { wrapper: inertWrapper })

    expect(result.current.isLoaded).toBe(true)
    expect(result.current.isSignedIn).toBe(false)
    expect(result.current.userId).toBeNull()
    await expect(result.current.getToken()).resolves.toBeNull()
  })

  it('exposes null sign-in/sign-up handles and an inert finalize', async () => {
    const signIn = renderHook(() => useMobileSignIn(), { wrapper: inertWrapper })
    const signUp = renderHook(() => useMobileSignUp(), { wrapper: inertWrapper })

    expect(signIn.result.current.signIn).toBeNull()
    expect(signUp.result.current.signUp).toBeNull()
    await expect(
      signIn.result.current.finalize({ destination: '/dashboard', navigate: jest.fn() }),
    ).resolves.toEqual({ pendingTask: false, error: null })
    await expect(
      signUp.result.current.finalize({ destination: '/dashboard', navigate: jest.fn() }),
    ).resolves.toEqual({ pendingTask: false, error: null })
  })

  it('reports a cancelled SSO flow', async () => {
    const { result } = renderHook(() => useMobileSSO(), { wrapper: inertWrapper })

    await expect(
      result.current.startSSOFlow({ strategy: 'oauth_google', redirectUrl: 'boostedbudget://sso' }),
    ).resolves.toEqual({
      createdSessionId: null,
      sessionActivated: false,
      cancelled: true,
      pendingTask: false,
    })
  })

  it('delegates reportTokenError to the registered reporter', () => {
    const reporter = jest.fn()
    setTokenErrorReporter(reporter)

    const { result } = renderHook(() => useMobileAuth(), { wrapper: inertWrapper })
    result.current.reportTokenError(new Error('token'))

    expect(reporter).toHaveBeenCalledWith(expect.any(Error))
    setTokenErrorReporter(null)
  })
})

describe('Clerk-backed mode', () => {
  it('reflects the signed-in state from useAuth', () => {
    mockClerkState.auth.isSignedIn = true
    mockClerkState.auth.userId = 'user_1'

    const { result } = renderHook(() => useMobileAuth(), { wrapper: clerkWrapper })

    expect(result.current.isLoaded).toBe(true)
    expect(result.current.isSignedIn).toBe(true)
    expect(result.current.userId).toBe('user_1')
  })

  it('does not treat a pending-task session as signed in', () => {
    mockClerkState.auth.isSignedIn = true
    mockClerkState.session.session = { currentTask: { key: 'choose-organization' } }

    const { result } = renderHook(() => useMobileAuth(), { wrapper: clerkWrapper })

    expect(result.current.isSignedIn).toBe(false)
  })

  it('reports pendingTask from the fresh clerk.session after a self-activating SSO flow', async () => {
    mockClerkState.clerk.session = { currentTask: { key: 'choose-organization' } }
    mockClerkState.sso.startSSOFlow.mockResolvedValue({
      createdSessionId: 'sess_1',
      authSessionResult: { type: 'success' },
      signIn: null,
      signUp: null,
    })

    const { result } = renderHook(() => useMobileSSO(), { wrapper: clerkWrapper })

    await act(async () => {
      await expect(
        result.current.startSSOFlow({ strategy: 'oauth_google', redirectUrl: 'boostedbudget://sso' }),
      ).resolves.toEqual({
        createdSessionId: 'sess_1',
        sessionActivated: true,
        cancelled: false,
        pendingTask: true,
      })
    })
  })

  it('reports pendingTask false when the SSO session carries no task', async () => {
    mockClerkState.sso.startSSOFlow.mockResolvedValue({
      createdSessionId: 'sess_2',
      authSessionResult: { type: 'success' },
      signIn: null,
      signUp: null,
    })

    const { result } = renderHook(() => useMobileSSO(), { wrapper: clerkWrapper })

    await act(async () => {
      const outcome = await result.current.startSSOFlow({
        strategy: 'oauth_apple',
        redirectUrl: 'boostedbudget://sso',
      })
      expect(outcome.pendingTask).toBe(false)
      expect(outcome.sessionActivated).toBe(true)
    })
  })

  it('derives cancelled from the auth session result type', async () => {
    mockClerkState.sso.startSSOFlow.mockResolvedValue({
      createdSessionId: null,
      authSessionResult: { type: 'cancel' },
      signIn: null,
      signUp: null,
    })

    const { result } = renderHook(() => useMobileSSO(), { wrapper: clerkWrapper })

    await act(async () => {
      const outcome = await result.current.startSSOFlow({
        strategy: 'oauth_google',
        redirectUrl: 'boostedbudget://sso',
      })
      expect(outcome.cancelled).toBe(true)
      expect(outcome.sessionActivated).toBe(false)
    })
  })

  it('derives activateSession pendingTask from the navigate callback argument', async () => {
    mockClerkState.clerk.setActive.mockImplementation(async ({ navigate }) => {
      navigate({ session: { currentTask: { key: 'setup-mfa' } }, decorateUrl: (url: string) => url })
    })

    const { result } = renderHook(() => useMobileAuth(), { wrapper: clerkWrapper })

    await act(async () => {
      await expect(result.current.activateSession('sess_1')).resolves.toEqual({ pendingTask: true })
    })
  })

  it('calls decorateUrl with /dashboard when the activated session has no task', async () => {
    const decorateUrl = jest.fn((url: string) => url)
    mockClerkState.clerk.setActive.mockImplementation(async ({ navigate }) => {
      navigate({ session: {}, decorateUrl })
    })

    const { result } = renderHook(() => useMobileAuth(), { wrapper: clerkWrapper })

    await act(async () => {
      await expect(result.current.activateSession('sess_1')).resolves.toEqual({ pendingTask: false })
    })
    expect(decorateUrl).toHaveBeenCalledWith('/dashboard')
  })

  it('no-ops activateSession for a null session id', async () => {
    const { result } = renderHook(() => useMobileAuth(), { wrapper: clerkWrapper })

    await act(async () => {
      await expect(result.current.activateSession(null)).resolves.toEqual({ pendingTask: false })
    })
    expect(mockClerkState.clerk.setActive).not.toHaveBeenCalled()
  })

  it('routes a pending session task to onSessionTask and never navigates', async () => {
    const navigate = jest.fn()
    const onSessionTask = jest.fn()
    mockClerkState.signIn.signIn = {
      finalize: jest.fn(async ({ navigate: nav }) => {
        nav({ session: { currentTask: { key: 'reset-password' } }, decorateUrl: (url: string) => url })
        return { error: null }
      }),
    }

    const { result } = renderHook(() => useMobileSignIn(), { wrapper: clerkWrapper })

    await act(async () => {
      await expect(
        result.current.finalize({ destination: '/dashboard', navigate, onSessionTask }),
      ).resolves.toEqual({ pendingTask: true, error: null })
    })
    expect(onSessionTask).toHaveBeenCalledWith({ key: 'reset-password' })
    expect(navigate).not.toHaveBeenCalled()
  })

  it('surfaces finalize error without navigating', async () => {
    const navigate = jest.fn()
    mockClerkState.signIn.signIn = {
      finalize: jest.fn(async () => ({ error: { message: 'bad' } })),
    }

    const { result } = renderHook(() => useMobileSignIn(), { wrapper: clerkWrapper })

    await act(async () => {
      const outcome = await result.current.finalize({ destination: '/dashboard', navigate })
      expect(outcome).toEqual({ pendingTask: false, error: { message: 'bad' } })
    })
    expect(navigate).not.toHaveBeenCalled()
  })

  it('normalizes a thrown finalize error into the result envelope', async () => {
    mockClerkState.signIn.signIn = {
      finalize: jest.fn(async () => {
        throw new Error('boom')
      }),
    }

    const { result } = renderHook(() => useMobileSignIn(), { wrapper: clerkWrapper })

    await act(async () => {
      const outcome = await result.current.finalize({ destination: '/dashboard', navigate: jest.fn() })
      expect(outcome.pendingTask).toBe(false)
      expect(outcome.error).toBeInstanceOf(Error)
    })
  })

  it('hands an external decorated URL to the consumer unchanged', async () => {
    const navigate = jest.fn()
    mockClerkState.signIn.signIn = {
      finalize: jest.fn(async ({ navigate: nav }) => {
        nav({ session: {}, decorateUrl: () => 'https://external.example.com/refresh' })
        return { error: null }
      }),
    }

    const { result } = renderHook(() => useMobileSignIn(), { wrapper: clerkWrapper })

    await act(async () => {
      await result.current.finalize({ destination: '/dashboard', navigate })
    })
    expect(navigate).toHaveBeenCalledWith('https://external.example.com/refresh')
  })

  it('wraps signUp.finalize the same way', async () => {
    const navigate = jest.fn()
    mockClerkState.signUp.signUp = {
      finalize: jest.fn(async ({ navigate: nav }) => {
        nav({ session: {}, decorateUrl: (url: string) => url })
        return { error: null }
      }),
    }

    const { result } = renderHook(() => useMobileSignUp(), { wrapper: clerkWrapper })

    await act(async () => {
      await expect(
        result.current.finalize({ destination: '/dashboard', navigate }),
      ).resolves.toEqual({ pendingTask: false, error: null })
    })
    expect(navigate).toHaveBeenCalledWith('/dashboard')
  })
})