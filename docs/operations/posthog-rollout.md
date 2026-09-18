# PostHog project rollout

`config/posthog_rollout.yaml` is the reviewable desired state for PostHog. CI validates it against
the event and product-metric contracts, but nothing in this repository applies PostHog project
settings. This runbook is therefore an operator procedure, not evidence that remote configuration
exists.

## What the application already enforces

- Browser analytics and custom surveys require the user's analytics consent. Autocapture, session
  replay, automatic survey display, flags, and persistence before consent are disabled in code.
- Support is an authenticated, user-requested service. Surveys require authentication, a verified
  Clerk identity, and consent. Public routes cannot initialize either isolated SDK instance.
- Support identity verification uses the authenticated Clerk subject and the server-only
  `POSTHOG_SECRET_API_TOKEN`. Logout, account switch, public-route navigation, or verification
  failure tears the instance down. The Support SDK owns its reply polling; do not add app polling.
- Frontend, backend, and MCP custom events pass through consent-aware, schema-aware boundaries.
  `config/posthog_events.yaml` is the property allowlist and `config/product_metrics.yaml` is the
  metric decision contract.
- `account_link_completed` is emitted once, with `provider: plaid`, only after the backend accepts
  the Plaid token exchange. The former `plaid_link_succeeded` and cache-derived
  `first_account_linked` events are deprecated.

See [Support and Surveys](../security/posthog-support-and-surveys.md), [browser privacy](../security/posthog-pii-masking.md),
and [backend analytics](../security/posthog-backend-analytics.md) for the enforcement details.

## Ownership and evidence

Product Analytics owns the PostHog projects, dashboard definitions, exclusion cohorts, and this
runbook. The deploy operator owns environment-scoped secrets. A remote change is complete only when
the evidence record contains the date, operator, deployment commit, environment, PostHog project ID,
setting or entity ID, and a redacted screenshot or API response. Never record tokens, Clerk subjects,
message text, survey answers, or event payload values in that record.

Keep staging and production evidence separate. Apply every change to staging, complete the staging
verification below, and obtain Product Analytics approval before repeating it in production.

## 1. Isolate and configure the projects

Create or select two distinct US Cloud projects:

| Environment | Project label | Exact application origin |
|---|---|---|
| staging | `boostedbudget-staging` | `https://staging.boostedbudget.com` |
| production | `boostedbudget-production` | `https://boostedbudget.com` |

For each project:

1. Record its immutable project ID in the private deployment evidence.
2. Set the project timezone to UTC and enable IP-address discarding. PostHog documents that IP
   discarding is an organization setting for newly created projects and a project setting for
   existing projects; verify the effective project rather than assuming inheritance.
3. Allow only the environment's exact origin. Do not add wildcards, localhost, preview deployments,
   or the other environment's origin.
4. Enable the Support product/API, but turn off PostHog's project-rendered widget launcher. The
   app-owned bottom-right **Support** control is the sole launcher because it can enforce the
   authenticated route and verified identity boundary. Enabling PostHog's launcher recreates the
   original logged-out-homepage defect.
5. Enable Surveys and Web Analytics. Keep session replay, Error Tracking, experiments, feature
   flags, logs, and tracing disabled at merge. Session replay merges INERT and is enabled only
   through the **Replay enablement gate** below; Error Tracking, experiments, feature flags, logs,
   and tracing follow in issue #1651.
6. Disable PostHog autocapture, heatmaps (the project analysis toggle), and its Support
   identification form. Clerk plus the signed backend endpoint is the only Support identity source.

Configure environment-scoped deployment secrets with credentials from that environment's project:

- frontend build: `VITE_POSTHOG_KEY` (the deployment workflows source this from
  `POSTHOG_API_KEY`) and optional `VITE_POSTHOG_HOST=https://us.i.posthog.com`;
- backend: `POSTHOG_API_KEY`, `POSTHOG_HOST=https://us.i.posthog.com`, and
  `POSTHOG_SECRET_API_TOKEN`;
- erasure worker, when deployed: a least-privilege `POSTHOG_PERSONAL_API_KEY`, the environment's
  `POSTHOG_PROJECT_ID`, and `POSTHOG_PRIVATE_HOST=https://us.posthog.com`.

Never reuse a project token, project ID, Support secret, or personal API key across environments.
Removing the public project token and redeploying is the browser kill switch; removing the backend
project token and restarting is the backend analytics kill switch.

## 2. Exclude staff and test identities

In each project, create a static cohort named `boostedbudget_internal_and_test`. Populate it with a
private CSV or PostHog's individual-person selector using Clerk subjects as PostHog distinct IDs.
Use no email addresses and do not add an `is_internal` browser property.

The Product Analytics owner updates membership whenever a staff or durable test identity changes,
reviews it quarterly, and records only membership counts plus the cohort ID in evidence. Apply
`is not a member of boostedbudget_internal_and_test` to every metric insight and dashboard tile.
Static cohorts are project-local, so repeat and verify this process independently in staging and
production.

## 3. Add defence-in-depth ingestion scrubbing

The application boundary is authoritative. In staging, create an incoming-event transformation
that applies only to cataloged BoostedBudget custom events and removes properties not allowed by
that event's entry in `config/posthog_events.yaml`, apart from PostHog metadata required for
ingestion. Preview the transformation against captured staging payloads before enabling it.

Do not make a global “drop unknown event” rule. Native `$identify`, Support, and Survey events are
outside the custom event catalog and would be broken by such a rule. Do not attach the
transformation to those native events. The transformation is a second line of defence; an event
rejected by the app or gateway must not be made acceptable only by remote scrubbing.

PostHog's transformation product and UI can change independently of this repository. Have a second
operator review the actual event selector, preview output, and deployment ordering in the current UI,
then capture its transformation ID and redacted test output. If the current plan cannot scope the
transformation this narrowly, leave it disabled and keep `legacy_event_schemas_unreviewed` open.

## 4. Create the reporting views

Every Product Analytics insight uses UTC, excludes the static internal/test cohort, and displays the
consent caveat from `config/product_metrics.yaml`. A zero denominator renders null, not zero percent.

### MCP Product Health — ready to create

Create one weekly dashboard from `mcp_tool_completed`:

- distinct consenting MCP users;
- completion count;
- succeeded completions divided by all completions;
- bounded breakdowns for `tool_surface`, `tool_family`, `outcome`, and `duration_bucket`.

Do not include requests rejected before an authenticated tool callback begins. Confirm the insight's
event and property names against the generated contract before saving it.

### Observed Product Health — blocked pending post-deploy validation

Do not label this dashboard live yet. After staging verification, create these six insights directly
from the formulas in `config/product_metrics.yaml`:

1. `weekly_observed_value_users`: distinct users with a curated value action per UTC week.
2. `observed_linked_account_activation`: ordered first `account_link_completed`, initial
   `financial_data_sync_completed`, then curated value within seven days. Filter
   `account_link_completed.provider = plaid`; exclude re-links and incomplete windows.
3. `observed_w1_retention`: activated users with curated value in days 7–13, using only cohorts at
   least fourteen days old.
4. `observed_w4_retention`: activated users with curated value in days 28–34, using only cohorts at
   least thirty-five days old.
5. `consenting_subscription_conversion`: activated trial users with both `subscription_created`
   and `payment_succeeded` inside the complete sixty-day trial window; exclude complimentary,
   failed, and refunded cases.
6. `feature_adoption`: distinct weekly observed-value users performing each curated feature event,
   divided by the same week's observed-value users.

Prefer ordered funnel and retention insights where their semantics exactly match the contract. Use
SQL insights when exclusions or complete-window constraints cannot be expressed without changing
the denominator. Save the insight IDs in deployment evidence. Only a reviewed code change may move
a metric's `availability` from `planned` to `live`.

### Native Support and Survey views

Support and Surveys are product-native views, not custom analytics metrics:

- Support: review new/open tickets, unresolved age, and first-response time. Never put message text
  on a shared analytics dashboard or treat analytics consent as the Support denominator.
- Surveys: review response rate and distributions for rating, single-choice, and multiple-choice
  questions. Do not create open-text, link, or free-text “Other” questions.

Verify from a public session that neither product is available, then from an authenticated session
that Support appears only after the app Support button and surveys only after analytics consent.

## 5. Add alerts only after a useful baseline

Do not enable the proposed `mcp_success_rate_degradation` alert at rollout. Wait for at least eight
complete weekly periods with at least 100 completions in every evaluated period. In PostHog, simulate
a weekly MAD anomaly detector over the historical series and choose the least-sensitive threshold
that would have found a meaningful degradation without routine noise. Record the simulation window
and result before enabling the notification to the MCP Product owner.

If the baseline is seasonal, sparse, or fails the denominator floor, keep the alert deferred. Do
not alert on raw low-volume percentages. PostHog anomaly alerts are beta; re-simulate after any
metric-definition change.

## 6. Least-privilege PostHog MCP access

Use a separately named and project-pinned connection for each environment. For routine inspection,
use OAuth where supported, CLI mode, read-only mode, and only the required feature categories:

```text
https://mcp.posthog.com/mcp?mode=cli&readonly=true&features=alerts,cohorts,conversations,dashboards,events,insights,surveys
```

Set `x-posthog-project-id` to the one environment's project ID. Pinning removes the project-switching
tools. If OAuth is unavailable, create a personal key with PostHog's MCP Server preset and keep its
Bearer header in the operator's secret store, never in this repository.

Read-only MCP cannot perform this rollout. For an approved mutation, create a separate short-lived,
project-pinned session, filter to the exact feature or tool needed, review each proposed tool call,
and revoke its key or OAuth grant afterward. If the MCP server does not expose a required project
setting, perform that step in the PostHog UI and record evidence; do not broaden agent access merely
to search for another route.

## 7. Staging payload and lifecycle verification

Use a designated staging test identity that is not in the exclusion cohort while collecting the
verification payloads, then add it to the cohort and prove the dashboard excludes it.

- Signed out on the homepage: no PostHog-rendered launcher, app Support button, Support widget,
  Survey UI, or Support/Survey requests; no analytics request before consent. Navigating between
  public routes must not initialize either isolated product.
- Signed in with analytics denied: no custom analytics or Survey capture. Support initializes only
  after selecting the app-owned button and only after the signed identity endpoint succeeds.
- Signed in with analytics granted: a sanitized `$pageview` and cataloged events contain no email,
  financial value, account/token value, query string, or unbounded free text. The PostHog distinct ID
  equals the Clerk subject and the effective stored IP is absent.
- Complete a Plaid link: exactly one `account_link_completed` appears after successful token exchange
  with only `provider: plaid`; neither `plaid_link_succeeded` nor `first_account_linked` appears.
- Trigger one initial sync and curated value action: event order and bounded properties match the
  activation contract. Do not infer activation from a dashboard until the full seven-day window is
  testable.
- Open Support, send a test ticket, send an operator reply, and keep the conversation open. The reply
  appears through the SDK refresh loop without a page reload. Hide the tab, return, log out, and
  confirm polling pauses/resumes and then stops without restoring the old identity.
- Create one staging-only closed-answer survey. It never appears signed out or without consent,
  records the answer once, and does not redisplay in the same session.
- Complete one consenting MCP tool call. Confirm one bounded `mcp_tool_completed`; a denied-consent
  call emits none.
- Confirm there are no `$autocapture`, replay, feature-flag, exception, log, or trace payloads.
- Confirm staging traffic appears only in the staging project and production traffic only in the
  production project. Verify the internal/test cohort excludes the designated identity afterward.

Record event IDs and timestamps only when they contain no sensitive values. When this checklist and
remote-setting evidence are complete, close `remote_configuration_unapplied`; evaluate
`legacy_event_schemas_unreviewed`; and move each proven metric to `live` in a reviewed code change.

## Replay enablement gate

Session replay merges INERT: `project_controls.session_replay` is `false` and the client pin
(`disable_session_recording: true`) is in code. `config/posthog_rollout.yaml` records the operator
contract — `session_replay_target: true` plus `enablement_gates.session_replay:
staging_inspection_required`. The validator couples the target to this section. Do not flip
`project_controls.session_replay` to `true`, and do not enable replay in production, until every
step below passes in staging and is recorded in the deployment evidence.

**PRE-MERGE BLOCKING STEP (do not skip): the `$unset`-ingestion-survival check.** The identified-
diagnostics opt-out emits a post-commit `$unset` when a user turns identified diagnostics off. Before
this branch is merged, run that opt-out against staging and confirm the `$unset` SURVIVES ingestion
(it is not dropped or rewritten by PostHog's transformation). **If it does not survive, raise with
PostHog BEFORE MERGE — do not merge with a dead opt-out.** This is gated by the
`unset_ingestion_survival_unverified` coverage gap, which stays `open` until the check passes.

1. **Payload OFF→ON.** Capture a replay payload with the current config while replay is disabled,
   then enable the client recorder for the test identity only and capture the same route with it ON.
   Diff the two: the ON payload must carry a `$snapshot` whose element attributes and text follow the
   masking contract, and the OFF payload must carry none.
2. **Resolved masking sentinels — text AND attributes.** In the ON `$snapshot`, confirm a
   `[data-ph-unmask]` sentinel survives verbatim while ordinary text is asterisk-masked
   (`maskTextSelector: '*'`), AND confirm an attribute sentinel (`aria-label`, `title`, `placeholder`,
   `alt`) is masked by `maskAllElementAttributes: true`. If attributes arrive UNMASKED, replay
   enablement stays BLOCKED — do not enable it. Record both the text and attribute outcomes.
3. **Remote-config-arrival fallback.** If the replay payload arrives before the remote project
   config does (or the remote config is unavailable), confirm the client defaults are the safe
   defaults — masking ON, `disable_session_recording` behaviour unchanged — and that no unmasked
   snapshot is captured during the window. If remote-config arrival cannot be proven to fail closed,
   keep replay disabled.
4. **CSP enforcement — BLOCKING.** `apps/web/nginx.conf` ships the Content-Security-Policy in
   Report-Only mode, and `disable_external_dependency_loading: false` adds a runtime third-party
   recorder fetch. **Before enabling replay, flip the CSP from Report-Only to enforcing** (the
   recorder host `us-assets.i.posthog.com` is already in `script-src`) and confirm no CSP violation
   blocks the recorder. A Report-Only CSP is not a gate.
5. **Heatmap-erasure counts.** On an ANONYMOUS route with a synthetic `distinct_id`, capture
   `$$heatmap` data, record the pre-erasure count, run the erasure path, and record the post-erasure
   count. Both counts are the exit evidence for `heatmap_erasure_unverified`; until they are
   recorded, the gap stays `open` and heatmap capture is not trusted.
6. **Operator flip for identified diagnostics (R16-05).** Once the `$unset`-ingestion-survival
   check in the PRE-MERGE step passes and `unset_ingestion_survival_unverified` is closed, flip
   `identified_diagnostics_opt_out_certification.status` from `blocked` to `go` in a reviewed code
   change. This certifies the OPT-OUT PATH (post-commit `$unset` survives ingestion) only — it does
   NOT certify feature liveness, because identified diagnostics is already live at merge. The
   validator fails if the status is `go` while the gap is still `open`.

Only after steps 1–5 pass and step 6's precondition holds may the operator flip
`project_controls.session_replay` to `true` in the PostHog project and record the setting ID,
date, operator, and commit in the deployment evidence.

## Incident kill switches

1. Browser telemetry incident: remove `VITE_POSTHOG_KEY`/its deployment secret and redeploy. Confirm
   analytics, Support, and Surveys make no PostHog requests.
2. Backend analytics incident: remove `POSTHOG_API_KEY` and restart every backend instance. Confirm
   the no-op analytics path and queue health.
3. Support-only incident: disable the Support widget in the affected project; remove or rotate the
   Support secret if identity verification is implicated.
4. Survey-only incident: archive every active survey in the affected project.

Apply a kill switch to the affected environment first. If project isolation is in doubt, disable
both environments and investigate before restoring either.

## Known coverage gaps

| Gap | Consequence | Exit condition |
|---|---|---|
| Remote configuration unapplied | No remote project, dashboard, cohort, or alert may be claimed live | All staging and production evidence above is recorded |
| Legacy event schemas unreviewed | Legacy callers may carry properties less precise than the v2 contract | Inventory payloads, migrate or deprecate each legacy schema, and verify the transformation |
| Planned metrics post-deploy validation | Activation, retention, adoption, and subscription dashboards can misstate reality | Required windows and exclusions are reproduced from clean staging payloads, then approved in code |
| Tracing/spans deferred | Cross-service traces and LLM/MCP spans are intentionally absent | Issue #1651 defines privacy, sampling, retention, and cost controls before enablement |

## Official PostHog references

- [Controlling data collection](https://posthog.com/docs/privacy/data-collection)
- [JavaScript configuration](https://posthog.com/docs/libraries/js/config)
- [Support widget and identity verification](https://posthog.com/docs/support/widget)
- [Custom Survey APIs](https://posthog.com/docs/surveys/implementing-custom-surveys)
- [Cohorts, including static cohorts](https://posthog.com/docs/data/cohorts)
- [Alerts and anomaly simulation](https://posthog.com/docs/alerts)
- [PostHog MCP advanced setup](https://posthog.com/docs/model-context-protocol/faq)
