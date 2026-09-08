-- Issue #1609 Item 11: RE-ISSUE the COMMENT on custom_categories.parent_pfc_primary. The wording
-- 000065 shipped described the column as decoration:
--
--     "Optional PFCv2 primary hint for icon/color fallback resolution. Does not restrict usage."
--
-- That was already false when it shipped -- repository.categoryMatchClause has always rolled a
-- PFC-primary budget up through this column, so a category nested under FOOD_AND_DRINK already
-- moved money in a budget report -- and #1609 makes it load-bearing a second way:
-- repository.effectiveTransferExpr now resolves a custom-categorised transaction's transfer
-- classification through it, so a category nested under TRANSFER_IN or TRANSFER_OUT makes its
-- transactions transfers and they leave cash flow when include_transfers is false.
--
-- WHY THAT WORDING MATTERED RATHER THAN BEING MERELY UNTIDY. A real user, reading custom categories
-- as "a pure label with no semantics", was advised by an MCP client to assign one to reclassify
-- TRANSFER_IN paychecks as income. That would have APPEARED to work before #1609 -- the reported
-- income would have jumped -- but only because a custom-categorised row escaped the transfer filter
-- by accident of NULL-ness (effective_pfc_primary is NULL for every custom-kind row, migration
-- 000073), not because anything became income. A category named "Internal Transfer To Savings"
-- would have produced the identical jump. The catalogue text was one of the things that made that
-- reading look correct.
--
-- WHY A NEW FILE AND ALSO AN EDIT TO 000065. Correcting an already-applied COMMENT takes TWO edits,
-- the convention 000175 and 000184 follow and TestReissuedColumnCommentsDoNotDrift enforces: the
-- ORIGINAL file (000065) is what a FRESH database gets and has been edited in place in the same
-- commit as this file; THIS file is what every already-migrated database gets, since golang-migrate
-- never re-runs an applied version. The guard compares the two bodies with whitespace COLLAPSED, so
-- re-wrapping across lines is not drift but any change of word or punctuation is -- it is not a byte
-- comparison. Edit both or neither.
--
-- SCOPE: one comment. No column, constraint, index, default, type or datum is touched, and no
-- behaviour changes here -- the behaviour change is in Go (effectiveTransferExpr), and it is that
-- transactions already filed under a custom category nested beneath a TRANSFER_* primary are now
-- EXCLUDED from cash flow where they were previously included. Squawk: reasoned from the operation
-- rather than from a clean report (this repo runs squawk with excluded_rules set, so a clean report
-- would be weak evidence). COMMENT ON COLUMN takes only the ordinary catalogue lock on the
-- relation, rewrites no heap or index, adds no constraint, sets no default and validates nothing,
-- so every rule this repo excludes is INAPPLICABLE here rather than merely silenced.
--
-- Idempotent: COMMENT is inherently repeatable, and the statement is guarded on the column still
-- existing, so this is a clean no-op on a database where 000065 was rolled back.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'custom_categories'
          AND column_name = 'parent_pfc_primary'
    ) THEN
        COMMENT ON COLUMN custom_categories.parent_pfc_primary IS
        'Which PFCv2 primary this custom category nests under, or NULL for a category that nests '
        'under nothing. This is NOT a display hint. The original wording said it was ("Optional '
        'PFCv2 primary hint for icon/color fallback resolution. Does not restrict usage.") and that '
        'was already false when it shipped: budget roll-up counts this category''s transactions '
        'under that primary (repository.categoryMatchClause), and since issue #1609 the analytics '
        'transfer filter classifies them through it (repository.effectiveTransferExpr) -- a category '
        'nested under TRANSFER_IN or TRANSFER_OUT makes its transactions TRANSFERS, so they leave '
        'cash flow whenever include_transfers is false. NULL keeps a category out of both mechanisms '
        'rather than defaulting it into one, which was the behaviour of EVERY custom category before '
        '#1609. CHECK valid_custom_category_parent_pfc constrains the value to the 18 PFCv2 '
        'primaries and the API advertises that same set as an enum; '
        'TestPFCPrimaryEnumsMatchTheCustomCategoryParentCheck keeps the two in step.';
    END IF;
END $$;
