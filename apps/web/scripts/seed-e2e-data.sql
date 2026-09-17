-- E2E Test Data Seed Script
-- This script populates the database with test data for E2E tests
-- Run after migrations during E2E test setup
--
-- Usage: psql $DATABASE_URL -f seed-e2e-data.sql
--
-- Note: The Clerk user ID will be set dynamically by the run script
-- using the E2E_CLERK_USER_ID environment variable

-- =============================================================================
-- Configuration: Set the test user's Clerk ID
-- This is replaced by the run script with the actual Clerk user ID
-- =============================================================================
\set clerk_user_id '''__E2E_CLERK_USER_ID__'''
\set test_email '''e2e+clerk_test@boostedbudget.com'''

-- =============================================================================
-- Step 1: Create test user, identity, and privacy settings in a single DO block
-- Uses email for conflict resolution since clerk_user_id no longer has unique constraint
-- =============================================================================
DO $$
DECLARE
    v_user_id UUID;
    v_clerk_user_id VARCHAR := '__E2E_CLERK_USER_ID__';
    v_test_email VARCHAR := 'e2e+clerk_test@boostedbudget.com';
BEGIN
    -- Delete any existing E2E test user to ensure clean slate
    -- This handles the case where a previous run created the user
    DELETE FROM user_identities WHERE clerk_user_id = v_clerk_user_id;
    DELETE FROM privacy_settings WHERE user_id IN (SELECT id FROM users WHERE email = v_test_email);
    DELETE FROM users WHERE email = v_test_email;

    -- Create new user with premium subscription
    INSERT INTO users (
        clerk_user_id,
        email,
        email_verified,
        primary_region,
        subscription_tier,
        subscription_status,
        is_active,
        is_deleted
    ) VALUES (
        v_clerk_user_id,
        v_test_email,
        true,
        'iad',  -- Fly.io IATA code for us-east-1
        'premium',  -- Premium tier for full E2E testing
        'active',
        true,
        false
    )
    RETURNING id INTO v_user_id;

    IF v_user_id IS NULL THEN
        RAISE EXCEPTION 'Failed to create test user';
    END IF;

    RAISE NOTICE 'Created E2E test user with ID: %', v_user_id;

    -- Insert into user_identities (CRITICAL - required for Clerk middleware to find the user)
    -- Without this, the middleware won't find the user and will auto-register a NEW user
    INSERT INTO user_identities (
        user_id,
        clerk_user_id,
        provider,
        provider_user_id,
        email,
        email_verified,
        is_primary
    ) VALUES (
        v_user_id,
        v_clerk_user_id,
        'clerk',
        v_clerk_user_id,
        v_test_email,
        true,
        true
    );

    RAISE NOTICE 'Created user identity for Clerk auth';

    -- Insert into privacy_settings (required for registration consistency)
    INSERT INTO privacy_settings (
        user_id,
        gdpr_necessary_consent,
        gdpr_analytics_consent,
        gdpr_advertising_consent
    ) VALUES (
        v_user_id,
        true,
        false,
        false
    );

    RAISE NOTICE 'Created privacy settings';
    RAISE NOTICE 'E2E test user setup complete: % (subscription_status=active, subscription_tier=premium)', v_user_id;
END $$;

-- Store user ID for subsequent inserts
DO $$
DECLARE
    v_user_id UUID;
    v_linked_item_id UUID;
    v_checking_id UUID;
    v_savings_id UUID;
    v_credit_id UUID;
    v_budget_id UUID;
    v_series_id UUID := gen_random_uuid();
    -- Issue #1370 Task 12: stable UserAccount ids backing accounts.user_account_id.
    -- Populated below so household-sharing (AccountCustomizationModal's "Share with
    -- household" switch requires a non-null account.user_account_id — see
    -- MatchOrCreate's (user_id, institution_id, mask) matching in
    -- user_account_service.go) works against these seeded accounts, exactly as it
    -- would for a real Plaid-linked account.
    v_checking_ua_id UUID;
    v_savings_ua_id UUID;
    v_credit_ua_id UUID;
    v_institution_id VARCHAR := 'ins_e2e_test'; -- Must match linked_items.institution_id above
BEGIN
    -- Get the user ID
    SELECT id INTO v_user_id FROM users WHERE clerk_user_id = '__E2E_CLERK_USER_ID__';

    IF v_user_id IS NULL THEN
        RAISE EXCEPTION 'Test user not found. Ensure clerk_user_id is set correctly.';
    END IF;

    -- =============================================================================
    -- Step 2: Create linked item (fake Plaid connection)
    -- =============================================================================
    INSERT INTO linked_items (
        id,
        user_id,
        item_id,
        access_token_encrypted,
        institution_id,
        institution_name,
        cursor,
        last_synced_at,
        first_transaction_sync_completed_at,
        transaction_sync_state
    ) VALUES (
        gen_random_uuid(),
        v_user_id,
        'e2e-test-item-001',
        'encrypted-fake-token-for-e2e-tests',  -- Not a real token
        'ins_e2e_test',
        'E2E Test Bank',
        NULL,
        NOW(),
        NOW(),
        'completed'
    )
    ON CONFLICT (user_id, item_id) DO UPDATE SET
        institution_name = EXCLUDED.institution_name,
        last_synced_at = NOW(),
        first_transaction_sync_completed_at = COALESCE(
            linked_items.first_transaction_sync_completed_at,
            EXCLUDED.first_transaction_sync_completed_at
        ),
        transaction_sync_state = 'completed'
    RETURNING id INTO v_linked_item_id;

    -- =============================================================================
    -- Step 3: Create test accounts
    -- =============================================================================

    -- Checking Account
    INSERT INTO accounts (
        id,
        linked_item_id,
        user_id,
        plaid_account_id,
        name,
        official_name,
        type,
        subtype,
        mask,
        current_balance,
        available_balance,
        currency_code
    ) VALUES (
        gen_random_uuid(),
        v_linked_item_id,
        v_user_id,
        'e2e-checking-001',
        'E2E Checking',
        'E2E Test Checking Account',
        'depository',
        'checking',
        '1234',
        5000.00,
        4850.00,
        'USD'
    )
    ON CONFLICT (user_id, plaid_account_id) DO UPDATE SET
        current_balance = EXCLUDED.current_balance,
        available_balance = EXCLUDED.available_balance
    RETURNING id INTO v_checking_id;

    -- Savings Account
    INSERT INTO accounts (
        id,
        linked_item_id,
        user_id,
        plaid_account_id,
        name,
        official_name,
        type,
        subtype,
        mask,
        current_balance,
        available_balance,
        currency_code
    ) VALUES (
        gen_random_uuid(),
        v_linked_item_id,
        v_user_id,
        'e2e-savings-001',
        'E2E Savings',
        'E2E Test Savings Account',
        'depository',
        'savings',
        '5678',
        10000.00,
        10000.00,
        'USD'
    )
    ON CONFLICT (user_id, plaid_account_id) DO UPDATE SET
        current_balance = EXCLUDED.current_balance,
        available_balance = EXCLUDED.available_balance
    RETURNING id INTO v_savings_id;

    -- Credit Card Account
    INSERT INTO accounts (
        id,
        linked_item_id,
        user_id,
        plaid_account_id,
        name,
        official_name,
        type,
        subtype,
        mask,
        current_balance,
        available_balance,
        currency_code
    ) VALUES (
        gen_random_uuid(),
        v_linked_item_id,
        v_user_id,
        'e2e-credit-001',
        'E2E Credit Card',
        'E2E Test Credit Card',
        'credit',
        'credit card',
        '9012',
        -1500.00,  -- Credit cards show negative for amount owed
        3500.00,   -- Available credit
        'USD'
    )
    ON CONFLICT (user_id, plaid_account_id) DO UPDATE SET
        current_balance = EXCLUDED.current_balance,
        available_balance = EXCLUDED.available_balance
    RETURNING id INTO v_credit_id;

    -- =============================================================================
    -- Step 3b: Create stable UserAccount rows + link accounts.user_account_id
    -- Issue #1370 Task 12: a real Plaid Link exchange always creates a user_accounts
    -- row via UserAccountService.MatchOrCreate (matched on user_id + institution_id +
    -- mask) and stamps accounts.user_account_id with its id (migration 000033/000037).
    -- This seed script inserts directly into `accounts` and previously skipped that
    -- step entirely, leaving accounts.user_account_id NULL. Several features key off
    -- that column being non-null for these accounts — most relevantly here,
    -- AccountCustomizationModal's "Share with household" switch
    -- (canShowShareSwitch = isInHousehold && !!account.user_account_id) never renders
    -- without it, which would silently break any E2E test that shares a seeded
    -- account with a household. Mirrors MatchOrCreate's own matching/conflict key.
    --
    -- institution_id/mask are stored PLAINTEXT here (not "ENC$"-prefixed), matching how
    -- MatchOrCreate would store them pre-encryption-migration (#964). This is safe:
    -- FieldEncryptor.DecryptString() treats any value without the "ENC$" prefix as a
    -- plaintext passthrough (dual-read for the encryption migration window), so no
    -- decrypt error occurs when these rows are read back.
    --
    -- institution_id_blind_idx, however, MUST be non-NULL: ListActiveUserAccountFingerprints
    -- (household_share_repository.go) scans it into a non-pointer Go `string`, which errors
    -- on SQL NULL — and this exact query backs SetAccountShareVisibility, the endpoint the
    -- "Share with household" switch calls. A real blind index is HMAC-SHA256 keyed by a
    -- runtime secret unavailable to this seed script, but nothing in this E2E flow reads
    -- institution_id_blind_idx for an equality lookup (no re-link, no joint-account
    -- fingerprint dedup is exercised here) — it only needs to be non-null, not
    -- cryptographically real.
    --
    -- mask_blind_idx is scanned into a nullable `*string`, so it could stay NULL from a
    -- read standpoint — BUT migration 000100 defines
    -- unique_user_accounts_inst_mask_blind as UNIQUE (user_id, institution_id_blind_idx,
    -- mask_blind_idx) NULLS NOT DISTINCT WHERE institution_id_blind_idx IS NOT NULL. With
    -- NULLS NOT DISTINCT, Postgres treats NULL as an ordinary, colliding value in this
    -- index (not "distinct from everything" as usual) — so three rows sharing the same
    -- user_id + institution_id_blind_idx + a NULL mask_blind_idx would violate this unique
    -- index on the second/third INSERT and roll back this entire DO block (all of Step 2
    -- through Step 4, not just these rows). Each row below therefore gets its own
    -- placeholder mask_blind_idx = md5(mask), matching the same "non-null but not
    -- cryptographically real" reasoning as institution_id_blind_idx above. Refreshed on
    -- every ON CONFLICT (not just updated_at) so a database that already had a NULL-blind-idx
    -- row from before this fix self-heals on the next seed run. Any additional accounts
    -- seeded at this same institution (ins_e2e_test) MUST also get a distinct
    -- mask_blind_idx to avoid re-triggering this exact collision.
    --
    -- #1569 round 11 task 4 (coordinator round 2 fix): the ON CONFLICT arbiter below was
    -- (user_id, institution_id, mask) — migration 000031's unique_user_account_identity, a
    -- PLAIN constraint on the (here plaintext) institution_id/mask columns. That constraint was
    -- DROPPED by migration 000178: it predates encryption/blind-indexes, was already documented
    -- (000100) as practically inert for real (independently encrypted) inserts, and directly
    -- blocked migration 000178's own masked-collision split (a migration-minted sibling row
    -- copies the surviving row's ciphertext verbatim, so it is BYTE-IDENTICAL institution_id/mask
    -- for a masked pair — see 000178's Step 1 comment). The arbiter is now the surviving
    -- narrowed blind-index constraint, unique_user_accounts_inst_mask_identity_blind
    -- (user_id, institution_id_blind_idx, mask_blind_idx, identity_type, identity_subtype),
    -- repeating its partial-index WHERE predicate as Postgres requires for arbiter inference
    -- (SQLSTATE 42P10 otherwise). This script never sets identity_type/identity_subtype, so
    -- they stay NULL on every row here — harmless for THIS script's own idempotent re-run
    -- (each of the three rows below already has a distinct, non-NULL mask_blind_idx, so they
    -- never collide with each other regardless of identity), but see the same "distinct
    -- mask_blind_idx" rule above before adding a fourth account at this institution.
    -- =============================================================================
    INSERT INTO user_accounts (user_id, institution_id, mask, account_name_hint, primary_type, primary_subtype, institution_id_blind_idx, mask_blind_idx)
    VALUES (v_user_id, v_institution_id, '1234', 'E2E Checking', 'depository', 'checking', md5(v_institution_id), md5('1234'))
    ON CONFLICT (user_id, institution_id_blind_idx, mask_blind_idx, identity_type, identity_subtype)
        WHERE institution_id_blind_idx IS NOT NULL
        DO UPDATE SET updated_at = NOW(), institution_id_blind_idx = EXCLUDED.institution_id_blind_idx, mask_blind_idx = EXCLUDED.mask_blind_idx
    RETURNING id INTO v_checking_ua_id;

    INSERT INTO user_accounts (user_id, institution_id, mask, account_name_hint, primary_type, primary_subtype, institution_id_blind_idx, mask_blind_idx)
    VALUES (v_user_id, v_institution_id, '5678', 'E2E Savings', 'depository', 'savings', md5(v_institution_id), md5('5678'))
    ON CONFLICT (user_id, institution_id_blind_idx, mask_blind_idx, identity_type, identity_subtype)
        WHERE institution_id_blind_idx IS NOT NULL
        DO UPDATE SET updated_at = NOW(), institution_id_blind_idx = EXCLUDED.institution_id_blind_idx, mask_blind_idx = EXCLUDED.mask_blind_idx
    RETURNING id INTO v_savings_ua_id;

    INSERT INTO user_accounts (user_id, institution_id, mask, account_name_hint, primary_type, primary_subtype, institution_id_blind_idx, mask_blind_idx)
    VALUES (v_user_id, v_institution_id, '9012', 'E2E Credit Card', 'credit', 'credit card', md5(v_institution_id), md5('9012'))
    ON CONFLICT (user_id, institution_id_blind_idx, mask_blind_idx, identity_type, identity_subtype)
        WHERE institution_id_blind_idx IS NOT NULL
        DO UPDATE SET updated_at = NOW(), institution_id_blind_idx = EXCLUDED.institution_id_blind_idx, mask_blind_idx = EXCLUDED.mask_blind_idx
    RETURNING id INTO v_credit_ua_id;

    UPDATE accounts SET user_account_id = v_checking_ua_id WHERE id = v_checking_id;
    UPDATE accounts SET user_account_id = v_savings_ua_id WHERE id = v_savings_id;
    UPDATE accounts SET user_account_id = v_credit_ua_id WHERE id = v_credit_id;

    -- =============================================================================
    -- Step 3c: Create today's balance snapshots
    -- Issue #1370 Task 12: GetBalanceSummary (personal AND household scope) reads
    -- exclusively from balance_snapshots (BalanceSnapshotRepository.GetLatestByUserID /
    -- GetSnapshotsAsOfForHousehold) — never accounts.current_balance directly. Real
    -- accounts get a snapshot row on every Plaid webhook sync
    -- (transaction_sync_service.go's createBalanceSnapshots); this seed script's
    -- directly-inserted accounts never go through that path, so without this step no
    -- dashboard balance widget (personal or household scope) would ever show these
    -- accounts, regardless of whether household sharing itself is working correctly.
    -- Snapshots are keyed by user_account_id (required by the household-scope query's
    -- household_accounts CTE join), not just account_id.
    -- No unique constraint remains on (account_id, snapshot_at) since #364's migration
    -- to timestamp-based snapshots (multiple snapshots/day are valid) — delete+insert
    -- keeps this step idempotent across repeated seed runs instead of accumulating a
    -- new row per run.
    -- =============================================================================
    DELETE FROM balance_snapshots
      WHERE account_id IN (v_checking_id, v_savings_id, v_credit_id) AND source = 'e2e_seed';

    INSERT INTO balance_snapshots (account_id, user_id, user_account_id, current_balance, available_balance, currency_code, snapshot_at, source)
    VALUES
        (v_checking_id, v_user_id, v_checking_ua_id, 5000.00, 4850.00, 'USD', NOW(), 'e2e_seed'),
        (v_savings_id, v_user_id, v_savings_ua_id, 10000.00, 10000.00, 'USD', NOW(), 'e2e_seed'),
        (v_credit_id, v_user_id, v_credit_ua_id, -1500.00, 3500.00, 'USD', NOW(), 'e2e_seed');

    -- =============================================================================
    -- Step 4: Create test transactions (last 30 days)
    -- =============================================================================

    -- Clear existing test transactions for this user
    DELETE FROM transactions WHERE user_id = v_user_id AND source = 'plaid' AND external_transaction_id LIKE 'e2e-%';

    -- Insert diverse transactions for testing (PFC v2 canonical, #1073)
    INSERT INTO transactions (
        user_id, account_id, linked_item_id, source, external_transaction_id,
        amount, date, name, merchant_name, pfc_primary, pfc_detailed, payment_channel, pending,
        tags, category_source
    ) VALUES
    -- Groceries
    (v_user_id, v_checking_id, v_linked_item_id, 'plaid', 'e2e-txn-001',
     125.50, CURRENT_DATE - INTERVAL '1 day', 'Whole Foods Market', 'Whole Foods',
     'GENERAL_MERCHANDISE', 'GENERAL_MERCHANDISE_GROCERIES', 'in store', false,
     '["groceries", "essentials"]'::jsonb, 'plaid'),

    -- Restaurants
    --
    -- ⚠ This row's date is CLAMPED into the current calendar month, and that is load-bearing —
    -- do NOT "simplify" it back to a bare CURRENT_DATE - INTERVAL '2 days'.
    --
    -- cache-freshness.auth.spec.ts moves THIS transaction's category and then asserts that the
    -- dashboard's Cash Flow Summary widget's Expenses total drops by exactly $45. That widget
    -- has no way to choose its own window: DashboardContext defaults `timeframe` to 'this_month'
    -- (startOfMonth..endOfMonth, src/shared/dates/presets.ts) and the preset lives in plain
    -- useState with no URL or localStorage persistence, so the spec's `page.reload()` always
    -- lands back on the current month. A transaction dated 2 days ago falls in the PREVIOUS
    -- month on the 1st and 2nd of every month, putting it outside the only window the widget
    -- will ever request — the move then changes September's total by $0 and the spec fails with
    -- "the dashboard is serving stale data", blaming the cache for a calendar artifact. That is
    -- exactly what happened on 2026-09-02.
    --
    -- GREATEST() keeps the intended "2 days ago" recency on every other day of the month and
    -- only pulls the date forward to the 1st when 2 days ago would cross the boundary. Verified
    -- against real PostgreSQL 17.11 over all 365 days of 2026: 0 future-dated results, 0 landing
    -- in the wrong month, and the clamp engaging on exactly 24 days -- the 1st and 2nd of each
    -- month. The ::date result type was confirmed there too, not inferred.
    --
    -- Early in the month this can collide with the '1 day' rows above; no spec asserts an
    -- ordering over seeded transaction dates (url-sort.auth.spec.ts runs against
    -- mockTransactionPage, not this fixture), and cache-freshness finds this row by amount, not
    -- by date or position.
    --
    -- UTC is explicit rather than inherited. Three clocks have to agree for the assertion to mean
    -- anything, and only two of them are UTC by default: the Playwright browser is PINNED to UTC
    -- (playwright.config.ts `timezoneId: 'UTC'`) and computes the widget's window with
    -- startOfMonth(new Date()); this container happens to run UTC, but a bare CURRENT_DATE would
    -- silently follow a non-UTC session timezone and could disagree with the browser by a day at
    -- the boundary. Naming UTC here removes that dependency instead of relying on a default.
    (v_user_id, v_checking_id, v_linked_item_id, 'plaid', 'e2e-txn-002',
     45.00, GREATEST(
       (CURRENT_TIMESTAMP AT TIME ZONE 'UTC')::date - INTERVAL '2 days',
       date_trunc('month', (CURRENT_TIMESTAMP AT TIME ZONE 'UTC')::date)
     )::date,
     'Chipotle Mexican Grill', 'Chipotle',
     'FOOD_AND_DRINK', 'FOOD_AND_DRINK_RESTAURANTS', 'in store', false,
     '["dining"]'::jsonb, 'plaid'),

    -- Online Shopping
    (v_user_id, v_credit_id, v_linked_item_id, 'plaid', 'e2e-txn-003',
     89.99, CURRENT_DATE - INTERVAL '3 days', 'Amazon.com', 'Amazon',
     'GENERAL_MERCHANDISE', 'GENERAL_MERCHANDISE_ONLINE_MARKETPLACES', 'online', false,
     '["shopping"]'::jsonb, 'plaid'),

    -- Gas
    (v_user_id, v_checking_id, v_linked_item_id, 'plaid', 'e2e-txn-004',
     55.00, CURRENT_DATE - INTERVAL '4 days', 'Shell Gas Station', 'Shell',
     'TRANSPORTATION', 'TRANSPORTATION_GAS', 'in store', false,
     '["transportation"]'::jsonb, 'plaid'),

    -- Paycheck (income)
    (v_user_id, v_checking_id, v_linked_item_id, 'plaid', 'e2e-txn-005',
     -3500.00, CURRENT_DATE - INTERVAL '7 days', 'ACME Corp Payroll', 'ACME Corp',
     'INCOME', 'INCOME_WAGES', 'other', false,
     '["income", "salary"]'::jsonb, 'plaid'),

    -- Utilities
    (v_user_id, v_checking_id, v_linked_item_id, 'plaid', 'e2e-txn-006',
     120.00, CURRENT_DATE - INTERVAL '10 days', 'Electric Company', 'Electric Co',
     'RENT_AND_UTILITIES', 'RENT_AND_UTILITIES_GAS_AND_ELECTRICITY', 'online', false,
     '["bills", "utilities"]'::jsonb, 'plaid'),

    -- Subscription
    (v_user_id, v_credit_id, v_linked_item_id, 'plaid', 'e2e-txn-007',
     15.99, CURRENT_DATE - INTERVAL '12 days', 'Netflix', 'Netflix',
     'ENTERTAINMENT', 'ENTERTAINMENT_TV_AND_MOVIES', 'online', false,
     '["entertainment", "subscriptions"]'::jsonb, 'plaid'),

    -- Coffee Shop
    (v_user_id, v_checking_id, v_linked_item_id, 'plaid', 'e2e-txn-008',
     6.50, CURRENT_DATE - INTERVAL '1 day', 'Starbucks', 'Starbucks',
     'FOOD_AND_DRINK', 'FOOD_AND_DRINK_COFFEE', 'in store', false,
     '["coffee"]'::jsonb, 'plaid'),

    -- Transfer to Savings
    (v_user_id, v_checking_id, v_linked_item_id, 'plaid', 'e2e-txn-009',
     500.00, CURRENT_DATE - INTERVAL '15 days', 'Transfer to Savings', NULL,
     'TRANSFER_OUT', 'TRANSFER_OUT_SAVINGS', 'other', false,
     '["savings"]'::jsonb, 'plaid'),

    -- Credit Card Payment
    (v_user_id, v_checking_id, v_linked_item_id, 'plaid', 'e2e-txn-010',
     250.00, CURRENT_DATE - INTERVAL '5 days', 'Credit Card Payment', NULL,
     'LOAN_PAYMENTS', 'LOAN_PAYMENTS_CREDIT_CARD_PAYMENT', 'other', false,
     '["bills"]'::jsonb, 'plaid'),

    -- Pending Transaction
    (v_user_id, v_checking_id, v_linked_item_id, 'plaid', 'e2e-txn-011',
     35.00, CURRENT_DATE, 'Uber Eats', 'Uber Eats',
     'FOOD_AND_DRINK', 'FOOD_AND_DRINK_RESTAURANTS', 'online', true,
     '["dining"]'::jsonb, 'plaid'),

    -- Healthcare
    (v_user_id, v_credit_id, v_linked_item_id, 'plaid', 'e2e-txn-012',
     75.00, CURRENT_DATE - INTERVAL '20 days', 'CVS Pharmacy', 'CVS',
     'MEDICAL', 'MEDICAL_PHARMACIES_AND_SUPPLEMENTS', 'in store', false,
     '["health"]'::jsonb, 'plaid');

    -- #1531 Task 11e finding: effective_category_name (migration 000073, Issue #1123) is an
    -- APP-MAINTAINED column, not a generated one (unlike effective_pfc_primary/
    -- effective_pfc_detailed, which ARE `GENERATED ALWAYS AS ... STORED` and so are populated
    -- automatically on INSERT regardless of how the row was written). Real application writes
    -- (TransactionRepository.UpdateCategory, the Plaid sync path) always set it explicitly; this
    -- raw SQL INSERT above never has, leaving it NULL on every seeded transaction. Any consumer
    -- that reads it — GetSpendingByCategory, GetSpendingTrends (see
    -- backend/internal/repository/analytics_repository.go's effectiveCategoryDisplayExpr) — scans
    -- it into a non-nullable *string and 500s with "cannot scan NULL into *string" the first time
    -- anything queries real (non-mocked) analytics against this seed data. Backfill it here with
    -- the EXACT same CASE expression migration 000073's own one-time backfill uses, scoped to
    -- this user's rows so re-running this script against already-seeded data stays idempotent.
    UPDATE transactions
    SET effective_category_name = CASE
        WHEN category_override_kind = 'custom' THEN
            (SELECT name FROM custom_categories WHERE id = custom_category_override_id AND user_id = transactions.user_id)
        WHEN category_override_kind = 'pfc' THEN pfc_primary_override
        ELSE pfc_primary
    END
    WHERE user_id = v_user_id;

    -- =============================================================================
    -- Step 5: Create user tags
    -- =============================================================================
    INSERT INTO user_tags (user_id, name, color, usage_count)
    VALUES
        (v_user_id, 'groceries', '#4CAF50', 1),
        (v_user_id, 'essentials', '#2196F3', 1),
        (v_user_id, 'dining', '#FF9800', 2),
        (v_user_id, 'shopping', '#9C27B0', 1),
        (v_user_id, 'transportation', '#795548', 1),
        (v_user_id, 'income', '#4CAF50', 1),
        (v_user_id, 'salary', '#8BC34A', 1),
        (v_user_id, 'bills', '#F44336', 2),
        (v_user_id, 'utilities', '#FF5722', 1),
        (v_user_id, 'entertainment', '#E91E63', 1),
        (v_user_id, 'subscriptions', '#673AB7', 1),
        (v_user_id, 'coffee', '#795548', 1),
        (v_user_id, 'savings', '#009688', 1),
        (v_user_id, 'health', '#00BCD4', 1)
    ON CONFLICT (user_id, name) DO UPDATE SET
        usage_count = user_tags.usage_count + 1;

    -- =============================================================================
    -- Step 6: Create custom categories
    -- =============================================================================
    INSERT INTO custom_categories (user_id, name, parent_pfc_primary, icon, color, usage_count)
    VALUES
        (v_user_id, 'Work Expenses', 'GENERAL_SERVICES', 'briefcase', '#607D8B', 0),
        (v_user_id, 'Side Hustle', 'INCOME', 'trending-up', '#00BCD4', 0),
        (v_user_id, 'Pet Supplies', 'GENERAL_MERCHANDISE', 'heart', '#E91E63', 0)
    -- #1569 Task 20 (A1): the arbiter MUST carry `WHERE deleted_at IS NULL`.
    -- Migration 000159 (this issue's task 9) dropped the UNIQUE CONSTRAINT
    -- `unique_user_category_name` and recreated it as a PARTIAL unique index of the same name.
    -- Postgres will not infer a partial index from a bare `ON CONFLICT (user_id, name)`; it
    -- raises `there is no unique or exclusion constraint matching the ON CONFLICT specification`,
    -- which aborts this whole DO block and rolls back all thirteen seeded inserts. The index
    -- predicate must be restated here verbatim for inference to succeed.
    ON CONFLICT (user_id, name) WHERE deleted_at IS NULL DO NOTHING;

    -- =============================================================================
    -- Step 7: Create E2E fixture budget + categories (description='e2e-fixture-v1' marker)
    -- =============================================================================
    INSERT INTO budgets (
        id, user_id, name, description, period, start_date, amount, currency_code,
        income_amount, income_sources, expected_income, is_active, series_id
    ) VALUES (
        v_series_id,
        v_user_id,
        'E2E Monthly Budget',
        'e2e-fixture-v1',
        'monthly',
        DATE_TRUNC('month', CURRENT_DATE)::DATE,
        5000.00,
        'USD',
        0,
        '[]'::jsonb,
        7000.00,
        true,
        v_series_id
    )
    -- #1569 Task 20 (A1): partial-index arbiter, same reason as custom_categories above --
    -- migration 000158 turned `unique_budget_name_per_user` into a partial unique index.
    ON CONFLICT (user_id, name) WHERE deleted_at IS NULL DO UPDATE SET
        description = EXCLUDED.description,
        amount = EXCLUDED.amount,
        expected_income = EXCLUDED.expected_income,
        is_active = true,
        updated_at = NOW()
    RETURNING id INTO v_budget_id;

    -- Remove stale categories before re-inserting (UNIQUE constraint has NULLs so ON CONFLICT
    -- is unreliable for pfc rows; idempotent delete+insert is safer)
    DELETE FROM budget_categories WHERE budget_id = v_budget_id;

    INSERT INTO budget_categories (budget_id, category_kind, pfc_primary, allocated_amount, tags, match_logic)
    VALUES
        (v_budget_id, 'pfc', 'FOOD_AND_DRINK',       500.00, '[]'::jsonb, 'category_only'),
        (v_budget_id, 'pfc', 'GENERAL_MERCHANDISE',  300.00, '[]'::jsonb, 'category_only'),
        (v_budget_id, 'pfc', 'TRANSPORTATION',        200.00, '[]'::jsonb, 'category_only');

    RAISE NOTICE 'E2E fixture budget created: % (id=%)', 'E2E Monthly Budget', v_budget_id;

    RAISE NOTICE 'E2E test data seeded successfully for user %', v_user_id;
END $$;

-- =============================================================================
-- Step 5: Investment fixtures — Issue #1631 (Portfolio tab 500 + "Cash" mislabelling)
--
-- Before this step, `securities` / `holdings` / `investment_transactions` were never seeded, so
-- the Portfolio tab (/insights?tab=portfolio) had nothing to render and
-- portfolio-and-investment-activity.auth.spec.ts could not exist. The shape below is the
-- PRODUCTION shape, not a softened stand-in:
--
--   - `close_price_as_of` is NON-NULL on the real security. This is not decoration: pgx's scan
--     plan for this DATE column only fails for a REAL value (a NULL is rescued), which is exactly
--     the 500 this branch fixes and exactly why every pre-existing fixture (always NULL) missed
--     it. See holding_repository.go's GetByUserIDWithSecurities and
--     holding_close_price_as_of_integration_test.go.
--   - The "reinvestment" pair below is a `cash`/`deposit` row and a `cash`/`withdrawal` row against
--     a REAL (non-cash-equivalent) security — the shape this brokerage actually reports a
--     distribution as — NOT a `subtype = 'dividend reinvestment'` row. Seeding the literal subtype
--     would exercise an already-working branch and prove nothing about this branch's fix (see
--     domain.NormalizeTradeType's structural-inference branch in investment_trade_type.go).
--     Plaid's sign convention is inverted vs. the Transactions product: inflow is NEGATIVE.
--   - A third row shares the exact same raw shape (`cash`/`withdrawal`) but is booked against the
--     CASH-EQUIVALENT security, so it must still display as Cash — proving the fix discriminates
--     on the joined security rather than relabelling every cash/withdrawal row.
-- =============================================================================
DO $$
DECLARE
    v_user_id UUID;
    v_linked_item_id UUID;
    v_investment_account_id UUID;
    v_real_security_id UUID;
    v_cash_security_id UUID;
BEGIN
    SELECT id INTO v_user_id FROM users WHERE clerk_user_id = '__E2E_CLERK_USER_ID__';
    IF v_user_id IS NULL THEN
        RAISE EXCEPTION 'Test user not found. Ensure clerk_user_id is set correctly.';
    END IF;

    SELECT id INTO v_linked_item_id FROM linked_items
        WHERE user_id = v_user_id AND item_id = 'e2e-test-item-001';
    IF v_linked_item_id IS NULL THEN
        RAISE EXCEPTION 'E2E linked item not found -- investment fixtures require Step 2 to have run first.';
    END IF;

    -- Investment (brokerage) account. Gated on a.type = 'investment' by
    -- holding_repository.go's GetByUserIDWithSecurities.
    INSERT INTO accounts (
        id, linked_item_id, user_id, plaid_account_id, name, official_name,
        type, subtype, mask, current_balance, available_balance, currency_code
    ) VALUES (
        gen_random_uuid(), v_linked_item_id, v_user_id, 'e2e-investment-001',
        'E2E Brokerage', 'E2E Test Brokerage Account', 'investment', 'brokerage', '4321',
        1005.00, 1005.00, 'USD'
    )
    ON CONFLICT (user_id, plaid_account_id) DO UPDATE SET
        current_balance = EXCLUDED.current_balance,
        available_balance = EXCLUDED.available_balance
    RETURNING id INTO v_investment_account_id;

    -- A REAL, non-cash-equivalent security with a non-NULL close_price_as_of (see step header).
    INSERT INTO securities (
        id, plaid_security_id, ticker_symbol, name, type, is_cash_equivalent,
        close_price, close_price_as_of, currency_code
    ) VALUES (
        gen_random_uuid(), 'e2e-sec-etf-001', 'E2ETF', 'E2E Test ETF Fund', 'etf', false,
        100.50, CURRENT_DATE - INTERVAL '1 day', 'USD'
    )
    ON CONFLICT (plaid_security_id) DO UPDATE SET
        ticker_symbol = EXCLUDED.ticker_symbol,
        name = EXCLUDED.name,
        type = EXCLUDED.type,
        is_cash_equivalent = EXCLUDED.is_cash_equivalent,
        close_price = EXCLUDED.close_price,
        close_price_as_of = EXCLUDED.close_price_as_of,
        currency_code = EXCLUDED.currency_code
    RETURNING id INTO v_real_security_id;

    -- A cash-equivalent security, so the Cash rows (and the bill-payment row below) have
    -- somewhere real to come from.
    INSERT INTO securities (
        id, plaid_security_id, ticker_symbol, name, type, is_cash_equivalent, currency_code
    ) VALUES (
        gen_random_uuid(), 'e2e-sec-cash-usd', 'CUR:USD', 'US Dollar', 'cash', true, 'USD'
    )
    ON CONFLICT (plaid_security_id) DO UPDATE SET
        ticker_symbol = EXCLUDED.ticker_symbol,
        name = EXCLUDED.name,
        type = EXCLUDED.type,
        is_cash_equivalent = EXCLUDED.is_cash_equivalent,
        currency_code = EXCLUDED.currency_code
    RETURNING id INTO v_cash_security_id;

    -- A holding on the real security, so the Portfolio tab has a value, a gain/loss and a day
    -- change to render.
    INSERT INTO holdings (
        id, user_id, account_id, security_id, quantity, cost_basis,
        institution_value, institution_price, currency_code
    ) VALUES (
        gen_random_uuid(), v_user_id, v_investment_account_id, v_real_security_id,
        10, 900.00, 1005.00, 100.50, 'USD'
    )
    ON CONFLICT (account_id, security_id) DO UPDATE SET
        quantity = EXCLUDED.quantity,
        cost_basis = EXCLUDED.cost_basis,
        institution_value = EXCLUDED.institution_value,
        institution_price = EXCLUDED.institution_price,
        currency_code = EXCLUDED.currency_code;

    -- The reinvestment PAIR, against the REAL security. amount < 0, quantity = 0 (structural
    -- dividend leg) -- displays as Dividend / "Dividend" sub-category.
    --
    -- ⚠ All three investment_transactions rows below use a DISTINCT absolute amount
    -- (18.21 / 24.68 / 31.09). TransactionTable's unified feed renders every row against this
    -- SAME security as an identical "E2ETF · E2E Test ETF Fund" description cell (ticker ·
    -- security_name takes priority over the per-row `name`), so the amount is the only text in
    -- the row that portfolio-and-investment-activity.auth.spec.ts can use to find ONE specific
    -- row unambiguously.
    INSERT INTO investment_transactions (
        id, user_id, account_id, security_id, plaid_investment_transaction_id,
        type, subtype, name, quantity, amount, price, date, currency_code
    ) VALUES (
        gen_random_uuid(), v_user_id, v_investment_account_id, v_real_security_id,
        'e2e-inv-txn-dividend-001', 'cash', 'deposit', 'E2E Test ETF Fund Dividend',
        0, -18.21, NULL, CURRENT_DATE - INTERVAL '1 day', 'USD'
    )
    ON CONFLICT (plaid_investment_transaction_id) DO UPDATE SET
        amount = EXCLUDED.amount, quantity = EXCLUDED.quantity, date = EXCLUDED.date;

    -- amount > 0, quantity > 0 (structural reinvest leg) -- displays as Reinvest / "Dividend
    -- Reinvestment" sub-category (NOT the raw subtype, "Withdrawal").
    INSERT INTO investment_transactions (
        id, user_id, account_id, security_id, plaid_investment_transaction_id,
        type, subtype, name, quantity, amount, price, date, currency_code
    ) VALUES (
        gen_random_uuid(), v_user_id, v_investment_account_id, v_real_security_id,
        'e2e-inv-txn-reinvest-001', 'cash', 'withdrawal', 'E2E Test ETF Fund Reinvestment',
        0.2455, 24.68, 100.50, CURRENT_DATE - INTERVAL '1 day', 'USD'
    )
    ON CONFLICT (plaid_investment_transaction_id) DO UPDATE SET
        amount = EXCLUDED.amount, quantity = EXCLUDED.quantity, price = EXCLUDED.price,
        date = EXCLUDED.date;

    -- A bill-payment row against the CASH-EQUIVALENT security. Same raw shape as the reinvestment
    -- leg above (cash/withdrawal) but must still display as Cash -- the negative control that
    -- proves the fix discriminates rather than relabelling every cash/withdrawal row.
    INSERT INTO investment_transactions (
        id, user_id, account_id, security_id, plaid_investment_transaction_id,
        type, subtype, name, quantity, amount, price, date, currency_code
    ) VALUES (
        gen_random_uuid(), v_user_id, v_investment_account_id, v_cash_security_id,
        'e2e-inv-txn-billpay-001', 'cash', 'withdrawal', 'E2E Advisory Fee Payment',
        NULL, 31.09, NULL, CURRENT_DATE - INTERVAL '1 day', 'USD'
    )
    ON CONFLICT (plaid_investment_transaction_id) DO UPDATE SET
        amount = EXCLUDED.amount, date = EXCLUDED.date;

    RAISE NOTICE 'E2E investment fixtures seeded (1 holding, 3 investment transactions) for user %', v_user_id;
END $$;

-- =============================================================================
-- Verification: Show seeded data summary
-- =============================================================================
SELECT 'Users' as table_name, COUNT(*) as count FROM users WHERE email = 'e2e+clerk_test@boostedbudget.com'
UNION ALL
SELECT 'User Identities', COUNT(*) FROM user_identities ui JOIN users u ON ui.user_id = u.id WHERE u.email = 'e2e+clerk_test@boostedbudget.com'
UNION ALL
SELECT 'Privacy Settings', COUNT(*) FROM privacy_settings ps JOIN users u ON ps.user_id = u.id WHERE u.email = 'e2e+clerk_test@boostedbudget.com'
UNION ALL
SELECT 'Linked Items', COUNT(*) FROM linked_items li JOIN users u ON li.user_id = u.id WHERE u.email = 'e2e+clerk_test@boostedbudget.com'
UNION ALL
SELECT 'Accounts', COUNT(*) FROM accounts a JOIN users u ON a.user_id = u.id WHERE u.email = 'e2e+clerk_test@boostedbudget.com'
UNION ALL
SELECT 'Transactions', COUNT(*) FROM transactions t JOIN users u ON t.user_id = u.id WHERE u.email = 'e2e+clerk_test@boostedbudget.com'
UNION ALL
SELECT 'User Tags', COUNT(*) FROM user_tags ut JOIN users u ON ut.user_id = u.id WHERE u.email = 'e2e+clerk_test@boostedbudget.com'
UNION ALL
SELECT 'Custom Categories', COUNT(*) FROM custom_categories cc JOIN users u ON cc.user_id = u.id WHERE u.email = 'e2e+clerk_test@boostedbudget.com'
UNION ALL
SELECT 'Budgets', COUNT(*) FROM budgets b JOIN users u ON b.user_id = u.id WHERE u.email = 'e2e+clerk_test@boostedbudget.com'
UNION ALL
SELECT 'Budget Categories', COUNT(*) FROM budget_categories bc JOIN budgets b ON bc.budget_id = b.id JOIN users u ON b.user_id = u.id WHERE u.email = 'e2e+clerk_test@boostedbudget.com'
UNION ALL
SELECT 'Holdings', COUNT(*) FROM holdings h JOIN users u ON h.user_id = u.id WHERE u.email = 'e2e+clerk_test@boostedbudget.com'
UNION ALL
SELECT 'Investment Transactions', COUNT(*) FROM investment_transactions it JOIN users u ON it.user_id = u.id WHERE u.email = 'e2e+clerk_test@boostedbudget.com';
