import { act, fireEvent, render, screen } from '@testing-library/react-native'
import { afterEach, beforeEach } from '@jest/globals'
import { router } from 'expo-router'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { Linking } from 'react-native'
import type { ReactNode } from 'react'
import {
  MobileAuthContext,
  inertMobileAuthContextValue,
  type FinalizeOptions,
  type MobileAuthContextValue,
  type SignInHandle,
} from '../../auth/mobile-auth'
import { navigateToClerkDestination } from '../../auth/navigation'
import { ThemeProvider } from '../../theme/ThemeProvider'
import AuthLayout from '../../../app/(auth)/_layout'
import SessionTaskScreen from '../../../app/(auth)/session-task'
import SignInScreen from '../../../app/(auth)/sign-in'

// The real provider hydrates asynchronously from AsyncStorage, which would need `act` around every
// render. `Screen`/`ScreenHeader` only need a resolved theme, so a pass-through provider is enough
// here; the real provider's behaviour is covered by `theme-provider.test.tsx` / `theme-wiring.test.tsx`.
jest.mock('../../theme/ThemeProvider', () => ({
  ThemeProvider: ({ children }: { children: ReactNode }) => children,
  useTheme: () => ({
    preference: 'system',
    resolvedTheme: 'light',
    setPreference: jest.fn(),
    hydrated: true,
  }),
}))

/**
 * The sign-in screen is adapter-backed: these tests drive it through the adapter's own context
 * (`MobileAuthContext`), never through Clerk. That keeps the unit tier free of `@clerk/expo` —
 * the adapter seam is exactly what the screen is allowed to consume.
 *
 * `navigateToClerkDestination` and the `session-task` placeholder are pinned here too, so all of
 * todo 12's new surface has executable coverage in one file.
 */

const MOBILE_ROOT = dirname(require.resolve('../../../package.json'))

const SESSION_TASK_NOTICE = 'additional verification or selection is required'

const EMAIL_LABEL = 'Email required'
const PASSWORD_LABEL = 'Password required'

function makeSignInHandle(
  password: SignInHandle['password'] = jest.fn(async () => ({ error: null })),
): SignInHandle {
  return {
    status: 'needs_first_factor',
    createdSessionId: null,
    password,
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

function makeContext(overrides: Partial<MobileAuthContextValue> = {}): MobileAuthContextValue {
  return {
    auth: inertMobileAuthContextValue.auth,
    signIn: inertMobileAuthContextValue.signIn,
    signUp: inertMobileAuthContextValue.signUp,
    sso: inertMobileAuthContextValue.sso,
    ...overrides,
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

/** Fill both fields and press submit, awaiting the async handler. */
async function submitCredentials(email = 'user@example.com', password = 'hunter2') {
  fireEvent.changeText(screen.getByLabelText(EMAIL_LABEL), email)
  fireEvent.changeText(screen.getByLabelText(PASSWORD_LABEL), password)
  await act(async () => {
    fireEvent.press(screen.getByRole('button', { name: 'Sign in' }))
  })
}

beforeEach(() => {
  jest.clearAllMocks()
})

afterEach(() => {
  jest.restoreAllMocks()
})

describe('navigateToClerkDestination', () => {
  it('opens an external https URL through Linking instead of routing', () => {
    const openURL = jest.spyOn(Linking, 'openURL').mockResolvedValue(true)

    navigateToClerkDestination('https://external.example.com/refresh')

    expect(openURL).toHaveBeenCalledWith('https://external.example.com/refresh')
    expect(router.replace).not.toHaveBeenCalled()
  })

  it('routes a relative destination through router.replace', () => {
    navigateToClerkDestination('/dashboard')

    expect(router.replace).toHaveBeenCalledWith('/dashboard')
  })
})

describe('sign-in screen', () => {
  it('renders named email, password, submit and SSO controls', () => {
    renderScreen(makeContext())

    expect(screen.getByLabelText(EMAIL_LABEL)).toBeOnTheScreen()
    expect(screen.getByLabelText(PASSWORD_LABEL)).toBeOnTheScreen()
    expect(screen.getByRole('button', { name: 'Sign in' })).toBeOnTheScreen()
    expect(screen.getByRole('button', { name: 'Continue with Google' })).toBeOnTheScreen()
    expect(screen.getByRole('button', { name: 'Continue with Apple' })).toBeOnTheScreen()
    expect(screen.getByRole('button', { name: 'Create an account' })).toBeOnTheScreen()
    expect(screen.getByRole('button', { name: 'Forgot password?' })).toBeOnTheScreen()
  })

  it('renders the real form disabled with an accessible notice in inert mode', () => {
    renderScreen(makeContext())

    const email = screen.getByTestId('sign-in-email')
    expect(email.props.editable).toBe(false)
    expect(email.props.accessibilityState.disabled).toBe(true)
    expect(screen.getByTestId('sign-in-submit').props.accessibilityState.disabled).toBe(true)

    const notice = screen.getByText('sign-in is unavailable in this build')
    expect(notice).toBeOnTheScreen()
    expect(
      screen.getByTestId('sign-in-unavailable').props.accessibilityLiveRegion,
    ).toBe('assertive')
  })

  it('disables the controls, without the notice, while the adapter is not loaded', () => {
    renderScreen(makeContext({ signIn: { isLoaded: false, signIn: makeSignInHandle(), finalize: jest.fn() } }))

    expect(screen.getByTestId('sign-in-submit').props.accessibilityState.disabled).toBe(true)
    expect(screen.queryByText('sign-in is unavailable in this build')).not.toBeOnTheScreen()
  })

  it('submits credentials, finalizes once, and navigates to /dashboard', async () => {
    const password = jest.fn(async () => ({ error: null }))
    const finalize = jest.fn(async (options: FinalizeOptions) => {
      options.navigate('/dashboard')
      return { pendingTask: false, error: null }
    })
    renderScreen(
      makeContext({ signIn: { isLoaded: true, signIn: makeSignInHandle(password), finalize } }),
    )

    await submitCredentials()

    expect(password).toHaveBeenCalledWith({ password: 'hunter2', identifier: 'user@example.com' })
    expect(finalize).toHaveBeenCalledTimes(1)
    expect(router.replace).toHaveBeenCalledWith('/dashboard')
  })

  it('renders an accessible error and does not activate when the password is rejected', async () => {
    const password = jest.fn(async () => ({
      error: { errors: [{ message: 'Password is incorrect' }] },
    }))
    const finalize = jest.fn()
    renderScreen(
      makeContext({ signIn: { isLoaded: true, signIn: makeSignInHandle(password), finalize } }),
    )

    await submitCredentials('user@example.com', 'wrong-password')

    const alert = screen.getByTestId('sign-in-error')
    expect(alert.props.accessibilityLiveRegion).toBe('assertive')
    expect(screen.getByText('Password is incorrect')).toBeOnTheScreen()
    expect(finalize).not.toHaveBeenCalled()
    expect(router.replace).not.toHaveBeenCalled()
  })

  it('renders an accessible error from a finalize failure without navigating', async () => {
    const finalize = jest.fn(async () => ({ pendingTask: false, error: { message: 'Too many attempts' } }))
    renderScreen(
      makeContext({
        signIn: { isLoaded: true, signIn: makeSignInHandle(), finalize },
      }),
    )

    await submitCredentials()

    expect(screen.getByTestId('sign-in-error').props.accessibilityLiveRegion).toBe('assertive')
    expect(screen.getByText('Too many attempts')).toBeOnTheScreen()
    expect(router.replace).not.toHaveBeenCalled()
  })

  it('normalizes a thrown submission error into an accessible error', async () => {
    const password = jest.fn(async () => {
      throw new Error('network down')
    })
    renderScreen(
      makeContext({
        signIn: { isLoaded: true, signIn: makeSignInHandle(password), finalize: jest.fn() },
      }),
    )

    await submitCredentials()

    expect(screen.getByTestId('sign-in-error').props.accessibilityLiveRegion).toBe('assertive')
  })

  it('routes a pending session task to /session-task instead of /dashboard', async () => {
    const finalize = jest.fn(async (options: FinalizeOptions) => {
      options.onSessionTask?.({ key: 'setup-mfa' })
      return { pendingTask: true, error: null }
    })
    renderScreen(
      makeContext({ signIn: { isLoaded: true, signIn: makeSignInHandle(), finalize } }),
    )

    await submitCredentials()

    expect(router.replace).toHaveBeenCalledWith('/session-task')
    expect(router.replace).not.toHaveBeenCalledWith('/dashboard')
  })

  it('opens an external decorated URL through Linking, not the router', async () => {
    const openURL = jest.spyOn(Linking, 'openURL').mockResolvedValue(true)
    const finalize = jest.fn(async (options: FinalizeOptions) => {
      options.navigate('https://external.example.com/refresh')
      return { pendingTask: false, error: null }
    })
    renderScreen(
      makeContext({ signIn: { isLoaded: true, signIn: makeSignInHandle(), finalize } }),
    )

    await submitCredentials()

    expect(openURL).toHaveBeenCalledWith('https://external.example.com/refresh')
    expect(router.replace).not.toHaveBeenCalled()
  })
})

describe('session-task placeholder', () => {
  it('states that more verification is required and offers a continue action', () => {
    render(
      <ThemeProvider>
        <SessionTaskScreen />
      </ThemeProvider>,
    )

    expect(screen.getByText(new RegExp(SESSION_TASK_NOTICE, 'i'))).toBeOnTheScreen()
    fireEvent.press(screen.getByRole('button', { name: 'Continue' }))
    expect(router.replace).toHaveBeenCalledWith('/dashboard')
  })
})

describe('auth group layout', () => {
  it('renders a plain public group and never uses Stack.Protected', () => {
    const source = readFileSync(resolve(MOBILE_ROOT, 'app/(auth)/_layout.tsx'), 'utf8')

    expect(source).not.toContain('Stack.Protected')
    expect(() =>
      render(
        <ThemeProvider>
          <AuthLayout />
        </ThemeProvider>,
      ),
    ).not.toThrow()
  })
})
