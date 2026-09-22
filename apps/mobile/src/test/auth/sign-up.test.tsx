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
  type SignUpHandle,
} from '../../auth/mobile-auth'
import { ThemeProvider } from '../../theme/ThemeProvider'
import SignUpScreen from '../../../app/(auth)/sign-up'

// The real provider hydrates asynchronously from AsyncStorage; `Screen`/`ScreenHeader` only need a
// resolved theme, so a pass-through provider is enough and keeps every render free of `act` noise.
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
 * The sign-up screen is adapter-backed: these tests drive it through the adapter's own context
 * (`MobileAuthContext`), never through Clerk. That keeps the unit tier free of `@clerk/expo` — the
 * adapter seam is exactly what the screen is allowed to consume.
 *
 * The screen must route on the Core 3 `signUp` surface pinned in todo 5: `status`,
 * `unverifiedFields`, `missingFields` and `createdSessionId`. The fake handle below is mutable so a
 * mocked `password`/`verifyEmailCode` can advance that surface the way Clerk would.
 */

const CODE_SENT_MESSAGE = 'We sent a verification code to your email.'
const MISSING_FIELDS_MESSAGE = 'Your account is missing required information.'

const EMAIL_LABEL = 'Email required'
const PASSWORD_LABEL = 'Password required'
const CODE_LABEL = 'Verification code required'

interface MutableSignUpHandle {
  status: SignUpHandle['status']
  createdSessionId: string | null
  missingFields: unknown[]
  unverifiedFields: unknown[]
  password: SignUpHandle['password']
  verifications: SignUpHandle['verifications']
  reset: SignUpHandle['reset']
}

function makeSignUpHandle(overrides: Partial<MutableSignUpHandle> = {}): MutableSignUpHandle {
  return {
    status: 'missing_requirements',
    createdSessionId: null,
    missingFields: [],
    unverifiedFields: [],
    password: jest.fn(async () => ({ error: null })),
    verifications: {
      sendEmailCode: jest.fn(async () => ({ error: null })),
      verifyEmailCode: jest.fn(async () => ({ error: null })),
    },
    reset: jest.fn(async () => ({ error: null })),
    ...overrides,
  }
}

/** A handle that needs an email code, then completes once the correct code is verified. */
function makeVerificationHandle(): MutableSignUpHandle {
  const handle = makeSignUpHandle()
  handle.password = jest.fn(async () => {
    handle.status = 'missing_requirements'
    handle.unverifiedFields = ['email_address']
    return { error: null }
  })
  handle.verifications.verifyEmailCode = jest.fn(async () => {
    handle.status = 'complete'
    handle.createdSessionId = 'sess_abc'
    return { error: null }
  })
  return handle
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
        <SignUpScreen />
      </MobileAuthContext.Provider>
    </ThemeProvider>,
  )
}

function makeFinalize() {
  return jest.fn(async (options: FinalizeOptions) => {
    options.navigate('/dashboard')
    return { pendingTask: false, error: null }
  })
}

/** Fill both credential fields and press "Create account", awaiting the async handler. */
async function submitCredentials(email = 'new@example.com', password = 'hunter2') {
  fireEvent.changeText(screen.getByLabelText(EMAIL_LABEL), email)
  fireEvent.changeText(screen.getByLabelText(PASSWORD_LABEL), password)
  await act(async () => {
    fireEvent.press(screen.getByRole('button', { name: 'Create account' }))
  })
}

/** Fill the code field and press "Verify", awaiting the async handler. */
async function submitCode(code = '123456') {
  fireEvent.changeText(screen.getByLabelText(CODE_LABEL), code)
  await act(async () => {
    fireEvent.press(screen.getByRole('button', { name: 'Verify' }))
  })
}

beforeEach(() => {
  jest.clearAllMocks()
})

afterEach(() => {
  jest.restoreAllMocks()
})

describe('sign-up screen', () => {
  it('renders named email, password, create-account and sign-in controls', () => {
    renderScreen(makeContext({ signUp: { isLoaded: true, signUp: makeSignUpHandle(), finalize: jest.fn() } }))

    expect(screen.getByLabelText(EMAIL_LABEL)).toBeOnTheScreen()
    expect(screen.getByLabelText(PASSWORD_LABEL)).toBeOnTheScreen()
    expect(screen.getByRole('button', { name: 'Create account' })).toBeOnTheScreen()
    expect(screen.getByRole('button', { name: 'Sign in' })).toBeOnTheScreen()
  })

  it('renders the real form disabled with an accessible notice in inert mode', () => {
    renderScreen(makeContext())

    const email = screen.getByTestId('sign-up-email')
    expect(email.props.editable).toBe(false)
    expect(email.props.accessibilityState.disabled).toBe(true)
    expect(screen.getByTestId('sign-up-submit').props.accessibilityState.disabled).toBe(true)

    expect(screen.getByText('sign-up is unavailable in this build')).toBeOnTheScreen()
    expect(screen.getByTestId('sign-up-unavailable').props.accessibilityLiveRegion).toBe('assertive')
  })

  it('disables the controls, without the notice, while the adapter is not loaded', () => {
    renderScreen(
      makeContext({ signUp: { isLoaded: false, signUp: makeSignUpHandle(), finalize: jest.fn() } }),
    )

    expect(screen.getByTestId('sign-up-submit').props.accessibilityState.disabled).toBe(true)
    expect(screen.queryByText('sign-up is unavailable in this build')).not.toBeOnTheScreen()
  })

  it('creates the account, sends a code, and shows the verification step without finalizing yet', async () => {
    const handle = makeVerificationHandle()
    const finalize = jest.fn()
    renderScreen(makeContext({ signUp: { isLoaded: true, signUp: handle, finalize } }))

    await submitCredentials()

    expect(handle.password).toHaveBeenCalledWith({
      password: 'hunter2',
      emailAddress: 'new@example.com',
    })
    expect(handle.verifications.sendEmailCode).toHaveBeenCalledTimes(1)
    expect(screen.getByLabelText(CODE_LABEL)).toBeOnTheScreen()
    expect(screen.getByText(CODE_SENT_MESSAGE)).toBeOnTheScreen()
    expect(screen.getByTestId('sign-up-notice').props.accessibilityLiveRegion).toBe('assertive')
    expect(finalize).not.toHaveBeenCalled()
  })

  it('verifies a correct code and finalizes exactly once, navigating to /dashboard', async () => {
    const handle = makeVerificationHandle()
    const finalize = makeFinalize()
    renderScreen(makeContext({ signUp: { isLoaded: true, signUp: handle, finalize } }))

    await submitCredentials()
    await submitCode('654321')

    expect(handle.verifications.verifyEmailCode).toHaveBeenCalledWith({ code: '654321' })
    expect(finalize).toHaveBeenCalledTimes(1)
    expect(finalize).toHaveBeenCalledWith(
      expect.objectContaining({ destination: '/dashboard', navigate: expect.any(Function) }),
    )
    expect(router.replace).toHaveBeenCalledWith('/dashboard')
  })

  it('routes a pending session task to /session-task instead of dead-ending on /dashboard', async () => {
    const handle = makeVerificationHandle()
    const finalize = jest.fn(async (options: FinalizeOptions) => {
      options.onSessionTask?.({ key: 'setup-mfa' })
      return { pendingTask: true, error: null }
    })
    renderScreen(makeContext({ signUp: { isLoaded: true, signUp: handle, finalize } }))

    await submitCredentials()
    await submitCode()

    expect(finalize).toHaveBeenCalledTimes(1)
    expect(router.replace).toHaveBeenCalledWith('/session-task')
    expect(router.replace).not.toHaveBeenCalledWith('/dashboard')
  })

  it('renders an accessible error for a rejected code and keeps the form interactive', async () => {
    const handle = makeVerificationHandle()
    handle.verifications.verifyEmailCode = jest.fn(async () => ({
      error: { errors: [{ message: 'Code is incorrect' }] },
    }))
    const finalize = makeFinalize()
    renderScreen(makeContext({ signUp: { isLoaded: true, signUp: handle, finalize } }))

    await submitCredentials()
    await submitCode('000000')

    const alert = screen.getByTestId('sign-up-error')
    expect(alert.props.accessibilityLiveRegion).toBe('assertive')
    expect(screen.getByText('Code is incorrect')).toBeOnTheScreen()
    expect(finalize).not.toHaveBeenCalled()
    expect(router.replace).not.toHaveBeenCalled()

    // The form must remain usable: the verify control is enabled and a retry reaches Clerk again.
    expect(screen.getByTestId('sign-up-verify').props.accessibilityState.disabled).toBe(false)
    await submitCode('111111')
    expect(handle.verifications.verifyEmailCode).toHaveBeenCalledTimes(2)
  })

  it('renders an accessible error from a finalize failure without navigating', async () => {
    const handle = makeVerificationHandle()
    const finalize = jest.fn(async () => ({ pendingTask: false, error: { message: 'Too many attempts' } }))
    renderScreen(makeContext({ signUp: { isLoaded: true, signUp: handle, finalize } }))

    await submitCredentials()
    await submitCode()

    expect(screen.getByTestId('sign-up-error').props.accessibilityLiveRegion).toBe('assertive')
    expect(screen.getByText('Too many attempts')).toBeOnTheScreen()
    expect(router.replace).not.toHaveBeenCalled()
  })

  it('opens an external decorated URL through Linking, not the router', async () => {
    const openURL = jest.spyOn(Linking, 'openURL').mockResolvedValue(true)
    const handle = makeVerificationHandle()
    const finalize = jest.fn(async (options: FinalizeOptions) => {
      options.navigate('https://external.example.com/refresh')
      return { pendingTask: false, error: null }
    })
    renderScreen(makeContext({ signUp: { isLoaded: true, signUp: handle, finalize } }))

    await submitCredentials()
    await submitCode()

    expect(openURL).toHaveBeenCalledWith('https://external.example.com/refresh')
    expect(router.replace).not.toHaveBeenCalled()
  })

  it('routes on missingFields: surfaces an error, skips the code step and does not finalize', async () => {
    const handle = makeSignUpHandle()
    handle.password = jest.fn(async () => {
      handle.status = 'missing_requirements'
      handle.missingFields = ['phone_number']
      handle.unverifiedFields = []
      return { error: null }
    })
    const finalize = jest.fn()
    renderScreen(makeContext({ signUp: { isLoaded: true, signUp: handle, finalize } }))

    await submitCredentials()

    expect(screen.getByText(MISSING_FIELDS_MESSAGE)).toBeOnTheScreen()
    expect(screen.queryByLabelText(CODE_LABEL)).not.toBeOnTheScreen()
    expect(handle.verifications.sendEmailCode).not.toHaveBeenCalled()
    expect(finalize).not.toHaveBeenCalled()
  })

  it('finalizes immediately when the password step already completes the sign-up', async () => {
    const handle = makeSignUpHandle()
    handle.password = jest.fn(async () => {
      handle.status = 'complete'
      handle.createdSessionId = 'sess_done'
      return { error: null }
    })
    const finalize = makeFinalize()
    renderScreen(makeContext({ signUp: { isLoaded: true, signUp: handle, finalize } }))

    await submitCredentials()

    expect(finalize).toHaveBeenCalledTimes(1)
    expect(screen.queryByLabelText(CODE_LABEL)).not.toBeOnTheScreen()
    expect(router.replace).toHaveBeenCalledWith('/dashboard')
  })

  it('resends the verification code from the verification step', async () => {
    const handle = makeVerificationHandle()
    renderScreen(
      makeContext({ signUp: { isLoaded: true, signUp: handle, finalize: makeFinalize() } }),
    )

    await submitCredentials()
    await act(async () => {
      fireEvent.press(screen.getByRole('button', { name: 'Resend code' }))
    })

    expect(handle.verifications.sendEmailCode).toHaveBeenCalledTimes(2)
  })

  it('normalizes a thrown submission error into an accessible error', async () => {
    const handle = makeSignUpHandle()
    handle.password = jest.fn(async () => {
      throw new Error('network down')
    })
    renderScreen(
      makeContext({ signUp: { isLoaded: true, signUp: handle, finalize: jest.fn() } }),
    )

    await submitCredentials()

    expect(screen.getByTestId('sign-up-error').props.accessibilityLiveRegion).toBe('assertive')
  })
})
