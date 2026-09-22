import { act, fireEvent, render, screen } from '@testing-library/react-native'
import { afterEach, beforeEach } from '@jest/globals'
import { router } from 'expo-router'
import { Linking } from 'react-native'
import type { ReactNode } from 'react'
import {
  MobileAuthContext,
  inertMobileAuthContextValue,
  type FinalizeOptions,
  type MobileAuthContextValue,
  type SignInHandle,
} from '../../auth/mobile-auth'
import { ThemeProvider } from '../../theme/ThemeProvider'
import ForgotPasswordScreen from '../../../app/(auth)/forgot-password'

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
 * The forgot-password screen is adapter-backed: these tests drive it through the adapter's own
 * context (`MobileAuthContext`), never through Clerk. That keeps the unit tier free of
 * `@clerk/expo` — the adapter seam is exactly what the screen is allowed to consume.
 *
 * The three steps are exercised end-to-end: request a code, verify it, then set the new password
 * and finalize. Each step's error path is asserted through the `Alert` primitive's
 * `accessibilityLiveRegion`, matching the sign-in screen's convention.
 */

// `FormField required` appends " required" to the accessible label.
const EMAIL_LABEL = 'Email required'
const CODE_LABEL = 'Verification code required'
const PASSWORD_LABEL = 'New password required'

function makeSignInHandle(
  reset: Partial<SignInHandle['resetPasswordEmailCode']> = {},
): SignInHandle {
  return {
    status: 'needs_new_password',
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
      ...reset,
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

function activeContext(
  reset: Partial<SignInHandle['resetPasswordEmailCode']> = {},
  finalize: MobileAuthContextValue['signIn']['finalize'] = jest.fn(
    async (options: FinalizeOptions) => {
      options.navigate('/dashboard')
      return { pendingTask: false, error: null }
    },
  ),
): MobileAuthContextValue {
  return makeContext({
    signIn: { isLoaded: true, signIn: makeSignInHandle(reset), finalize },
  })
}

function renderScreen(value: MobileAuthContextValue) {
  return render(
    <ThemeProvider>
      <MobileAuthContext.Provider value={value}>
        <ForgotPasswordScreen />
      </MobileAuthContext.Provider>
    </ThemeProvider>,
  )
}

/** Fill the email field and press "Send reset code", awaiting the async handler. */
async function requestCode(email = 'user@example.com') {
  fireEvent.changeText(screen.getByLabelText(EMAIL_LABEL), email)
  await act(async () => {
    fireEvent.press(screen.getByRole('button', { name: 'Send reset code' }))
  })
}

/** Fill the code field and press "Verify code", awaiting the async handler. */
async function verifyCode(code = '123456') {
  fireEvent.changeText(screen.getByLabelText(CODE_LABEL), code)
  await act(async () => {
    fireEvent.press(screen.getByRole('button', { name: 'Verify code' }))
  })
}

/** Fill the new-password field and press "Set new password", awaiting the async handler. */
async function submitNewPassword(password = 'correct-horse-battery') {
  fireEvent.changeText(screen.getByLabelText(PASSWORD_LABEL), password)
  await act(async () => {
    fireEvent.press(screen.getByRole('button', { name: 'Set new password' }))
  })
}

beforeEach(() => {
  jest.clearAllMocks()
})

afterEach(() => {
  jest.restoreAllMocks()
})

describe('forgot-password screen', () => {
  it('renders the request step with a named email field, submit and back link', () => {
    renderScreen(activeContext())

    expect(screen.getByLabelText(EMAIL_LABEL)).toBeOnTheScreen()
    expect(screen.getByRole('button', { name: 'Send reset code' })).toBeOnTheScreen()
    expect(screen.getByRole('button', { name: 'Back to sign in' })).toBeOnTheScreen()
  })

  it('renders the real form disabled with an accessible notice in inert mode', () => {
    renderScreen(makeContext())

    const email = screen.getByTestId('forgot-password-email')
    expect(email.props.editable).toBe(false)
    expect(email.props.accessibilityState.disabled).toBe(true)
    expect(
      screen.getByTestId('forgot-password-send-code').props.accessibilityState.disabled,
    ).toBe(true)

    expect(screen.getByText('password reset is unavailable in this build')).toBeOnTheScreen()
    expect(
      screen.getByTestId('forgot-password-unavailable').props.accessibilityLiveRegion,
    ).toBe('assertive')
  })

  it('disables the controls, without the notice, while the adapter is not loaded', () => {
    renderScreen(
      makeContext({ signIn: { isLoaded: false, signIn: makeSignInHandle(), finalize: jest.fn() } }),
    )

    expect(
      screen.getByTestId('forgot-password-send-code').props.accessibilityState.disabled,
    ).toBe(true)
    expect(screen.queryByText('password reset is unavailable in this build')).not.toBeOnTheScreen()
  })

  it('sends the reset code once and advances to the verify step', async () => {
    const sendCode = jest.fn(async () => ({ error: null }))
    renderScreen(activeContext({ sendCode }))

    await requestCode()

    expect(sendCode).toHaveBeenCalledTimes(1)
    expect(screen.getByLabelText(CODE_LABEL)).toBeOnTheScreen()
  })

  it('blocks the request step on an empty email without calling sendCode', async () => {
    const sendCode = jest.fn(async () => ({ error: null }))
    renderScreen(activeContext({ sendCode }))

    await requestCode('   ')

    expect(sendCode).not.toHaveBeenCalled()
    expect(
      screen.getByTestId('forgot-password-error').props.accessibilityLiveRegion,
    ).toBe('assertive')
    expect(screen.queryByLabelText(CODE_LABEL)).not.toBeOnTheScreen()
  })

  it('renders an accessible error and stays on the request step when sendCode fails', async () => {
    const sendCode = jest.fn(async () => ({
      error: { errors: [{ message: 'No account with that email' }] },
    }))
    renderScreen(activeContext({ sendCode }))

    await requestCode()

    expect(
      screen.getByTestId('forgot-password-error').props.accessibilityLiveRegion,
    ).toBe('assertive')
    expect(screen.getByText('No account with that email')).toBeOnTheScreen()
    expect(screen.queryByLabelText(CODE_LABEL)).not.toBeOnTheScreen()
  })

  it('verifies the code once and advances to the reset step', async () => {
    const verifyCodeMock = jest.fn(async () => ({ error: null }))
    renderScreen(activeContext({ verifyCode: verifyCodeMock }))

    await requestCode()
    await verifyCode('654321')

    expect(verifyCodeMock).toHaveBeenCalledWith({ code: '654321' })
    expect(screen.getByLabelText(PASSWORD_LABEL)).toBeOnTheScreen()
  })

  it('renders an accessible error and stays on verify when the code is expired or mismatched', async () => {
    const verifyCodeMock = jest.fn(async () => ({
      error: { errors: [{ message: 'Code is incorrect' }] },
    }))
    const submitPassword = jest.fn(async () => ({ error: null }))
    renderScreen(activeContext({ verifyCode: verifyCodeMock, submitPassword }))

    await requestCode()
    await verifyCode('000000')

    expect(
      screen.getByTestId('forgot-password-error').props.accessibilityLiveRegion,
    ).toBe('assertive')
    expect(screen.getByText('Code is incorrect')).toBeOnTheScreen()
    expect(submitPassword).not.toHaveBeenCalled()
    expect(screen.queryByLabelText(PASSWORD_LABEL)).not.toBeOnTheScreen()
  })

  it('submits the new password, finalizes once, and navigates to /dashboard', async () => {
    const submitPassword = jest.fn(async () => ({ error: null }))
    const finalize = jest.fn(async (options: FinalizeOptions) => {
      options.navigate('/dashboard')
      return { pendingTask: false, error: null }
    })
    renderScreen(activeContext({ submitPassword }, finalize))

    await requestCode()
    await verifyCode()
    await submitNewPassword('new-password-123')

    expect(submitPassword).toHaveBeenCalledWith({ password: 'new-password-123' })
    expect(finalize).toHaveBeenCalledTimes(1)
    expect(router.replace).toHaveBeenCalledWith('/dashboard')
  })

  it('renders an accessible error and does not activate when the new password is rejected', async () => {
    const submitPassword = jest.fn(async () => ({
      error: { errors: [{ message: 'Password is too short' }] },
    }))
    const finalize = jest.fn()
    renderScreen(activeContext({ submitPassword }, finalize))

    await requestCode()
    await verifyCode()
    await submitNewPassword('short')

    expect(
      screen.getByTestId('forgot-password-error').props.accessibilityLiveRegion,
    ).toBe('assertive')
    expect(screen.getByText('Password is too short')).toBeOnTheScreen()
    expect(finalize).not.toHaveBeenCalled()
    expect(router.replace).not.toHaveBeenCalled()
  })

  it('renders an accessible error from a finalize failure without navigating', async () => {
    const finalize = jest.fn(async () => ({
      pendingTask: false,
      error: { message: 'Too many attempts' },
    }))
    renderScreen(activeContext({}, finalize))

    await requestCode()
    await verifyCode()
    await submitNewPassword()

    expect(
      screen.getByTestId('forgot-password-error').props.accessibilityLiveRegion,
    ).toBe('assertive')
    expect(screen.getByText('Too many attempts')).toBeOnTheScreen()
    expect(router.replace).not.toHaveBeenCalled()
  })

  it('routes a pending session task to /session-task instead of /dashboard', async () => {
    const finalize = jest.fn(async (options: FinalizeOptions) => {
      options.onSessionTask?.({ key: 'reset-password' })
      return { pendingTask: true, error: null }
    })
    renderScreen(activeContext({}, finalize))

    await requestCode()
    await verifyCode()
    await submitNewPassword()

    expect(router.replace).toHaveBeenCalledWith('/session-task')
    expect(router.replace).not.toHaveBeenCalledWith('/dashboard')
  })

  it('opens an external decorated URL through Linking, not the router', async () => {
    const openURL = jest.spyOn(Linking, 'openURL').mockResolvedValue(true)
    const finalize = jest.fn(async (options: FinalizeOptions) => {
      options.navigate('https://external.example.com/refresh')
      return { pendingTask: false, error: null }
    })
    renderScreen(activeContext({}, finalize))

    await requestCode()
    await verifyCode()
    await submitNewPassword()

    expect(openURL).toHaveBeenCalledWith('https://external.example.com/refresh')
    expect(router.replace).not.toHaveBeenCalled()
  })

  it('normalizes a thrown step error into an accessible error', async () => {
    const verifyCodeMock = jest.fn(async () => {
      throw new Error('network down')
    })
    renderScreen(activeContext({ verifyCode: verifyCodeMock }))

    await requestCode()
    await verifyCode()

    expect(
      screen.getByTestId('forgot-password-error').props.accessibilityLiveRegion,
    ).toBe('assertive')
    expect(screen.queryByLabelText(PASSWORD_LABEL)).not.toBeOnTheScreen()
  })

  it('returns to sign-in from the back link', () => {
    renderScreen(activeContext())

    fireEvent.press(screen.getByRole('button', { name: 'Back to sign in' }))

    expect(router.replace).toHaveBeenCalledWith('/sign-in')
  })
})