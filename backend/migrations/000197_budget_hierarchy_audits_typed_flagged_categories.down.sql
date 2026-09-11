ALTER TABLE budget_hierarchy_audits
    DROP COLUMN IF EXISTS flagged_duplicate_identity_category_ids;

ALTER TABLE budget_hierarchy_audits
    DROP COLUMN IF EXISTS flagged_escaping_pair_category_ids;
