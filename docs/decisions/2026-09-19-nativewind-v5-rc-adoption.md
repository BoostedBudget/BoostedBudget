# ADR: The NativeWind v5 Release Candidate Is the Mobile Primitive Substrate

**Date:** 2026-09-19
**Status:** Accepted
**Issue:** #1658
**Evidence:** `apps/mobile/nativewind-gate.evidence.json` (13 measured items, all passing),
produced by the harness `apps/mobile/scripts/nativewind-gate.ts` against the frozen
baseline `apps/mobile/scripts/nativewind-gate-baseline.json` (re-recorded 2026-09-22 for #1657 —
see [Re-measurement](#re-measurement-2026-09-22-the-1657-auth-dependency-re-baseline))
**Record guard:** `apps/mobile/src/test/nativewind-gate-record.test.ts` ties the version pins in
this ADR to `apps/mobile/package.json` and derives the verdict below from
`evidence.items.every(item => item.pass)`
**Related:** the #1658 work plan (Todos 4, 6, 7, 26);
`docs/decisions/2026-08-24-financial-history-retained-by-default.md` (ADR style)

---

## Context

Issue #1658 builds the shared mobile UI primitive library on a styling substrate. The candidate is
NativeWind v5, whose only published line is a release candidate: `nativewind@5.0.0-rc.0`
(published 2026-09-13) with `react-native-css@3.1.0-rc.0` as its exact peer. **npm `latest` is
still `4.2.7`.** The v5 versions live on `preview` / `rc` / `rc-staging` only, and the maintainers'
stated plan (discussion #1818) is to let the RC bake for roughly two weeks before promoting it
(Correction 11). As of this ADR the RC is about six days into that window.

The issue's acceptance rule is therefore an evidence gate, not a promotion: adopt the RC only
after compatibility, bundle and migration evidence clears a bar, and otherwise retain the
StyleSheet token adapter. Todo 4 built the runnable gate; Todo 6 runs it on the post-Wave-1 tree
and records the decision here. **A passing gate does NOT mean "promote to stable" — there is no
v5 stable to promote to.** The word `PROMOTE` is deliberately not a verdict token in this ADR;
it appears in this paragraph only to describe its own removal from the verdict vocabulary.

## Decision

**Verdict:** KEEP_RC

The gate has exactly two outcomes:

- **KEEP_RC** — every measured item passed. Keep the pinned release candidate as the committed
  primitive substrate and proceed with Todos 8-18 on it.
- **RETAIN** — at least one measured item failed. Abandon the RC, trigger Todo 7 (the StyleSheet
  token adapter and substrate swap), and do not start Todos 8-18 until Todo 7 completes.

The verdict is `KEEP_RC`: all 13 items in the evidence file passed. This is a decision to keep
using a *release candidate* with the known open issues listed below, not a claim that the RC is
stable or issue-free.

## Recorded version pins

| Package | Declared pin (`apps/mobile/package.json`) | Resolved in the installed tree |
| --- | --- | --- |
| `nativewind` | `5.0.0-rc.0` | `5.0.0-rc.0` |
| `react-native-css` | `3.1.0-rc.0` | `3.1.0-rc.0` |

Both are exact pins (no `^`/`~`). `dependency-policy.test.ts` asserts the `nativewind` pin and the
`react-native-css` pin independently, and the gate's two version items re-check the declared pin
against the resolved install. The exact pin is the rollback lever: changing it is a reviewed
commit, never an implicit range upgrade.

## The five measurements

Measured by `nativewind-gate.ts`. The table above is the **2026-09-19 adoption recording**; the
frozen baseline was re-recorded once since then, for #1657 (see
[Re-measurement](#re-measurement-2026-09-22-the-1657-auth-dependency-re-baseline) below). Original
adoption baseline (post-Wave-1, pre-primitive): web gzip 347,166 B, iOS gzip 1,774,298 B, Android
gzip 1,880,903 B. Current frozen baseline (2026-09-22): web gzip 476,267 B, iOS gzip 2,582,435 B,
Android gzip 2,691,912 B.

The five measurements, re-measured 2026-09-22 against the current frozen baseline:

| # | Measurement | Measured | Threshold | Result |
| --- | --- | --- | --- | --- |
| M1 | `expo-doctor` exit code | `0` | must equal `0` | PASS |
| M2 | `expo export` exit code for web / iOS / Android | `0` / `0` / `0` | each must equal `0` | PASS |
| M3 | Distinct resolved copies of `react-native-css` in `bun.lock` | `1` | exactly `1` | PASS |
| M4 | Gzipped export size and signed delta vs baseline, per platform | web 476,267 B (Δ 0); iOS 2,582,429 B (Δ −6); Android 2,691,910 B (Δ −2) | web ≤ +15% (≤ 547,708 B; Δ ≤ 71,441 B); native ≤ +20% (iOS ≤ 3,098,922 B, Δ ≤ 516,487 B; Android ≤ 3,230,295 B, Δ ≤ 538,383 B) | PASS |
| M5 | Resolved version vs declared pin for `nativewind` and `react-native-css` | `5.0.0-rc.0` / `3.1.0-rc.0` | exact equality to the declared pins | PASS |

The deltas are 0 / −6 / −2 bytes. The baseline is the upper of two consecutive local runs and sits a
few dozen bytes above the CI gate-environment measurement, because a Metro rebuild is not
bit-identical run to run (the web export differed by ~974 B between two consecutive local runs — CSS
content-hash / build-order noise, not a content change), and the baseline was intentionally taken at
the higher value so both environments pass with margin. All three sit far inside the percentage
bands. Percentage thresholds, not byte identity, are the correct bar for these artifacts (see the
harness header for why `.hbc` is included in the measurement).

### Full evidence ledger (13 items)

Every item in `nativewind-gate.evidence.json` is recorded here (2026-09-22 re-measurement).
`measured` is a number or boolean, never prose.

| Evidence item id | Measured | Threshold | Result |
| --- | --- | --- | --- |
| `expo-doctor-exit` | `0` | `0` | PASS |
| `expo-export-web-exit` | `0` | `0` | PASS |
| `expo-export-ios-exit` | `0` | `0` | PASS |
| `expo-export-android-exit` | `0` | `0` | PASS |
| `react-native-css-copies` | `1` | `1` | PASS |
| `web-export-gzip-bytes` | `476267` | `{ baselineBytes: 476267, maxDeltaPercent: 15, maxBytes: 547708 }` | PASS |
| `web-export-gzip-delta-bytes` | `0` | `{ baselineBytes: 476267, maxDeltaBytes: 71441 }` | PASS |
| `ios-export-gzip-bytes` | `2582429` | `{ baselineBytes: 2582435, maxDeltaPercent: 20, maxBytes: 3098922 }` | PASS |
| `ios-export-gzip-delta-bytes` | `-6` | `{ baselineBytes: 2582435, maxDeltaBytes: 516487 }` | PASS |
| `android-export-gzip-bytes` | `2691910` | `{ baselineBytes: 2691912, maxDeltaPercent: 20, maxBytes: 3230295 }` | PASS |
| `android-export-gzip-delta-bytes` | `-2` | `{ baselineBytes: 2691912, maxDeltaBytes: 538383 }` | PASS |
| `nativewind-version` | `true` | `{ expected: "5.0.0-rc.0", resolved: "5.0.0-rc.0" }` | PASS |
| `react-native-css-version` | `true` | `{ expected: "3.1.0-rc.0", resolved: "3.1.0-rc.0" }` | PASS |

13/13 items passed. The verdict above is mechanically derived from this table, not authored.

## Re-measurement (2026-09-22): the #1657 auth-dependency re-baseline

**Why this section exists.** The gate's other three measured items (doctor/export exits, the
single-copy `react-native-css` check, the two version pins) are absolute and did not move. Only the
frozen reference bytes did, because #1657 added the mobile Clerk authentication dependencies
(`@clerk/expo`, `expo-secure-store`, `expo-web-browser`, `expo-auth-session`, `expo-crypto`), and
Metro bundles them **statically** even though the app deliberately keeps Clerk out of *execution*
(the `require` is runtime-gated behind `authAvailable`). Non-execution of Clerk in an inert build is
a runtime property; it is not a bundler property, and no bundler setting here can exclude a module
Metro reaches through a static `require`. The size growth is therefore the accepted, understood cost
of the auth dependencies — **not a NativeWind regression**.

**What was done, and what was not.** `nativewind-gate-baseline.json` was re-recorded through its
sanctioned path — a reviewed commit — with a `note` recording exactly this. The gate's percentage
thresholds in `nativewind-gate.ts` (`webMaxDeltaPercent: 15`, `nativeMaxDeltaPercent: 20`) are
**unchanged**; the gate was not loosened, removed, or exempted, and the CI step still runs
`gate:nativewind --check` as a `REQUIRED_UNGATED_STEPS` member.

**The three trees, for the audit trail:**

| Platform | Adoption baseline (2026-09-19, post-Wave-1) | CI gate measurement (#1709) | Frozen baseline (2026-09-22) |
| --- | --- | --- | --- |
| web gzip | 347,166 B | 476,211 B | 476,267 B |
| iOS gzip | 1,774,298 B | 2,582,381 B | 2,582,435 B |
| Android gzip | 1,880,903 B | 2,691,853 B | 2,691,912 B |

The CI tree is the authoritative gate environment; the frozen baseline is the local re-measurement,
which sits 54–59 bytes above it (well inside the 15%/20% bands), so both the CI and local
measurements pass with the thresholds untouched. The +25–31% gzip growth versus the adoption
baseline is the Clerk/Expo dependency addition; the unfiltered raw-byte growth is larger (for example
web `.js` 1,797,625 B) because the Clerk bundle compresses well.

**Residual.** The re-baseline does not re-open the adoption decision: the verdict stays `KEEP_RC`,
the pins are unchanged, and the known risks below are unchanged. A follow-up that changes the
bundle-size source (for example removing a stale static `require` target) should re-run
`gate:nativewind --check` and, if it changes the frozen reference again, re-record through this same
reviewed-commit path.

## Migration-checklist verification

Each v4→v5 migration item was verified against the actual tree, not assumed. One plan premise was
wrong and is corrected here.

1. **`nativewind/babel` present in `babel.config.js`** — VERIFIED, and it is JEST-SCOPED and
   INTENTIONAL. `apps/mobile/babel.config.js` has `env: { test: { presets: ['nativewind/babel'] } }`.
   This plugin performs the `react-native` → `react-native-css/components` import rewrite that is
   the only thing making `className` resolve in jest (Todo 26); `registerCSS` alone is not enough.
   ⚠ **The earlier "no Babel preset" premise was WRONG.** Metro performs the same rewrite in its
   own resolver for the native build, so a top-level Babel entry would be redundant and would
   perturb a working native path — hence the `env.test` scope. Do not remove it, and do not add
   any other NativeWind Babel plugin.
2. **`babel-preset-expo` retained** — VERIFIED. `babel.config.js` still lists
   `presets: ['babel-preset-expo']` (and `babel-preset-expo ~57.0.11` remains a devDependency).
   The v5 change removes NativeWind's *own* preset, not Expo's.
3. **`withNativewind` casing** — VERIFIED. `apps/mobile/metro.config.js` uses
   `withNativewind(getDefaultConfig(__dirname))` — lowercase `nw`, and no `input` option. v4's
   `withNativeWind(config, { input })` is gone.
4. **`nativewind/types` → `react-native-css/types`** — VERIFIED with nuance.
   `apps/mobile/tsconfig.json` declares `"types": ["jest", "react-native-css/types"]` (the
   canonical v5 type surface). `apps/mobile/nativewind-env.d.ts` still references
   `nativewind/types`, which in `5.0.0-rc.0` is a one-line `/// <reference types="react-native-css/types" />`
   re-export (verified in the NativeWind package's `types.d.ts`), so both paths resolve to the same
   types and no v4-only type surface is in play.
5. **No `cssInterop` / `remapProps`** — VERIFIED. `grep -rn "cssInterop\|remapProps" apps/mobile`
   over `.ts`/`.tsx`/`.js` (excluding `node_modules` and `dist`) returns zero hits. v5 removed
   these APIs in favour of `styled`.
6. **Class-based dark rejected on native** — VERIFIED. `packages/design-tokens/generated/nativewind.css`
   contains no `.dark {` selector (the design-tokens guard asserts this), and the mobile theme
   architecture uses runtime CSS custom properties plus `@media (prefers-color-scheme: dark)` —
   never a class selector. `apps/mobile` has no `.dark` usage.
7. **`styled` used where ambient props would have been needed** — VERIFIED as not-yet-applicable
   at this gate, and recorded as a forward commitment. `grep -rn "styled" apps/mobile/app apps/mobile/src`
   returns zero hits because no primitive exists yet: Todos 8-18 are Wave 2 and are gated on this
   verdict. v5 replaces `cssInterop`/`remapProps` with `styled`; Todos 8-18 must use `styled`
   where a third-party component needs ambient props. Today the item is satisfied by the *absence*
   of the removed APIs, not by a shipped `styled` call.

Also verified: the v4 `tailwind.config.js` is gone (CSS-first `@theme`; `global.css` imports
`tailwindcss`, `nativewind/theme` and `@boostedbudget/design-tokens/nativewind.css`), and the
engine swap `react-native-css-interop` → `react-native-css` is reflected in the single resolved
copy the gate measures.

## Known risks

This ADR does **not** claim the RC is issue-free. The following open engine issues are known
risks, with the mitigation the primitive library must apply. None of them is caught by the
evidence gate, which measures build compatibility, bundle size and migration shape — not runtime
utility coverage.

- **SafeAreaView `className` silently no-ops (#386).** A `className` on `SafeAreaView` is dropped
  rather than rejected, so a primitive could render unstyled with no error. *Mitigation:* do not
  style safe-area containers via `className`; use `react-native-safe-area-context`'s
  `SafeAreaView` / `useSafeAreaInsets` with explicit `style` props (the dependency is already
  pinned at `~5.7.0`). Todos 8-18 must apply this.
- **`rtl:` / `ltr:` variants silently dropped (#453).** Logical-direction variants compile to
  nothing on the engine. *Mitigation:* do not use `rtl:`/`ltr:` in primitives; direction-sensitive
  layout uses explicit `flexDirection` / `start` / `end` style props (or `I18nManager`), and a
  guard must reject these variants in `src/components/ui/**`.
- **`text-start` / `text-end` silently dropped (#454).** Logical text-alignment utilities are
  dropped. *Mitigation:* use physical `text-left` / `text-right` or the RN `textAlign` style
  prop; do not rely on logical text-alignment utilities.
- **Variable dual-package injection (#409 / #410).** Two resolved copies of the engine can inject
  their runtime variables twice. *Mitigation:* the repository uses Bun's hoisted linker and the
  gate's M3 `react-native-css-copies` item (measured `1`) is the standing guard; a second copy
  fails the gate rather than shipping.

## Rollback path

The rollback is a documented, rehearsed path — not an implicit dependency downgrade. Because the
RC is pinned exactly and `apps/mobile` has no primitives yet at this gate, the rollback cost today
is bounded.

- **Trigger.** Any gate item failing on a re-run, any `nativewind` / `react-native-css` RC bump
  that regresses, or a confirmed regression in the known-risk list above. Record the failing
  evidence, amend this ADR with the swap date and the residual NativeWind surface, and set the
  verdict to `RETAIN`.
- **Mechanism.** Execute Todo 7 — the conditional fallback that was NOT built, because the gate
  returned `KEEP_RC`: add a `StyleSheet`-based adapter (styleAdapter.ts) under
  `apps/mobile/src/theme/` implementing the same semantic surface, plus `createThemedStyles`,
  rewire Todos 8-18's primitives to consume it, and amend each Todo 8-19 acceptance line to assert
  the same *values* through the returned style object instead of resolved `className`. Keep
  `global.css` and NativeWind installed but unused by the primitives.
- **What does NOT change.** `packages/design-tokens` and `apps/web` are untouched by the rollback:
  the StyleSheet adapter reads the same generated `@boostedbudget/design-tokens/native` source, so
  no token value forks. The pinned versions stay in `package.json` (the pin guard must stay green),
  so the dependency graph does not move and the rollback is a code change, not a package change.
- **Reversal.** Rolling back to NativeWind later is the same operation in reverse; the primitives'
  public prop signatures are identical on both substrates, which is what makes the swap cheap.

## Re-evaluation expiry and owner

- **Expiry: 2026-09-27.** The RC published 2026-09-13 and the maintainers' plan is a ~2-week bake,
  so the window closes on or about 2026-09-27.
- **Owner:** the `apps/mobile` maintainer holding #1658's Wave-2 work (the universal-app owner).
- **Required action on or before expiry:** re-run `bun run --cwd apps/mobile gate:nativewind --check`
  and re-derive the verdict from the fresh evidence file. If `v5.0.0` stable has been published,
  run the same evidence file against it and record a new decision. Re-evaluate earlier on any RC
  version bump or any confirmed regression in the known-risk list.

## Follow-up: the real promotion

There is nothing to promote to today — npm `latest` is `4.2.7` and the v5 line is `rc`/`preview`
only. The real promotion is a separate, gated follow-up, recorded here rather than actioned:

- **Issue:** #1695 — "Adopt NativeWind v5 stable for the mobile primitive substrate (gated on v5.0.0)".
- **Gate:** `v5.0.0` (or later) is published to npm `latest`, AND
  `bun run --cwd apps/mobile gate:nativewind --check` passes against it with the same evidence
  shape, AND the known-risk issues (#386, #453, #454, #409/#410) are re-checked and their
  mitigations still hold.
- **Deliverable:** a new evidence file and an update to this ADR (or a superseding ADR) recording
  the promoted version, the re-measured five measurements, and the re-derived verdict.

## Consequences

- Todos 8-18 may proceed on the pinned `nativewind@5.0.0-rc.0` substrate; Todo 7 stays cancelled
  unless the verdict flips to `RETAIN`.
- The mobile primitive library inherits the known-risk mitigations above as hard constraints.
- The exact pins, the evidence file and the baseline are the audit trail; any change to any of
  them is a reviewed commit. The record test fails if this ADR's pins drift from
  `package.json` or if its verdict stops matching the evidence.
