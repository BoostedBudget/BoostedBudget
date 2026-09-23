import { ClerkProvider, useAuth, useClerk, useSession, useSignIn, useSignUp } from '@clerk/expo'
import { useSSO } from '@clerk/expo/experimental'
import { ReactNode, useCallback, useMemo } from 'react'
import {
  ClerkMobileAuthProviderProps,
  FinalizeOptions,
  FinalizeResult,
  MobileAuthContext,
  MobileAuthContextValue,
  reportTokenError,
} from './mobile-auth'

/**
 * The Clerk-backed implementation of the auth-adapter seam.
 *
 * This module is the ONLY production file allowed to import `@clerk/*` (besides the token cache).
 * It is loaded lazily by `mobile-auth.tsx` behind the `authAvailable` gate, so an inert bundle
 * never executes Clerk.
 *
 * Every member below is pinned against `@clerk/expo@4.6.8` in
 * `apps/mobile/docs/clerk-auth-api.md` (todo 5).
 */

type ClerkSignInResource = ReturnType<typeof useSignIn>['signIn']
type ClerkSignUpResource = ReturnType<typeof useSignUp>['signUp']

/**
 * The OAuth-only branch of the pinned `StartSSOFlowParams` union. The adapter's public
 * `strategy: string` is narrowed to this branch's strategy union at the call site; the
 * enterprise-SSO branch (which additionally requires `identifier`) is not part of this seam.
 */
type OAuthSSOParams = Extract<
  Parameters<ReturnType<typeof useSSO>['startSSOFlow']>[0],
  { identifier?: never }
>

/**
 * Faithfully wraps Clerk's `finalize({ navigate })` contract.
 *
 * Clerk passes `navigate` a `({ session, decorateUrl })` object — not a URL — and `finalize` does
 * NOT resolve `session.currentTask` itself. The callback inspects the task and diverts; otherwise
 * it hands the decorated destination to the consumer. `finalize`'s own `{ error }` is surfaced,
 * and a thrown error is normalized into `{ error }` too.
 */
async function finalizeWith(
  resource: ClerkSignInResource | ClerkSignUpResource,
  { destination, navigate, onSessionTask }: FinalizeOptions,
): Promise<FinalizeResult> {
  let pendingTask = false
  try {
    const { error } = await resource.finalize({
      navigate: ({ session, decorateUrl }) => {
        if (session.currentTask) {
          pendingTask = true
          onSessionTask?.(session.currentTask)
          return
        }
        // `decorateUrl` may return an external (`https://…`) URL for the Safari-ITP refresh; the
        // consumer decides how to open it. Hand it through unchanged.
        navigate(decorateUrl(destination))
      },
    })
    return { pendingTask, error: error ?? null }
  } catch (error) {
    return { pendingTask, error }
  }
}

/**
 * Reads every Clerk hook and assembles the adapter's context value. Exported so the compile-time
 * interface assertion in the unit tests can check it against `MobileAuthContextValue`.
 */
export function useClerkMobileAuthContext(): MobileAuthContextValue {
  const { isLoaded, isSignedIn, userId, getToken } = useAuth()
  const clerk = useClerk()
  const { session } = useSession()
  const { signIn } = useSignIn()
  const { signUp } = useSignUp()
  const { startSSOFlow } = useSSO()

  const activateSession = useCallback(
    async (sessionId: string | null): Promise<{ pendingTask: boolean }> => {
      if (sessionId === null) return { pendingTask: false }

      let pendingTask = false
      // Idempotent: re-activating an already-active session is a same-session re-activation. The
      // callback reads `currentTask` from its OWN argument (always fresh, never a closure).
      await clerk.setActive({
        session: sessionId,
        navigate: ({ session: activeSession, decorateUrl }) => {
          if (activeSession.currentTask) {
            pendingTask = true
            return
          }
          // Call `decorateUrl` so Clerk does not emit its "navigate callback did not call
          // decorateUrl" dev warning.
          decorateUrl('/dashboard')
        },
      })
      return { pendingTask }
    },
    [clerk],
  )

  const finalizeSignIn = useCallback(
    (options: FinalizeOptions) => finalizeWith(signIn, options),
    [signIn],
  )

  const finalizeSignUp = useCallback(
    (options: FinalizeOptions) => finalizeWith(signUp, options),
    [signUp],
  )

  const startSSO = useCallback(
    async ({ strategy, redirectUrl }: { strategy: string; redirectUrl: string }) => {
      const result = await startSSOFlow({
        strategy: strategy as OAuthSSOParams['strategy'],
        redirectUrl,
      })
      const { createdSessionId, authSessionResult } = result
      const cancelled =
        authSessionResult?.type === 'cancel' || authSessionResult?.type === 'dismiss'

      return {
        createdSessionId,
        // The pinned `@clerk/expo/experimental` hook self-activates the completed session.
        sessionActivated: createdSessionId !== null,
        cancelled,
        // Fresh read: `clerk.session` is the live object, unlike `useSession()`'s render-time
        // closure which is stale in the same tick.
        pendingTask: Boolean(clerk.session?.currentTask),
      }
    },
    [startSSOFlow, clerk],
  )

  return useMemo<MobileAuthContextValue>(
    () => ({
      auth: {
        isLoaded,
        // A pending-task session is not an active session, so it must not satisfy the route guard.
        isSignedIn: Boolean(isSignedIn) && !session?.currentTask,
        userId: userId ?? null,
        getToken: (options) => getToken(options),
        activateSession,
        signOut: () => clerk.signOut(),
        reportTokenError,
      },
      signIn: { isLoaded, signIn: signIn ?? null, finalize: finalizeSignIn },
      signUp: { isLoaded, signUp: signUp ?? null, finalize: finalizeSignUp },
      sso: { startSSOFlow: startSSO },
    }),
    [
      isLoaded,
      isSignedIn,
      userId,
      getToken,
      session,
      signIn,
      signUp,
      clerk,
      activateSession,
      finalizeSignIn,
      finalizeSignUp,
      startSSO,
    ],
  )
}

function ClerkAuthContextBridge({ children }: { children: ReactNode }) {
  const value = useClerkMobileAuthContext()
  return <MobileAuthContext.Provider value={value}>{children}</MobileAuthContext.Provider>
}

/** Mounts `ClerkProvider` and provides the adapter context to the tree. */
export function ClerkMobileAuthProvider({
  publishableKey,
  tokenCache,
  children,
}: ClerkMobileAuthProviderProps) {
  return (
    <ClerkProvider publishableKey={publishableKey} tokenCache={tokenCache}>
      <ClerkAuthContextBridge>{children}</ClerkAuthContextBridge>
    </ClerkProvider>
  )
}