# Production Paddle Catalog Design

## Goal

Create the initial live Paddle catalog for BoostedBudget and bind its production price IDs to the repository configuration tracked by GitHub issue #746.

## Catalog model

Production will use five standard Paddle products, one for each configured subscription tier:

| Internal tier | Customer-facing product | Seats | Monthly | Yearly |
| --- | --- | ---: | ---: | ---: |
| `premium` | BoostedBudget Premium | 1 | $8.99 | $79.00 |
| `household_2` | BoostedBudget Household (2) | 2 | $12.99 | $119.00 |
| `household_3` | BoostedBudget Household (3) | 3 | $14.99 | $139.00 |
| `household_4` | BoostedBudget Household (4) | 4 | $16.99 | $159.00 |
| `household_5` | BoostedBudget Household (5) | 5 | $18.99 | $179.00 |

Each product uses Paddle tax category `saas`. Each product has one monthly and one yearly recurring USD price, uses location-based tax calculation, and permits a quantity of exactly one. Products and prices carry structured custom data with the stable tier key and billing interval.

The existing 60-day trial remains application-managed. Paddle prices do not define a trial period, preventing a second billing-platform trial from beginning at checkout.

## Naming decision

Production uses polished customer-facing names rather than the sandbox catalog's lowercase machine names. The internal tier keys remain unchanged and webhook tier resolution continues to use configured Paddle price IDs.

The repository currently labels `household_5` as "Household (5+)" even though `users_included: 5` is enforced as a hard seat cap. That broader cleanup is tracked separately in #1467. The new production product uses the accurate "Household (5)" name immediately, without expanding #746 into a cross-application copy change.

## Repository integration

After Paddle creates the catalog, the ten live price IDs replace the null values in `config/subscription_tiers.production.yaml`. Backend configuration selection already loads this file when `PADDLE_ENVIRONMENT=production`.

Tests will load the production file explicitly and verify that all five tiers have non-empty, distinct monthly and yearly Paddle price IDs. The test will be written and observed failing before the configuration is updated.

## Operational safeguards

- Query the live catalog before creation and reuse an exact existing product or price if one appears.
- Create products and their dependent prices in one bounded Paddle operation where practical.
- Re-read the complete live catalog after creation and reconcile every amount, currency, cycle, tax mode, status, and product relationship.
- Set `PADDLE_ENVIRONMENT=production` in Fly configuration and `VITE_PADDLE_ENV=production` in the production frontend build so both applications select the live Paddle environment.
- Install the production API key and webhook secret as Fly secrets, and install the live client token as a GitHub `production` environment secret.
- The production notification destination is configured for all 12 events handled by the backend. Because the production API is offline during setup, defer delivery simulation and log verification until the first production deployment.

## Delivery

Work proceeds on `feature/746-production-paddle-catalog`. The production config and tests will be verified with the repository test suite, reviewed, committed with issue references, pushed, and opened as a pull request against `staging` with `Closes #746`.
