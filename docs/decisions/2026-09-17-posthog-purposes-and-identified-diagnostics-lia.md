# ADR: PostHog Purposes and the Identified-Diagnostics LIA

**Date:** 2026-09-17
**Status:** Accepted
**Issue:** #1683
**Related:** `docs/decisions/2026-08-01-gdpr-erasure-hard-delete.md`,
`docs/operations/posthog-rollout.md`, `docs/security/posthog-pii-masking.md`,
`config/posthog_rollout.yaml`

---

## Context

#1683 turns PostHog from a single consent-gated analytics surface into six separately-governed
purposes: web analytics, web vitals, heatmaps, session replay, autocapture, and dead clicks. It also
introduces **identified diagnostics** — a `name` person property on the PostHog person — and changes
how consent withdrawal survives a scope change. Each of those needs a written basis, because the
rollout contract (`config/posthog_rollout.yaml`) must not claim more than the code does, and the
legal copy must not claim less.

This ADR records the per-surface decisions, the lawful basis for the name-only identity, three
invariants that the implementation depends on, and two corrections to statements that were
previously wrong.

---

## Per-surface go/no-go

| Surface | Go / No-go | Purpose | Lawful basis | Consent |
|---|---|---|---|---|
| Web analytics | **Go** | Understand feature usage | Consent, Art. 6(1)(a) | Analytics consent |
| Web vitals | **Go** | Performance measurement | Consent, Art. 6(1)(a) | Analytics consent |
| Heatmaps | **Go** | Aggregate click maps on anonymous routes | Consent, Art. 6(1)(a) | Analytics consent |
| Session replay | **No-go until the staging gate** | Diagnostic replay | Consent, Art. 6(1)(a) | Analytics **and** interaction-capture consent |
| Autocapture | **No-go** | n/a | n/a | Disabled entirely |
| Dead clicks | **No-go** | n/a | n/a | Disabled entirely |

Web analytics, web vitals, and heatmaps are live under analytics consent. Session replay merges
**inert** — the client pin (`disable_session_recording: true`) plus
`project_controls.session_replay: false` — and is enabled only after the staging inspection in
`docs/operations/posthog-rollout.md` § *Replay enablement gate*. Autocapture and dead clicks stay
disabled; they are named here only so their absence is a recorded decision rather than an omission.

---

## Identified diagnostics: name-only identity and the LIA

Identified diagnostics writes **only the user's `name`** to the PostHog person. It never writes an
email address, and it is default **opt-out** (the column defaults to enabled). It exists so that
support and diagnostic tooling can tell two consenting users apart without exposing a contact
identifier.

**Lawful basis: legitimate interests, Art. 6(1)(f).** A legitimate-interests assessment supports
this because:

- the data is a single display name, not a contact identifier, and never an email;
- it is not used for profiling, advertising, or automated decision-making, and is not combined with
  financial data for any such purpose;
- the user can switch it off at any time, and the opt-out takes effect immediately; and
- the risk to the individual is low and bounded, while the benefit — legible, correct diagnostics —
  is concrete.

The opt-out is implemented as a **post-commit `$unset`**: turning identified diagnostics off emits
`$unset: ['name']` against the Clerk subject as the distinct ID. The rollout contract records
whether that opt-out survives ingestion in
`identified_diagnostics_opt_out_certification`. That field certifies the **opt-out path only** — that
the post-commit `$unset` survives PostHog ingestion — and explicitly **not** feature liveness. The
identity feature IS live at merge; only ingestion-survival is uncertified until the staging check
passes and the `unset_ingestion_survival_unverified` coverage gap closes. The validator fails if the
certification is `go` while that gap is open, and fails closed if the gap row is absent.

---

## Invariant 1 — Scope independence and withdrawal that persists

A consent withdrawal is a decision about a **purpose**, not about the tab or route the user happened
to be on when they made it. Earlier behaviour cleared withdrawal state when the analytics scope
changed, which meant a navigation or scope switch could silently re-grant a purpose the user had
just refused.

This PR makes withdrawal **scope-independent and persistent**: the withdrawal is recorded against the
purpose scope in a set that `setScope` does **not** clear. Only an explicit re-grant —
`clearWithdrawal(scope)` followed by `apply()` — removes it. A scope change therefore cannot
re-enable capture, and the mounted identify hook re-applies against the still-withdrawn scope and
stays denied. This is why the Profile re-enable path calls `clearWithdrawal` before `apply()`: without
it, `apply()` early-returns on the withdrawn scope and the user's explicit re-grant would do nothing.

---

## Invariant 2 — Erasure atomicity

The outbox obligation for an erasure and the `DELETE FROM users` statement run in **one
transaction**. The foreign-key cascade from `users` removes `privacy_settings` inside that same
transaction. A privacy-settings write therefore cannot interleave with an in-flight erasure: either
the erasure commits and the settings row is gone, or it rolls back and nothing was deleted. There is
no window in which a settings write can resurrect a purpose for a user whose erasure is committing.

This is why no fence or advisory lock is needed on the settings write path, and why the earlier
"in-flight fence; dead_letter still emits and alerts" wording was removed — the atomicity invariant
makes the fence unnecessary rather than merely absent.

---

## Invariant 3 — The server-side opt-out emit is a backstop (directional double-emit correction)

The opt-out `$unset` has two possible emitters: the browser lifecycle (`reconcile()`) and the
server-side privacy service. An earlier statement described the server emit as conditional on
analytics consent being **off**. That was wrong.

**Corrected:** the server-side emit is a **backstop** that fires **idempotently on the true→false
delta** of identified diagnostics, **even when analytics consent is ON**. When consent is ON, both
emitters fire for the same opt-out — the browser emit and the server backstop. That is harmless:
`$unset` is idempotent, so the second emit un-sets a property that is already unset. The backstop is
unconditional on the delta precisely so that a missed or blocked browser emit cannot leave `name`
set on the person after the user opted out. "Both fire when consent is ON" is the intended
behaviour, not a double-count defect.

---

## Invariant 4 — Client-wins masking precedence (corrected)

Masking is decided by **client-side recorder configuration**, not by remote project settings. The
recorder options `maskTextSelector`, `maskTextFn`, and `maskAllElementAttributes` are passed to rrweb
at record time; a remote PostHog project masking configuration cannot un-mask what the client masks.
The same precedence holds for heatmaps: the client boolean wins over the remote project toggle
(probe-verified — `isUndefinedOrNull(capture_heatmaps) ? remote-path : capture_heatmaps !== false`).

**Corrected statement:** the client configuration is authoritative for masking and capture gating;
remote project settings cannot loosen it. The earlier claim that remote project settings govern
masking precedence was wrong. `data-ph-mask` markers remain defence-in-depth under the default-deny
global text mask, and element attributes are covered by `maskAllElementAttributes: true`.

---

## Consequences

- `config/posthog_rollout.yaml` records the six surfaces, the replay target/gate, the
  opt-out-path certification, and the exact project-settings diff; CI validates all of it.
- Session replay stays inert until the staging gate records clean text **and** attribute sentinels.
- The legal copy (Privacy Policy §6 and Cookie Policy §2.2) names three cookie categories and
  describes replay as consent-gated and masked, not disabled.
- LIA counsel sign-off is the one human step outside agent execution; this ADR is its written
  record.
