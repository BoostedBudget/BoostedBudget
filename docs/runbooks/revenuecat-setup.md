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
BoostedBudget UUID, or anonymous purchase identities.

No Apple or Google store configuration is required for this issue.

Use these existing deployment targets; configure each environment independently:

| Environment | GitHub environment | Backend Fly app | SPA |
| --- | --- | --- | --- |
| Staging | `staging` | `staging-boostedbudget-api` | `https://staging.boostedbudget.com` |
| Production | `production` | `boostedbudget-api` | `https://boostedbudget.com` |

The frontend deployment jobs build separately for their matching GitHub
environment. Set the public key there before rebuilding; configuring only the
backend Fly secrets will not configure the SPA.

## Product catalog

In each project:

1. Connect the matching Paddle account using RevenueCat's Paddle integration setup.
   Grant the documented Paddle permissions, including customer portal session write
   permission when using authenticated management links.
2. Import the existing monthly and annual prices for Premium and Household 2–5.
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

References: [Paddle integration](https://www.revenuecat.com/docs/web/integrations/paddle),
[Web paywalls](https://www.revenuecat.com/docs/web/paywalls).

## Backend credentials

Create separate server keys for each RevenueCat project with only the permissions
needed for customer/entitlement/subscription reads, promotional grant management,
and customer erasure. Grant operations use the documented REST v1 promotional API;
ensure that the configured grant key supports that API. Never put these keys in a
`VITE_` variable or GitHub build argument.

The implementation's environment template is the source of truth for exact names.
Required settings (all six are required in staging and production):

| Variable | Purpose | Visibility |
| --- | --- | --- |
| `REVENUECAT_PROJECT_ID` | Matching RevenueCat project | Backend configuration |
| `REVENUECAT_ENVIRONMENT` | `sandbox` for staging; `production` for production | Backend configuration |
| `REVENUECAT_SECRET_API_KEY` | REST v2 customer create/read/delete and catalog/subscription reads | Server secret |
| `REVENUECAT_GRANT_API_KEY` | REST v1 grants and revocation | Server secret |
| `REVENUECAT_WEBHOOK_AUTHORIZATION` | Exact webhook authorization header value | Server secret |
| `REVENUECAT_WEBHOOK_SIGNING_SECRET` | Webhook HMAC verification | Server secret |

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
`NAME=value` settings through secure stdin from your secret manager. Keep the
existing `PADDLE_ENVIRONMENT` equal to `REVENUECAT_ENVIRONMENT`. Do not import
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
Send all relevant customer lifecycle and promotional events to the matching
environment. Promotional events may have no app ID; filtering them out would
break trials and comps. Never route staging events to production.

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
Both deployment workflows require it, so rerun the appropriate frontend deploy
after setting or changing it. There is no separate RevenueCat server secret,
project ID, or native key to configure in the SPA. Docker deployments instead
accept `REVENUECAT_PUBLIC_API_KEY` at container startup, with
`VITE_REVENUECAT_PUBLIC_API_KEY` as a fallback. Local Vite reads
`apps/web/.env.local`. QA accepts the same public key plus the six backend
settings; keep all backend settings blank only for deliberately unconfigured QA.

The SPA still uses Clerk for sign-in. Its RevenueCat identity must switch with the
Clerk user. No native SDK or mobile store key belongs in the SPA configuration.

## Bootstrap and validation

This is a **prelaunch-only** cutover, not a paid-customer migration. Keep
registration, checkout, and admin grant writes closed for the whole inventory,
apply, and reconciliation window. Drain old trial/subscription workers before
starting the new version; do not run old and new access writers together.
Take the normal database backup and apply migrations through `000210` using
the existing deployment migration workflow. Do not run down migrations against
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
