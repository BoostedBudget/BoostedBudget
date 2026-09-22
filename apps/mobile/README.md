# BoostedBudget universal app

This Expo SDK 57 application is the shared foundation for iOS, Android, and
mobile web. It is deliberately a build-only scaffold: the desktop experience
continues to live in `apps/web`, and production mobile traffic is not routed
here yet.

## Commands

Run these from `apps/mobile` after `bun install` at the repository root:

```bash
bun run start
bun run ios
bun run android
bun run web
bun run test
bun run test:ci
bun run lint
bun run typecheck
bun run export:web
bun run export:ios
bun run export:android
```

`test:ci` enforces at least 82% branches, functions, lines, and statements for
mobile-owned TypeScript. The three export commands validate that Metro can
produce each platform bundle; they do not prove reproducible output, sign, or
publish an application.

## Runtime configuration

Two `EXPO_PUBLIC_*` variables are supported in this slice. Expo embeds
`EXPO_PUBLIC_*` values in client bundles, so both must be public and never a
secret, API key, session token, or credential-bearing URL.

- `EXPO_PUBLIC_API_URL` — a public HTTP(S) API origin. The readiness screen
  reports only whether the value is present and valid. It never displays or
  logs the value.
- `EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY` — the Clerk **publishable** client key
  (`pk_test_*` / `pk_live_*`). It is a public client key, safe to embed in the
  bundle, and is **never a secret**; the Clerk secret key must never be set
  here. When it is absent the app is fully usable signed-out — the readiness
  screen simply reports authentication as unavailable. Session tokens are
  stored on-device through `expo-secure-store`, never in the bundle or in
  plaintext storage.

The app configures the runtime-neutral `@boostedbudget/api-client` through
`configureApiClient`; the Clerk adapter supplies tokens through that seam. The
registered provider requests the default session token (no JWT template), so a
first registration can trigger the backend's Clerk-API email fallback; this is
accepted and matches the web app.

The `@clerk/expo` config plugin's native defaults attach as config mods that
only run at `expo prebuild`, so they are inert for `expo export` and remain
unverified until #1660: the Sign in with Apple entitlement and the iOS
deployment target (`17.0`), the Android hosted-auth intent filter for the
`clerk` scheme, and the Android packaging exclusions / Kotlin metadata flag.

### OAuth redirect URL (owner setup)

Google and Apple sign-in return to the app through the `boostedbudget` scheme at
`boostedbudget://sso-callback` (the `app/(auth)/sso-callback.tsx` route). Before
OAuth works in a given Clerk instance, an owner must register that exact URL as
an allowed redirect URL in the Clerk Dashboard (Configure → SSO connections /
Redirect URLs). The value is pinned to `expo.scheme` in `app.json`; changing the
scheme without updating the screen and the Dashboard breaks the callback.

## Architecture seams

- Expo Router owns universal navigation. TanStack Router is not used in the
  React Native application.
- A root TanStack Query client provides conservative online defaults. There is
  no cache persistence or offline mutation queue.
- NativeWind `5.0.0-rc.0` compiles Tailwind CSS 4 classes through Metro and
  consumes `@boostedbudget/design-tokens/nativewind.css`. System light/dark
  preference is the only theme control. The repository uses Bun's hoisted
  linker so Expo native modules resolve to a single copy.
- Theme preference is device-local and persisted through
  `@react-native-async-storage/async-storage`, pinned to the Expo SDK 57
  compatibility version `2.2.0` that `npx expo install` resolves. The default
  export is used deliberately: its React Native Web build reads and writes
  `window.localStorage` under the exact, unprefixed `bb-theme` key, which the
  no-flash web bootstrap reads directly. `bb-theme:<userId>` is reserved for
  the user-scoped preference seam in #1657.
- `selectBillingProvider` is a pure contract: iOS and Android select
  RevenueCat; web selects Paddle. Neither payment SDK is installed.
- Bundle identifier, Android package, URL scheme, and slug are fixed to
  `com.boostedbudget.app` and `boostedbudget`.

## UI primitives

The shared primitive library lives in `apps/mobile/src/components/ui/` and is
re-exported from its `index.ts` barrel. Feature teams consume these primitives
instead of authoring their own styled views.

- **Layout** — `Box`, `Stack`, `Inline`, `Surface`, `Screen`.
- **Typography** — `Text` (`variant`, `tone`, `align`, `numberOfLines`).
- **Actions** — `Button`, `IconButton`, `BackButton`.
- **Fields** — `TextField`, `TextArea`, `Label`, `FormField`, `Switch`,
  `Checkbox`.
- **Surfaces and lists** — `Card`, `StatCard`, `SectionHeader`, `Separator`,
  `List`, `ListItem`.
- **Feedback** — `Alert`, `Toast` / `ToastProvider`, `Spinner`, `Skeleton`,
  `ProgressBar`.
- **Overlays** — `Dialog`, `ConfirmDialog`, `BottomSheet`, `ActionSheet`.
- **Navigation chrome** — `ScreenHeader` (also exported as `AppBar`), `TabBar`.
- **States** — `EmptyState`, `ErrorState`.

Each primitive's colour, spacing, typography, radius, elevation, motion, touch
and focus values come from the generated token adapters (see
`packages/design-tokens/README.md`); no primitive authors a token literal.

⚠ **The issue's `color` category is delivered as the semantic token layer plus
the `tone`/`variant` surface of `Text`/`Surface`/`Button`/`Alert`, not as a
standalone `Color` component.** There is deliberately no `Color.tsx`: colour is
a property of the primitives that paint it, and the semantic token layer is the
single source those `tone`/`variant` props resolve against. This mapping is
recorded here so the issue's scope can be audited against the shipped surface.

## Theme architecture

- **Preference values** are `light`, `dark`, and `system` (`ThemePreference` in
  `src/theme/preference.ts`). `system` follows the OS scheme; the other two are
  explicit user choices.
- **Storage key.** The preference is device-local and persisted under the
  unprefixed `bb-theme` key. `themeStorageKey(userId)` returns `bb-theme` when
  no user is supplied and `bb-theme:<userId>` otherwise — the user-scoped seam
  reserved for #1657. The default AsyncStorage export is used because its React
  Native Web build reads and writes `window.localStorage` under that exact key,
  which the no-flash web bootstrap reads directly.
- **Runtime variables, not a class selector.** The resolved theme's CSS custom
  properties are written at runtime: on web the pre-paint bootstrap in
  `src/theme/bootstrap-script.ts` sets them on `document.documentElement.style`
  before first paint, and on native `react-native-css` resolves the semantic
  variables. The desktop `web.css` adapter keeps an explicit `.dark` class
  selector so the desktop theme stays user-controlled; mobile does **not** use a
  class selector. `nativewind.css` follows `prefers-color-scheme` for the
  initial system-theme policy, and the manual preference is applied through the
  runtime variables. A class-based dark mode is rejected on native (the ADR
  verified `nativewind.css` contains no `.dark {` selector), and `dark:`
  utilities are banned inside `src/components/ui/**` because a manual light
  selection while the OS is dark would otherwise leave every `dark:` utility
  applied while the semantic variables flipped — a half-themed UI.

## Platform exceptions

These are the places where mobile genuinely differs from the desktop web
implementation. They are part of the #1658 primitive contract, not incidental
detail.

- **Icon-only affordance naming.** `IconButton` and `BackButton` require a
  `label` prop and hide the icon child from the accessibility tree
  (`accessibilityElementsHidden` / `importantForAccessibility="no-hide-descendants"`),
  so an icon-only control always has an accessible name and never relies on the
  glyph.
- **Safe-area handling.** `Screen`, `BottomSheet`, and `ToastProvider` read
  `SafeAreaInsetsContext` directly with a zero fallback, because
  `useSafeAreaInsets()` throws without a provider and `jest.setup.ts` is
  off-limits. Safe-area padding is applied as an inline `style`, never a
  `className`: a `className` on `SafeAreaView` silently no-ops (ADR known risk
  #386).
- **Android back handling.** Overlays (`Dialog`, `ConfirmDialog`,
  `BottomSheet`) pass `onRequestClose` to React Native's `Modal`, which is the
  Android hardware-back path. The backdrop is a non-focusable `View`, not a
  `Pressable`, so the web focus trap cannot land on it.
- **Dynamic-type caps.** `Text` defaults `allowFontScaling` to true and exposes
  `maxFontSizeMultiplier`; no primitive sets `allowFontScaling={false}`, and a
  mechanical test fails if one ever does.
- **Reduced-motion behaviour.** `useReducedMotion()` (shared by `Spinner`,
  `Skeleton`, `BottomSheet`, and `ToastProvider`) takes one of two reduced
  branches: `BottomSheet` and `ToastProvider` COLLAPSE their transition to zero
  (`animationType="none"` / a zero duration), while `Spinner` and `Skeleton`
  deliberately SLOW to the slowest motion token rather than stopping — a frozen
  spinner reads as a hung page.
- **Shadows via React Native `boxShadow`.** Elevation tokens are projected
  verbatim as CSS shadow strings; React Native 0.86 resolves `shadow-bb-md` /
  `shadow-bb-lg` to a `boxShadow` array, because layered shadows cannot be
  represented as a single React Native shadow object.
- **NativeWind v5 value rejections.** `rounded-sm` / `rounded-md` /
  `rounded-xl` do not resolve on native (a `var()` nested inside a `calc()` is
  not recursed), so `BoxRadiusTone` is `none | lg | full`. The `rtl:` / `ltr:`
  variants and the `text-start` / `text-end` utilities are silently dropped
  (ADR known risks #453 / #454), so direction-sensitive layout uses explicit
  `flexDirection` / `start` / `end` style props. `dark:` utilities are banned
  (see Theme architecture).

## Migration guidance

Feature teams migrating onto the primitive library must follow these rules.
They are the #1658 migration contract.

- **Consume these primitives.** Import from
  `apps/mobile/src/components/ui` rather than authoring new styled views.
- **Do not author token literals.** No literal hex, no arbitrary Tailwind
  bracket values, and no second token source. Every colour, spacing,
  typography, radius, elevation, motion, touch and focus value comes from the
  generated token adapters.
- **Do not add a `tailwind.config`.** Tailwind 4 is CSS-first here; the theme
  is declared in `global.css` and the generated `nativewind.css`.
- **Do not remove the jest-scoped `nativewind/babel` entry.** The
  `env: { test: { presets: ['nativewind/babel'] } }` entry in
  `apps/mobile/babel.config.js` is intentional and required for `className` to
  resolve in jest (see Todo 26). Do not add any OTHER NativeWind Babel plugin:
  Metro performs the import rewrite in its own resolver for the native build, so
  a top-level Babel entry would be redundant and would perturb a working path.
- **Use semantic-token utilities only.** No `dark:` utilities inside
  `src/components/ui/**`; the theme flip happens through the variables.

## Version pin policy

The mobile app pins `nativewind` exactly to `5.0.0-rc.0` and `react-native-css`
exactly to `3.1.0-rc.0` — exact pins, no `^` or `~`. The exact pin is the
rollback lever: changing it is a reviewed commit, never an implicit range
upgrade.

To change a pin: update `apps/mobile/package.json`, re-run
`bun run --cwd apps/mobile gate:nativewind --check`, and update the ADR and this
README in the same commit. The record guard
(`apps/mobile/src/test/nativewind-gate-record.test.ts`) and the documentation
guard (`apps/mobile/src/test/docs.test.ts`) fail if the ADR, this README, and
`package.json` drift apart.

## Cutover decision and rollback

The substrate decision is recorded in
[`docs/decisions/2026-09-19-nativewind-v5-rc-adoption.md`](../../docs/decisions/2026-09-19-nativewind-v5-rc-adoption.md).
The verdict is **`KEEP_RC`**: all 13 measured items in
`apps/mobile/nativewind-gate.evidence.json` passed, so the pinned release
candidate is kept as the committed primitive substrate. This is a decision to
keep using a release candidate with the known open issues listed in the ADR —
not a claim that the RC is stable or issue-free, and not a claim that the gate
proves runtime utility coverage (it measures build compatibility, bundle size,
and migration shape).

The rollback path is documented in the ADR and is a code change, not a package
change: if the verdict flips to `RETAIN`, Todo 7 adds a `StyleSheet`-based
implementation of the same semantic surface sourced only from
`@boostedbudget/design-tokens/native` and rewires the primitives to it, keeping
every public prop signature identical. The `StyleSheet` adapter is a
design-tokens projection, not a NativeWind feature. `packages/design-tokens` and
`apps/web` are untouched by the rollback, and the pinned versions stay in
`package.json`.

## Deliberately deferred

This scaffold does not include Clerk or passkeys ([#1657](https://github.com/BoostedBudget/BoostedBudget/issues/1657)),
PostHog ([#1659](https://github.com/BoostedBudget/BoostedBudget/issues/1659)),
EAS signing, OTA, store delivery, or device E2E ([#1660](https://github.com/BoostedBudget/BoostedBudget/issues/1660)),
notifications or background refresh ([#1661](https://github.com/BoostedBudget/BoostedBudget/issues/1661)),
mobile-web traffic cutover ([#1662](https://github.com/BoostedBudget/BoostedBudget/issues/1662)),
biometric app lock or encrypted read cache ([#1663](https://github.com/BoostedBudget/BoostedBudget/issues/1663)),
payment SDKs and entitlement aggregation ([#1664](https://github.com/BoostedBudget/BoostedBudget/issues/1664)),
Plaid and deep links ([#1665](https://github.com/BoostedBudget/BoostedBudget/issues/1665)),
or authenticated product screens ([#1655](https://github.com/BoostedBudget/BoostedBudget/issues/1655)).

NativeWind 5 remains a release candidate and is pinned exactly to the validated
`5.0.0-rc.0` build. This initial gate passed Expo Doctor
and static exports for all three platforms; the decision to graduate or replace
the release candidate is tracked in #1695.