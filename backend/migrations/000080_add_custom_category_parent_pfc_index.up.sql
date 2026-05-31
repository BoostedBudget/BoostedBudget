-- Issue #1203: index on custom_categories (user_id, parent_pfc_primary) for
-- cross-taxonomy roll-up lookups (custom categories nested under a PFC primary).
-- Created CONCURRENTLY as the sole statement in this file (mirrors 000077).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_custom_categories_parent_pfc
    ON custom_categories (user_id, parent_pfc_primary) WHERE parent_pfc_primary IS NOT NULL;
