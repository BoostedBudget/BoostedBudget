import { act, fireEvent, render, screen } from '@testing-library/react-native'
import { afterEach, beforeEach } from '@jest/globals'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { StyleSheet } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import type { ReactElement, ReactNode } from 'react'
import {
  MobileAuthContext,
  inertMobileAuthContextValue,
  type MobileAuthContextValue,
  type SignInHandle,
  type SignUpHandle,
} from '../../auth/mobile-auth'
import { ThemeProvider } from '../../theme/ThemeProvider'
import { IconButton } from '../../components/ui/IconButton'
import SignInScreen from '../../../app/(auth)/sign-in'
import SignUpScreen from '../../../app/(auth)/sign-up'
import ForgotPasswordScreen from '../../../app/(auth)/forgot-password'
import SessionTaskScreen from '../../../app/(auth)/session-task'

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
 * Todo 16 (#1657): the four auth screens' accessibility and state coverage.
 *
 * The primitive suites (`roles`, `touch-targets`, `dynamic-type`, `focus-order`, `announcements`)
 * pin the CONTRACT of the shared primitives. This suite pins that the auth screens actually USE
 * that contract: every control is reachable and named, every interactive node clears the 44px
 * touch floor, document order is the visual order, no screen disables dynamic type, and every
 * loading / error / empty / unavailable state renders without regression.
 *
 * The screens are adapter-backed, so they are driven through `MobileAuthContext` exactly as the
 * `src/test/auth/*` suites do — never through Clerk.
 */

const MOBILE_ROOT = dirname(require.resolve('../../../package.json'))
const TOUCH_MIN = nativeTokens.light.touchTarget.min

const AUTH_SCREEN_FILES = [
  'app/(auth)/sign-in.tsx',
  'app/(auth)/sign-up.tsx',
  'app/(auth)/forgot-password.tsx',
  'app/(auth)/session-task.tsx',
] as const

/**
 * Test-only slot for the failure-QA mutation. It is `null` in the committed suite; the failure
 * demonstration renders an unnamed `IconButton` here and the naming assertion goes red. It is
 * deliberately a module constant so the mutation is a one-line edit that is trivially reverted.
 */
const UNNAMED_CONTROL_FIXTURE: ReactNode = null

// ── tree walking ────────────────────────────────────────────────────────────────────────────────

interface JsonNode {
  type?: string
  props?: Record<string, unknown>
  children?: unknown
}

function walkTree(node: unknown, visit: (node: JsonNode) => void): void {
  if (!node || typeof node !== 'object') return
  if (Array.isArray(node)) {
    node.forEach((child) => walkTree(child, visit))
    return
  }
  const element = node as JsonNode
  visit(element)
  const children = element.children
  if (Array.isArray(children)) children.forEach((child) => walkTree(child, visit))
}

/** A control is a button-role node or a text input; both are focusable and must be named. */
function isInteractive(node: JsonNode): boolean {
  return node.props?.accessibilityRole === 'button' || node.type === 'TextInput'
}

function accessibleName(node: JsonNode): unknown {
  return node.props?.accessibilityLabel ?? node.props?.['aria-label']
}

function labelOf(node: JsonNode): string {
  return typeof node.props?.testID === 'string' ? node.props.testID : `<${node.type ?? 'node'}>`
}

/** Every interactive node whose accessible name is missing or blank. */
function unnamedControls(): string[] {
  const unnamed: string[] = []
  walkTree(screen.toJSON(), (node) => {
    if (!isInteractive(node)) return
    const name = accessibleName(node)
    if (typeof name !== 'string' || name.trim().length === 0) unnamed.push(labelOf(node))
  })
  return unnamed
}

/** Interactive testIDs in document order — the order a screen reader traverses them. */
function reachableTestIds(): string[] {
  const ids: string[] = []
  walkTree(screen.toJSON(), (node) => {
    if (!isInteractive(node)) return
    if (typeof node.props?.testID === 'string') ids.push(node.props.testID)
  })
  return ids
}

/** Interactive nodes whose resolved `minHeight` is below the 44px floor. */
function touchTargetViolations(): string[] {
  const violations: string[] = []
  walkTree(screen.toJSON(), (node) => {
    if (!isInteractive(node)) return
    const style = StyleSheet.flatten(node.props?.style) as Record<string, unknown> | undefined
    const minHeight = style?.minHeight
    if (typeof minHeight !== 'number' || minHeight < TOUCH_MIN) {
      violations.push(`${labelOf(node)}: minHeight=${String(minHeight)}`)
    }
  })
  return violations
}

/** Every rendered `Text` host node, for the dynamic-type render walk. */
function textNodes(): JsonNode[] {
  const nodes: JsonNode[] = []
  walkTree(screen.toJSON(), (node) => {
    if (node.type === 'Text') nodes.push(node)
  })
  return nodes
}

// ── contexts ────────────────────────────────────────────────────────────────────────────────────

function makeSignInHandle(overrides: Partial<SignInHandle> = {}): SignInHandle {
  return {
    status: 'needs_first_factor',
    createdSessionId: null,
    create: jest.fn(async () => ({ error: null })),
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
    ...overrides,
  }
}

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

/** A sign-up handle that needs an emailed code, so the screen advances to the verify step. */
function makeVerificationHandle(): MutableSignUpHandle {
  const handle = makeSignUpHandle()
  handle.password = jest.fn(async () => {
    handle.status = 'missing_requirements'
    handle.unverifiedFields = ['email_address']
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

function activeSignInContext(
  signIn: Partial<MobileAuthContextValue['signIn']> = {},
): MobileAuthContextValue {
  return makeContext({
    signIn: {
      isLoaded: true,
      signIn: makeSignInHandle(),
      finalize: jest.fn(async () => ({ pendingTask: false, error: null })),
      ...signIn,
    },
  })
}

function activeSignUpContext(
  signUp: Partial<MobileAuthContextValue['signUp']> = {},
): MobileAuthContextValue {
  return makeContext({
    signUp: {
      isLoaded: true,
      signUp: makeSignUpHandle(),
      finalize: jest.fn(async () => ({ pendingTask: false, error: null })),
      ...signUp,
    },
  })
}

function renderWithAuth(
  element: ReactElement,
  value: MobileAuthContextValue,
  extra?: ReactNode,
): ReturnType<typeof render> {
  return render(
    <ThemeProvider>
      <MobileAuthContext.Provider value={value}>
        {element}
        {extra}
      </MobileAuthContext.Provider>
    </ThemeProvider>,
  )
}

// ── control manifests ───────────────────────────────────────────────────────────────────────────

interface ControlSpec {
  testID: string
  name: string
  kind: 'button' | 'field'
}

const SIGN_IN_CONTROLS: ControlSpec[] = [
  { testID: 'sign-in-email', name: 'Email required', kind: 'field' },
  { testID: 'sign-in-password', name: 'Password required', kind: 'field' },
  { testID: 'sign-in-submit', name: 'Sign in', kind: 'button' },
  { testID: 'sign-in-sign-up-link', name: 'Create an account', kind: 'button' },
  { testID: 'sign-in-forgot-password-link', name: 'Forgot password?', kind: 'button' },
  { testID: 'sign-in-google', name: 'Continue with Google', kind: 'button' },
  { testID: 'sign-in-apple', name: 'Continue with Apple', kind: 'button' },
]

const SIGN_UP_CREDENTIAL_CONTROLS: ControlSpec[] = [
  { testID: 'sign-up-email', name: 'Email required', kind: 'field' },
  { testID: 'sign-up-password', name: 'Password required', kind: 'field' },
  { testID: 'sign-up-submit', name: 'Create account', kind: 'button' },
  { testID: 'sign-up-sign-in-link', name: 'Sign in', kind: 'button' },
]

const SIGN_UP_VERIFY_CONTROLS: ControlSpec[] = [
  { testID: 'sign-up-email', name: 'Email required', kind: 'field' },
  { testID: 'sign-up-password', name: 'Password required', kind: 'field' },
  { testID: 'sign-up-code', name: 'Verification code required', kind: 'field' },
  { testID: 'sign-up-verify', name: 'Verify', kind: 'button' },
  { testID: 'sign-up-resend', name: 'Resend code', kind: 'button' },
  { testID: 'sign-up-sign-in-link', name: 'Sign in', kind: 'button' },
]

const FORGOT_REQUEST_CONTROLS: ControlSpec[] = [
  { testID: 'forgot-password-email', name: 'Email required', kind: 'field' },
  { testID: 'forgot-password-send-code', name: 'Send reset code', kind: 'button' },
  { testID: 'forgot-password-back', name: 'Back to sign in', kind: 'button' },
]

const FORGOT_VERIFY_CONTROLS: ControlSpec[] = [
  { testID: 'forgot-password-code', name: 'Verification code required', kind: 'field' },
  { testID: 'forgot-password-verify-code', name: 'Verify code', kind: 'button' },
  { testID: 'forgot-password-back', name: 'Back to sign in', kind: 'button' },
]

const FORGOT_RESET_CONTROLS: ControlSpec[] = [
  { testID: 'forgot-password-password', name: 'New password required', kind: 'field' },
  { testID: 'forgot-password-submit', name: 'Set new password', kind: 'button' },
  { testID: 'forgot-password-back', name: 'Back to sign in', kind: 'button' },
]

const SESSION_TASK_CONTROLS: ControlSpec[] = [
  { testID: 'session-task-continue', name: 'Continue', kind: 'button' },
]

/** Resolve each declared control by its role/name and pin the testID it resolved to. */
function expectControls(specs: ControlSpec[]): void {
  for (const spec of specs) {
    const node =
      spec.kind === 'button'
        ? screen.getByRole('button', { name: spec.name })
        : screen.getByLabelText(spec.name)
    expect({ control: spec.testID, testID: node.props.testID }).toEqual({
      control: spec.testID,
      testID: spec.testID,
    })
  }
}

// ── interactions ────────────────────────────────────────────────────────────────────────────────

async function submitSignIn(email = 'user@example.com', password = 'hunter2'): Promise<void> {
  fireEvent.changeText(screen.getByLabelText('Email required'), email)
  fireEvent.changeText(screen.getByLabelText('Password required'), password)
  await act(async () => {
    fireEvent.press(screen.getByRole('button', { name: 'Sign in' }))
  })
}

async function submitSignUp(email = 'new@example.com', password = 'hunter2'): Promise<void> {
  fireEvent.changeText(screen.getByLabelText('Email required'), email)
  fireEvent.changeText(screen.getByLabelText('Password required'), password)
  await act(async () => {
    fireEvent.press(screen.getByRole('button', { name: 'Create account' }))
  })
}

async function requestResetCode(email = 'user@example.com'): Promise<void> {
  fireEvent.changeText(screen.getByLabelText('Email required'), email)
  await act(async () => {
    fireEvent.press(screen.getByRole('button', { name: 'Send reset code' }))
  })
}

async function verifyResetCode(code = '123456'): Promise<void> {
  fireEvent.changeText(screen.getByLabelText('Verification code required'), code)
  await act(async () => {
    fireEvent.press(screen.getByRole('button', { name: 'Verify code' }))
  })
}

beforeEach(() => {
  jest.clearAllMocks()
})

afterEach(() => {
  jest.restoreAllMocks()
})

// ── criterion 1: accessible names ───────────────────────────────────────────────────────────────

describe('auth screens: accessible names (criterion 1)', () => {
  it('names every control on sign-in', () => {
    renderWithAuth(<SignInScreen />, activeSignInContext(), UNNAMED_CONTROL_FIXTURE)
    expect(unnamedControls()).toEqual([])
    expectControls(SIGN_IN_CONTROLS)
  })

  it('names every control on sign-up (credentials step)', () => {
    renderWithAuth(<SignUpScreen />, activeSignUpContext())
    expect(unnamedControls()).toEqual([])
    expectControls(SIGN_UP_CREDENTIAL_CONTROLS)
  })

  it('names every control on sign-up (verify step)', async () => {
    renderWithAuth(<SignUpScreen />, activeSignUpContext({ signUp: makeVerificationHandle() }))
    await submitSignUp()
    expect(unnamedControls()).toEqual([])
    expectControls(SIGN_UP_VERIFY_CONTROLS)
  })

  it('names every control on forgot-password (request step)', () => {
    renderWithAuth(<ForgotPasswordScreen />, activeSignInContext())
    expect(unnamedControls()).toEqual([])
    expectControls(FORGOT_REQUEST_CONTROLS)
  })

  it('names every control on forgot-password (verify step)', async () => {
    renderWithAuth(<ForgotPasswordScreen />, activeSignInContext())
    await requestResetCode()
    expect(unnamedControls()).toEqual([])
    expectControls(FORGOT_VERIFY_CONTROLS)
  })

  it('names every control on forgot-password (reset step)', async () => {
    renderWithAuth(<ForgotPasswordScreen />, activeSignInContext())
    await requestResetCode()
    await verifyResetCode()
    expect(unnamedControls()).toEqual([])
    expectControls(FORGOT_RESET_CONTROLS)
  })

  it('names every control on session-task', () => {
    renderWithAuth(<SessionTaskScreen />, makeContext())
    expect(unnamedControls()).toEqual([])
    expectControls(SESSION_TASK_CONTROLS)
  })

  it('exposes each screen title as a header', () => {
    renderWithAuth(<SignInScreen />, activeSignInContext())
    expect(screen.getByRole('header', { name: 'Sign in' })).toBeOnTheScreen()
  })

  it('detects an unnamed icon button (non-vacuity guard)', () => {
    render(<IconButton testID="fixture-unnamed" label="" />)
    expect(unnamedControls()).toEqual(['fixture-unnamed'])
  })
})

// ── criterion 7: touch targets ──────────────────────────────────────────────────────────────────

describe('auth screens: touch targets (criterion 7)', () => {
  it('is not vacuous — the sign-in screen renders interactive controls', () => {
    renderWithAuth(<SignInScreen />, activeSignInContext())
    expect(reachableTestIds().length).toBeGreaterThanOrEqual(SIGN_IN_CONTROLS.length)
    expect(TOUCH_MIN).toBeGreaterThanOrEqual(44)
  })

  it('clears the 44px floor on every sign-in control', () => {
    renderWithAuth(<SignInScreen />, activeSignInContext())
    expect(touchTargetViolations()).toEqual([])
  })

  it('clears the 44px floor on every sign-up control (both steps)', async () => {
    renderWithAuth(<SignUpScreen />, activeSignUpContext({ signUp: makeVerificationHandle() }))
    expect(touchTargetViolations()).toEqual([])
    await submitSignUp()
    expect(touchTargetViolations()).toEqual([])
  })

  it('clears the 44px floor on every forgot-password control (all three steps)', async () => {
    renderWithAuth(<ForgotPasswordScreen />, activeSignInContext())
    expect(touchTargetViolations()).toEqual([])
    await requestResetCode()
    expect(touchTargetViolations()).toEqual([])
    await verifyResetCode()
    expect(touchTargetViolations()).toEqual([])
  })

  it('clears the 44px floor on the session-task control', () => {
    renderWithAuth(<SessionTaskScreen />, makeContext())
    expect(touchTargetViolations()).toEqual([])
  })
})

// ── criterion 2: focus order ────────────────────────────────────────────────────────────────────

describe('auth screens: focus order (criterion 2)', () => {
  it('traverses sign-in in visual order', () => {
    renderWithAuth(<SignInScreen />, activeSignInContext())
    expect(reachableTestIds()).toEqual([
      'sign-in-email',
      'sign-in-password',
      'sign-in-submit',
      'sign-in-sign-up-link',
      'sign-in-forgot-password-link',
      'sign-in-google',
      'sign-in-apple',
    ])
  })

  it('traverses sign-up credentials in visual order', () => {
    renderWithAuth(<SignUpScreen />, activeSignUpContext())
    expect(reachableTestIds()).toEqual([
      'sign-up-email',
      'sign-up-password',
      'sign-up-submit',
      'sign-up-sign-in-link',
    ])
  })

  it('traverses sign-up verify in visual order', async () => {
    renderWithAuth(<SignUpScreen />, activeSignUpContext({ signUp: makeVerificationHandle() }))
    await submitSignUp()
    expect(reachableTestIds()).toEqual([
      'sign-up-email',
      'sign-up-password',
      'sign-up-code',
      'sign-up-verify',
      'sign-up-resend',
      'sign-up-sign-in-link',
    ])
  })

  it('traverses forgot-password request in visual order', () => {
    renderWithAuth(<ForgotPasswordScreen />, activeSignInContext())
    expect(reachableTestIds()).toEqual([
      'forgot-password-email',
      'forgot-password-send-code',
      'forgot-password-back',
    ])
  })

  it('traverses forgot-password verify in visual order', async () => {
    renderWithAuth(<ForgotPasswordScreen />, activeSignInContext())
    await requestResetCode()
    expect(reachableTestIds()).toEqual([
      'forgot-password-code',
      'forgot-password-verify-code',
      'forgot-password-back',
    ])
  })

  it('traverses forgot-password reset in visual order', async () => {
    renderWithAuth(<ForgotPasswordScreen />, activeSignInContext())
    await requestResetCode()
    await verifyResetCode()
    expect(reachableTestIds()).toEqual([
      'forgot-password-password',
      'forgot-password-submit',
      'forgot-password-back',
    ])
  })

  it('traverses session-task in visual order', () => {
    renderWithAuth(<SessionTaskScreen />, makeContext())
    expect(reachableTestIds()).toEqual(['session-task-continue'])
  })
})

// ── criterion 4: dynamic type ───────────────────────────────────────────────────────────────────

describe('auth screens: dynamic type (criterion 4)', () => {
  it('never disables font scaling or caps the multiplier in any auth screen source', () => {
    const offenders: string[] = []
    for (const file of AUTH_SCREEN_FILES) {
      const source = readFileSync(resolve(MOBILE_ROOT, file), 'utf8')
      if (/allowFontScaling\s*[:=]\s*\{?\s*false/.test(source)) offenders.push(file)
      if (/maxFontSizeMultiplier\s*[:=]/.test(source)) offenders.push(`${file} (caps multiplier)`)
    }
    expect(offenders).toEqual([])
  })

  it('keeps every rendered text node scaling with the OS preference', () => {
    renderWithAuth(<SignInScreen />, activeSignInContext())
    const nodes = textNodes()
    expect(nodes.length).toBeGreaterThan(0)
    for (const node of nodes) {
      expect(node.props?.allowFontScaling).not.toBe(false)
      expect(node.props?.maxFontSizeMultiplier).toBeUndefined()
    }
  })

  it('keeps the session-task copy scaling with the OS preference', () => {
    renderWithAuth(<SessionTaskScreen />, makeContext())
    const nodes = textNodes()
    expect(nodes.length).toBeGreaterThan(0)
    for (const node of nodes) {
      expect(node.props?.allowFontScaling).not.toBe(false)
      expect(node.props?.maxFontSizeMultiplier).toBeUndefined()
    }
  })
})

// ── states: empty / unavailable / loading / error ───────────────────────────────────────────────

describe('auth screens: state coverage', () => {
  it('sign-in renders the empty form with no error or notice', () => {
    renderWithAuth(<SignInScreen />, activeSignInContext())
    expect(screen.queryByTestId('sign-in-error')).not.toBeOnTheScreen()
    expect(screen.queryByTestId('sign-in-unavailable')).not.toBeOnTheScreen()
    expect(screen.getByTestId('sign-in-submit').props.accessibilityState.disabled).toBe(false)
  })

  it('sign-in renders the inert notice and disables every control when unavailable', () => {
    renderWithAuth(<SignInScreen />, makeContext())
    const notice = screen.getByTestId('sign-in-unavailable')
    expect(notice.props.accessibilityRole).toBe('alert')
    expect(notice.props.accessibilityLiveRegion).toBe('assertive')
    expect(screen.getByTestId('sign-in-submit').props.accessibilityState.disabled).toBe(true)
    expect(screen.getByTestId('sign-in-email').props.accessibilityState.disabled).toBe(true)
    expect(screen.getByTestId('sign-in-google').props.accessibilityState.disabled).toBe(true)
  })

  it('sign-in disables controls without the notice while the adapter is not loaded', () => {
    renderWithAuth(<SignInScreen />, activeSignInContext({ isLoaded: false }))
    expect(screen.queryByTestId('sign-in-unavailable')).not.toBeOnTheScreen()
    expect(screen.getByTestId('sign-in-submit').props.accessibilityState.disabled).toBe(true)
  })

  it('sign-in exposes a busy submit control while a sign-in is in flight', async () => {
    let resolvePassword: (value: { error: null }) => void = () => {}
    const password = jest.fn(
      () =>
        new Promise<{ error: null }>((resolve) => {
          resolvePassword = resolve
        }),
    )
    renderWithAuth(<SignInScreen />, activeSignInContext({ signIn: makeSignInHandle({ password }) }))

    fireEvent.changeText(screen.getByLabelText('Email required'), 'user@example.com')
    fireEvent.changeText(screen.getByLabelText('Password required'), 'hunter2')
    await act(async () => {
      fireEvent.press(screen.getByRole('button', { name: 'Sign in' }))
    })

    const submit = screen.getByTestId('sign-in-submit')
    expect(submit.props.accessibilityState.busy).toBe(true)
    expect(submit.props.accessibilityState.disabled).toBe(true)

    await act(async () => {
      resolvePassword({ error: null })
    })
  })

  it('sign-in announces a rejected credential in an assertive live region', async () => {
    const password = jest.fn(async () => ({
      error: { errors: [{ message: 'Password is incorrect' }] },
    }))
    renderWithAuth(<SignInScreen />, activeSignInContext({ signIn: makeSignInHandle({ password }) }))

    await submitSignIn('user@example.com', 'wrong-password')

    const alert = screen.getByTestId('sign-in-error')
    expect(alert.props.accessibilityRole).toBe('alert')
    expect(alert.props.accessibilityLiveRegion).toBe('assertive')
    expect(screen.getByText('Password is incorrect')).toBeOnTheScreen()
  })

  it('sign-up renders the empty credentials form with no error or notice', () => {
    renderWithAuth(<SignUpScreen />, activeSignUpContext())
    expect(screen.queryByTestId('sign-up-error')).not.toBeOnTheScreen()
    expect(screen.queryByTestId('sign-up-notice')).not.toBeOnTheScreen()
    expect(screen.queryByTestId('sign-up-unavailable')).not.toBeOnTheScreen()
  })

  it('sign-up renders the inert notice and disables every control when unavailable', () => {
    renderWithAuth(<SignUpScreen />, makeContext())
    const notice = screen.getByTestId('sign-up-unavailable')
    expect(notice.props.accessibilityLiveRegion).toBe('assertive')
    expect(screen.getByTestId('sign-up-submit').props.accessibilityState.disabled).toBe(true)
    expect(screen.getByTestId('sign-up-email').props.accessibilityState.disabled).toBe(true)
  })

  it('sign-up announces the code-sent notice and freezes the credential fields', async () => {
    renderWithAuth(<SignUpScreen />, activeSignUpContext({ signUp: makeVerificationHandle() }))
    await submitSignUp()

    const notice = screen.getByTestId('sign-up-notice')
    expect(notice.props.accessibilityLiveRegion).toBe('assertive')
    expect(screen.getByText('We sent a verification code to your email.')).toBeOnTheScreen()
    expect(screen.getByTestId('sign-up-email').props.accessibilityState.disabled).toBe(true)
    expect(screen.getByTestId('sign-up-verify').props.accessibilityState.disabled).toBe(false)
  })

  it('sign-up disables the resend control while a resend is in flight', async () => {
    let resolveResend: (value: { error: null }) => void = () => {}
    let sendCalls = 0
    const handle = makeVerificationHandle()
    handle.verifications.sendEmailCode = jest.fn(() => {
      sendCalls += 1
      if (sendCalls === 1) return Promise.resolve({ error: null })
      return new Promise<{ error: null }>((resolve) => {
        resolveResend = resolve
      })
    })
    renderWithAuth(<SignUpScreen />, activeSignUpContext({ signUp: handle }))

    await submitSignUp()

    await act(async () => {
      fireEvent.press(screen.getByRole('button', { name: 'Resend code' }))
    })

    const resend = screen.getByTestId('sign-up-resend')
    expect(resend.props.accessibilityState.disabled).toBe(true)

    await act(async () => {
      resolveResend({ error: null })
    })
  })

  it('sign-up announces a rejected credential in an assertive live region', async () => {
    const signUp = makeSignUpHandle({
      password: jest.fn(async () => ({ error: { message: 'Email already taken' } })),
    })
    renderWithAuth(<SignUpScreen />, activeSignUpContext({ signUp }))

    await submitSignUp()

    const alert = screen.getByTestId('sign-up-error')
    expect(alert.props.accessibilityLiveRegion).toBe('assertive')
    expect(screen.getByText('Email already taken')).toBeOnTheScreen()
  })

  it('forgot-password renders the empty request form with no error or notice', () => {
    renderWithAuth(<ForgotPasswordScreen />, activeSignInContext())
    expect(screen.queryByTestId('forgot-password-error')).not.toBeOnTheScreen()
    expect(screen.queryByTestId('forgot-password-unavailable')).not.toBeOnTheScreen()
  })

  it('forgot-password renders the inert notice and disables every control when unavailable', () => {
    renderWithAuth(<ForgotPasswordScreen />, makeContext())
    const notice = screen.getByTestId('forgot-password-unavailable')
    expect(notice.props.accessibilityLiveRegion).toBe('assertive')
    expect(screen.getByTestId('forgot-password-send-code').props.accessibilityState.disabled).toBe(
      true,
    )
    expect(screen.getByTestId('forgot-password-email').props.accessibilityState.disabled).toBe(true)
  })

  it('forgot-password announces a rejected reset code in an assertive live region', async () => {
    const signIn = makeSignInHandle({
      resetPasswordEmailCode: {
        sendCode: jest.fn(async () => ({ error: null })),
        verifyCode: jest.fn(async () => ({ error: { message: 'Code is invalid' } })),
        submitPassword: jest.fn(async () => ({ error: null })),
      },
    })
    renderWithAuth(<ForgotPasswordScreen />, activeSignInContext({ signIn }))

    await requestResetCode()
    await verifyResetCode('000000')

    const alert = screen.getByTestId('forgot-password-error')
    expect(alert.props.accessibilityLiveRegion).toBe('assertive')
    expect(screen.getByText('Code is invalid')).toBeOnTheScreen()
  })

  it('session-task renders its single continue action with no error state', () => {
    renderWithAuth(<SessionTaskScreen />, makeContext())
    expect(screen.getByTestId('session-task-continue').props.accessibilityState.disabled).toBe(false)
    expect(screen.queryByTestId('session-task-error')).not.toBeOnTheScreen()
  })
})