-- Issue #1569 Task 14: the automation audit trail must outlive the definition that produced it.
-- ⚠ SUPERSEDED CLAIM (2026-08-25, #1569 T8). The statement below asserting that
-- `grep -rn 'rule_execution_history' backend --include='*.go'` matches nothing outside migrations
-- were TRUE WHEN WRITTEN and are now FALSE: #1569 T5 shipped
-- backend/internal/repository/rule_execution_history_repository.go, which both writes and reads
-- this table, plus a service and an endpoint. Nothing about the SCHEMA below changes -- this is a
-- comment-only amendment, made here rather than only in the Go file because a reader who opens
-- this migration has no way to discover a correction that lives somewhere else.
--
--
-- Five foreign keys were still ON DELETE CASCADE at the start of this task. Four of them are
-- flipped to SET NULL here; the fifth is deliberately left alone, and the reason is recorded
-- below because "finish the job" is the obvious wrong next move.
--
--   fk_rule_execution_history_rule_id            -> SET NULL   (live user-facing bug)
--   fk_boost_executions_boost_id                 -> SET NULL
--   fk_rollover_history_budget_id                -> SET NULL
--   fk_rollover_history_source_budget_category_id-> SET NULL
--   fk_budget_period_events_budget               -> STAYS CASCADE, see step 4
--
-- WHY rule_execution_history IS THE URGENT ONE. Task 8 (000158) tombstoned savings_goals, boosts,
-- budgets and budget_categories, so those three cascades now fire only on purge and GDPR paths.
-- categorization_rules was NOT one of the four: it has no deleted_at column, and
-- CategorizationRuleRepository.Delete still issues a hard `DELETE FROM categorization_rules
-- WHERE id = $1` (categorization_rule_repository.go:348). Deleting a rule in the UI therefore
-- destroys the record of everything that rule ever did to the user's transactions, today.
--
-- THE GDPR HAZARD THIS MIGRATION CREATES AND CLOSES. Three of the five FKs were their table's
-- ONLY path to users. Erasure is `DELETE FROM users` (profile_repository.go DeleteUserCascade)
-- walking the FK graph:
--
--   rule_execution_history: no user_id column at all. Reached only via
--       rule_id -> categorization_rules(id) CASCADE -> user_id -> users(id) CASCADE.
--   boost_executions:       no user_id column at all. Reached only via
--       boost_id -> boosts(id) CASCADE -> user_id -> users(id) CASCADE.
--   budget_period_events:   user_id is a bare TEXT column with NO foreign key to users;
--       reached only via budget_id -> budgets(id) CASCADE. profile_repository.go's Issue #1440
--       Finding 3 comment excludes it from the explicit FK-less erasure list *because of* that
--       cascade.
--
-- Flipping any of those to SET NULL without a replacement erasure path would strand an erased
-- user's rows forever. Steps 1 and 2 therefore give the two history tables their own
-- user_id -> users(id) ON DELETE CASCADE before their parent FK is loosened. Step 4 explains why
-- budget_period_events gets neither treatment.
--
-- rollover_history already has fk_rollover_history_user_id -> users(id) ON DELETE CASCADE and
-- needs no new column. That CASCADE is untouched here.
--
-- THE READABILITY HAZARD. A retained row that no query can return is retention that reads as
-- deletion -- the trap task 8 hit on savings_goal_contributions. Before this migration EVERY
-- user-facing read of boost_executions was keyed on `boost_id = $1` (GetByBoostID,
-- GetByBoostIDWithFilter, GetLastExecutionForBoost, GetFailedExecutions), so a NULL-boost_id row
-- would have been invisible in every list and count in the app. The user_id column added below is
-- also the key for a new GetByUserID reader. rollover_history was already reachable by
-- GetByUserID/GetByID; only its scan needed the nullable columns.
--
-- Idempotent throughout: ADD COLUMN IF NOT EXISTS, DO $$ guards on every constraint change, and
-- NOT NULL drops that are no-ops once applied.

-- =============================================================================
-- Step 1: rule_execution_history -- own erasure path, snapshot columns, SET NULL.
-- =============================================================================

ALTER TABLE rule_execution_history
    ADD COLUMN IF NOT EXISTS user_id UUID,
    ADD COLUMN IF NOT EXISTS rule_name VARCHAR(100),
    ADD COLUMN IF NOT EXISTS definition_snapshot JSONB;

-- Backfill from the parent while the parent link is still guaranteed present.
UPDATE rule_execution_history h
SET user_id = r.user_id
FROM categorization_rules r
WHERE h.rule_id = r.id AND h.user_id IS NULL;

UPDATE rule_execution_history h
SET rule_name = r.name
FROM categorization_rules r
WHERE h.rule_id = r.id AND h.rule_name IS NULL;

UPDATE rule_execution_history h
SET definition_snapshot = jsonb_strip_nulls(jsonb_build_object(
        'rule_id',                   r.id,
        'name',                      r.name,
        'enabled',                   r.enabled,
        'priority',                  r.priority,
        'condition_logic',           r.condition_logic,
        'conditions',                r.conditions,
        'target_kind',               r.target_kind,
        'target_pfc_primary',        r.target_pfc_primary,
        'target_pfc_detailed',       r.target_pfc_detailed,
        'target_custom_category_id', r.target_custom_category_id,
        'target_tags',               r.target_tags,
        'tag_behavior',              r.tag_behavior,
        'source',                    r.source,
        'captured_at',               to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SSZ'),
        'captured_by',               'migration_000164_backfill'
    ))
FROM categorization_rules r
WHERE h.rule_id = r.id AND h.definition_snapshot IS NULL;

-- Any row whose rule is already gone cannot be backfilled (there is nothing left to read), and a
-- row with no user_id has no erasure path. There is no such row: rule_id is still NOT NULL at this
-- point in the migration, so every row has a live parent. Asserted rather than assumed, because
-- SET NOT NULL below would otherwise fail with a bare constraint error and no explanation.
DO $$
DECLARE orphans BIGINT;
BEGIN
    SELECT COUNT(*) INTO orphans FROM rule_execution_history WHERE user_id IS NULL;
    IF orphans > 0 THEN
        RAISE EXCEPTION 'rule_execution_history has % row(s) with no resolvable user_id; '
            'they would survive GDPR erasure once rule_id becomes nullable', orphans;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_rule_execution_history_user_id'
    ) THEN
        ALTER TABLE rule_execution_history
            ADD CONSTRAINT fk_rule_execution_history_user_id
            FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE NOT VALID;
        ALTER TABLE rule_execution_history
            VALIDATE CONSTRAINT fk_rule_execution_history_user_id;
    END IF;
END $$;

-- The erasure path must be MANDATORY, not best-effort: a NULL here is a row GDPR cannot reach.
-- Enforced with a validated CHECK rather than `SET NOT NULL` -- squawk's adding-not-nullable-field
-- rule (deliberately left enabled in .squawk.toml) recommends exactly this, because SET NOT NULL
-- takes an ACCESS EXCLUSIVE lock and scans the whole table while blocking reads, whereas
-- ADD CONSTRAINT ... NOT VALID takes the lock only momentarily and VALIDATE CONSTRAINT scans under
-- a weaker SHARE UPDATE EXCLUSIVE lock. A validated CHECK (user_id IS NOT NULL) rejects a NULL
-- insert just as absolutely as NOT NULL does; the only difference is that the column still reads
-- as nullable in \d, which is why this comment exists.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'rule_execution_history_user_id_not_null'
    ) THEN
        ALTER TABLE rule_execution_history
            ADD CONSTRAINT rule_execution_history_user_id_not_null
            CHECK (user_id IS NOT NULL) NOT VALID;
        ALTER TABLE rule_execution_history
            VALIDATE CONSTRAINT rule_execution_history_user_id_not_null;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_rule_exec_user_id
    ON rule_execution_history (user_id, executed_at DESC);

-- rule_id must accept NULL before SET NULL can ever fire. squawk's ban-drop-not-null warns that
-- this breaks existing clients; the only Go code touching this table is test seed SQL
-- (`grep -rn "rule_execution_history" backend --include="*.go"` matches migrations, comments and
-- integration-test fixtures only -- there is no repository for it), and the column is being made
-- nullable precisely so the row can outlive its parent. Suppressed for THIS STATEMENT ONLY. The
-- directive must sit on its own line immediately above the statement; a trailing
-- `; -- squawk-ignore ...` on the same line is NOT honoured.
-- squawk-ignore ban-drop-not-null
ALTER TABLE rule_execution_history ALTER COLUMN rule_id DROP NOT NULL;

-- unique_rule_transaction UNIQUE (rule_id, transaction_id) is unaffected: Postgres's default
-- NULLS DISTINCT means orphaned rows never collide with each other.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'fk_rule_execution_history_rule_id' AND confdeltype <> 'n'
    ) THEN
        ALTER TABLE rule_execution_history
            DROP CONSTRAINT fk_rule_execution_history_rule_id;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_rule_execution_history_rule_id'
    ) THEN
        ALTER TABLE rule_execution_history
            ADD CONSTRAINT fk_rule_execution_history_rule_id
            FOREIGN KEY (rule_id) REFERENCES categorization_rules(id) ON DELETE SET NULL NOT VALID;
        ALTER TABLE rule_execution_history
            VALIDATE CONSTRAINT fk_rule_execution_history_rule_id;
    END IF;
END $$;

COMMENT ON COLUMN rule_execution_history.rule_id IS
$COMMENT$The rule that fired, or NULL once that rule has been deleted (ON DELETE SET NULL since
#1569 Task 14). NULL means "the rule is gone", never "no rule". rule_name and definition_snapshot
are what a reader shows in its place.$COMMENT$;

COMMENT ON COLUMN rule_execution_history.user_id IS
$COMMENT$Owner of the rule that fired, denormalised at execution time. This is the row's own
ON DELETE CASCADE path to users -- once rule_id can be NULL it is the ONLY path GDPR erasure has to
this table -- and the key any per-user read of the automation trail uses.$COMMENT$;

COMMENT ON COLUMN rule_execution_history.rule_name IS
$COMMENT$The rule's name AS IT WAS at execution time. Never updated when the rule is renamed: this
column exists to say what the user saw when the automation ran.$COMMENT$;

COMMENT ON COLUMN rule_execution_history.definition_snapshot IS
$COMMENT$The rule definition AS IT WAS at execution time, so an orphaned row can still answer "what
did this automation do, and why" with no parent to join to. Records rule_id, name, enabled,
priority, condition_logic, conditions (why it fired), the target_* fields and target_tags/
tag_behavior (what it did), source, and captured_at/captured_by. Deliberately EXCLUDES: description
(free text, no bearing on behaviour), match_count/override_count/last_matched_at (counters
describing the rule's whole lifetime, not this execution), and created_at/updated_at (properties of
the definition, not of what it did). Written once; never rewritten when the rule changes.$COMMENT$;

-- =============================================================================
-- Step 2: boost_executions -- own erasure path, snapshot columns, SET NULL.
-- =============================================================================

ALTER TABLE boost_executions
    ADD COLUMN IF NOT EXISTS user_id UUID,
    ADD COLUMN IF NOT EXISTS boost_name VARCHAR(100),
    ADD COLUMN IF NOT EXISTS definition_snapshot JSONB;

UPDATE boost_executions e
SET user_id = b.user_id
FROM boosts b
WHERE e.boost_id = b.id AND e.user_id IS NULL;

UPDATE boost_executions e
SET boost_name = b.name
FROM boosts b
WHERE e.boost_id = b.id AND e.boost_name IS NULL;

UPDATE boost_executions e
SET definition_snapshot = jsonb_strip_nulls(jsonb_build_object(
        'boost_id',        b.id,
        'name',            b.name,
        'boost_type',      b.boost_type,
        'enabled',         b.enabled,
        'priority',        b.priority,
        'condition_logic', b.condition_logic,
        'conditions',      b.conditions,
        'actions',         b.actions,
        'trigger_config',  b.trigger_config,
        'captured_at',     to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SSZ'),
        'captured_by',     'migration_000164_backfill'
    ))
FROM boosts b
WHERE e.boost_id = b.id AND e.definition_snapshot IS NULL;

DO $$
DECLARE orphans BIGINT;
BEGIN
    SELECT COUNT(*) INTO orphans FROM boost_executions WHERE user_id IS NULL;
    IF orphans > 0 THEN
        RAISE EXCEPTION 'boost_executions has % row(s) with no resolvable user_id; '
            'they would survive GDPR erasure once boost_id becomes nullable', orphans;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_boost_executions_user_id'
    ) THEN
        ALTER TABLE boost_executions
            ADD CONSTRAINT fk_boost_executions_user_id
            FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE NOT VALID;
        ALTER TABLE boost_executions
            VALIDATE CONSTRAINT fk_boost_executions_user_id;
    END IF;
END $$;

-- Same reasoning as step 1: a validated CHECK enforces the erasure path without the blocking
-- full-table scan that `SET NOT NULL` would take.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'boost_executions_user_id_not_null'
    ) THEN
        ALTER TABLE boost_executions
            ADD CONSTRAINT boost_executions_user_id_not_null
            CHECK (user_id IS NOT NULL) NOT VALID;
        ALTER TABLE boost_executions
            VALIDATE CONSTRAINT boost_executions_user_id_not_null;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_boost_executions_user_id
    ON boost_executions (user_id, started_at DESC);

-- boost_id must accept NULL before SET NULL can fire. Every existing reader either supplies a
-- non-NULL boost_id as a predicate (in which case a NULL row simply does not match, which is the
-- intended behaviour once the boost is gone) or is being taught the nullable type in the same
-- change (BoostExecutionRepository's scans, domain.BoostExecution.BoostID). Suppressed for THIS
-- STATEMENT ONLY.
-- squawk-ignore ban-drop-not-null
ALTER TABLE boost_executions ALTER COLUMN boost_id DROP NOT NULL;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'fk_boost_executions_boost_id' AND confdeltype <> 'n'
    ) THEN
        ALTER TABLE boost_executions DROP CONSTRAINT fk_boost_executions_boost_id;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_boost_executions_boost_id'
    ) THEN
        ALTER TABLE boost_executions
            ADD CONSTRAINT fk_boost_executions_boost_id
            FOREIGN KEY (boost_id) REFERENCES boosts(id) ON DELETE SET NULL NOT VALID;
        ALTER TABLE boost_executions
            VALIDATE CONSTRAINT fk_boost_executions_boost_id;
    END IF;
END $$;

COMMENT ON COLUMN boost_executions.boost_id IS
$COMMENT$The boost that ran, or NULL once that boost has been purged (ON DELETE SET NULL since
#1569 Task 14). NULL means "the boost is gone", never "no boost". Ordinary deletes no longer reach
this FK at all -- task 8 made them tombstones (boosts.deleted_at).$COMMENT$;

COMMENT ON COLUMN boost_executions.user_id IS
$COMMENT$Owner of the boost that ran, denormalised at execution time. Once boost_id can be NULL
this is the ONLY path GDPR erasure has to this table, and the key GetByUserID reads by -- without
it an orphaned execution would be invisible to every list in the app, because all four existing
reads are keyed on boost_id.$COMMENT$;

COMMENT ON COLUMN boost_executions.boost_name IS
$COMMENT$The boost's name AS IT WAS at execution time. Never updated when the boost is renamed.$COMMENT$;

COMMENT ON COLUMN boost_executions.definition_snapshot IS
$COMMENT$The boost definition AS IT WAS at execution time, so an orphaned row can still answer
"what did this automation do, and why" with no parent to join to. Records boost_id, name,
boost_type and actions (what it did), trigger_config (why it ran), conditions/condition_logic and
enabled (why it fired, or why status='completed' with no effect), priority, and
captured_at/captured_by. Deliberately EXCLUDES: description (free text, no bearing on behaviour),
execution_count/last_executed_at/last_successful_at (counters describing the boost's whole
lifetime, not this execution -- and this row is itself one of the things they count), and
created_at/updated_at. Written once, at INSERT; Update() never touches it.$COMMENT$;

-- =============================================================================
-- Step 3: rollover_history -- both remaining CASCADEs to SET NULL.
--
-- No new column is needed: fk_rollover_history_user_id -> users(id) ON DELETE CASCADE already
-- exists and is UNTOUCHED here (GDPR erasure runs through the FK graph off users -- do not
-- loosen any user_id cascade), and GetByUserID/GetByID already read by columns that survive, so
-- an orphan is reachable. Only the scan needed the nullable types.
-- =============================================================================

-- squawk-ignore ban-drop-not-null
ALTER TABLE rollover_history ALTER COLUMN budget_id DROP NOT NULL;

-- squawk-ignore ban-drop-not-null
ALTER TABLE rollover_history ALTER COLUMN source_budget_category_id DROP NOT NULL;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'fk_rollover_history_budget_id' AND confdeltype <> 'n'
    ) THEN
        ALTER TABLE rollover_history DROP CONSTRAINT fk_rollover_history_budget_id;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_rollover_history_budget_id'
    ) THEN
        ALTER TABLE rollover_history
            ADD CONSTRAINT fk_rollover_history_budget_id
            FOREIGN KEY (budget_id) REFERENCES budgets(id) ON DELETE SET NULL NOT VALID;
        ALTER TABLE rollover_history VALIDATE CONSTRAINT fk_rollover_history_budget_id;
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'fk_rollover_history_source_budget_category_id' AND confdeltype <> 'n'
    ) THEN
        ALTER TABLE rollover_history DROP CONSTRAINT fk_rollover_history_source_budget_category_id;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'fk_rollover_history_source_budget_category_id'
    ) THEN
        ALTER TABLE rollover_history
            ADD CONSTRAINT fk_rollover_history_source_budget_category_id
            FOREIGN KEY (source_budget_category_id) REFERENCES budget_categories(id)
            ON DELETE SET NULL NOT VALID;
        ALTER TABLE rollover_history
            VALIDATE CONSTRAINT fk_rollover_history_source_budget_category_id;
    END IF;
END $$;

COMMENT ON COLUMN rollover_history.budget_id IS
$COMMENT$The budget this rollover moved money within, or NULL once that budget has been purged
(ON DELETE SET NULL since #1569 Task 14). NULL means "the budget is gone", never "no budget".
The row still records the amount, the type, the period and the user.$COMMENT$;

COMMENT ON COLUMN rollover_history.source_budget_category_id IS
$COMMENT$The category the money came from, or NULL once that category has been purged (ON DELETE
SET NULL since #1569 Task 14). NULL means "the category is gone", never "no source" -- unlike
target_budget_category_id, where NULL has always meant "to unallocated funds".$COMMENT$;

-- =============================================================================
-- Step 4: budget_period_events -- fk_budget_period_events_budget STAYS ON DELETE CASCADE.
--
-- This is the one FK of the five deliberately NOT flipped, recorded here so a later reader does
-- not "finish the job" and reintroduce two bugs.
--
-- budget_period_events is not history. It is an exactly-once marker table
-- (BudgetPeriodEventsRepository: HasMarker / InsertMarker / DeleteMarker) whose only purpose is to
-- stop the daily period-close scanner enqueueing the same budget period twice. SET NULL would be
-- harmful three ways:
--
--   1. Unreadable. All three repository methods filter on the natural key including
--      `budget_id = $2`, so a NULL-budget_id row is invisible to the entire API of its own
--      repository, and nothing else in the codebase reads the table.
--   2. Meaningless. The marker suppresses re-processing of a budget period. With the budget purged
--      there is no period left to re-process, so a retained marker records nothing a user could
--      be shown -- retention with no reader and no reason.
--   3. A GDPR leak. user_id here is a bare TEXT column with NO foreign key to users; budget_id
--      CASCADE is the table's only path to erasure, which profile_repository.go's Issue #1440
--      Finding 3 comment relies on explicitly.
--
-- Pinned by TestAuditTrail_BudgetPeriodEventsStayCascaded_Integration.
-- =============================================================================

COMMENT ON CONSTRAINT fk_budget_period_events_budget ON budget_period_events IS
$COMMENT$Deliberately ON DELETE CASCADE, unlike the four automation-history FKs flipped to SET NULL
by #1569 Task 14. This table holds exactly-once scanner markers, not history: every read is keyed
on budget_id so an orphan would be unreadable, a marker for a purged budget suppresses nothing, and
this FK is the table's only path to GDPR erasure (user_id is bare TEXT with no FK to users).$COMMENT$;
