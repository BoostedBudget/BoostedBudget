import { setAuthTokenProvider } from '@boostedbudget/api-client/client'
import { ReactNode, useEffect, useRef } from 'react'
import { ToastProvider } from '../components/ui/ToastProvider'
import { ThemeProvider as ThemePreferenceProvider } from '../theme/ThemeProvider'
import { MobileAuthValue, useMobileAuth } from './mobile-auth'

/**
 * Registration seam for the API-client token provider (todo 10).
 *
 * `AuthProviders` hands the adapter's `getToken` to this callback once the adapter reports
 * `isLoaded`. It is an observation/extension point: the ref-based provider registration itself
 * lives in `AuthProviders` (todo 10) and must NOT call `configureApiClient` a second time.
 */
export type TokenProviderSeam = (getToken: MobileAuthValue['getToken']) => void

/**
 * Registration seam for the 401 recovery handler (todo 11).
 *
 * `AuthProviders` hands the adapter's sign-out / token-error surface to this callback once the
 * adapter reports `isLoaded`. Inert today: no default implementation.
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
 * (the #691 race — children must not fire API calls before the token provider is registered), and
 * mounts `ToastProvider`. The two registration seams are extension points for todos 10 and 11.
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

  useEffect(() => {
    getTokenRef.current = getToken
  }, [getToken])

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

  return (
    <ThemePreferenceProvider userId={userId ?? undefined}>
      <ToastProvider>{isLoaded ? children : null}</ToastProvider>
    </ThemePreferenceProvider>
  )
}