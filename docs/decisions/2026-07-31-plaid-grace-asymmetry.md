# ADR: The Three-Way Plaid Grace Asymmetry Is Deliberate

**Date:** 2026-07-31
**Status:** Accepted
**Issue:** #1451

> **Amendment (#1652, 2026-09-15):** The trial-expiry leg below was rehomed to RevenueCat. The
> legacy `TrialService`/`ExpireTrial` flow — and the file that held it — has been deleted; an
> RC-resolved access lapse (an expired paid/comp grant dropping off) now arms the same
> `plaid_cleanup_pending_at` marker through `revenueCatSession.Save`. The grace numbers and the
> reasoning in this record are unchanged; only the code that arms the clock moved.

---

## Context

**This is a decision record, not a bug report.** It exists so a future reader who notices the
inconsistency below does not "fix" it into uniformity without first reading why it exists.

Three separate paths cause a user to lose subscription access, and each waits a different amount
of wall-clock time before an **irreversible external Plaid disconnect call** removes their linked
bank items. The three numbers are three orders of magnitude apart:

| Path | Grace before Plaid disconnect | Where |
|---|---|---|
| Deliberate cancellation (`processSubscriptionCanceled`) | **0s** — inline, in the same webhook handler, immediately after the `cancelled` status write | `backend/internal/queue/jobs/subscription_webhook.go:841` (function), `:860` (cancelled write), `:977` (`DisconnectAllUserItems` call) |
| Trial expiry (now RevenueCat-resolved) | **72–96h** — deferred to `RetentionCleanupCheckWorker`'s repair pass, gated on `plaid_cleanup_pending_at` + a 72h floor, running on a roughly-daily cadence | retention armed by `revenueCatSession.Save` when an RC-resolved grant lapses, `backend/internal/repository/revenuecat_repository.go:175-185`; marker stamped by `ExpireTrialIfNoOverride`, `backend/internal/repository/profile_repository.go:3197-3208`; grace constant `plaidDisconnectRepairGracePeriod = 72 * time.Hour`, `backend/internal/queue/jobs/retention_cleanup_check.go:57` |
| Past due (`PastDueService`) | **14 days** | `config/subscription_tiers.yaml:100` (`plaid_disconnect_days: 14`); consumed at `backend/internal/services/past_due_service.go:286` (`processPlaidDisconnectWindow`, defined at `:646`) |

The 72–96h range for trial expiry is not two separate numbers for the same event: the grace
constant itself is a flat 72h, but `RetentionCleanupCheckWorker` only ticks roughly once a day
(23-hour dedup window per the constant's doc comment at `retention_cleanup_check.go:48-56`), so a
marker that becomes eligible right after one tick isn't picked up until the next — worst case
adds close to another 24h on top of the 72h floor, hence "72–96h" rather than a single number.

**All three numbers were verified against the current code, not transcribed from the ticket on
faith** — see the file:line citations above. They match the ticket's table exactly; no
discrepancy was found.

---

## Why It Is Deliberate

The 72h trial-expiry deferral was introduced in PR #1443 specifically because **a trial silently
lapsing is a weak signal of intent**. The user may simply not have noticed their trial ended —
there was no active choice on their part. `/subscription/checkout` is deliberately whitelisted for
read-only users (`internal/huma/middleware/read_only.go`) precisely so a lapsed user can recover
without friction. Disconnecting their Plaid items immediately would make that recovery strictly
worse (they would have to relink their banks) for no offsetting benefit — the subscription is
already inactive either way; the only thing immediate disconnection buys is a few days less of a
non-paying live Plaid connection.

A **deliberate cancellation**, by contrast, is a strong signal of intent: the user affirmatively
chose to cancel. Zero grace on that path is a **product judgement about the strength of the
signal**, not an oversight — the two paths are asymmetric because the events they respond to are
not equally ambiguous.

---

## The Honest Caveat

⚠️ This is the part of the record that matters most, and the part most likely to be dropped by a
future summary.

`ExpireTrial`'s own doc comment stated a *technical* premise for why
the trial-expiry disconnect can't be safely ordered inline with the status flip: **no ordering of
a status flip and an irreversible Plaid call is fully safe**, because `/subscription/checkout`
staying reachable for read-only users means the user can always resume paying between the flip and
the disconnect, and a synchronous disconnect racing that resumption can delete a paying customer's
banks after they already paid.

**That technical premise applies verbatim to the cancellation path too.** `processSubscriptionCanceled`
disconnects Plaid inline, in the same synchronous call chain as the `cancelled` write
(`subscription_webhook.go:860` → `:977`), and a cancelled user is exactly as read-only, and exactly as
able to reach `/subscription/checkout` to resubscribe, as a lapsed-trial user. The round-4 comment
directly above the disconnect call (`subscription_webhook.go:939-951`) explicitly acknowledges the
residual race — a comp or resubscribe landing in the window between the re-check and the disconnect
call — and accepts it as a documented, non-zero-but-tolerable risk *specifically because* a Plaid
disconnect is recoverable (the user relinks), not because the race itself is any smaller than the
trial-expiry case.

**So the asymmetry rests entirely on the product judgement about intent (strong signal vs. weak
signal), not on the technical argument** — the technical argument, taken alone, would call for
deferring the cancellation path too. If someone later decides deliberate cancellation should also
get a grace window, that is a coherent, defensible position under the same technical reasoning
already in the codebase. It should be made as an **explicit product decision**, not adopted as a
"consistency cleanup" that treats this record's silence on the point as license — a record that
omitted this caveat would read as a stronger justification for the current asymmetry than actually
exists.

---

## Consequences

### Positive
- A user who deliberately cancels experiences the "clean break" they asked for — no lingering live
  bank connection after an intentional cancellation.
- A user whose trial silently lapses gets multiple days of runway to notice and resubscribe without
  having to relink banks.
- Both behaviors are individually defensible; neither is an accident.

### Negative / Risks
- The 1000x spread between paths is easy to mistake for inconsistency by a reader who hasn't seen
  this record, inviting an unreviewed "fix" that quietly removes the cancellation path's zero-grace
  behavior (or worse, adds a grace window to cancellation without recognizing it as a product
  decision).
- The technical race described in the caveat is real and currently *accepted*, not eliminated, on
  the cancellation path. If Plaid disconnect volume or complaint volume ever makes that race look
  costly, the fix is a product decision to add a grace window to cancellation — not a technical fix,
  since no reordering closes the race (per `ExpireTrial`'s own conclusion, which applies here too).

---

## References

- `backend/internal/queue/jobs/subscription_webhook.go:836-1015` — `processSubscriptionCanceled`, the inline zero-grace path
- `backend/internal/repository/revenuecat_repository.go:163-185` — `revenueCatSession.Save`, which since #1652 owns retention arming when an RC-resolved grant lapses (the legacy `ExpireTrial` and its ordering doc comment were deleted with the trial service in #1652)
- `backend/internal/repository/profile_repository.go:3170-3210` — `ExpireTrialIfNoOverride`, which stamps `plaid_cleanup_pending_at` atomically with the trial→inactive CAS
- `backend/internal/queue/jobs/retention_cleanup_check.go:29-57` — `plaidDisconnectRepairGracePeriod` (72h) and the rationale for that specific value
- `backend/internal/services/past_due_service.go:160-206,281-289` — `PastDueService`'s ordering contract and the `plaidDisconnectDays` threshold
- `config/subscription_tiers.yaml:100-101` — `plaid_disconnect_days: 14`, `grace_days: 21`
- Issue #1451 — this decision record's source ticket, filed from the round-3 gap analysis of PR #1443
- Issue #1413 / PR #1443 — the work that introduced the 72h trial-expiry deferral and the comp-safety re-check pattern cited above
