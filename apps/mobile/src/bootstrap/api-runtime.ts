import type { ApiClientErrorDetails } from '@boostedbudget/api-client/client'

/**
 * The mobile API-runtime bridges (todo 11, #1657).
 *
 * The shared client (`@boostedbudget/api-client/client`) owns the 401 policy: it calls
 * `onUnauthorized` only for a non-public path and only after a request has carried a token
 * (`hasEverAuthenticated`). That flag is set by `customInstance` and is reset **only** by
 * `configureApiClient`, which `initializeMobileRuntime` calls exactly once — there is no public
 * reset. So `hasEverAuthenticated` keeps the client willing to report a 401 even after the user
 * signs out, and it cannot be the mechanism that stops post-sign-out recovery.
 *
 * The mechanism is instead the lifecycle of the handler registered here: `AuthProviders` registers
 * a handler only while `isSignedIn` is true and clears it when the session signs out (and on
 * unmount), and `recover` clears the handler before invoking it. A cleared handler makes `recover`
 * a no-op, so a 401 that arrives after sign-out dispatches nothing.
 */

/** A handler that recovers the session after the API reports an unauthorized response. */
export type AuthRecoveryHandler = (details: ApiClientErrorDetails) => void

/**
 * The error sink. It accepts `unknown` because two sources funnel through it: intercept 401/5xx
 * details from the shared client, and arbitrary token-resolution failures from the adapter's
 * `reportTokenError` sink. A permissive parameter keeps the single reporter usable for both.
 */
export type ApiErrorReporter = (details: unknown) => void

let authRecoveryHandler: AuthRecoveryHandler | null = null
let apiErrorReporter: ApiErrorReporter | null = null

/**
 * Registers (or, with `null`, clears) the recovery handler.
 *
 * `AuthProviders` owns this registration: it sets a handler while signed in and clears it when the
 * session signs out or the provider unmounts.
 */
export function setAuthRecoveryHandler(handler: AuthRecoveryHandler | null): void {
  authRecoveryHandler = handler
}

/**
 * Dispatches a single unauthorized response to the registered handler.
 *
 * The handler is cleared BEFORE it is invoked — the one-shot re-entrancy guard. Two 401s that
 * arrive together therefore dispatch exactly once: the first call takes the handler and nulls the
 * slot, the second finds nothing. When no handler is registered (a signed-out tree) this is a
 * no-op.
 */
export function recover(details: ApiClientErrorDetails): void {
  const handler = authRecoveryHandler
  authRecoveryHandler = null
  handler?.(details)
}

/** Registers (or, with `null`, clears) the error reporter. */
export function setApiErrorReporter(reporter: ApiErrorReporter | null): void {
  apiErrorReporter = reporter
}

/** Forwards API error details (and token-resolution failures) to the registered reporter. */
export function reportApiError(details: unknown): void {
  apiErrorReporter?.(details)
}
