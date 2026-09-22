/**
 * Compile probe for the pinned `@clerk/expo@4.6.8` surface recorded in
 * `clerk-auth-api.md`. This file is intentionally type-only: it never runs, it is
 * only compiled by `bun run --cwd apps/mobile typecheck` (the tsconfig `include`
 * globs every `.ts`/`.tsx` under the app root, so `docs` is type-checked).
 *
 * It lives outside `app/**` and `src/**` (except `src/test/**`) on purpose: the
 * todo-7 source guard forbids any `@clerk/` specifier in the app sources, and this
 * probe must import Clerk directly. It is also outside jest's `collectCoverageFrom`.
 *
 * Every assertion is a *used* `ExpectTrue` alias: a violated constraint makes
 * `tsc` fail. A bare alias resolving to `never` would NOT fail typecheck, so do
 * not "simplify" these into that form.
 *
 * Assertion families, per hook:
 *   - return shape  — what the hook hands back (e.g. `signIn.status`)
 *   - param shape   — the arguments of the methods the adapter calls
 *                     (e.g. `finalize`'s `navigate` argument)
 */

import type { ComponentProps } from 'react';
import { ClerkProvider, useAuth, useClerk, useSession, useSignIn, useSignUp } from '@clerk/expo';
import {
  useSSO as useSSOExperimental,
  type StartSSOFlowParams as ExperimentalStartSSOFlowParams,
  type StartSSOFlowReturnType as ExperimentalStartSSOFlowReturnType,
} from '@clerk/expo/experimental';
import { useSSO as useSSOMain } from '@clerk/expo';
import type {
  ClerkProviderProps,
  TokenCache,
  SessionTask,
  SessionResource,
  SetActiveNavigate,
  SignInStatus,
  SignUpStatus,
  SignUpField,
  SignUpIdentificationField,
  SignInFutureResource,
  SignUpFutureResource,
  SignInFutureCreateParams,
  SignInFutureEmailCodeSendParams,
  SignInFutureEmailCodeVerifyParams,
  SignInFutureResetPasswordSubmitParams,
  SignUpFutureEmailCodeVerifyParams,
} from '@clerk/expo/types';

/** Fails typecheck unless `T` is exactly `true`. */
type ExpectTrue<T extends true> = T;

// ---------------------------------------------------------------------------
// ClerkProvider — props, incl. publishableKey and tokenCache
// ---------------------------------------------------------------------------

/** Provider accepts the required publishable key and the optional built-in token cache. */
export type _ProviderProps = ExpectTrue<
  ComponentProps<typeof ClerkProvider> extends {
    publishableKey: string;
    tokenCache?: TokenCache;
  }
    ? true
    : false
>;

/** `ClerkProviderProps` re-declares `publishableKey` as required (base omits it). */
export type _ProviderPropsType = ExpectTrue<
  ClerkProviderProps extends { publishableKey: string } ? true : false
>;

/** `TokenCache` is the shape the adapter hands to the provider. */
export type _TokenCache = ExpectTrue<
  TokenCache extends {
    getToken: (key: string) => Promise<string | undefined | null>;
    saveToken: (key: string, token: string) => Promise<void>;
  }
    ? true
    : false
>;

// ---------------------------------------------------------------------------
// useAuth() — isLoaded / isSignedIn / userId / getToken
// ---------------------------------------------------------------------------

/** Return shape: `isLoaded` is always present and boolean across the union. */
export type _AuthReturn = ExpectTrue<
  ReturnType<typeof useAuth> extends { isLoaded: boolean } ? true : false
>;

/** Return shape: `userId` may be `string | null | undefined`; `getToken` always present. */
export type _AuthUser = ExpectTrue<
  ReturnType<typeof useAuth> extends { userId: string | null | undefined; getToken: unknown }
    ? true
    : false
>;

/** Param shape of `getToken`: the options the adapter passes through. */
export type _GetTokenParams = ExpectTrue<
  Parameters<ReturnType<typeof useAuth>['getToken']>[0] extends
    | { template?: string; skipCache?: boolean; organizationId?: string }
    | undefined
    ? true
    : false
>;

/** Return shape of `getToken`: `Promise<string | null>`. */
export type _GetTokenReturn = ExpectTrue<
  ReturnType<ReturnType<typeof useAuth>['getToken']> extends Promise<string | null> ? true : false
>;

// ---------------------------------------------------------------------------
// useClerk() — setActive, signOut, and session
// ---------------------------------------------------------------------------

/** Return shape: `useClerk()` is the loaded Clerk object, which exposes `session`. */
export type _ClerkSession = ExpectTrue<
  ReturnType<typeof useClerk> extends { session: unknown } ? true : false
>;

/** `clerk.session` carries the pending task the SSO branch reads fresh. */
export type _ClerkSessionTask = ExpectTrue<
  NonNullable<ReturnType<typeof useClerk>['session']> extends { currentTask?: SessionTask }
    ? true
    : false
>;

/** Param shape of `setActive` (session + navigate), per the pinned `SetActiveParams`. */
export type _SetActiveParams = ExpectTrue<
  Parameters<ReturnType<typeof useClerk>['setActive']>[0] extends {
    session?: unknown;
    navigate?: SetActiveNavigate;
  }
    ? true
    : false
>;

/** Return shape of `setActive`: `Promise<void>`. */
export type _SetActiveReturn = ExpectTrue<
  ReturnType<ReturnType<typeof useClerk>['setActive']> extends Promise<void> ? true : false
>;

/** Return shape of `signOut`: `Promise<void>`. */
export type _SignOutReturn = ExpectTrue<
  ReturnType<ReturnType<typeof useClerk>['signOut']> extends Promise<void> ? true : false
>;

// ---------------------------------------------------------------------------
// useSession() — { session } and session.currentTask
// ---------------------------------------------------------------------------

/** Return shape: the union always exposes `session` and `isSignedIn`. */
export type _SessionReturn = ExpectTrue<
  ReturnType<typeof useSession> extends { session: unknown; isSignedIn: unknown } ? true : false
>;

/** A signed-in session exposes `currentTask`; todo 6 derives `isSignedIn` from its absence. */
export type _SessionCurrentTask = ExpectTrue<
  NonNullable<ReturnType<typeof useSession>['session']> extends { currentTask?: SessionTask }
    ? true
    : false
>;

/** `SessionTask.key` is the closed task union the task screen routes on. */
export type _SessionTaskKey = ExpectTrue<
  SessionTask extends { key: 'choose-organization' | 'reset-password' | 'setup-mfa' }
    ? true
    : false
>;

// ---------------------------------------------------------------------------
// useSignIn() — Core 3 signal shape: { signIn, errors, fetchStatus }, NO isLoaded
// ---------------------------------------------------------------------------

/** Return shape: `signIn` is present. */
export type _SignInReturn = ExpectTrue<
  ReturnType<typeof useSignIn> extends { signIn: unknown } ? true : false
>;

/** Return shape: `errors` + `fetchStatus` are the Core 3 signal fields. */
export type _SignInSignal = ExpectTrue<
  ReturnType<typeof useSignIn> extends {
    errors: unknown;
    fetchStatus: 'idle' | 'fetching';
  }
    ? true
    : false
>;

/** Core 3: `useSignIn()` does NOT return `isLoaded` — readiness comes from `useAuth()`. */
export type _SignInHasNoIsLoaded = ExpectTrue<
  ReturnType<typeof useSignIn> extends { isLoaded: unknown } ? false : true
>;

/** Return shape: the resource surface the screens call. */
export type _SignInResource = ExpectTrue<
  ReturnType<typeof useSignIn>['signIn'] extends {
    status: SignInStatus;
    createdSessionId: string | null;
    create: unknown;
    password: unknown;
    finalize: unknown;
    emailCode: unknown;
    resetPasswordEmailCode: unknown;
  }
    ? true
    : false
>;

/** Param shape of `signIn.create(...)`: the identifier the reset flow sets before `sendCode`. */
export type _SignInCreateParams = ExpectTrue<
  Parameters<ReturnType<typeof useSignIn>['signIn']['create']>[0] extends SignInFutureCreateParams
    ? true
    : false
>;

/** `create` accepts an `identifier` (the collected email) and resolves the `{ error }` envelope. */
export type _SignInCreateIdentifier = ExpectTrue<
  Parameters<ReturnType<typeof useSignIn>['signIn']['create']>[0] extends { identifier?: string }
    ? ReturnType<ReturnType<typeof useSignIn>['signIn']['create']> extends Promise<{
        error: unknown;
      }>
      ? true
      : false
    : false
>;

/** Param shape of `signIn.password(...)`: a password is required. */
export type _SignInPasswordParams = ExpectTrue<
  Parameters<ReturnType<typeof useSignIn>['signIn']['password']>[0] extends { password: string }
    ? true
    : false
>;

/** Param shape of `signIn.emailCode.sendCode()` and `.verifyCode()`. */
export type _SignInEmailCode = ExpectTrue<
  ReturnType<typeof useSignIn>['signIn']['emailCode'] extends {
    sendCode: (params?: SignInFutureEmailCodeSendParams) => Promise<{ error: unknown }>;
    verifyCode: (params: SignInFutureEmailCodeVerifyParams) => Promise<{ error: unknown }>;
  }
    ? true
    : false
>;

/** Param shape of the forgot-password steps: send / verify / submitPassword. */
export type _SignInResetPassword = ExpectTrue<
  ReturnType<typeof useSignIn>['signIn']['resetPasswordEmailCode'] extends {
    sendCode: () => Promise<{ error: unknown }>;
    verifyCode: (params: SignInFutureEmailCodeVerifyParams) => Promise<{ error: unknown }>;
    submitPassword: (
      params: SignInFutureResetPasswordSubmitParams
    ) => Promise<{ error: unknown }>;
  }
    ? true
    : false
>;

/** Param shape of `finalize`: optional `{ navigate }`, where navigate is a SetActiveNavigate. */
export type _SignInFinalizeParams = ExpectTrue<
  Parameters<ReturnType<typeof useSignIn>['signIn']['finalize']>[0] extends
    | { navigate?: SetActiveNavigate }
    | undefined
    ? true
    : false
>;

/** The `navigate` callback argument: `{ session, decorateUrl }`, NOT a URL string. */
export type _SignInFinalizeNavigateArg = ExpectTrue<
  Parameters<
    NonNullable<
      NonNullable<Parameters<ReturnType<typeof useSignIn>['signIn']['finalize']>[0]>['navigate']
    >
  >[0] extends {
    session: SessionResource;
    decorateUrl: (url: string) => string;
  }
    ? true
    : false
>;

/** `navigate` returns `void | Promise<unknown>` (it diverts; it is not a URL producer). */
export type _SignInFinalizeNavigateReturn = ExpectTrue<
  ReturnType<
    NonNullable<
      NonNullable<Parameters<ReturnType<typeof useSignIn>['signIn']['finalize']>[0]>['navigate']
    >
  > extends void | Promise<unknown>
    ? true
    : false
>;

/** Return shape of `finalize`: resolves `{ error }`; the adapter must surface it. */
export type _SignInFinalizeReturn = ExpectTrue<
  ReturnType<ReturnType<typeof useSignIn>['signIn']['finalize']> extends Promise<{ error: unknown }>
    ? true
    : false
>;

// ---------------------------------------------------------------------------
// useSignUp() — Core 3 signal shape: { signUp, errors, fetchStatus }, NO isLoaded
// ---------------------------------------------------------------------------

/** Return shape: `signUp` is present. */
export type _SignUpReturn = ExpectTrue<
  ReturnType<typeof useSignUp> extends { signUp: unknown } ? true : false
>;

/** Return shape: `errors` + `fetchStatus` are the Core 3 signal fields. */
export type _SignUpSignal = ExpectTrue<
  ReturnType<typeof useSignUp> extends {
    errors: unknown;
    fetchStatus: 'idle' | 'fetching';
  }
    ? true
    : false
>;

/** Core 3: `useSignUp()` does NOT return `isLoaded` — readiness comes from `useAuth()`. */
export type _SignUpHasNoIsLoaded = ExpectTrue<
  ReturnType<typeof useSignUp> extends { isLoaded: unknown } ? false : true
>;

/** Return shape: the resource surface the screens call. */
export type _SignUpResource = ExpectTrue<
  ReturnType<typeof useSignUp>['signUp'] extends {
    status: SignUpStatus;
    createdSessionId: string | null;
    missingFields: SignUpField[];
    unverifiedFields: SignUpIdentificationField[];
    password: unknown;
    finalize: unknown;
    verifications: unknown;
  }
    ? true
    : false
>;

/** Param shape of `signUp.password(...)`: a password is required. */
export type _SignUpPasswordParams = ExpectTrue<
  Parameters<ReturnType<typeof useSignUp>['signUp']['password']>[0] extends { password: string }
    ? true
    : false
>;

/** Param shape of the sign-up verification steps: send / verify email code. */
export type _SignUpVerifications = ExpectTrue<
  ReturnType<typeof useSignUp>['signUp']['verifications'] extends {
    sendEmailCode: () => Promise<{ error: unknown }>;
    verifyEmailCode: (params: SignUpFutureEmailCodeVerifyParams) => Promise<{ error: unknown }>;
  }
    ? true
    : false
>;

/** Param shape of `signUp.finalize(...)` mirrors `signIn.finalize(...)`. */
export type _SignUpFinalizeParams = ExpectTrue<
  Parameters<ReturnType<typeof useSignUp>['signUp']['finalize']>[0] extends
    | { navigate?: SetActiveNavigate }
    | undefined
    ? true
    : false
>;

/** Return shape of `signUp.finalize`: resolves `{ error }`. */
export type _SignUpFinalizeReturn = ExpectTrue<
  ReturnType<ReturnType<typeof useSignUp>['signUp']['finalize']> extends Promise<{ error: unknown }>
    ? true
    : false
>;

// ---------------------------------------------------------------------------
// useSSO — pinned subpath decision
//
// PINNED: `@clerk/expo/experimental` (Core 3 future resources; SELF-ACTIVATES).
// The main `@clerk/expo` export is pinned as the rejected alternative: it returns
// `setActive` and does NOT self-activate, so the adapter would have to activate
// the session itself — inconsistent with the Core 3 `finalize` contract.
// ---------------------------------------------------------------------------

/** PINNED: experimental `startSSOFlow` params (strategy + redirectUrl + oidc hints). */
export type _SSOParams = ExpectTrue<
  Parameters<ReturnType<typeof useSSOExperimental>['startSSOFlow']>[0] extends {
    strategy: string;
    redirectUrl?: string;
    oidcPrompt?: string;
    oidcLoginHint?: string;
  }
    ? true
    : false
>;

/** PINNED: experimental `startSSOFlow` return shape (future resources; no `setActive`). */
export type _SSOReturn = ExpectTrue<
  Awaited<ReturnType<ReturnType<typeof useSSOExperimental>['startSSOFlow']>> extends {
    createdSessionId: string | null;
    authSessionResult: unknown;
    signIn?: unknown;
    signUp?: unknown;
  }
    ? true
    : false
>;

/** PINNED: experimental variant self-activates — it does NOT hand back `setActive`. */
export type _SSOExperimentalSelfActivates = ExpectTrue<
  Awaited<ReturnType<ReturnType<typeof useSSOExperimental>['startSSOFlow']>> extends {
    setActive: unknown;
  }
    ? false
    : true
>;

/** Rejected alternative: main `@clerk/expo` `startSSOFlow` DOES return `setActive`. */
export type _SSOMainReturnsSetActive = ExpectTrue<
  Awaited<ReturnType<ReturnType<typeof useSSOMain>['startSSOFlow']>> extends { setActive?: unknown }
    ? true
    : false
>;

/** The pinned params/return aliases match what the adapter imports from the subpath. */
export type _SSOTypeAliases = ExpectTrue<
  Parameters<ReturnType<typeof useSSOExperimental>['startSSOFlow']>[0] extends ExperimentalStartSSOFlowParams
    ? Awaited<
        ReturnType<ReturnType<typeof useSSOExperimental>['startSSOFlow']>
      > extends ExperimentalStartSSOFlowReturnType
      ? true
      : false
    : false
>;

// ---------------------------------------------------------------------------
// Resource aliases the adapter imports, pinned directly
// ---------------------------------------------------------------------------

/** The installed `SignInFutureResource` is the Core 3 sign-in resource. */
export type _SignInFutureResource = ExpectTrue<
  ReturnType<typeof useSignIn>['signIn'] extends SignInFutureResource ? true : false
>;

/** The installed `SignUpFutureResource` is the Core 3 sign-up resource. */
export type _SignUpFutureResource = ExpectTrue<
  ReturnType<typeof useSignUp>['signUp'] extends SignUpFutureResource ? true : false
>;
