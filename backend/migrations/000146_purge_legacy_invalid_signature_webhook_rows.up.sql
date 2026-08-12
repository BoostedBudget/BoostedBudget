-- Issue #1514 (Codex adversarial review finding, High): closes the residual half of the
-- idempotency-poisoning vector. HandlePaddleWebhook/HandlePlaidWebhook, as of migration 000145,
-- never persist an unsigned/invalid-signature delivery into paddle_webhook_events/webhook_events
-- again — but paddle_webhook_events.event_id is UNIQUE NOT NULL, and any row already sitting in
-- these tables with signature_valid = false from BEFORE this fix shipped still occupies its
-- event_id, so a genuinely legitimate webhook reusing that same event_id would still be
-- suppressed by HandlePaddleWebhook's duplicate check. Purging every pre-existing
-- signature_valid = false row removes that residual poisoning risk without adding new
-- application-level "is this duplicate row actually trustworthy?" logic to the hot path.
--
-- Safe to run at any deployment stage: as of this writing there are zero production users (see
-- issue tracker), so this is a no-op in practice; it is still the correct one-time fix for
-- environments (staging/QA) that may carry pre-fix rows, and is idempotent (deletes nothing on a
-- second run once the offending rows are gone).
DELETE FROM paddle_webhook_events WHERE signature_valid = false;
DELETE FROM webhook_events WHERE signature_valid = false;
