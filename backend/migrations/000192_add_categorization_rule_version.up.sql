-- #1625 final review B2: give categorization_rules a version that only USER edits move.
--
-- THE DEFECT
--   Task 19 closed a lost update by guarding Update with `WHERE id = $13 AND updated_at = $14`.
--   updated_at is the wrong key. The table carries a BEFORE UPDATE trigger
--   (update_categorization_rules_updated_at) that stamps NOW() on EVERY write, and
--   IncrementMatchCount/IncrementOverrideCount are writes — engine bookkeeping that runs during an
--   ordinary Plaid sync with no user involved. A sync landing inside a user's read-modify-write
--   window therefore refused their save with "This rule was changed by another request", which is
--   FALSE: nothing they can see changed. The likelihood scaled with how WELL the rule worked.
--
-- WHY NOT JUST REVERT THE GUARD
--   Dropping back to `WHERE id = $13` reinstates the lost update Task 19 existed to close. A false
--   409 and a silent lost update are both wrong, but only one of them is invisible to the user.
--
-- WHAT version MEANS
--   The number of USER-FACING changes this rule has had. The test a write must pass is "would the
--   user SEE this change if they were looking at the rule?", not "is this a write".
--   Match/override counters deliberately do not bump it, and
--   TestCategorizationRuleRepository_MatchCountBumpIsNotAConflict_Integration asserts that on the
--   COLUMN rather than merely on the absence of a conflict.
--
--   ⚠ #1625 third fix wave F1: an earlier revision of this file named ONE writer and said "and by
--   nothing else". There are TWO, and the second arrived on this same branch (second fix wave H1):
--     * CategorizationRuleRepository.Update -- the read-modify-write behind UpdateRule and
--       ToggleRule.
--     * CustomCategoryRepository's category tombstone -- deleting a custom category disables the
--       rules that targeted it, which the user sees as the card flipping to "Disabled". Pinned by
--       TestCategoryTombstone_DisablingRulesMovesTheConcurrencyToken_Integration.
--   Do not add a third by matching that list; add one by satisfying the test above.
--
-- NOT NULL DEFAULT 1 rather than 0: every existing row is "version 1", and a zero would be
-- indistinguishable from an uninitialised in-memory struct at the Go boundary — a guard that
-- silently matches on the zero value is not a guard.
--
-- No index: every read of this column is by primary key.
ALTER TABLE categorization_rules
    ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT 1;

COMMENT ON COLUMN categorization_rules.version IS
    'Optimistic-concurrency token for USER-facing changes. Moved by a write the user would SEE if they were looking at the rule -- today CategorizationRuleRepository.Update (the read-modify-write behind UpdateRule and ToggleRule) and CustomCategoryRepository''s category tombstone, which disables the rules that targeted the deleted category. IncrementMatchCount and IncrementOverrideCount are background bookkeeping and must NOT move it, or an ordinary sync would refuse the user''s save as a conflict. updated_at is unsuitable for this because a BEFORE UPDATE trigger stamps it on every write, background ones included. #1625.';
