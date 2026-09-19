# ADR: The NativeWind v5 Release Candidate Is the Mobile Primitive Substrate

**Date:** 2026-09-19
**Status:** Accepted
**Issue:** #1658
**Evidence:** `apps/mobile/nativewind-gate.evidence.json` (13 measured items, all passing),
produced by the harness `apps/mobile/scripts/nativewind-gate.ts` against the frozen post-Wave-1
baseline `apps/mobile/scripts/nativewind-gate-baseline.json`
**Record guard:** `apps/mobile/src/test/nativewind-gate-record.test.ts` ties the version pins in
this ADR to `apps/mobile/package.json` and derives the verdict below from
`evidence.items.every(item => item.pass)`
**Related:** `.omo/plans/1658-mobile-ui-primitives-and-themes.md` (Todos 4, 6, 7, 26);
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

Measured by `nativewind-gate.ts` on the post-Wave-1 tree — the same tree the frozen baseline was
recorded from — so the bundle deltas measure the NativeWind substrate rather than Wave-1 token
work. Baseline (post-Wave-1, pre-primitive): web gzip 347,166 B, iOS gzip 1,774,298 B, Android
gzip 1,880,903 B.

| # | Measurement | Measured | Threshold | Result |
| --- | --- | --- | --- | --- |
| M1 | `expo-doctor` exit code | `0` | must equal `0` | PASS |
| M2 | `expo export` exit code for web / iOS / Android | `0` / `0` / `0` | each must equal `0` | PASS |
| M3 | Distinct resolved copies of `react-native-css` in `bun.lock` | `1` | exactly `1` | PASS |
| M4 | Gzipped export size and signed delta vs baseline, per platform | web 347,166 B (Δ 0); iOS 1,774,293 B (Δ −5); Android 1,880,900 B (Δ −3) | web ≤ +15% (≤ 399,241 B; Δ ≤ 52,075 B); native ≤ +20% (iOS ≤ 2,129,158 B, Δ ≤ 354,860 B; Android ≤ 2,257,084 B, Δ ≤ 376,181 B) | PASS |
| M5 | Resolved version vs declared pin for `nativewind` and `react-native-css` | `5.0.0-rc.0` / `3.1.0-rc.0` | exact equality to the declared pins | PASS |

The native deltas are negative (−5 and −3 bytes) and the web delta is exactly zero; the small
native drift is per-build Hermes bytecode noise, not a regression, and is far inside the +20%
native band. Percentage thresholds, not byte identity, are the correct bar for the native
artifacts (see the harness header for why `.hbc` is included in the measurement).

### Full evidence ledger (13 items)

Every item in `nativewind-gate.evidence.json` is recorded here. `measured` is a number or boolean,
never prose.

| Evidence item id | Measured | Threshold | Result |
| --- | --- | --- | --- |
| `expo-doctor-exit` | `0` | `0` | PASS |
| `expo-export-web-exit` | `0` | `0` | PASS |
| `expo-export-ios-exit` | `0` | `0` | PASS |
| `expo-export-android-exit` | `0` | `0` | PASS |
| `react-native-css-copies` | `1` | `1` | PASS |
| `web-export-gzip-bytes` | `347166` | `{ baselineBytes: 347166, maxDeltaPercent: 15, maxBytes: 399241 }` | PASS |
| `web-export-gzip-delta-bytes` | `0` | `{ baselineBytes: 347166, maxDeltaBytes: 52075 }` | PASS |
| `ios-export-gzip-bytes` | `1774293` | `{ baselineBytes: 1774298, maxDeltaPercent: 20, maxBytes: 2129158 }` | PASS |
| `ios-export-gzip-delta-bytes` | `-5` | `{ baselineBytes: 1774298, maxDeltaBytes: 354860 }` | PASS |
| `android-export-gzip-bytes` | `1880900` | `{ baselineBytes: 1880903, maxDeltaPercent: 20, maxBytes: 2257084 }` | PASS |
| `android-export-gzip-delta-bytes` | `-3` | `{ baselineBytes: 1880903, maxDeltaBytes: 376181 }` | PASS |
| `nativewind-version` | `true` | `{ expected: "5.0.0-rc.0", resolved: "5.0.0-rc.0" }` | PASS |
| `react-native-css-version` | `true` | `{ expected: "3.1.0-rc.0", resolved: "3.1.0-rc.0" }` | PASS |

13/13 items passed. The verdict above is mechanically derived from this table, not authored.

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
   re-export (verified in `node_modules/nativewind/types.d.ts`), so both paths resolve to the same
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
- **Mechanism.** Execute Todo 7: add `apps/mobile/src/theme/styleAdapter.ts` (a `StyleSheet`-based
  implementation of the same semantic surface) plus `createThemedStyles`, rewire Todos 8-18's
  primitives to consume it, and amend each Todo 8-19 acceptance line to assert the same *values*
  through the returned style object instead of resolved `className`. Keep `global.css` and
  NativeWind installed but unused by the primitives.
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
