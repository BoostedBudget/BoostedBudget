import { setAuthTokenProvider } from '@boostedbudget/api-client/client'
import { router } from 'expo-router'
import { ReactNode, useEffect, useRef } from 'react'
import {
  reportApiError,
  setAuthRecoveryHandler,
} from '../bootstrap/api-runtime'
import { ToastProvider } from '../components/ui/ToastProvider'
import { ThemeProvider as ThemePreferenceProvider } from '../theme/ThemeProvider'
import { MobileAuthValue, setTokenErrorReporter, useMobileAuth } from './mobile-auth'

/**
 * Registration seam for the API-client token provider (todo 10).
 *
 * `AuthProviders` hands the adapter's `getToken` to this callback once the adapter reports
 * `isLoaded`. It is an observation/extension point: the ref-based provider registration itself
 * lives in `AuthProviders` (todo 10) and must NOT call `configureApiClient` a second time.
 */
export type TokenProviderSeam = (getToken: MobileAuthValue['getToken']) => void

/**
 * Observation seam for the 401 recovery surface (todo 11).
 *
 * `AuthProviders` hands the adapter's sign-out / token-error surface to this callback once the
 * adapter reports `isLoaded`. It is a notification/extension point only: the handler registration
 * itself lives in `AuthProviders` (todo 11). Inert when omitted.
 */
export type RecoveryHandlerSeam = (
  auth: Pick<MobileAuthValue, 'isSignedIn' | 'signOut' | 'reportTokenError'>,
) => void

export interface AuthProvidersProps {
  children: ReactNode
  /** Todo 10 fills this seam. Inert when omitted. */
  onTokenProviderReady?: TokenProviderSeam
  /** Todo 11 fills this seam. Inert when omitted. */
  onRecoveryReady?: RecoveryHandlerSeam
}

/**
 * The auth-aware provider bridge, mounted INSIDE `MobileAuthProvider`.
 *
 * It is the only place that reads `useMobileAuth()` to configure the rest of the provider tree:
 * it scopes the theme preference to the signed-in user, gates the app on the adapter's `isLoaded`
 * (the #691 race — children must not fire API calls before the token provider is registered),
 * mounts `ToastProvider`, registers the API-client token provider, and owns the 401 recovery
 * lifecycle: it routes the adapter's token-error sink into `reportApiError`, registers a one-shot
 * recovery handler while signed in, and clears it on sign-out and unmount. The two optional
 * callback props are notification/extension points, not the registration mechanism.
 */
export function AuthProviders({
  children,
  onTokenProviderReady,
  onRecoveryReady,
}: AuthProvidersProps) {
  const { isLoaded, userId, getToken, isSignedIn, signOut, reportTokenError } = useMobileAuth()

  // The registered wrapper must always call the LATEST adapter `getToken`. A ref is the only way
  // to guarantee that without re-registering the provider every time `getToken`'s identity
  // changes (the adapter recreates it on each auth-state render).
  const getTokenRef = useRef(getToken)
  // The adapter also recreates `signOut` on each auth-state render. Registration is gated on
  // `isSignedIn`, not on `signOut`'s identity, so the handler reads it through a ref.
  const signOutRef = useRef(signOut)

  useEffect(() => {
    getTokenRef.current = getToken
  }, [getToken])

  useEffect(() => {
    signOutRef.current = signOut
  }, [signOut])

  // Register once per `isLoaded` transition. The wrapper reads the ref, so it never captures a
  // stale `getToken` even though registration does not repeat when `getToken` changes. This is
  // NOT a second `configureApiClient` call — `initializeMobileRuntime` owns that.
  useEffect(() => {
    if (!isLoaded) return
    setAuthTokenProvider(async () => {
      try {
        return await getTokenRef.current()
      } catch (error) {
        reportTokenError(error)
        return null
      }
    })
  }, [isLoaded, reportTokenError])

  useEffect(() => {
    if (!isLoaded) return
    onTokenProviderReady?.(getToken)
    onRecoveryReady?.({ isSignedIn, signOut, reportTokenError })
  }, [
    isLoaded,
    getToken,
    isSignedIn,
    signOut,
    reportTokenError,
    onTokenProviderReady,
    onRecoveryReady,
  ])

  // The token-error sink and the one-shot 401 recovery handler share the signed-in lifecycle.
  // Token errors only originate from an authenticated token fetch, and `hasEverAuthenticated` in
  // the shared client is never reset — there is no public API for it — so clearing both on
  // sign-out (and on unmount) is what stops a stale handler from firing after sign-out. While
  // signed out the effect returns early and leaves any externally-installed sink untouched.
  useEffect(() => {
    if (!isSignedIn) {
      setAuthRecoveryHandler(null)
      return
    }
    setTokenErrorReporter(reportApiError)
    setAuthRecoveryHandler(() => {
      void signOutRef.current()
      router.replace('/(auth)/sign-in')
    })
    return () => {
      setAuthRecoveryHandler(null)
      setTokenErrorReporter(null)
    }
  }, [isSignedIn])

  return (
    <ThemePreferenceProvider userId={userId ?? undefined}>
      <ToastProvider>{isLoaded ? children : null}</ToastProvider>
    </ThemePreferenceProvider>
  )
}