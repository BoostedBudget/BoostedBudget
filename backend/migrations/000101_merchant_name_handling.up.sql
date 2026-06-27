-- Issue #957: Merchant name normalization, per-user overrides, and merge/dedupe.
--
-- Extends the existing merchants table with:
--   normalized_name  — deterministic clean name computed by the Go Normalize()
--                      function (strips card-reader prefixes, store codes, etc.)
--   canonical_merchant_id — system-level merge pointer (NULL = self is canonical)
--
-- Adds merchant_name_overrides: per-user rename and per-user merge.
--
-- Display resolution at read time:
--   COALESCE(override.display_name,
--            effective_merchant.normalized_name,
--            t.merchant_name)
-- where effective_merchant follows override.merged_into_merchant_id → canonical.

-- ---------------------------------------------------------------------------
-- 1. Extend merchants table
-- ---------------------------------------------------------------------------
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'merchants' AND column_name = 'normalized_name'
    ) THEN
        ALTER TABLE merchants ADD COLUMN normalized_name TEXT;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'merchants' AND column_name = 'canonical_merchant_id'
    ) THEN
        -- Self-FK: NULL means this merchant is its own canonical.
        ALTER TABLE merchants ADD COLUMN canonical_merchant_id UUID;
    END IF;
END $$;

-- Add FK constraint separately with NOT VALID to avoid a full-table scan lock.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'merchants_canonical_merchant_id_fkey'
    ) THEN
        ALTER TABLE merchants
            ADD CONSTRAINT merchants_canonical_merchant_id_fkey
            FOREIGN KEY (canonical_merchant_id) REFERENCES merchants(id)
            NOT VALID;
    END IF;
END $$;

-- Validate in a separate step (ShareUpdateExclusiveLock, less disruptive).
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint c
        JOIN pg_class r ON c.conrelid = r.oid
        WHERE c.conname = 'merchants_canonical_merchant_id_fkey'
          AND r.relname = 'merchants'
          AND NOT c.convalidated
    ) THEN
        ALTER TABLE merchants VALIDATE CONSTRAINT merchants_canonical_merchant_id_fkey;
    END IF;
END $$;

-- Index for following canonical chains.
CREATE INDEX IF NOT EXISTS idx_merchants_canonical_merchant_id
    ON merchants (canonical_merchant_id)
    WHERE canonical_merchant_id IS NOT NULL;

-- Backfill normalized_name for existing merchants using a SQL-only approximation
-- (strips common prefixes/suffixes; the Go service will re-normalize more accurately
-- on next sync or via a backfill job).
UPDATE merchants
SET normalized_name = initcap(
        regexp_replace(
            regexp_replace(
                regexp_replace(
                    trim(name),
                    '(?i)^\s*(SQ|TST|SP|PP|PAYPAL)\s*\*\s*', '', 'g'
                ),
                '(\s+(#\d+|STORE\s+\d+|\d+))+\s*$', '', 'gi'
            ),
            '\s+', ' ', 'g'
        )
    )
WHERE normalized_name IS NULL;

-- ---------------------------------------------------------------------------
-- 2. Enable pg_trgm for fuzzy duplicate detection
-- ---------------------------------------------------------------------------
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- ---------------------------------------------------------------------------
-- 3. Create merchant_name_overrides table
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS merchant_name_overrides (
    id                     UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                TEXT        NOT NULL,
    merchant_id            UUID        NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
    -- Per-user custom display name (NULL = use system normalized_name).
    display_name           TEXT,
    -- Per-user merge: collapse this merchant into another.
    -- NULL = not merged; non-NULL = all reads follow to the target merchant.
    merged_into_merchant_id UUID       REFERENCES merchants(id) ON DELETE SET NULL,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_merchant_name_overrides_user_merchant UNIQUE (user_id, merchant_id),
    -- At least one of display_name or merged_into_merchant_id must be set.
    CONSTRAINT chk_merchant_override_has_action
        CHECK (display_name IS NOT NULL OR merged_into_merchant_id IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS idx_merchant_name_overrides_user_id
    ON merchant_name_overrides (user_id);

CREATE INDEX IF NOT EXISTS idx_merchant_name_overrides_merchant_id
    ON merchant_name_overrides (merchant_id);

CREATE INDEX IF NOT EXISTS idx_merchant_name_overrides_merged_into
    ON merchant_name_overrides (merged_into_merchant_id)
    WHERE merged_into_merchant_id IS NOT NULL;

-- Trigram index on normalized_name for fast duplicate-suggestion queries.
CREATE INDEX IF NOT EXISTS idx_merchants_normalized_name_trgm
    ON merchants USING gin (normalized_name gin_trgm_ops)
    WHERE normalized_name IS NOT NULL;
