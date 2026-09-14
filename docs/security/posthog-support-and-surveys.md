# PostHog Support and Surveys

Issue #1650 treats Support and Surveys as separate products with separate authority.

## Support

Support is an authenticated, user-requested communications feature; it does not depend on the
optional analytics-consent setting. The browser does not initialize its isolated Support PostHog
instance, load its external widget code, create its local widget state, or contact PostHog for
Support until all of these are true:

1. Clerk is loaded and has a signed-in user.
2. The current route belongs to the authenticated application shell.
3. The user selects the app-owned **Support** button.
4. `GET /api/v1/user/posthog-support-identity` returns the same Clerk subject the browser currently
   holds and a non-empty identity hash.

The backend derives the distinct ID exclusively from authenticated middleware state and computes
`HMAC-SHA256(distinct_id, POSTHOG_SECRET_API_TOKEN)`. The response is `Cache-Control: no-store`.
`POSTHOG_SECRET_API_TOKEN` is a backend/Fly secret and must never use a `VITE_` prefix or appear in
`window.ENV`. This HMAC is deterministic and does not expire; it remains valid until the server
secret is rotated. It is held only in memory by the active page and is never persisted by the app.

The signature's non-expiry means it is not itself a revocation credential. While Support is active,
the browser revalidates the signed identity endpoint every 15 seconds, including in background tabs;
an error, account-erasure fence, focus return, logout, or account switch shuts down the transport and
removes the widget before it can be shown again. Returning to a visible tab always performs a fresh
authority check before reactivation.

The vendor container is hidden by default in CSS. An app-owned Support button hands off to the
PostHog widget only after verification. Logout, account switch, a public route, or verification
failure hides the widget, clears its verified identity, cancels its native polling, and resets the
isolated instance.

Support necessarily sends the text the user enters to PostHog. For a new ticket, the SDK also
provides page context; the app's pinned outbound-request boundary reduces that value to a sanitized
route with query strings, fragments, capability tokens, account IDs, and budget IDs removed. Session
replay is disabled and replay URLs and PostHog analytics session IDs are removed from every
Conversations payload. The SDK creates a local `ph_conv_<project token>` widget/session record only
after Support is selected; authenticated requests use the verified Clerk identity rather than that
widget session ID, and teardown removes the local record.

PostHog JS 1.430.2 provides the reply-refresh loop itself. It polls every 5 seconds while the
conversation is open and every 15 seconds while closed, backs HTTP 429 responses off to at most 60
seconds, generation-fences stale in-flight work, and stops on widget destroy/reset. The app pauses
the widget while the document is hidden. A pinned transport boundary aborts outstanding requests,
settles their callbacks as inactive, and discards queues before shutdown so a late response cannot
restore old-account state. Remote configuration remains enabled because both product extensions need
it, while analytics, flags, replay, and automatic capture remain disabled. The same exact-version
lifecycle boundary removes SDK window listeners on teardown so repeated route and consent changes do
not retain inactive clients. Do not add a second application polling loop.

Required PostHog project settings:

- Enable Support and its in-app widget.
- Restrict allowed domains to the production and staging app origins.
- Configure the project's secret API token as `POSTHOG_SECRET_API_TOKEN` in each backend deployment.
- Do not enable PostHog's identification form; identity comes from Clerk and the signed endpoint.

## Surveys

Surveys use another isolated instance and require the same authenticated-route and server-verified
identity checks plus a `granted` analytics lifecycle scoped to that Clerk subject. Surveys do not
require the user to open Support. Automatic survey display remains disabled.
The app fetches matching surveys and displays only surveys whose questions are all rating,
single-choice, or multiple-choice. Open and link questions, and choices with an open-text option,
are rejected as a whole before rendering. Withdrawal, logout, or account switch disposes the survey
extension and clears its queued work.

The Surveys instance has its own stable `boostedbudget_surveys` session-storage namespace and cannot
read or delete the primary analytics identity. PostHog's unnamespaced survey display keys are cleared
on logout, consent withdrawal, and account switch to prevent cross-account state bleed. A temporary
route exclusion preserves those keys while replacing the isolated client, so returning to the app
does not redisplay a survey already seen or answered in that session.

Required PostHog project settings:

- Create only popover surveys made entirely of rating and closed choice questions.
- Do not add free-text “Other” choices. The client guard rejects them, but the project constraint is
  still the operator-visible first line of defence.
- Target surveys to identified users as needed; the app will never load them for signed-out users.

References: [Support widget and identity verification](https://posthog.com/docs/support/widget) and
[custom survey display APIs](https://posthog.com/docs/surveys/implementing-custom-surveys).

## GDPR erasure

Analytics-consent withdrawal stops future optional analytics capture; it is not an erasure request.
Account erasure creates an encrypted PostHog obligation in the same database transaction as the
local hard delete. The obligation includes the current Clerk subject, recorded historical Clerk
identities, and the legacy internal user UUID, but never an email address. It survives the user-row
cascade and retries independently of PostHog availability. See the
[PostHog erasure runbook](../runbooks/posthog-gdpr-erasure.md) for provider capabilities and the
mandatory manual Support/survey-response verification phase.

The obligation first durably retries deletion of the canonical Clerk user. A keyed, non-recoverable
Clerk-subject fence survives the local user cascade and permanently prevents middleware
auto-registration or new Support identity issuance for that subject. PostHog deletion cannot begin
until Clerk deletion is confirmed and a 30-second database-clock cutoff—twice the foreground
revalidation interval—has elapsed. Provider completion additionally requires that Clerk checkpoint.
This is the application revocation boundary; it does not misrepresent PostHog's deterministic HMAC
as expiring. A request already accepted by PostHog before browser teardown is covered by the later
PostHog deletion and manual Support zero verification.
