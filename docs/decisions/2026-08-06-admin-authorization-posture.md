# ADR: Admin Endpoints Are Authorized by Identity and Role in Every Environment

**Date:** 2026-08-06
**Status:** Accepted
**Issues:** #1499 (environment gate removal), #1498 (hardcoded admin allowlist), #638, #821

---

## Context

Until #1499, `NewAdminMiddleware` (`backend/internal/huma/middleware/admin.go`) refused every
request under `/api/v1/admin` outside the `qa` and `staging` environments — it returned 404, so
the admin surface did not exist in production. That made the entire admin dashboard (system
health, user management, subscription administration, the dead-letter queue) unreachable in the
one environment where an operator most needs it.

Removing that gate was correct, but it changed the security model in a way worth recording,
because two things that were previously inert became live production authorization inputs.

## Decision 1 — No environment gate on admin endpoints

Admin authorization is Clerk identity plus admin role, in **every** environment including
production. There is deliberately no environment check in the middleware. `cfg` is still
threaded into `NewAdminMiddleware` for callers and future per-environment policy, but it is not
consulted for authorization.

The `/api/v1/admin/check-role` exemption (#638) stands unchanged: any authenticated user may
ask whether they are an admin. It is informational and returns no privileged data.

The failure mode is fail-closed: if the role lookup itself errors, the middleware refuses with
500 rather than falling through.

## Decision 2 — The hardcoded admin email list is split by environment

`config/admin_emails.yaml` grants admin access **bypassing the role check entirely** — the
middleware's `IsHardcodedAdmin` branch returns before `HasRole` is ever consulted. The file is
git-tracked and copied into the runtime image (`backend/Dockerfile`). With the environment gate
gone, it is a live production ACL.

Two of its five entries were test accounts: the CI E2E user
(`e2e+clerk_test@boostedbudget.com`) and the QA admin (`qa-admin@boostedbudget.com`, #821).
Those were harmless while production 404'd every admin request. They are not harmless now.

The file therefore has two keys:

- `admin_emails` — granted admin access in every environment, production included. Real human
  platform owners only.
- `non_production_admin_emails` — granted admin access only in a recognised **non-production**
  environment. Test, CI, and QA accounts live here.

`domain.AdminEmailConfig.EffectiveAdminEmails(env)` resolves the two, and `main.go` passes only
the resolved set to `NewAdminService`. Nothing should read `AdminEmails` directly when building
the ACL.

### The environment predicate is an allowlist, and that is the point

`domain.IsNonProductionEnvironment` matches against an explicit allowlist —
`development`, `qa`, `staging` — rather than testing `env != "production"`.

This is the fail-safe direction. With `if env == "production" { exclude }`, an unset,
misspelled, or newly-introduced `ENVIRONMENT` value would silently load CI credentials as
production admins. With the allowlist, an unrecognised value gets the production treatment: the
test entries are excluded, and the platform owners still work.

### An allowlist alone was not enough: the resolution requires an *explicit* ENVIRONMENT

The allowlist was necessary but insufficient, and this is the subtlest part of the decision. An
independent adversarial review caught it before this shipped.

`getEnv` (`internal/config/config.go`) maps an unset **and** an empty `ENVIRONMENT` to the
`"development"` default, so `cfg.Environment` is never `""`. And `development` is *on* the
non-production allowlist. So the failure the allowlist was built to prevent still occurred by a
different route: a production deployment that simply forgot to set `ENVIRONMENT` would resolve as
`development` and load both CI test accounts as role-check-free production admins.

The first version of the test suite asserted `EffectiveAdminEmails("")` fails safe. That is true,
and unreachable — nothing in the startup path ever passes `""`. The test passed while the real
path was broken.

Resolution therefore requires an **explicitly set** `ENVIRONMENT`:

- `Config.EnvironmentExplicitlySet` records whether the variable was actually present.
  `Environment` alone cannot distinguish a dev box from a misconfigured production deploy.
- `Config.AdminAllowlistEnvironment()` returns `Environment` only when it was explicitly set,
  and `""` otherwise — not a recognised non-production name, so the extra entries are excluded.
- `main.go` resolves against `AdminAllowlistEnvironment()`, never `Environment`, and warns at
  startup when `ENVIRONMENT` is unset.

`Environment` itself still defaults to `"development"`; that behaviour is relied on elsewhere and
is unchanged. Only the ACL decision is stricter.

Every real deployment sets `ENVIRONMENT` explicitly, so this costs nothing in practice. A bare
`go run ./cmd/server` with no `ENVIRONMENT` loses the two test admins and keeps the owners, which
is the correct direction to fail.

**The general lesson, worth carrying to the next security predicate in this codebase:** a
defaulted config value cannot carry a fail-safe. If a decision must be safe when an operator omits
a variable, it has to be able to observe the omission — and the test has to go through the real
loader, not call the predicate directly.

The allowlist covers every non-production value this codebase actually produces:

| Value | Where it comes from |
|---|---|
| `development` | `Config.Load` default (`getEnv("ENVIRONMENT", "development")`), `docker-compose.yml` |
| `qa` | `docker-compose.qa.yml`, `.github/workflows/deploy-qa.yml`, the E2E backend in `apps/web/playwright.config.ts` |
| `staging` | `backend/fly.staging.toml` |
| `production` | `backend/fly.toml` — **not** in the allowlist |

Adding a new deployment environment means adding it here too. Forgetting costs test-admin
access in that environment; it never causes production exposure.

Matching is case- and whitespace-insensitive, so a stray `"  QA  "` is still recognised as qa.
A value that is not a known name still fails safe.

The effective set size and the active mode are logged at startup so an operator can see which
posture is live. The addresses themselves are never logged.

## Decision 3 — `grant-trial` stays reachable in production, deliberately

`POST /api/v1/admin/subscription/grant-trial` (#698) exists so the E2E suite can put its test
user into an active subscription. Its OpenAPI summary says "QA only", and with the environment
gate gone that wording no longer describes an enforced restriction.

**A gate was considered and declined.** Granting a trial is a legitimate admin action: any real
admin can already change a user's subscription through the other admin subscription endpoints
(tier overrides, subscription management in `AdminUserService`). Gating this one endpoint would
buy no security while breaking the E2E flow. The "QA only" wording describes who normally calls
it, not who may.

This is recorded because it looks like an oversight and is not. Do not add an environment gate
to this handler; see the comment at `AdminHandler.GrantTrial`.

## Consequences

- The admin dashboard works in production, for actual admins.
- `config/admin_emails.yaml` must be reviewed as an access control list. Adding an address to
  `admin_emails` grants permanent, role-check-free access to live customer data.
- The E2E suite is unaffected: its backend runs with an explicit `ENVIRONMENT=qa`
  (`apps/web/playwright.config.ts`), which is in the non-production allowlist, so
  `e2e+clerk_test@boostedbudget.com` keeps admin access. That `ENVIRONMENT=qa` is now
  load-bearing for a different reason than its old comment claimed — it is what grants the E2E
  user admin, not what "enables" admin endpoints — and the comment there says so, because
  removing it as obsolete would 403 every admin spec.
- `ADMIN_EMAILS_CONFIG` relocates the ACL. Nothing sets it, and startup now warns when it is
  set, so an overridden file cannot silently replace the reviewed, committed one.
- The QA auth-bypass endpoint's own two guards (#821) are untouched and still restrict it to
  `qa`/`staging` with a shared secret.

## Alternatives rejected

- **Keep the environment gate and expose the admin dashboard another way.** Rejected: there is
  no other way, and a 404 is not an authorization mechanism.
- **Pattern-match test accounts by address shape** (e.g. anything containing `+clerk_test` or
  starting `qa-`). Rejected: implicit, brittle, and silently reclassifies addresses when someone
  picks a name that happens to match. The two-list schema states intent explicitly.
- **Infer intent from the trailing `# comment`** the file already carried on those two lines.
  Rejected for the same reason — a comment was the only signal before, which is precisely why
  this was easy to miss.
- **Drop the test accounts from the file entirely** and provision them through Clerk roles.
  Rejected for now: it works, but it moves E2E/QA setup out of the repo and into manual Clerk
  state. Worth revisiting; it would let the hardcoded list shrink to owners only.
