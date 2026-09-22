import { createContext, ReactNode, useContext } from 'react'

/**
 * The public auth-adapter seam for the mobile app.
 *
 * This module is the ONLY auth surface the app consumes. It deliberately declares its own
 * STRUCTURAL types instead of importing Clerk directly, so an inert build (no publishable key)
 * never pulls Clerk into the module graph. The Clerk-backed implementation lives in
 * `./mobile-auth.clerk` and is loaded through a runtime-gated `require` taken only when
 * `authAvailable` is true — see `MobileAuthProvider`.
 *
 * The member names and shapes below mirror the pinned Clerk v4 surface recorded in
 * `apps/mobile/docs/clerk-auth-api.md` (todo 5). If that surface changes, the compile-time
 * assertions in `src/test/auth/mobile-auth.test.tsx` fail.
 */

// ---------------------------------------------------------------------------
// Structural aliases for the Clerk resources the screens drive
// ---------------------------------------------------------------------------

/** The closed set of post-authentication tasks Clerk can surface on a session. */
export type SessionTaskKey = 'choose-organization' | 'reset-password' | 'setup-mfa'

/** Structural stand-in for Clerk's `SessionTask`; the task screen routes on `key`. */
export interface SessionTaskHandle {
  key: SessionTaskKey
}

/** Structural stand-in for Clerk's `SignInStatus`. */
export type SignInStatus =
  | 'needs_identifier'
  | 'needs_first_factor'
  | 'needs_second_factor'
  | 'needs_client_trust'
  | 'needs_new_password'
  | 'needs_protect_check'
  | 'complete'

/** Structural stand-in for Clerk's `SignUpStatus`. */
export type SignUpStatus = 'missing_requirements' | 'complete' | 'abandoned'

/** The `{ error }` envelope every Clerk future-resource operation resolves. */
export interface AuthOperationResult {
  error: unknown | null
}

/**
 * The sign-in resource surface the screens call. `finalize` is intentionally absent: the adapter
 * wraps it at the hook level (`useMobileSignIn().finalize`) because Clerk's `finalize` takes a
 * `navigate` callback, not the consumer's destination/navigate pair.
 */
export interface SignInHandle {
  readonly status: SignInStatus
  readonly createdSessionId: string | null
  password(params: {
    password: string
    identifier?: string
    emailAddress?: string
    phoneNumber?: string
  }): Promise<AuthOperationResult>
  emailCode: {
    sendCode(params?: { emailAddress?: string }): Promise<AuthOperationResult>
    verifyCode(params: { code: string }): Promise<AuthOperationResult>
  }
  resetPasswordEmailCode: {
    sendCode(): Promise<AuthOperationResult>
    verifyCode(params: { code: string }): Promise<AuthOperationResult>
    submitPassword(params: {
      password: string
      signOutOfOtherSessions?: boolean
    }): Promise<AuthOperationResult>
  }
  reset(): Promise<AuthOperationResult>
}

/** The sign-up resource surface the screens call. `finalize` is wrapped at the hook level. */
export interface SignUpHandle {
  readonly status: SignUpStatus
  readonly createdSessionId: string | null
  readonly missingFields: readonly unknown[]
  readonly unverifiedFields: readonly unknown[]
  password(params: {
    password: string
    emailAddress?: string
    phoneNumber?: string
    username?: string
  }): Promise<AuthOperationResult>
  verifications: {
    sendEmailCode(): Promise<AuthOperationResult>
    verifyEmailCode(params: { code: string }): Promise<AuthOperationResult>
  }
  reset(): Promise<AuthOperationResult>
}

// ---------------------------------------------------------------------------
// Public hook value shapes
// ---------------------------------------------------------------------------

export interface MobileAuthValue {
  isLoaded: boolean
  isSignedIn: boolean
  userId: string | null
  getToken(options?: { template?: string; skipCache?: boolean }): Promise<string | null>
  /** SSO only: activates a session created by the SSO flow. Idempotent. */
  activateSession(sessionId: string | null): Promise<{ pendingTask: boolean }>
  signOut(): Promise<void>
  reportTokenError(error: unknown): void
}

export interface FinalizeOptions {
  /** e.g. '/dashboard' */
  destination: string
  /** The consumer's router. Receives the decorated URL (possibly external). */
  navigate: (url: string) => void
  /** Route to the task screen when the finalized session carries a `currentTask`. */
  onSessionTask?: (task: SessionTaskHandle) => void
}

export interface FinalizeResult {
  pendingTask: boolean
  error: unknown | null
}

export interface MobileSignInValue {
  isLoaded: boolean
  signIn: SignInHandle | null
  finalize(options: FinalizeOptions): Promise<FinalizeResult>
}

export interface MobileSignUpValue {
  isLoaded: boolean
  signUp: SignUpHandle | null
  finalize(options: FinalizeOptions): Promise<FinalizeResult>
}

export interface MobileSSOValue {
  startSSOFlow(params: { strategy: string; redirectUrl: string }): Promise<{
    createdSessionId: string | null
    sessionActivated: boolean
    cancelled: boolean
    pendingTask: boolean
  }>
}

/** Everything the context carries; both implementations provide exactly this. */
export interface MobileAuthContextValue {
  auth: MobileAuthValue
  signIn: MobileSignInValue
  signUp: MobileSignUpValue
  sso: MobileSSOValue
}

/** Structural stand-in for Clerk's `TokenCache`. */
export interface TokenCacheLike {
  getToken: (key: string) => Promise<string | undefined | null>
  saveToken: (key: string, token: string) => Promise<void>
  clearToken?: (key: string) => void | Promise<void>
}

export interface MobileAuthProviderProps {
  authAvailable: boolean
  publishableKey?: string
  tokenCache?: TokenCacheLike
  children: ReactNode
}

/** Props the Clerk-backed provider accepts (no `authAvailable`; it is only mounted when true). */
export interface ClerkMobileAuthProviderProps {
  publishableKey: string
  tokenCache?: TokenCacheLike
  children: ReactNode
}

/** The shape of the lazily-required Clerk module. */
interface ClerkAuthModule {
  ClerkMobileAuthProvider: (props: ClerkMobileAuthProviderProps) => ReactNode
}

// ---------------------------------------------------------------------------
// Token-error sink (owned here; todo 11 registers the reporter)
// ---------------------------------------------------------------------------

export type TokenErrorReporter = (error: unknown) => void

let tokenErrorReporter: TokenErrorReporter | null = null

/**
 * Registers the sink `reportTokenError` delegates to. Exported for the bootstrap layer; the public
 * hook interface deliberately does not expose it.
 */
export function setTokenErrorReporter(reporter: TokenErrorReporter | null): void {
  tokenErrorReporter = reporter
}

/** Delegates to the registered reporter; a no-op when none is registered. */
export function reportTokenError(error: unknown): void {
  tokenErrorReporter?.(error)
}

// ---------------------------------------------------------------------------
// Inert implementation (no publishable key)
// ---------------------------------------------------------------------------

export const inertMobileAuthContextValue: MobileAuthContextValue = {
  auth: {
    isLoaded: true,
    isSignedIn: false,
    userId: null,
    getToken: async () => null,
    activateSession: async () => ({ pendingTask: false }),
    signOut: async () => {},
    reportTokenError,
  },
  signIn: {
    isLoaded: true,
    signIn: null,
    finalize: async () => ({ pendingTask: false, error: null }),
  },
  signUp: {
    isLoaded: true,
    signUp: null,
    finalize: async () => ({ pendingTask: false, error: null }),
  },
  sso: {
    startSSOFlow: async () => ({
      createdSessionId: null,
      sessionActivated: false,
      cancelled: true,
      pendingTask: false,
    }),
  },
}

export const MobileAuthContext = createContext<MobileAuthContextValue>(inertMobileAuthContextValue)

// ---------------------------------------------------------------------------
// Selectors
// ---------------------------------------------------------------------------

export function useMobileAuth(): MobileAuthValue {
  return useContext(MobileAuthContext).auth
}

export function useMobileSignIn(): MobileSignInValue {
  return useContext(MobileAuthContext).signIn
}

export function useMobileSignUp(): MobileSignUpValue {
  return useContext(MobileAuthContext).signUp
}

export function useMobileSSO(): MobileSSOValue {
  return useContext(MobileAuthContext).sso
}

// ---------------------------------------------------------------------------
// Provider
// ---------------------------------------------------------------------------

/**
 * Mounts the Clerk-backed provider only when `authAvailable` is true. The `require` is taken
 * inside that branch, so an inert bundle never executes Clerk (which transitively imports the
 * browser Clerk bundle).
 */
export function MobileAuthProvider({
  authAvailable,
  publishableKey,
  tokenCache,
  children,
}: MobileAuthProviderProps) {
  if (authAvailable && publishableKey) {
    const { ClerkMobileAuthProvider } = require('./mobile-auth.clerk') as ClerkAuthModule
    return (
      <ClerkMobileAuthProvider publishableKey={publishableKey} tokenCache={tokenCache}>
        {children}
      </ClerkMobileAuthProvider>
    )
  }

  return (
    <MobileAuthContext.Provider value={inertMobileAuthContextValue}>
      {children}
    </MobileAuthContext.Provider>
  )
}