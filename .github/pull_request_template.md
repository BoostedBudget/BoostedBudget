## Summary
<!-- 1-3 bullets describing the change -->

Closes #

## Test plan
- [ ]

## PII / session-replay checklist
- [ ] No new currency, balance, account-mask, or merchant/nickname/institution/tag renders introduced (or all wrapped in `data-ph-mask`)
- [ ] No PII placed in `aria-label`, `title`, `placeholder`, `alt`, or URL search params
- [ ] No PII passed as `trackEvent` event properties (use IDs)
- [ ] `bun run test src/test/pii-coverage.test.tsx` passes if frontend pages were touched
- [ ] If PostHog session-replay or autocapture config changed, `docs/security/posthog-pii-masking.md` updated
