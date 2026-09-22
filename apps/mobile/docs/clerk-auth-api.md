# Pinned `@clerk/expo` auth API surface (#1657, todo 5)

**Source of truth: the installed declarations, not Clerk's docs.** Every symbol,
return shape and called member below was read from the hoisted install at the
repository root:

```
node_modules/@clerk/expo/         @clerk/expo@4.6.8   (hoisted; NOT apps/mobile/node_modules)
node_modules/@clerk/react/        @clerk/react@6.16.1
node_modules/@clerk/shared/       @clerk/shared@4.33.0
node_modules/expo-web-browser/    expo-web-browser@57.0.3
```

The compile probe `clerk-auth-api.probe.ts` (same directory) imports each symbol
below and asserts the return and param shapes with **used** type-level
assertions; `bun run --cwd apps/mobile typecheck` compiles it. A renamed symbol, a
changed return shape, or a changed param shape fails `typecheck` (see the t5
evidence files).

> ⚠ Any external Clerk doc's method names are **unverified** against this install.
> If this note and the `.d.ts` disagree, the `.d.ts` wins.

## Version and subpath resolution

`@clerk/expo@4.6.8` `package.json` `exports` (cited:
`node_modules/@clerk/expo/package.json`) include `.` (main), `./experimental`,
`./token-cache`, `./secure-store`, `./google`, `./apple`, `./hosted-auth`,
`./passkeys`, `./legacy` and `./types`.

The adapter uses exactly two of them:

| Specifier | Types file | Purpose |
| --- | --- | --- |
| `@clerk/expo` | `node_modules/@clerk/expo/dist/index.d.ts` | `ClerkProvider`, `useAuth`, `useClerk`, `useSession`, `useSignIn`, `useSignUp`, `TokenCache` |
| `@clerk/expo/experimental` | `node_modules/@clerk/expo/dist/experimental.d.ts` → `dist/hooks/useSSO.experimental.d.ts` | the pinned `useSSO` (see §7) |
| `@clerk/expo/types` | `node_modules/@clerk/expo/dist/types/index.d.ts` | re-exports `@clerk/shared/types` plus the package's own aliases; used by the probe only |

`dist/hooks/index.d.ts` re-exports `useClerk`, `useSession`, `useSignIn`,
`useSignUp`, … **from `@clerk/react`**, and its own `useAuth` and `useSSO`. So the
hook *types* below live in `@clerk/react` / `@clerk/shared`, not in the Expo
package itself — the citations say which.

---

## 1. `ClerkProvider`

- Symbol: `ClerkProvider` — `node_modules/@clerk/expo/dist/provider/ClerkProvider.d.ts`
- Props type: `ClerkProviderProps<TUi extends Ui = Ui>`

Return shape: a React component (`(props) => JSX.Element`).

Props the adapter passes (both are declared here, not inherited):

| Prop | Type | Notes |
| --- | --- | --- |
| `publishableKey` | `string` (**required**) | The Expo provider re-declares it required: `Omit<ReactClerkProviderProps<TUi>, 'publishableKey'> & { publishableKey: string; … }`. Env vars are not inlined inside `node_modules`, so it must be passed explicitly. |
| `tokenCache` | `TokenCache \| undefined` (optional) | The built-in secure-storage-backed cache. |
| `__experimental_passkeys`, `__experimental_resourceCache`, `__experimental_disableNativeClientSync` | optional | Not used by the adapter. |

`TokenCache` — `node_modules/@clerk/expo/dist/cache/types.d.ts`:

```ts
interface TokenCache {
  getToken: (key: string) => Promise<string | undefined | null>;
  saveToken: (key: string, token: string) => Promise<void>;
  clearToken?: (key: string) => void | Promise<void>;
}
```

---

## 2. `useAuth()`

- Symbol: `useAuth` — `node_modules/@clerk/expo/dist/hooks/useAuth.d.ts`
- Declared as `(options?: Parameters<typeof useAuthBase>[0]) => UseAuthReturn`
- `UseAuthReturn` — `node_modules/@clerk/shared/dist/types/hooks.d.ts` (`@clerk/react/dist/useAuth-JxuKh0yj.d.mts` declares the same)

Return shape: a **union** discriminated by `isLoaded` / `isSignedIn`. Every
branch exposes `getToken`:

```ts
type UseAuthReturn =
  | { isLoaded: false; isSignedIn: undefined; userId: undefined; …; getToken: GetToken }
  | { isLoaded: true;  isSignedIn: false;     userId: null;      …; getToken: GetToken }
  | { isLoaded: true;  isSignedIn: true;      userId: string;    …; getToken: GetToken };
```

Members the adapter calls:

| Member | Type | Citation |
| --- | --- | --- |
| `isLoaded` | `boolean` | `hooks.d.ts` |
| `isSignedIn` | `boolean \| undefined` | `hooks.d.ts` |
| `userId` | `string \| null \| undefined` | `hooks.d.ts` |
| `getToken` | `GetToken` | `hooks.d.ts` |

`GetToken` — `node_modules/@clerk/shared/dist/types/session.d.ts`:

```ts
type GetTokenOptions = {
  organizationId?: string;
  skipCache?: boolean;
  template?: string;
};
type GetToken = (options?: GetTokenOptions) => Promise<string | null>;
```

The adapter calls the **default session token**: `getToken()` with no arguments,
exactly as the web app does.

---

## 3. `useClerk()` — `setActive`, `signOut`, **and `session`**

- Symbol: `useClerk` — declared in `node_modules/@clerk/shared/dist/react/hooks/useClerk.d.ts`, re-exported through `@clerk/react/dist/index.d.mts` and `@clerk/expo/dist/hooks/index.d.ts`
- Declared as `() => LoadedClerk`
- `LoadedClerk` / `Clerk` — `node_modules/@clerk/shared/dist/types/clerk.d.ts`

Return shape (`LoadedClerk extends Clerk`, adding `client: ClientResource`):

| Member | Type | Citation |
| --- | --- | --- |
| `setActive` | `SetActive` | `clerk.d.ts` |
| `signOut` | `SignOut` | `clerk.d.ts` |
| `session` | `SignedInSessionResource \| null \| undefined` | `clerk.d.ts` |

`session` is the **fresh** session read: the SSO `pendingTask` branch reads
`clerk.session?.currentTask` immediately after `await startSSOFlow(...)`, never
`useSession()`, whose value is a render-time closure and is stale in the same tick.

`SetActive` — `node_modules/@clerk/shared/dist/types/clerk.d.ts`:

```ts
type DecorateUrl = (url: string) => string;
type SetActiveNavigate = (params: {
  session: SessionResource;
  decorateUrl: DecorateUrl;
}) => void | Promise<unknown>;

type SetActiveParams = {
  session?: SignedInSessionResource | string | null;
  organization?: OrganizationResource | string | null;
  redirectUrl?: string;
  navigate?: SetActiveNavigate;
};
type SetActive = (setActiveParams: SetActiveParams) => Promise<void>;
```

> ⚠ **Pin `useClerk().setActive`.** Core 3's `useSession()` does **not** return
> `setActive` (`UseSessionReturn` has only `isLoaded` / `isSignedIn` / `session` —
> `hooks.d.ts`). Using `useSession().setActive` would not compile.

`SignOut` — `clerk.d.ts` (overloaded; both resolve `Promise<void>`):

```ts
type SignOutCallback = () => void | Promise<any>;
type SignOutOptions = { sessionId?: string; redirectUrl?: string };
type SignOut = {
  (options?: SignOutOptions): Promise<void>;
  (signOutCallback?: SignOutCallback, options?: SignOutOptions): Promise<void>;
};
```

---

## 4. `useSession()`

- Symbol: `useSession` — declared in `node_modules/@clerk/shared/dist/react/hooks/useSession.d.ts`, re-exported through `@clerk/react/dist/index.d.mts` and `@clerk/expo/dist/hooks/index.d.ts`
- Declared as `() => UseSessionReturn`
- `UseSessionReturn` — `node_modules/@clerk/shared/dist/types/hooks.d.ts`

Return shape (union):

```ts
type UseSessionReturn =
  | { isLoaded: false; isSignedIn: undefined; session: undefined }
  | { isLoaded: true;  isSignedIn: false;     session: null }
  | { isLoaded: true;  isSignedIn: boolean;   session: SignedInSessionResource };
```

`signedIn`'s `session` is `SignedInSessionResource` =
`ActiveSessionResource | PendingSessionResource` (`session.d.ts`). Both carry
`currentTask`:

```ts
interface SessionResource { …; currentTask?: SessionTask; … }
interface ActiveSessionResource  extends SessionResource { status: 'active';  user: UserResource; }
interface PendingSessionResource extends SessionResource { status: 'pending'; user: UserResource; currentTask: SessionTask; }
interface SessionTask { key: 'choose-organization' | 'reset-password' | 'setup-mfa'; }
```

Todo 6 derives `isSignedIn = useAuth().isSignedIn && !useSession().session?.currentTask`,
so a **pending-task** session does not satisfy the route guard. `SessionTask.key`
is the closed union the task screen routes on.

---

## 5. `useSignIn()` — Core 3 signal hook, **no `isLoaded`**

- Symbol: `useSignIn` — `node_modules/@clerk/react/dist/index.d.mts` (via `@clerk/expo/dist/hooks/index.d.ts`)
- Declared as `() => SignInSignalValue`
- `SignInSignalValue` — `node_modules/@clerk/shared/dist/types/state.d.ts`

Return shape:

```ts
interface SignInSignalValue {
  errors: SignInErrors;               // Errors<SignInFields>
  fetchStatus: 'idle' | 'fetching';
  signIn: SignInFutureResource;
}
```

> ⚠ **`useSignIn()` does NOT return `isLoaded`.** `SignInSignalValue` has exactly
> the three members above. The probe pins the *absence* with
> `_SignInHasNoIsLoaded`. Readiness for `useMobileSignIn().isLoaded` is derived
> from `useAuth().isLoaded`; never destructure `isLoaded` off this hook.

`SignInFutureResource` — `node_modules/@clerk/shared/dist/types/signInFuture.d.ts`.
Members the screens / adapter call:

| Member | Type | Citation |
| --- | --- | --- |
| `status` | `SignInStatus` = `'needs_identifier' \| 'needs_first_factor' \| 'needs_second_factor' \| 'needs_client_trust' \| 'needs_new_password' \| 'needs_protect_check' \| 'complete'` | `signInCommon.d.ts` |
| `createdSessionId` | `string \| null` (non-null only when `status === 'complete'`) | `signInFuture.d.ts` |
| `create(params)` | `(params: SignInFutureCreateParams) => Promise<{ error: ClerkError \| null }>` | `signInFuture.d.ts` |
| `password(params)` | `(params: SignInFuturePasswordParams) => Promise<{ error: ClerkError \| null }>` | `signInFuture.d.ts` |
| `emailCode.sendCode(params?)` | `(params?: SignInFutureEmailCodeSendParams) => Promise<{ error: ClerkError \| null }>` | `signInFuture.d.ts` |
| `emailCode.verifyCode(params)` | `(params: SignInFutureEmailCodeVerifyParams) => Promise<{ error: ClerkError \| null }>` | `signInFuture.d.ts` |
| `resetPasswordEmailCode.sendCode()` | `() => Promise<{ error: ClerkError \| null }>` | `signInFuture.d.ts` |
| `resetPasswordEmailCode.verifyCode(params)` | `(params: SignInFutureEmailCodeVerifyParams) => Promise<{ error: ClerkError \| null }>` | `signInFuture.d.ts` |
| `resetPasswordEmailCode.submitPassword(params)` | `(params: SignInFutureResetPasswordSubmitParams) => Promise<{ error: ClerkError \| null }>` | `signInFuture.d.ts` |
| `reset()` | `() => Promise<{ error: ClerkError \| null }>` | `signInFuture.d.ts` |
| `finalize(params?)` | `(params?: SignInFutureFinalizeParams) => Promise<{ error: ClerkError \| null }>` | `signInFuture.d.ts` |

Param shapes (`signInFuture.d.ts`):

```ts
type SignInFutureCreateParams = {
  identifier?: string;
  password?: string;
  strategy?: OAuthStrategy | 'enterprise_sso' | PasskeyStrategy | TicketStrategy;
  redirectUrl?: string;
  // …OIDC / ticket / transfer fields, unused by this seam
};
type SignInFuturePasswordParams = { password: string }
  & ( { identifier: string } | { emailAddress: string } | { phoneNumber: string } );
type SignInFutureEmailCodeVerifyParams = { code: string };
type SignInFutureResetPasswordSubmitParams = { password: string; signOutOfOtherSessions?: boolean };
```

> ⚠ **`resetPasswordEmailCode.sendCode()` takes no argument** and sends to the first email
> address on the account — i.e. the identifier already on the `signIn` resource. The
> forgot-password screen must therefore call `signIn.create({ identifier })` with the collected
> address **before** `sendCode()`, or the code is sent to whatever identifier the resource
> already carries (nothing, on a fresh sign-in). `create` needs no `strategy` for this flow;
> `strategy` selects OAuth / passkey / ticket / enterprise-SSO, none of which apply to a
> password reset.

### The completion primitive and its navigate contract

Custom flows finish with **`finalize({ navigate })`, not `setActive`**:

```ts
interface SignInFutureFinalizeParams { navigate?: SetActiveNavigate; }
finalize: (params?: SignInFutureFinalizeParams) => Promise<{ error: ClerkError | null }>;
```

`navigate` is a `SetActiveNavigate` — `({ session, decorateUrl }) => void | Promise<unknown>`
— **not** a URL, and it is the **callback**, not `finalize` itself, that must
inspect `session.currentTask` and divert. `finalize` resolves `{ error }` for API
failures (it can still throw); the adapter must surface that `error`, never
swallow it.

Todo 6 implements, faithfully:

```ts
finalize({ destination, navigate, onSessionTask }) →
  signIn.finalize({ navigate: ({ session, decorateUrl }) => {
    if (session?.currentTask) { onSessionTask?.(session.currentTask); return; }
    navigate(decorateUrl(destination));
  } })
// returns { pendingTask, error }
```

`decorateUrl` may return an external (`https://…`) URL for the Safari-ITP refresh;
the consumer opens those externally rather than assuming client-side routing.

---

## 6. `useSignUp()` — Core 3 signal hook, **no `isLoaded`**

- Symbol: `useSignUp` — `node_modules/@clerk/react/dist/index.d.mts` (via `@clerk/expo/dist/hooks/index.d.ts`)
- Declared as `() => SignUpSignalValue`
- `SignUpSignalValue` — `node_modules/@clerk/shared/dist/types/state.d.ts`

Return shape:

```ts
interface SignUpSignalValue {
  errors: SignUpErrors;               // Errors<SignUpFields>
  fetchStatus: 'idle' | 'fetching';
  signUp: SignUpFutureResource;
}
```

> ⚠ **`useSignUp()` does NOT return `isLoaded`** (pinned by `_SignUpHasNoIsLoaded`).
> Readiness is derived from `useAuth().isLoaded`.

`SignUpFutureResource` — `node_modules/@clerk/shared/dist/types/signUpFuture.d.ts`.
Members the screens / adapter call:

| Member | Type | Citation |
| --- | --- | --- |
| `status` | `SignUpStatus` = `'missing_requirements' \| 'complete' \| 'abandoned'` | `signUpCommon.d.ts` |
| `createdSessionId` | `string \| null` | `signUpFuture.d.ts` |
| `missingFields` | `SignUpField[]` | `signUpFuture.d.ts` |
| `unverifiedFields` | `SignUpIdentificationField[]` | `signUpFuture.d.ts` |
| `password(params)` | `(params: SignUpFuturePasswordParams) => Promise<{ error: ClerkError \| null }>` | `signUpFuture.d.ts` |
| `verifications.sendEmailCode()` | `() => Promise<{ error: ClerkError \| null }>` | `signUpFuture.d.ts` |
| `verifications.verifyEmailCode(params)` | `(params: SignUpFutureEmailCodeVerifyParams) => Promise<{ error: ClerkError \| null }>` | `signUpFuture.d.ts` |
| `reset()` | `() => Promise<{ error: ClerkError \| null }>` | `signUpFuture.d.ts` |
| `finalize(params?)` | `(params?: SignUpFutureFinalizeParams) => Promise<{ error: ClerkError \| null }>` | `signUpFuture.d.ts` |

Field aliases (`signUpCommon.d.ts`):

```ts
type SignUpField = SignUpAttributeField | SignUpIdentificationField | ProtectCheckField;
type SignUpIdentificationField = SignUpVerifiableField | OAuthStrategy | EnterpriseSSOStrategy;
type SignUpFuturePasswordParams = SignUpFutureAdditionalParams & { password: string } & (…);
type SignUpFutureEmailCodeVerifyParams = { code: string };
```

`SignUpFutureFinalizeParams` is identical to `SignInFutureFinalizeParams`
(`{ navigate?: SetActiveNavigate }`) and `finalize` returns the same
`Promise<{ error: ClerkError | null }>`.

---

## 7. `useSSO` — **pinned subpath: `@clerk/expo/experimental`**

Two installed variants exist; the adapter uses **`@clerk/expo/experimental`**
because it is the Core 3 (future-resource) hook and it **self-activates** the
completed session, which is what makes `sessionActivated` meaningful.

### Pinned: `@clerk/expo/experimental`

- File: `node_modules/@clerk/expo/dist/hooks/useSSO.experimental.d.ts`
  (re-exported by `dist/experimental.d.ts`)
- Declared as `() => UseSSOReturn`

```ts
type UseSSOReturn = {
  startSSOFlow: (params: StartSSOFlowParams) => Promise<StartSSOFlowReturnType>;
};

type StartSSOFlowParams = {
  redirectUrl?: string;
  oidcPrompt?: string;
  oidcLoginHint?: string;
  unsafeMetadata?: SignUpUnsafeMetadata;
  authSessionOptions?: Pick<WebBrowser.AuthSessionOpenOptions, 'showInRecents'>;
} & ({ strategy: OAuthStrategy } | { strategy: EnterpriseSSOStrategy; identifier: string });

type StartSSOFlowReturnType = {
  createdSessionId: string | null;
  authSessionResult: WebBrowser.WebBrowserAuthSessionResult | null;
  signIn?: SignInFutureResource | null;
  signUp?: SignUpFutureResource | null;
};
```

- **Self-activates**: the return has **no `setActive`** (pinned by
  `_SSOExperimentalSelfActivates`). `sessionActivated` therefore reflects that the
  hook activated the completed session; `pendingTask` is read from
  `clerk.session?.currentTask` (the top-level `useClerk()` capture) right after
  `await startSSOFlow(...)`.
- `cancelled` is derived from `authSessionResult.type` — `WebBrowserAuthSessionResult`
  = `WebBrowserRedirectResult | WebBrowserResult`; the cancel/dismiss result types
  live in `node_modules/expo-web-browser/build/WebBrowser.types.d.ts`.

### Rejected alternative: `@clerk/expo` (main)

- File: `node_modules/@clerk/expo/dist/hooks/useSSO.d.ts`

```ts
type StartSSOFlowReturnType = {
  createdSessionId: string | null;
  authSessionResult: WebBrowser.WebBrowserAuthSessionResult | null;
  setActive?: SetActive;               // ← returns setActive
  signIn?: SignInResource;
  signUp?: SignUpResource;
};
```

It does **not** self-activate; the caller must `setActive`. That contradicts the
Core 3 `finalize` contract, so it is not used. (Pinned for contrast by
`_SSOMainReturnsSetActive`.)

---

## Adapter → pinned member map (consumer contract for todo 6)

| Adapter surface | Pinned member | Citation |
| --- | --- | --- |
| `MobileAuthProvider` | `ClerkProvider publishableKey tokenCache` | `provider/ClerkProvider.d.ts` |
| `useMobileAuth().isLoaded/isSignedIn/userId/getToken` | `useAuth()` | `hooks/useAuth.d.ts` + `@clerk/shared/.../hooks.d.ts` |
| `useMobileAuth().isSignedIn` guard | `useAuth().isSignedIn && !useSession().session?.currentTask` | `hooks.d.ts`, `session.d.ts` |
| `activateSession` / `signOut` | `useClerk().setActive` / `useClerk().signOut` | `@clerk/shared/.../clerk.d.ts` |
| SSO `pendingTask` fresh read | `useClerk().session?.currentTask` | `clerk.d.ts`, `session.d.ts` |
| `useMobileSignIn().signIn` | `useSignIn().signIn` (`SignInFutureResource`) | `state.d.ts`, `signInFuture.d.ts` |
| `signIn.create({ identifier })` (reset flow) | `signIn.create(params)` | `signInFuture.d.ts` |
| `useMobileSignIn().finalize` | `signIn.finalize({ navigate })` | `signInFuture.d.ts` |
| `useMobileSignUp().signUp` | `useSignUp().signUp` (`SignUpFutureResource`) | `state.d.ts`, `signUpFuture.d.ts` |
| `useMobileSignUp().finalize` | `signUp.finalize({ navigate })` | `signUpFuture.d.ts` |
| `useMobileSignIn/Up().isLoaded` | `useAuth().isLoaded` (NOT the signal hooks) | `hooks.d.ts` |
| `useMobileSSO().startSSOFlow` | experimental `useSSO().startSSOFlow` | `hooks/useSSO.experimental.d.ts` |

## Citation index

| Path (all relative to the repo root) | Symbols pinned |
| --- | --- |
| `node_modules/@clerk/expo/dist/index.d.ts` | package entry (`ClerkProvider`, hooks, `TokenCache`) |
| `node_modules/@clerk/expo/dist/provider/ClerkProvider.d.ts` | `ClerkProvider`, `ClerkProviderProps` |
| `node_modules/@clerk/expo/dist/cache/types.d.ts` | `TokenCache` |
| `node_modules/@clerk/expo/dist/hooks/index.d.ts` | re-exports of `useClerk`/`useSession`/`useSignIn`/`useSignUp`; own `useAuth`/`useSSO` |
| `node_modules/@clerk/expo/dist/hooks/useAuth.d.ts` | `useAuth` (Expo wrapper) |
| `node_modules/@clerk/expo/dist/hooks/useSSO.d.ts` | main `useSSO` (rejected) |
| `node_modules/@clerk/expo/dist/hooks/useSSO.experimental.d.ts` | **pinned** `useSSO`, `StartSSOFlowParams`, `StartSSOFlowReturnType` |
| `node_modules/@clerk/expo/dist/types/index.d.ts` | `@clerk/expo/types` re-export surface |
| `node_modules/@clerk/react/dist/index.d.mts` | `useClerk`, `useSession`, `useSignIn`, `useSignUp` declarations |
| `node_modules/@clerk/shared/dist/types/state.d.ts` | `SignInSignalValue`, `SignUpSignalValue` |
| `node_modules/@clerk/shared/dist/types/hooks.d.ts` | `UseAuthReturn`, `UseSessionReturn` |
| `node_modules/@clerk/shared/dist/types/clerk.d.ts` | `LoadedClerk`, `SetActive`, `SetActiveParams`, `SetActiveNavigate`, `SignOut` |
| `node_modules/@clerk/shared/dist/types/session.d.ts` | `SessionResource`, `SignedInSessionResource`, `SessionTask`, `GetToken`, `GetTokenOptions` |
| `node_modules/@clerk/shared/dist/types/signInFuture.d.ts` | `SignInFutureResource`, `SignInFutureFinalizeParams`, `SignInFuturePasswordParams`, email-code / reset-password params |
| `node_modules/@clerk/shared/dist/types/signUpFuture.d.ts` | `SignUpFutureResource`, `SignUpFutureFinalizeParams`, `SignUpFuturePasswordParams`, `SignUpFutureEmailCodeVerifyParams` |
| `node_modules/@clerk/shared/dist/types/signInCommon.d.ts` | `SignInStatus` |
| `node_modules/@clerk/shared/dist/types/signUpCommon.d.ts` | `SignUpStatus`, `SignUpField`, `SignUpIdentificationField` |
| `node_modules/expo-web-browser/build/WebBrowser.types.d.ts` | `WebBrowserAuthSessionResult`, `AuthSessionOpenOptions` |
