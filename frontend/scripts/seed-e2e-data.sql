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
-- Step 1: Create test user (or update if exists)
-- =============================================================================
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
    :clerk_user_id,
    :test_email,
    true,
    'us-east-1',
    'premium',  -- Premium tier for full E2E testing
    'active',
    true,
    false
)
ON CONFLICT (clerk_user_id) DO UPDATE SET
    email = EXCLUDED.email,
    subscription_tier = EXCLUDED.subscription_tier,
    subscription_status = EXCLUDED.subscription_status,
    is_active = EXCLUDED.is_active,
    is_deleted = EXCLUDED.is_deleted,
    updated_at = NOW();

-- Store user ID for subsequent inserts
DO $$
DECLARE
    v_user_id UUID;
    v_linked_item_id UUID;
    v_checking_id UUID;
    v_savings_id UUID;
    v_credit_id UUID;
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
        last_synced_at
    ) VALUES (
        gen_random_uuid(),
        v_user_id,
        'e2e-test-item-001',
        'encrypted-fake-token-for-e2e-tests',  -- Not a real token
        'ins_e2e_test',
        'E2E Test Bank',
        NULL,
        NOW()
    )
    ON CONFLICT (user_id, item_id) DO UPDATE SET
        institution_name = EXCLUDED.institution_name,
        last_synced_at = NOW()
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
    -- Step 4: Create test transactions (last 30 days)
    -- =============================================================================

    -- Clear existing test transactions for this user
    DELETE FROM transactions WHERE user_id = v_user_id AND source = 'plaid' AND external_transaction_id LIKE 'e2e-%';

    -- Insert diverse transactions for testing
    INSERT INTO transactions (
        user_id, account_id, linked_item_id, source, external_transaction_id,
        amount, date, name, merchant_name, category, payment_channel, pending,
        tags, category_source
    ) VALUES
    -- Groceries
    (v_user_id, v_checking_id, v_linked_item_id, 'plaid', 'e2e-txn-001',
     -125.50, CURRENT_DATE - INTERVAL '1 day', 'Whole Foods Market', 'Whole Foods',
     '["Food and Drink", "Groceries"]'::jsonb, 'in store', false,
     '["groceries", "essentials"]'::jsonb, 'plaid'),

    -- Restaurants
    (v_user_id, v_checking_id, v_linked_item_id, 'plaid', 'e2e-txn-002',
     -45.00, CURRENT_DATE - INTERVAL '2 days', 'Chipotle Mexican Grill', 'Chipotle',
     '["Food and Drink", "Restaurants"]'::jsonb, 'in store', false,
     '["dining"]'::jsonb, 'plaid'),

    -- Online Shopping
    (v_user_id, v_credit_id, v_linked_item_id, 'plaid', 'e2e-txn-003',
     -89.99, CURRENT_DATE - INTERVAL '3 days', 'Amazon.com', 'Amazon',
     '["Shops", "Online Marketplaces"]'::jsonb, 'online', false,
     '["shopping"]'::jsonb, 'plaid'),

    -- Gas
    (v_user_id, v_checking_id, v_linked_item_id, 'plaid', 'e2e-txn-004',
     -55.00, CURRENT_DATE - INTERVAL '4 days', 'Shell Gas Station', 'Shell',
     '["Travel", "Gas Stations"]'::jsonb, 'in store', false,
     '["transportation"]'::jsonb, 'plaid'),

    -- Paycheck (income)
    (v_user_id, v_checking_id, v_linked_item_id, 'plaid', 'e2e-txn-005',
     3500.00, CURRENT_DATE - INTERVAL '7 days', 'ACME Corp Payroll', 'ACME Corp',
     '["Transfer", "Payroll"]'::jsonb, 'other', false,
     '["income", "salary"]'::jsonb, 'plaid'),

    -- Utilities
    (v_user_id, v_checking_id, v_linked_item_id, 'plaid', 'e2e-txn-006',
     -120.00, CURRENT_DATE - INTERVAL '10 days', 'Electric Company', 'Electric Co',
     '["Service", "Utilities"]'::jsonb, 'online', false,
     '["bills", "utilities"]'::jsonb, 'plaid'),

    -- Subscription
    (v_user_id, v_credit_id, v_linked_item_id, 'plaid', 'e2e-txn-007',
     -15.99, CURRENT_DATE - INTERVAL '12 days', 'Netflix', 'Netflix',
     '["Service", "Subscription"]'::jsonb, 'online', false,
     '["entertainment", "subscriptions"]'::jsonb, 'plaid'),

    -- Coffee Shop
    (v_user_id, v_checking_id, v_linked_item_id, 'plaid', 'e2e-txn-008',
     -6.50, CURRENT_DATE - INTERVAL '1 day', 'Starbucks', 'Starbucks',
     '["Food and Drink", "Coffee Shop"]'::jsonb, 'in store', false,
     '["coffee"]'::jsonb, 'plaid'),

    -- Transfer to Savings
    (v_user_id, v_checking_id, v_linked_item_id, 'plaid', 'e2e-txn-009',
     -500.00, CURRENT_DATE - INTERVAL '15 days', 'Transfer to Savings', NULL,
     '["Transfer", "Internal Account Transfer"]'::jsonb, 'other', false,
     '["savings"]'::jsonb, 'plaid'),

    -- Credit Card Payment
    (v_user_id, v_checking_id, v_linked_item_id, 'plaid', 'e2e-txn-010',
     -250.00, CURRENT_DATE - INTERVAL '5 days', 'Credit Card Payment', NULL,
     '["Transfer", "Credit Card"]'::jsonb, 'other', false,
     '["bills"]'::jsonb, 'plaid'),

    -- Pending Transaction
    (v_user_id, v_checking_id, v_linked_item_id, 'plaid', 'e2e-txn-011',
     -35.00, CURRENT_DATE, 'Uber Eats', 'Uber Eats',
     '["Food and Drink", "Restaurants"]'::jsonb, 'online', true,
     '["dining"]'::jsonb, 'plaid'),

    -- Healthcare
    (v_user_id, v_credit_id, v_linked_item_id, 'plaid', 'e2e-txn-012',
     -75.00, CURRENT_DATE - INTERVAL '20 days', 'CVS Pharmacy', 'CVS',
     '["Healthcare", "Pharmacies"]'::jsonb, 'in store', false,
     '["health"]'::jsonb, 'plaid');

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
    INSERT INTO custom_categories (user_id, name, parent_category, icon, color, usage_count)
    VALUES
        (v_user_id, 'Work Expenses', 'Transfer', 'briefcase', '#607D8B', 0),
        (v_user_id, 'Side Hustle', 'Transfer', 'trending-up', '#00BCD4', 0),
        (v_user_id, 'Pet Supplies', 'Shops', 'heart', '#E91E63', 0)
    ON CONFLICT (user_id, name) DO NOTHING;

    RAISE NOTICE 'E2E test data seeded successfully for user %', v_user_id;
END $$;

-- =============================================================================
-- Verification: Show seeded data summary
-- =============================================================================
SELECT 'Users' as table_name, COUNT(*) as count FROM users WHERE email = 'e2e+clerk_test@boostedbudget.com'
UNION ALL
SELECT 'Linked Items', COUNT(*) FROM linked_items li JOIN users u ON li.user_id = u.id WHERE u.email = 'e2e+clerk_test@boostedbudget.com'
UNION ALL
SELECT 'Accounts', COUNT(*) FROM accounts a JOIN users u ON a.user_id = u.id WHERE u.email = 'e2e+clerk_test@boostedbudget.com'
UNION ALL
SELECT 'Transactions', COUNT(*) FROM transactions t JOIN users u ON t.user_id = u.id WHERE u.email = 'e2e+clerk_test@boostedbudget.com'
UNION ALL
SELECT 'User Tags', COUNT(*) FROM user_tags ut JOIN users u ON ut.user_id = u.id WHERE u.email = 'e2e+clerk_test@boostedbudget.com'
UNION ALL
SELECT 'Custom Categories', COUNT(*) FROM custom_categories cc JOIN users u ON cc.user_id = u.id WHERE u.email = 'e2e+clerk_test@boostedbudget.com';
