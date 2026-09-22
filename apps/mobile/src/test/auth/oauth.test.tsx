import { act, fireEvent, render, screen } from '@testing-library/react-native'
import { afterEach, beforeEach } from '@jest/globals'
import { router } from 'expo-router'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import type { ReactNode } from 'react'
import {
  MobileAuthContext,
  inertMobileAuthContextValue,
  type MobileAuthContextValue,
  type MobileAuthValue,
  type MobileSignInValue,
  type MobileSSOValue,
  type SignInHandle,
} from '../../auth/mobile-auth'
import { ThemeProvider } from '../../theme/ThemeProvider'
import SignInScreen from '../../../app/(auth)/sign-in'
import SSOCallbackScreen from '../../../app/(auth)/sso-callback'

/**
 * The Google/Apple SSO path is adapter-backed: these tests drive the sign-in screen through the
 * adapter's own context (`MobileAuthContext`), never through Clerk. The screen is allowed to
 * consume `useMobileSSO()` / `useMobileAuth().activateSession` and nothing else.
 *
 * The redirect URL is pinned to `app.json`'s `expo.scheme` here, so changing the scheme without
 * updating the screen fails this suite rather than silently shipping a dead deep link.
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

const MOBILE_ROOT = dirname(require.resolve('../../../package.json'))
const appConfig = JSON.parse(readFileSync(resolve(MOBILE_ROOT, 'app.json'), 'utf8')) as {
  expo: { scheme: string }
}
const EXPECTED_REDIRECT = `${appConfig.expo.scheme}://sso-callback`

type SSOResult = Awaited<ReturnType<MobileSSOValue['startSSOFlow']>>

function makeSignInHandle(): SignInHandle {
  return {
    status: 'needs_first_factor',
    createdSessionId: null,
    password: jest.fn(async () => ({ error: null })),
    emailCode: {
      sendCode: jest.fn(async () => ({ error: null })),
      verifyCode: jest.fn(async () => ({ error: null })),
    },
    resetPasswordEmailCode: {
      sendCode: jest.fn(async () => ({ error: null })),
      verifyCode: jest.fn(async () => ({ error: null })),
      submitPassword: jest.fn(async () => ({ error: null })),
    },
    reset: jest.fn(async () => ({ error: null })),
  }
}

function makeSignInValue(): MobileSignInValue {
  return {
    isLoaded: true,
    signIn: makeSignInHandle(),
    finalize: jest.fn(async () => ({ pendingTask: false, error: null })),
  }
}

function makeSSO(result: Partial<SSOResult> = {}): MobileSSOValue {
  return {
    startSSOFlow: jest.fn(async () => ({
      createdSessionId: null,
      sessionActivated: false,
      cancelled: false,
      pendingTask: false,
      ...result,
    })),
  }
}

function makeContext(
  overrides: {
    sso?: MobileSSOValue
    activateSession?: MobileAuthValue['activateSession']
    signIn?: MobileSignInValue
  } = {},
): MobileAuthContextValue {
  return {
    auth: {
      ...inertMobileAuthContextValue.auth,
      activateSession:
        overrides.activateSession ?? jest.fn(async () => ({ pendingTask: false })),
    },
    signIn: overrides.signIn ?? makeSignInValue(),
    signUp: inertMobileAuthContextValue.signUp,
    sso: overrides.sso ?? makeSSO(),
  }
}

function renderScreen(value: MobileAuthContextValue) {
  return render(
    <ThemeProvider>
      <MobileAuthContext.Provider value={value}>
        <SignInScreen />
      </MobileAuthContext.Provider>
    </ThemeProvider>,
  )
}

async function pressSSO(name: string) {
  await act(async () => {
    fireEvent.press(screen.getByRole('button', { name }))
  })
}

beforeEach(() => {
  jest.clearAllMocks()
})

afterEach(() => {
  jest.restoreAllMocks()
})

describe('sign-in SSO buttons', () => {
  it('starts the Google flow with the app-scheme redirect', async () => {
    const sso = makeSSO({ createdSessionId: 'sess_1', sessionActivated: true })
    renderScreen(makeContext({ sso }))

    await pressSSO('Continue with Google')

    expect(sso.startSSOFlow).toHaveBeenCalledWith({
      strategy: 'oauth_google',
      redirectUrl: EXPECTED_REDIRECT,
    })
  })

  it('starts the Apple flow with the app-scheme redirect', async () => {
    const sso = makeSSO({ createdSessionId: 'sess_1', sessionActivated: true })
    renderScreen(makeContext({ sso }))

    await pressSSO('Continue with Apple')

    expect(sso.startSSOFlow).toHaveBeenCalledWith({
      strategy: 'oauth_apple',
      redirectUrl: EXPECTED_REDIRECT,
    })
  })

  it('routes a self-activated session to /dashboard without activating again', async () => {
    const activateSession = jest.fn(async () => ({ pendingTask: false }))
    const sso = makeSSO({ createdSessionId: 'sess_1', sessionActivated: true })
    renderScreen(makeContext({ sso, activateSession }))

    await pressSSO('Continue with Google')

    expect(router.replace).toHaveBeenCalledWith('/dashboard')
    expect(activateSession).not.toHaveBeenCalled()
  })

  it('routes a pending task from startSSOFlow to /session-task and never /dashboard', async () => {
    const activateSession = jest.fn(async () => ({ pendingTask: false }))
    const sso = makeSSO({ createdSessionId: 'sess_1', sessionActivated: true, pendingTask: true })
    renderScreen(makeContext({ sso, activateSession }))

    await pressSSO('Continue with Google')

    expect(router.replace).toHaveBeenCalledWith('/session-task')
    expect(router.replace).not.toHaveBeenCalledWith('/dashboard')
    expect(activateSession).not.toHaveBeenCalled()
  })

  it('activates a non-self-activated session exactly once and routes to /dashboard', async () => {
    const activateSession = jest.fn(async () => ({ pendingTask: false }))
    const sso = makeSSO({ createdSessionId: 'sess_1', sessionActivated: false })
    renderScreen(makeContext({ sso, activateSession }))

    await pressSSO('Continue with Apple')

    expect(activateSession).toHaveBeenCalledTimes(1)
    expect(activateSession).toHaveBeenCalledWith('sess_1')
    expect(router.replace).toHaveBeenCalledWith('/dashboard')
  })

  it('routes a pending task from activateSession to /session-task and never /dashboard', async () => {
    const activateSession = jest.fn(async () => ({ pendingTask: true }))
    const sso = makeSSO({ createdSessionId: 'sess_1', sessionActivated: false })
    renderScreen(makeContext({ sso, activateSession }))

    await pressSSO('Continue with Google')

    expect(activateSession).toHaveBeenCalledTimes(1)
    expect(router.replace).toHaveBeenCalledWith('/session-task')
    expect(router.replace).not.toHaveBeenCalledWith('/dashboard')
  })

  it('does not activate a null session id and still routes to /dashboard', async () => {
    const activateSession = jest.fn(async () => ({ pendingTask: false }))
    const sso = makeSSO({ createdSessionId: null, sessionActivated: false })
    renderScreen(makeContext({ sso, activateSession }))

    await pressSSO('Continue with Google')

    expect(activateSession).not.toHaveBeenCalled()
    expect(router.replace).toHaveBeenCalledWith('/dashboard')
  })

  it('shows no error and performs no activation when the flow is cancelled', async () => {
    const activateSession = jest.fn(async () => ({ pendingTask: false }))
    const sso = makeSSO({ createdSessionId: null, sessionActivated: false, cancelled: true })
    renderScreen(makeContext({ sso, activateSession }))

    await pressSSO('Continue with Google')

    expect(screen.queryByTestId('sign-in-error')).not.toBeOnTheScreen()
    expect(activateSession).not.toHaveBeenCalled()
    expect(router.replace).not.toHaveBeenCalled()
  })

  it('renders an accessible error when startSSOFlow throws', async () => {
    const sso: MobileSSOValue = {
      startSSOFlow: jest.fn(async () => {
        throw new Error('provider unavailable')
      }),
    }
    renderScreen(makeContext({ sso }))

    await pressSSO('Continue with Google')

    const alert = screen.getByTestId('sign-in-error')
    expect(alert.props.accessibilityLiveRegion).toBe('assertive')
    expect(screen.getByText('provider unavailable')).toBeOnTheScreen()
    expect(router.replace).not.toHaveBeenCalled()
  })

  it('renders an accessible error when activateSession throws', async () => {
    const activateSession = jest.fn(async () => {
      throw new Error('activation failed')
    })
    const sso = makeSSO({ createdSessionId: 'sess_1', sessionActivated: false })
    renderScreen(makeContext({ sso, activateSession }))

    await pressSSO('Continue with Apple')

    expect(screen.getByTestId('sign-in-error').props.accessibilityLiveRegion).toBe('assertive')
    expect(screen.getByText('activation failed')).toBeOnTheScreen()
    expect(router.replace).not.toHaveBeenCalled()
  })

  it('is a no-op in inert mode: the controls are disabled and no flow starts', async () => {
    const sso = makeSSO()
    renderScreen(
      makeContext({ sso, signIn: inertMobileAuthContextValue.signIn }),
    )

    expect(screen.getByTestId('sign-in-google').props.accessibilityState.disabled).toBe(true)
    expect(screen.getByTestId('sign-in-apple').props.accessibilityState.disabled).toBe(true)

    await pressSSO('Continue with Google')

    expect(sso.startSSOFlow).not.toHaveBeenCalled()
    expect(router.replace).not.toHaveBeenCalled()
  })
})

describe('sso-callback route', () => {
  it('renders an accessible completing state with a way back to sign in', () => {
    render(
      <ThemeProvider>
        <SSOCallbackScreen />
      </ThemeProvider>,
    )

    expect(screen.getByText(/completing sign-in/i)).toBeOnTheScreen()
    fireEvent.press(screen.getByRole('button', { name: 'Back to sign in' }))
    expect(router.replace).toHaveBeenCalledWith('/sign-in')
  })
})