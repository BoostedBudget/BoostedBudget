# RevenueCat staging and production setup (#1652)

This is an operator checklist, not a record of deployment. No environment has
been configured and no live purchase has been validated by this document.

## Environment separation

Create a RevenueCat project for production and a separate project for staging/QA.
Connect production only to Paddle production and staging/QA only to Paddle sandbox.
The API environment selector filters subscriptions, not active entitlements.
Separate projects are therefore required for access isolation; do not treat a
`sandbox` selector as isolation for promotional trials or complimentary access.
Use the Clerk user ID as RevenueCat's App User ID. Do not use email, the internal
BoostedBudget UUID, or anonymous purchase identities. The mapping is one-way and
silent: RevenueCat attributes every purchase to whichever App User ID the SDK was
configured with, and the webhook resolves that ID back to a local account by an
exact Clerk ID match. A mismatch therefore **silently separates a user's purchases
from their account**: the entitlement is credited to an App User ID no local
account owns, the webhook cannot resolve it (the handler returns 503 and the
delivery is retried), and the paid access never appears in the product. Nothing
reconciles this automatically; correcting it is an operator task.

No Apple or Google store configuration is required for this issue.

Use these existing deployment targets; configure each environment independently:

| Environment | GitHub environment | Backend Fly app | SPA |
| --- | --- | --- | --- |
| Staging | `staging` | `staging-boostedbudget-api` | `https://staging.boostedbudget.com` |
| Production | `production` | `boostedbudget-api` | `https://boostedbudget.com` |

The frontend deployment jobs build separately for their matching GitHub
environment. Set the public key there before rebuilding; configuring only the
backend Fly secrets will not configure the SPA.

## RevenueCat-side setup checklist (ordered)

Work through this list once per project. Staging/QA and production are separate
projects; every identifier below is exact, and a typo in an entitlement key or a
price mapping is not cosmetic: it silently breaks access resolution or the
bootstrap's catalog validation. Detail for each step is in the sections that
follow.

1. **Create or confirm the RevenueCat project** for this environment. Use one
   project per environment (see Environment separation).
2. **Connect the Paddle integration** to the matching Paddle account (sandbox for
   staging/QA, production for production) and record the integration's Paddle API
   key. Grant the documented Paddle permissions, including the exact permission
   `customer_portal_session.write`, so RevenueCat can populate the subscription
   management URL.
3. **Create the entitlement keys** exactly as `domain.RevenueCatKey` expects. For
   every tier in `config/subscription_tiers.yaml` (`premium`, `household_2`,
   `household_3`, `household_4`, `household_5`) create `paid_<tier>` and
   `comp_<tier>`; create `trial_premium` once. That is eleven entitlements, listed
   in Product catalog below. The alignment is mechanically enforced by
   `backend/internal/config/tier_entitlement_alignment_test.go`.
4. **Import or create the products** and associate each `paid_<tier>` entitlement
   with both its monthly and annual Paddle price IDs. Product catalog below lists
   the exact IDs from the tier catalogue.
5. **Create the `web` offering** with all supported tier/interval packages and
   publish its web paywall.
6. **Generate the REST v2 secret API key** for this project with only the
   permissions listed in Backend credentials.
7. **Generate the REST v1-compatible grant API key** as a separate key; do not
   assume a v2-only key works for v1 grants.
8. **Configure the webhook**: URL, the exact `Authorization` value, HMAC signing,
   and the event types in Webhook connection.
9. **Capture the public SDK key** (web/Paddle) for the SPA (SPA configuration).
10. **Set the environment**: `sandbox` for staging/QA, `production` for
    production, and make `PADDLE_ENVIRONMENT` match it.
11. **Set the backend secrets** in the target runtime (Backend credentials).
12. **Set the frontend public key** in the matching GitHub environment and rebuild
    the SPA (SPA configuration).
13. **Confirm the identity mapping**: the Clerk user ID is the RevenueCat App User
    ID (Environment separation).
14. **Validate** with the bootstrap's read-only inventory before enabling
    purchases (Bootstrap and validation).

## Product catalog

In each project:

1. Connect the matching Paddle account using RevenueCat's Paddle integration setup.
   Grant the documented Paddle permissions, including the exact permission
   `customer_portal_session.write` when using authenticated management links.
2. Import the existing monthly and annual prices for Premium and Household 2 through 5.
   Keep the existing price amounts and billing intervals. The card-free trial is
   a `trial_premium` grant, not a second trial configured on a paid Paddle price.
3. Create `paid_premium`, `paid_household_2`, `paid_household_3`,
   `paid_household_4`, and `paid_household_5`; associate both billing intervals
   with the matching paid entitlement.
4. Create `trial_premium` and `comp_premium`, `comp_household_2`,
   `comp_household_3`, `comp_household_4`, `comp_household_5` without paid products.
   These identifiers are reserved for grants. Never combine trial and comp grants
   under the same entitlement, because revocation is entitlement-scoped.
5. Create the `web` offering with all supported tier/interval packages and publish
   its web paywall. Configure responsive layouts, keyboard access, and branding.
6. Approve the SPA checkout domain in Paddle production. RevenueCat-presented
   checkouts require disabling Paddle abandoned-cart emails, whose checkout links
   cannot resume RevenueCat-created checkout flows.

The eleven entitlement keys, and the tier/source each must resolve to under
`domain.RevenueCatKey` (`backend/internal/domain/revenuecat.go`):

| Entitlement key | Source | Tier |
| --- | --- | --- |
| `trial_premium` | `trial` | `premium` |
| `paid_premium` | `paid` | `premium` |
| `paid_household_2` | `paid` | `household_2` |
| `paid_household_3` | `paid` | `household_3` |
| `paid_household_4` | `paid` | `household_4` |
| `paid_household_5` | `paid` | `household_5` |
| `comp_premium` | `comp` | `premium` |
| `comp_household_2` | `comp` | `household_2` |
| `comp_household_3` | `comp` | `household_3` |
| `comp_household_4` | `comp` | `household_4` |
| `comp_household_5` | `comp` | `household_5` |

`trial_` is valid only for `premium`; every tier accepts `paid_` and `comp_`.
Never combine trial and comp grants under one entitlement, because revocation is
entitlement-scoped.

Each `paid_<tier>` entitlement is attached to both Paddle prices for that tier.
These IDs come from `config/subscription_tiers.yaml`; treat that file as the
source of truth and re-read it before the bootstrap's catalog validation.

| Tier | Monthly price ID | Annual price ID |
| --- | --- | --- |
| `premium` | `pri_01jmwgcb66fqp57epz7abzcpym` | `pri_01jmwgcyc80s12t63w4nt2c4jy` |
| `household_2` | `pri_01jmwge4e7hhp52tz1fnt85hj8` | `pri_01jmwgeeemscr1nv0dh6mkhvdg` |
| `household_3` | `pri_01jmwgej5rqnmwevg4wmn8vgj0` | `pri_01jmwgerr3vmwsktmvt7m8jcbm` |
| `household_4` | `pri_01jmwgewjqtzy3kc05wvswk87r` | `pri_01jmwgf2tkx2hfz6jrs9m3jr1q` |
| `household_5` | `pri_01jmwgf798kbh5mkny2j7g08m4` | `pri_01jmwgfbpdpmjpxpkv7q39e3xs` |

References: [Paddle integration](https://www.revenuecat.com/docs/web/integrations/paddle),
[Web paywalls](https://www.revenuecat.com/docs/web/paywalls).

## Backend credentials

Create separate server keys for each RevenueCat project with only the permissions
needed for customer/entitlement/subscription reads, promotional grant management,
and customer erasure. Grant operations use the documented REST v1 promotional API;
ensure that the configured grant key supports that API. Never put these keys in a
`VITE_` variable or GitHub build argument.

The implementation's environment template is the source of truth for exact names.
All eight settings below are required in staging and production; the first six are
validated at startup by `backend/internal/config/revenuecat.go`, and the last two
are the retained Paddle prerequisites for plan-change and management-link flows.

| Variable | Scope / permission | Where it is set | What breaks without it |
| --- | --- | --- | --- |
| `REVENUECAT_PROJECT_ID` | Non-secret project identifier; must name the project the keys below belong to | Fly secret (staging/production); operator `.env` / QA compose | Staging and production startup validation fails; the bootstrap CLI refuses |
| `REVENUECAT_ENVIRONMENT` | `sandbox` for staging/QA, `production` for production | Fly secret / QA compose | Startup validation fails; must equal `PADDLE_ENVIRONMENT` or the bootstrap refuses |
| `REVENUECAT_SECRET_API_KEY` | REST v2 secret key: customer read/write, subscription read, entitlement read, app read | Fly secret | Startup validation fails; customer initialization/read, catalog validation and GDPR deletion are unavailable |
| `REVENUECAT_GRANT_API_KEY` | REST v1-compatible secret key: promotional grant and revoke | Fly secret | Startup validation fails; trials and comps cannot be granted or revoked |
| `REVENUECAT_WEBHOOK_AUTHORIZATION` | Exact `Authorization` header value configured on the RevenueCat webhook (include a `Bearer ` prefix only if you chose one, on both sides) | Fly secret, and the RevenueCat webhook config | Every delivery is rejected 401 (`invalid_signature`); access never synchronizes |
| `REVENUECAT_WEBHOOK_SIGNING_SECRET` | HMAC signing secret for `X-RevenueCat-Webhook-Signature` | Fly secret, and the RevenueCat webhook config | Every delivery is rejected 401; access never synchronizes |
| `PADDLE_ENVIRONMENT` | `sandbox`/`production`; must equal `REVENUECAT_ENVIRONMENT` | Fly secret / QA compose | The bootstrap refuses on a mismatch; this value also selects `config/subscription_tiers.sandbox.yaml` or `config/subscription_tiers.production.yaml`, so a wrong value loads the wrong price catalogue |
| `PADDLE_API_KEY` | Paddle API key carrying the exact permission `customer_portal_session.write` | Fly secret | Subscription management URLs stay empty: RevenueCat cannot resolve a self-service link |

Install secrets into the correct Fly application using secure input. Do not paste
actual values into shell history, PR comments, logs, or this document. Keep the
Paddle backend credentials required by our retained plan-change and erasure flows.

For the v2 key, enable customer read/write, subscription read, entitlement read,
and app read permissions. The last two cover catalog validation; customer write
is needed for initialization and GDPR deletion, not just grants. No RevenueCat
subscription cancellation/refund or chart-write permission is needed. Confirm
the endpoint permission labels in the [v2 customer reference](https://www.revenuecat.com/docs/api-v2/customer),
[subscription reference](https://www.revenuecat.com/docs/api-v2/subscription),
[entitlement reference](https://www.revenuecat.com/docs/api-v2/entitlement), and
[app reference](https://www.revenuecat.com/docs/api-v2/app).

Create a separate REST v1-compatible secret key for promotional grants and
revocation; do not assume a v2-only key works with v1. The public SDK key is
different from both server keys. See [API key configuration](https://www.revenuecat.com/docs/projects/authentication).

Use `fly secrets import --app staging-boostedbudget-api` (or
`fly secrets import --app boostedbudget-api` for production), feeding the six
RevenueCat `NAME=value` settings through secure stdin from your secret manager,
and keep the retained `PADDLE_ENVIRONMENT` / `PADDLE_API_KEY` values in place.
Keep `PADDLE_ENVIRONMENT` equal to `REVENUECAT_ENVIRONMENT`. Do not import
staging secrets into the production app. Restart/deploy the matching backend
after migrations; configured environments fail startup on partial settings.

## Webhook connection

Create one webhook integration in each RevenueCat project:

| Project | Webhook URL |
| --- | --- |
| Staging | `https://staging-api.boostedbudget.com/webhooks/revenuecat` |
| Production | `https://api.boostedbudget.com/webhooks/revenuecat` |

Set its Authorization value to exactly `REVENUECAT_WEBHOOK_AUTHORIZATION`
(including `Bearer ` if you chose that prefix). Enable HMAC signing and store
that integration's signing secret as `REVENUECAT_WEBHOOK_SIGNING_SECRET`.
The handler requires `X-RevenueCat-Webhook-Signature`, validates the raw request
body, and accepts timestamps only within five minutes. Keep server clocks in
sync. See [RevenueCat webhook setup](https://www.revenuecat.com/docs/integrations/webhooks).
Send exactly these event types (RevenueCat webhook configuration → event types).
The ingestion handler accepts any well-formed event that carries an `app_user_id`,
but these are the types the code classifies (`ClassifyAccessChange` in
`backend/internal/integration/revenuecat/webhook.go`); an unlisted type is folded
into the `other` metric label and still triggers a refresh.

- **Access-changing** (refresh entitlement state): `INITIAL_PURCHASE`, `RENEWAL`,
  `NON_RENEWING_PURCHASE`, `PRODUCT_CHANGE`, `SUBSCRIPTION_EXTENDED`,
  `TEMPORARY_ENTITLEMENT_GRANT`, `EXPIRATION`, `SUBSCRIPTION_PAUSED`.
- **Billing-truth lifecycle** (recorded, no access change): `CANCELLATION`,
  `UNCANCELLATION`, `BILLING_ISSUE`, `TRANSFER`.
- **Delivery probe**: `TEST` (returns `test_received`; creates no customer
  projection or inbox row).

Promotional events may have no app ID; filtering them out would break trials and
comps. Never route staging events to production.

After configuration, confirm that a valid delivery reaches the admin webhook
explorer and applies a customer projection. A delivery or queue acknowledgement
alone does not prove access was synchronized. Verify that invalid authentication
is counted separately and cannot be replayed as a trusted event.
A dashboard `TEST` event returns `test_received`; it intentionally creates no
customer projection or inbox row. Use a real sandbox lifecycle event to validate
the inbox/worker path.

## SPA configuration

The SPA needs only the matching RevenueCat **public web/Paddle API key**, expected
as `VITE_REVENUECAT_PUBLIC_API_KEY`. Vite variables are built into downloadable
JavaScript: they are public and require a new frontend build when changed.
Configure the appropriate GitHub deployment environment, not a shared value that
mixes staging and production. Never supply a secret RevenueCat API key here.

In GitHub **Settings → Environments → staging / production → Environment secrets**,
add `VITE_REVENUECAT_PUBLIC_API_KEY` using that project's Paddle app SDK key.
`deploy-to-staging.yaml` and `deploy-production.yml` hard-fail the build when it is
empty; `deploy-qa.yml` supplies the same secret to the container. Rerun the
appropriate frontend deploy after setting or changing it. There is no separate
RevenueCat server secret, project ID, or native key to configure in the SPA.

The SPA resolves the key from two sources, `window.ENV` first and
`import.meta.env` second (`apps/web/src/features/billing/revenueCatRuntime.ts`):

| Runtime | Where the key comes from | Which source wins |
| --- | --- | --- |
| Local Vite dev | `apps/web/.env.local` (operator-created, gitignored) | `import.meta.env.VITE_REVENUECAT_PUBLIC_API_KEY` |
| Staging / production SPA | GitHub environment secret, baked in at build time | `import.meta.env.VITE_REVENUECAT_PUBLIC_API_KEY` |
| Docker / QA container | `REVENUECAT_PUBLIC_API_KEY` at container start (falls back to `VITE_REVENUECAT_PUBLIC_API_KEY`), written into `window.ENV` by `apps/web/docker-entrypoint.sh` | `window.ENV.REVENUECAT_PUBLIC_API_KEY` |

QA accepts the same public key plus the six backend settings; keep all backend
settings blank only for deliberately unconfigured QA.

The SPA still uses Clerk for sign-in. Its RevenueCat identity must switch with the
Clerk user. No native SDK or mobile store key belongs in the SPA configuration.

## Manual versus automated

**Manual, human-owned and sensitive**: no CLI performs these: creating the
RevenueCat project and connecting the Paddle integration; granting the exact
Paddle permission `customer_portal_session.write`; creating entitlements,
products and the `web` offering; generating the REST v2 secret key, the grant key,
the webhook `Authorization` value and the signing secret; capturing the public SDK
key; installing secrets into Fly and GitHub; and rotating any of them.

**Performed by the bootstrap CLI** (`backend/cmd/revenuecat-bootstrap`): a
read-only inventory that validates all eleven entitlement mappings, the app's
Paddle environment and every customer's inventory before any apply; and, with
`--apply`, recording durable grant intents. It does **not** create projects, keys,
entitlements, products or webhooks, and it never changes access directly or sends
promotional mutations itself. See Bootstrap and validation.

## Key rotation

Rotate one key at a time and verify a real sandbox event reaches the inbox and
applies a projection before moving on. Never commit a value; names and scopes only.

- **REST v2 secret key / grant key**: generate a new key in RevenueCat with the
  same permission scope, import it into the matching Fly app
  (`fly secrets import`), and restart/redeploy. RevenueCat may accept both keys
  during the overlap, so rotate the value before revoking the old one.
- **Webhook `Authorization` value**: change it in RevenueCat's webhook config and
  in `REVENUECAT_WEBHOOK_AUTHORIZATION` in the same window. A mismatch rejects
  every delivery with 401 (`invalid_signature`) until both sides agree.
- **Webhook signing secret**: re-generate it in the integration and store it as
  `REVENUECAT_WEBHOOK_SIGNING_SECRET` before the next delivery; a delivery signed
  with the old secret fails HMAC verification.
- **Public SDK key**: rotate it in RevenueCat, update the matching GitHub
  environment secret, and rebuild the SPA (SPA configuration).

## Bootstrap and validation

This is a **prelaunch-only** cutover, not a paid-customer migration. Keep
registration, checkout, and admin grant writes closed for the whole inventory,
apply, and reconciliation window. Drain old trial/subscription workers before
starting the new version; do not run old and new access writers together.
Take the normal database backup and apply migrations through `000219` using
the existing deployment migration workflow. (Staging's maximum before this branch
is `000213`; the branch adds `000216` through `000219`, so `000219` is the post-merge
maximum. Re-derive it from `backend/migrations/` (take the highest `*.up.sql`
version) rather than trusting this number.) Do not run down migrations against
populated authority tables as a rollback strategy.

Run from `backend/` with the target environment's server credentials and
`DATABASE_URL` and the existing database `ENCRYPTION_KEY` securely injected.
Use the target database's existing key; never generate a new key for bootstrap.
`PADDLE_ENVIRONMENT` selects
`config/subscription_tiers.sandbox.yaml` or `.production.yaml`; verify every
monthly/yearly price ID against Paddle first. Replace the non-secret project
and app placeholders below with the same project's IDs. The app ID is the
Paddle app owning the SPA's public SDK key, not the project ID.

```bash
go run ./cmd/revenuecat-bootstrap --paddle-app-id app_REPLACE
go run ./cmd/revenuecat-bootstrap --paddle-app-id app_REPLACE \
  --apply --confirm-project proj_REPLACE --maintenance-confirmed
```

The first command is read-only and validates all eleven entitlement mappings,
the app's Paddle environment, and every customer's inventory before any apply.
The second records durable operations; it does not claim they have applied.
Existing fixed trial expiries and valid comps are preserved; consumed trials
are not restarted. Existing modern operations are not overwritten. Any legacy
Paddle customer/subscription ID or unexpected remote paid relationship refuses
this path: investigate and plan that migration rather than removing IDs to
force it through. Also inspect the Paddle account for subscriptions not mapped
to local users; the CLI cannot discover those through local identity inventory.

1. Run the bootstrap's read-only inventory. Verify there are no live paid customers
   before using the prelaunch cutover path.
2. Review existing trial ends, consumed eligibility and current valid comps.
   Bootstrap must preserve them; it must not restart trials or erase history.
3. Apply the bootstrap and verify every grant in RevenueCat and the admin user
   detail. Do not open purchases while bootstrap operations are unresolved.
4. In staging, create an account and confirm a 60-day card-free trial. Repeat login
   and retry processing: the expiry must stay fixed.
5. Make an actual Paddle sandbox purchase via the paywall. Confirm the paid plan
   and effective access on our page and in admin diagnostics. Check browser
   network/CSP errors and any enforcing edge policy for RevenueCat SDK/API and
   paywall assets; a successful frontend build does not validate those policies.
6. Exercise plan changes, cancellation, recovery and grants. Revoke a comp on a paid
   account and verify that paid access survives.
7. Exercise processing failure and DLQ retry; verify the same operation recovers
   without a second grant. Check metrics and timestamps, not only success toasts.
8. Confirm keys, project identity and webhook routing separately in production
   before enabling public purchases. Never use real production cards for automated
   tests. Production validation must distinguish configuration checks from an
   actual paid purchase.

Paddle webhook simulator events are not a substitute for actual sandbox purchases
when validating RevenueCat ingestion. Monitor stale customer projections, failed
grant operations and DLQ records during rollout. If validation fails, keep
purchases closed; do not enable a second Paddle entitlement authority.

## Operational recovery

Use admin user diagnostics to distinguish own paid/trial/comp access from
household-effective access and to inspect freshness and billing failures.
Queued grants/revocations are not access changes until provider readback confirms
them. Retry the existing failed operation; do not create a replacement grant to
clear an error. Replaying a completed receipt requests a customer refresh, not
another entitlement mutation. Never clear inbox deduplication to force replay.

Health metrics are local integration counters, not RevenueCat revenue charts.
Unavailable metrics must not be interpreted as zero. Check event deliveries,
processing attempts, stale projections, pending operation age, and the DLQ
separately. A duplicate paid relationship requires operator reconciliation;
the system does not automatically refund or cancel one.

GDPR erasure fences stop first-party grants, checkout, and customer recreation.
Provider failure preserves retry state and local identity. A previously issued
public SDK checkout cannot be revoked by this fence; reconcile any in-flight
purchase with Paddle. Canceling a deletion request does not automatically remove
the fence or recreate remote access. Treat this as an operator recovery case.

## Automated E2E boundary

The local and CI E2E runners deliberately clear RevenueCat server settings and
enable a simulated projection fixture in their disposable loopback `test`
database. Playwright's managed fixture command runs real PostgreSQL safety tests
before starting its heartbeat or running browser tests. Teardown checks both
process liveness and final database invariants, including changes since the last
heartbeat.
Only the configured primary and optional `TEST_CLERK_EMAIL_2` identities can
register fixtures, after authenticated profile verification. An existing
confirmed projection or erasure fence is never replaced. A managed test process
keeps only those unchanged fixtures fresh using current observation timestamps;
it does not relax the production fifteen-minute freshness rule or apply grant
operations. This tests application behavior against simulated confirmed access,
not RevenueCat connectivity, purchases, trial creation, or reconciliation.

Use test-mode Clerk credentials in the existing local E2E environment file.
On a shared machine, supply a unique numeric `GITHUB_RUN_ID` even locally: the
current E2E container cleanup is scoped to that run only when this variable is
set. Never run the fixture against a development or deployed database. The
real sandbox acceptance steps above remain required before cutover.
