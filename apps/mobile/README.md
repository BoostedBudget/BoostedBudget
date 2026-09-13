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
mobile-owned TypeScript. The three export commands validate deterministic
Metro bundles; they do not sign or publish an application.

## Runtime configuration

`EXPO_PUBLIC_API_URL` is the only supported mobile environment variable in
this slice. Expo embeds `EXPO_PUBLIC_*` values in client bundles, so it must
contain a public HTTP(S) API origin and never a secret, API key, session token,
or credential-bearing URL. The readiness screen reports only whether the value
is present and valid. It never displays or logs the value.

The app configures the runtime-neutral `@boostedbudget/api-client` through
`configureApiClient`. Authentication will later supply tokens through that
adapter; this scaffold does not store credentials.

## Architecture seams

- Expo Router owns universal navigation. TanStack Router is not used in the
  React Native application.
- A root TanStack Query client provides conservative online defaults. There is
  no cache persistence or offline mutation queue.
- NativeWind 5 preview compiles Tailwind CSS 4 classes through Metro and
  consumes `@boostedbudget/design-tokens/nativewind.css`. System light/dark
  preference is the only theme control. The repository uses Bun's hoisted
  linker so Expo native modules resolve to a single copy.
- `selectBillingProvider` is a pure contract: iOS and Android select
  RevenueCat; web selects Paddle. Neither payment SDK is installed.
- Bundle identifier, Android package, URL scheme, and slug are fixed to
  `com.boostedbudget.app` and `boostedbudget`.

## Deliberately deferred

This scaffold does not include Clerk or passkeys ([#1657](https://github.com/BoostedBudget/BoostedBudget/issues/1657)),
shared UI primitives or manual themes ([#1658](https://github.com/BoostedBudget/BoostedBudget/issues/1658)),
PostHog ([#1659](https://github.com/BoostedBudget/BoostedBudget/issues/1659)),
EAS signing, OTA, store delivery, or device E2E ([#1660](https://github.com/BoostedBudget/BoostedBudget/issues/1660)),
notifications or background refresh ([#1661](https://github.com/BoostedBudget/BoostedBudget/issues/1661)),
mobile-web traffic cutover ([#1662](https://github.com/BoostedBudget/BoostedBudget/issues/1662)),
biometric app lock or encrypted read cache ([#1663](https://github.com/BoostedBudget/BoostedBudget/issues/1663)),
payment SDKs and entitlement aggregation ([#1664](https://github.com/BoostedBudget/BoostedBudget/issues/1664)),
Plaid and deep links ([#1665](https://github.com/BoostedBudget/BoostedBudget/issues/1665)),
or authenticated product screens ([#1655](https://github.com/BoostedBudget/BoostedBudget/issues/1655)).

NativeWind 5 remains a release candidate. This initial gate passed Expo Doctor
and static exports for all three platforms; production UI primitives and the
decision to graduate or replace the preview are tracked in #1658.
