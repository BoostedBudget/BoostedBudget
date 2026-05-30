DROP INDEX IF EXISTS unique_budget_category_discriminated;
ALTER TABLE budget_categories
    ADD CONSTRAINT unique_budget_category_discriminated
    UNIQUE (budget_id, category_kind, pfc_primary, pfc_detailed, custom_category_id);
