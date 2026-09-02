-- #1569 Task 6 (#1579), review fix round 2 (D-4): the settlement move overwrites an annotation too,
-- so it needs a change_source of its own.
--
-- WHAT WAS WRONG. 000169 shipped with two sources, 'category' and 'review', on the belief that the
-- only statements overwriting a transaction's annotation were the three user-facing ones. The
-- guard added in fix round 1 carried an allowlist entry excusing TombstoneByExternalID's settlement
-- move on the grounds that it is "fill-only in every column it touches". Re-derived from the SQL,
-- THAT REASON IS FALSE, and it is the third false allowlist reason this issue has produced.
--
-- The category leg is gated by settlementCategoryMoveCond:
--
--     (s.category_source = 'manual' AND twin.category_source IS DISTINCT FROM 'manual')
--
-- which is MANUAL-WINS, not fill-only. `IS DISTINCT FROM 'manual'` admits a twin whose
-- category_source is 'rule' — and rules run in place before the posted twin is ever inserted, so a
-- rule-categorised twin is the COMMON case, not an edge one (transaction_repository.go's own
-- comment on that constant says so). When it fires, all seven category columns on the twin are
-- replaced by the pending row's, and nothing recorded what they had been.
--
-- The other legs really are fill-only, re-derived column by column:
--   user_notes   COALESCE(twin, s)                      — only fills a NULL
--   tags         gated on the twin having none          — only fills an empty array
--   reviewed     twin.reviewed OR <cond>                — monotonic, never true -> false
--   reviewed_at  keeps twin's whenever twin.reviewed    — only fills an unreviewed row
--
-- WHY A THIRD SOURCE AND NOT 'category'. change_source exists to tell a reader whether the user
-- asked for the change. A settlement move is neither: the provider settled a pending charge and the
-- sync propagated the user's older manual category onto the posted twin. Filing it under 'category'
-- would claim the user recategorised the twin, which they did not — and the row's whole value is
-- that it explains why the twin stopped agreeing with its own rule_execution_history entry.

-- squawk-ignore prefer-robust-stmts
ALTER TABLE transaction_annotation_history
    DROP CONSTRAINT IF EXISTS chk_transaction_annotation_history_source;
-- squawk-ignore constraint-missing-not-valid
ALTER TABLE transaction_annotation_history
    ADD CONSTRAINT chk_transaction_annotation_history_source
    CHECK (change_source IN ('category', 'review', 'settlement'));
