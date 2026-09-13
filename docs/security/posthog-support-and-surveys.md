# PostHog Support and Surveys

Issue #1650 treats Support and Surveys as separate products with separate authority.

## Support

Support is an authenticated, user-requested communications feature; it does not depend on the
optional analytics-consent setting. The browser does not initialize its isolated Support PostHog
instance until all of these are true:

1. Clerk is loaded and has a signed-in user.
2. The current route belongs to the authenticated application shell.
3. `GET /api/v1/user/posthog-support-identity` returns the same Clerk subject the browser currently
   holds and a non-empty identity hash.

The backend derives the distinct ID exclusively from authenticated middleware state and computes
`HMAC-SHA256(distinct_id, POSTHOG_SECRET_API_TOKEN)`. The response is `Cache-Control: no-store`.
`POSTHOG_SECRET_API_TOKEN` is a backend/Fly secret and must never use a `VITE_` prefix or appear in
`window.ENV`.

The vendor container is hidden by default in CSS. An app-owned Support button hands off to the
PostHog widget only after verification. Logout, account switch, a public route, or verification
failure hides the widget, clears its verified identity, cancels its native polling, and resets the
isolated instance.

PostHog JS 1.430.2 provides the reply-refresh loop itself. It polls every 5 seconds while the
conversation is open and every 15 seconds while closed, backs HTTP 429 responses off to at most 60
seconds, generation-fences stale in-flight work, and stops on widget destroy/reset. The app pauses
the widget while the document is hidden. Do not add a second application polling loop.

Required PostHog project settings:

- Enable Support and its in-app widget.
- Restrict allowed domains to the production and staging app origins.
- Configure the project's secret API token as `POSTHOG_SECRET_API_TOKEN` in each backend deployment.
- Do not enable PostHog's identification form; identity comes from Clerk and the signed endpoint.

## Surveys

Surveys use another isolated instance and require all Support identity checks plus a `granted`
analytics lifecycle scoped to that same Clerk subject. Automatic survey display remains disabled.
The app fetches matching surveys and displays only surveys whose questions are all rating,
single-choice, or multiple-choice. Open and link questions, and choices with an open-text option,
are rejected as a whole before rendering. Withdrawal, logout, or account switch disposes the survey
extension and clears its queued work.

Required PostHog project settings:

- Create only popover surveys made entirely of rating and closed choice questions.
- Do not add free-text “Other” choices. The client guard rejects them, but the project constraint is
  still the operator-visible first line of defence.
- Target surveys to identified users as needed; the app will never load them for signed-out users.

References: [Support widget and identity verification](https://posthog.com/docs/support/widget) and
[custom survey display APIs](https://posthog.com/docs/surveys/implementing-custom-surveys).
