-- Issue #1203: index on custom_categories.parent_id (hierarchy lookups).
-- Created CONCURRENTLY as the sole statement in this file (mirrors 000077) so it
-- is not run inside a transaction.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_custom_categories_parent_id
    ON custom_categories (parent_id) WHERE parent_id IS NOT NULL;
