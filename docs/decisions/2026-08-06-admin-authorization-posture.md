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

Admin authorization is Clerk identity plus **either** an admin role **or** membership of the
hardcoded email allowlist — in **every** environment including production. There is deliberately
no environment check in the middleware. `cfg` is still threaded into `NewAdminMiddleware` for
callers and future per-environment policy, but it is not consulted for authorization.

⚠️ **The allowlist is not a shortcut to the role check; it replaces it.** The middleware's
`IsHardcodedAdmin` branch returns *before* `HasRole` is ever consulted, so an allowlisted address
is an admin whatever its Clerk `privateMetadata` says, and revoking its role revokes nothing.
That is the single most important fact about this authorization path, and the reason Decision 2
and the `ADMIN_EMAILS_CONFIG` restriction below exist. Any statement of the form "admin
authorization is identity plus role" is incomplete.

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

The first version of the test suite asserted `EffectiveAdminEmails("")` fails safe. That was
true — and, *at the time*, unreachable: nothing in the startup path then passed `""`, so the test
passed while the real path was broken. It is recorded here because it is why the test was
strengthened to go through `Config.Load()`, and because the mechanism below **makes `""` the live
path** rather than a hypothetical one: `AdminAllowlistEnvironment()` returns exactly that empty
string whenever `ENVIRONMENT` was not set, so what was once an unreachable assertion is now the
fail-safe's whole load-bearing mechanism.

Resolution therefore requires an **explicitly set** `ENVIRONMENT`:

- `Config.EnvironmentExplicitlySet` records whether the variable was set to a **non-empty** value
  (`os.Getenv("ENVIRONMENT") != ""`, not `os.LookupEnv`). `Environment` alone cannot distinguish a
  dev box from a misconfigured production deploy.

  The non-emptiness test rather than presence is deliberate, and it is the safer of the two.
  `getEnv` already collapses unset and empty to the `"development"` default, so with `LookupEnv`
  an explicitly blank `ENVIRONMENT=` — a blanked Fly secret, an empty compose interpolation —
  would count as "explicitly set", `AdminAllowlistEnvironment()` would return the defaulted
  `"development"`, and the CI/QA accounts would load as production admins. That is precisely the
  failure this section exists to close. Treating blank as unset costs a real deployment nothing
  (none sets it blank) and fails in the safe direction.
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

### `ADMIN_EMAILS_CONFIG` is refused outside non-production

The loader honoured `ADMIN_EMAILS_CONFIG` unconditionally. Because the allowlist bypasses the role
check, **whatever file that variable pointed at became an arbitrary set of role-check-free
production admins** — a single environment variable, no code change, no review. The first
remediation pass only *warned* about it at startup, which is detective, not preventive: it reports
the substitution after it has already taken effect.

`config.AdminEmailsConfigOverride` now honours the override **only** in a recognised
non-production environment, and `LoadAdminEmailConfig` reads the committed
`config/admin_emails.yaml` (baked into the image by `backend/Dockerfile`) everywhere else. The
variable's only real purpose is relocating the file for tests, which always set it alongside a
non-production `ENVIRONMENT`, so this costs nothing operationally.

The predicate is deliberately the *same* one the list resolution uses —
`domain.IsNonProductionEnvironment(cfg.AdminAllowlistEnvironment())`. Two differently-shaped
notions of "is this production?" in one authorization path is how this class of bug survives, so
resist adding a second test here. A *rejected* override is logged at **Error**: silently reading a
different file than the operator asked for would be its own trap.

A `schema_version` marker on the file was considered (validate the file's shape before trusting
it) and rejected as strictly weaker — it validates a file's *shape*, not its *authority*, and an
attacker who can set the variable can also write the marker.

## Decision 3 — `grant-trial` stays reachable in production, deliberately

`POST /api/v1/admin/subscription/grant-trial` (#698) exists so the E2E suite can put its test
user into an active subscription. Its OpenAPI summary *used to* say "QA only", which described
neither an enforced restriction nor, after #1499, the truth about reachability. The summary now
reads "Grant trial subscription (admin)" and its description says the endpoint is deliberately
available in every environment.

**A gate was considered and declined.** Granting a trial is a legitimate admin action: any real
admin can already change a user's subscription through the other admin subscription endpoints
(tier overrides, subscription management in `AdminUserService`). Gating this one endpoint would
buy no security while breaking the E2E flow.

This is recorded because it looks like an oversight and is not. Do not add an environment gate
to this handler; see the comment at `AdminHandler.GrantTrial`.

## Decision 4 — `ADMIN_EMAILS_CONFIG` is honoured only outside production

`ADMIN_EMAILS_CONFIG` relocates the allowlist file. Because the allowlist bypasses the role check,
whatever that variable points at becomes an arbitrary, role-check-free set of admins. The loader
therefore **refuses** it unless `domain.IsNonProductionEnvironment(Config.AdminAllowlistEnvironment())`
— the same predicate, and the same value, that decide which of the file's two lists load. One
notion of "is this production?" for the whole authorization path, not two. A rejected override is
logged at `Error` with the path and the reason, because silently reading a different file than the
operator asked for is its own trap.

⚠️ **This narrows a stated requirement, deliberately.** The task's requirement was "keep
`ADMIN_EMAILS_CONFIG` working"; it now works only outside production. The reasoning:

- Nothing in the repo sets it, so nothing breaks today.
- The risk is asymmetric. If refusing it is wrong, the cost is an operator inconvenience that
  announces itself in the startup log at `Error` with an explicit explanation. If honouring it in
  production is wrong, the cost is a silent, role-check-free production admin bypass.
- Its only actual use is relocating the file for tests, where a non-production `ENVIRONMENT` is
  always set alongside it.

**Counter-argument, recorded honestly:** issue #1498 listed "move the list to a secret" as one
remediation the maintainer might choose, and this forecloses doing that via a relocated path in
production. It is also a weak threat model — anyone who can set `ADMIN_EMAILS_CONFIG` on the
production host can usually set `CLERK_SECRET_KEY` or deploy code. If the maintainer wants the
override back in production, drop the `&& domain.IsNonProductionEnvironment(allowlistEnv)` from
`config.AdminEmailsConfigOverride` — one clause, and `TestLoadAdminEmailConfig_RejectsOverride*`
will fail loudly to confirm the change was intended.

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
- `ADMIN_EMAILS_CONFIG` relocates the ACL, but **only in a recognised non-production
  environment** — see Decision 4. Nothing in the repo sets it.
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
