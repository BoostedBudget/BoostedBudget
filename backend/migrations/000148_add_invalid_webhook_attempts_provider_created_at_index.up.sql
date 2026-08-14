-- Issue #1514, requirement #5 (Phase 2 gap fix): restores operator visibility into
-- invalid_webhook_attempts (the audit signal for rejected/forged webhook deliveries) via a new
-- repository read method, InvalidWebhookAttemptRepository.GetStats24h, backing
-- /admin/health's "Rejected webhook attempts (24h)" tile.
--
-- GetStats24h's query is `WHERE created_at >= <cutoff> GROUP BY provider` — it does NOT filter on
-- provider at all (it wants every provider's rows in the window). Codex adversarial review caught
-- an earlier version of this migration leading with `provider` (i.e. `(provider, created_at)`),
-- which cannot satisfy that predicate as a single bounded range scan: with no equality
-- constraint on the leading column, Postgres must consider every distinct provider value's
-- sub-range separately. Leading with `created_at DESC` instead lets Postgres do one index range
-- scan from "now" backward to the cutoff and stop — provider is carried along in the same index
-- entry so no heap access is needed either. (In practice, with rows capped at
-- invalidWebhookAttemptCap per provider and few providers, this table stays small enough that
-- either column order would perform fine — but the comment on an index should describe the scan
-- it can actually do, not one it can't.)
CREATE INDEX IF NOT EXISTS idx_invalid_webhook_attempts_created_at_provider
    ON invalid_webhook_attempts (created_at DESC, provider);

COMMENT ON INDEX idx_invalid_webhook_attempts_created_at_provider IS 'Issue #1514 requirement #5: backs InvalidWebhookAttemptRepository.GetStats24h (per-provider count + most-recent-attempt over the trailing 24h, filtered on created_at with no provider constraint) used by /admin/health.';
