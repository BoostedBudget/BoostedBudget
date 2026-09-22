jest.unmock('expo-router')

import { afterEach, describe, expect, it } from '@jest/globals'
import { act } from '@testing-library/react-native'
import { Stack } from 'expo-router'
import { cleanup, renderRouter, screen } from 'expo-router/testing-library'
import type { ReactNode } from 'react'
import AppLayout from '../../../app/(app)/_layout'
import DashboardScreen from '../../../app/(app)/dashboard'
import AuthLayout from '../../../app/(auth)/_layout'
import SignInScreen from '../../../app/(auth)/sign-in'
import HomeScreen from '../../../app/index'
import ReadinessScreen from '../../../app/readiness'
import GalleryScreen from '../../../app/gallery'
import {
  MobileAuthContext,
  inertMobileAuthContextValue,
  type MobileAuthContextValue,
  type MobileAuthValue,
  type SignInHandle,
} from '../../auth/mobile-auth'
import { ThemeProvider } from '../../theme/ThemeProvider'

/**
 * The route-guard state machine, exercised over the REAL router tree.
 *
 * `guard.test.tsx` drives the guard component in isolation with a recording `Redirect`. This suite
 * is the integration counterpart: it mounts the real `(app)` and `(auth)` layouts and the real
 * screens through `expo-router`'s own navigation, so a redirect is observed as an actual pathname
 * change and "no guarded content" is observed as the absence of the rendered screen.
 *
 * The auth adapter is injected through `MobileAuthContext` (the seam the guard consumes) rather
 * than through Clerk: the four states are adapter states, and the adapter's own Clerk wiring is
 * pinned by `mobile-auth.test.tsx` / `auth-providers.test.tsx`. Injecting the context is what lets
 * all four states — including the inert build — be driven from one file.
 */

function RootLayout() {
  return <Stack screenOptions={{ headerShown: false }} />
}

const ROUTES = {
  _layout: RootLayout,
  index: HomeScreen,
  readiness: ReadinessScreen,
  gallery: GalleryScreen,
  '(app)/_layout': AppLayout,
  '(app)/dashboard': DashboardScreen,
  '(auth)/_layout': AuthLayout,
  '(auth)/sign-in': SignInScreen,
}

/** A minimal, non-null sign-in handle so the signed-out screen renders its real form. */
const signInStub: SignInHandle = {
  status: 'needs_identifier',
  createdSessionId: null,
  password: async () => ({ error: null }),
  emailCode: {
    sendCode: async () => ({ error: null }),
    verifyCode: async () => ({ error: null }),
  },
  resetPasswordEmailCode: {
    sendCode: async () => ({ error: null }),
    verifyCode: async () => ({ error: null }),
    submitPassword: async () => ({ error: null }),
  },
  reset: async () => ({ error: null }),
}

/** A Clerk-backed adapter value with the given auth state; `signIn` is present (loaded). */
function clerkContext(auth: Partial<MobileAuthValue>): MobileAuthContextValue {
  return {
    ...inertMobileAuthContextValue,
    auth: { ...inertMobileAuthContextValue.auth, ...auth },
    signIn: {
      isLoaded: auth.isLoaded ?? true,
      signIn: signInStub,
      finalize: async () => ({ pendingTask: false, error: null }),
    },
  }
}

const LOADING = clerkContext({ isLoaded: false, isSignedIn: false, userId: null })
const SIGNED_OUT = clerkContext({ isLoaded: true, isSignedIn: false, userId: null })
const SIGNED_IN = clerkContext({ isLoaded: true, isSignedIn: true, userId: 'user_1' })
const INERT = inertMobileAuthContextValue

function wrapperFor(value: MobileAuthContextValue) {
  return function AuthWrapper({ children }: { children: ReactNode }) {
    return (
      <ThemeProvider>
        <MobileAuthContext.Provider value={value}>{children}</MobileAuthContext.Provider>
      </ThemeProvider>
    )
  }
}

function renderAt(value: MobileAuthContextValue, initialUrl: string) {
  return renderRouter(ROUTES, { initialUrl, wrapper: wrapperFor(value) })
}

afterEach(() => {
  cleanup()
  jest.useRealTimers()
})

describe('(app) guard state machine over the real router tree', () => {
  it('loading: renders neither guarded content nor a redirect', async () => {
    const router = renderAt(LOADING, '/dashboard')
    await act(async () => {})

    expect(router.getPathname()).toBe('/dashboard')
    expect(screen.queryByTestId('dashboard-screen')).not.toBeOnTheScreen()
    expect(screen.queryByTestId('sign-in-screen')).not.toBeOnTheScreen()
  })

  it('signed-out: lands on the (auth) sign-in route', async () => {
    const router = renderAt(SIGNED_OUT, '/dashboard')
    await act(async () => {})

    expect(router.getPathname()).toBe('/sign-in')
    expect(screen.getByTestId('sign-in-screen')).toBeOnTheScreen()
    expect(screen.queryByTestId('dashboard-screen')).not.toBeOnTheScreen()
  })

  it('inert: lands on the (auth) sign-in route', async () => {
    const router = renderAt(INERT, '/dashboard')
    await act(async () => {})

    expect(router.getPathname()).toBe('/sign-in')
    expect(screen.getByTestId('sign-in-screen')).toBeOnTheScreen()
    expect(screen.queryByTestId('dashboard-screen')).not.toBeOnTheScreen()
  })

  it('signed-in: lands on /dashboard and renders the guarded content', async () => {
    const router = renderAt(SIGNED_IN, '/dashboard')
    await act(async () => {})

    expect(router.getPathname()).toBe('/dashboard')
    expect(screen.getByTestId('dashboard-screen')).toBeOnTheScreen()
    expect(screen.queryByTestId('sign-in-screen')).not.toBeOnTheScreen()
  })
})

describe('public diagnostic routes are unaffected by the guard state', () => {
  const states: Array<[string, MobileAuthContextValue]> = [
    ['loading', LOADING],
    ['signed-out', SIGNED_OUT],
    ['signed-in', SIGNED_IN],
    ['inert', INERT],
  ]

  const publicRoutes: Array<[string, () => void]> = [
    ['/', () => expect(screen.getByRole('header', { name: 'BoostedBudget' })).toBeOnTheScreen()],
    ['/readiness', () => expect(screen.getByRole('header', { name: 'App readiness' })).toBeOnTheScreen()],
    ['/gallery', () => expect(screen.getByTestId('gallery')).toBeOnTheScreen()],
  ]

  for (const [stateName, value] of states) {
    for (const [url, assertContent] of publicRoutes) {
      it(`${stateName}: ${url} stays reachable`, async () => {
        const router = renderAt(value, url)
        await act(async () => {})

        expect(router.getPathname()).toBe(url)
        assertContent()
      })
    }
  }
})