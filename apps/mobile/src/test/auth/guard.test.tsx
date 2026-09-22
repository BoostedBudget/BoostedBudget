import { render, screen } from '@testing-library/react-native'
import { beforeEach } from '@jest/globals'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import type { ReactNode } from 'react'
import {
  MobileAuthContext,
  inertMobileAuthContextValue,
  type MobileAuthContextValue,
  type MobileAuthValue,
} from '../../auth/mobile-auth'
import AppLayout from '../../../app/(app)/_layout'
import DashboardScreen from '../../../app/(app)/dashboard'

/**
 * The `(app)` guard is adapter-backed: these tests drive it through `MobileAuthContext`, never
 * through Clerk. `Redirect` is replaced with a recorder so the redirect TARGET is assertable —
 * the global mock renders it as `null`, which would make "a redirect was produced" unobservable.
 */
const mockRedirectTargets: string[] = []

jest.mock('expo-router', () => ({
  Redirect: ({ href }: { href: string }) => {
    mockRedirectTargets.push(href)
    return null
  },
  Stack: ({ children }: { children?: ReactNode }) => children ?? null,
}))

// The real provider hydrates asynchronously from AsyncStorage; the dashboard only needs a
// resolved theme, so a pass-through provider keeps these tests synchronous.
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

beforeEach(() => {
  mockRedirectTargets.length = 0
})

function makeContext(auth: Partial<MobileAuthValue> = {}): MobileAuthContextValue {
  return {
    ...inertMobileAuthContextValue,
    auth: { ...inertMobileAuthContextValue.auth, ...auth },
  }
}

function renderGuard(value: MobileAuthContextValue) {
  return render(
    <MobileAuthContext.Provider value={value}>
      <AppLayout />
    </MobileAuthContext.Provider>,
  )
}

function renderDashboard(value: MobileAuthContextValue) {
  return render(
    <MobileAuthContext.Provider value={value}>
      <DashboardScreen />
    </MobileAuthContext.Provider>,
  )
}

describe('(app) route guard', () => {
  it('renders neither guarded content nor a redirect while the adapter is not loaded', () => {
    renderGuard(makeContext({ isLoaded: false }))

    expect(mockRedirectTargets).toEqual([])
    expect(screen.queryByTestId('dashboard-screen')).not.toBeOnTheScreen()
    expect(screen.toJSON()).toBeNull()
  })

  it('redirects a signed-out user to the sign-in route', () => {
    renderGuard(makeContext({ isLoaded: true, isSignedIn: false, userId: null }))

    expect(mockRedirectTargets).toEqual(['/(auth)/sign-in'])
    expect(screen.queryByTestId('dashboard-screen')).not.toBeOnTheScreen()
  })

  it('redirects the inert adapter (no publishable key) to the sign-in route', () => {
    renderGuard(inertMobileAuthContextValue)

    expect(mockRedirectTargets).toEqual(['/(auth)/sign-in'])
  })

  it('renders the guarded stack without redirecting a signed-in user', () => {
    renderGuard(makeContext({ isLoaded: true, isSignedIn: true, userId: 'user_1' }))

    expect(mockRedirectTargets).toEqual([])
  })

  it('never uses the Protected stack variant (open iOS defect expo/expo#37305)', () => {
    const source = readFileSync(resolve(MOBILE_ROOT, 'app/(app)/_layout.tsx'), 'utf8')

    expect(source).not.toContain('Stack.Protected')
  })
})

describe('(app) dashboard placeholder', () => {
  it('renders the signed-in identity and a sign-out control', () => {
    renderDashboard(makeContext({ isLoaded: true, isSignedIn: true, userId: 'user_1' }))

    expect(screen.getByTestId('dashboard-screen')).toBeOnTheScreen()
    expect(screen.getByText('user_1')).toBeOnTheScreen()
    expect(screen.getByRole('button', { name: 'Sign out' })).toBeOnTheScreen()
  })
})